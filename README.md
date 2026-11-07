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

## Who sees what

One account system with two roles. Each role only ever sees its own screens.

| Role | Screens | How they get in |
|---|---|---|
| Creator | `/onboard` (craft, BVN/NIN, bank via Busha) → `/app` wallet → **New gig link** modal → `/app/gigs/:id` (who booked, timeline, ledger) | Sign up as creator |
| Client | `/pay/:id?t=…` preview → sign up / log in → `/bookings` → `/bookings/:id` (pay, mark done, complain, cancel) | Opens the link the creator sent (WhatsApp, copy) |

Both sides know each other: the client sees the creator's name, craft and verified badge; the creator sees the client's name and email once they join. The link token proves the client was invited, and the first client to open it owns the booking.

Demo logins (password `demo1234`): `chidinma@demo.gigvault` (creator), `tolu@demo.gigvault` (client).

## API

Busha-style envelope: `{"status": "success"|"error", "message": "...", "data": ..., "code": "..."}`. Auth: `Authorization: Bearer <token>` from sign-up or login.

| Method | Path | Who | What it does |
|---|---|---|---|
| POST | `/v1/auth/signup` · `/v1/auth/login` | anyone | `role`: creator or client |
| POST | `/v1/banks/resolve` · GET `/v1/banks` | anyone | Busha bank list and account-name check |
| POST | `/v1/creator/onboard` | creator | Busha recipient + Busha customer + KYC link |
| GET | `/v1/creator/wallet` | creator | Available / locked / frozen, in naira |
| POST | `/v1/creator/gigs` | creator | Create a gig link to send to the client |
| GET | `/v1/creator/gigs/{id}` | creator | Client, audit timeline, ledger |
| POST | `/v1/creator/cashout` | creator | Busha quote + transfer to her bank |
| GET | `/v1/pay/{id}?t=` | anyone with link | Gig preview |
| POST | `/v1/client/claim/{id}` | client | Add the linked gig to My bookings |
| GET | `/v1/client/bookings[/{id}]` | client | Bookings |
| POST | `/v1/client/bookings/{id}/checkout` | client | Busha payment request (NGN bank transfer or USDT) |
| POST | `/v1/client/bookings/{id}/complete` · `/complaints` · `/cancel` | client | Done, complain, cancel |
| POST | `/v1/admin/gigs/{id}/complaints/resolve` | admin key | `{"outcome":"worker"|"client"}` |
| GET | `/v1/admin/reconciliation` | admin key | Ledger vs Busha balances |
| POST | `/v1/webhooks/busha` | Busha | Signed webhooks |
| POST | `/v1/dev/gigs/{id}/simulate-payment` | dev only | Sends a signed payment webhook through the real path |

Errors use real HTTP codes: 400 validation, 401 not logged in or bad webhook signature, 403 wrong role, 404 not found (also for gigs that aren't yours), 409 invalid state or insufficient funds, 422 bank account not verified, 502 Busha error.

## Demo script (3 minutes)

1. **Problem (30s):** Chidinma, 51% of Nigerian creatives deal with payment disputes, banks won't escrow a ₦30,000 nail appointment.
2. **Creator phone (30s):** Chidinma taps *New gig link*, "Saturday wedding, ₦120,000", sends it on WhatsApp.
3. **Judge's phone (45s):** opens the link, sees who they're booking and the protection rules, signs up as a client, pays via Busha. Status turns *Locked*.
4. **Creator phone (20s):** "₦120,000 locked", ₦30,000 deposit already withdrawable.
5. **Judge marks done (30s):** countdown hits zero and the money lands in Chidinma's wallet. No one pressed anything.
6. **Cash out (15s):** to GTBank via a Busha quote + transfer.
7. **Gig detail (20s):** audit trail, double-entry ledger, reconciliation against Busha. Mention duplicate webhooks are ignored.

## Team

- Abdullah: Busha integration and webhooks
- Peter: ledger, state machine, worker
- ALIPHATIC: frontend, design, pitch
