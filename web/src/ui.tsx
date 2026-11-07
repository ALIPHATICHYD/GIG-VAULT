import { useCallback, useEffect, useState, type ReactNode } from "react";
import { naira, usdt, type Dispute, type GigStatus, type TimelineEvent } from "./api";

export function usePoll<T>(fn: () => Promise<T>, ms = 2000, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const refresh = useCallback(() => {
    fn().then((d) => { setData(d); setError(null); }).catch((e: Error) => setError(e.message));
  }, deps);
  useEffect(() => { refresh(); const t = setInterval(refresh, ms); return () => clearInterval(t); }, [refresh, ms]);
  return { data, error, refresh };
}

export function Shell({ title, subtitle, right, children, tone = "creator" }: {
  title: string; subtitle?: string; right?: ReactNode; children: ReactNode; tone?: "creator" | "client";
}) {
  return (
    <div className="mx-auto min-h-screen max-w-md bg-white shadow-sm">
      <header className={`${tone === "creator" ? "bg-vault-900" : "bg-indigo-950"} px-5 pb-5 pt-6 text-white`}>
        <div className="flex items-center justify-between gap-2">
          <div>
            <p className="text-xs font-semibold uppercase tracking-widest text-white/60">GigVault{tone === "client" ? " · Client" : ""}</p>
            <h1 className="text-xl font-bold tracking-tight">{title}</h1>
          </div>
          {right}
        </div>
        {subtitle && <p className="mt-1 text-sm text-white/70">{subtitle}</p>}
      </header>
      <main className="space-y-5 p-5">{children}</main>
    </div>
  );
}

export function ModeBadge({ mode }: { mode?: string }) {
  if (mode === "mock") return <span className="rounded bg-amber-400 px-2 py-0.5 text-[10px] font-bold text-amber-950">MOCK BUSHA</span>;
  if (mode === "live") return <span className="rounded bg-emerald-500 px-2 py-0.5 text-[10px] font-bold">BUSHA SANDBOX</span>;
  return null;
}

const STATUS: Record<GigStatus, { label: string; cls: string }> = {
  AWAITING_PAYMENT: { label: "Awaiting payment", cls: "bg-stone-200 text-stone-700" },
  BOOKED: { label: "Locked 🔒", cls: "bg-vault-100 text-vault-900" },
  DONE_PENDING: { label: "Releasing", cls: "bg-amber-100 text-amber-800" },
  DISPUTED: { label: "In dispute", cls: "bg-red-100 text-red-700" },
  ESCALATED: { label: "GigVault reviewing", cls: "bg-red-200 text-red-900" },
  RELEASED: { label: "Paid ✓", cls: "bg-vault-500 text-white" },
  PARTIALLY_REFUNDED: { label: "Settled", cls: "bg-stone-300 text-stone-800" },
  CANCELLED: { label: "Cancelled", cls: "bg-stone-200 text-stone-500" },
};

export function StatusChip({ status }: { status: GigStatus }) {
  const s = STATUS[status];
  return <span className={`whitespace-nowrap rounded-full px-2.5 py-1 text-xs font-semibold ${s.cls}`}>{s.label}</span>;
}

