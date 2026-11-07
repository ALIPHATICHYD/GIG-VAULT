import { useCallback, useEffect, useState, type ReactNode } from "react";
import { naira, usdt, type Dispute, type GigStatus, type TimelineEvent } from "./api";
import { navigate } from "./router";
import { useTheme } from "./theme";
import { DIcon, Icon, Strip, type IconName } from "./brand";

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

export function useNow(ms = 250) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => { const t = setInterval(() => setNow(Date.now()), ms); return () => clearInterval(t); }, [ms]);
  return now;
}

// ---------------- layout ----------------

/** One phone-width screen. `bottom` is pinned above the fold (main buttons), `nav` is the pill tab bar. */
export function Screen({ children, bottom, nav }: { children: ReactNode; bottom?: ReactNode; nav?: ReactNode }) {
  return (
    <div className="mx-auto flex min-h-dvh max-w-[440px] flex-col">
      <main className="gv-up flex flex-1 flex-col gap-[22px] px-[18px] pt-5" style={{ paddingBottom: nav ? 120 : bottom ? 24 : 32 }}>
        {children}
      </main>
      {bottom && <div className="sticky bottom-0 z-10 flex flex-col items-center gap-3 bg-gradient-to-t from-bg via-bg to-transparent px-[18px] pb-7 pt-6">{bottom}</div>}
      {nav}
    </div>
  );
}

export function RoundButton({ icon, label, onClick, dot }: { icon: IconName; label: string; onClick?: () => void; dot?: boolean }) {
  return (
    <button type="button" aria-label={label} title={label} onClick={onClick}
      className="relative flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-card text-ink">
      <Icon name={icon} />
      {dot && <span className="absolute right-3 top-[11px] h-2 w-2 rounded-full border-2 border-card bg-orange-500" />}
    </button>
  );
}

export function ThemeToggle() {
  const { theme, toggle } = useTheme();
  return <RoundButton icon={theme === "dark" ? "sun" : "moon"} label={theme === "dark" ? "Light mode" : "Dark mode"} onClick={toggle} />;
}

export function TopBar({ title, back, right }: { title: string; back?: string | (() => void); right?: ReactNode }) {
  const goBack = () => (typeof back === "function" ? back() : back ? navigate(back) : history.back());
  return (
    <div className="flex items-center justify-between gap-2">
      {back !== undefined ? <RoundButton icon="back" label="Back" onClick={goBack} /> : <span className="w-11" />}
      <h1 className="truncate text-[17px] font-extrabold">{title}</h1>
      {right ?? <span className="w-11" />}
    </div>
  );
}

export function Card({ children, className = "", pad = "p-[18px]" }: { children: ReactNode; className?: string; pad?: string }) {
  return <div className={`rounded-[22px] bg-card ${pad} ${className}`}>{children}</div>;
}

/** Deep green card with the lime dither strip: where the money lives. */
export function Hero({ children, className = "", strip = [150, 200] }: { children: ReactNode; className?: string; strip?: [number, number] | null }) {
  return (
    <div className={`relative overflow-hidden rounded-[24px] border border-[var(--hero-line)] bg-hero p-5 text-white ${className}`}>
      {strip && <Strip w={strip[0]} h={strip[1]} />}
      <div className="relative">{children}</div>
    </div>
  );
}

export function SectionTitle({ children, right }: { children: ReactNode; right?: ReactNode }) {
  return <div className="flex items-baseline justify-between"><h2 className="text-lg font-extrabold">{children}</h2>{right}</div>;
}

// ---------------- controls ----------------

type BtnVariant = "primary" | "secondary" | "lime" | "outlineLight" | "danger" | "soft";
export function Btn({ children, onClick, disabled, variant = "primary", icon, full = true, small, type = "button" }: {
  children: ReactNode; onClick?: () => void; disabled?: boolean; variant?: BtnVariant; icon?: IconName; full?: boolean; small?: boolean; type?: "button" | "submit";
}) {
  const cls: Record<BtnVariant, string> = {
    primary: "bg-pbg text-pfg",
    secondary: "bg-card text-ink border border-line",
    lime: "bg-lime text-dg",
    outlineLight: "bg-transparent text-white border border-white/35",
    danger: "bg-transparent text-red border border-red",
    soft: "bg-soft text-acc",
  };
  return (
    <button type={type} onClick={onClick} disabled={disabled}
      className={`${full ? "w-full" : ""} ${small ? "px-3.5 py-2 text-[13px]" : "px-[22px] py-[16px] text-base"} inline-flex items-center justify-center gap-2 rounded-full font-extrabold transition active:scale-[.98] disabled:opacity-40 ${cls[variant]}`}>
      {icon && <Icon name={icon} size={small ? 16 : 19} sw={1.9} />}{children}
    </button>
  );
}

