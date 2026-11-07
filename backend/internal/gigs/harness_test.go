package gigs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/config"
	"github.com/aliphatichyd/gig-vault/backend/internal/ledger"
	"github.com/aliphatichyd/gig-vault/backend/internal/store/storetest"
)

// Shared setup for the service-level tests. Everything runs against a real
// Postgres (see storetest) and the Busha mock, so no network is touched but the
// escrow rules, the ledger and the state machine are all the real ones.

const mockRateNGN = 1600

// harness is one test's service plus the ids it needs.
type harness struct {
	svc           *Service
	pool          *pgxpool.Pool
	ctx           context.Context
	workerID      string
	clientID      string
	creatorUserID string
}

// newHarness returns a service wired to a private database schema. The windows
// are short so time-dependent paths can be driven without sleeping: the
// complaint window stays open long enough to raise a dispute, while the dispute
// window is already over so escalation can run immediately.
func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := storetest.New(t)
	ctx := context.Background()

	cfg := config.Config{
		DepositBps:       2000,
		MaxDepositBps:    7000,
		DisputeWindow:    0,
		DisputeFeeBps:    1000,
		FreezeBps:        5000,
		ComplaintWindow:  5 * time.Minute,
		LateCancelWindow: 24 * time.Hour,
		InvoiceExpiry:    168 * time.Hour,
		MockRateNGN:      mockRateNGN,
	}
	svc := &Service{
		DB:    pool,
		Busha: busha.NewMock(mockRateNGN),
		Cfg:   cfg,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	h := &harness{svc: svc, pool: pool, ctx: ctx,
		workerID: "WRK_chidinma", clientID: "USR_client", creatorUserID: "USR_creator"}
	if _, err := pool.Exec(ctx, `INSERT INTO workers (id, name, email, kyc_status)
		VALUES ($1, 'Chidinma Okafor', 'chidinma@test.local', 'verified')`, h.workerID); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name, role, password_hash, worker_id) VALUES
		($1, 'chidinma@test.local', 'Chidinma Okafor', 'creator', 'x', $2),
		($3, 'tolu@test.local',     'Tolu Adeyemi',    'client',  'x', NULL)`,
		h.creatorUserID, h.workerID, h.clientID); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	return h
}

// sendInvoice creates an invoice for amountNGN naira with the given upfront
// percentage, as the creator would.
func (h *harness) sendInvoice(t *testing.T, amountNGN string, upfrontPercent int) *Gig {
	t.Helper()
	link, err := h.svc.CreateGigLink(h.ctx, h.workerID, CreateGigLinkInput{
		Title:          "Instagram campaign: 3 reels",
		EventAt:        time.Now().Add(14 * 24 * time.Hour),
		AmountNGN:      amountNGN,
		ClientName:     "Tolu Adeyemi",
		ClientEmail:    "tolu@test.local",
		UpfrontPercent: &upfrontPercent,
	})
	if err != nil {
		t.Fatalf("send invoice: %v", err)
	}
	return link.Gig
}

// claim attaches the invoice to the seeded client account, the way opening the
// pay link and signing up does.
func (h *harness) claim(t *testing.T, gigID string) {
	t.Helper()
	if _, err := h.pool.Exec(h.ctx, `UPDATE gigs SET client_user_id = $2 WHERE id = $1`, gigID, h.clientID); err != nil {
		t.Fatalf("claim: %v", err)
	}
}

// pay runs the funding path a Busha payment_request.completed webhook triggers.
// The reference is the gig id, which is what CreateGigLink stores.
func (h *harness) pay(t *testing.T, g *Gig) {
	t.Helper()
	if err := h.payErr(g); err != nil {
		t.Fatalf("mark funded: %v", err)
	}
}

// payErr is pay without the assertion, for tests that replay the webhook.
func (h *harness) payErr(g *Gig) error {
	return h.svc.MarkFunded(h.ctx, g.ID, FormatMinor(usdtFor(g.AmountNGNKobo), 6), "payment_request.completed")
}

// escalatedDispute drives an invoice all the way to an unresolved dispute
// awaiting GigVault's decision, with a proposal from each side.
func (h *harness) escalatedDispute(t *testing.T, amountNGN string) *Gig {
	t.Helper()
	g := h.sendInvoice(t, amountNGN, 20)
	h.claim(t, g.ID)
	h.pay(t, g)
	if _, err := h.svc.Complete(h.ctx, g.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := h.svc.Complain(h.ctx, g.ID, "only 2 of the 3 reels were delivered"); err != nil {
		t.Fatalf("complain: %v", err)
	}
	if err := h.svc.ProposeSplit(h.ctx, g.ID, "client", 30); err != nil {
		t.Fatalf("client proposal: %v", err)
	}
	if err := h.svc.ProposeSplit(h.ctx, g.ID, "creator", 70); err != nil {
		t.Fatalf("creator proposal: %v", err)
	}
	if err := h.svc.EscalateDispute(h.ctx, g.ID); err != nil {
		t.Fatalf("escalate: %v", err)
	}
	return h.status(t, g.ID, StatusEscalated)
}

// status reloads a gig and asserts the status it should now be in.
func (h *harness) status(t *testing.T, gigID, want string) *Gig {
	t.Helper()
	g, err := getGig(h.ctx, h.pool, gigID, false)
	if err != nil {
		t.Fatalf("reload gig: %v", err)
	}
	if g.Status != want {
		t.Fatalf("gig %s is %s, want %s", gigID, g.Status, want)
	}
	return g
}

func (h *harness) balance(t *testing.T, account string) int64 {
	t.Helper()
	b, err := ledger.Balance(h.ctx, h.pool, account)
	if err != nil {
		t.Fatalf("balance(%s): %v", account, err)
	}
	return b
}

// assertBalances checks several ledger accounts at once, which is what makes a
// lifecycle test meaningful: the status alone doesn't prove the money moved.
func (h *harness) assertBalances(t *testing.T, want map[string]int64) {
	t.Helper()
	for account, w := range want {
		if got := h.balance(t, account); got != w {
			t.Errorf("balance(%s) = %d, want %d", account, got, w)
		}
	}
}

// actions returns the audit actions recorded for a gig, in order.
func (h *harness) actions(t *testing.T, gigID string) []string {
	t.Helper()
	rows, err := h.pool.Query(h.ctx, `SELECT action FROM audit_log WHERE gig_id = $1 ORDER BY id`, gigID)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		out = append(out, a)
	}
	return out
}

func (h *harness) hasAction(t *testing.T, gigID, action string) bool {
	t.Helper()
	for _, a := range h.actions(t, gigID) {
		if a == action {
			return true
		}
	}
	return false
}

// pendingJobs returns the queued, not-yet-run jobs of a kind for a gig.
func (h *harness) pendingJobs(t *testing.T, kind, gigID string) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM jobs
		WHERE kind = $1 AND done_at IS NULL AND payload->>'gig_id' = $2`, kind, gigID).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return n
}

