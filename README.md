# GigVault

**Invoices for creators, with the money held safely until the work is done.** Built on Busha for Busha Build Day (7 Nov 2026).

Abdullah is a photographer who shoots weddings, birthdays and brand campaigns. Today he sends an invoice with his account number and hopes the client pays. With GigVault:

1. **She sends an invoice** to a client by name: amount, what it's for, and how much she needs **upfront to start work** (0 to 70%, default 20%).
2. **The client opens the link**, signs up or logs in, and sees it under **Pending payments**.
3. **The client pays** by naira bank transfer or USDT from abroad (a Busha payment request). Busha settles it as USDT and GigVault holds it, tagged to the invoice.
4. **The upfront % goes to her straight away.**
5. **The client confirms the work is done** and the rest is released. A short complaint window protects the client.
6. **Disputes:** part of the money is frozen. Both sides get **3 days** to agree a split for free. If they can't, **GigVault decides and keeps a 10% resolution fee**. Frozen money never goes back on its own.
7. **She cashes out** to her bank in naira (Busha quote + transfer).

Both sides see the same **timeline**, from "invoice sent" to "paid out".

---

## Run it in 5 steps

You need: **Go 1.23+**, **Node 20+**, a free **[Neon](https://neon.tech)** Postgres database.

**1. Clone**
```bash
git clone https://github.com/ALIPHATICHYD/GIG-VAULT.git && cd GIG-VAULT
```

**2. Database.** In Neon, create a project, then copy the connection string.

**3. Backend**
```bash
cp backend/.env.example backend/.env
# open backend/.env and paste your Neon string into DATABASE_URL
make api          # http://localhost:8080/healthz should say "ok"
```
Tables are created automatically. It starts in **mock mode** (yellow MOCK BUSHA badge) so you can work before you have Busha keys.

**4. Frontend** (new terminal)
```bash
cp web/.env.example web/.env
make web          # open http://localhost:5173
```

**5. Log in with the demo accounts** (password `demo1234`)
- Creator: `abdullah@demo.gigvault`
- Client: `aminat@demo.gigvault`

Use two browsers (or one normal + one incognito) so you can be both at once.

### Switch to the real Busha sandbox

In `backend/.env`:
```
BUSHA_MODE=live
BUSHA_SECRET_KEY=...        # Busha dashboard > Settings > Developer tools > API Tokens
BUSHA_WEBHOOK_SECRET=...    # Busha dashboard > Settings > Developer tools > Webhooks
```
Busha can't send webhooks to localhost, so run `make tunnel` (cloudflared) and register `https://<your-tunnel>/v1/webhooks/busha` in the dashboard.

### Demo settings

For a live demo, shorten the timers in `backend/.env`:
```
COMPLAINT_WINDOW=60s
DISPUTE_WINDOW=2m
INVOICE_EXPIRY=30s
```

### Start the demo from a clean slate

```bash
make seed                            # or: cd backend && go run ./cmd/seed -reset
```

This **wipes the database** and rebuilds a known state: the two demo accounts and
three invoices, one awaiting payment (the one you let a judge pay), one paid and
counting down to release, one finished and cashed out to the bank. It prints the
pay link, the wallet totals and the ledger position, so you know the numbers
before you are asked about them.

It builds that state by calling the real service — checkout, funding, release,
cash-out — so the timelines and ledger entries are genuine, not fixtures.

`-reset` truncates every table, because `ledger_entries` rejects `DELETE` by
design and a partial clean-up is impossible. It refuses to run against
`BUSHA_MODE=live` unless you add `-force`. Use `-countdown 20m` to give the
releasing invoice more time than the default 10 minutes.

---

## Who does what in the code

| Person | Folder | What's there |
|---|---|---|
| Abdullah | `backend/internal/busha/` | Busha API client, webhook signature check, mock |
| Peter | `backend/internal/gigs/`, `backend/internal/ledger/` | Invoices, escrow rules, disputes, ledger, wallet, cash-out |
| ALIPHATIC | `web/src/pages/` | `Creator.tsx`, `Client.tsx`, `Auth.tsx` screens; shared UI in `web/src/ui.tsx` |

### Running the tests

```bash
cd backend && go vet ./... && go test ./...     # or: make test
```

The ledger and invoice-lifecycle tests need a real Postgres, because what they check is enforced
by the database: the append-only trigger, the unique idempotency key, transaction rollback. Set
`TEST_DATABASE_URL` to a throwaway database and they run; leave it unset and they skip, so the
command above passes either way.

```bash
TEST_DATABASE_URL='postgresql://...neon.tech/test?sslmode=require' go test ./...
```

Create a **Neon branch** for it rather than using your dev database. Each test runs in its own
schema, created and dropped automatically, so nothing else in the database is touched.

Use the **direct** connection string, not the `-pooler` one. Neon's pooled endpoint rejects the
`search_path` parameter the tests use to isolate themselves (*"unsupported startup parameter …
please use unpooled connection"*), so the harness strips `-pooler` for you and stops with a clear
error if a connection still slips through. Worth knowing why: a `SET search_path` that lands on a
pooled connection leaks to whichever client is handed that shared server connection next, which
shows up as unrelated queries failing with `relation "gigs" does not exist`.

**These tests are slow against Neon** — roughly 5–25s each, and 10+ minutes for the suite, because
every assertion is an internet round trip and each test migrates its own schema. Hence the
`-timeout 40m` in `make test`; Go's default is 10 minutes and the suite will be killed partway
without it. A local Postgres is dramatically faster if you run them often:

```bash
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=gigvault -e POSTGRES_DB=gigvault postgres:16
TEST_DATABASE_URL='postgres://postgres:gigvault@localhost:5432/gigvault?sslmode=disable' go test ./...
```

---

## How it's built

```
React + Vite + Tailwind  ──►  Go API (chi)  ──►  Postgres (Neon)
                                  │                ├─ users (creator | client)
                                  │                ├─ gigs = invoices (state machine)
                                  │                ├─ ledger (double-entry, append-only)
                                  │                ├─ complaints (disputes)
                                  │                ├─ busha_events (raw webhooks, de-duplicated)
                                  │                ├─ jobs (queue: auto-release, escalation, webhooks)
                                  ▼                └─ audit_log (the timeline)
                         Busha Business API  ◄── signed webhooks
```

- **Escrow is our ledger.** Busha has no escrow product, so every naira is tracked as a debit + credit in our own append-only ledger on top of Busha balances. A database trigger blocks edits and deletes.
- **Webhooks are safe to replay.** Verified with `X-BU-SIGNATURE` (HMAC-SHA256), stored raw, de-duplicated, then processed by a background worker.
- **Cash-out can't lose money.** Funds are reserved first; if Busha's transfer fails, they're put back automatically.
- **Reconciliation:** `GET /v1/admin/reconciliation` compares our ledger with Busha's `GET /v1/balances`.
- **Each side only sees its own screens.** Wrong role = 403; someone else's invoice = 404.

### Busha APIs used

| Step | Busha endpoint |
|---|---|
| Onboarding: list banks, check account name | `GET /v1/banks`, `POST /v1/recipients/resolve-bank-account` |
| Onboarding: save payout account | `POST /v1/recipients` |
| Onboarding: identity | `POST /v1/customers`, `POST /v1/customers/{id}/kyc-link` |
| Client pays (NGN bank transfer or USDT priced in NGN) | `POST /v1/payments/requests` |
| Payment confirmed | webhooks `payment_request.completed`, `deposit.success` |
| Cash out to bank | `POST /v1/quotes` (USDT→NGN, `pay_out: bank_transfer`), `POST /v1/transfers`, webhooks `transfer.*` |
| Reconciliation | `GET /v1/balances` |

### API

Responses look like Busha's: `{"status": "success"|"error", "message": "...", "data": ..., "code": "..."}`. Log in to get a token, then send `Authorization: Bearer <token>`.

| Who | Method and path | What it does |
|---|---|---|
| anyone | `POST /v1/auth/signup`, `POST /v1/auth/login` | `role`: `creator` or `client`. Login allows 5 tries a minute per email and per IP, then 429 `too_many_attempts` |
| creator | `POST /v1/creator/onboard` | Bank, BVN/NIN, Busha recipient + KYC link |
| creator | `GET /v1/creator/wallet` | Withdrawable, locked, frozen (in naira) |
| creator | `POST /v1/creator/gigs` | Send an invoice: `client_name`, `client_email`, `amount_ngn`, `upfront_percent`, `title`, `event_at` |
| creator | `GET /v1/creator/gigs/{id}` | Client, timeline, ledger |
| creator | `POST /v1/creator/gigs/{id}/void` | Withdraw an unpaid invoice so it can't be paid |
| creator | `POST /v1/creator/gigs/{id}/dispute/propose` · `/accept` | Agree a split |
| creator | `POST /v1/creator/cashout` | `{"amount_ngn": "50000"}` |
| anyone with link | `GET /v1/pay/{id}?t=` | Invoice preview |
| client | `POST /v1/client/claim/{id}` | Add a linked invoice to the account |
| client | `GET /v1/client/bookings` | Pending payments and paid invoices |
| client | `POST /v1/client/bookings/{id}/checkout` | Pay: `{"currency": "NGN"}` or `"USDT"` |
| client | `POST /v1/client/bookings/{id}/complete` · `/complaints` · `/cancel` | Done, dispute, cancel |
| client | `POST /v1/client/bookings/{id}/dispute/propose` · `/accept` | Agree a split |
| admin (`X-Admin-Key`) | `GET /v1/admin/disputes` | The arbitration queue: escalated disputes, both sides' last offer |
| admin (`X-Admin-Key`) | `POST /v1/admin/gigs/{id}/dispute/resolve` | After 3 days: `{"creator_percent": 50}`, 10% fee |
| admin | `GET /v1/admin/reconciliation` | Ledger vs Busha |
| Busha | `POST /v1/webhooks/busha` | Signed webhooks |
| dev only | `POST /v1/dev/gigs/{id}/simulate-payment` | Fake a signed payment webhook |

---

## 3-minute demo script

1. **Problem (30s):** creators chase payments; 51% of Nigerian creatives deal with payment disputes; banks won't escrow a ₦30,000 job.
2. **Creator phone (30s):** Abdullah sends an invoice: "Instagram campaign: 3 reels, ₦150,000, 50% upfront". He shares it on WhatsApp.
3. **Judge's phone (45s):** opens the link and signs up as a client. The invoice is under Pending payments. They pay through Busha and it shows "Locked".
4. **Creator phone (15s):** ₦75,000 is already withdrawable. The rest shows as locked.
5. **Dispute (30s):** the judge raises "only 2 of 3 reels". Abdullah proposes 70/30 and the judge accepts. It settles at no fee, and the timeline shows every step.
6. **Close (30s):** cash out to bank through Busha; show the ledger and reconciliation. If they can't agree within 3 days, GigVault decides and earns 10%. That's the business model.