export function Field({ label, value, onChange, type = "text", placeholder, inputMode, optional, mono, hint }: {
  label: string; value: string; onChange: (v: string) => void; type?: string; placeholder?: string;
  inputMode?: "numeric" | "decimal" | "email" | "tel" | "text"; optional?: boolean; mono?: boolean; hint?: ReactNode;
}) {
  return (
    <label className="flex flex-col gap-2">
      <span className="text-[13px] font-bold">{label}{optional && <span className="font-medium text-mu"> (optional)</span>}</span>
      <input type={type} value={value} placeholder={placeholder} inputMode={inputMode} onChange={(e) => onChange(e.target.value)}
        className={`w-full rounded-2xl border border-line bg-card px-4 py-[15px] text-base font-semibold text-ink outline-none placeholder:font-medium placeholder:text-mu/70 focus:border-acc ${mono ? "tracking-[0.06em]" : ""}`} />
      {hint}
    </label>
  );
}

export function Seg<T extends string>({ options, value, onChange }: { options: { value: T; label: string }[]; value: T; onChange: (v: T) => void }) {
  return (
    <div className="flex rounded-full bg-circ p-1" role="tablist">
      {options.map((o) => (
        <button key={o.value} type="button" role="tab" aria-selected={value === o.value} onClick={() => onChange(o.value)}
          className={`h-11 flex-1 rounded-full text-sm transition ${value === o.value ? "bg-pbg font-extrabold text-pfg" : "font-bold text-mu"}`}>{o.label}</button>
      ))}
    </div>
  );
}

export function Note({ tone = "amb", icon = "clock", children }: { tone?: "amb" | "red" | "soft"; icon?: IconName; children: ReactNode }) {
  const cls = { amb: "bg-amb-bg text-amb", red: "bg-red-bg text-red", soft: "bg-soft text-acc" }[tone];
  return <div className={`flex items-center gap-2 rounded-2xl px-3.5 py-3 text-[13px] font-bold leading-snug ${cls}`}><Icon name={icon} size={18} sw={1.8} /><span>{children}</span></div>;
}

export function ErrorNote({ msg }: { msg: string | null }) {
  return msg ? <Note tone="red" icon="alert">{msg}</Note> : null;
}

export function KV({ k, v, strong, accent }: { k: ReactNode; v: ReactNode; strong?: boolean; accent?: boolean }) {
  return (
    <div className="flex justify-between gap-3 border-t border-line py-2.5 text-sm first:border-t-0">
      <span className="text-mu">{k}</span>
      <span className={`text-right ${strong ? "font-extrabold" : "font-bold"} ${accent ? "text-acc" : ""}`}>{v}</span>
    </div>
  );
}

// ---------------- money ----------------

export function Money({ kobo, size = 34, className = "", light }: { kobo: number; size?: number; className?: string; light?: boolean }) {
  const [whole, dec] = (kobo / 100).toLocaleString("en-NG", { minimumFractionDigits: 2, maximumFractionDigits: 2 }).split(".");
  return (
    <div className={`font-extrabold leading-tight tracking-[-0.03em] ${className}`} style={{ fontSize: size }}>
      ₦{whole}<span className={`font-normal ${light ? "text-white/55" : "text-mu/70"}`}>.{dec}</span>
    </div>
  );
}

// ---------------- status ----------------

const STATUS: Record<GigStatus, { label: string; kind: "pending" | "held" | "rel" | "disp" | "paid" | "off"; icon: IconName }> = {
  AWAITING_PAYMENT: { label: "Awaiting payment", kind: "pending", icon: "clock" },
  BOOKED: { label: "Held", kind: "held", icon: "lock" },
  DONE_PENDING: { label: "Releasing", kind: "rel", icon: "clock" },
  DISPUTED: { label: "Disputed", kind: "disp", icon: "alert" },
  ESCALATED: { label: "GigVault deciding", kind: "disp", icon: "alert" },
  RELEASED: { label: "Paid", kind: "paid", icon: "check" },
  PARTIALLY_REFUNDED: { label: "Settled", kind: "off", icon: "check" },
  CANCELLED: { label: "Cancelled", kind: "off", icon: "x" },
};
const CHIP = { pending: "bg-circ text-mu", held: "bg-soft text-acc", rel: "bg-amb-bg text-amb", disp: "bg-red-bg text-red", paid: "bg-lime text-dg", off: "bg-circ text-mu" };

