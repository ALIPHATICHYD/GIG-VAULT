package gigs

import "testing"

// The client's notifications feed shows events on invoices addressed to them,
// newest first, and nothing from other clients' invoices.
func TestClientActivityShowsOnlyTheirInvoices(t *testing.T) {
	h := newHarness(t)
	mine := h.sendInvoice(t, "40000", 20)
	h.claim(t, mine.ID)
	h.pay(t, mine)
	h.sendInvoice(t, "20000", 20) // not claimed by this client

	rows, err := h.svc.ClientActivity(h.ctx, h.clientID)
	if err != nil {
		t.Fatalf("ClientActivity: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no activity for the client")
	}
	funded := false
	for i, r := range rows {
		if r.GigID == nil || *r.GigID != mine.ID {
			t.Fatalf("row %d is for gig %v, want only %s", i, r.GigID, mine.ID)
		}
		if i > 0 && r.ID >= rows[i-1].ID {
			t.Fatalf("not newest first at row %d", i)
		}
		if r.Action == "gig.funded" {
			funded = true
		}
	}
	if !funded {
		t.Error("missing gig.funded event")
	}
}
