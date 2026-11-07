package gigs

import (
	"errors"
	"testing"
	"time"

	"github.com/aliphatichyd/gig-vault/backend/internal/ledger"
)

// Every path an invoice can take, end to end, asserting the status *and* the
// ledger balances at each step. The status alone proves nothing: what matters
// is that the right amount of money ended up with the right party.
//
// Amounts throughout use a ₦150,000 invoice at the mock rate of 1600 NGN/USDT:
//
//	150,000 NGN  = 93.75 USDT = 93,750,000 micro
//	20% upfront  = 18,750,000 micro
//	remainder    = 75,000,000 micro
const (
	invoiceNGN  = "150000"
	totalUSDT   = int64(93_750_000)
	upfrontUSDT = int64(18_750_000)
	restUSDT    = int64(75_000_000)
)

// ---------- the happy path ----------

func TestPaidThenDoneThenReleased(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)
	h.claim(t, g.ID)

	// Nothing has moved yet: an invoice is just a request.
	h.assertBalances(t, map[string]int64{h.escrow(g.ID): 0, h.worker(): 0})

	// The client pays. The upfront goes straight to the creator so she can
	// start work; the rest is locked in escrow.
	h.pay(t, g)
	h.status(t, g.ID, StatusBooked)
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID):        restUSDT,
		h.worker():            upfrontUSDT,
		ledger.ExternalClient: -totalUSDT,
	})

	// The client confirms the work is done, which starts the complaint window.
	if _, err := h.svc.Complete(h.ctx, g.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	done := h.status(t, g.ID, StatusDonePending)
	if done.ReleaseAt == nil {
		t.Fatal("release_at was not set, so nothing would auto-release")
	}
	if n := h.pendingJobs(t, JobAutoRelease, g.ID); n != 1 {
		t.Errorf("%d auto_release jobs queued, want 1", n)
	}
	// Still locked until the window closes.
	h.assertBalances(t, map[string]int64{h.escrow(g.ID): restUSDT, h.worker(): upfrontUSDT})

	// The window closes and the worker releases the remainder.
	h.makeReleaseDue(t, g.ID)
	if err := h.svc.AutoRelease(h.ctx, g.ID); err != nil {
		t.Fatalf("auto release: %v", err)
	}
	h.status(t, g.ID, StatusReleased)
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID): 0,
		h.frozen(g.ID): 0,
		h.worker():     totalUSDT, // upfront + remainder, nothing lost
	})

	for _, want := range []string{"invoice.sent", "gig.funded", "upfront.released", "gig.marked_done", "gig.auto_released"} {
		if !h.hasAction(t, g.ID, want) {
			t.Errorf("timeline is missing %q: %v", want, h.actions(t, g.ID))
		}
	}
}

// A creator can ask for nothing upfront, in which case everything waits.
func TestZeroUpfrontLocksEverything(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 0)
	h.pay(t, g)

	h.status(t, g.ID, StatusBooked)
	h.assertBalances(t, map[string]int64{h.escrow(g.ID): totalUSDT, h.worker(): 0})
	if h.hasAction(t, g.ID, "upfront.released") {
		t.Error("upfront.released was recorded for a 0% invoice")
	}
}

// ---------- dispute settled between the two sides ----------

