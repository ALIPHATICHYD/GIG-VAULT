package gigs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/ledger"
	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

type Payout struct {
	ID              string    `json:"id"`
	AmountUSDTMicro int64     `json:"amount_usdt_micro"`
	AmountNGNKobo   int64     `json:"amount_ngn_kobo"`
	BushaQuoteID    *string   `json:"busha_quote_id"`
	BushaTransferID *string   `json:"busha_transfer_id"`
	Status          string    `json:"status"`
	FailureReason   *string   `json:"failure_reason"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Service) listPayouts(ctx context.Context, workerID string) ([]Payout, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, amount_usdt_micro, amount_ngn_kobo, busha_quote_id, busha_transfer_id, status, failure_reason, created_at
		FROM payouts WHERE worker_id = $1 ORDER BY created_at DESC LIMIT 20`, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payout{}
	for rows.Next() {
		var p Payout
		if err := rows.Scan(&p.ID, &p.AmountUSDTMicro, &p.AmountNGNKobo, &p.BushaQuoteID, &p.BushaTransferID, &p.Status, &p.FailureReason, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type CashoutInput struct {
	AmountNGN string `json:"amount_ngn"`
}

// Cashout sends naira to the worker's bank:
//  1. Busha quote USDT -> NGN with pay_out bank_transfer to her recipient
//  2. reserve the USDT in our ledger (worker -> external:payout)
//  3. Busha transfer to execute the quote
//
// If step 3 fails the reservation is reversed, so she never loses money to an error.
func (s *Service) Cashout(ctx context.Context, workerID string, in CashoutInput) (*Payout, error) {
	w, err := getWorker(ctx, s.DB, workerID)
	if err != nil {
		return nil, err
	}
	kobo, err := ParseDecimal(in.AmountNGN, 2)
	if err != nil || kobo <= 0 {
		return nil, badRequest("validation_error", "amount_ngn must be a positive number")
	}
	recipient := ""
	if w.BushaRecipientID != nil {
		recipient = *w.BushaRecipientID
	}
	if recipient == "" && s.Busha.Mode() == "live" {
		if recipient, err = s.ensureRecipient(ctx, w); err != nil {
			return nil, err
		}
	}

	q, err := s.Busha.CreateQuote(ctx, busha.QuoteInput{SourceCurrency: "USDT", TargetCurrency: "NGN",
		TargetAmount: in.AmountNGN, PayOut: &busha.PayOut{Type: "bank_transfer", RecipientID: recipient}})
	if err != nil {
		return nil, upstream("could not get a cash-out quote from Busha", err)
	}
	usdt, err := ParseDecimal(q.SourceAmount, 6)
	if err != nil || usdt <= 0 {
		return nil, upstream("Busha quote had no source amount", err)
	}

	p := &Payout{ID: store.NewID("PAY"), AmountUSDTMicro: usdt, AmountNGNKobo: kobo, BushaQuoteID: &q.ID, Status: "processing"}
	err = store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := ledger.Move(ctx, tx, "payout:"+p.ID, "payout", "", ledger.Worker(workerID), ledger.ExternalPayout, usdt); err != nil {
			if errors.Is(err, ledger.ErrInsufficientFunds) {
				return conflict("insufficient_funds", "not enough available balance for this cash-out")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO payouts (id, worker_id, amount_usdt_micro, amount_ngn_kobo, busha_quote_id, status)
			VALUES ($1,$2,$3,$4,$5,'processing')`, p.ID, workerID, usdt, kobo, q.ID); err != nil {
			return err
		}
		return store.Audit(ctx, tx, "", workerID, "worker", "payout.requested", "", "processing",
			map[string]any{"payout_id": p.ID, "quote_id": q.ID, "usdt_micro": usdt, "ngn_kobo": kobo})
	})
	if err != nil {
		return nil, err
	}

	t, terr := s.Busha.CreateTransfer(ctx, q.ID)
	if terr != nil {
		_ = s.failPayout(ctx, p.ID, terr.Error())
		return nil, upstream("Busha transfer failed; your balance was restored", terr)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE payouts SET busha_transfer_id = $2, updated_at = now() WHERE id = $1`, p.ID, t.ID); err != nil {
		return nil, err
	}
	_ = store.Audit(ctx, s.DB, "", workerID, "busha", "busha.transfer_created", "", "",
		map[string]any{"payout_id": p.ID, "transfer_id": t.ID, "quote_id": q.ID, "ngn_kobo": kobo, "status": t.Status})
	p.BushaTransferID = &t.ID
	if t.Mock {
		// No real webhook will come in mock mode, so simulate the settlement.
		_ = store.Enqueue(ctx, s.DB, JobMockSettlement, map[string]string{"transfer_id": t.ID}, "mock_settle:"+t.ID, time.Now().Add(3*time.Second))
	}
	return p, nil
}

func (s *Service) CompletePayoutByTransfer(ctx context.Context, transferID string) error {
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		var id, worker, status string
		err := tx.QueryRow(ctx, `SELECT id, worker_id, status FROM payouts WHERE busha_transfer_id = $1 FOR UPDATE`, transferID).Scan(&id, &worker, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not one of ours (e.g. a funding conversion)
		}
		if err != nil || status != "processing" {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'completed', updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		var k int64
		_ = tx.QueryRow(ctx, `SELECT amount_ngn_kobo FROM payouts WHERE id = $1`, id).Scan(&k)
		return store.Audit(ctx, tx, "", worker, "busha", "payout.completed", "processing", "completed", map[string]any{"payout_id": id, "transfer_id": transferID, "ngn_kobo": k})
	})
}

func (s *Service) FailPayoutByTransfer(ctx context.Context, transferID, reason string) error {
	var id string
	err := s.DB.QueryRow(ctx, `SELECT id FROM payouts WHERE busha_transfer_id = $1`, transferID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.failPayout(ctx, id, reason)
}

// failPayout marks a payout failed and gives the money back, exactly once.
func (s *Service) failPayout(ctx context.Context, payoutID, reason string) error {
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		var worker, status string
		var usdt int64
		if err := tx.QueryRow(ctx, `SELECT worker_id, status, amount_usdt_micro FROM payouts WHERE id = $1 FOR UPDATE`, payoutID).Scan(&worker, &status, &usdt); err != nil {
			return err
		}
		if status != "processing" {
			return nil
		}
		if _, err := ledger.Move(ctx, tx, "payout_reversal:"+payoutID, "payout_reversal", "", ledger.ExternalPayout, ledger.Worker(worker), usdt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE payouts SET status = 'failed', failure_reason = $2, updated_at = now() WHERE id = $1`, payoutID, reason); err != nil {
			return err
		}
		return store.Audit(ctx, tx, "", worker, "system", "payout.failed_reversed", "processing", "failed", map[string]any{"payout_id": payoutID, "reason": reason})
	})
}

// ---------- reconciliation ----------

type Reconciliation struct {
	BushaMode          string          `json:"busha_mode"`
	LedgerOwedUSDT     int64           `json:"ledger_owed_usdt_micro"` // escrow + frozen + worker balances
	EscrowUSDT         int64           `json:"escrow_usdt_micro"`
	FrozenUSDT         int64           `json:"frozen_usdt_micro"`
	WorkerBalancesUSDT int64           `json:"worker_balances_usdt_micro"`
	PlatformFeesUSDT   int64           `json:"platform_fees_usdt_micro"`
	BushaUSDT          *string         `json:"busha_usdt_total"`
	BushaBalances      []busha.Balance `json:"busha_balances"`
	BushaError         *string         `json:"busha_error,omitempty"`
}

func (s *Service) Reconcile(ctx context.Context) (*Reconciliation, error) {
	r := &Reconciliation{BushaMode: s.Busha.Mode()}
	var err error
	if r.EscrowUSDT, err = ledger.SumPrefix(ctx, s.DB, "escrow:"); err != nil {
		return nil, err
	}
	if r.FrozenUSDT, err = ledger.SumPrefix(ctx, s.DB, "frozen:"); err != nil {
		return nil, err
	}
	if r.WorkerBalancesUSDT, err = ledger.SumPrefix(ctx, s.DB, "worker:"); err != nil {
		return nil, err
	}
	if r.PlatformFeesUSDT, err = ledger.Balance(ctx, s.DB, PlatformFees); err != nil {
		return nil, err
	}
	r.LedgerOwedUSDT = r.EscrowUSDT + r.FrozenUSDT + r.WorkerBalancesUSDT
	bals, err := s.Busha.ListBalances(ctx)
	if err != nil {
		msg := err.Error()
		r.BushaError = &msg
		return r, nil
	}
	r.BushaBalances = bals
	for _, b := range bals {
		if b.Currency == "USDT" {
			v := b.Total.Amount
			r.BushaUSDT = &v
		}
	}
	return r, nil
}

// ---------- Busha webhook events ----------

// ProcessEvent applies one stored Busha webhook. Safe to run more than once.
func (s *Service) ProcessEvent(ctx context.Context, eventRowID int64) error {
	var name string
	var payload []byte
	var processed *time.Time
	if err := s.DB.QueryRow(ctx, `SELECT event, payload, processed_at FROM busha_events WHERE id = $1`, eventRowID).Scan(&name, &payload, &processed); err != nil {
		return err
	}
	if processed != nil {
		return nil
	}
	var ev busha.Event
	if err := json.Unmarshal(payload, &ev); err != nil {
		return s.markEvent(ctx, eventRowID, err)
	}
	var d struct {
		busha.EventData
		TargetAmount string `json:"target_amount"`
	}
	_ = json.Unmarshal(ev.Data, &d)

	var err error
	switch name {
	case "payment_request.completed", "deposit.success":
		err = s.MarkFunded(ctx, d.Reference, d.TargetAmount, name)
	case "transfer.funds_delivered", "transfer.completed":
		err = s.CompletePayoutByTransfer(ctx, d.ID)
	case "transfer.failed", "transfer.funds_not_delivered", "transfer.funds_refunded", "transfer.cancelled":
		err = s.FailPayoutByTransfer(ctx, d.ID, name)
	default:
		s.Log.Info("ignoring busha event", "event", name)
	}
	if err != nil {
		if ae, ok := AsAppError(err); ok && ae.HTTP == 404 {
			return s.markEvent(ctx, eventRowID, err) // permanent: don't retry
		}
		return err // transient: job retries
	}
	return s.markEvent(ctx, eventRowID, nil)
}

func (s *Service) markEvent(ctx context.Context, id int64, cause error) error {
	var msg *string
	if cause != nil {
		m := cause.Error()
		msg = &m
	}
	_, err := s.DB.Exec(ctx, `UPDATE busha_events SET processed_at = now(), error = $2 WHERE id = $1`, id, msg)
	return err
}

// IngestEvent stores a verified webhook and queues it. Duplicates are ignored.
func (s *Service) IngestEvent(ctx context.Context, raw []byte) (duplicate bool, err error) {
	var ev busha.Event
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Event == "" {
		return false, badRequest("invalid_payload", "webhook body is not a Busha event")
	}
	var d busha.EventData
	_ = json.Unmarshal(ev.Data, &d)
	ref := d.ID
	if ref == "" {
		ref = d.Reference
	}
	if ref == "" {
		return false, badRequest("invalid_payload", "event has no id or reference")
	}
	err = store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO busha_events (event, reference, payload) VALUES ($1,$2,$3)
			ON CONFLICT (event, reference) DO NOTHING RETURNING id`, ev.Event, ref, raw).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			duplicate = true
			_, err := tx.Exec(ctx, `UPDATE busha_events SET duplicates = duplicates + 1, last_duplicate_at = now()
				WHERE event = $1 AND reference = $2`, ev.Event, ref)
			return err
		}
		if err != nil {
			return err
		}
		return store.Enqueue(ctx, tx, JobBushaEvent, map[string]int64{"event_id": id}, "", nil)
	})
	return duplicate, err
}

// EventRow is one stored Busha webhook, for the admin view.
type EventRow struct {
	ID              int64      `json:"id"`
	Event           string     `json:"event"`
	Reference       string     `json:"reference"`
	ReceivedAt      time.Time  `json:"received_at"`
	ProcessedAt     *time.Time `json:"processed_at"`
	Error           *string    `json:"error"`
	Duplicates      int        `json:"duplicates"`
	LastDuplicateAt *time.Time `json:"last_duplicate_at"`
}

// RecentEvents lists the latest stored webhooks, newest first.
func (s *Service) RecentEvents(ctx context.Context, limit int) ([]EventRow, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, event, reference, received_at, processed_at, error, duplicates, last_duplicate_at
		FROM busha_events ORDER BY received_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventRow{}
	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.ID, &e.Event, &e.Reference, &e.ReceivedAt, &e.ProcessedAt, &e.Error, &e.Duplicates, &e.LastDuplicateAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ensureRecipient registers the worker's saved bank account with Busha the
// first time they cash out. Workers created before live mode (like the seeded
// demo worker) have bank details but no Busha recipient yet.
func (s *Service) ensureRecipient(ctx context.Context, w *Worker) (string, error) {
	if w.BankCode == nil || *w.BankCode == "" || w.AccountNumber == nil || *w.AccountNumber == "" {
		return "", conflict("no_bank_account", "add a bank account before cashing out")
	}
	in := busha.RecipientInput{Type: "ngn_bank", Currency: "NGN", CountryCode: "NG",
		AccountNumber: *w.AccountNumber, BankCode: *w.BankCode, AccountName: w.Name}
	if w.AccountName != nil && *w.AccountName != "" {
		in.AccountName = *w.AccountName
	}
	if w.BankName != nil {
		in.BankName = *w.BankName
	}
	if w.Email != nil {
		in.Email = *w.Email
	}
	rcp, err := s.Busha.CreateRecipient(ctx, in)
	if err != nil {
		return "", upstream("could not register your bank account with Busha", err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE workers SET busha_recipient_id = $2 WHERE id = $1`, w.ID, rcp.ID); err != nil {
		return "", err
	}
	w.BushaRecipientID = &rcp.ID
	return rcp.ID, nil
}
