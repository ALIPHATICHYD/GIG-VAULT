package gigs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

// Gig statuses. Allowed moves:
//
//	AWAITING_PAYMENT -> BOOKED (payment confirmed by Busha webhook; deposit released)
//	AWAITING_PAYMENT -> CANCELLED
//	BOOKED           -> DONE_PENDING (client or organiser confirms the gig is done)
//	BOOKED           -> CANCELLED (remaining escrow refunded; deposit kept on late cancel)
//	DONE_PENDING     -> RELEASED (complaint window ends, worker job auto-releases)
//	DONE_PENDING     -> DISPUTED (complaint inside the window freezes part)
//	DISPUTED         -> RELEASED | PARTIALLY_REFUNDED (complaint resolved)
const (
	StatusAwaitingPayment   = "AWAITING_PAYMENT"
	StatusBooked            = "BOOKED"
	StatusDonePending       = "DONE_PENDING"
	StatusDisputed          = "DISPUTED"
	StatusReleased          = "RELEASED"
	StatusPartiallyRefunded = "PARTIALLY_REFUNDED"
	StatusCancelled         = "CANCELLED"
)

type Gig struct {
	ID                     string          `json:"id"`
	WorkerID               string          `json:"worker_id"`
	Title                  string          `json:"title"`
	ClientName             string          `json:"client_name"`
	ClientEmail            string          `json:"client_email"`
	EventAt                time.Time       `json:"event_at"`
	AmountNGNKobo          int64           `json:"amount_ngn_kobo"`
	AmountUSDTMicro        *int64          `json:"amount_usdt_micro"`
	RateNGNPerUSDT         *float64        `json:"rate_ngn_per_usdt"`
	DepositBps             int             `json:"deposit_bps"`
	FreezeBps              int             `json:"freeze_bps"`
	ComplaintWindowSeconds int             `json:"complaint_window_seconds"`
	Status                 string          `json:"status"`
	PayInMethod            string          `json:"pay_in_method"`
	BushaPaymentRequestID  *string         `json:"busha_payment_request_id"`
	PaymentReference       *string         `json:"payment_reference"`
	PaymentDetails         json.RawMessage `json:"payment_details"`
	DoneAt                 *time.Time      `json:"done_at"`
	ReleaseAt              *time.Time      `json:"release_at"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

const gigCols = `id, worker_id, title, client_name, client_email, event_at, amount_ngn_kobo, amount_usdt_micro,
	rate_ngn_per_usdt::float8, deposit_bps, freeze_bps, complaint_window_seconds, status, pay_in_method,
	busha_payment_request_id, payment_reference, payment_details, done_at, release_at, created_at, updated_at`

func scanGig(row pgx.Row) (*Gig, error) {
	var g Gig
	err := row.Scan(&g.ID, &g.WorkerID, &g.Title, &g.ClientName, &g.ClientEmail, &g.EventAt, &g.AmountNGNKobo,
		&g.AmountUSDTMicro, &g.RateNGNPerUSDT, &g.DepositBps, &g.FreezeBps, &g.ComplaintWindowSeconds, &g.Status,
		&g.PayInMethod, &g.BushaPaymentRequestID, &g.PaymentReference, &g.PaymentDetails, &g.DoneAt, &g.ReleaseAt,
		&g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("gig")
	}
	return &g, err
}

func getGig(ctx context.Context, db store.DBTX, id string, forUpdate bool) (*Gig, error) {
	q := `SELECT ` + gigCols + ` FROM gigs WHERE id = $1`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	return scanGig(db.QueryRow(ctx, q, id))
}

func setStatus(ctx context.Context, db store.DBTX, g *Gig, to, actor, action string, details any) error {
	if _, err := db.Exec(ctx, `UPDATE gigs SET status = $2, version = version + 1, updated_at = now() WHERE id = $1`, g.ID, to); err != nil {
		return err
	}
	if err := store.Audit(ctx, db, g.ID, g.WorkerID, actor, action, g.Status, to, details); err != nil {
		return err
	}
	g.Status = to
	return nil
}

type Worker struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Email            *string   `json:"email"`
	Phone            *string   `json:"phone"`
	BushaCustomerID  *string   `json:"busha_customer_id"`
	BushaRecipientID *string   `json:"busha_recipient_id"`
	BankName         *string   `json:"bank_name"`
	AccountNumber    *string   `json:"account_number"`
	AccountName      *string   `json:"account_name"`
	CreatedAt        time.Time `json:"created_at"`
}

func getWorker(ctx context.Context, db store.DBTX, id string) (*Worker, error) {
	var w Worker
	err := db.QueryRow(ctx, `SELECT id, name, email, phone, busha_customer_id, busha_recipient_id, bank_name, account_number, account_name, created_at
		FROM workers WHERE id = $1`, id).Scan(&w.ID, &w.Name, &w.Email, &w.Phone, &w.BushaCustomerID, &w.BushaRecipientID,
		&w.BankName, &w.AccountNumber, &w.AccountName, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("worker")
	}
	return &w, err
}
