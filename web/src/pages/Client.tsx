import { Bell, Notifications } from "../notify";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import qrcode from "qrcode-generator";
import { api, ApiError, fmtDate, naira, session, usdt, type PublicGig } from "../api";
import { navigate } from "../router";
import { coinNote, done as doneArt, split as splitArt } from "../art";
import { Art, Avatar, DIcon, Icon, Lockup, Mark, type IconName } from "../brand";
import {
  BottomNav, Btn, Card, CountdownBoxes, CountdownText, DisputePanel, ErrorNote, Field, Hero, KV, Money, Note,
  Screen, SectionTitle, StatusChip, Steps, ThemeToggle, Timeline, TopBar, useAction, usePoll, type Step,
} from "../ui";
import { LogoutButton } from "./Auth";

const SHOW_DEV_TOOLS = import.meta.env.VITE_SHOW_DEV_TOOLS !== "false";
const LATE_CANCEL_MS = 24 * 3600e3;
const NAV = [
  { label: "Payments", icon: "file" as const, to: "/bookings" },
  { label: "History", icon: "clock" as const, to: "/bookings/history" },
  { label: "Me", icon: "user" as const, to: "/bookings/me" },
];
const first = (name?: string | null) => (name ?? "").split(" ")[0];
const windowText = (s: number) => (s >= 3600 ? `${Math.round(s / 3600)} hours` : s >= 60 ? `${Math.round(s / 60)} minutes` : `${s} seconds`);
const upfrontKobo = (g: PublicGig) => Math.round((g.amount_ngn_kobo * g.deposit_percent) / 100);

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
        catch (e) { if (!(e instanceof ApiError && e.code === "already_claimed")) throw e; throw new Error("This invoice was already paid by someone else."); }
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, token]);

  return (
    <Screen bottom={gig && !s ? <>
      <Btn onClick={() => navigate(`/signup?role=client&next=${encodeURIComponent(here)}`)}>Create account to pay</Btn>
      <p className="text-sm text-mu">Have an account?{" "}
        <button className="font-extrabold text-acc underline" onClick={() => navigate(`/login?next=${encodeURIComponent(here)}`)}>Log in</button></p>
    </> : undefined}>
      <div className="flex items-center justify-between">
        <Lockup size={32} anim />
        <div className="flex items-center gap-2"><span className="text-xs text-mu">Powered by Busha</span><ThemeToggle /></div>
      </div>
      <ErrorNote msg={err} />
      {!gig && !err && <p className="text-mu">Loading invoice…</p>}
      {gig && <InvoiceCard gig={gig} />}
      {gig && <Promises gig={gig} />}
      {gig && s?.user.role === "creator" && <Note tone="amb" icon="user">You're logged in as a creator. Log out and use a client account to pay this invoice.</Note>}
    </Screen>
  );
}

function InvoiceCard({ gig }: { gig: PublicGig }) {
  const up = upfrontKobo(gig);
  return (
    <Hero strip={[130, 240]}>
      <div className="flex items-center gap-3">
        <Avatar name={gig.creator.name} />
        <div><div className="text-xs text-sage">Invoice from</div><div className="text-[17px] font-extrabold">{gig.creator.name}</div></div>
        {gig.creator.verified && (
          <span className="ml-auto inline-flex items-center gap-1 rounded-full bg-lime px-[9px] py-[5px] text-[11px] font-extrabold text-dg"><Icon name="shield" size={13} sw={2} />Verified</span>
        )}
      </div>
      <div className="mt-[18px] text-[13px] text-sage">{gig.title} · {fmtDate(gig.event_at)}</div>
      <div className="mt-0.5"><Money kobo={gig.amount_ngn_kobo} size={40} light /></div>
      {gig.note && <div className="mt-1 text-[13px] text-sage">“{gig.note}”</div>}
      <div className="mt-3.5 flex h-2 gap-1">
        <div className="gv-fill rounded-full bg-lime" style={{ width: `${gig.deposit_percent}%` }} />
        <div className="flex-1 rounded-full bg-white/22" />
      </div>
      <div className="mt-2 flex justify-between text-xs text-sage">
        <span><b className="text-lime">{naira(up)}</b> at payment</span>
        <span><b className="text-white">{naira(gig.amount_ngn_kobo - up)}</b> when done</span>
      </div>
    </Hero>
  );
}

