import { useEffect, useState } from "react";
import { api, fmtDate, naira, session, usdt, type Gig } from "../api";
import { navigate } from "../router";
import { Button, Countdown, DisputePanel, ErrorNote, Field, ModeBadge, Shell, StatusChip, Timeline, useAction, usePoll } from "../ui";
import { LogoutButton } from "./Auth";

// ---------------- Onboarding: 3 steps ----------------

export function Onboard() {
  const user = session.get()?.user;
  const [step, setStep] = useState(1);
  const [f, setF] = useState({ craft: "", birth_date: "", city: "Lagos", id_type: "bvn", id_number: "", bank_code: "", bank_name: "", account_number: "" });
  const [banks, setBanks] = useState<{ name: string; code: string }[]>([]);
  const [accountName, setAccountName] = useState<string | null>(null);
  const [kycLink, setKycLink] = useState<string | null>(null);
  const { busy, err, run, setErr } = useAction();

  useEffect(() => { api.banks().then(setBanks).catch(() => {}); }, []);
  useEffect(() => {
    setAccountName(null);
    if (f.bank_code && f.account_number.length === 10) {
      api.resolveBank(f.bank_code, f.account_number).then((a) => setAccountName(a.account_name)).catch((e) => setErr(e.message));
    }
  }, [f.bank_code, f.account_number, setErr]);

  const finish = () => run(async () => {
    const r = await api.onboard(f);
    const cur = session.get();
    if (cur) session.set({ ...cur, user: { ...cur.user, worker_id: r.worker.id } });
    setKycLink(r.kyc_link);
    setStep(4);
  });

  return (
    <Shell title={`Hi ${user?.name.split(" ")[0] ?? ""}, let's get you paid`} subtitle={step < 4 ? `Step ${step} of 3` : "You're all set"} right={<LogoutButton />}>
      {step === 1 && (
        <>
          <Field label="What do you do?" placeholder="Singer · Nail tech · Dancer" value={f.craft} onChange={(v) => setF({ ...f, craft: v })} />
          <Field label="Date of birth (DD-MM-YYYY)" value={f.birth_date} onChange={(v) => setF({ ...f, birth_date: v })} placeholder="24-12-1998" />
          <Field label="City" value={f.city} onChange={(v) => setF({ ...f, city: v })} />
          <Button onClick={() => setStep(2)} disabled={!f.craft}>Continue</Button>
        </>
      )}
      {step === 2 && (
        <>
          <p className="text-sm text-stone-600">Verify once so clients know you're real. Busha checks it; we only keep the last 4 digits.</p>
          <div className="grid grid-cols-2 gap-1 rounded-lg bg-stone-100 p-1 text-sm">
            {["bvn", "nin"].map((t) => (
              <button key={t} onClick={() => setF({ ...f, id_type: t })}
                className={`rounded-md py-1.5 font-medium uppercase ${f.id_type === t ? "bg-white shadow" : "text-stone-500"}`}>{t}</button>
            ))}
          </div>
          <Field label={f.id_type.toUpperCase() + " (11 digits)"} value={f.id_number} inputMode="numeric"
            onChange={(v) => setF({ ...f, id_number: v.replace(/\D/g, "").slice(0, 11) })} />
          <Button onClick={() => setStep(3)} disabled={f.id_number.length !== 11}>Continue</Button>
          <button className="w-full text-sm text-stone-500" onClick={() => setStep(1)}>Back</button>
        </>
      )}
      {step === 3 && (
        <>
          <p className="text-sm text-stone-600">Where should we send your money?</p>
          <label className="block text-sm">
            <span className="text-stone-500">Bank</span>
            <select className="mt-1 w-full rounded-lg border border-stone-300 px-3 py-2" value={f.bank_code}
              onChange={(e) => setF({ ...f, bank_code: e.target.value, bank_name: e.target.selectedOptions[0]?.text ?? "" })}>
              <option value="">Choose your bank</option>
              {banks.map((b) => <option key={b.code} value={b.code}>{b.name}</option>)}
            </select>
          </label>
          <Field label="Account number" value={f.account_number} inputMode="numeric"
            onChange={(v) => setF({ ...f, account_number: v.replace(/\D/g, "").slice(0, 10) })} />
          {accountName && <p className="rounded-lg bg-vault-100 p-3 text-sm font-semibold text-vault-900">✓ {accountName}</p>}
          <ErrorNote msg={err} />
          <Button onClick={finish} disabled={busy || !accountName}>Finish setup</Button>
          <button className="w-full text-sm text-stone-500" onClick={() => setStep(2)}>Back</button>
        </>
      )}
      {step === 4 && (
        <>
          <p className="text-sm text-stone-600">Your bank account is saved on Busha. Complete identity verification with Busha to get the verified badge.</p>
          {kycLink && <a href={kycLink} target="_blank" rel="noreferrer" className="block rounded-xl border border-vault-500 p-3 text-center font-semibold text-vault-700">Verify my identity with Busha ↗</a>}
          <Button onClick={() => navigate("/app")}>Go to my wallet</Button>
        </>
      )}
    </Shell>
  );
}

