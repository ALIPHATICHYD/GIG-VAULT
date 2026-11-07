package gigs

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aliphatichyd/gig-vault/backend/internal/ledger"
	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

// Dispute flow
//
//  1. Client raises a dispute inside the complaint window. Part of the remaining
//     escrow is frozen; the rest still releases to the creator on time.
//  2. Both sides have DISPUTE_WINDOW (3 days) to agree a split of the frozen money:
//     one side proposes "creator gets X%", the other accepts. No fee.
//  3. No agreement in time: the dispute is escalated and GigVault decides.
//     GigVault keeps DISPUTE_FEE_BPS (10%) of the frozen money as a resolution fee.
//
// Frozen money never goes back to anyone automatically.

const PlatformFees = "platform:fees"

type Dispute struct {
	ID                 string     `json:"id"`
	Reason             string     `json:"reason"`
	Frozen             int64      `json:"frozen_usdt_micro"`
	Status             string     `json:"status"` // open, settled, escalated, resolved
	RaisedBy           string     `json:"raised_by"`
	NegotiateUntil     *time.Time `json:"negotiate_until"`
	ProposalCreatorPct *int       `json:"proposal_creator_percent"`
	ProposedBy         *string    `json:"proposed_by"`
	OutcomeCreatorPct  *int       `json:"outcome_creator_percent"`
	Fee                int64      `json:"fee_usdt_micro"`
	CreatedAt          time.Time  `json:"created_at"`
}

const disputeCols = `id, reason, frozen_usdt_micro, status, raised_by, negotiate_until,
	proposal_creator_bps / 100, proposed_by, outcome_creator_bps / 100, fee_usdt_micro, created_at`

func scanDispute(row pgx.Row) (*Dispute, error) {
	var d Dispute
	err := row.Scan(&d.ID, &d.Reason, &d.Frozen, &d.Status, &d.RaisedBy, &d.NegotiateUntil,
		&d.ProposalCreatorPct, &d.ProposedBy, &d.OutcomeCreatorPct, &d.Fee, &d.CreatedAt)
	return &d, err
}

