# GigVault

Escrow for creative gig workers, built on Busha. Busha Build Day, 7 Nov 2026.

Chidinma does nails on weekdays, sings at weddings on weekends and picks up dance gigs. Platforms pay late, and sometimes clients don't pay at all. GigVault locks the client's money **before** the gig and releases it the moment the gig is done.

1. **Client books and pays upfront**, in naira by bank transfer or in USDT from abroad (a Busha payment request).
2. **Busha settles it as USDT**, tagged to the gig. Our ledger holds it in escrow.
3. **25% goes to Chidinma at booking** for materials and transport.
4. **Client marks the gig done.** A complaint window starts (6h in production, 60s in the demo).
5. **No complaint means auto-release.** A complaint freezes part of the money until it's settled; the rest still releases.
6. **Late cancel?** The deposit stays with her.
7. **She sees everything in naira** and cashes out to her bank in one transfer (Busha quote + transfer to a bank recipient).

## Architecture

```
React (Vite + Tailwind)  ──►  Go API (chi)  ──►  Postgres on Neon
                                │   ▲             ├─ gigs (state machine)
                                │   │             ├─ ledger_txns / ledger_entries (double-entry, append-only)
                                │   │             ├─ busha_events (raw webhooks, unique per event)
                                │   │             ├─ jobs (queue: FOR UPDATE SKIP LOCKED)
                                ▼   │             └─ audit_log
                         Busha Business API      worker goroutine: process webhooks, auto-release on timer
                         ◄── signed webhooks (X-BU-SIGNATURE, HMAC-SHA256)
```

**Why it's safe with money**
- Busha has no "escrow" product. The escrow is **our double-entry ledger** on top of Busha balances. Every movement is one debit plus one credit, with an idempotency key. A database trigger makes ledger rows append-only.
- Webhooks are verified, stored raw, de-duplicated by `(event, reference)`, then processed by the worker. Redelivery is a no-op.
- Cash-out reserves funds first, then calls Busha. If the transfer fails, the reservation is reversed automatically.
- `GET /v1/admin/reconciliation` compares what the ledger says we hold with `GET /v1/balances` on Busha.
- Integer minor units everywhere (kobo, micro-USDT). No floats for money.

## Busha APIs used

| Purpose | Endpoint |
|---|---|
| Client pays (NGN temporary bank account or USDT address) | `POST /v1/payments/requests` |
| Payment confirmed | webhook `payment_request.completed` / `deposit.success` |
| Rates and cash-out conversion | `POST /v1/quotes` (USDT→NGN, `pay_out: bank_transfer`) |
| Execute cash-out | `POST /v1/transfers` + webhooks `transfer.funds_delivered` / `transfer.failed` |
| Bank list and account check | `GET /v1/banks`, `POST /v1/recipients/resolve-bank-account`, `POST /v1/recipients` |
| Reconciliation | `GET /v1/balances` |

Client code: `backend/internal/busha/client.go`. A clearly labelled mock (`BUSHA_MODE=mock`) lets the UI run when the sandbox is unreachable.

## Run it

Needs Go 1.23+, Node 20+, a Neon Postgres database, and (for real webhooks) cloudflared or ngrok.

```bash
# 1. Database: create a Neon project, copy its connection string
cp backend/.env.example backend/.env       # paste DATABASE_URL, Busha keys

# 2. API + worker (migrations run automatically on start)
make api                                    # http://localhost:8080/healthz

# 3. Web
cp web/.env.example web/.env
make web                                    # http://localhost:5173

# 4. Busha webhooks (they can't reach localhost)
make tunnel                                 # register https://<tunnel>/v1/webhooks/busha in the Busha dashboard
```

## API

Busha-style envelope: `{"status": "success"|"error", "message": "...", "data": ..., "code": "..."}`.

| Method | Path | What it does |
|---|---|---|
| POST | `/v1/gigs` | Book a gig, creates the Busha payment request |
| GET | `/v1/gigs/{id}` | Gig + audit timeline + ledger entries |
| POST | `/v1/gigs/{id}/complete` | Client confirms done, starts complaint window |
| POST | `/v1/gigs/{id}/complaints` | Freeze part of the remaining escrow |
| POST | `/v1/gigs/{id}/complaints/resolve` | `{"outcome":"worker"|"client"}` |
| POST | `/v1/gigs/{id}/cancel` | Refund; deposit kept on late cancel |
| GET | `/v1/workers/{id}/wallet` | Available / locked / frozen, in naira |
| POST | `/v1/workers/{id}/cashout` | `{"amount_ngn":"50000"}` |
| POST | `/v1/webhooks/busha` | Signed Busha webhooks |
| GET | `/v1/admin/reconciliation` | Ledger vs Busha balances |
| POST | `/v1/dev/gigs/{id}/simulate-payment` | Dev only: sends a signed payment webhook through the real path |

Errors use real HTTP codes: 400 validation, 404 not found, 409 invalid state or insufficient funds, 401 bad webhook signature, 502 Busha error (with Busha's `X-Request-Id`).

## Demo script (3 minutes)

1. **Problem (30s):** Chidinma, 51% of Nigerian creatives deal with payment disputes, banks won't escrow a ₦30,000 nail appointment.
2. **Client tab (45s):** book "Saturday wedding" for ₦120,000 and show the Busha bank account. Payment lands and the status turns *Locked*.
3. **Chidinma tab (30s):** "₦120,000 locked for Saturday", ₦30,000 deposit already withdrawable.
4. **Client marks done (30s):** countdown hits zero and the money lands in her wallet. No one pressed anything.
5. **Cash out (15s):** ₦100,000 to GTBank via a Busha quote + transfer.
6. **Timeline tab (30s):** audit trail, double-entry ledger, reconciliation against Busha. Mention duplicate webhooks are ignored.

## Team

- Abdullah: Busha integration and webhooks
- Peter: ledger, state machine, worker
- ALIPHATIC: frontend, design, pitch
