package ledger_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliphatichyd/gig-vault/backend/internal/ledger"
	"github.com/aliphatichyd/gig-vault/backend/internal/store"
	"github.com/aliphatichyd/gig-vault/backend/internal/store/storetest"
)

// The ledger holds real money, so these tests assert the four properties the
// rest of GigVault takes for granted: every transaction balances, a replayed
// webhook can't post twice, an internal account can't be overdrawn, and history
// can't be rewritten.

const gigID = "GIG_test"

// setup returns a migrated pool with one worker and one gig, which ledger
// transactions reference by foreign key.
func setup(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	pool := storetest.New(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `INSERT INTO workers (id, name) VALUES ('WRK_test', 'Test Creator')`); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO gigs (id, worker_id, title, event_at, amount_ngn_kobo,
		deposit_bps, freeze_bps, complaint_window_seconds, status, payment_reference)
		VALUES ($1, 'WRK_test', 'Test invoice', now(), 15000000, 2000, 5000, 60, 'AWAITING_PAYMENT', $1)`, gigID); err != nil {
		t.Fatalf("seed gig: %v", err)
	}
	return pool, ctx
}

// fund moves amount from the client into this gig's escrow.
func fund(t *testing.T, pool *pgxpool.Pool, ctx context.Context, key string, amount int64) {
	t.Helper()
	applied, err := ledger.Move(ctx, pool, key, "fund", gigID, ledger.ExternalClient, ledger.Escrow(gigID), amount)
	if err != nil {
		t.Fatalf("fund: %v", err)
	}
	if !applied {
		t.Fatalf("fund with key %q was not applied", key)
	}
}

func countEntries(t *testing.T, pool *pgxpool.Pool, ctx context.Context) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries`).Scan(&n); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	return n
}

// ---------- every transaction balances ----------

