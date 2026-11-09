package gigs

import (
	"testing"

	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
)

// liveMock behaves like the mock but reports live mode, so Cashout takes the
// path that needs a real Busha recipient.
type liveMock struct{ *busha.Mock }

func (liveMock) Mode() string { return "live" }

// A worker with saved bank details but no Busha recipient (the seeded demo
// worker) gets one registered on first cash-out instead of an error.
func TestCashoutRegistersMissingRecipient(t *testing.T) {
	h := newHarness(t)
	if _, err := h.pool.Exec(h.ctx, `UPDATE workers SET bank_code = '000013', account_number = '0123456789',
		bank_name = 'GTBank', account_name = 'ABDULLAH OYETORO' WHERE id = $1`, h.workerID); err != nil {
		t.Fatal(err)
	}
	h.svc.Busha = liveMock{h.svc.Busha.(*busha.Mock)}

	id, err := h.svc.ensureRecipient(h.ctx, mustWorker(t, h))
	if err != nil {
		t.Fatalf("ensureRecipient: %v", err)
	}
	if id == "" {
		t.Fatal("no recipient id")
	}
	if w := mustWorker(t, h); w.BushaRecipientID == nil || *w.BushaRecipientID != id {
		t.Errorf("recipient not saved: %v", w.BushaRecipientID)
	}
}

func TestCashoutWithoutBankDetailsStillAsks(t *testing.T) {
	h := newHarness(t)
	h.svc.Busha = liveMock{h.svc.Busha.(*busha.Mock)}
	if _, err := h.svc.ensureRecipient(h.ctx, mustWorker(t, h)); err == nil {
		t.Fatal("want no_bank_account error")
	}
}

func mustWorker(t *testing.T, h *harness) *Worker {
	t.Helper()
	w, err := getWorker(h.ctx, h.pool, h.workerID)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
