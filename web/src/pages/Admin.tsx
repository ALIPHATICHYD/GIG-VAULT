import { useState, type ReactNode } from "react";
import { api, fmtDate, type BushaEvent, type Reconciliation } from "../api";
import { Icon, Lockup } from "../brand";
import { Btn, Card, ErrorNote, Field, Hero, KV, Screen, SectionTitle, ThemeToggle, usePoll } from "../ui";

const KEY = "gigvault.adminKey";
const u = (micro: number) => (micro / 1e6).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** Judges' view: our ledger next to what Busha says we hold, plus the webhooks we received. */
export function Admin() {
  const [key, setKey] = useState<string>(() => localStorage.getItem(KEY) ?? import.meta.env.VITE_ADMIN_KEY ?? "");
  const [draft, setDraft] = useState("");

  if (!key) {
    return (
      <Screen>
        <Header />
        <Card>
          <div className="mb-3 text-sm text-mu">Enter the admin key to see the reconciliation.</div>
          <Field label="Admin key" value={draft} onChange={setDraft} mono />
          <div className="mt-3"><Btn onClick={() => { localStorage.setItem(KEY, draft); setKey(draft); }} disabled={!draft}>Open</Btn></div>
        </Card>
      </Screen>
    );
  }
  return <AdminBody adminKey={key} onReset={() => { localStorage.removeItem(KEY); setKey(""); }} />;
}

function Header() {
  return (
    <div className="flex items-center justify-between pb-2 pt-1">
      <Lockup size={28} />
      <ThemeToggle />
    </div>
  );
}