function Promises({ gig }: { gig: PublicGig }) {
  const items: [IconName, string, string][] = [
    ["lock", "Held until you're happy", `Busha holds it as dollars. ${first(gig.creator.name)} gets ${gig.deposit_percent}% to start, the rest when you confirm.`],
    ["alert", "Something wrong? Raise it", `You get ${windowText(gig.complaint_window_seconds)} after confirming. ${gig.freeze_percent}% of what's left freezes straight away.`],
    ["chat", "Fair if you disagree", `Agree a split for free, or GigVault decides for a ${gig.dispute_fee_percent}% fee.`],
  ];
  return (
    <div className="flex flex-col gap-3.5">
      {items.map(([ic, t, s]) => (
        <div key={t} className="flex items-center gap-3"><DIcon name={ic} /><div><div className="text-sm font-extrabold">{t}</div><div className="text-[13px] text-mu">{s}</div></div></div>
      ))}
    </div>
  );
}

// ---------------- /bookings ----------------

export function Bookings({ tab = "pending" }: { tab?: "pending" | "history" }) {
  const b = usePoll(() => api.bookings(), 3000);
  const user = session.get()?.user;
  const pending = b.data?.filter((g) => g.status === "AWAITING_PAYMENT") ?? [];
  const rest = b.data?.filter((g) => g.status !== "AWAITING_PAYMENT") ?? [];
  const owed = pending.reduce((a, g) => a + g.amount_ngn_kobo, 0);

  return (
    <Screen nav={<BottomNav items={NAV} active={tab === "pending" ? "Payments" : "History"} />}>
      <div className="flex items-center gap-3">
        <Avatar name={user?.name ?? ""} lime={false} />
        <div className="flex-1"><div className="text-[13px] text-mu">Welcome back</div><div className="text-lg font-extrabold">{first(user?.name)}</div></div>
        <Bell />
        <ThemeToggle />
      </div>
      {b.error && <ErrorNote msg={b.error} />}
      {tab === "pending" && (
        <>
          <Hero strip={[120, 90]} className="!py-[18px]">
            <div className="text-[13px] text-sage">Waiting for you</div>
            <div className="text-2xl font-extrabold">{pending.length} invoice{pending.length === 1 ? "" : "s"} · {naira(owed)}</div>
          </Hero>
          <SectionTitle>Pending payments</SectionTitle>
          {b.data && pending.length === 0 && <p className="-mt-3 text-sm text-mu">Nothing to pay. Invoices creators send you show up here.</p>}
          {pending.map((g, i) => <InvoiceTile key={g.id} g={g} cta={i === 0} />)}
          {rest.filter((g) => !["RELEASED", "CANCELLED", "PARTIALLY_REFUNDED", "VOIDED", "EXPIRED"].includes(g.status)).length > 0 && <SectionTitle>In progress</SectionTitle>}
          {rest.filter((g) => !["RELEASED", "CANCELLED", "PARTIALLY_REFUNDED", "VOIDED", "EXPIRED"].includes(g.status)).map((g) => <InvoiceTile key={g.id} g={g} />)}
        </>
      )}
      {tab === "history" && (
        <>
          <TopBar title="History" />
          <Card pad="px-[18px] py-1.5">
            {b.data && rest.length === 0 && <p className="py-3 text-sm text-mu">Nothing yet.</p>}
            {rest.map((g) => (
              <button key={g.id} onClick={() => navigate(`/bookings/${g.id}`)} className="flex w-full items-center gap-3 py-[11px] text-left">
                <Avatar name={g.creator.name} lime={false} />
                <div className="min-w-0 flex-1"><div className="truncate text-[15px] font-bold">{g.title}</div><div className="text-xs text-mu">{g.creator.name} · {naira(g.amount_ngn_kobo)}</div></div>
                <StatusChip status={g.status} />
              </button>
            ))}
          </Card>
        </>
      )}
    </Screen>
  );
}

function InvoiceTile({ g, cta }: { g: PublicGig; cta?: boolean }) {
  return (
    <Card>
      <button onClick={() => navigate(`/bookings/${g.id}`)} className="flex w-full items-center gap-3 text-left">
        <Avatar name={g.creator.name} lime={false} />
        <div className="min-w-0 flex-1"><div className="truncate text-[15px] font-extrabold">{g.creator.name}</div><div className="truncate text-xs text-mu">{g.title}</div></div>
        <StatusChip status={g.status} />
      </button>
      <div className="mt-3.5 flex items-baseline justify-between"><Money kobo={g.amount_ngn_kobo} size={24} /><div className="text-[13px] text-mu">{fmtDate(g.event_at)}</div></div>
      {cta && <div className="mt-3.5"><Btn onClick={() => navigate(`/bookings/${g.id}`)}>Review and pay</Btn></div>}
    </Card>
  );
}