// ---------------- Creator home: wallet + gigs ----------------

export function CreatorHome() {
  const wallet = usePoll(() => api.wallet());
  const activity = usePoll(() => api.activity(), 3000);
  const [showNew, setShowNew] = useState(false);
  const [amount, setAmount] = useState("");
  const { busy, err, run } = useAction();
  const w = wallet.data;

  if (wallet.error?.includes("finish setting up")) { navigate("/onboard"); return null; }

  return (
    <Shell title={w ? `Hi ${w.worker.name.split(" ")[0]}` : "Your wallet"} subtitle={w?.worker.craft ?? undefined}
      right={<div className="flex flex-col items-end gap-1"><ModeBadge mode={w?.busha_mode} /><LogoutButton /></div>}>
      {!w && <p className="text-stone-500">{wallet.error ?? "Loading…"}</p>}
      {w && (
        <>
          <section className="rounded-2xl bg-vault-900 p-5 text-white">
            <p className="text-sm text-white/70">You can withdraw</p>
            <p className="text-4xl font-bold">{naira(w.available_ngn_kobo)}</p>
            <p className="text-xs text-white/60">≈ {usdt(w.available_usdt_micro)} · 1 USDT ≈ {naira(w.rate_ngn_per_usdt * 100)}</p>
            <div className="mt-4 grid grid-cols-2 gap-3 text-sm">
              <div className="rounded-lg bg-white/10 p-3"><p className="text-white/70">🔒 Locked for you</p><p className="text-lg font-semibold">{naira(w.locked_ngn_kobo)}</p></div>
              <div className="rounded-lg bg-white/10 p-3"><p className="text-white/70">⏸ Frozen</p><p className="text-lg font-semibold">{naira(w.frozen_ngn_kobo)}</p></div>
            </div>
          </section>

          <Button onClick={() => setShowNew(true)}>+ New invoice</Button>

          <section className="space-y-2">
            <h3 className="font-semibold">Invoices</h3>
            {w.gigs.length === 0 && <p className="text-sm text-stone-500">No invoices yet. Agree a price with a client, then send them an invoice.</p>}
            {w.gigs.map((g) => <GigRow key={g.id} g={g} />)}
          </section>

          <section className="space-y-2">
            <h3 className="font-semibold">Cash out to {w.worker.bank_name ?? "your bank"} {w.worker.account_number ? `•••${w.worker.account_number.slice(-4)}` : ""}</h3>
            <div className="flex gap-2">
              <input className="flex-1 rounded-lg border border-stone-300 px-3 py-2" placeholder="Amount in ₦" inputMode="decimal"
                value={amount} onChange={(e) => setAmount(e.target.value)} />
              <button className="rounded-lg bg-vault-700 px-4 font-medium text-white disabled:opacity-40" disabled={!amount || busy}
                onClick={() => run(async () => { await api.cashout(amount); setAmount(""); wallet.refresh(); })}>Withdraw</button>
            </div>
            <ErrorNote msg={err} />
            {w.payouts.map((p) => (
              <div key={p.id} className="flex justify-between border-b border-stone-100 py-1 text-sm">
                <span>{naira(p.amount_ngn_kobo)}</span>
                <span className={p.status === "completed" ? "text-vault-700" : p.status === "failed" ? "text-red-600" : "text-amber-600"}>{p.status}</span>
              </div>
            ))}
          </section>
          {activity.data && activity.data.length > 0 && (
            <details className="rounded-xl border border-stone-200 p-3" open>
              <summary className="cursor-pointer font-semibold">Activity</summary>
              <div className="mt-2">
                <Timeline events={[...activity.data].reverse().slice(-15).map((e) => ({ ...e, action: e.action, actor: e.gig_title ? `${e.actor} · ${e.gig_title}` : e.actor }))} />
              </div>
            </details>
          )}
        </>
      )}
      {showNew && <NewGigModal onClose={() => { setShowNew(false); wallet.refresh(); }} />}
    </Shell>
  );
}