export function StatusChip({ status }: { status: GigStatus }) {
  const s = STATUS[status];
  return (
    <span className={`inline-flex items-center gap-[5px] whitespace-nowrap rounded-full px-[11px] py-1.5 text-xs font-bold ${CHIP[s.kind]}`}>
      <Icon name={s.icon} size={13} sw={2} />{s.label}
    </span>
  );
}

export function splitTime(until: string | null, now: number) {
  const ms = until ? Math.max(0, new Date(until).getTime() - now) : 0;
  return { ms, h: Math.floor(ms / 3.6e6), m: Math.floor((ms % 3.6e6) / 6e4), s: Math.floor((ms % 6e4) / 1000) };
}
const pad = (n: number) => String(n).padStart(2, "0");

/** Lime digit boxes, for dark cards. */
export function CountdownBoxes({ until }: { until: string | null }) {
  const now = useNow();
  const t = splitTime(until, now);
  return (
    <div className="flex gap-2" aria-live="polite">
      {([[t.h, "hours"], [t.m, "mins"], [t.s, "secs"]] as const).map(([v, u], i) => (
        <div key={u} className="rounded-[14px] bg-white/10 px-3 py-2 text-center">
          <div className={`text-[30px] font-extrabold leading-[1.1] text-lime ${i === 2 ? "gv-tick" : ""}`}>{pad(v)}</div>
          <div className="text-[11px] text-sage">{u}</div>
        </div>
      ))}
    </div>
  );
}

export function CountdownText({ until }: { until: string | null }) {
  const now = useNow();
  const t = splitTime(until, now);
  return <span className="tabular-nums">{t.h > 0 ? `${t.h}h ` : ""}{pad(t.m)}:{pad(t.s)}</span>;
}

// ---------------- rows ----------------

export function Row({ icon, title, sub, amount, amountClass = "", onClick, accent, right }: {
  icon?: IconName; title: ReactNode; sub?: ReactNode; amount?: ReactNode; amountClass?: string; onClick?: () => void; accent?: string; right?: ReactNode;
}) {
  const body = (
    <>
      {icon && <DIcon name={icon} accent={accent} />}
      <div className="min-w-0 flex-1 text-left">
        <div className="truncate text-[15px] font-bold">{title}</div>
        {sub && <div className="truncate text-xs text-mu">{sub}</div>}
      </div>
      {amount !== undefined && <div className={`whitespace-nowrap text-[15px] font-extrabold ${amountClass}`}>{amount}</div>}
      {right}
    </>
  );
  return onClick
    ? <button type="button" onClick={onClick} className="flex w-full items-center gap-3 py-[11px]">{body}</button>
    : <div className="flex items-center gap-3 py-[11px]">{body}</div>;
}

// ---------------- bottom nav ----------------

export function BottomNav({ items, active }: { items: { label: string; icon: IconName; to: string }[]; active: string }) {
  return (
    <nav className="fixed inset-x-0 bottom-[22px] z-20 mx-auto flex h-[66px] w-[calc(100%-36px)] max-w-[404px] items-center rounded-full border border-[var(--hero-line)] bg-card px-2.5"
      style={{ boxShadow: "var(--nav-shadow)" }}>
      {items.map((n) => {
        const on = n.label === active;
        return (
          <button key={n.label} type="button" onClick={() => navigate(n.to)} aria-current={on ? "page" : undefined}
            className={`flex flex-1 flex-col items-center gap-[3px] text-[11px] ${on ? "font-extrabold text-dg" : "font-medium text-mu"}`}>
            <span className={`flex h-[30px] w-10 items-center justify-center rounded-full ${on ? "bg-lime text-dg" : ""}`}><Icon name={n.icon} size={21} /></span>
            <span className={on ? "text-ink" : ""}>{n.label}</span>
          </button>
        );
      })}
    </nav>
  );
}

// ---------------- shared timeline ----------------