export function ClientMe() {
  const user = session.get()?.user;
  return (
    <Screen nav={<BottomNav items={NAV} active="Me" />}>
      <TopBar title="Me" right={<ThemeToggle />} />
      <div className="flex items-center gap-3"><Avatar name={user?.name ?? ""} size={56} lime={false} />
        <div><div className="text-lg font-extrabold">{user?.name}</div><div className="text-[13px] text-mu">{user?.email}</div></div></div>
      <Btn variant="secondary" icon="logout" onClick={() => { session.clear(); navigate("/"); }}>Log out</Btn>
    </Screen>
  );
}

// ---------------- /bookings/:id ----------------

export function Booking({ id }: { id: string }) {
  const g = usePoll(() => api.booking(id), 2000, [id]);
  const { busy, err, run } = useAction();
  const [reason, setReason] = useState("");
  const [raising, setRaising] = useState(false);
  const gig = g.data;
  const act = (fn: () => Promise<unknown>) => run(async () => { await fn(); g.refresh(); });
  const name = first(gig?.creator.name);

  if (!gig) return <Screen><TopBar title="Payment" back="/bookings" /><p className="text-mu">{g.error ?? "Loading…"}</p></Screen>;

  // Paid in full: the receipt.
  if (gig.status === "RELEASED") return <Receipt gig={gig} />;

  const s = gig.status;
  const awaiting = s === "AWAITING_PAYMENT";
  const paying = awaiting && !!gig.payment_details;
  const todo: Step[] = s === "BOOKED"
    ? [{ title: "You confirm done", sub: `${naira(gig.amount_ngn_kobo - upfrontKobo(gig))} goes to ${name}`, state: "now" }]
    : s === "DONE_PENDING" ? [{ title: "Rest releases by itself", sub: `Unless you raise an issue`, state: "now" }] : [];

  let bottom: ReactNode = undefined;
  if (awaiting && !paying) bottom = <Btn disabled={busy} onClick={() => act(() => api.checkout(id, "NGN"))}>Pay {naira(gig.amount_ngn_kobo)} securely</Btn>;

  return (
    <Screen bottom={bottom}>
      <TopBar title={awaiting ? `Pay ${name}` : gig.title} back="/bookings" right={!awaiting ? <LogoutButton /> : undefined} />

      {awaiting && !paying && (
        <>
          <div className="text-center">
            <div className="text-[13px] text-mu">Amount to send</div>
            <div className="mt-1 flex justify-center"><Money kobo={gig.amount_ngn_kobo} size={40} /></div>
            <div className="text-[13px] text-mu">{gig.amount_usdt_micro ? `≈ ${usdt(gig.amount_usdt_micro)}, held by Busha` : "Held by Busha as dollars until you confirm"}</div>
          </div>
          <PayWith />
          <InvoiceCard gig={gig} />
          <Promises gig={gig} />
        </>
      )}

      {paying && (gig.pay_in_method === "address" ? <UsdtDetails gig={gig} /> : <BankDetails gig={gig} />)}
      {paying && <Waiting gig={gig} />}
      {paying && SHOW_DEV_TOOLS && <Btn variant="secondary" disabled={busy} onClick={() => act(() => api.simulatePayment(id))}>Simulate Busha payment webhook (demo)</Btn>}

      {!awaiting && (
        <div className="flex items-end justify-between gap-3">
          <div><Money kobo={gig.amount_ngn_kobo} /><div className="text-[13px] text-mu">To {gig.creator.name}</div></div>
          <StatusChip status={s} />
        </div>
      )}

      {s === "BOOKED" && (
        <Hero strip={[110, 140]}>
          <div className="flex flex-col gap-3">
            <div className="text-xl font-extrabold">Was the job done well?</div>
            <div className="text-sm leading-normal text-sage">
              Confirming sends the last {naira(gig.amount_ngn_kobo - upfrontKobo(gig))} to {name}, after a {windowText(gig.complaint_window_seconds)} window to raise an issue.
            </div>
            <Btn variant="lime" icon="check" disabled={busy} onClick={() => act(() => api.complete(id))}>Confirm done</Btn>
          </div>
        </Hero>
      )}
      {s === "BOOKED" && <Btn variant="secondary" onClick={() => navigate(`/bookings/${id}/cancel`)}>Cancel booking</Btn>}

      {s === "DONE_PENDING" && (
        <Hero strip={[110, 140]}>
          <div className="flex flex-col gap-3">
            <div className="text-xl font-extrabold">Releases to {name} in</div>
            <CountdownBoxes until={gig.release_at} />
            {!raising && <Btn variant="outlineLight" icon="alert" onClick={() => setRaising(true)}>Raise an issue</Btn>}
          </div>
        </Hero>
      )}
      {s === "DONE_PENDING" && raising && (
        <Card className="flex flex-col gap-3">
          <Field label="What went wrong?" placeholder="One nail lifted after 2 days" value={reason} onChange={setReason} />
          <Note tone="amb" icon="snow">{gig.freeze_percent}% of what's left freezes until you and {name} agree.</Note>
          <Btn variant="danger" disabled={busy || !reason} onClick={() => act(() => api.complain(id, reason))}>Raise the issue</Btn>
        </Card>
      )}

      {gig.dispute && (s === "DISPUTED" || s === "ESCALATED") && (
        <>
          <div className="flex items-center gap-3"><Art svg={splitArt} size={64} /><div className="text-sm text-mu">Part of the money is frozen until you both agree, or GigVault decides.</div></div>
          <DisputePanel dispute={gig.dispute} me="client" otherName={name} feePercent={gig.dispute_fee_percent} busy={busy}
            onPropose={(pct) => act(() => api.clientPropose(id, pct))} onAccept={() => act(() => api.clientAccept(id))} />
        </>
      )}
      {s === "PARTIALLY_REFUNDED" && <Note tone="soft" icon="check">The dispute is settled. Your share has been refunded.</Note>}
      {s === "CANCELLED" && <Note tone="soft" icon="x">This booking was cancelled.</Note>}
      {s === "VOIDED" && <Note tone="soft" icon="x">{name} withdrew this invoice, so it can no longer be paid. Ask for a new one if you still need it.</Note>}
      {s === "EXPIRED" && <Note tone="soft" icon="clock">This invoice expired before it was paid. Ask {name} to send a new one.</Note>}
      <ErrorNote msg={err} />
      {!awaiting && <Timeline events={gig.timeline} amountKobo={gig.amount_ngn_kobo} extra={todo} />}
    </Screen>
  );
}