func TestDisputeSettledByAgreementChargesNoFee(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)
	h.claim(t, g.ID)
	h.pay(t, g)
	if _, err := h.svc.Complete(h.ctx, g.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// A complaint freezes half of what is left; the other half still releases
	// to the creator on time.
	if _, err := h.svc.Complain(h.ctx, g.ID, "only 2 of the 3 reels were delivered"); err != nil {
		t.Fatalf("complain: %v", err)
	}
	h.status(t, g.ID, StatusDisputed)
	const frozen = restUSDT / 2 // FREEZE_BPS = 5000
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID): restUSDT - frozen,
		h.frozen(g.ID): frozen,
		h.worker():     upfrontUSDT,
	})

	// They agree the creator keeps 70% of the frozen money. One side proposes,
	// the other accepts.
	if err := h.svc.ProposeSplit(h.ctx, g.ID, "client", 70); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := h.svc.AcceptSplit(h.ctx, g.ID, "creator"); err != nil {
		t.Fatalf("accept: %v", err)
	}

	toCreator := frozen * 70 / 100
	toClient := frozen - toCreator
	h.status(t, g.ID, StatusPartiallyRefunded)
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID):        0,
		h.frozen(g.ID):        0,
		h.worker():            upfrontUSDT + (restUSDT - frozen) + toCreator,
		ledger.ExternalRefund: toClient,
		PlatformFees:          0, // settling between themselves is free
	})
	if !h.hasAction(t, g.ID, "dispute.settled_by_agreement") {
		t.Errorf("timeline is missing the settlement: %v", h.actions(t, g.ID))
	}
}

// Agreeing that the creator keeps all of it leaves nothing to refund, so the
// invoice ends RELEASED rather than PARTIALLY_REFUNDED.
func TestDisputeSettledFullyForCreatorEndsReleased(t *testing.T) {
	h := newHarness(t)
	g := h.disputed(t, invoiceNGN)

	if err := h.svc.ProposeSplit(h.ctx, g.ID, "client", 100); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := h.svc.AcceptSplit(h.ctx, g.ID, "creator"); err != nil {
		t.Fatalf("accept: %v", err)
	}

	h.status(t, g.ID, StatusReleased)
	h.assertBalances(t, map[string]int64{
		h.worker():            totalUSDT,
		ledger.ExternalRefund: 0,
		PlatformFees:          0,
	})
}

func TestOneSideCannotAcceptItsOwnProposal(t *testing.T) {
	h := newHarness(t)
	g := h.disputed(t, invoiceNGN)

	if err := h.svc.ProposeSplit(h.ctx, g.ID, "creator", 90); err != nil {
		t.Fatalf("propose: %v", err)
	}
	err := h.svc.AcceptSplit(h.ctx, g.ID, "creator")
	assertAppCode(t, err, "own_proposal")

	// Nothing settled.
	h.status(t, g.ID, StatusDisputed)
}

func TestAcceptingWithNoProposalIsRejected(t *testing.T) {
	h := newHarness(t)
	g := h.disputed(t, invoiceNGN)
	assertAppCode(t, h.svc.AcceptSplit(h.ctx, g.ID, "client"), "no_proposal")
}

// ---------- dispute escalated and decided by GigVault ----------

func TestEscalatedDisputeResolvedByGigVaultTakesTheFee(t *testing.T) {
	h := newHarness(t)
	g := h.escalatedDispute(t, invoiceNGN)

	const frozen = restUSDT / 2
	// Escalation itself moves no money; it only hands the decision over.
	h.assertBalances(t, map[string]int64{h.frozen(g.ID): frozen, PlatformFees: 0})

	// GigVault decides 60/40 and keeps its 10% resolution fee.
	if _, err := h.svc.ResolveDispute(h.ctx, g.ID, 60); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	fee := frozen * 10 / 100
	toCreator := (frozen - fee) * 60 / 100
	toClient := frozen - fee - toCreator

	h.status(t, g.ID, StatusPartiallyRefunded)
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID):        0,
		h.frozen(g.ID):        0,
		PlatformFees:          fee,
		h.worker():            upfrontUSDT + (restUSDT - frozen) + toCreator,
		ledger.ExternalRefund: toClient,
	})

	// That fee is GigVault's business model, so pin the number down: 10% of
	// the frozen money, and nothing from the rest of the invoice.
	if fee != 3_750_000 {
		t.Errorf("fee = %d, want 3750000 (10%% of %d frozen)", fee, frozen)
	}
	if !h.hasAction(t, g.ID, "dispute.resolved_by_gigvault") {
		t.Errorf("timeline is missing the resolution: %v", h.actions(t, g.ID))
	}
}

