import { useEffect, useState } from "react";
import { api, ApiError, fmtDate, naira, session, usdt, type PublicGig } from "../api";
import { navigate } from "../router";
import { Button, Countdown, ErrorNote, Field, ModeBadge, Shell, StatusChip, useAction, usePoll } from "../ui";
import { LogoutButton } from "./Auth";

const SHOW_DEV_TOOLS = import.meta.env.VITE_SHOW_DEV_TOOLS !== "false";

// ---------------- /pay/:id?t= : the link the creator sends ----------------

export function PayLink({ id, token }: { id: string; token: string }) {
  const [gig, setGig] = useState<PublicGig | null>(null);
  const { err, run, setErr } = useAction();
  const s = session.get();
  const here = `/pay/${id}?t=${encodeURIComponent(token)}`;

  useEffect(() => { api.preview(id, token).then(setGig).catch((e: Error) => setErr(e.message)); }, [id, token, setErr]);

  // A logged-in client joins the gig straight away and goes to the booking.
  useEffect(() => {
    if (s?.user.role === "client") {
      run(async () => {
        try { await api.claim(id, token); navigate(`/bookings/${id}`); }
        catch (e) { if (!(e instanceof ApiError && e.code === "already_claimed")) throw e; throw new Error("This gig was already booked by someone else."); }
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, token]);

  return (
    <Shell tone="client" title={gig ? `Book ${gig.creator.name}` : "Gig booking"} subtitle={gig?.creator.craft ?? undefined} right={<ModeBadge mode={gig?.busha_mode} />}>
      <ErrorNote msg={err} />
      {gig && <GigSummary gig={gig} />}
      {gig && !s && (
        <>
          <Button variant="client" onClick={() => navigate(`/signup?role=client&next=${encodeURIComponent(here)}`)}>Create an account to book</Button>
          <Button variant="ghost" onClick={() => navigate(`/login?next=${encodeURIComponent(here)}`)}>I already have an account</Button>
        </>
      )}
      {gig && s?.user.role === "creator" && (
        <p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">You're logged in as a creator. Log out and log in with a client account to book this gig.</p>
      )}
    </Shell>
  );
}

function GigSummary({ gig }: { gig: PublicGig }) {
  return (
    <>
      <div className="rounded-xl border border-stone-200 p-4">
        <p className="text-sm text-stone-500">{gig.title}</p>
        <p className="text-3xl font-bold">{naira(gig.amount_ngn_kobo)}</p>
        <p className="text-sm text-stone-500">{fmtDate(gig.event_at)}</p>
        {gig.note && <p className="mt-2 text-sm">“{gig.note}”</p>}
        <p className="mt-3 text-sm">
          with <span className="font-semibold">{gig.creator.name}</span>
          {gig.creator.verified && <span className="ml-1 rounded bg-indigo-100 px-1.5 text-xs font-semibold text-indigo-800">✓ Verified</span>}
        </p>
      </div>
      <ul className="space-y-1 rounded-xl bg-indigo-50 p-4 text-sm text-indigo-950">
        <li>🔒 Your money is held safely on Busha, not sent straight to the creator.</li>
        <li>💸 {gig.deposit_percent}% goes to {gig.creator.name.split(" ")[0]} at booking for materials and transport.</li>
        <li>⏱ After the gig you have {windowText(gig.complaint_window_seconds)} to raise a problem before the rest is released.</li>
        <li>⚠️ A complaint freezes {gig.freeze_percent}% of what's left until it's settled.</li>
      </ul>
    </>
  );
}

const windowText = (s: number) => (s >= 3600 ? `${Math.round(s / 3600)} hours` : s >= 60 ? `${Math.round(s / 60)} minutes` : `${s} seconds`);

// ---------------- /bookings ----------------

export function Bookings() {
  const b = usePoll(() => api.bookings(), 3000);
  const user = session.get()?.user;
  return (
    <Shell tone="client" title="My bookings" subtitle={user?.name} right={<LogoutButton />}>
      {b.error && <ErrorNote msg={b.error} />}
      {b.data?.length === 0 && <p className="text-sm text-stone-500">No bookings yet. Open a gig link a creator sent you to book them.</p>}
      {b.data?.map((g) => (
        <button key={g.id} onClick={() => navigate(`/bookings/${g.id}`)} className="flex w-full items-center justify-between rounded-xl border border-stone-200 p-3 text-left">
          <div>
            <p className="font-medium">{g.title}</p>
            <p className="text-xs text-stone-500">{g.creator.name} · {naira(g.amount_ngn_kobo)} · {fmtDate(g.event_at)}</p>
          </div>
          <StatusChip status={g.status} />
        </button>
      ))}
    </Shell>
  );
}

// ---------------- /bookings/:id ----------------

export function Booking({ id }: { id: string }) {
  const g = usePoll(() => api.booking(id), 2000, [id]);
  const { busy, err, run } = useAction();
  const [currency, setCurrency] = useState<"NGN" | "USDT">("NGN");
  const [reason, setReason] = useState("");
  const gig = g.data;
  const act = (fn: () => Promise<unknown>) => run(async () => { await fn(); g.refresh(); });

  return (
    <Shell tone="client" title={gig?.title ?? "Booking"} subtitle={gig ? `with ${gig.creator.name}` : undefined}
      right={<button className="text-sm text-white/80" onClick={() => navigate("/bookings")}>← Bookings</button>}>
      {g.error && <ErrorNote msg={g.error} />}
      {gig && (
        <>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-3xl font-bold">{naira(gig.amount_ngn_kobo)}</p>
              {gig.amount_usdt_micro && <p className="text-sm text-stone-500">≈ {usdt(gig.amount_usdt_micro)}</p>}
            </div>
            <StatusChip status={gig.status} />
          </div>

          {gig.status === "AWAITING_PAYMENT" && !gig.payment_details && (
            <>
              <GigSummary gig={gig} />
              <div className="grid grid-cols-2 gap-1 rounded-lg bg-stone-100 p-1 text-sm">
                {(["NGN", "USDT"] as const).map((c) => (
                  <button key={c} onClick={() => setCurrency(c)} className={`rounded-md py-1.5 font-medium ${currency === c ? "bg-white shadow" : "text-stone-500"}`}>
                    {c === "NGN" ? "₦ Bank transfer" : "USDT (abroad)"}
                  </button>
                ))}
              </div>
              <Button variant="client" disabled={busy} onClick={() => act(() => api.checkout(id, currency))}>Pay {naira(gig.amount_ngn_kobo)} securely</Button>
            </>
          )}

          {gig.status === "AWAITING_PAYMENT" && gig.payment_details && (
            <>
              <PaymentDetails gig={gig} />
              <p className="text-center text-sm text-stone-500">Waiting for Busha to confirm your payment…</p>
              {SHOW_DEV_TOOLS && <Button variant="ghost" disabled={busy} onClick={() => act(() => api.simulatePayment(id))}>Simulate Busha payment webhook (dev)</Button>}
            </>
          )}

          {gig.status === "BOOKED" && (
            <>
              <p className="rounded-xl bg-indigo-50 p-4 text-sm text-indigo-950">
                Payment received and locked. {gig.creator.name.split(" ")[0]} has the {gig.deposit_percent}% deposit. Tap below when the gig is done.
              </p>
              <Button variant="client" disabled={busy} onClick={() => act(() => api.complete(id))}>The gig is done ✓</Button>
              <Button variant="ghost" disabled={busy} onClick={() => act(() => api.cancel(id))}>Cancel booking</Button>
            </>
          )}

          {gig.status === "DONE_PENDING" && (
            <>
              <Countdown until={gig.release_at} label={`Releases to ${gig.creator.name.split(" ")[0]} in`} />
              <Field label="Something wrong? Tell us before the timer ends" value={reason} onChange={setReason} />
              <Button variant="ghost" disabled={busy || !reason} onClick={() => act(() => api.complain(id, reason))}>Raise a complaint</Button>
            </>
          )}

          {gig.status === "DISPUTED" && <p className="rounded-xl bg-red-50 p-4 text-sm text-red-800">Your complaint is open. Part of the payment is frozen while GigVault reviews it.</p>}
          {gig.status === "RELEASED" && <p className="rounded-xl bg-vault-100 p-4 text-sm text-vault-900">All done. {gig.creator.name.split(" ")[0]} has been paid. Thanks for booking safely.</p>}
          {gig.status === "CANCELLED" && <p className="rounded-xl bg-stone-100 p-4 text-sm">This booking was cancelled.</p>}
          <ErrorNote msg={err} />
        </>
      )}
    </Shell>
  );
}

function PaymentDetails({ gig }: { gig: PublicGig }) {
  const d = gig.payment_details ?? {};
  return (
    <div className="rounded-xl bg-stone-50 p-4 text-sm">
      {gig.pay_in_method === "address" ? (
        <>
          <p className="font-medium">Send {usdt(gig.amount_usdt_micro)} on {d.network ?? "TRX"} to</p>
          <p className="break-all font-mono">{d.address ?? "address pending"}</p>
        </>
      ) : (
        <>
          <p className="font-medium">Transfer {naira(gig.amount_ngn_kobo)} to</p>
          <p className="font-mono text-2xl">{d.account_number ?? "…"}</p>
          <p>{d.bank_name} · {d.account_name}</p>
        </>
      )}
      <p className="mt-2 text-xs text-stone-500">Powered by Busha. Confirmed automatically, any hour.</p>
    </div>
  );
}
