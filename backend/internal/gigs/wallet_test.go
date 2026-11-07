package gigs

import "testing"

// Wallet sums every invoice's escrow and frozen balance in one query. Prove the
// totals still match the per-account ledger balances.
func TestWalletTotalsMatchLedger(t *testing.T) {
	h := newHarness(t)
	disputed := h.escalatedDispute(t, "150000")
	held := h.sendInvoice(t, "40000", 20)
	h.claim(t, held.ID)
	h.pay(t, held)
	h.sendInvoice(t, "20000", 20) // unpaid: contributes nothing

	wl, err := h.svc.Wallet(h.ctx, "WRK_chidinma")
	if err != nil {
		t.Fatalf("Wallet: %v", err)
	}
	wantLocked := h.balance(t, h.escrow(disputed.ID)) + h.balance(t, h.escrow(held.ID))
	wantFrozen := h.balance(t, h.frozen(disputed.ID)) + h.balance(t, h.frozen(held.ID))
	if wl.LockedUSDT != wantLocked {
		t.Errorf("locked = %d, want %d", wl.LockedUSDT, wantLocked)
	}
	if wl.FrozenUSDT != wantFrozen || wantFrozen <= 0 {
		t.Errorf("frozen = %d, want %d (> 0)", wl.FrozenUSDT, wantFrozen)
	}
	if wl.AvailableUSDT != h.balance(t, h.worker()) {
		t.Errorf("available = %d, want %d", wl.AvailableUSDT, h.balance(t, h.worker()))
	}
}
