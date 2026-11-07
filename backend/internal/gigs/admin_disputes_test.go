package gigs

import (
	"testing"
)

// GET /v1/admin/disputes is GigVault's arbitration queue. The query behind it
// stitches together complaints, gigs, workers, users and the audit log, so it
// is worth proving against a real database rather than only compiling.

func TestEscalatedDisputesListsWhatGigVaultNeedsToDecide(t *testing.T) {
	h := newHarness(t)
	g := h.escalatedDispute(t, "150000")

	list, err := h.svc.EscalatedDisputes(h.ctx)
	if err != nil {
		t.Fatalf("EscalatedDisputes: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d escalated disputes, want 1", len(list))
	}
	d := list[0]

	if d.GigID != g.ID {
		t.Errorf("gig id = %q, want %q", d.GigID, g.ID)
	}
	if d.Title != "Instagram campaign: 3 reels" {
		t.Errorf("title = %q", d.Title)
	}
	if d.AmountNGNKobo != 15_000_000 {
		t.Errorf("amount = %d kobo, want 15000000", d.AmountNGNKobo)
	}
	if d.Reason == "" {
		t.Error("reason is empty")
	}
	if d.RaisedBy != "client" {
		t.Errorf("raised_by = %q, want client", d.RaisedBy)
	}
	if d.FeePercent != 10 {
		t.Errorf("fee_percent = %d, want 10", d.FeePercent)
	}

	// Both parties, so the admin knows who they are arbitrating between.
	if d.Creator.Name == nil || *d.Creator.Name != "Chidinma Okafor" {
		t.Errorf("creator name = %v, want Chidinma Okafor", d.Creator.Name)
	}
	if d.Client.Name == nil || *d.Client.Name != "Tolu Adeyemi" {
		t.Errorf("client name = %v, want Tolu Adeyemi", d.Client.Name)
	}

	// The frozen amount is read from the ledger, and matches it.
	if want := h.balance(t, h.frozen(g.ID)); d.FrozenUSDTMicro != want {
		t.Errorf("frozen = %d, want %d (the ledger balance)", d.FrozenUSDTMicro, want)
	}
	if d.FrozenUSDTMicro <= 0 {
		t.Error("frozen amount is not positive; a dispute should have money frozen")
	}
	if d.FrozenNGNKobo <= 0 {
		t.Error("frozen amount in naira is not positive")
	}

	if d.EscalatedAt == nil {
		t.Error("escalated_at is nil; the admin can't tell how long this has been waiting")
	}

	// Each side's last proposal, which is the whole point of the endpoint.
	if len(d.Proposals) != 2 {
		t.Fatalf("got %d proposals, want one per side: %+v", len(d.Proposals), d.Proposals)
	}
	byActor := map[string]int{}
	for _, p := range d.Proposals {
		byActor[p.By] = p.CreatorPercent
		if p.At.IsZero() {
			t.Errorf("proposal by %s has no timestamp", p.By)
		}
	}
	if byActor["client"] != 30 {
		t.Errorf("client proposal = %d%%, want 30", byActor["client"])
	}
	if byActor["creator"] != 70 {
		t.Errorf("creator proposal = %d%%, want 70", byActor["creator"])
	}
}

// complaints keeps only one proposal column, overwritten by each new proposal,
// which is why the endpoint reads the audit log instead. Re-proposing must
// therefore update that side's figure without losing the other side's.
func TestEscalatedDisputesShowsEachSidesLatestProposal(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, "150000", 20)
	h.claim(t, g.ID)
	h.pay(t, g)
	if _, err := h.svc.Complete(h.ctx, g.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := h.svc.Complain(h.ctx, g.ID, "two reels, not three"); err != nil {
		t.Fatalf("complain: %v", err)
	}
	// Three rounds of haggling before the window closes.
	for _, p := range []struct {
		by  string
		pct int
	}{{"client", 20}, {"creator", 80}, {"client", 45}} {
		if err := h.svc.ProposeSplit(h.ctx, g.ID, p.by, p.pct); err != nil {
			t.Fatalf("%s proposing %d: %v", p.by, p.pct, err)
		}
	}
	if err := h.svc.EscalateDispute(h.ctx, g.ID); err != nil {
		t.Fatalf("escalate: %v", err)
	}

	list, err := h.svc.EscalatedDisputes(h.ctx)
	if err != nil {
		t.Fatalf("EscalatedDisputes: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d disputes, want 1", len(list))
	}
	byActor := map[string]int{}
	for _, p := range list[0].Proposals {
		byActor[p.By] = p.CreatorPercent
	}
	if byActor["client"] != 45 {
		t.Errorf("client's latest proposal = %d%%, want 45 (not the earlier 20)", byActor["client"])
	}
	if byActor["creator"] != 80 {
		t.Errorf("creator's proposal = %d%%, want 80", byActor["creator"])
	}
}

func TestEscalatedDisputesExcludesEverythingElse(t *testing.T) {
	h := newHarness(t)

	// An unpaid invoice and a settled booking are not disputes.
	h.sendInvoice(t, "50000", 20)
	paid := h.sendInvoice(t, "80000", 20)
	h.claim(t, paid.ID)
	h.pay(t, paid)

	// An open dispute both sides are still negotiating is not GigVault's to
	// decide yet, so it must not appear either.
	open := h.sendInvoice(t, "90000", 20)
	h.claim(t, open.ID)
	h.pay(t, open)
	if _, err := h.svc.Complete(h.ctx, open.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := h.svc.Complain(h.ctx, open.ID, "still talking"); err != nil {
		t.Fatalf("complain: %v", err)
	}

	list, err := h.svc.EscalatedDisputes(h.ctx)
	if err != nil {
		t.Fatalf("EscalatedDisputes: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("got %d disputes, want 0: only escalated ones belong here, got %+v", len(list), list)
	}
}

// "Done when: the endpoint returns escalated disputes and resolved ones drop
// off the list."
func TestResolvedDisputesDropOffTheList(t *testing.T) {
	h := newHarness(t)
	g := h.escalatedDispute(t, "150000")

	list, err := h.svc.EscalatedDisputes(h.ctx)
	if err != nil {
		t.Fatalf("EscalatedDisputes: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("before resolving: got %d, want 1", len(list))
	}

	if _, err := h.svc.ResolveDispute(h.ctx, g.ID, 60); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	list, err = h.svc.EscalatedDisputes(h.ctx)
	if err != nil {
		t.Fatalf("EscalatedDisputes after resolving: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("after resolving: got %d, want 0", len(list))
	}
}

// Several waiting disputes come back oldest escalation first, so the admin
// works through the queue in the order people have been kept waiting.
func TestEscalatedDisputesAreOldestFirst(t *testing.T) {
	h := newHarness(t)
	first := h.escalatedDispute(t, "100000")
	second := h.escalatedDispute(t, "200000")

	list, err := h.svc.EscalatedDisputes(h.ctx)
	if err != nil {
		t.Fatalf("EscalatedDisputes: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d disputes, want 2", len(list))
	}
	if list[0].GigID != first.ID || list[1].GigID != second.ID {
		t.Errorf("order = [%s, %s], want [%s, %s]", list[0].GigID, list[1].GigID, first.ID, second.ID)
	}
}
