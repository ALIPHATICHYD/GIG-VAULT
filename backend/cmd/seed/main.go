// Command seed puts the database into a known, ready-to-demo state: one
// creator, one client and three invoices at three different stages.
//
// It builds that state by calling the real service — CreateGigLink, Checkout,
// MarkFunded, Complete, AutoRelease, Cashout — rather than writing rows by
// hand, so what you demo is what the code actually does, and the ledger
// balances and timelines are genuine.
//
// Run it from the backend directory:
//
//	go run ./cmd/seed -reset        # wipe, then seed
//	go run ./cmd/seed               # seed on top of what's there
//
// -reset is destructive: see the comment on wipe.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/config"
	"github.com/aliphatichyd/gig-vault/backend/internal/gigs"
	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

// The demo accounts EnsureDemoWorker creates. Password: demo1234.
const (
	demoWorkerID = "WRK_chidinma"
	demoClientID = "USR_demo_client"
	clientName   = "Tolu Adeyemi"
	clientEmail  = "tolu@demo.gigvault"
)

// Every table, for -reset. ledger_entries has an append-only trigger that
// rejects DELETE, so a partial clean-up is impossible by design and TRUNCATE
// (which bypasses row triggers) is the only way to start fresh.
var allTables = []string{
	"ledger_entries", "ledger_txns", "audit_log", "complaints",
	"payouts", "busha_events", "jobs", "gigs", "users", "workers",
}

func main() {
	reset := flag.Bool("reset", false, "wipe every table before seeding (destructive)")
	force := flag.Bool("force", false, "allow -reset even when BUSHA_MODE=live")
	countdown := flag.Duration("countdown", 10*time.Minute, "time left on the releasing invoice")
	flag.Parse()

	if err := run(*reset, *force, *countdown); err != nil {
		fmt.Fprintln(os.Stderr, "seed failed:", err)
		os.Exit(1)
	}
}

