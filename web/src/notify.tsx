import { useEffect, useState, useSyncExternalStore } from "react";
import { api, fmtDate, naira, session, type ActivityRow } from "./api";
import { navigate } from "./router";
import { BottomNav, RoundButton, Screen, TopBar } from "./ui";
import { Icon, type IconName } from "./brand";

// Live notifications: both roles poll their own activity feed, turn each event
// into a plain-English notice, pop a toast for anything the other side (or
// Busha) did, and keep an unread count for the bell.

type Role = "creator" | "client";
type Tone = "good" | "alert" | "info";
export interface Notice { id: number; title: string; body: string; tone: Tone; icon: IconName; href: string; amount: string; at: string; mine: boolean }

const POLL_MS = 4000;

function describe(r: ActivityRow, role: Role): Notice | null {
  const gig = r.gig_title ?? "Your invoice";
  const kobo = Number((r.details as Record<string, unknown> | null)?.ngn_kobo ?? 0);
  const amt = kobo > 0 ? naira(kobo) : "";
  const creator = role === "creator";
  const href = creator ? (r.gig_id ? `/app/gigs/${r.gig_id}` : "/app/cashout") : r.gig_id ? `/bookings/${r.gig_id}` : "/bookings";
  const mine = creator ? r.actor === "worker" || r.actor === "creator" : r.actor === "client";
  const n = (title: string, tone: Tone, icon: IconName, body = gig, amount = ""): Notice =>
    ({ id: r.id, title, body, tone, icon, href, amount, at: r.created_at, mine });

  switch (r.action) {
    case "invoice.sent": return creator ? n("Invoice sent", "info", "send") : n("New invoice for you", "info", "file");
    case "invoice.opened": return creator ? n("Your client opened the invoice", "info", "eye") : null;
    case "gig.claimed_by_client": return creator ? n("Your client accepted the invoice", "info", "check") : null;
    case "gig.checkout_started": return creator ? n("Your client is paying now", "info", "clock") : n("Bank details ready, waiting for your transfer", "info", "bank");
    case "gig.funded": return creator ? n("Payment received, held by Busha", "good", "lock", gig, amt) : n("Payment confirmed. Your money is held safely", "good", "shield", gig, amt);
    case "upfront.released": return creator ? n("Upfront paid to you", "good", "down", gig, amt ? "+" + amt : "") : n("Upfront part paid to the creator", "info", "up", gig, amt);
    case "gig.marked_done": return creator ? n("Client confirmed the job is done", "good", "check") : n("You confirmed the job. Complaint window started", "info", "clock");
    case "gig.auto_released": return creator ? n("Money released to you", "good", "down", gig, amt ? "+" + amt : "") : n("Payment released to the creator", "info", "check", gig, amt);
    case "gig.dispute_opened": return creator ? n("Your client raised an issue", "alert", "alert") : n("Issue raised. Part of the money is frozen", "alert", "snow");
    case "gig.unfrozen_part_released": return creator ? n("Unfrozen part released to you", "good", "down", gig, amt ? "+" + amt : "") : null;
    case "dispute.split_proposed": return n(mine ? "You proposed a split" : creator ? "Your client proposed a split" : "The creator proposed a split", "alert", "chat");
    case "dispute.settled_by_agreement": return n("Dispute settled by agreement", "good", "check");
    case "dispute.escalated_to_gigvault": return n("Dispute sent to GigVault to decide", "alert", "shield");
    case "dispute.resolved_by_gigvault": return n("GigVault resolved the dispute", "info", "shield");
    case "gig.cancelled": return creator ? n("Booking cancelled by the client", "alert", "x") : n("Booking cancelled", "info", "x");
    case "invoice.voided": return creator ? n("Invoice voided", "info", "x") : n("The creator withdrew this invoice", "info", "x");
    case "invoice.expired": return n("Invoice expired unpaid", "info", "clock");
    case "invoice.reopened_by_payment": return n("Late payment received, invoice reopened", "good", "lock");
    case "payout.requested": return creator ? n("Cash-out started", "info", "bank", "Busha is sending naira to your bank", amt) : null;
    case "payout.completed": return creator ? n("Money landed in your bank", "good", "bank", "Cash-out complete", amt) : null;
    case "payout.failed_reversed": return creator ? n("Cash-out failed, money returned", "alert", "alert", "Your balance was restored", amt) : null;
    default: return null;
  }
}

// ---------- store ----------

interface State { role: Role | null; items: Notice[]; seen: number; toasts: Notice[]; ready: boolean }
let state: State = { role: null, items: [], seen: 0, toasts: [], ready: false };
const subs = new Set<() => void>();
const emit = (next: Partial<State>) => { state = { ...state, ...next }; subs.forEach((f) => f()); };
const seenKey = () => `gigvault.seen.${session.get()?.user.id ?? "anon"}`;
let timer: number | undefined;
let busy = false;
let maxId = 0;
let uidNow: string | undefined;

async function tick() {
  if (busy || !state.role || document.hidden) return;
  busy = true;
  try {
    const rows = await (state.role === "creator" ? api.activity() : api.clientActivity());
    const items = rows.map((r) => describe(r, state.role!)).filter((x): x is Notice => !!x);
    const fresh = state.ready ? items.filter((i) => i.id > maxId && !i.mine) : [];
    maxId = Math.max(maxId, ...rows.map((r) => r.id), 0);
    // First time on this device: start the unread count from now, not from all history.
    if (!state.ready && localStorage.getItem(seenKey()) === null) {
      localStorage.setItem(seenKey(), String(maxId));
      state = { ...state, seen: maxId };
    }
    emit({ items, ready: true, toasts: [...state.toasts, ...fresh.reverse()].slice(-3) });
    fresh.forEach((f) => { systemAlert(f); window.setTimeout(() => dismiss(f.id), 6000); });
  } catch { /* offline or logged out: try again next tick */ } finally { busy = false; }
}