export function Countdown({ until, label, small }: { until: string | null; label: string; small?: boolean }) {
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

export function Field({ label, value, onChange, type = "text", placeholder, inputMode }: {
  label: string; value: string; onChange: (v: string) => void; type?: string; placeholder?: string;
  inputMode?: "numeric" | "decimal" | "email" | "tel" | "text";
}) {
  return (
    <label className="block text-sm">
      <span className="text-stone-500">{label}</span>
      <input type={type} value={value} placeholder={placeholder} inputMode={inputMode} onChange={(e) => onChange(e.target.value)}
        className="mt-1 w-full rounded-lg border border-stone-300 px-3 py-2 focus:border-vault-500 focus:outline-none" />
    </label>
  );
}

export function Button({ children, onClick, disabled, variant = "primary", type = "button" }: {
  children: ReactNode; onClick?: () => void; disabled?: boolean; variant?: "primary" | "ghost" | "client"; type?: "button" | "submit";
}) {
  const cls = {
    primary: "bg-vault-700 text-white hover:bg-vault-900",
    client: "bg-indigo-700 text-white hover:bg-indigo-900",
    ghost: "border border-stone-300 text-stone-700 hover:bg-stone-50",
  }[variant];
  return (
    <button type={type} onClick={onClick} disabled={disabled}
      className={`w-full rounded-xl px-4 py-3 font-semibold transition disabled:opacity-40 ${cls}`}>{children}</button>
  );
}

export function ErrorNote({ msg }: { msg: string | null }) {
  return msg ? <p className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{msg}</p> : null;
}

// Runs an async action with busy + error state.
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true); setErr(null);
    try { await fn(); } catch (e) { setErr(e instanceof Error ? e.message : String(e)); } finally { setBusy(false); }
  };
  return { busy, err, run, setErr };
}

// ---------------- shared timeline ----------------

const LABELS: Record<string, string> = {
  "invoice.sent": "Invoice sent",
  "invoice.opened": "Client opened the invoice",
  "gig.claimed_by_client": "Client joined",
  "gig.checkout_started": "Busha payment request created",
  "busha.payment_confirmed": "Busha confirmed the payment",
  "gig.funded": "Money locked in escrow",
  "upfront.released": "Upfront released to the creator",
  "gig.marked_done": "Client confirmed the gig is done",
  "gig.auto_released": "Remaining money released",
  "gig.unfrozen_part_released": "Unfrozen part released",
  "gig.dispute_opened": "Dispute raised, part of the money frozen",
  "dispute.split_proposed": "Split proposed",
  "dispute.settled_by_agreement": "Dispute settled by agreement",
  "dispute.escalated_to_gigvault": "No agreement: GigVault stepped in",
  "dispute.resolved_by_gigvault": "GigVault resolved the dispute",
  "gig.cancelled": "Cancelled",
  "worker.onboarded": "Creator profile set up on Busha",
  "payout.requested": "Cash-out requested",
  "busha.transfer_created": "Busha transfer sent to the bank",
  "payout.completed": "Money delivered to the bank",
  "payout.failed_reversed": "Cash-out failed, money returned to wallet",
};

function detailLine(e: TimelineEvent, amountKobo?: number): string | null {
  const d = (e.details ?? {}) as Record<string, number | string>;
  const n = (k: string) => (Number(d[k]) > 0 ? naira(Number(d[k])) : null);
  switch (e.action) {
    case "invoice.sent": return amountKobo ? `${naira(amountKobo)} · ${d.upfront_percent}% upfront` : null;
    case "gig.checkout_started": return d.currency === "USDT" ? "Paying in USDT from abroad" : "Paying by naira bank transfer";
    case "busha.payment_confirmed": return `${n("ngn_kobo") ?? ""} received as ${usdt(d.usdt_micro as number)} · webhook ${d.event}`;
    case "gig.funded": return `${usdt(d.usdt_micro as number)} held for this invoice`;
    case "upfront.released": return `${n("ngn_kobo")} (${d.percent}%) so work can start`;
    case "gig.auto_released": case "gig.unfrozen_part_released": return n("ngn_kobo") ? `${n("ngn_kobo")} to the creator` : null;
    case "dispute.split_proposed": return `${e.actor} proposes ${d.creator_percent}% to the creator`;
    case "gig.dispute_opened": return `"${d.reason}" · ${usdt(d.frozen_usdt_micro as number)} frozen`;
    case "dispute.settled_by_agreement": case "dispute.resolved_by_gigvault":
      return `${d.creator_percent}% to creator${Number(d.fee_usdt_micro) > 0 ? ` · ${usdt(d.fee_usdt_micro as number)} resolution fee` : ""}`;
    case "payout.requested": return n("ngn_kobo");
    case "busha.transfer_created": return `${n("ngn_kobo")} · transfer ${d.transfer_id}`;
    case "payout.completed": return n("ngn_kobo");
    default: return null;
  }
}

