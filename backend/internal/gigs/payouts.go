package gigs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
		return nil, conflict("no_bank_account", "add a bank account (Busha recipient) before cashing out")
	}

	payoutID := store.NewID("PAY")
	quoteInput := busha.QuoteInput{SourceCurrency: "USDT", TargetCurrency: "NGN", TargetAmount: in.AmountNGN,
		Reference: payoutID, PayOut: &busha.PayOut{Type: "bank_transfer", RecipientID: recipient}}
	q, err := s.Busha.CreateQuote(ctx, quoteInput)
	if err != nil {
		return nil, upstream("could not get a cash-out quote from Busha", err)
	}
	usdt, err := ParseDecimal(q.SourceAmount, 6)
	if err != nil || usdt <= 0 {
		return nil, upstream("Busha quote had no source amount", err)
	}

	p := &Payout{ID: payoutID, AmountUSDTMicro: usdt, AmountNGNKobo: kobo, BushaQuoteID: &q.ID, Status: "processing"}
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

	t, terr := s.Busha.CreateTransfer(ctx, q.ID, p.ID)
	if isExpiredQuote(terr) {
		q, terr = s.Busha.CreateQuote(ctx, quoteInput)
		if terr == nil {
			newUSDT, parseErr := ParseDecimal(q.SourceAmount, 6)
			if parseErr != nil || newUSDT <= 0 {
				terr = errors.New("Busha re-quote had no source amount")
			} else if err := s.adjustPayoutReservation(ctx, p, newUSDT); err != nil {
				terr = err
			} else {
				p.AmountUSDTMicro, p.BushaQuoteID = newUSDT, &q.ID
				_, terr = s.Busha.CreateTransfer(ctx, q.ID, p.ID)
			}
		}
	}
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

func isExpiredQuote(err error) bool {
	if err == nil {
		return false
	}
	var be *busha.Error
	if !errors.As(err, &be) {
		return false
	}
	body := strings.ToLower(be.Body)
	return strings.Contains(body, "quote") && strings.Contains(body, "expir")
}

