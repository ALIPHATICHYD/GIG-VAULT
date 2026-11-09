package gigs

import "testing"

// The demo cast was renamed on a database that already had the old rows, so
// EnsureDemoWorker must update them in place, not skip them.
func TestEnsureDemoWorkerRenamesExistingCast(t *testing.T) {
	h := newHarness(t) // worker WRK_chidinma is "Chidinma Okafor"
	g := h.sendInvoice(t, "20000", 20) // client_name "Tolu Adeyemi"
	// Swap the harness logins for the old demo logins, as on the team's database.
	if _, err := h.pool.Exec(h.ctx, `UPDATE users SET id = 'USR_demo_creator', email = 'chidinma@demo.gigvault' WHERE id = $1`, h.creatorUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(h.ctx, `UPDATE users SET id = 'USR_demo_client', email = 'tolu@demo.gigvault' WHERE id = $1`, h.clientID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureDemoWorker(h.ctx); err != nil {
		t.Fatalf("EnsureDemoWorker: %v", err)
	}
	w, err := getWorker(h.ctx, h.pool, h.workerID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "Abdullah Oyetoro" {
		t.Errorf("worker name = %q, want Abdullah Oyetoro", w.Name)
	}
	var client string
	if err := h.pool.QueryRow(h.ctx, `SELECT client_name FROM gigs WHERE id = $1`, g.ID).Scan(&client); err != nil {
		t.Fatal(err)
	}
	if client != "Aminat Bello" {
		t.Errorf("client_name = %q, want Aminat Bello", client)
	}
	// Running it again changes nothing and does not fail.
	if err := h.svc.EnsureDemoWorker(h.ctx); err != nil {
		t.Fatalf("second EnsureDemoWorker: %v", err)
	}
}

// On the team's database someone had already signed up with a demo email under
// another id, and the API refused to start (users_email_key).
func TestEnsureDemoWorkerToleratesTakenEmail(t *testing.T) {
	h := newHarness(t)
	if _, err := h.pool.Exec(h.ctx, `UPDATE users SET email = 'abdullah@demo.gigvault' WHERE id = $1`, h.creatorUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(h.ctx, `UPDATE users SET email = 'aminat@demo.gigvault' WHERE id = $1`, h.clientID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureDemoWorker(h.ctx); err != nil {
		t.Fatalf("EnsureDemoWorker: %v", err)
	}
	var n int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM users WHERE email IN ('abdullah@demo.gigvault','aminat@demo.gigvault')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("demo email rows = %d, want 2", n)
	}
}