const LABELS: Record<string, string> = {
  "invoice.sent": "Invoice sent",
  "invoice.opened": "Client opened the invoice",
  "gig.claimed_by_client": "Client joined",
  "gig.checkout_started": "Busha payment request created",
  "busha.payment_confirmed": "Busha confirmed the payment",
  "gig.funded": "Held by Busha for this gig",
  "upfront.released": "Upfront part paid out",
  "gig.marked_done": "Client confirmed done",
  "gig.auto_released": "Rest released",
  "gig.unfrozen_part_released": "Unfrozen part released",
  "gig.dispute_opened": "Issue raised, part frozen",
  "dispute.split_proposed": "Split proposed",
  "dispute.settled_by_agreement": "Settled by agreement",
  "dispute.escalated_to_gigvault": "GigVault stepped in",
  "dispute.resolved_by_gigvault": "GigVault decided",
  "gig.cancelled": "Cancelled",
  "worker.onboarded": "Payout account set up on Busha",
  "payout.requested": "Cash-out requested",
  "busha.transfer_created": "Busha transfer sent",
  "payout.completed": "Money landed in the bank",
  "payout.failed_reversed": "Cash-out failed, money returned",
};

function detailLine(e: TimelineEvent, amountKobo?: number): string | null {
  const d = (e.details ?? {}) as Record<string, number | string | boolean>;
  const n = (k: string) => (Number(d[k]) > 0 ? naira(Number(d[k])) : null);
  switch (e.action) {
    case "invoice.sent": return amountKobo ? `${naira(amountKobo)} · ${d.upfront_percent}% upfront` : null;
    case "gig.checkout_started": return d.currency === "USDT" ? "Paying in USDT from abroad" : "Paying by naira bank transfer";
    case "busha.payment_confirmed": return `${n("ngn_kobo") ?? ""} received as ${usdt(d.usdt_micro as number)}`;
    case "gig.funded": return `${usdt(d.usdt_micro as number)}, locked to this gig`;
    case "upfront.released": return `${n("ngn_kobo")} (${d.percent}%) so work can start`;
    case "gig.auto_released": case "gig.unfrozen_part_released": return n("ngn_kobo") ? `${n("ngn_kobo")} to the creator` : null;
    case "dispute.split_proposed": return `${e.actor === "worker" ? "creator" : e.actor} proposes ${d.creator_percent}% to the creator`;
    case "gig.dispute_opened": return `"${d.reason}" · ${usdt(d.frozen_usdt_micro as number)} frozen`;
    case "dispute.settled_by_agreement": case "dispute.resolved_by_gigvault":
      return `${d.creator_percent}% to creator${Number(d.fee_usdt_micro) > 0 ? ` · ${usdt(d.fee_usdt_micro as number)} fee` : " · no fee"}`;
    case "gig.cancelled": return d.late ? "Late cancel: the creator keeps the upfront part" : d.refunded_usdt_micro ? `${usdt(d.refunded_usdt_micro as number)} refunded` : null;
    case "payout.requested": case "payout.completed": return n("ngn_kobo");
    case "busha.transfer_created": return `${n("ngn_kobo")} · ${d.transfer_id}`;
    default: return null;
  }
}

export type Step = { title: string; sub?: string | null; when?: string; state: "done" | "now" | "todo" | "bad" };

export function eventsToSteps(events: TimelineEvent[], amountKobo?: number): Step[] {
  return events.map((e) => ({
    title: LABELS[e.action] ?? e.action.replace(/[._]/g, " "),
    sub: detailLine(e, amountKobo),
    when: new Date(e.created_at).toLocaleString(undefined, { day: "numeric", month: "short", hour: "numeric", minute: "2-digit" }),
    state: e.action === "gig.dispute_opened" ? "bad" : "done",
  }));
}

export function Steps({ steps }: { steps: Step[] }) {
  return (
    <ol>
      {steps.map((s, i) => {
        const last = i === steps.length - 1;
        const dot = {
          done: "bg-acc border-2 border-acc",
          now: "bg-lime border-2 border-dg gv-pulse",
          todo: "bg-transparent border-2 border-line",
          bad: "bg-[#EF4444] border-2 border-[#EF4444]",
        }[s.state];
        return (
          <li key={i} className="relative flex gap-3" style={{ paddingBottom: last ? 0 : 13 }}>
            {!last && <span className={`absolute bottom-[-2px] left-[6px] top-[18px] w-0.5 ${s.state === "done" ? "bg-acc" : "bg-line"}`} />}
            <span className={`mt-[3px] h-3.5 w-3.5 shrink-0 rounded-full ${dot}`} />
            <div className="flex min-w-0 flex-1 justify-between gap-2">
              <div className="min-w-0">
                <div className={`text-sm font-bold ${s.state === "todo" ? "text-mu" : ""}`}>{s.title}</div>
                {s.sub && <div className="text-xs text-mu">{s.sub}</div>}
              </div>
              {s.when && <div className="whitespace-nowrap text-xs text-mu">{s.when}</div>}
            </div>
          </li>
        );
      })}
    </ol>
  );
}