func (s *Service) adjustPayoutReservation(ctx context.Context, p *Payout, newUSDT int64) error {
	delta := newUSDT - p.AmountUSDTMicro
	if delta == 0 {
		return nil
	}
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		from, to := ledger.Worker(""), ledger.ExternalPayout
		if delta < 0 {
			from, to, delta = ledger.ExternalPayout, ledger.Worker(""), -delta
		}
		// The worker account is filled by the payout row lookup inside the transaction.
		var workerID string
		if err := tx.QueryRow(ctx, `SELECT worker_id FROM payouts WHERE id = $1 FOR UPDATE`, p.ID).Scan(&workerID); err != nil {
			return err
		}
		if from == ledger.Worker("") {
			from = ledger.Worker(workerID)
		} else {
			to = ledger.Worker(workerID)
		}
		_, err := ledger.Move(ctx, tx, "payout_quote_adjustment:"+p.ID, "payout_quote_adjustment", "", from, to, delta)
		return err
	})
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
	var d busha.EventData
	_ = json.Unmarshal(ev.Data, &d)

	var err error
	switch name {
	case "payment_request.completed", "deposit.success", "transfer.funds_received", "transfer.funds_converted", "ramp.transfer.completed":
		ref := d.Reference
		if ref == "" {
			ref = d.ID
		}
		err = s.MarkFunded(ctx, ref, d.TargetAmount, name)
		if ae, ok := AsAppError(err); ok && ae.HTTP == 404 && d.ID != "" && d.ID != ref {
			err = s.MarkFunded(ctx, d.ID, d.TargetAmount, name)
		}
		if ae, ok := AsAppError(err); ok && ae.HTTP == 404 && d.QuoteID != "" {
			err = s.MarkFunded(ctx, d.QuoteID, d.TargetAmount, name)
		}
	case "transfer.funds_delivered", "transfer.completed":
		// A completed transfer can be either a client deposit (quote + transfer) or a creator cash-out.
		var gigID string
		_ = s.DB.QueryRow(ctx, `SELECT id FROM gigs WHERE busha_payment_request_id = $1 OR payment_reference = $2 OR busha_quote_id = $3 LIMIT 1`,
			d.ID, d.Reference, d.QuoteID).Scan(&gigID)
		if gigID != "" {
			err = s.MarkFunded(ctx, gigID, d.TargetAmount, name)
		} else {
			err = s.CompletePayoutByTransfer(ctx, d.ID)
		}
	case "transfer.failed", "transfer.funds_not_delivered", "transfer.funds_refunded", "transfer.cancelled":
		err = s.FailPayoutByTransfer(ctx, d.ID, name)
	case "customer.verification.active":
		custID := d.ID
		if custID == "" {
			custID = d.CustomerID
		}
		err = s.UpdateWorkerKYC(ctx, custID, "verified", name)
	case "customer.verification.rejected":
		custID := d.ID
		if custID == "" {
			custID = d.CustomerID
		}
		err = s.UpdateWorkerKYC(ctx, custID, "rejected", name)
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

// UpdateWorkerKYC updates workers.kyc_status when Busha sends a customer verification webhook.
func (s *Service) UpdateWorkerKYC(ctx context.Context, bushaCustomerID, kycStatus, event string) error {
	if bushaCustomerID == "" {
		return nil
	}
	return store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		var workerID, prevStatus string
		err := tx.QueryRow(ctx, `SELECT id, kyc_status FROM workers WHERE busha_customer_id = $1 FOR UPDATE`, bushaCustomerID).Scan(&workerID, &prevStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if prevStatus == kycStatus {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE workers SET kyc_status = $2 WHERE id = $1`, workerID, kycStatus); err != nil {
			return err
		}
		action := "worker.kyc_" + kycStatus
		return store.Audit(ctx, tx, "", workerID, "busha", action, prevStatus, kycStatus,
			map[string]any{"busha_customer_id": bushaCustomerID, "event": event})
	})
}

// PollStale checks Busha directly for any payment request or transfer that has been
// stuck in a pending/processing state for more than 2 minutes (missed-webhook fallback).
func (s *Service) PollStale(ctx context.Context) error {
	// 1. Check gigs awaiting payment for > 2 minutes.
	gigRows, err := s.DB.Query(ctx, `SELECT id, busha_payment_request_id FROM gigs
		WHERE status = $1 AND busha_payment_request_id IS NOT NULL
		  AND updated_at < now() - interval '2 minutes'
		ORDER BY updated_at ASC LIMIT 50`, StatusAwaitingPayment)
	if err != nil {
		return err
	}
	type staleGig struct {
		gigID string
		reqID string
	}
	var staleGigs []staleGig
	for gigRows.Next() {
		var sg staleGig
		if err := gigRows.Scan(&sg.gigID, &sg.reqID); err != nil {
			gigRows.Close()
			return err
		}
		staleGigs = append(staleGigs, sg)
	}
	gigRows.Close()

	for _, sg := range staleGigs {
		if strings.HasPrefix(sg.reqID, "PAYR") {
			pr, err := s.Busha.GetPaymentRequest(ctx, sg.reqID)
			if err != nil {
				s.Log.Warn("poll stale payment request failed", "gig_id", sg.gigID, "id", sg.reqID, "err", err)
				continue
			}
			st := strings.ToLower(pr.Status)
			if st == "completed" || st == "funds_received" || st == "success" {
				_ = s.MarkFunded(ctx, sg.gigID, pr.TargetAmount, "poll.payment_request."+st)
			}
		} else {
			trf, err := s.Busha.GetTransfer(ctx, sg.reqID)
			if err != nil {
				s.Log.Warn("poll stale deposit transfer failed", "gig_id", sg.gigID, "id", sg.reqID, "err", err)
				continue
			}
			st := strings.ToLower(trf.Status)
			if st == "completed" || st == "funds_received" || st == "funds_converted" || st == "funds_delivered" {
				_ = s.MarkFunded(ctx, sg.gigID, trf.TargetAmount, "poll.transfer."+st)
			}
		}
	}

	// 2. Check payouts stuck in processing for > 2 minutes.
	payoutRows, err := s.DB.Query(ctx, `SELECT id, busha_transfer_id FROM payouts
		WHERE status = 'processing' AND busha_transfer_id IS NOT NULL
		  AND updated_at < now() - interval '2 minutes'
		ORDER BY updated_at ASC LIMIT 50`)
	if err != nil {
		return err
	}
	type stalePayout struct {
		payoutID   string
		transferID string
	}
	var stalePayouts []stalePayout
	for payoutRows.Next() {
		var sp stalePayout
		if err := payoutRows.Scan(&sp.payoutID, &sp.transferID); err != nil {
			payoutRows.Close()
			return err
		}
		stalePayouts = append(stalePayouts, sp)
	}
	payoutRows.Close()

	for _, sp := range stalePayouts {
		trf, err := s.Busha.GetTransfer(ctx, sp.transferID)
		if err != nil {
			s.Log.Warn("poll stale payout transfer failed", "payout_id", sp.payoutID, "transfer_id", sp.transferID, "err", err)
			continue
		}
		st := strings.ToLower(trf.Status)
		switch st {
		case "completed", "funds_delivered":
			_ = s.CompletePayoutByTransfer(ctx, sp.transferID)
		case "failed", "cancelled", "funds_not_delivered", "funds_refunded":
			_ = s.FailPayoutByTransfer(ctx, sp.transferID, "poll.transfer."+st)
		}
	}
	return nil
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
		ref = d.CustomerID
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