func (s *Service) disputes(ctx context.Context, gigID string) ([]Dispute, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+disputeCols+` FROM complaints WHERE gig_id = $1 ORDER BY created_at`, gigID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dispute{}
	for rows.Next() {
		d, err := scanDispute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ---------- admin: the arbitration queue ----------

// EscalatedDispute is one row of GigVault's to-do list: a dispute both sides
// failed to settle, with everything needed to decide a split.
type EscalatedDispute struct {
	DisputeID       string            `json:"dispute_id"`
	GigID           string            `json:"gig_id"`
	Title           string            `json:"title"`
	AmountNGNKobo   int64             `json:"amount_ngn_kobo"`
	Reason          string            `json:"reason"`
	RaisedBy        string            `json:"raised_by"`
	FrozenUSDTMicro int64             `json:"frozen_usdt_micro"`
	FrozenNGNKobo   int64             `json:"frozen_ngn_kobo"`
	Creator         DisputeParty      `json:"creator"`
	Client          DisputeParty      `json:"client"`
	Proposals       []DisputeProposal `json:"proposals"`
	OpenedAt        time.Time         `json:"opened_at"`
	EscalatedAt     *time.Time        `json:"escalated_at"`
	FeePercent      int               `json:"fee_percent"`
}

type DisputeParty struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
}

// DisputeProposal is one side's most recent offer: "the creator should get X%
// of the frozen money".
type DisputeProposal struct {
	By             string    `json:"by"` // creator | client
	CreatorPercent int       `json:"creator_percent"`
	At             time.Time `json:"at"`
}

// EscalatedDisputes lists the disputes awaiting GigVault's decision. A resolved
// dispute drops off on its own, because ResolveDispute moves it off 'escalated'.
//
// Each side's last proposal comes from audit_log, not from complaints: that
// table keeps a single proposal column that each new proposal overwrites, so it
// cannot say what both sides last offered. The audit log has every one, with
// the actor who made it. The escalation time comes from the same place, which
// is why none of this needs a schema change.
func (s *Service) EscalatedDisputes(ctx context.Context) ([]EscalatedDispute, error) {
	rows, err := s.DB.Query(ctx, `SELECT c.id, c.gig_id, g.title, g.amount_ngn_kobo, g.amount_usdt_micro,
			c.reason, c.raised_by, c.created_at, w.name, w.email,
			COALESCE(u.name, g.client_name), COALESCE(u.email, g.client_email)
		FROM complaints c
		JOIN gigs g    ON g.id = c.gig_id
		JOIN workers w ON w.id = g.worker_id
		LEFT JOIN users u ON u.id = g.client_user_id
		WHERE c.status = 'escalated'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EscalatedDispute{}
	gigIDs := []string{}
	totals := map[string]int64{} // gig id -> amount_usdt_micro, for the naira conversion
	for rows.Next() {
		var d EscalatedDispute
		var usdtTotal *int64
		if err := rows.Scan(&d.DisputeID, &d.GigID, &d.Title, &d.AmountNGNKobo, &usdtTotal,
			&d.Reason, &d.RaisedBy, &d.OpenedAt, &d.Creator.Name, &d.Creator.Email,
			&d.Client.Name, &d.Client.Email); err != nil {
			return nil, err
		}
		d.FeePercent = s.Cfg.DisputeFeeBps / 100
		d.Proposals = []DisputeProposal{}
		if usdtTotal != nil {
			totals[d.GigID] = *usdtTotal
		}
		gigIDs = append(gigIDs, d.GigID)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	proposals, err := s.lastProposals(ctx, gigIDs)
	if err != nil {
		return nil, err
	}
	escalatedAt, err := s.escalationTimes(ctx, gigIDs)
	if err != nil {
		return nil, err
	}

	for i := range out {
		d := &out[i]
		d.Proposals = proposals[d.GigID]
		if at, ok := escalatedAt[d.GigID]; ok {
			d.EscalatedAt = &at
		}
		// Read the frozen amount from the ledger rather than the stored
		// column, so a partial release is reflected.
		if d.FrozenUSDTMicro, err = ledger.Balance(ctx, s.DB, ledger.Frozen(d.GigID)); err != nil {
			return nil, err
		}
		if total := totals[d.GigID]; total > 0 {
			d.FrozenNGNKobo = d.AmountNGNKobo * d.FrozenUSDTMicro / total
		}
	}

	// Oldest escalation first: that is the one GigVault has kept waiting longest.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].EscalatedAt, out[j].EscalatedAt
		switch {
		case a != nil && b != nil:
			return a.Before(*b)
		case a != nil:
			return true
		case b != nil:
			return false
		default:
			return out[i].OpenedAt.Before(out[j].OpenedAt)
		}
	})
	return out, nil
}

// lastProposals returns each side's most recent proposal per gig, newest wins.
func (s *Service) lastProposals(ctx context.Context, gigIDs []string) (map[string][]DisputeProposal, error) {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT ON (gig_id, actor)
			gig_id, actor, (details->>'creator_percent')::int, created_at
		FROM audit_log
		WHERE action = 'dispute.split_proposed' AND gig_id = ANY($1)
		  AND details ? 'creator_percent'
		ORDER BY gig_id, actor, id DESC`, gigIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byGig := map[string][]DisputeProposal{}
	for rows.Next() {
		var gigID string
		var p DisputeProposal
		if err := rows.Scan(&gigID, &p.By, &p.CreatorPercent, &p.At); err != nil {
			return nil, err
		}
		byGig[gigID] = append(byGig[gigID], p)
	}
	return byGig, rows.Err()
}

func (s *Service) escalationTimes(ctx context.Context, gigIDs []string) (map[string]time.Time, error) {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT ON (gig_id) gig_id, created_at
		FROM audit_log
		WHERE action = 'dispute.escalated_to_gigvault' AND gig_id = ANY($1)
		ORDER BY gig_id, id DESC`, gigIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	at := map[string]time.Time{}
	for rows.Next() {
		var gigID string
		var t time.Time
		if err := rows.Scan(&gigID, &t); err != nil {
			return nil, err
		}
		at[gigID] = t
	}
	return at, rows.Err()
}

func openDispute(ctx context.Context, tx pgx.Tx, gigID string) (*Dispute, error) {
	d, err := scanDispute(tx.QueryRow(ctx, `SELECT `+disputeCols+` FROM complaints
		WHERE gig_id = $1 AND status IN ('open','escalated') ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, gigID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, conflict("no_dispute", "there is no open dispute on this gig")
	}
	return d, err
}

// ProposeSplit records "the creator should get pct% of the frozen money".
func (s *Service) ProposeSplit(ctx context.Context, gigID, by string, pct int) error {
	if pct < 0 || pct > 100 {
		return badRequest("validation_error", "creator_percent must be between 0 and 100")
	}
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		if g.Status != StatusDisputed {
			return conflict("invalid_state", "proposals are only possible while the dispute is being negotiated")
		}
		d, err := openDispute(ctx, tx, gigID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET proposal_creator_bps = $2, proposed_by = $3 WHERE id = $1`, d.ID, pct*100, by); err != nil {
			return err
		}
		return store.Audit(ctx, tx, g.ID, g.WorkerID, by, "dispute.split_proposed", "", "", map[string]int{"creator_percent": pct})
	})
}

// AcceptSplit settles the dispute on the other side's proposal. No fee.
func (s *Service) AcceptSplit(ctx context.Context, gigID, by string) error {
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		if g.Status != StatusDisputed {
			return conflict("invalid_state", "this dispute can no longer be settled between you")
		}
		d, err := openDispute(ctx, tx, gigID)
		if err != nil {
			return err
		}
		if d.ProposalCreatorPct == nil || d.ProposedBy == nil {
			return conflict("no_proposal", "there is no proposal to accept yet")
		}
		if *d.ProposedBy == by {
			return conflict("own_proposal", "the other side has to accept your proposal")
		}
		return s.settle(ctx, tx, g, d, *d.ProposalCreatorPct, 0, by, "dispute.settled_by_agreement", "settled")
	})
}

// EscalateDispute runs when the negotiation window ends without agreement.
func (s *Service) EscalateDispute(ctx context.Context, gigID string) error {
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		if g.Status != StatusDisputed {
			return nil // already settled
		}
		d, err := openDispute(ctx, tx, gigID)
		if err != nil {
			return err
		}
		if d.NegotiateUntil != nil && time.Now().Before(*d.NegotiateUntil) {
			return errors.New("dispute window still open")
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET status = 'escalated' WHERE id = $1`, d.ID); err != nil {
			return err
		}
		return setStatus(ctx, tx, g, StatusEscalated, "system", "dispute.escalated_to_gigvault", map[string]any{
			"fee_percent": s.Cfg.DisputeFeeBps / 100})
	})
}