function CopyRow({ k, v, copy = true, mono }: { k: string; v: string; copy?: boolean; mono?: boolean }) {
  const [done, setDone] = useState(false);
  return (
    <div className="flex items-center justify-between border-t border-line py-[13px] first:border-t-0">
      <div className="min-w-0"><div className="text-xs text-mu">{k}</div><div className={`break-all text-[17px] font-extrabold ${mono ? "font-mono text-sm font-medium" : ""}`}>{v}</div></div>
      {copy && <Btn small full={false} variant="soft" icon={done ? "check" : "copy"} onClick={() => { navigator.clipboard?.writeText(v); setDone(true); }}>{done ? "Copied" : "Copy"}</Btn>}
    </div>
  );
}

function BankDetails({ gig }: { gig: PublicGig }) {
  const d = gig.payment_details ?? {};
  return (
    <>
      <div className="text-center">
        <div className="text-[13px] text-mu">Transfer exactly</div>
        <div className="mt-1 flex justify-center"><Money kobo={gig.amount_ngn_kobo} size={40} /></div>
        {gig.amount_usdt_micro && <div className="text-[13px] text-mu">≈ {usdt(gig.amount_usdt_micro)}, held by Busha</div>}
      </div>
      <Card pad="px-[18px] pt-1 pb-[18px]">
        <CopyRow k="Account number" v={d.account_number ?? "…"} />
        <CopyRow k="Bank" v={d.bank_name ?? "Busha partner bank"} copy={false} />
        <CopyRow k="Account name" v={d.account_name ?? "GigVault"} copy={false} />
        {d.expires_at && <div className="mt-2"><Note tone="amb" icon="clock">This account expires in <CountdownText until={d.expires_at} /></Note></div>}
      </Card>
    </>
  );
}