export function Timeline({ events, amountKobo, extra = [] }: { events: TimelineEvent[]; amountKobo?: number; extra?: Step[] }) {
  return (
    <section>
      <h2 className="text-lg font-extrabold">Timeline</h2>
      <div className="mt-3.5"><Steps steps={[...eventsToSteps(events, amountKobo), ...extra]} /></div>
    </section>
  );
}

// ---------------- shared dispute panel ----------------

export function DisputePanel({ dispute, me, otherName, feePercent, onPropose, onAccept, busy }: {
  dispute: Dispute; me: "creator" | "client"; otherName: string; feePercent: number; busy: boolean;
  onPropose: (pct: number) => void; onAccept: () => void;
}) {
  const [pct, setPct] = useState(dispute.proposal_creator_percent ?? 50);
  const frozenNgnHint = usdt(dispute.frozen_usdt_micro);
  if (dispute.status === "escalated") {
    return (
      <Hero strip={[110, 120]}>
        <div className="flex items-center gap-3"><span className="text-[15px] font-extrabold">GigVault is deciding</span></div>
        <p className="mt-2 text-[13px] leading-relaxed text-sage">You didn't agree in time, so GigVault will split the {frozenNgnHint} frozen. A {feePercent}% fee comes out of the frozen part.</p>
      </Hero>
    );
  }
  if (dispute.status !== "open") return null;
  const other = me === "creator" ? "client" : "creator";
  const theirs = dispute.proposed_by === other ? dispute.proposal_creator_percent : null;
  const mine = me === "creator" ? pct : 100 - pct;
  return (
    <Hero strip={[110, 120]}>
      <div className="flex flex-col gap-3">
        <div>
          <div className="text-[15px] font-extrabold">{frozenNgnHint} frozen</div>
          <div className="text-[13px] font-bold text-lime">
            <CountdownText until={dispute.negotiate_until} /> to agree for free
          </div>
          <div className="mt-1 text-[13px] text-sage">“{dispute.reason}”</div>
        </div>
        {theirs !== null && (
          <div className="text-[13px] text-sage">
            {otherName} suggests the creator keeps <b className="text-white">{theirs}%</b> of the frozen part.
          </div>
        )}
        <div className="relative h-[26px]">
          <input aria-label="Creator's share of the frozen part" type="range" min={0} max={100} step={5} value={pct} onChange={(e) => setPct(Number(e.target.value))}
            className="absolute inset-0 z-10 w-full cursor-pointer opacity-0" />
          <div className="absolute inset-x-0 top-[11px] h-1 rounded-full bg-white/20" />
          <div className="absolute left-0 top-[11px] h-1 rounded-full bg-lime" style={{ width: `${pct}%` }} />
          <div className="absolute top-0 h-[26px] w-[26px] rounded-full bg-lime" style={{ left: `calc(${pct}% - 13px)` }} />
        </div>
        <div className="flex justify-between text-xs text-sage">
          <span>{me === "creator" ? "You" : "Creator"} {pct}%</span><span>{me === "client" ? "You" : "Client"} {100 - pct}%</span>
        </div>
        <div className="flex gap-2">
          {theirs !== null
            ? <Btn variant="lime" small disabled={busy} onClick={onAccept}>Accept {theirs}/{100 - theirs}</Btn>
            : null}
          <Btn variant={theirs !== null ? "outlineLight" : "lime"} small disabled={busy} onClick={() => onPropose(pct)}>
            {theirs !== null ? "Counter" : `Propose ${mine}% for you`}
          </Btn>
        </div>
        {dispute.proposed_by === me && <div className="text-xs text-sage">You proposed {dispute.proposal_creator_percent}% to the creator. Waiting for the {other}.</div>}
        <div className="text-xs text-sage">If you can't agree, GigVault decides and keeps {feePercent}%.</div>
      </div>
    </Hero>
  );
}
