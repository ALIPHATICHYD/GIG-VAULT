import { useEffect, useMemo, useState } from "react";
import { api, fmtDate, naira, session, usdt, type Gig, type Wallet } from "../api";
import { navigate } from "../router";
import { paper, split as splitArt } from "../art";
import { Art, Avatar, DIcon, Icon, Tilt } from "../brand";
import {
  BottomNav, Btn, Card, CountdownBoxes, CountdownText, DisputePanel, ErrorNote, Field, Hero, KV, Money, Note, RoundButton,
  Row, Screen, SectionTitle, Seg, StatusChip, Steps, ThemeToggle, Timeline, TopBar, useAction, useNow, usePoll,
} from "../ui";
import { LogoutButton } from "./Auth";

const NAV = [
  { label: "Home", icon: "home" as const, to: "/app" },
  { label: "Invoices", icon: "file" as const, to: "/app/invoices" },
  { label: "Cash out", icon: "bank" as const, to: "/app/cashout" },
  { label: "Me", icon: "user" as const, to: "/app/me" },
];
const first = (name?: string | null) => (name ?? "").split(" ")[0];

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

  const titles = ["", `Hi ${first(user?.name)}, what do you do?`, "Show clients you're real", "Where should we send your money?", "You're all set"];
  const bottom = {
    1: <Btn onClick={() => setStep(2)} disabled={!f.craft}>Continue</Btn>,
    2: <Btn onClick={() => setStep(3)} disabled={f.id_number.length !== 11}>Continue</Btn>,
    3: <Btn onClick={finish} disabled={busy || !accountName}>Finish setup</Btn>,
    4: <Btn onClick={() => navigate("/app")}>Go to my wallet</Btn>,
  }[step];

  return (
    <Screen bottom={bottom}>
      <TopBar title={step < 4 ? "Set up payouts" : "All set"} back={step > 1 && step < 4 ? () => setStep(step - 1) : undefined} right={<LogoutButton />} />
      {step < 4 && (
        <div className="flex gap-1.5" aria-label={`Step ${step} of 3`}>
          {[1, 2, 3].map((i) => <span key={i} className={`h-1.5 flex-1 rounded-full ${i <= step ? "bg-acc" : "bg-line"}`} />)}
        </div>
      )}
      <div>
        {step < 4 && <p className="text-[13px] font-bold text-mu">Step {step} of 3</p>}
        <h2 className="mt-1 text-[28px] font-extrabold leading-[1.15] tracking-[-0.02em]">{titles[step]}</h2>
        {step === 2 && <p className="mt-2 text-[15px] text-mu">Busha checks it once. We only keep the last 4 digits.</p>}
        {step === 3 && <p className="mt-2 text-[15px] text-mu">Cash-outs land here in naira.</p>}
      </div>
      {step === 1 && (
        <div className="flex flex-col gap-3">
          <Field label="Your craft" placeholder="Makeup artist · Photographer · DJ" value={f.craft} onChange={(v) => setF({ ...f, craft: v })} />
          <Field label="Date of birth" optional placeholder="24-12-1998" value={f.birth_date} onChange={(v) => setF({ ...f, birth_date: v })} />
          <Field label="City" value={f.city} onChange={(v) => setF({ ...f, city: v })} />
        </div>
      )}
      {step === 2 && (
        <div className="flex flex-col gap-3">
          <Seg value={f.id_type} onChange={(v) => setF({ ...f, id_type: v })} options={[{ value: "bvn", label: "BVN" }, { value: "nin", label: "NIN" }]} />
          <Field label={`${f.id_type.toUpperCase()} (11 digits)`} value={f.id_number} inputMode="numeric" mono
            onChange={(v) => setF({ ...f, id_number: v.replace(/\D/g, "").slice(0, 11) })} />
        </div>
      )}
      {step === 3 && (
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-2">
            <span className="text-[13px] font-bold">Bank</span>
            <span className="flex items-center gap-3 rounded-2xl border border-line bg-card px-3 py-2.5">
              <DIcon name="bank" size={36} />
              <select className="flex-1 bg-transparent py-1.5 text-base font-semibold text-ink outline-none" value={f.bank_code}
                onChange={(e) => setF({ ...f, bank_code: e.target.value, bank_name: e.target.selectedOptions[0]?.text ?? "" })}>
                <option value="">Choose your bank</option>
                {banks.map((b) => <option key={b.code} value={b.code}>{b.name}</option>)}
              </select>
            </span>
          </label>
          <Field label="Account number" value={f.account_number} inputMode="numeric" mono
            onChange={(v) => setF({ ...f, account_number: v.replace(/\D/g, "").slice(0, 10) })} />
          {accountName && (
            <div className="flex items-center gap-2.5 rounded-2xl bg-soft px-4 py-3.5">
              <Icon name="check" size={20} sw={2.2} className="text-acc" />
              <span className="text-sm font-extrabold uppercase">{accountName}</span>
              <span className="ml-auto text-xs font-medium text-mu">Checked with Busha</span>
            </div>
          )}
          <ErrorNote msg={err} />
        </div>
      )}
      {step === 4 && (
        <>
          <Note tone="soft" icon="check">Your bank account is saved on Busha. Cash-outs go straight there.</Note>
          {kycLink && <a href={kycLink} target="_blank" rel="noreferrer"><Btn variant="secondary" icon="shield">Verify my identity with Busha</Btn></a>}
        </>
      )}
    </Screen>
  );
}