function Qr({ text }: { text: string }) {
  const cells = useMemo(() => {
    const q = qrcode(0, "H"); q.addData(text); q.make();
    const n = q.getModuleCount(); const out: [number, number][] = [];
    for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) if (q.isDark(r, c)) out.push([c, r]);
    return { n, out };
  }, [text]);
  const c = cells.n / 2;
  return (
    <svg role="img" aria-label="Deposit address QR code" width="164" height="164" viewBox={`-1 -1 ${cells.n + 2} ${cells.n + 2}`} shapeRendering="crispEdges">
      <rect x="-1" y="-1" width={cells.n + 2} height={cells.n + 2} fill="#FFFFFF" />
      <path fill="#10201B" d={cells.out.map(([x, y]) => `M${x} ${y}h1v1h-1z`).join("")} />
      <rect x={c - 3.5} y={c - 3.5} width="7" height="7" rx="1.2" fill="#FFFFFF" />
      <svg x={c - 3} y={c - 3} width="6" height="6" viewBox="0 0 48 48"><Mark size={48} /></svg>
    </svg>
  );
}

function UsdtDetails({ gig }: { gig: PublicGig }) {
  const d = gig.payment_details ?? {};
  const addr = d.address ?? "";
  return (
    <>
      <div className="text-center">
        <div className="text-[13px] text-mu">Send exactly</div>
        <div className="text-[40px] font-extrabold tracking-[-0.03em]">{((gig.amount_usdt_micro ?? 0) / 1e6).toFixed(2)} <span className="text-[22px]">USDT</span></div>
        <div className="text-[13px] text-mu">= {naira(gig.amount_ngn_kobo)} for {first(gig.creator.name)}</div>
      </div>
      <Card>
        <div className="flex items-center gap-3.5">
          <div className="rounded-2xl border border-line bg-white p-2">{addr ? <Qr text={addr} /> : <div className="h-[164px] w-[164px]" />}</div>
          <div className="flex min-w-0 flex-col gap-2">
            <div className="text-xs text-mu">Network</div>
            <span className="self-start rounded-full bg-soft px-[11px] py-1.5 text-[13px] font-extrabold text-acc">{d.network ?? "Tron (TRC-20)"}</span>
            <div className="mt-1 text-xs text-mu">Busha deposit address</div>
            <div className="break-all font-mono text-[13px] font-medium leading-snug">{addr || "Address pending…"}</div>
          </div>
        </div>
        <div className="mt-3.5"><Btn variant="soft" icon="copy" small onClick={() => navigator.clipboard?.writeText(addr)}>Copy address</Btn></div>
      </Card>
      <Note tone="red" icon="alert">Only send USDT on {d.network ?? "Tron"}. Other coins or networks can't be recovered.</Note>
    </>
  );
}

function Waiting({ gig }: { gig: PublicGig }) {
  const up = upfrontKobo(gig);
  return (
    <Card>
      <div className="flex flex-col items-center gap-1.5 text-center">
        <div className="rounded-[26px] bg-soft p-2"><Art svg={coinNote} size={150} /></div>
        <div className="mt-2 text-[22px] font-extrabold tracking-[-0.02em]">Waiting for your {gig.pay_in_method === "address" ? usdt(gig.amount_usdt_micro) : naira(gig.amount_ngn_kobo)}</div>
        <div className="max-w-[280px] text-sm leading-normal text-mu">When it lands, Busha holds it as dollars for this gig. This page updates by itself.</div>
      </div>
      <div className="mt-5">
        <Steps steps={[
          { title: gig.pay_in_method === "address" ? "Send USDT" : "Send the bank transfer", sub: "From your own account", state: "done" },
          { title: "Busha receives it", sub: "Usually under a minute", state: "now" },
          { title: gig.amount_usdt_micro ? `Held as ${usdt(gig.amount_usdt_micro)}` : "Held as dollars", sub: "Locked to this gig", state: "todo" },
          { title: `${naira(up)} to ${first(gig.creator.name)}`, sub: "The upfront part", state: "todo" },
        ]} />
      </div>
    </Card>
  );
}

function Receipt({ gig }: { gig: PublicGig }) {
  const up = upfrontKobo(gig);
  return (
    <Screen bottom={<Btn onClick={() => navigate("/bookings")}>Done</Btn>}>
      <TopBar title="Receipt" back="/bookings" />
      <div className="flex flex-col items-center gap-1 text-center">
        <Art svg={doneArt} size={170} />
        <h2 className="text-[26px] font-extrabold tracking-[-0.02em]">Paid in full</h2>
        <p className="text-sm text-mu">{first(gig.creator.name)} got all {naira(gig.amount_ngn_kobo)}. Thanks for booking safely.</p>
      </div>
      <Card pad="px-[18px] pt-1.5 pb-4">
        <KV k={gig.title} v={naira(gig.amount_ngn_kobo)} strong />
        <KV k="Upfront at payment" v={naira(up)} />
        <KV k="Released when done" v={naira(gig.amount_ngn_kobo - up)} />
        <KV k="Paid in" v={gig.pay_currency === "USDT" ? "USDT from abroad" : "Naira bank transfer"} />
        {gig.amount_usdt_micro && <KV k="Held as" v={`${usdt(gig.amount_usdt_micro)} on Busha`} />}
        <div className="flex justify-between border-t border-line pt-2.5 text-xs text-mu"><span>Reference</span><span className="font-mono">{gig.id}</span></div>
      </Card>
      <Timeline events={gig.timeline} amountKobo={gig.amount_ngn_kobo} />
    </Screen>
  );
}