export function Timeline({ events, amountKobo, tone = "creator" }: { events: TimelineEvent[]; amountKobo?: number; tone?: "creator" | "client" }) {
  const dot = tone === "client" ? "bg-indigo-500" : "bg-vault-500";
  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold">Timeline</h3>
      <ol className="relative space-y-3 border-l-2 border-stone-200 pl-4">
        {events.map((e, i) => {
          const extra = detailLine(e, amountKobo);
          return (
            <li key={i}>
              <span className={`absolute -left-[7px] mt-1.5 h-3 w-3 rounded-full ${dot}`} />
              <p className="text-sm font-medium">{LABELS[e.action] ?? e.action.replace(/[._]/g, " ")}</p>
              {extra && <p className="text-xs text-stone-600">{extra}</p>}
              <p className="text-xs text-stone-400">{new Date(e.created_at).toLocaleString(undefined, { day: "numeric", month: "short", hour: "numeric", minute: "2-digit", second: "2-digit" })} · {e.actor === "worker" ? "creator" : e.actor}</p>
            </li>
          );
        })}
      </ol>
    </section>
  );
}

// ---------------- shared dispute panel ----------------

export function DisputePanel({ dispute, me, feePercent, onPropose, onAccept, busy }: {
  dispute: Dispute; me: "creator" | "client"; feePercent: number; busy: boolean;
  onPropose: (pct: number) => void; onAccept: () => void;
}) {
  const [pct, setPct] = useState(dispute.proposal_creator_percent ?? 50);
  const other = me === "creator" ? "client" : "creator";
  if (dispute.status === "escalated") {
    return (
      <div className="space-y-1 rounded-xl bg-red-50 p-4 text-sm text-red-900">
        <p className="font-semibold">GigVault is reviewing this dispute</p>
        <p>You didn't agree within the window, so GigVault will decide how the {usdt(dispute.frozen_usdt_micro)} frozen is split. A {feePercent}% resolution fee comes out of the frozen amount.</p>
      </div>
    );
  }
  if (dispute.status !== "open") return null;
  const theirs = dispute.proposed_by === other ? dispute.proposal_creator_percent : null;
  return (
    <div className="space-y-3 rounded-xl border border-red-200 p-4 text-sm">
      <p className="font-semibold text-red-800">Dispute: “{dispute.reason}”</p>
      <p className="text-stone-600">{usdt(dispute.frozen_usdt_micro)} is frozen. Agree a split between you, free. If you can't, GigVault steps in and charges a {feePercent}% fee.</p>
      <Countdown until={dispute.negotiate_until} label="Time left to agree" small />
      {theirs !== null && (
        <div className="rounded-lg bg-stone-50 p-3">
          <p>The {other} proposes <b>{theirs}%</b> to the creator, <b>{100 - theirs}%</b> back to the client.</p>
          <button disabled={busy} onClick={onAccept} className="mt-2 w-full rounded-lg bg-red-700 py-2 font-semibold text-white disabled:opacity-40">Accept this split</button>
        </div>
      )}
      {dispute.proposed_by === me && <p className="text-xs text-stone-500">You proposed {dispute.proposal_creator_percent}% to the creator. Waiting for the {other}.</p>}
      <label className="block">
        <span className="text-stone-500">Propose: creator gets {pct}%, client gets {100 - pct}%</span>
        <input type="range" min={0} max={100} step={5} value={pct} onChange={(e) => setPct(Number(e.target.value))} className="mt-1 w-full" />
      </label>
      <button disabled={busy} onClick={() => onPropose(pct)} className="w-full rounded-lg border border-stone-300 py-2 font-semibold disabled:opacity-40">Send proposal</button>
    </div>
  );
}
