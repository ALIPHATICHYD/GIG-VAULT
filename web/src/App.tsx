import { useCallback, useEffect, useState } from "react";
import { api, ApiError, naira, usdt, type Gig, type GigDetail, type GigStatus, type Wallet } from "./api";

const WORKER_ID = "WRK_chidinma";
type Tab = "client" | "wallet" | "timeline";

// Polls fn every `ms` and on demand via refresh().
function usePoll<T>(fn: () => Promise<T>, ms = 2000, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const refresh = useCallback(() => {
    fn().then((d) => { setData(d); setError(null); }).catch((e: Error) => setError(e.message));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  useEffect(() => {
    refresh();
    const t = setInterval(refresh, ms);
    return () => clearInterval(t);
  }, [refresh, ms]);
  return { data, error, refresh };
}

export default function App() {
  const [tab, setTab] = useState<Tab>("client");
  const [gigId, setGigId] = useState<string | null>(null);
  const wallet = usePoll(() => api.wallet(WORKER_ID));

  return (
    <div className="mx-auto min-h-screen max-w-md bg-white shadow-sm">
      <header className="bg-vault-900 px-5 pb-4 pt-6 text-white">
        <div className="flex items-center justify-between">
          <h1 className="text-xl font-bold tracking-tight">GigVault</h1>
          {wallet.data?.busha_mode === "mock" && (
            <span className="rounded bg-amber-400 px-2 py-0.5 text-xs font-bold text-amber-950">MOCK BUSHA</span>
          )}
          {wallet.data?.busha_mode === "live" && (
            <span className="rounded bg-vault-500 px-2 py-0.5 text-xs font-bold">BUSHA SANDBOX</span>
          )}
        </div>
        <p className="text-sm text-vault-100/80">Get paid for every gig. Guaranteed.</p>
        <nav className="mt-4 grid grid-cols-3 gap-1 rounded-lg bg-vault-700 p-1 text-sm">
          {(["client", "wallet", "timeline"] as Tab[]).map((t) => (
            <button key={t} onClick={() => setTab(t)}
              className={`rounded-md py-1.5 font-medium capitalize ${tab === t ? "bg-white text-vault-900" : "text-white/80"}`}>
              {t === "client" ? "Client" : t === "wallet" ? "Chidinma" : "Timeline"}
            </button>
          ))}
        </nav>
      </header>
      <main className="p-5">
        {tab === "client" && <ClientView gigId={gigId} setGigId={setGigId} onChange={wallet.refresh} />}
        {tab === "wallet" && <WalletView wallet={wallet.data} error={wallet.error} refresh={wallet.refresh}
          openGig={(id) => { setGigId(id); setTab("timeline"); }} />}
        {tab === "timeline" && <TimelineView gigId={gigId} />}
      </main>
    </div>
  );
}

// ---------------- Client: book and pay ----------------

function ClientView({ gigId, setGigId, onChange }: { gigId: string | null; setGigId: (id: string) => void; onChange: () => void }) {
  const [form, setForm] = useState({
    title: "Saturday wedding: lead vocals",
    client_name: "Tolu Adeyemi",
    client_email: "tolu@example.com",
    event_at: new Date(Date.now() + 3 * 864e5).toISOString().slice(0, 16),
    amount: "120000",
    currency: "NGN" as "NGN" | "USDT",
  });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const detail = usePoll(() => (gigId ? api.gig(gigId) : Promise.resolve(null)), 2000, [gigId]);

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true); setErr(null);
    try { await fn(); detail.refresh(); onChange(); }
    catch (e) { setErr(e instanceof ApiError ? e.message : String(e)); }
    finally { setBusy(false); }
  };

  const book = () => run(async () => {
    const g = await api.createGig({ ...form, worker_id: WORKER_ID, event_at: new Date(form.event_at).toISOString() });
    setGigId(g.id);
  });

  const gig = detail.data?.gig;
  return (
    <div className="space-y-5">
      {!gig && (
        <section className="space-y-3">
          <h2 className="text-lg font-semibold">Book Chidinma</h2>
          <Field label="Gig" value={form.title} onChange={(v) => setForm({ ...form, title: v })} />
          <Field label="Your name" value={form.client_name} onChange={(v) => setForm({ ...form, client_name: v })} />
          <Field label="Email" value={form.client_email} onChange={(v) => setForm({ ...form, client_email: v })} />
          <Field label="Date and time" type="datetime-local" value={form.event_at} onChange={(v) => setForm({ ...form, event_at: v })} />
          <div className="flex gap-2">
            <div className="flex-1"><Field label="Amount" value={form.amount} onChange={(v) => setForm({ ...form, amount: v })} /></div>
            <label className="block text-sm">
              <span className="text-stone-500">Pay in</span>
              <select className="mt-1 w-full rounded-lg border border-stone-300 px-3 py-2" value={form.currency}
                onChange={(e) => setForm({ ...form, currency: e.target.value as "NGN" | "USDT" })}>
                <option value="NGN">₦ Naira</option>
                <option value="USDT">USDT</option>
              </select>
            </label>
          </div>
          <p className="rounded-lg bg-vault-100 p-3 text-sm text-vault-900">
            Your money is held safely until the gig is done. 25% goes to Chidinma now for materials and transport.
            You have a complaint window after the gig before the rest is released.
          </p>
          <Button onClick={book} disabled={busy}>Book and pay {form.currency === "NGN" ? naira(Number(form.amount) * 100) : form.amount + " USDT"}</Button>
        </section>
      )}

      {gig && (
        <section className="space-y-4">
          <div className="flex items-start justify-between">
            <div>
              <h2 className="text-lg font-semibold">{gig.title}</h2>
              <p className="text-sm text-stone-500">{new Date(gig.event_at).toLocaleString()}</p>
            </div>
            <StatusChip status={gig.status} />
          </div>
          <div className="rounded-xl border border-stone-200 p-4">
            <p className="text-3xl font-bold">{naira(gig.amount_ngn_kobo)}</p>
            <p className="text-sm text-stone-500">≈ {usdt(gig.amount_usdt_micro)} held on Busha</p>
          </div>

          {gig.status === "AWAITING_PAYMENT" && (
            <div className="space-y-3">
              <PaymentDetails gig={gig} />
              {import.meta.env.VITE_SHOW_DEV_TOOLS !== "false" ? (
                <Button variant="ghost" onClick={() => run(() => api.simulatePayment(gig.id))} disabled={busy}>
                  Simulate Busha payment webhook (dev)
                </Button>
              ) : null}
              <Button variant="ghost" onClick={() => run(() => api.cancel(gig.id))} disabled={busy}>Cancel booking</Button>
            </div>
          )}
          {gig.status === "BOOKED" && (
            <div className="space-y-2">
              <Button onClick={() => run(() => api.complete(gig.id))} disabled={busy}>The gig is done ✓</Button>
              <Button variant="ghost" onClick={() => run(() => api.cancel(gig.id))} disabled={busy}>Cancel gig</Button>
            </div>
          )}
          {gig.status === "DONE_PENDING" && <ComplaintBox gig={gig} busy={busy} run={run} />}
          {gig.status === "DISPUTED" && (
            <div className="space-y-2">
              <p className="text-sm text-stone-600">Complaint open: part of the payment is frozen. Settle it (demo: as the platform):</p>
              <div className="grid grid-cols-2 gap-2">
                <Button onClick={() => run(() => api.resolve(gig.id, "worker"))} disabled={busy}>Pay Chidinma</Button>
                <Button variant="ghost" onClick={() => run(() => api.resolve(gig.id, "client"))} disabled={busy}>Refund client</Button>
              </div>
            </div>
          )}
          <Button variant="ghost" onClick={() => { setGigId(""); }}>Book another gig</Button>
        </section>
      )}
      {err && <p className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{err}</p>}
    </div>
  );
}

