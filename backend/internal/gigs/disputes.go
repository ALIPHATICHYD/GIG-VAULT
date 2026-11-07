package gigs

import (
	"context"
	"errors"
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