function GigRow({ g }: { g: Gig }) {
  return (
    <button onClick={() => navigate(`/app/gigs/${g.id}`)} className="flex w-full items-center justify-between rounded-xl border border-stone-200 p-3 text-left">
      <div>
        <p className="font-medium">{g.title}</p>
        <p className="text-xs text-stone-500">{naira(g.amount_ngn_kobo)} · {g.client_name ?? "client"} · {fmtDate(g.event_at)}</p>
        {g.status === "DONE_PENDING" && <Countdown until={g.release_at} label="Paying out in" small />}
      </div>
      <StatusChip status={g.status} />
    </button>
  );
}

// ---------------- New invoice modal: creates the link to send ----------------

const UPFRONT_CHOICES = [0, 20, 30, 50, 70];

function NewGigModal({ onClose }: { onClose: () => void }) {
  const [f, setF] = useState({ title: "", client_name: "", client_email: "", amount_ngn: "", event_at: new Date(Date.now() + 3 * 864e5).toISOString().slice(0, 16), note: "" });
  const [upfront, setUpfront] = useState(20);
  const [link, setLink] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const { busy, err, run } = useAction();
  const amount = Number(f.amount_ngn) || 0;

  const create = () => run(async () => {
    const r = await api.createGigLink({ ...f, upfront_percent: upfront, event_at: new Date(f.event_at).toISOString() });
    setLink(window.location.origin + r.pay_path);
  });
  const wa = link ? `https://wa.me/?text=${encodeURIComponent(`Hi ${f.client_name}, here's my invoice for "${f.title}" (${naira(amount * 100)}). Pay securely on GigVault: your money is held safely until the work is done. ${link}`)}` : "";

  return (
    <div className="fixed inset-0 z-10 flex items-end justify-center bg-black/40 sm:items-center" onClick={onClose}>
      <div className="max-h-[92vh] w-full max-w-md space-y-3 overflow-y-auto rounded-t-2xl bg-white p-5 sm:rounded-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold">{link ? "Invoice ready. Send it to your client" : "New invoice"}</h2>
          <button onClick={onClose} className="text-stone-400">✕</button>
        </div>
        {!link && (
          <>
            <Field label="Client name" value={f.client_name} onChange={(v) => setF({ ...f, client_name: v })} />
            <Field label="Client email (optional, it shows up in their account)" inputMode="email" value={f.client_email} onChange={(v) => setF({ ...f, client_email: v })} />
            <Field label="What is it for?" placeholder="Instagram campaign: 3 reels" value={f.title} onChange={(v) => setF({ ...f, title: v })} />
            <Field label="Amount (₦)" inputMode="decimal" value={f.amount_ngn} onChange={(v) => setF({ ...f, amount_ngn: v })} />
            <Field label="Date of the gig / delivery" type="datetime-local" value={f.event_at} onChange={(v) => setF({ ...f, event_at: v })} />
            <div className="text-sm">
              <p className="text-stone-500">Upfront to start work</p>
              <div className="mt-1 grid grid-cols-5 gap-1">
                {UPFRONT_CHOICES.map((p) => (
                  <button key={p} onClick={() => setUpfront(p)}
                    className={`rounded-lg border py-2 font-semibold ${upfront === p ? "border-vault-700 bg-vault-700 text-white" : "border-stone-300"}`}>{p}%</button>
                ))}
              </div>
              <p className="mt-1 text-xs text-stone-500">
                {amount > 0 ? `${naira(amount * upfront)} comes to you as soon as they pay. The other ${naira(amount * (100 - upfront))} is released when they confirm the work is done.` : "Paid to you the moment the client pays, for materials and transport."}
              </p>
            </div>
            <Field label="Note (optional)" value={f.note} onChange={(v) => setF({ ...f, note: v })} />
            <ErrorNote msg={err} />
            <Button onClick={create} disabled={busy || !f.title || !f.amount_ngn || !f.client_name}>Create invoice</Button>
          </>
        )}
        {link && (
          <>
            <p className="break-all rounded-lg bg-stone-100 p-3 font-mono text-xs">{link}</p>
            {f.client_email && <p className="text-xs text-stone-500">It will also appear under Pending payments when {f.client_name} logs in with {f.client_email}.</p>}
            <div className="grid grid-cols-2 gap-2">
              <Button variant="ghost" onClick={() => { navigator.clipboard?.writeText(link); setCopied(true); }}>{copied ? "Copied ✓" : "Copy link"}</Button>
              <a href={wa} target="_blank" rel="noreferrer" className="rounded-xl bg-[#25D366] px-4 py-3 text-center font-semibold text-white">WhatsApp</a>
            </div>
            <Button onClick={onClose}>Done</Button>
          </>
        )}
      </div>
    </div>
  );
}

