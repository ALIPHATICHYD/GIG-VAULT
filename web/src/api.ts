// Typed client for the GigVault Go API.
const BASE = import.meta.env.VITE_API_URL ?? "http://localhost:8080";

export type Role = "creator" | "client";
export type GigStatus =
  | "AWAITING_PAYMENT" | "BOOKED" | "DONE_PENDING" | "DISPUTED" | "ESCALATED"
  | "RELEASED" | "PARTIALLY_REFUNDED" | "CANCELLED" | "VOIDED" | "EXPIRED";

export interface User { id: string; email: string; name: string; phone: string | null; role: Role; worker_id: string | null }

export interface Gig {
  id: string;
  title: string;
  client_name: string | null;
  client_email: string | null;
  event_at: string;
  amount_ngn_kobo: number;
  amount_usdt_micro: number | null;
  status: GigStatus;
  release_at: string | null;
  deposit_bps: number;
}

export interface Wallet {
  worker: { id: string; name: string; craft: string | null; bank_name: string | null; account_number: string | null; account_name: string | null; kyc_status: string; kyc_link: string | null };
  rate_ngn_per_usdt: number;
  available_usdt_micro: number;
  available_ngn_kobo: number;
  locked_ngn_kobo: number;
  frozen_ngn_kobo: number;
  gigs: Gig[];
  payouts: { id: string; amount_ngn_kobo: number; status: string; created_at: string }[];
  busha_mode: "mock" | "live";
}

export interface TimelineEvent { actor: string; action: string; to_status: string | null; details: Record<string, unknown> | null; created_at: string }

export interface Dispute {
  id: string;
  reason: string;
  frozen_usdt_micro: number;
  status: "open" | "settled" | "escalated" | "resolved";
  negotiate_until: string | null;
  proposal_creator_percent: number | null;
  proposed_by: "creator" | "client" | null;
  outcome_creator_percent: number | null;
  fee_usdt_micro: number;
}

export interface GigDetail {
  gig: Gig;
  pay_path: string;
  balances_usdt_micro: { escrow: number; frozen: number };
  ledger: { txn_id: string; kind: string; account: string; direction: string; amount_usdt_micro: number }[];
  audit: TimelineEvent[];
  complaints: Dispute[];
}

export interface PublicGig {
  id: string;
  title: string;
  note: string | null;
  event_at: string;
  amount_ngn_kobo: number;
  amount_usdt_micro: number | null;
  status: GigStatus;
  client_name: string | null;
  pay_currency: "NGN" | "USDT" | null;
  pay_in_method: string | null;
  payment_details: Record<string, string> | null;
  release_at: string | null;
  deposit_percent: number;
  freeze_percent: number;
  complaint_window_seconds: number;
  creator: { name: string; craft: string | null; verified: boolean };
  timeline: TimelineEvent[];
  dispute: Dispute | null;
  dispute_fee_percent: number;
  busha_mode: "mock" | "live";
}

export class ApiError extends Error {
  constructor(public code: string, message: string, public status: number) { super(message); }
}

// ---- session ----
const KEY = "gigvault.session";
export interface Session { token: string; user: User }
export const session = {
  get(): Session | null { try { return JSON.parse(localStorage.getItem(KEY) ?? "null"); } catch { return null; } },
  set(s: Session) { localStorage.setItem(KEY, JSON.stringify(s)); },
  clear() { localStorage.removeItem(KEY); },
};

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const s = session.get();
  const res = await fetch(BASE + path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(s ? { Authorization: `Bearer ${s.token}` } : {}),
      ...(init?.headers ?? {}),
    },
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok || body.status !== "success") {
    if (res.status === 401 && s) session.clear();
    throw new ApiError(body.code ?? "network_error", body.message ?? `HTTP ${res.status}`, res.status);
  }
  return body.data as T;
}
const post = <T,>(path: string, data: unknown = {}) => call<T>(path, { method: "POST", body: JSON.stringify(data) });

export const api = {
  // accounts
  signup: (d: { email: string; password: string; name: string; phone?: string; role: Role }) => post<Session>("/v1/auth/signup", d),
  login: (email: string, password: string) => post<Session>("/v1/auth/login", { email, password }),
  me: () => call<User>("/v1/me"),
  // onboarding
  banks: () => call<{ name: string; code: string }[]>("/v1/banks"),
  resolveBank: (bank_code: string, account_number: string) =>
    post<{ account_name: string; bank_name: string }>("/v1/banks/resolve", { bank_code, account_number }),
  onboard: (d: Record<string, string>) => post<{ worker: Wallet["worker"]; kyc_link: string | null; notes: string[] }>("/v1/creator/onboard", d),
  // creator
  wallet: () => call<Wallet>("/v1/creator/wallet"),
  createGigLink: (d: { title: string; event_at: string; amount_ngn: string; client_name: string; client_email?: string; upfront_percent: number; note?: string }) =>
    post<{ gig: Gig; pay_path: string }>("/v1/creator/gigs", d),
  activity: () => call<(TimelineEvent & { gig_id: string | null; gig_title: string | null })[]>("/v1/creator/activity"),
  creatorGig: (id: string) => call<GigDetail>(`/v1/creator/gigs/${id}`),
  voidInvoice: (id: string) => post<GigDetail>(`/v1/creator/gigs/${id}/void`),
  cashout: (amount_ngn: string) => post("/v1/creator/cashout", { amount_ngn }),
  creatorPropose: (id: string, creator_percent: number) => post(`/v1/creator/gigs/${id}/dispute/propose`, { creator_percent }),
  creatorAccept: (id: string) => post(`/v1/creator/gigs/${id}/dispute/accept`),
  // public + client
  preview: (id: string, t: string) => call<PublicGig>(`/v1/pay/${id}?t=${encodeURIComponent(t)}`),
  claim: (id: string, token: string) => post<PublicGig>(`/v1/client/claim/${id}`, { token }),
  bookings: () => call<PublicGig[]>("/v1/client/bookings"),
  booking: (id: string) => call<PublicGig>(`/v1/client/bookings/${id}`),
  checkout: (id: string, currency: "NGN" | "USDT") => post<PublicGig>(`/v1/client/bookings/${id}/checkout`, { currency }),
  complete: (id: string) => post<PublicGig>(`/v1/client/bookings/${id}/complete`),
  complain: (id: string, reason: string) => post<PublicGig>(`/v1/client/bookings/${id}/complaints`, { reason }),
  cancel: (id: string) => post<PublicGig>(`/v1/client/bookings/${id}/cancel`),
  clientPropose: (id: string, creator_percent: number) => post<PublicGig>(`/v1/client/bookings/${id}/dispute/propose`, { creator_percent }),
  clientAccept: (id: string) => post<PublicGig>(`/v1/client/bookings/${id}/dispute/accept`),
  // dev
  simulatePayment: (id: string) => post(`/v1/dev/gigs/${id}/simulate-payment`),
};

export const naira = (kobo: number) => "₦" + (kobo / 100).toLocaleString("en-NG", { maximumFractionDigits: 0 });
export const usdt = (micro: number | null | undefined) =>
  ((micro ?? 0) / 1e6).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " USDT";
export const fmtDate = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", hour: "numeric", minute: "2-digit" });