// ---------------- Creator home ----------------

function useWallet() {
  const wallet = usePoll(() => api.wallet());
  if (wallet.error?.includes("finish setting up")) queueMicrotask(() => navigate("/onboard"));
  return wallet;
}

function act(kind: string): { icon: "down" | "bank" | "alert" | "lock" | "file"; sign: string; accent: boolean } | null {
  switch (kind) {
    case "upfront.released": case "gig.auto_released": case "gig.unfrozen_part_released": return { icon: "down", sign: "+", accent: true };
    case "payout.completed": return { icon: "bank", sign: "−", accent: false };
    case "gig.dispute_opened": return { icon: "alert", sign: "", accent: false };
    case "gig.funded": return { icon: "lock", sign: "", accent: false };
    case "invoice.sent": return { icon: "file", sign: "", accent: false };
    default: return null;
  }
}

export function CreatorHome() {
  const wallet = useWallet();
  const activity = usePoll(() => api.activity(), 4000);
  const [hide, setHide] = useState(false);
  const w = wallet.data;
  const total = w ? w.available_ngn_kobo + w.locked_ngn_kobo + w.frozen_ngn_kobo : 0;
  const feed = (activity.data ?? []).filter((e) => act(e.action)).slice(0, 5);

  return (
    <Screen nav={<BottomNav items={NAV} active="Home" />}>
      <div className="flex items-center gap-3">
        <Avatar name={w?.worker.name ?? session.get()?.user.name ?? ""} />
        <div className="flex-1">
          <div className="text-[13px] text-mu">Welcome back</div>
          <div className="text-lg font-extrabold">{first(w?.worker.name ?? session.get()?.user.name)}</div>
        </div>
        <ThemeToggle />
      </div>

      {!w && <p className="text-mu">{wallet.error ?? "Loading your wallet…"}</p>}
      {w && (
        <>
          <Hero strip={[150, 250]}>
            <button className="flex items-center gap-2 text-sm text-sage" onClick={() => setHide(!hide)} aria-label={hide ? "Show balance" : "Hide balance"}>
              Total balance <Icon name="eye" size={18} />
            </button>
            <div className="mt-1.5">{hide ? <div className="text-4xl font-extrabold">₦ ••••••</div> : <Money kobo={total} size={36} light />}</div>
            <div className="mt-1.5 flex flex-wrap gap-x-3.5 gap-y-1 text-xs text-sage">
              <span><b className="text-lime">{naira(w.available_ngn_kobo)}</b> ready</span>
              <span><b className="text-white">{naira(w.locked_ngn_kobo)}</b> held</span>
              <span><b className="text-white">{naira(w.frozen_ngn_kobo)}</b> frozen</span>
            </div>
            <div className="mt-1 text-[11px] text-sage/80">≈ {usdt(w.available_usdt_micro)} ready · held as USDT on Busha</div>
            <div className="mt-5 flex">
              {([["Invoice", "plus", "/app/new"], ["Cash out", "bank", "/app/cashout"], ["Invoices", "file", "/app/invoices"], ["Theme", "dots", "/app/me"]] as const).map(([l, ic, to], i) => (
                <button key={l} onClick={() => navigate(to)} className="flex flex-1 flex-col items-center gap-2 text-xs font-bold text-white">
                  <span className={`flex h-[54px] w-[54px] items-center justify-center rounded-full ${i === 0 ? "bg-lime text-dg" : "bg-white/12"}`}>
                    <Icon name={ic} size={24} sw={1.8} />
                  </span>{i === 3 ? "More" : l}
                </button>
              ))}
            </div>
          </Hero>

          <button onClick={() => navigate("/app/new")} className="relative h-[112px] overflow-hidden rounded-[22px] bg-soft px-[18px] py-4 text-left">
            <div className="max-w-[180px] text-base font-extrabold leading-[1.3]">Get paid before the gig. Up to 70% upfront.</div>
            <span className="mt-2.5 inline-block text-[13px] font-extrabold text-acc underline">New invoice</span>
            <div className="absolute right-3 top-[18px]"><Tilt text="70%" /></div>
          </button>

          <section>
            <SectionTitle right={<button className="text-sm font-bold text-acc" onClick={() => navigate("/app/invoices")}>See all</button>}>Your invoices</SectionTitle>
            {w.gigs.length === 0 && <p className="mt-2 text-sm text-mu">No invoices yet. Agree a price with a client, then send them an invoice.</p>}
            {w.gigs.slice(0, 3).map((g) => <GigRow key={g.id} g={g} />)}
          </section>

          {feed.length > 0 && (
            <section>
              <SectionTitle>Recent activity</SectionTitle>
              {feed.map((e, i) => {
                const a = act(e.action)!;
                const k = Number((e.details as Record<string, unknown> | null)?.ngn_kobo ?? 0);
                return <Row key={i} icon={a.icon} title={e.gig_title ?? "Cash out"}
                  sub={`${{ "upfront.released": "Upfront", "gig.auto_released": "Rest released", "gig.unfrozen_part_released": "Unfrozen part", "payout.completed": "Cash out to your bank", "gig.dispute_opened": "Issue raised", "gig.funded": "Client paid, held by Busha", "invoice.sent": "Invoice sent" }[e.action]} · ${fmtDate(e.created_at)}`}
                  amount={k > 0 ? `${a.sign}${naira(k)}` : ""} amountClass={a.accent ? "text-acc" : ""} />;
              })}
            </section>
          )}
        </>
      )}
    </Screen>
  );
}