function PaymentDetails({ gig }: { gig: Gig }) {
  const d = (gig.payment_details ?? {}) as Record<string, string>;
  return (
    <div className="rounded-xl bg-stone-50 p-4 text-sm">
      {gig.pay_in_method === "address" ? (
        <>
          <p className="font-medium">Send USDT ({d.network ?? "TRX"}) to</p>
          <p className="break-all font-mono">{d.address ?? "address pending"}</p>
        </>
      ) : (
        <>
          <p className="font-medium">Transfer to this account</p>
          <p className="font-mono text-lg">{d.account_number ?? "…"}</p>
          <p>{d.bank_name} · {d.account_name}</p>
        </>
      )}
      <p className="mt-2 text-xs text-stone-500">Powered by Busha. Confirmed automatically, any hour.</p>
    </div>
  );
}

function ComplaintBox({ gig, busy, run }: { gig: Gig; busy: boolean; run: (fn: () => Promise<unknown>) => void }) {
  const [reason, setReason] = useState("");
  return (
    <div className="space-y-2">
      <Countdown until={gig.release_at} label="Releases to Chidinma in" />
      <Field label="Something wrong? Tell us before the timer ends" value={reason} onChange={setReason} />
      <Button variant="ghost" onClick={() => run(() => api.complain(gig.id, reason))} disabled={busy || !reason}>
        Raise a complaint
      </Button>
    </div>
  );
}

// ---------------- Chidinma's wallet ----------------