// GigVault may only step in after the window has passed without agreement.
func TestGigVaultCannotResolveADisputeStillBeingNegotiated(t *testing.T) {
	h := newHarness(t)
	g := h.disputed(t, invoiceNGN)

	_, err := h.svc.ResolveDispute(h.ctx, g.ID, 50)
	assertAppCode(t, err, "invalid_state")
	h.assertBalances(t, map[string]int64{PlatformFees: 0})
}

// Escalation must not fire while the two sides still have time to agree.
func TestEscalationDoesNothingOnceSettled(t *testing.T) {
	h := newHarness(t)
	g := h.disputed(t, invoiceNGN)

	if err := h.svc.ProposeSplit(h.ctx, g.ID, "client", 50); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := h.svc.AcceptSplit(h.ctx, g.ID, "creator"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	before := h.balance(t, h.worker())

	// The escalate job fires late, after they already agreed. It must be inert.
	if err := h.svc.EscalateDispute(h.ctx, g.ID); err != nil {
		t.Fatalf("late escalation returned an error: %v", err)
	}
	h.status(t, g.ID, StatusPartiallyRefunded)
	if got := h.balance(t, h.worker()); got != before {
		t.Errorf("worker balance changed from %d to %d on a no-op escalation", before, got)
	}
}

// ---------- cancellation ----------

func TestEarlyCancelRefundsEverythingIncludingTheUpfront(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)
	h.claim(t, g.ID)
	h.pay(t, g)

	// The gig is a fortnight away, so this is not a late cancellation.
	if _, err := h.svc.Cancel(h.ctx, g.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	h.status(t, g.ID, StatusCancelled)
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID):        0,
		h.worker():            0,         // the upfront went back too
		ledger.ExternalRefund: totalUSDT, // the client is whole again
	})
}

func TestLateCancelLeavesTheUpfrontWithTheCreator(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)
	h.claim(t, g.ID)
	h.pay(t, g)

	// The gig is tomorrow morning: inside LATE_CANCEL_WINDOW, so the creator
	// keeps the upfront for the time she has already lost.
	h.setEventAt(t, g.ID, time.Now().Add(6*time.Hour))

	if _, err := h.svc.Cancel(h.ctx, g.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	h.status(t, g.ID, StatusCancelled)
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID):        0,
		h.worker():            upfrontUSDT,
		ledger.ExternalRefund: restUSDT,
	})
}

func TestCancellingBeforePaymentJustClosesTheInvoice(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)

	if _, err := h.svc.Cancel(h.ctx, g.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	h.status(t, g.ID, StatusCancelled)
	if n := h.txnCount(t, g.ID, "refund"); n != 0 {
		t.Errorf("%d refund transactions posted, want 0: no money had moved", n)
	}
}

func TestCannotCancelAfterTheWorkIsDone(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)
	h.pay(t, g)
	if _, err := h.svc.Complete(h.ctx, g.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	_, err := h.svc.Cancel(h.ctx, g.ID)
	assertAppCode(t, err, "invalid_state")
	h.status(t, g.ID, StatusDonePending)
}

// ---------- webhook replay ----------

func TestDuplicatePaymentWebhookDoesNotDoubleFund(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)

	h.pay(t, g)
	after := map[string]int64{
		h.escrow(g.ID):        h.balance(t, h.escrow(g.ID)),
		h.worker():            h.balance(t, h.worker()),
		ledger.ExternalClient: h.balance(t, ledger.ExternalClient),
	}

	// Busha redelivers the same event twice more.
	for i := 0; i < 2; i++ {
		if err := h.payErr(g); err != nil {
			t.Fatalf("replay %d: %v", i+1, err)
		}
	}

	h.status(t, g.ID, StatusBooked)
	h.assertBalances(t, after)
	if n := h.txnCount(t, g.ID, "fund"); n != 1 {
		t.Errorf("%d fund transactions, want 1", n)
	}
	if n := h.txnCount(t, g.ID, "deposit_release"); n != 1 {
		t.Errorf("%d deposit_release transactions, want 1", n)
	}
}