function GigRow({ g }: { g: Gig }) {
  return (
    <button onClick={() => navigate(`/app/gigs/${g.id}`)} className="flex w-full items-center gap-3 py-[11px] text-left">
      <DIcon name={g.status === "RELEASED" ? "check" : g.status === "DISPUTED" || g.status === "ESCALATED" ? "alert" : g.status === "AWAITING_PAYMENT" ? "file" : "lock"} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[15px] font-bold">{g.title}</div>
        <div className="truncate text-xs text-mu">
          {naira(g.amount_ngn_kobo)} · {g.client_name ?? "client"}
          {g.status === "DONE_PENDING" && <> · releasing in <CountdownText until={g.release_at} /></>}
        </div>
      </div>
      <StatusChip status={g.status} />
    </button>
  );
}

export function Invoices() {
  const wallet = useWallet();
  return (
    <Screen nav={<BottomNav items={NAV} active="Invoices" />}>
      <TopBar title="Invoices" right={<RoundButton icon="plus" label="New invoice" onClick={() => navigate("/app/new")} />} />
      <Card pad="px-[18px] py-1.5">
        {wallet.data?.gigs.length === 0 && <p className="py-3 text-sm text-mu">No invoices yet.</p>}
        {wallet.data?.gigs.map((g) => <GigRow key={g.id} g={g} />)}
        {!wallet.data && <p className="py-3 text-sm text-mu">Loading…</p>}
      </Card>
    </Screen>
  );
}