// ResolveDispute is GigVault's decision on an escalated dispute. GigVault keeps
// its resolution fee from the frozen money, then splits the rest.
func (s *Service) ResolveDispute(ctx context.Context, gigID string, creatorPct int) (*Gig, error) {
	if creatorPct < 0 || creatorPct > 100 {
		return nil, badRequest("validation_error", "creator_percent must be between 0 and 100")
	}
	err := store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		if g.Status != StatusEscalated {
			return conflict("invalid_state", "GigVault only steps in once the 3-day window has passed without agreement")
		}
		d, err := openDispute(ctx, tx, gigID)
		if err != nil {
			return err
		}
		return s.settle(ctx, tx, g, d, creatorPct, s.Cfg.DisputeFeeBps, "admin", "dispute.resolved_by_gigvault", "resolved")
	})
	if err != nil {
		return nil, err
	}
	return getGig(ctx, s.DB, gigID, false)
}

func (s *Service) settle(ctx context.Context, tx pgx.Tx, g *Gig, d *Dispute, creatorPct, feeBps int, actor, action, status string) error {
	frozen, err := ledger.Balance(ctx, tx, ledger.Frozen(g.ID))
	if err != nil {
		return err
	}
	fee := bps(frozen, feeBps)
	toCreator := bps(frozen-fee, creatorPct*100)
	toClient := frozen - fee - toCreator
	moves := []struct {
		key, kind, to string
		amt           int64
	}{
		{"dispute_fee:", "dispute_fee", PlatformFees, fee},
		{"dispute_creator:", "unfreeze", ledger.Worker(g.WorkerID), toCreator},
		{"dispute_client:", "refund", ledger.ExternalRefund, toClient},
	}
	for _, m := range moves {
		if _, err := ledger.Move(ctx, tx, m.key+d.ID, m.kind, g.ID, ledger.Frozen(g.ID), m.to, m.amt); err != nil {
			return err
		}
	}
	// Anything still in escrow (complaint window not over yet) goes to the creator.
	escrow, err := ledger.Balance(ctx, tx, ledger.Escrow(g.ID))
	if err != nil {
		return err
	}
	if _, err := ledger.Move(ctx, tx, "release:"+g.ID, "release", g.ID, ledger.Escrow(g.ID), ledger.Worker(g.WorkerID), escrow); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE complaints SET status = $2, outcome_creator_bps = $3, fee_usdt_micro = $4 WHERE id = $1`,
		d.ID, status, creatorPct*100, fee); err != nil {
		return err
	}
	final := StatusReleased
	if toClient > 0 {
		final = StatusPartiallyRefunded
	}
	return setStatus(ctx, tx, g, final, actor, action, map[string]any{
		"creator_percent": creatorPct, "to_creator_usdt_micro": toCreator, "to_client_usdt_micro": toClient, "fee_usdt_micro": fee})
}
