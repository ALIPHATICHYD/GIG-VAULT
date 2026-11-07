// Package gigs holds the escrow rules: booking, funding, completion,
// complaints, auto-release, cancellation, wallet and cash-out.
package gigs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/config"
	"github.com/aliphatichyd/gig-vault/backend/internal/ledger"
	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

// Job kinds handled by the worker.
const (
	JobBushaEvent     = "busha_event"
	JobAutoRelease    = "auto_release"
	JobMockSettlement = "mock_transfer_settle"
)

type Service struct {
	DB    *pgxpool.Pool
	Busha busha.API
	Cfg   config.Config
	Log   *slog.Logger
}

// ---------- booking ----------

type CreateGigInput struct {
	WorkerID    string    `json:"worker_id"`
	Title       string    `json:"title"`
	ClientName  string    `json:"client_name"`
	ClientEmail string    `json:"client_email"`
	EventAt     time.Time `json:"event_at"`
	Amount      string    `json:"amount"`   // decimal string in Currency
	Currency    string    `json:"currency"` // NGN (bank transfer) or USDT (client abroad)
}

func (s *Service) CreateGig(ctx context.Context, in CreateGigInput) (*Gig, error) {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "NGN"
	}
	switch {
	case in.WorkerID == "":
		return nil, badRequest("validation_error", "worker_id is required")
	case strings.TrimSpace(in.Title) == "":
		return nil, badRequest("validation_error", "title is required")
	case strings.TrimSpace(in.ClientName) == "" || !strings.Contains(in.ClientEmail, "@"):
		return nil, badRequest("validation_error", "client_name and a valid client_email are required")
	case in.EventAt.IsZero():
		return nil, badRequest("validation_error", "event_at is required (RFC 3339)")
	case in.Currency != "NGN" && in.Currency != "USDT":
		return nil, badRequest("validation_error", "currency must be NGN or USDT")
	}
	if _, err := getWorker(ctx, s.DB, in.WorkerID); err != nil {
		return nil, err
	}

	id := store.NewID("GIG")
	payIn := &busha.PayIn{Type: "temporary_bank_account"}
	if in.Currency == "USDT" {
		payIn = &busha.PayIn{Type: "address", Network: "TRX"}
	}
	req := busha.PaymentRequestInput{SourceCurrency: in.Currency, TargetCurrency: "USDT", SourceAmount: in.Amount,
		PayIn: payIn, Reference: id}
	req.AdditionalInfo.Email = in.ClientEmail
	req.AdditionalInfo.Name = in.ClientName

	var amountKobo, usdtMicro int64
	var rate float64
	var err error
	if in.Currency == "NGN" {
		if amountKobo, err = ParseDecimal(in.Amount, 2); err != nil || amountKobo <= 0 {
			return nil, badRequest("validation_error", "amount must be a positive number")
		}
	} else {
		if usdtMicro, err = ParseDecimal(in.Amount, 6); err != nil || usdtMicro <= 0 {
			return nil, badRequest("validation_error", "amount must be a positive number")
		}
	}

	// Ask Busha for a payment request: NGN via a temporary bank account, or USDT
	// to an on-chain address. Either way the funds settle to us as USDT.
	pr, err := s.Busha.CreatePaymentRequest(ctx, req)
	if err != nil {
		return nil, upstream("could not create Busha payment request", err)
	}
	if t, err := ParseDecimal(pr.TargetAmount, 6); err == nil && t > 0 && in.Currency == "NGN" {
		usdtMicro = t
	}
	if in.Currency == "NGN" && usdtMicro > 0 {
		rate = float64(amountKobo) / 100 / (float64(usdtMicro) / 1e6)
	}
	if in.Currency == "USDT" {
		rate = float64(s.Cfg.MockRateNGN)
		if q, err := s.Busha.CreateQuote(ctx, busha.QuoteInput{SourceCurrency: "USDT", TargetCurrency: "NGN", SourceAmount: in.Amount}); err == nil {
			if r, err := strconv.ParseFloat(q.Rate.Rate, 64); err == nil && r > 0 {
				rate = r
			}
		}
		amountKobo = usdtToKobo(usdtMicro, rate)
	}
	var usdtPtr *int64
	if usdtMicro > 0 {
		usdtPtr = &usdtMicro
	}
	var ratePtr *float64
	if rate > 0 {
		ratePtr = &rate
	}

	err = store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO gigs (id, worker_id, title, client_name, client_email, event_at, amount_ngn_kobo,
			amount_usdt_micro, rate_ngn_per_usdt, deposit_bps, freeze_bps, complaint_window_seconds, status, pay_in_method,
			busha_payment_request_id, payment_reference, payment_details)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			id, in.WorkerID, in.Title, in.ClientName, in.ClientEmail, in.EventAt, amountKobo, usdtPtr, ratePtr,
			s.Cfg.DepositBps, s.Cfg.FreezeBps, int(s.Cfg.ComplaintWindow.Seconds()), StatusAwaitingPayment, payIn.Type,
			pr.ID, id, []byte(pr.PayIn))
		if err != nil {
			return err
		}
		return store.Audit(ctx, tx, id, in.WorkerID, "client", "gig.created", "", StatusAwaitingPayment,
			map[string]any{"busha_payment_request_id": pr.ID, "currency": in.Currency, "amount": in.Amount, "busha_mode": s.Busha.Mode()})
	})
	if err != nil {
		return nil, err
	}
	return getGig(ctx, s.DB, id, false)
}