export function Me() {
  const wallet = useWallet();
  const w = wallet.data;
  return (
    <Screen nav={<BottomNav items={NAV} active="Me" />}>
      <TopBar title="Me" right={<ThemeToggle />} />
      <div className="flex items-center gap-3">
        <Avatar name={w?.worker.name ?? ""} size={56} />
        <div><div className="text-lg font-extrabold">{w?.worker.name}</div><div className="text-[13px] text-mu">{w?.worker.craft}</div></div>
      </div>
      {w && (
        <Card pad="px-[18px] py-2">
          <KV k="Payout bank" v={`${w.worker.bank_name ?? "—"} ••${w.worker.account_number?.slice(-4) ?? ""}`} />
          <KV k="Account name" v={w.worker.account_name ?? "—"} />
          <KV k="Identity check" v={w.worker.kyc_status} />
          <KV k="Busha" v={w.busha_mode === "live" ? "Sandbox" : "Mock"} />
        </Card>
      )}
      <Btn variant="secondary" icon="logout" onClick={() => { session.clear(); navigate("/"); }}>Log out</Btn>
    </Screen>
  );
}

// ---------------- New invoice + sent ----------------

const UPFRONT = [0, 20, 30, 50, 70];
const defaultDate = () => { const d = new Date(Date.now() + 3 * 864e5); d.setMinutes(0, 0, 0); return new Date(d.getTime() - d.getTimezoneOffset() * 6e4).toISOString().slice(0, 16); };

export function NewInvoice() {
  const [f, setF] = useState({ title: "", client_name: "", client_email: "", amount_ngn: "", event_at: defaultDate(), note: "" });
  const [upfront, setUpfront] = useState(20);
  const [sent, setSent] = useState<{ link: string } | null>(null);
  const { busy, err, run } = useAction();
  const amount = Number(f.amount_ngn.replace(/[^\d.]/g, "")) || 0;
  const up = Math.round(amount * upfront) / 100;

  const create = () => run(async () => {
    const r = await api.createGigLink({ ...f, amount_ngn: String(amount), upfront_percent: upfront, event_at: new Date(f.event_at).toISOString() });
    setSent({ link: window.location.origin + r.pay_path });
  });

  if (sent) return <InvoiceSent link={sent.link} f={f} amount={amount} upfront={upfront} />;

  return (
    <Screen bottom={<Btn icon="send" onClick={create} disabled={busy || !f.title || !amount || !f.client_name}>Send invoice</Btn>}>
      <TopBar title="New invoice" back="/app" />
      <div className="flex flex-col gap-3.5">
        <Field label="What's the job?" placeholder="Wedding makeup + 3 bridesmaids" value={f.title} onChange={(v) => setF({ ...f, title: v })} />
        <div className="grid grid-cols-2 gap-3">
          <Field label="Amount (₦)" inputMode="decimal" placeholder="120,000" value={f.amount_ngn} onChange={(v) => setF({ ...f, amount_ngn: v })} />
          <Field label="Date" type="datetime-local" value={f.event_at} onChange={(v) => setF({ ...f, event_at: v })} />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Client name" placeholder="Tolu Adeyemi" value={f.client_name} onChange={(v) => setF({ ...f, client_name: v })} />
          <Field label="Email" optional inputMode="email" placeholder="tolu@mail.com" value={f.client_email} onChange={(v) => setF({ ...f, client_email: v })} />
        </div>
      </div>
      <div>
        <div className="mb-2.5 text-[13px] font-bold">Paid to you upfront</div>
        <div className="flex gap-1.5">
          {UPFRONT.map((p) => (
            <button key={p} type="button" onClick={() => setUpfront(p)} aria-pressed={upfront === p}
              className={`h-11 flex-1 rounded-full text-sm font-extrabold ${upfront === p ? "bg-pbg text-pfg" : "bg-card text-ink"}`}>{p}%</button>
          ))}
        </div>
      </div>
      <Hero strip={[110, 110]} className="!p-[18px]">
        <div className="flex justify-between">
          <div><div className="text-xs text-sage">When {first(f.client_name) || "they"} pays</div><div className="text-[22px] font-extrabold text-lime">{naira(up * 100)}</div></div>
          <div className="text-right"><div className="text-xs text-sage">When the gig is done</div><div className="text-[22px] font-extrabold">{naira((amount - up) * 100)}</div></div>
        </div>
        <div className="mt-3 flex h-2 gap-1">
          <div className="gv-fill rounded-full bg-lime" style={{ width: `${upfront}%` }} />
          <div className="flex-1 rounded-full bg-white/22" />
        </div>
      </Hero>
      <Field label="Note" optional placeholder="Includes transport to the venue" value={f.note} onChange={(v) => setF({ ...f, note: v })} />
      <ErrorNote msg={err} />
    </Screen>
  );
}

