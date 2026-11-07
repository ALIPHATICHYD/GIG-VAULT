// Package ledger is an append-only double-entry ledger.
//
// Every movement debits one account and credits another for the same amount,
// so the sum over all accounts is always zero. Balances are derived, never stored.
package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

const USDT = "USDT"

var ErrInsufficientFunds = errors.New("insufficient funds")

func Escrow(gigID string) string    { return "escrow:" + gigID }
func Frozen(gigID string) string    { return "frozen:" + gigID }
func Worker(workerID string) string { return "worker:" + workerID }

const (
	ExternalClient = "external:client" // money arriving from the client via Busha
	ExternalPayout = "external:payout" // money leaving to a worker's bank via Busha
	ExternalRefund = "external:refund" // money returned to a client
)

// Move transfers amount from one account to another. It is idempotent on key:
// calling it twice with the same key posts once and returns applied=false.
// Internal (non-external) source accounts may not go negative.
func Move(ctx context.Context, db store.DBTX, key, kind, gigID, from, to string, amount int64) (applied bool, err error) {
	if amount <= 0 {
		return false, nil
	}
	if !isExternal(from) {
		bal, err := Balance(ctx, db, from)
		if err != nil {
			return false, err
		}
		if bal < amount {
			return false, fmt.Errorf("%w: %s has %d, needs %d", ErrInsufficientFunds, from, bal, amount)
		}
	}
	txnID := store.NewID("TXN")
	tag, err := db.Exec(ctx, `INSERT INTO ledger_txns (id, idempotency_key, kind, gig_id)
		VALUES ($1, $2, $3, NULLIF($4,'')) ON CONFLICT (idempotency_key) DO NOTHING`, txnID, key, kind, gigID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil // already posted
	}
	_, err = db.Exec(ctx, `INSERT INTO ledger_entries (txn_id, account, direction, amount_minor, currency) VALUES
		($1, $2, 'debit', $4, $5), ($1, $3, 'credit', $4, $5)`, txnID, from, to, amount, USDT)
	return err == nil, err
}

// Balance returns credits minus debits for an account, in USDT micro-units.
func Balance(ctx context.Context, db store.DBTX, account string) (int64, error) {
	var b int64
	err := db.QueryRow(ctx, `SELECT COALESCE(SUM(CASE direction WHEN 'credit' THEN amount_minor ELSE -amount_minor END), 0)
		FROM ledger_entries WHERE account = $1`, account).Scan(&b)
	return b, err
}

// SumPrefix returns the total balance of all accounts starting with prefix (e.g. "escrow:").
func SumPrefix(ctx context.Context, db store.DBTX, prefix string) (int64, error) {
	var b int64
	err := db.QueryRow(ctx, `SELECT COALESCE(SUM(CASE direction WHEN 'credit' THEN amount_minor ELSE -amount_minor END), 0)
		FROM ledger_entries WHERE account LIKE $1 || '%'`, prefix).Scan(&b)
	return b, err
}

type Entry struct {
	TxnID     string `json:"txn_id"`
	Kind      string `json:"kind"`
	Account   string `json:"account"`
	Direction string `json:"direction"`
	Amount    int64  `json:"amount_usdt_micro"`
	CreatedAt string `json:"created_at"`
}

func EntriesForGig(ctx context.Context, db store.DBTX, gigID string) ([]Entry, error) {
	rows, err := db.Query(ctx, `SELECT e.txn_id, t.kind, e.account, e.direction, e.amount_minor, to_char(e.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.MSOF')
		FROM ledger_entries e JOIN ledger_txns t ON t.id = e.txn_id WHERE t.gig_id = $1 ORDER BY e.id`, gigID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.TxnID, &e.Kind, &e.Account, &e.Direction, &e.Amount, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func isExternal(account string) bool { return len(account) > 9 && account[:9] == "external:" }