func run(reset, force bool, countdown time.Duration) error {
	config.LoadDotEnv(".env")
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()

	// A wipe against live Busha keys would destroy records of real money, so
	// it takes a second, explicit flag.
	if reset && cfg.BushaMode == "live" && !force {
		return errors.New("BUSHA_MODE=live: refusing to wipe a database tied to real Busha keys.\n" +
			"  Point DATABASE_URL at a demo database, or pass -force if you are certain")
	}

	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}

	if reset {
		if err := wipe(ctx, pool, cfg.DatabaseURL); err != nil {
			return err
		}
	}

	svc := &gigs.Service{
		DB:    pool,
		Busha: busha.NewMock(cfg.MockRateNGN),
		Cfg:   cfg,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if cfg.BushaMode == "live" {
		// Seeding talks to Busha for payment requests and quotes. Against live
		// keys that would create real objects, so always use the mock here.
		fmt.Println("note: BUSHA_MODE=live, but seeding uses the Busha mock so no real objects are created")
	}

	if err := svc.EnsureDemoWorker(ctx); err != nil {
		return fmt.Errorf("create demo accounts: %w", err)
	}
	fmt.Println("accounts:  chidinma@demo.gigvault (creator) · tolu@demo.gigvault (client) · password demo1234")

	s := &seeder{svc: svc, pool: pool, ctx: ctx, cfg: cfg}

	// 1. Awaiting payment: what the client pays during the demo.
	pending, err := s.invoice("Wedding reception — live band set", "120000", 30, 10*24*time.Hour)
	if err != nil {
		return err
	}
	fmt.Printf("  %s  %-34s ₦120,000  30%% upfront  AWAITING_PAYMENT\n", pending.ID, "Wedding reception")
	fmt.Printf("             pay link: /pay/%s?t=%s\n", pending.ID, pending.ClientToken)

	// 2. Paid and counting down to release, so the demo has a live timer.
	releasing, err := s.invoice("Instagram campaign: 3 reels", "150000", 50, -2*24*time.Hour)
	if err != nil {
		return err
	}
	if err := s.pay(releasing); err != nil {
		return err
	}
	if _, err := svc.Complete(ctx, releasing.ID); err != nil {
		return fmt.Errorf("mark %s done: %w", releasing.ID, err)
	}
	if err := s.setCountdown(releasing.ID, countdown); err != nil {
		return err
	}
	fmt.Printf("  %s  %-34s ₦150,000  50%% upfront  DONE_PENDING, releasing in %s\n",
		releasing.ID, "Instagram campaign", countdown)

	// 3. Finished and cashed out to the bank.
	done, err := s.invoice("Bridal nails — party of 6", "45000", 20, -7*24*time.Hour)
	if err != nil {
		return err
	}
	if err := s.pay(done); err != nil {
		return err
	}
	if _, err := svc.Complete(ctx, done.ID); err != nil {
		return fmt.Errorf("mark %s done: %w", done.ID, err)
	}
	if err := s.releaseNow(done.ID); err != nil {
		return err
	}
	payout, err := s.cashout("45000")
	if err != nil {
		return err
	}
	fmt.Printf("  %s  %-34s ₦45,000   20%% upfront  RELEASED and paid out (%s)\n",
		done.ID, "Bridal nails", payout)

	// Leave the operator with the numbers they will be asked about on stage.
	w, err := svc.Wallet(ctx, demoWorkerID)
	if err != nil {
		return err
	}
	fmt.Printf("\nwallet:    %s withdrawable · %s locked · %s frozen\n",
		naira(w.AvailableNGN), naira(w.LockedNGN), naira(w.FrozenNGN))
	rec, err := svc.Reconcile(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("ledger:    owed %s · platform fees %s\n",
		usdt(rec.LedgerOwedUSDT), usdt(rec.PlatformFeesUSDT))
	fmt.Println("\nready. Open http://localhost:5173 and log in with the accounts above.")
	return nil
}

// wipe empties every table. This is the whole database, not just demo rows:
// see the comment on allTables for why a narrower clean-up is not possible.
func wipe(ctx context.Context, pool *pgxpool.Pool, dsn string) error {
	fmt.Printf("reset:     %s\n", safeTarget(dsn))
	fmt.Printf("           truncating %d tables: %v\n", len(allTables), allTables)

	stmt := "TRUNCATE " + strings.Join(allTables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	return nil
}

type seeder struct {
	svc  *gigs.Service
	pool *pgxpool.Pool
	ctx  context.Context
	cfg  config.Config
}

// invoice sends an invoice and attaches it to the demo client, so it shows up
// under their Pending payments without anyone opening the link first.
// eventIn may be negative for a gig that has already happened.
func (s *seeder) invoice(title, amountNGN string, upfrontPercent int, eventIn time.Duration) (*gigs.Gig, error) {
	link, err := s.svc.CreateGigLink(s.ctx, demoWorkerID, gigs.CreateGigLinkInput{
		Title:          title,
		EventAt:        time.Now().Add(eventIn),
		AmountNGN:      amountNGN,
		ClientName:     clientName,
		ClientEmail:    clientEmail,
		UpfrontPercent: &upfrontPercent,
		Note:           "Seeded for the demo.",
	})
	if err != nil {
		return nil, fmt.Errorf("create invoice %q: %w", title, err)
	}
	if _, err := s.pool.Exec(s.ctx,
		`UPDATE gigs SET client_user_id = $2 WHERE id = $1`, link.Gig.ID, demoClientID); err != nil {
		return nil, fmt.Errorf("assign %s to the demo client: %w", link.Gig.ID, err)
	}
	return link.Gig, nil
}

// pay runs checkout and then the funding path a Busha webhook would trigger,
// so the invoice carries real payment details and a real rate.
func (s *seeder) pay(g *gigs.Gig) error {
	if _, err := s.svc.Checkout(s.ctx, g.ID, g.ClientToken, gigs.CheckoutInput{
		ClientName: clientName, ClientEmail: clientEmail, Currency: "NGN",
	}); err != nil {
		return fmt.Errorf("checkout %s: %w", g.ID, err)
	}
	settled := gigs.FormatMinor(g.AmountNGNKobo/100*1_000_000/s.cfg.MockRateNGN, 6)
	if err := s.svc.MarkFunded(s.ctx, g.ID, settled, "payment_request.completed"); err != nil {
		return fmt.Errorf("fund %s: %w", g.ID, err)
	}
	return nil
}

// setCountdown gives the releasing invoice a comfortable amount of time left,
// instead of whatever COMPLAINT_WINDOW happens to be. The queued auto_release
// job is moved with it, so the timer on screen matches when it will really fire.
func (s *seeder) setCountdown(gigID string, d time.Duration) error {
	at := time.Now().Add(d)
	if _, err := s.pool.Exec(s.ctx, `UPDATE gigs SET release_at = $2 WHERE id = $1`, gigID, at); err != nil {
		return fmt.Errorf("set release_at on %s: %w", gigID, err)
	}
	if _, err := s.pool.Exec(s.ctx,
		`UPDATE jobs SET run_at = $2 WHERE dedupe_key = $1 AND done_at IS NULL`, "auto_release:"+gigID, at); err != nil {
		return fmt.Errorf("reschedule auto-release for %s: %w", gigID, err)
	}
	return nil
}

// releaseNow brings the complaint window forward and releases, the way the
// auto_release job does once it is due.
func (s *seeder) releaseNow(gigID string) error {
	if _, err := s.pool.Exec(s.ctx,
		`UPDATE gigs SET release_at = now() - interval '1 second' WHERE id = $1`, gigID); err != nil {
		return fmt.Errorf("bring release forward on %s: %w", gigID, err)
	}
	if err := s.svc.AutoRelease(s.ctx, gigID); err != nil {
		return fmt.Errorf("release %s: %w", gigID, err)
	}
	// The job is already satisfied; mark it done so the worker skips it.
	if _, err := s.pool.Exec(s.ctx,
		`UPDATE jobs SET done_at = now() WHERE dedupe_key = $1`, "auto_release:"+gigID); err != nil {
		return fmt.Errorf("close the auto-release job for %s: %w", gigID, err)
	}
	return nil
}

// cashout sends naira to the creator's bank and settles it immediately, since
// no worker is running to pick up the mock settlement job.
func (s *seeder) cashout(amountNGN string) (string, error) {
	p, err := s.svc.Cashout(s.ctx, demoWorkerID, gigs.CashoutInput{AmountNGN: amountNGN})
	if err != nil {
		return "", fmt.Errorf("cash out: %w", err)
	}
	if p.BushaTransferID == nil {
		return "", errors.New("cash-out returned no transfer id")
	}
	if err := s.svc.CompletePayoutByTransfer(s.ctx, *p.BushaTransferID); err != nil {
		return "", fmt.Errorf("settle payout %s: %w", p.ID, err)
	}
	if _, err := s.pool.Exec(s.ctx,
		`UPDATE jobs SET done_at = now() WHERE dedupe_key = $1`, "mock_settle:"+*p.BushaTransferID); err != nil {
		return "", fmt.Errorf("close the settlement job: %w", err)
	}
	return p.ID, nil
}

// safeTarget describes the database without printing the password.
func safeTarget(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "(unparseable DATABASE_URL)"
	}
	user := ""
	if u.User != nil {
		user = u.User.Username() + "@"
	}
	return user + u.Host + u.Path
}

func naira(kobo int64) string { return fmt.Sprintf("₦%d", kobo/100) }
func usdt(micro int64) string { return gigs.FormatMinor(micro, 6) + " USDT" }