/** Naira bank transfer for now. USDT is listed so clients know it is on the way. */
function PayWith() {
  return (
    <label className="flex items-center justify-between gap-3 rounded-2xl bg-card px-4 py-3">
      <span className="text-sm text-mu">Pay with</span>
      <span className="relative">
        <select defaultValue="NGN" aria-label="Pay with"
          className="appearance-none rounded-full bg-circ py-2 pl-4 pr-9 text-sm font-bold text-ink outline-none">
          <option value="NGN">Naira, bank transfer</option>
          <option value="USDT" disabled>USDT (coming soon)</option>
        </select>
        <Icon name="chev" size={14} sw={2} className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 rotate-90 text-mu" />
      </span>
    </label>
  );
}

// ---------------- /bookings/:id/cancel ----------------

export function CancelBooking({ id }: { id: string }) {
  const g = usePoll(() => api.booking(id), 5000, [id]);
  const { busy, err, run } = useAction();
  const gig = g.data;
  if (!gig) return <Screen><TopBar title="Cancel booking" back={`/bookings/${id}`} /><p className="text-mu">{g.error ?? "Loading…"}</p></Screen>;
  if (gig.status !== "BOOKED") { queueMicrotask(() => navigate(`/bookings/${id}`)); return null; }

  const msLeft = new Date(gig.event_at).getTime() - Date.now();
  const late = msLeft < LATE_CANCEL_MS;
  const up = upfrontKobo(gig);
  const back = late ? gig.amount_ngn_kobo - up : gig.amount_ngn_kobo;
  const keepPct = late ? gig.deposit_percent : 0;
  const name = first(gig.creator.name);
  const hrs = Math.max(0, Math.floor(msLeft / 3.6e6)), mins = Math.max(0, Math.floor((msLeft % 3.6e6) / 6e4));

  return (
    <Screen bottom={<>
      <Btn onClick={() => navigate(`/bookings/${id}`)}>Keep my booking</Btn>
      <Btn variant="danger" disabled={busy} onClick={() => run(async () => { await api.cancel(id); navigate(`/bookings/${id}`); })}>Cancel and get {naira(back)} back</Btn>
    </>}>
      <TopBar title="Cancel booking" back={`/bookings/${id}`} />
      <div className="flex items-end justify-between gap-3">
        <div><div className="text-[13px] text-mu">{gig.title} · {gig.creator.name}</div><Money kobo={gig.amount_ngn_kobo} /></div>
        <StatusChip status={gig.status} />
      </div>
      {late
        ? <Note tone="amb" icon="clock">Starts in {hrs}h {mins}m. That's under 24 hours.</Note>
        : <Note tone="soft" icon="check">More than 24 hours to go, so you get everything back.</Note>}
      <Hero strip={[70, 56]}>
        <div className="flex flex-col gap-3">
          <div className="text-lg font-extrabold">If you cancel now</div>
          <div className="flex h-2.5 gap-1">
            {keepPct > 0 && <div className="gv-fill rounded-full bg-lime" style={{ width: `${keepPct}%` }} />}
            <div className="flex-1 rounded-full bg-white/30" />
          </div>
          <div className="flex justify-between text-[13px] text-sage">
            <div>{name} keeps<div className="text-xl font-extrabold text-lime">{naira(late ? up : 0)}</div>{late ? "the upfront part" : "nothing"}</div>
            <div className="text-right">Back to you<div className="text-xl font-extrabold text-white">{naira(back)}</div>refunded via Busha</div>
          </div>
          <div className="text-[13px] leading-normal text-sage">
            {late ? `${name} kept the day free for you. Cancel 24 hours or more before the gig and the upfront part comes back too.` : `${name} returns the upfront part, and the rest comes straight out of what Busha is holding.`}
          </div>
        </div>
      </Hero>
      <ErrorNote msg={err} />
    </Screen>
  );
}

export function ClientNotifications() {
  return <Notifications nav={NAV} />;
}