// makeReleaseDue brings the complaint window forward so AutoRelease can run,
// standing in for the auto_release job firing when it is due.
func (h *harness) makeReleaseDue(t *testing.T, gigID string) {
	t.Helper()
	if _, err := h.pool.Exec(h.ctx,
		`UPDATE gigs SET release_at = $2 WHERE id = $1`, gigID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("make release due: %v", err)
	}
}

// setEventAt moves the gig date, which is what decides whether a cancellation
// counts as late.
func (h *harness) setEventAt(t *testing.T, gigID string, at time.Time) {
	t.Helper()
	if _, err := h.pool.Exec(h.ctx, `UPDATE gigs SET event_at = $2 WHERE id = $1`, gigID, at); err != nil {
		t.Fatalf("set event_at: %v", err)
	}
}

// txnCount counts posted ledger transactions of a kind for a gig.
func (h *harness) txnCount(t *testing.T, gigID, kind string) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(h.ctx,
		`SELECT count(*) FROM ledger_txns WHERE gig_id = $1 AND kind = $2`, gigID, kind).Scan(&n); err != nil {
		t.Fatalf("count txns: %v", err)
	}
	return n
}

// usdtFor is the USDT amount the mock settles a naira invoice at.
func usdtFor(amountNGNKobo int64) int64 {
	return amountNGNKobo / 100 * 1_000_000 / mockRateNGN
}

// escrow, frozen and worker name the ledger accounts under test.
func (h *harness) escrow(gigID string) string { return ledger.Escrow(gigID) }
func (h *harness) frozen(gigID string) string { return ledger.Frozen(gigID) }
func (h *harness) worker() string             { return ledger.Worker("WRK_chidinma") }