// ---------------- Creator gig detail: who booked, timeline, ledger ----------------

export function CreatorGig({ id }: { id: string }) {
  const p = usePoll(() => api.creatorGig(id), 2000, [id]);
  const d = p.data;
  const { busy, err, run } = useAction();
  const [confirmVoid, setConfirmVoid] = useState(false);
  const dispute = d?.complaints.length ? d.complaints[d.complaints.length - 1] : null;
  return (
    <Shell title={d?.gig.title ?? "Invoice"} subtitle={d ? `${naira(d.gig.amount_ngn_kobo)} · ${d.gig.deposit_bps / 100}% upfront · ${fmtDate(d.gig.event_at)}` : undefined}
      right={<button className="text-sm text-white/80" onClick={() => navigate("/app")}>← Wallet</button>}>
      {d && (
        <>
          <div className="flex items-center justify-between">
            <div className="text-sm">
              <p className="text-stone-500">Client</p>
              <p className="font-semibold">{d.gig.client_name ?? "Not joined yet"}</p>
              {d.gig.client_email && <p className="text-xs text-stone-500">{d.gig.client_email}</p>}
            </div>
            <StatusChip status={d.gig.status} />
          </div>
          {d.gig.status === "AWAITING_PAYMENT" && d.pay_path && (
            <>
              <p className="break-all rounded-lg bg-stone-100 p-3 font-mono text-xs">{window.location.origin + d.pay_path}</p>
              {confirmVoid ? (
                <>
                  <p className="rounded-xl bg-stone-100 p-3 text-sm">
                    Withdraw this invoice? {d.gig.client_name ?? "The client"} will no longer be able to pay it, and this can't be undone.
                  </p>
                  <Button variant="ghost" disabled={busy}
                    onClick={() => run(async () => { await api.voidInvoice(id); setConfirmVoid(false); p.refresh(); })}>
                    Yes, void it
                  </Button>
                  <Button variant="ghost" disabled={busy} onClick={() => setConfirmVoid(false)}>Keep it</Button>
                </>
              ) : (
                <Button variant="ghost" disabled={busy} onClick={() => setConfirmVoid(true)}>Void this invoice</Button>
              )}
            </>
          )}
          {d.gig.status === "VOIDED" && (
            <p className="rounded-xl bg-stone-100 p-4 text-sm">You withdrew this invoice, so it can no longer be paid.</p>
          )}
          {d.gig.status === "EXPIRED" && (
            <p className="rounded-xl bg-stone-100 p-4 text-sm">This invoice expired before it was paid. Send {d.gig.client_name ?? "your client"} a new one.</p>
          )}
          {d.gig.status === "DONE_PENDING" && <Countdown until={d.gig.release_at} label="Paying out to you in" />}
          {dispute && (d.gig.status === "DISPUTED" || d.gig.status === "ESCALATED") && (
            <DisputePanel dispute={dispute} me="creator" feePercent={10} busy={busy}
              onPropose={(pct) => run(async () => { await api.creatorPropose(id, pct); p.refresh(); })}
              onAccept={() => run(async () => { await api.creatorAccept(id); p.refresh(); })} />
          )}
          <ErrorNote msg={err} />
          <Timeline events={d.audit} amountKobo={d.gig.amount_ngn_kobo} />
          <details className="text-xs">
            <summary className="cursor-pointer font-semibold">Ledger (double-entry, append-only)</summary>
            <table className="mt-1 w-full"><tbody>
              {d.ledger.map((e, i) => (
                <tr key={i} className="border-b border-stone-100">
                  <td className="py-1">{e.kind}</td>
                  <td className="font-mono">{e.account.replace(d.gig.id, "…")}</td>
                  <td className={e.direction === "credit" ? "text-vault-700" : "text-red-600"}>{e.direction}</td>
                  <td className="text-right">{usdt(e.amount_usdt_micro)}</td>
                </tr>
              ))}
            </tbody></table>
          </details>
        </>
      )}
    </Shell>
  );
}