function WalletView({ wallet, error, refresh, openGig }: {
  wallet: Wallet | null; error: string | null; refresh: () => void; openGig: (id: string) => void;
}) {
  const [amount, setAmount] = useState("");
  const [msg, setMsg] = useState<string | null>(null);
  if (error) return <p className="text-red-700">Can't reach the API: {error}</p>;
  if (!wallet) return <p className="text-stone-500">Loading…</p>;

  const cashout = async () => {
    setMsg(null);
    try { await api.cashout(WORKER_ID, amount); setAmount(""); setMsg("On its way to your bank."); refresh(); }
    catch (e) { setMsg(e instanceof Error ? e.message : String(e)); }
  };

  return (
    <div className="space-y-5">
      <section className="rounded-2xl bg-vault-900 p-5 text-white">
        <p className="text-sm text-white/70">Hi {wallet.worker.name.split(" ")[0]}, you can withdraw</p>
        <p className="text-4xl font-bold">{naira(wallet.available_ngn_kobo)}</p>
        <p className="text-xs text-white/60">≈ {usdt(wallet.available_usdt_micro)} · 1 USDT ≈ {naira(wallet.rate_ngn_per_usdt * 100)}</p>
        <div className="mt-4 grid grid-cols-2 gap-3 text-sm">
          <div className="rounded-lg bg-white/10 p-3">
            <p className="text-white/70">🔒 Locked for you</p>
            <p className="text-lg font-semibold">{naira(wallet.locked_ngn_kobo)}</p>
          </div>
          <div className="rounded-lg bg-white/10 p-3">
            <p className="text-white/70">⏸ Frozen by complaints</p>
            <p className="text-lg font-semibold">{naira(wallet.frozen_ngn_kobo)}</p>
          </div>
        </div>
      </section>

      <section className="space-y-2">
        <h3 className="font-semibold">Cash out to {wallet.worker.bank_name ?? "your bank"} {wallet.worker.account_number ? `•••${wallet.worker.account_number.slice(-4)}` : ""}</h3>
        <div className="flex gap-2">
          <input className="flex-1 rounded-lg border border-stone-300 px-3 py-2" placeholder="Amount in ₦"
            value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" />
          <button className="rounded-lg bg-vault-700 px-4 font-medium text-white disabled:opacity-40"
            disabled={!amount} onClick={cashout}>Withdraw</button>
        </div>
        {msg && <p className="text-sm text-stone-600">{msg}</p>}
      </section>

      <section className="space-y-2">
        <h3 className="font-semibold">Your gigs</h3>
        {wallet.gigs.length === 0 && <p className="text-sm text-stone-500">No gigs yet.</p>}
        {wallet.gigs.map((g) => (
          <button key={g.id} onClick={() => openGig(g.id)}
            className="flex w-full items-center justify-between rounded-xl border border-stone-200 p-3 text-left">
            <div>
              <p className="font-medium">{g.title}</p>
              <p className="text-xs text-stone-500">{lockedLine(g)}</p>
              {g.status === "DONE_PENDING" && <Countdown until={g.release_at} label="Paying out in" small />}
            </div>
            <StatusChip status={g.status} />
          </button>
        ))}
      </section>

      {wallet.payouts.length > 0 && (
        <section className="space-y-1 text-sm">
          <h3 className="font-semibold">Withdrawals</h3>
          {wallet.payouts.map((p) => (
            <div key={p.id} className="flex justify-between border-b border-stone-100 py-1">
              <span>{naira(p.amount_ngn_kobo)}</span>
              <span className={p.status === "completed" ? "text-vault-700" : p.status === "failed" ? "text-red-600" : "text-amber-600"}>{p.status}</span>
            </div>
          ))}
        </section>
      )}
    </div>
  );
}

function lockedLine(g: Gig) {
  const when = new Date(g.event_at).toLocaleDateString(undefined, { weekday: "long" });
  switch (g.status) {
    case "AWAITING_PAYMENT": return `${naira(g.amount_ngn_kobo)} · waiting for client to pay`;
    case "BOOKED": return `${naira(g.amount_ngn_kobo)} locked for ${when}`;
    case "RELEASED": return `${naira(g.amount_ngn_kobo)} paid to you`;
    default: return naira(g.amount_ngn_kobo);
  }
}

// ---------------- Timeline / audit ----------------

