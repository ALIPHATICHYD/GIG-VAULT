import { useCallback, useEffect, useState, type ReactNode } from "react";
import type { GigStatus } from "./api";

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
  DISPUTED: { label: "Frozen", cls: "bg-red-100 text-red-700" },
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
