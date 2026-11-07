// Typed client for the GigVault Go API.
const BASE = import.meta.env.VITE_API_URL ?? "http://localhost:8080";

export type GigStatus =
  | "AWAITING_PAYMENT" | "BOOKED" | "DONE_PENDING" | "DISPUTED"
  | "RELEASED" | "PARTIALLY_REFUNDED" | "CANCELLED";

export interface Gig {
  id: string;
  worker_id: string;
  title: string;
  client_name: string;
  client_email: string;
  event_at: string;
  amount_ngn_kobo: number;
  amount_usdt_micro: number | null;
  rate_ngn_per_usdt: number | null;
  deposit_bps: number;
  status: GigStatus;
  pay_in_method: string;
  payment_details: Record<string, unknown> | null;
  release_at: string | null;
}

export interface Wallet {
  worker: { id: string; name: string; bank_name: string | null; account_number: string | null };
  rate_ngn_per_usdt: number;
  available_usdt_micro: number;
  available_ngn_kobo: number;
  locked_ngn_kobo: number;
  frozen_ngn_kobo: number;
  gigs: Gig[];
  payouts: { id: string; amount_ngn_kobo: number; status: string; created_at: string }[];
  busha_mode: "mock" | "live";
}

export interface GigDetail {
  gig: Gig;
  balances_usdt_micro: { escrow: number; frozen: number };
  ledger: { txn_id: string; kind: string; account: string; direction: string; amount_usdt_micro: number; created_at: string }[];
  audit: { actor: string; action: string; from_status: string | null; to_status: string | null; details: unknown; created_at: string }[];
  complaints: { id: string; reason: string; frozen_usdt_micro: number; status: string }[];
  busha_mode: string;
}

export class ApiError extends Error {
  constructor(public code: string, message: string, public status: number) {
    super(message);
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok || body.status !== "success") {
    throw new ApiError(body.code ?? "network_error", body.message ?? `HTTP ${res.status}`, res.status);
  }
  return body.data as T;
}

const post = <T,>(path: string, data?: unknown) =>
  call<T>(path, { method: "POST", body: data === undefined ? undefined : JSON.stringify(data) });

export const api = {
  wallet: (workerId: string) => call<Wallet>(`/v1/workers/${workerId}/wallet`),
  gig: (id: string) => call<GigDetail>(`/v1/gigs/${id}`),
  createGig: (input: {
    worker_id: string; title: string; client_name: string; client_email: string;
    event_at: string; amount: string; currency: "NGN" | "USDT";
  }) => post<Gig>("/v1/gigs", input),
  simulatePayment: (id: string) => post(`/v1/dev/gigs/${id}/simulate-payment`),
  complete: (id: string) => post<Gig>(`/v1/gigs/${id}/complete`),
  complain: (id: string, reason: string) => post<Gig>(`/v1/gigs/${id}/complaints`, { reason }),
  resolve: (id: string, outcome: "worker" | "client") => post<Gig>(`/v1/gigs/${id}/complaints/resolve`, { outcome }),
  cancel: (id: string) => post<Gig>(`/v1/gigs/${id}/cancel`),
  cashout: (workerId: string, amount_ngn: string) => post(`/v1/workers/${workerId}/cashout`, { amount_ngn }),
  reconciliation: () => call<Record<string, unknown>>("/v1/admin/reconciliation"),
};

export const naira = (kobo: number) =>
  "₦" + (kobo / 100).toLocaleString("en-NG", { maximumFractionDigits: 0 });
export const usdt = (micro: number | null | undefined) =>
  ((micro ?? 0) / 1e6).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " USDT";