function TimelineView({ gigId }: { gigId: string | null }) {
  const detail = usePoll(() => (gigId ? api.gig(gigId) : Promise.resolve(null)), 2000, [gigId]);
  const recon = usePoll(() => api.reconciliation(), 4000);
  const d: GigDetail | null = detail.data;
  return (
    <div className="space-y-5">
      {!d && <p className="text-sm text-stone-500">Open a gig from Chidinma's wallet to see its timeline.</p>}
      {d && (
        <>
          <div className="flex items-center justify-between">
            <h2 className="font-semibold">{d.gig.title}</h2>
            <StatusChip status={d.gig.status} />
          </div>
          <ol className="relative space-y-3 border-l-2 border-vault-100 pl-4">
            {d.audit.map((a, i) => (
              <li key={i}>
                <span className="absolute -left-[7px] mt-1.5 h-3 w-3 rounded-full bg-vault-500" />
                <p className="text-sm font-medium">{a.action.replace("gig.", "").replaceAll("_", " ")}</p>
                <p className="text-xs text-stone-500">{new Date(a.created_at).toLocaleTimeString()} · by {a.actor}{a.to_status ? ` → ${a.to_status}` : ""}</p>
              </li>
            ))}
          </ol>
          <div>
            <h3 className="mb-1 text-sm font-semibold">Ledger (double-entry, append-only)</h3>
            <table className="w-full text-xs">
              <tbody>
                {d.ledger.map((e, i) => (
                  <tr key={i} className="border-b border-stone-100">
                    <td className="py-1">{e.kind}</td>
                    <td className="font-mono">{e.account.replace(d.gig.id, "…")}</td>
                    <td className={e.direction === "credit" ? "text-vault-700" : "text-red-600"}>{e.direction}</td>
                    <td className="text-right">{usdt(e.amount_usdt_micro)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {recon.data && (
        <div className="rounded-xl bg-stone-50 p-3 text-xs">
          <p className="mb-1 font-semibold">Reconciliation: our ledger vs Busha balance</p>
          <p>We owe workers + escrow: {usdt(recon.data.ledger_owed_usdt_micro as number)}</p>
          <p>Busha USDT balance: {String(recon.data.busha_usdt_total ?? recon.data.busha_error ?? "n/a")}</p>
        </div>
      )}
    </div>
  );
}

// ---------------- small UI pieces ----------------

const STATUS: Record<GigStatus, { label: string; cls: string }> = {
  AWAITING_PAYMENT: { label: "Awaiting payment", cls: "bg-stone-200 text-stone-700" },
  BOOKED: { label: "Locked 🔒", cls: "bg-vault-100 text-vault-900" },
  DONE_PENDING: { label: "Releasing", cls: "bg-amber-100 text-amber-800" },
  DISPUTED: { label: "Frozen", cls: "bg-red-100 text-red-700" },
  RELEASED: { label: "Paid ✓", cls: "bg-vault-500 text-white" },
  PARTIALLY_REFUNDED: { label: "Settled", cls: "bg-stone-300 text-stone-800" },
  CANCELLED: { label: "Cancelled", cls: "bg-stone-200 text-stone-500" },
};

function StatusChip({ status }: { status: GigStatus }) {
  const s = STATUS[status];
  return <span className={`whitespace-nowrap rounded-full px-2.5 py-1 text-xs font-semibold ${s.cls}`}>{s.label}</span>;
}

function Countdown({ until, label, small }: { until: string | null; label: string; small?: boolean }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => { const t = setInterval(() => setNow(Date.now()), 250); return () => clearInterval(t); }, []);
  if (!until) return null;
  const ms = Math.max(0, new Date(until).getTime() - now);
  const h = Math.floor(ms / 3.6e6), m = Math.floor((ms % 3.6e6) / 6e4), s = Math.floor((ms % 6e4) / 1000);
  const text = `${h > 0 ? h + ":" : ""}${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  return small
    ? <p className="text-xs font-medium text-amber-700">{label} {text}</p>
    : <div className="rounded-xl bg-amber-50 p-4 text-center">
        <p className="text-sm text-amber-800">{label}</p>
        <p className="font-mono text-4xl font-bold text-amber-900">{text}</p>
      </div>;
}

function Field({ label, value, onChange, type = "text" }: { label: string; value: string; onChange: (v: string) => void; type?: string }) {
  return (
    <label className="block text-sm">
      <span className="text-stone-500">{label}</span>
      <input type={type} value={value} onChange={(e) => onChange(e.target.value)}
        className="mt-1 w-full rounded-lg border border-stone-300 px-3 py-2 focus:border-vault-500 focus:outline-none" />
    </label>
  );
}

function Button({ children, onClick, disabled, variant = "primary" }: {
  children: React.ReactNode; onClick: () => void; disabled?: boolean; variant?: "primary" | "ghost";
}) {
  const cls = variant === "primary"
    ? "bg-vault-700 text-white hover:bg-vault-900"
    : "border border-stone-300 text-stone-700 hover:bg-stone-50";
  return (
    <button onClick={onClick} disabled={disabled}
      className={`w-full rounded-xl px-4 py-3 font-semibold transition disabled:opacity-40 ${cls}`}>
      {children}
    </button>
  );
}