function AdminBody({ adminKey, onReset }: { adminKey: string; onReset: () => void }) {
  const rec = usePoll<Reconciliation>(() => api.reconciliation(adminKey), 3000, [adminKey]);
  const ev = usePoll<BushaEvent[]>(() => api.events(adminKey), 3000, [adminKey]);
  const r = rec.data;

  const busha = r?.busha_usdt_total != null ? Math.round(parseFloat(r.busha_usdt_total) * 1e6) : null;
  // Busha's balance must cover everything we owe. It can hold more (sandbox
  // test funds, money not yet assigned to a gig), so "covered" is the test,
  // not exact equality.
  const matched = r && busha != null ? busha + 10_000 >= r.ledger_owed_usdt_micro : null; // 0.01 USDT tolerance
  const extra = r && busha != null ? busha - r.ledger_owed_usdt_micro : 0;
  const parts = r ? [
    { label: "In escrow", v: r.escrow_usdt_micro, cls: "bg-lime" },
    { label: "Frozen in disputes", v: r.frozen_usdt_micro, cls: "bg-[#F2B544]" },
    { label: "Worker balances", v: r.worker_balances_usdt_micro, cls: "bg-[#B7CFC6]" },
    { label: "Platform fees", v: r.platform_fees_usdt_micro, cls: "bg-white/40" },
  ] : [];
  const total = parts.reduce((a, p) => a + p.v, 0) || 1;

  return (
    <Screen>
      <Header />
      <div className="text-2xl font-extrabold tracking-[-0.02em]">Does the money add up?</div>
      <div className="text-sm text-mu">Live every 3 seconds. Busha mode: <b className="text-ink">{r?.busha_mode ?? "…"}</b></div>

      {(rec.error || ev.error) && (
        <div className="space-y-2">
          <ErrorNote msg={rec.error ?? ev.error} />
          <Btn variant="secondary" small full={false} onClick={onReset}>Change admin key</Btn>
        </div>
      )}

      {r && (
        <Hero strip={[80, 140]}>
          <div className="flex items-center justify-between">
            <div className="text-xs font-bold uppercase tracking-[0.12em] text-white/60">Reconciliation</div>
            {matched === null ? (
              <span className="rounded-full bg-white/15 px-3 py-1 text-xs font-bold">No Busha balance</span>
            ) : matched ? (
              <span className="gv-pulse inline-flex items-center gap-1 rounded-full bg-lime px-3 py-1 text-xs font-extrabold text-dg"><Icon name="check" size={13} sw={2.4} />Fully covered</span>
            ) : (
              <span className="inline-flex items-center gap-1 rounded-full bg-[#FF6B6B] px-3 py-1 text-xs font-extrabold text-white"><Icon name="alert" size={13} sw={2.2} />Short</span>
            )}
          </div>
          <div className="mt-4 grid grid-cols-2 gap-3">
            <div>
              <div className="text-xs text-white/60">Our ledger owes</div>
              <div className="font-mono text-[22px] font-bold">{u(r.ledger_owed_usdt_micro)}</div>
              <div className="text-[11px] text-white/50">USDT</div>
            </div>
            <div>
              <div className="text-xs text-white/60">Busha holds</div>
              <div className="font-mono text-[22px] font-bold">{busha != null ? u(busha) : "—"}</div>
              <div className="text-[11px] text-white/50">USDT</div>
            </div>
          </div>
          {matched && extra >= 10_000 && (
            <div className="mt-3 text-xs text-white/60">Busha holds {u(extra)} USDT more than we owe. That's money not assigned to any gig, such as sandbox test funds.</div>
          )}
          {matched === false && <div className="mt-3 text-xs text-[#FFB4B4]">Busha holds less than our ledger owes. Check for a missed webhook or payout.</div>}
          {r.busha_error && <div className="mt-3 text-xs text-[#FFB4B4]">{r.busha_error}</div>}
        </Hero>
      )}

      {r && (
        <Card>
          <SectionTitle>Where the money is</SectionTitle>
          <div className="mt-3 flex h-3 overflow-hidden rounded-full bg-circ">
            {parts.map((p) => p.v > 0 && <div key={p.label} className={`${p.cls} gv-fill`} style={{ width: `${(p.v / total) * 100}%` }} />)}
          </div>
          <div className="mt-3">
            {parts.map((p) => (
              <KV key={p.label} k={<span className="inline-flex items-center gap-2"><span className={`inline-block h-2.5 w-2.5 rounded-full ${p.cls} ring-1 ring-line`} />{p.label}</span>} v={<span className="font-mono">{u(p.v)}</span>} />
            ))}
          </div>
        </Card>
      )}

      <Card>
        <SectionTitle right={<span className="text-xs text-mu">latest 20</span>}>Busha webhooks</SectionTitle>
        <div className="mt-2">
          {ev.data?.length === 0 && <div className="py-4 text-sm text-mu">No webhooks yet. Pay an invoice to see one arrive.</div>}
          {ev.data?.map((e) => (
            <div key={e.id} className="flex items-start gap-3 border-t border-line py-3 first:border-t-0">
              <span className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full ${e.error ? "bg-red-bg text-red" : "bg-soft text-acc"}`}>
                <Icon name={e.error ? "alert" : "link"} size={16} sw={1.9} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-bold">{e.event}</div>
                <div className="truncate font-mono text-[11px] text-mu">{e.reference}</div>
                <div className="mt-1 flex flex-wrap gap-1.5">
                  {e.error ? <Pill tone="red">Error: {e.error}</Pill> : e.processed_at ? <Pill tone="ok">Processed</Pill> : <Pill tone="mu">Received</Pill>}
                  {e.duplicates > 0 && <Pill tone="amb">Duplicate, ignored ×{e.duplicates}</Pill>}
                </div>
              </div>
              <div className="shrink-0 text-right text-[11px] text-mu">{fmtDate(e.received_at)}</div>
            </div>
          ))}
        </div>
      </Card>
    </Screen>
  );
}

function Pill({ tone, children }: { tone: "ok" | "amb" | "red" | "mu"; children: ReactNode }) {
  const cls = { ok: "bg-lime text-dg", amb: "bg-amb-bg text-amb", red: "bg-red-bg text-red", mu: "bg-circ text-mu" }[tone];
  return <span className={`rounded-full px-2.5 py-1 text-[11px] font-bold ${cls}`}>{children}</span>;
}