// AutoRelease and the dispute settlement both post with the idempotency key
// "release:<gig>", so whichever runs second moves nothing. That is deliberate,
// and this test exists so nobody "fixes" it into a double release.
func TestUnfrozenPartIsNotReleasedTwice(t *testing.T) {
	h := newHarness(t)
	g := h.disputed(t, invoiceNGN)
	const frozen = restUSDT / 2

	// The complaint window closes while the dispute is still open, so the
	// unfrozen half goes to the creator and the invoice stays DISPUTED.
	h.makeReleaseDue(t, g.ID)
	if err := h.svc.AutoRelease(h.ctx, g.ID); err != nil {
		t.Fatalf("auto release: %v", err)
	}
	h.status(t, g.ID, StatusDisputed)
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID): 0,
		h.frozen(g.ID): frozen,
		h.worker():     upfrontUSDT + (restUSDT - frozen),
	})
	if !h.hasAction(t, g.ID, "gig.unfrozen_part_released") {
		t.Errorf("timeline is missing the partial release: %v", h.actions(t, g.ID))
	}

	// Now they agree. Settlement also tries to release the escrow remainder,
	// which is already empty and must not pay out a second time.
	if err := h.svc.ProposeSplit(h.ctx, g.ID, "client", 100); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := h.svc.AcceptSplit(h.ctx, g.ID, "creator"); err != nil {
		t.Fatalf("accept: %v", err)
	}

	h.status(t, g.ID, StatusReleased)
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID): 0,
		h.frozen(g.ID): 0,
		h.worker():     totalUSDT, // never more than the invoice
	})
	if n := h.txnCount(t, g.ID, "release"); n != 1 {
		t.Errorf("%d release transactions, want 1", n)
	}
}

// Funding is rejected outright if Busha reports an amount we can't read and we
// have none stored, rather than booking a gig with no money behind it.
func TestFundingWithAnUnknownAmountFails(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)

	if err := h.svc.MarkFunded(h.ctx, g.ID, "not-a-number", "payment_request.completed"); err == nil {
		t.Fatal("funding with an unreadable amount succeeded, want an error")
	}
	h.status(t, g.ID, StatusAwaitingPayment)
	h.assertBalances(t, map[string]int64{h.escrow(g.ID): 0, h.worker(): 0})
}

// We trust what Busha actually settled over what we quoted, since the client
// may have sent a slightly different amount.
func TestFundingTrustsTheSettledAmount(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)

	const settled = int64(90_000_000) // 90 USDT, less than the 93.75 quoted
	if err := h.svc.MarkFunded(h.ctx, g.ID, FormatMinor(settled, 6), "deposit.success"); err != nil {
		t.Fatalf("mark funded: %v", err)
	}

	h.status(t, g.ID, StatusBooked)
	upfront := settled * 20 / 100
	h.assertBalances(t, map[string]int64{
		h.escrow(g.ID): settled - upfront,
		h.worker():     upfront,
	})
}

// ---------- void and expiry (#10, #11) ----------

func TestVoidedInvoiceCannotBePaid(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)

	if _, err := h.svc.VoidInvoice(h.ctx, h.creatorUserID, g.ID); err != nil {
		t.Fatalf("void: %v", err)
	}
	voided := h.status(t, g.ID, StatusVoided)
	if !h.hasAction(t, g.ID, "invoice.voided") {
		t.Errorf("timeline is missing invoice.voided: %v", h.actions(t, g.ID))
	}

	// The pay link no longer works.
	_, err := h.svc.Checkout(h.ctx, g.ID, voided.ClientToken, CheckoutInput{
		ClientName: "Tolu Adeyemi", ClientEmail: "tolu@test.local", Currency: "NGN",
	})
	assertAppCode(t, err, "invalid_state")
}

func TestOnlyAnUnpaidInvoiceCanBeVoided(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)
	h.pay(t, g)

	_, err := h.svc.VoidInvoice(h.ctx, h.creatorUserID, g.ID)
	assertAppCode(t, err, "invalid_state")
	h.status(t, g.ID, StatusBooked)
}