// MarkFunded runs when Busha confirms the client paid. It is idempotent:
// a redelivered webhook finds the gig already BOOKED and does nothing.
func (s *Service) MarkFunded(ctx context.Context, reference string, settledUSDT string) error {
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM gigs WHERE payment_reference = $1`, reference).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("gig for reference " + reference)
			}
			return err
		}
		g, err := getGig(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if g.Status != StatusAwaitingPayment {
			s.Log.Info("funding already applied", "gig", g.ID, "status", g.Status)
			return nil
		}
		amount := int64(0)
		if g.AmountUSDTMicro != nil {
			amount = *g.AmountUSDTMicro
		}
		if v, err := ParseDecimal(settledUSDT, 6); err == nil && v > 0 {
			amount = v // trust what Busha actually settled
		}
		if amount <= 0 {
			return fmt.Errorf("gig %s funded with unknown USDT amount", g.ID)
		}
		if _, err := tx.Exec(ctx, `UPDATE gigs SET amount_usdt_micro = $2 WHERE id = $1`, g.ID, amount); err != nil {
			return err
		}
		if _, err := ledger.Move(ctx, tx, "fund:"+g.ID, "fund", g.ID, ledger.ExternalClient, ledger.Escrow(g.ID), amount); err != nil {
			return err
		}
		deposit := bps(amount, g.DepositBps)
		if _, err := ledger.Move(ctx, tx, "deposit:"+g.ID, "deposit_release", g.ID, ledger.Escrow(g.ID), ledger.Worker(g.WorkerID), deposit); err != nil {
			return err
		}
		return setStatus(ctx, tx, g, StatusBooked, "busha", "gig.funded",
			map[string]any{"usdt_micro": amount, "deposit_usdt_micro": deposit})
	})
}

// ---------- completion, complaints, release ----------

func (s *Service) Complete(ctx context.Context, gigID string) (*Gig, error) {
	err := store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		if g.Status != StatusBooked {
			return conflict("invalid_state", "only a BOOKED gig can be marked done (now "+g.Status+")")
		}
		releaseAt := time.Now().Add(time.Duration(g.ComplaintWindowSeconds) * time.Second)
		if _, err := tx.Exec(ctx, `UPDATE gigs SET done_at = now(), release_at = $2 WHERE id = $1`, g.ID, releaseAt); err != nil {
			return err
		}
		if err := store.Enqueue(ctx, tx, JobAutoRelease, map[string]string{"gig_id": g.ID}, "auto_release:"+g.ID, releaseAt); err != nil {
			return err
		}
		return setStatus(ctx, tx, g, StatusDonePending, "client", "gig.marked_done", map[string]any{"release_at": releaseAt})
	})
	if err != nil {
		return nil, err
	}
	return getGig(ctx, s.DB, gigID, false)
}

func (s *Service) Complain(ctx context.Context, gigID, reason string) (*Gig, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, badRequest("validation_error", "reason is required")
	}
	err := store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		if g.Status != StatusDonePending {
			return conflict("invalid_state", "complaints are only allowed while the complaint window is open")
		}
		if g.ReleaseAt != nil && time.Now().After(*g.ReleaseAt) {
			return conflict("window_closed", "the complaint window has closed")
		}
		escrow, err := ledger.Balance(ctx, tx, ledger.Escrow(g.ID))
		if err != nil {
			return err
		}
		freeze := bps(escrow, g.FreezeBps)
		if _, err := ledger.Move(ctx, tx, "freeze:"+g.ID, "freeze", g.ID, ledger.Escrow(g.ID), ledger.Frozen(g.ID), freeze); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO complaints (id, gig_id, reason, frozen_usdt_micro) VALUES ($1,$2,$3,$4)`,
			store.NewID("CMP"), g.ID, reason, freeze); err != nil {
			return err
		}
		return setStatus(ctx, tx, g, StatusDisputed, "client", "gig.complaint_opened",
			map[string]any{"reason": reason, "frozen_usdt_micro": freeze})
	})
	if err != nil {
		return nil, err
	}
	return getGig(ctx, s.DB, gigID, false)
}