function InvoiceSent({ link, f, amount, upfront }: { link: string; f: { title: string; client_name: string; client_email: string }; amount: number; upfront: number }) {
  const [copied, setCopied] = useState(false);
  const wa = `https://wa.me/?text=${encodeURIComponent(`Hi ${f.client_name}, here's my invoice for "${f.title}" (${naira(amount * 100)}). Pay on GigVault: Busha holds your money until the job is done. ${link}`)}`;
  return (
    <Screen bottom={<>
      <a href={wa} target="_blank" rel="noreferrer" className="w-full"><Btn icon="chat">Share on WhatsApp</Btn></a>
      <Btn variant="secondary" onClick={() => navigate("/app")}>Back to home</Btn>
    </>}>
      <TopBar title="Invoice sent" back="/app" />
      <div className="flex justify-center rounded-[26px] bg-soft p-2"><Art svg={paper} size={190} /></div>
      <div className="text-center">
        <h2 className="text-2xl font-extrabold tracking-[-0.02em]">Sent to {f.client_name}</h2>
        <p className="mt-1.5 text-sm leading-normal text-mu">
          {f.client_email ? `It's waiting in ${first(f.client_name)}'s account under Pending payments (${f.client_email}). ` : ""}You can also send the link yourself.
        </p>
      </div>
      <div className="flex items-center gap-2.5 rounded-2xl border-[1.5px] border-dashed border-acc bg-card py-2.5 pl-3.5 pr-2.5">
        <Icon name="link" size={20} />
        <div className="min-w-0 flex-1 truncate text-[13px] font-bold">{link.replace(/^https?:\/\//, "")}</div>
        <Btn small full={false} variant="soft" icon={copied ? "check" : "copy"} onClick={() => { navigator.clipboard?.writeText(link); setCopied(true); }}>{copied ? "Copied" : "Copy"}</Btn>
      </div>
      <Card>
        <Steps steps={[
          { title: "Invoice sent", sub: `${naira(amount * 100)} · ${upfront}% upfront`, when: "Now", state: "done" },
          { title: `${first(f.client_name)} opens it`, state: "todo" },
          { title: `${first(f.client_name)} pays`, sub: `${naira(amount * upfront)} comes to you`, state: "todo" },
        ]} />
      </Card>
    </Screen>
  );
}

// ---------------- Cash out ----------------

export function CashOut() {
  const wallet = useWallet();
  const w = wallet.data;
  const [amount, setAmount] = useState("");
  const [done, setDone] = useState<string | null>(null);
  const [lockUntil, setLockUntil] = useState(() => new Date(Date.now() + 180e3).toISOString());
  const now = useNow(1000);
  const { busy, err, run } = useAction();
  const n = Number(amount.replace(/[^\d.]/g, "")) || 0;
  useEffect(() => { if (new Date(lockUntil).getTime() <= now) setLockUntil(new Date(now + 180e3).toISOString()); }, [now, lockUntil]);
  useEffect(() => { if (w && !amount) setAmount(String(Math.floor(w.available_ngn_kobo / 100))); }, [w, amount]);

  const rate = w?.rate_ngn_per_usdt ?? 0;
  return (
    <Screen nav={done ? undefined : <BottomNav items={NAV} active="Cash out" />} bottom={done ? <Btn onClick={() => navigate("/app")}>Back to home</Btn> : undefined}>
      <TopBar title="Cash out" back="/app" />
      {done ? (
        <>
          <Note tone="soft" icon="check">{done}</Note>
          <p className="text-sm text-mu">Busha sends the naira to your bank. You'll see "Money landed in the bank" in your activity when it arrives.</p>
        </>
      ) : (
        <>
          <div className="pt-1.5 text-center">
            <div className="text-[13px] text-mu">Amount</div>
            <label className="flex items-baseline justify-center text-[52px] font-extrabold tracking-[-0.03em]">
              ₦<input aria-label="Amount in naira" inputMode="numeric" value={n ? n.toLocaleString("en-NG") : ""} placeholder="0"
                onChange={(e) => setAmount(e.target.value.replace(/\D/g, ""))}
                className="w-[230px] bg-transparent text-center outline-none placeholder:text-mu/50" />
            </label>
            <div className="text-[13px] text-mu">{w ? `${naira(w.available_ngn_kobo)} available` : "Loading…"}</div>
          </div>
          <div className="flex justify-center gap-2">
            {w && [["₦10,000", 10000], ["₦25,000", 25000], ["Max", Math.floor(w.available_ngn_kobo / 100)]].map(([l, v]) => (
              <button key={l} onClick={() => setAmount(String(v))}
                className={`rounded-full px-3.5 py-[9px] text-[13px] font-extrabold ${l === "Max" ? "bg-pbg text-pfg" : "bg-card text-ink"}`}>{l}</button>
            ))}
          </div>
          {w && (
            <Card>
              <div className="flex items-center gap-3">
                <DIcon name="bank" />
                <div className="flex-1"><div className="text-[15px] font-extrabold">{w.worker.bank_name} ••{w.worker.account_number?.slice(-4)}</div><div className="text-xs text-mu">{w.worker.account_name}</div></div>
              </div>
            </Card>
          )}
          {w && rate > 0 && (
            <Card>
              <KV k="From your dollars" v={`${(n / rate).toFixed(2)} USDT`} />
              <KV k="Rate" v={`${naira(rate * 100)} per USDT`} />
              <KV k="You receive" v={naira(n * 100)} accent />
              <div className="mt-2.5"><Note tone="amb" icon="clock">Rate locked for <span className="gv-tick"><CountdownText until={lockUntil} /></span></Note></div>
            </Card>
          )}
          <ErrorNote msg={err} />
          <Btn icon="bank" disabled={busy || !n || !w || n * 100 > w.available_ngn_kobo}
            onClick={() => run(async () => { await api.cashout(String(n)); setDone(`${naira(n * 100)} is on its way to ${w?.worker.bank_name}.`); wallet.refresh(); })}>
            Cash out {n ? naira(n * 100) : ""}
          </Btn>
        </>
      )}
    </Screen>
  );
}

// ---------------- Creator invoice detail ----------------

export function CreatorGig({ id }: { id: string }) {
  const p = usePoll(() => api.creatorGig(id), 2000, [id]);
  const d = p.data;
  const { busy, err, run } = useAction();
  const [copied, setCopied] = useState(false);
  const [confirmVoid, setConfirmVoid] = useState(false);
  const dispute = d?.complaints.length ? d.complaints[d.complaints.length - 1] : null;
  const link = d?.pay_path ? window.location.origin + d.pay_path : "";
  const upfront = d ? d.gig.deposit_bps / 100 : 0;
  const todo = useMemo(() => {
    if (!d) return [];
    const s = d.gig.status;
    const rest = naira(d.gig.amount_ngn_kobo * (100 - upfront) / 100);
    if (s === "AWAITING_PAYMENT") return [{ title: `${first(d.gig.client_name) || "Client"} pays`, sub: `${naira(d.gig.amount_ngn_kobo * upfront / 100)} comes to you`, state: "todo" as const }];
    if (s === "BOOKED") return [{ title: "Client confirms done", sub: `${rest} comes to you`, state: "now" as const }];
    if (s === "DONE_PENDING") return [{ title: "Rest releases by itself", sub: rest, state: "now" as const }];
    return [];
  }, [d, upfront]);

  return (
    <Screen>
      <TopBar title={d?.gig.title ?? "Invoice"} back="/app" />
      {!d && <p className="text-mu">{p.error ?? "Loading…"}</p>}
      {d && (
        <>
          <div className="flex items-end justify-between gap-3">
            <div>
              <Money kobo={d.gig.amount_ngn_kobo} />
              <div className="text-[13px] text-mu">{d.gig.client_name ?? "Client not joined yet"}{d.gig.client_email ? ` · ${d.gig.client_email}` : ""}</div>
              <div className="text-[13px] text-mu">{upfront}% upfront · {fmtDate(d.gig.event_at)}</div>
            </div>
            <StatusChip status={d.gig.status} />
          </div>
          {d.gig.status === "AWAITING_PAYMENT" && link && (
            <div className="flex items-center gap-2.5 rounded-2xl border-[1.5px] border-dashed border-acc bg-card py-2.5 pl-3.5 pr-2.5">
              <Icon name="link" size={20} />
              <div className="min-w-0 flex-1 truncate text-[13px] font-bold">{link.replace(/^https?:\/\//, "")}</div>
              <Btn small full={false} variant="soft" icon={copied ? "check" : "copy"} onClick={() => { navigator.clipboard?.writeText(link); setCopied(true); }}>{copied ? "Copied" : "Copy"}</Btn>
            </div>
          )}
          {d.gig.status === "AWAITING_PAYMENT" && (confirmVoid ? (
            <Card>
              <div className="text-sm">Withdraw this invoice? {d.gig.client_name ?? "The client"} won't be able to pay it, and this can't be undone.</div>
              <div className="mt-3 flex gap-2">
                <Btn variant="secondary" disabled={busy} onClick={() => setConfirmVoid(false)}>Keep it</Btn>
                <Btn variant="danger" disabled={busy} onClick={() => run(async () => { await api.voidInvoice(id); setConfirmVoid(false); p.refresh(); })}>Yes, void it</Btn>
              </div>
            </Card>
          ) : (
            <button className="self-center text-sm font-bold text-red" onClick={() => setConfirmVoid(true)}>Void this invoice</button>
          ))}
          {d.gig.status === "VOIDED" && <Note tone="soft" icon="x">You withdrew this invoice, so it can no longer be paid.</Note>}
          {d.gig.status === "EXPIRED" && <Note tone="soft" icon="clock">This invoice expired before it was paid. Send {d.gig.client_name ?? "your client"} a new one.</Note>}
          {d.gig.status === "DONE_PENDING" && (
            <Hero strip={[110, 140]}>
              <div className="text-xl font-extrabold">Client confirmed. Paying out to you in</div>
              <div className="mt-3"><CountdownBoxes until={d.gig.release_at} /></div>
            </Hero>
          )}
          {dispute && (d.gig.status === "DISPUTED" || d.gig.status === "ESCALATED") && (
            <div className="flex flex-col gap-3">
              <div className="flex items-center gap-3"><Art svg={splitArt} size={64} /><div className="text-sm text-mu">Part of the money is frozen until you both agree, or GigVault decides.</div></div>
              <DisputePanel dispute={dispute} me="creator" otherName={first(d.gig.client_name) || "The client"} feePercent={10} busy={busy}
                onPropose={(pct) => run(async () => { await api.creatorPropose(id, pct); p.refresh(); })}
                onAccept={() => run(async () => { await api.creatorAccept(id); p.refresh(); })} />
            </div>
          )}
          <ErrorNote msg={err} />
          <Timeline events={d.audit} amountKobo={d.gig.amount_ngn_kobo} extra={todo} />
          <details className="rounded-[22px] bg-card p-[18px] text-xs">
            <summary className="cursor-pointer text-sm font-extrabold">Ledger <span className="font-medium text-mu">(double-entry, append-only)</span></summary>
            <table className="mt-2 w-full font-mono"><tbody>
              {d.ledger.map((e, i) => (
                <tr key={i} className="border-t border-line">
                  <td className="py-1.5 pr-2">{e.kind}</td>
                  <td className="pr-2">{e.account.replace(d.gig.id, "…")}</td>
                  <td className={e.direction === "credit" ? "text-acc" : "text-red"}>{e.direction}</td>
                  <td className="text-right">{usdt(e.amount_usdt_micro)}</td>
                </tr>
              ))}
            </tbody></table>
          </details>
        </>
      )}
    </Screen>
  );
}

export type { Wallet };