// Another creator's invoice is a 404, not a 403: the endpoint gives away
// nothing about invoices you don't own.
func TestAnotherCreatorCannotVoidYourInvoice(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)

	if _, err := h.pool.Exec(h.ctx, `INSERT INTO workers (id, name) VALUES ('WRK_other', 'Other Creator')`); err != nil {
		t.Fatalf("seed other worker: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO users (id, email, name, role, password_hash, worker_id)
		VALUES ('USR_other', 'other@test.local', 'Other Creator', 'creator', 'x', 'WRK_other')`); err != nil {
		t.Fatalf("seed other user: %v", err)
	}

	_, err := h.svc.VoidInvoice(h.ctx, "USR_other", g.ID)
	assertAppCode(t, err, "not_found")
	h.status(t, g.ID, StatusAwaitingPayment)
}

func TestUnpaidInvoiceExpires(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)

	// The expiry job is queued when the invoice is sent.
	if n := h.pendingJobs(t, JobExpireInvoice, g.ID); n != 1 {
		t.Fatalf("%d expire_invoice jobs queued, want 1", n)
	}

	if err := h.svc.ExpireInvoice(h.ctx, g.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	expired := h.status(t, g.ID, StatusExpired)
	if !h.hasAction(t, g.ID, "invoice.expired") {
		t.Errorf("timeline is missing invoice.expired: %v", h.actions(t, g.ID))
	}

	_, err := h.svc.Checkout(h.ctx, g.ID, expired.ClientToken, CheckoutInput{
		ClientName: "Tolu Adeyemi", ClientEmail: "tolu@test.local", Currency: "NGN",
	})
	assertAppCode(t, err, "invalid_state")
}

// The expiry job runs a week after the invoice was sent, by which time it has
// usually been paid. It must leave anything but an unpaid invoice alone.
func TestExpiryIsANoOpOnceTheInvoiceMovedOn(t *testing.T) {
	h := newHarness(t)

	for _, tc := range []struct {
		name  string
		setup func(*Gig)
		want  string
	}{
		{"paid", func(g *Gig) { h.pay(t, g) }, StatusBooked},
		{"cancelled", func(g *Gig) {
			if _, err := h.svc.Cancel(h.ctx, g.ID); err != nil {
				t.Fatalf("cancel: %v", err)
			}
		}, StatusCancelled},
		{"voided", func(g *Gig) {
			if _, err := h.svc.VoidInvoice(h.ctx, h.creatorUserID, g.ID); err != nil {
				t.Fatalf("void: %v", err)
			}
		}, StatusVoided},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := h.sendInvoice(t, invoiceNGN, 20)
			tc.setup(g)

			if err := h.svc.ExpireInvoice(h.ctx, g.ID); err != nil {
				t.Fatalf("expire returned an error, so the job would retry for ever: %v", err)
			}
			h.status(t, g.ID, tc.want)
			if h.hasAction(t, g.ID, "invoice.expired") {
				t.Error("the invoice was marked expired anyway")
			}
		})
	}
}

// A client who opened checkout and paid late has really sent us money, so a
// closed invoice reopens rather than the payment being absorbed.
func TestPaymentForAClosedInvoiceReopensIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(*harness, *testing.T, *Gig)
		was   string
	}{
		{"voided", func(h *harness, t *testing.T, g *Gig) {
			if _, err := h.svc.VoidInvoice(h.ctx, h.creatorUserID, g.ID); err != nil {
				t.Fatalf("void: %v", err)
			}
		}, StatusVoided},
		{"expired", func(h *harness, t *testing.T, g *Gig) {
			if err := h.svc.ExpireInvoice(h.ctx, g.ID); err != nil {
				t.Fatalf("expire: %v", err)
			}
		}, StatusExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			g := h.sendInvoice(t, invoiceNGN, 20)
			tc.close(h, t, g)
			h.status(t, g.ID, tc.was)

			h.pay(t, g)

			h.status(t, g.ID, StatusBooked)
			h.assertBalances(t, map[string]int64{
				h.escrow(g.ID):        restUSDT,
				h.worker():            upfrontUSDT,
				ledger.ExternalClient: -totalUSDT,
			})
			if !h.hasAction(t, g.ID, "invoice.reopened_by_payment") {
				t.Errorf("timeline is missing invoice.reopened_by_payment: %v", h.actions(t, g.ID))
			}
			// And it carries on as a normal booking from there.
			if _, err := h.svc.Complete(h.ctx, g.ID); err != nil {
				t.Fatalf("complete after reopening: %v", err)
			}
			h.status(t, g.ID, StatusDonePending)
		})
	}
}

// A settled invoice must not reopen: that would be a replayed webhook paying
// out twice.
func TestPaymentDoesNotReopenASettledInvoice(t *testing.T) {
	h := newHarness(t)
	g := h.sendInvoice(t, invoiceNGN, 20)
	h.pay(t, g)
	if _, err := h.svc.Complete(h.ctx, g.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	h.makeReleaseDue(t, g.ID)
	if err := h.svc.AutoRelease(h.ctx, g.ID); err != nil {
		t.Fatalf("auto release: %v", err)
	}
	h.status(t, g.ID, StatusReleased)
	before := h.balance(t, h.worker())

	if err := h.payErr(g); err != nil {
		t.Fatalf("replayed webhook: %v", err)
	}
	h.status(t, g.ID, StatusReleased)
	if got := h.balance(t, h.worker()); got != before {
		t.Errorf("worker balance changed from %d to %d on a replayed webhook", before, got)
	}
	if h.hasAction(t, g.ID, "invoice.reopened_by_payment") {
		t.Error("a released invoice was reopened")
	}
}

// ---------- invoice validation ----------

func TestInvoiceValidation(t *testing.T) {
	h := newHarness(t)
	valid := CreateGigLinkInput{
		Title: "3 reels", EventAt: time.Now().Add(72 * time.Hour),
		AmountNGN: invoiceNGN, ClientName: "Tolu Adeyemi",
	}
	pct := func(n int) *int { return &n }

	for _, tc := range []struct {
		name  string
		mutit func(*CreateGigLinkInput)
	}{
		{"no title", func(in *CreateGigLinkInput) { in.Title = "  " }},
		{"no client name", func(in *CreateGigLinkInput) { in.ClientName = "" }},
		{"no event date", func(in *CreateGigLinkInput) { in.EventAt = time.Time{} }},
		{"bad client email", func(in *CreateGigLinkInput) { in.ClientEmail = "nope" }},
		{"below the minimum", func(in *CreateGigLinkInput) { in.AmountNGN = "99" }},
		{"amount not a number", func(in *CreateGigLinkInput) { in.AmountNGN = "abc" }},
		{"upfront above the cap", func(in *CreateGigLinkInput) { in.UpfrontPercent = pct(80) }},
		{"negative upfront", func(in *CreateGigLinkInput) { in.UpfrontPercent = pct(-1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.mutit(&in)
			_, err := h.svc.CreateGigLink(h.ctx, h.workerID, in)
			assertAppCode(t, err, "validation_error")
		})
	}
}

// ---------- helpers ----------

// disputed drives an invoice to an open dispute the two sides are still
// negotiating.
func (h *harness) disputed(t *testing.T, amountNGN string) *Gig {
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
	return h.status(t, g.ID, StatusDisputed)
}

// assertAppCode checks the error carries the stable machine-readable code the
// frontend switches on, not just that something failed.
func assertAppCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want %s", want)
	}
	var ae *AppError
	if !errors.As(err, &ae) {
		t.Fatalf("got %v (%T), want an *AppError with code %s", err, err, want)
	}
	if ae.Code != want {
		t.Fatalf("got code %q (%s), want %q", ae.Code, ae.Msg, want)
	}
}