// AutoRelease is run by the worker when the complaint window ends.
// The unfrozen remainder always goes to the worker; a frozen part waits for resolution.
func (s *Service) AutoRelease(ctx context.Context, gigID string) error {
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		if g.Status != StatusDonePending && g.Status != StatusDisputed {
			return nil // already settled or cancelled
		}
		if g.ReleaseAt != nil && time.Now().Before(*g.ReleaseAt) {
			return fmt.Errorf("release not due until %s", g.ReleaseAt.Format(time.RFC3339))
		}
		escrow, err := ledger.Balance(ctx, tx, ledger.Escrow(g.ID))
		if err != nil {
			return err
		}
		if _, err := ledger.Move(ctx, tx, "release:"+g.ID, "release", g.ID, ledger.Escrow(g.ID), ledger.Worker(g.WorkerID), escrow); err != nil {
			return err
		}
		if g.Status == StatusDisputed {
			return store.Audit(ctx, tx, g.ID, g.WorkerID, "system", "gig.unfrozen_part_released", g.Status, g.Status,
				map[string]any{"released_usdt_micro": escrow})
		}
		return setStatus(ctx, tx, g, StatusReleased, "system", "gig.auto_released", map[string]any{"released_usdt_micro": escrow})
	})
}

// ResolveComplaint settles the frozen part: to the worker, or back to the client.
func (s *Service) ResolveComplaint(ctx context.Context, gigID, outcome string) (*Gig, error) {
	if outcome != "worker" && outcome != "client" {
		return nil, badRequest("validation_error", "outcome must be worker or client")
	}
	err := store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		if g.Status != StatusDisputed {
			return conflict("invalid_state", "gig has no open complaint")
		}
		frozen, err := ledger.Balance(ctx, tx, ledger.Frozen(g.ID))
		if err != nil {
			return err
		}
		to, final := ledger.Worker(g.WorkerID), StatusReleased
		if outcome == "client" {
			to, final = ledger.ExternalRefund, StatusPartiallyRefunded
		}
		if _, err := ledger.Move(ctx, tx, "resolve:"+g.ID, "unfreeze", g.ID, ledger.Frozen(g.ID), to, frozen); err != nil {
			return err
		}
		// Whatever is still in escrow (window may not have ended) goes to the worker now.
		escrow, err := ledger.Balance(ctx, tx, ledger.Escrow(g.ID))
		if err != nil {
			return err
		}
		if _, err := ledger.Move(ctx, tx, "release:"+g.ID, "release", g.ID, ledger.Escrow(g.ID), ledger.Worker(g.WorkerID), escrow); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE complaints SET status = $2, resolved_at = now() WHERE gig_id = $1 AND status = 'open'`,
			g.ID, "resolved_"+outcome); err != nil {
			return err
		}
		return setStatus(ctx, tx, g, final, "admin", "gig.complaint_resolved",
			map[string]any{"outcome": outcome, "frozen_usdt_micro": frozen})
	})
	if err != nil {
		return nil, err
	}
	return getGig(ctx, s.DB, gigID, false)
}

// Cancel by the client. Before payment it just closes. After booking, the
// remaining escrow is refunded; the deposit stays with the worker on a late
// cancel and goes back to the client on an early one.
func (s *Service) Cancel(ctx context.Context, gigID string) (*Gig, error) {
	err := store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		g, err := getGig(ctx, tx, gigID, true)
		if err != nil {
			return err
		}
		switch g.Status {
		case StatusAwaitingPayment:
			return setStatus(ctx, tx, g, StatusCancelled, "client", "gig.cancelled", nil)
		case StatusBooked:
		default:
			return conflict("invalid_state", "a gig can't be cancelled once it is "+g.Status)
		}
		late := time.Until(g.EventAt) < s.Cfg.LateCancelWindow
		escrow, err := ledger.Balance(ctx, tx, ledger.Escrow(g.ID))
		if err != nil {
			return err
		}
		if _, err := ledger.Move(ctx, tx, "refund:"+g.ID, "refund", g.ID, ledger.Escrow(g.ID), ledger.ExternalRefund, escrow); err != nil {
			return err
		}
		depositBack := int64(0)
		if !late && g.AmountUSDTMicro != nil {
			depositBack = bps(*g.AmountUSDTMicro, g.DepositBps)
			if _, err := ledger.Move(ctx, tx, "deposit_refund:"+g.ID, "refund", g.ID, ledger.Worker(g.WorkerID), ledger.ExternalRefund, depositBack); err != nil {
				if errors.Is(err, ledger.ErrInsufficientFunds) {
					return conflict("deposit_spent", "the worker has already cashed out the deposit")
				}
				return err
			}
		}
		return setStatus(ctx, tx, g, StatusCancelled, "client", "gig.cancelled",
			map[string]any{"late": late, "refunded_usdt_micro": escrow + depositBack, "deposit_kept_by_worker": late})
	})
	if err != nil {
		return nil, err
	}
	return getGig(ctx, s.DB, gigID, false)
}

// ---------- reads ----------

type GigDetail struct {
	Gig        *Gig             `json:"gig"`
	Worker     *Worker          `json:"worker"`
	Balances   map[string]int64 `json:"balances_usdt_micro"`
	Ledger     []ledger.Entry   `json:"ledger"`
	Audit      []AuditRow       `json:"audit"`
	Complaints []Complaint      `json:"complaints"`
	BushaMode  string           `json:"busha_mode"`
}

type AuditRow struct {
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	From      *string         `json:"from_status"`
	To        *string         `json:"to_status"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

type Complaint struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	Frozen    int64     `json:"frozen_usdt_micro"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) GetGig(ctx context.Context, id string) (*GigDetail, error) {
	g, err := getGig(ctx, s.DB, id, false)
	if err != nil {
		return nil, err
	}
	w, err := getWorker(ctx, s.DB, g.WorkerID)
	if err != nil {
		return nil, err
	}
	d := &GigDetail{Gig: g, Worker: w, Balances: map[string]int64{}, BushaMode: s.Busha.Mode()}
	for k, acct := range map[string]string{"escrow": ledger.Escrow(id), "frozen": ledger.Frozen(id)} {
		if d.Balances[k], err = ledger.Balance(ctx, s.DB, acct); err != nil {
			return nil, err
		}
	}
	if d.Ledger, err = ledger.EntriesForGig(ctx, s.DB, id); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT actor, action, from_status, to_status, details, created_at FROM audit_log WHERE gig_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	d.Audit = []AuditRow{}
	for rows.Next() {
		var a AuditRow
		if err := rows.Scan(&a.Actor, &a.Action, &a.From, &a.To, &a.Details, &a.CreatedAt); err != nil {
			return nil, err
		}
		d.Audit = append(d.Audit, a)
	}
	crows, err := s.DB.Query(ctx, `SELECT id, reason, frozen_usdt_micro, status, created_at FROM complaints WHERE gig_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	d.Complaints = []Complaint{}
	for crows.Next() {
		var c Complaint
		if err := crows.Scan(&c.ID, &c.Reason, &c.Frozen, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		d.Complaints = append(d.Complaints, c)
	}
	return d, nil
}

func (s *Service) ListGigs(ctx context.Context, workerID string) ([]*Gig, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+gigCols+` FROM gigs WHERE ($1 = '' OR worker_id = $1) ORDER BY created_at DESC LIMIT 100`, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Gig{}
	for rows.Next() {
		g, err := scanGig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Service) ListWorkers(ctx context.Context) ([]*Worker, error) {
	rows, err := s.DB.Query(ctx, `SELECT id FROM workers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []*Worker{}
	for _, id := range ids {
		w, err := getWorker(ctx, s.DB, id)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

type CreateWorkerInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

func (s *Service) CreateWorker(ctx context.Context, in CreateWorkerInput) (*Worker, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, badRequest("validation_error", "name is required")
	}
	id := store.NewID("WRK")
	if _, err := s.DB.Exec(ctx, `INSERT INTO workers (id, name, email, phone) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''))`,
		id, in.Name, in.Email, in.Phone); err != nil {
		return nil, err
	}
	return getWorker(ctx, s.DB, id)
}

// EnsureDemoWorker seeds Chidinma so the demo works on a fresh database.
func (s *Service) EnsureDemoWorker(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO workers (id, name, email, phone, bank_name, account_number, account_name)
		VALUES ('WRK_chidinma', 'Chidinma Okafor', 'chidinma@example.com', '+2348000000000', 'GTBank', '0123456789', 'CHIDINMA OKAFOR')
		ON CONFLICT (id) DO NOTHING`)
	return err
}