func TestEveryTransactionSumsToZero(t *testing.T) {
	pool, ctx := setup(t)

	// A full invoice lifecycle: funded, upfront released, remainder frozen then
	// split between the creator and a refund.
	fund(t, pool, ctx, "fund:"+gigID, 75_000_000)
	moves := []struct {
		key, kind, from, to string
		amount              int64
	}{
		{"deposit:" + gigID, "deposit_release", ledger.Escrow(gigID), ledger.Worker("WRK_test"), 15_000_000},
		{"freeze:" + gigID, "freeze", ledger.Escrow(gigID), ledger.Frozen(gigID), 30_000_000},
		{"release:" + gigID, "release", ledger.Escrow(gigID), ledger.Worker("WRK_test"), 30_000_000},
		{"unfreeze:" + gigID, "unfreeze", ledger.Frozen(gigID), ledger.Worker("WRK_test"), 21_000_000},
		{"refund:" + gigID, "refund", ledger.Frozen(gigID), ledger.ExternalRefund, 9_000_000},
	}
	for _, m := range moves {
		if _, err := ledger.Move(ctx, pool, m.key, m.kind, gigID, m.from, m.to, m.amount); err != nil {
			t.Fatalf("move %s: %v", m.key, err)
		}
	}

	// No transaction may have a non-zero sum.
	rows, err := pool.Query(ctx, `SELECT txn_id, SUM(CASE direction WHEN 'credit' THEN amount_minor ELSE -amount_minor END)
		FROM ledger_entries GROUP BY txn_id HAVING SUM(CASE direction WHEN 'credit' THEN amount_minor ELSE -amount_minor END) <> 0`)
	if err != nil {
		t.Fatalf("query unbalanced: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var txn string
		var sum int64
		if err := rows.Scan(&txn, &sum); err != nil {
			t.Fatalf("scan: %v", err)
		}
		t.Errorf("transaction %s does not balance: sum %d", txn, sum)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// Each move writes exactly one debit and one credit.
	var entries int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries`).Scan(&entries); err != nil {
		t.Fatalf("count: %v", err)
	}
	if want := 2 * (len(moves) + 1); entries != want {
		t.Errorf("got %d entries, want %d (two per transaction)", entries, want)
	}

	// And the ledger as a whole nets to zero across every account.
	var total int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(CASE direction WHEN 'credit' THEN amount_minor ELSE -amount_minor END), 0)
		FROM ledger_entries`).Scan(&total); err != nil {
		t.Fatalf("total: %v", err)
	}
	if total != 0 {
		t.Errorf("ledger total is %d, want 0", total)
	}

	// The money ended where we sent it.
	assertBalance(t, pool, ctx, ledger.Worker("WRK_test"), 66_000_000)
	assertBalance(t, pool, ctx, ledger.Escrow(gigID), 0)
	assertBalance(t, pool, ctx, ledger.Frozen(gigID), 0)
	assertBalance(t, pool, ctx, ledger.ExternalRefund, 9_000_000)
	assertBalance(t, pool, ctx, ledger.ExternalClient, -75_000_000)
}

// ---------- idempotency ----------

func TestSameIdempotencyKeyPostsOnce(t *testing.T) {
	pool, ctx := setup(t)

	applied, err := ledger.Move(ctx, pool, "fund:"+gigID, "fund", gigID, ledger.ExternalClient, ledger.Escrow(gigID), 50_000_000)
	if err != nil || !applied {
		t.Fatalf("first move: applied=%v err=%v, want applied=true", applied, err)
	}

	// A redelivered Busha webhook replays the same key.
	applied, err = ledger.Move(ctx, pool, "fund:"+gigID, "fund", gigID, ledger.ExternalClient, ledger.Escrow(gigID), 50_000_000)
	if err != nil {
		t.Fatalf("replay returned an error: %v", err)
	}
	if applied {
		t.Error("replay reported applied=true; it must report false so callers know nothing moved")
	}

	var txns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_txns`).Scan(&txns); err != nil {
		t.Fatalf("count txns: %v", err)
	}
	if txns != 1 {
		t.Errorf("got %d transactions, want 1", txns)
	}
	if n := countEntries(t, pool, ctx); n != 2 {
		t.Errorf("got %d entries, want 2", n)
	}
	assertBalance(t, pool, ctx, ledger.Escrow(gigID), 50_000_000)
}

func TestZeroAndNegativeAmountsDoNothing(t *testing.T) {
	pool, ctx := setup(t)

	for _, amount := range []int64{0, -1} {
		applied, err := ledger.Move(ctx, pool, "noop", "fund", gigID, ledger.ExternalClient, ledger.Escrow(gigID), amount)
		if err != nil {
			t.Errorf("amount %d returned an error: %v", amount, err)
		}
		if applied {
			t.Errorf("amount %d reported applied=true", amount)
		}
	}
	if n := countEntries(t, pool, ctx); n != 0 {
		t.Errorf("got %d entries, want 0: a zero move must not post", n)
	}
}

// ---------- overdraw protection ----------

func TestInternalAccountCannotGoNegative(t *testing.T) {
	pool, ctx := setup(t)
	fund(t, pool, ctx, "fund:"+gigID, 10_000_000)

	_, err := ledger.Move(ctx, pool, "overdraw", "release", gigID,
		ledger.Escrow(gigID), ledger.Worker("WRK_test"), 10_000_001)
	if !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("got %v, want ErrInsufficientFunds", err)
	}

	// The rejected move must leave no trace.
	if n := countEntries(t, pool, ctx); n != 2 {
		t.Errorf("got %d entries, want 2: the rejected move must not post", n)
	}
	assertBalance(t, pool, ctx, ledger.Escrow(gigID), 10_000_000)

	// Spending the balance exactly is allowed.
	applied, err := ledger.Move(ctx, pool, "exact", "release", gigID,
		ledger.Escrow(gigID), ledger.Worker("WRK_test"), 10_000_000)
	if err != nil || !applied {
		t.Fatalf("spending the full balance: applied=%v err=%v, want applied=true", applied, err)
	}
	assertBalance(t, pool, ctx, ledger.Escrow(gigID), 0)
}

// External accounts are the boundary with Busha: money arriving from a client
// legitimately drives external:client negative, which is how we know how much
// has entered the system. Only internal accounts are balance-checked.
func TestExternalAccountsMayGoNegative(t *testing.T) {
	pool, ctx := setup(t)

	applied, err := ledger.Move(ctx, pool, "fund:"+gigID, "fund", gigID,
		ledger.ExternalClient, ledger.Escrow(gigID), 40_000_000)
	if err != nil || !applied {
		t.Fatalf("fund from empty external account: applied=%v err=%v", applied, err)
	}
	assertBalance(t, pool, ctx, ledger.ExternalClient, -40_000_000)
}

// ---------- append-only ----------

func TestLedgerEntriesAreAppendOnly(t *testing.T) {
	pool, ctx := setup(t)
	fund(t, pool, ctx, "fund:"+gigID, 25_000_000)

	t.Run("update is rejected", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE ledger_entries SET amount_minor = 1`)
		assertAppendOnly(t, err)
	})

	t.Run("delete is rejected", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM ledger_entries`)
		assertAppendOnly(t, err)
	})

	// Both attempts failed, so the history is intact.
	if n := countEntries(t, pool, ctx); n != 2 {
		t.Errorf("got %d entries, want 2", n)
	}
	assertBalance(t, pool, ctx, ledger.Escrow(gigID), 25_000_000)
}

// ---------- reads ----------

func TestBalanceSumPrefixAndEntriesForGig(t *testing.T) {
	pool, ctx := setup(t)

	// A second gig, so SumPrefix has more than one escrow account to add up.
	const otherGig = "GIG_other"
	if _, err := pool.Exec(ctx, `INSERT INTO gigs (id, worker_id, title, event_at, amount_ngn_kobo,
		deposit_bps, freeze_bps, complaint_window_seconds, status, payment_reference)
		VALUES ($1, 'WRK_test', 'Second invoice', now(), 5000000, 2000, 5000, 60, 'AWAITING_PAYMENT', $1)`, otherGig); err != nil {
		t.Fatalf("seed second gig: %v", err)
	}

	fund(t, pool, ctx, "fund:"+gigID, 30_000_000)
	if _, err := ledger.Move(ctx, pool, "fund:"+otherGig, "fund", otherGig,
		ledger.ExternalClient, ledger.Escrow(otherGig), 20_000_000); err != nil {
		t.Fatalf("fund second gig: %v", err)
	}

	got, err := ledger.SumPrefix(ctx, pool, "escrow:")
	if err != nil {
		t.Fatalf("SumPrefix: %v", err)
	}
	if got != 50_000_000 {
		t.Errorf("SumPrefix(escrow:) = %d, want 50000000", got)
	}

	// EntriesForGig is scoped to its own invoice.
	entries, err := ledger.EntriesForGig(ctx, pool, gigID)
	if err != nil {
		t.Fatalf("EntriesForGig: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries for %s, want 2", len(entries), gigID)
	}
	for _, e := range entries {
		if e.Kind != "fund" {
			t.Errorf("entry kind = %q, want fund", e.Kind)
		}
		if e.Amount != 30_000_000 {
			t.Errorf("entry amount = %d, want 30000000", e.Amount)
		}
	}
}

// Move must honour the caller's transaction: if the surrounding transaction
// rolls back, nothing is posted. This is what lets MarkFunded move money and
// change a gig's status atomically.
func TestMoveRollsBackWithItsTransaction(t *testing.T) {
	pool, ctx := setup(t)
	fund(t, pool, ctx, "fund:"+gigID, 20_000_000)

	wantErr := errors.New("caller changed its mind")
	err := store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := ledger.Move(ctx, tx, "doomed", "release", gigID,
			ledger.Escrow(gigID), ledger.Worker("WRK_test"), 20_000_000); err != nil {
			return err
		}
		// Something later in the same transaction fails.
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want the caller's error", err)
	}

	// The move was rolled back with everything else.
	assertBalance(t, pool, ctx, ledger.Escrow(gigID), 20_000_000)
	assertBalance(t, pool, ctx, ledger.Worker("WRK_test"), 0)
	if n := countEntries(t, pool, ctx); n != 2 {
		t.Errorf("got %d entries, want 2: the rolled-back move must leave nothing", n)
	}

	// And the idempotency key was released, so a retry can post.
	applied, err := ledger.Move(ctx, pool, "doomed", "release", gigID,
		ledger.Escrow(gigID), ledger.Worker("WRK_test"), 20_000_000)
	if err != nil || !applied {
		t.Fatalf("retry after rollback: applied=%v err=%v, want applied=true", applied, err)
	}
	assertBalance(t, pool, ctx, ledger.Worker("WRK_test"), 20_000_000)
}

// ---------- helpers ----------

func assertBalance(t *testing.T, pool *pgxpool.Pool, ctx context.Context, account string, want int64) {
	t.Helper()
	got, err := ledger.Balance(ctx, pool, account)
	if err != nil {
		t.Fatalf("Balance(%s): %v", account, err)
	}
	if got != want {
		t.Errorf("Balance(%s) = %d, want %d", account, got, want)
	}
}

func assertAppendOnly(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("statement succeeded; the append-only trigger should have rejected it")
	}
	if !strings.Contains(err.Error(), "ledger is append-only") {
		t.Fatalf("got %v, want the append-only trigger's error", err)
	}
}