export function startNotifications(role: Role, uid?: string) {
  if (state.role === role && uidNow === uid && timer) return;
  uidNow = uid;
  stopNotifications();
  maxId = 0;
  emit({ role, items: [], toasts: [], ready: false, seen: Number(localStorage.getItem(seenKey()) ?? 0) });
  tick();
  timer = window.setInterval(tick, POLL_MS);
}

export function stopNotifications() {
  if (timer) window.clearInterval(timer);
  timer = undefined;
  emit({ role: null, items: [], toasts: [], ready: false });
}

export function markAllSeen() {
  const top = state.items[0]?.id ?? 0;
  localStorage.setItem(seenKey(), String(top));
  emit({ seen: top });
}

function dismiss(id: number) { emit({ toasts: state.toasts.filter((t) => t.id !== id) }); }

function useStore() { return useSyncExternalStore((f) => { subs.add(f); return () => subs.delete(f); }, () => state); }

export function useUnread() {
  const s = useStore();
  return s.items.filter((i) => i.id > s.seen && !i.mine).length;
}

// Phone/desktop alert while the tab is in the background, if the user allowed it.
function systemAlert(n: Notice) {
  if (!("Notification" in window) || Notification.permission !== "granted" || !document.hidden) return;
  try { new Notification(n.title, { body: [n.body, n.amount].filter(Boolean).join(" · "), tag: String(n.id) }); } catch { /* some mobile browsers need a service worker */ }
}

// ---------- UI ----------

const toneCls: Record<Tone, string> = { good: "bg-lime text-dg", alert: "bg-[#FBE4E4] text-[#B42318]", info: "bg-soft text-ink" };

export function Bell() {
  const unread = useUnread();
  const to = state.role === "client" ? "/bookings/notifications" : "/app/notifications";
  return (
    <span className="relative">
      <RoundButton icon="bell" label={unread ? `${unread} new notifications` : "Notifications"} onClick={() => navigate(to)} />
      {unread > 0 && (
        <span className="pointer-events-none absolute -right-0.5 -top-0.5 flex h-5 min-w-5 items-center justify-center rounded-full bg-[#E5484D] px-1 text-[11px] font-extrabold text-white">
          {unread > 9 ? "9+" : unread}
        </span>
      )}
    </span>
  );
}

export function Toasts() {
  const { toasts } = useStore();
  if (!toasts.length) return null;
  return (
    <div className="pointer-events-none fixed inset-x-0 top-3 z-50 mx-auto flex max-w-[440px] flex-col gap-2 px-3">
      {toasts.map((t) => (
        <button key={t.id} onClick={() => { dismiss(t.id); navigate(t.href); }}
          className="gv-up pointer-events-auto flex items-center gap-3 rounded-[20px] bg-card p-3 text-left shadow-[0_12px_32px_rgba(0,0,0,.18)] ring-1 ring-black/5">
          <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-full ${toneCls[t.tone]}`}><Icon name={t.icon} size={20} /></span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-extrabold">{t.title}</span>
            <span className="block truncate text-xs text-mu">{t.body}</span>
          </span>
          {t.amount && <span className="text-sm font-extrabold text-acc">{t.amount}</span>}
        </button>
      ))}
    </div>
  );
}

export function Notifications({ nav }: { nav: Parameters<typeof BottomNav>[0]["items"] }) {
  const s = useStore();
  const [unreadBefore] = useState(() => state.seen);
  const [perm, setPerm] = useState(typeof Notification === "undefined" ? "unsupported" : Notification.permission);
  // Opening the page marks everything as read, including events that arrive while it's open.
  useEffect(() => { markAllSeen(); }, [s.items[0]?.id]);
  return (
    <Screen nav={<BottomNav items={nav} active="" />}>
      <TopBar title="Notifications" back={() => history.back()} />
      {perm === "default" && (
        <button onClick={() => Notification.requestPermission().then(setPerm)} className="flex items-center gap-3 rounded-[18px] bg-soft p-3.5 text-left">
          <Icon name="bell" size={20} /><span className="flex-1 text-sm font-bold">Get alerts even when this tab is in the background</span>
          <span className="text-sm font-extrabold text-acc">Turn on</span>
        </button>
      )}
      {!s.ready && <p className="text-mu">Loading…</p>}
      {s.ready && s.items.length === 0 && <p className="text-sm text-mu">Nothing yet. You'll see payments, releases and disputes here as they happen.</p>}
      <div className="flex flex-col">
        {s.items.map((n) => (
          <button key={n.id} onClick={() => navigate(n.href)} className="flex items-center gap-3 border-b border-black/5 py-3 text-left last:border-0">
            <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-full ${toneCls[n.tone]}`}><Icon name={n.icon} size={20} /></span>
            <span className="min-w-0 flex-1">
              <span className="block text-sm font-extrabold">{n.title}</span>
              <span className="block truncate text-xs text-mu">{n.body} · {fmtDate(n.at)}</span>
            </span>
            {n.amount && <span className="text-sm font-extrabold">{n.amount}</span>}
            {n.id > unreadBefore && !n.mine && <span className="h-2 w-2 rounded-full bg-[#E5484D]" />}
          </button>
        ))}
      </div>
    </Screen>
  );
}