// ---------- wallet ----------

type Wallet struct {
	Worker         *Worker  `json:"worker"`
	RateNGNPerUSDT float64  `json:"rate_ngn_per_usdt"`
	AvailableUSDT  int64    `json:"available_usdt_micro"`
	LockedUSDT     int64    `json:"locked_usdt_micro"`
	FrozenUSDT     int64    `json:"frozen_usdt_micro"`
	AvailableNGN   int64    `json:"available_ngn_kobo"`
	LockedNGN      int64    `json:"locked_ngn_kobo"`
	FrozenNGN      int64    `json:"frozen_ngn_kobo"`
	Gigs           []*Gig   `json:"gigs"`
	Payouts        []Payout `json:"payouts"`
	BushaMode      string   `json:"busha_mode"`
}

func (s *Service) Wallet(ctx context.Context, workerID string) (*Wallet, error) {
	w, err := getWorker(ctx, s.DB, workerID)
	if err != nil {
		return nil, err
	}
	wl := &Wallet{Worker: w, BushaMode: s.Busha.Mode(), RateNGNPerUSDT: float64(s.Cfg.MockRateNGN)}
	if wl.AvailableUSDT, err = ledger.Balance(ctx, s.DB, ledger.Worker(workerID)); err != nil {
		return nil, err
	}
	if wl.Gigs, err = s.ListGigs(ctx, workerID); err != nil {
		return nil, err
	}
	for _, g := range wl.Gigs {
		e, err := ledger.Balance(ctx, s.DB, ledger.Escrow(g.ID))
		if err != nil {
			return nil, err
		}
		f, err := ledger.Balance(ctx, s.DB, ledger.Frozen(g.ID))
		if err != nil {
			return nil, err
		}
		wl.LockedUSDT += e
		wl.FrozenUSDT += f
	}
	// Latest rate we saw from Busha for this worker's gigs.
	for _, g := range wl.Gigs {
		if g.RateNGNPerUSDT != nil && *g.RateNGNPerUSDT > 0 {
			wl.RateNGNPerUSDT = *g.RateNGNPerUSDT
			break
		}
	}
	wl.AvailableNGN = usdtToKobo(wl.AvailableUSDT, wl.RateNGNPerUSDT)
	wl.LockedNGN = usdtToKobo(wl.LockedUSDT, wl.RateNGNPerUSDT)
	wl.FrozenNGN = usdtToKobo(wl.FrozenUSDT, wl.RateNGNPerUSDT)
	if wl.Payouts, err = s.listPayouts(ctx, workerID); err != nil {
		return nil, err
	}
	return wl, nil
}
