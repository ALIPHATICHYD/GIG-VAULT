-- GigVault schema. Money is always stored as integer minor units:
--   NGN in kobo (1 NGN = 100 kobo), USDT in micro-units (1 USDT = 1,000,000).

CREATE TABLE IF NOT EXISTS workers (
    id                 TEXT PRIMARY KEY,               -- WRK_...
    name               TEXT NOT NULL,
    email              TEXT,
    phone              TEXT,
    busha_customer_id  TEXT,                           -- Busha customer (X-BU-PROFILE-ID) once KYC'd
    busha_recipient_id TEXT,                           -- Busha recipient used for NGN bank payouts
    bank_name          TEXT,
    account_number     TEXT,
    account_name       TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS gigs (
    id                        TEXT PRIMARY KEY,        -- GIG_...
    worker_id                 TEXT NOT NULL REFERENCES workers(id),
    title                     TEXT NOT NULL,
    client_name               TEXT NOT NULL,
    client_email              TEXT NOT NULL,
    event_at                  TIMESTAMPTZ NOT NULL,
    amount_ngn_kobo           BIGINT NOT NULL CHECK (amount_ngn_kobo > 0),
    amount_usdt_micro         BIGINT,                  -- set once funds are converted
    rate_ngn_per_usdt         NUMERIC(18,6),
    deposit_bps               INT NOT NULL,
    freeze_bps                INT NOT NULL,
    complaint_window_seconds  INT NOT NULL,
    status                    TEXT NOT NULL,
    pay_in_method             TEXT NOT NULL DEFAULT 'temporary_bank_account',
    busha_payment_request_id  TEXT,
    busha_quote_id            TEXT,
    payment_reference         TEXT UNIQUE,             -- our reference, echoed back by Busha
    payment_details           JSONB,                   -- bank account / address shown to the client
    done_at                   TIMESTAMPTZ,
    release_at                TIMESTAMPTZ,
    version                   INT NOT NULL DEFAULT 0,  -- optimistic locking
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS gigs_worker_idx ON gigs(worker_id);

-- Double-entry ledger. A transaction moves money from one account to another and
-- writes exactly one debit and one credit. Rows are never updated or deleted.
CREATE TABLE IF NOT EXISTS ledger_txns (
    id              TEXT PRIMARY KEY,                  -- TXN_...
    idempotency_key TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL,                     -- fund, deposit_release, release, freeze, unfreeze, refund, payout, payout_reversal
    gig_id          TEXT REFERENCES gigs(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id           BIGSERIAL PRIMARY KEY,
    txn_id       TEXT NOT NULL REFERENCES ledger_txns(id),
    account      TEXT NOT NULL,                        -- external:client, escrow:GIG_x, frozen:GIG_x, worker:WRK_x, external:payout, external:refund
    direction    TEXT NOT NULL CHECK (direction IN ('debit','credit')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency     TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ledger_entries_account_idx ON ledger_entries(account);

CREATE OR REPLACE FUNCTION forbid_ledger_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS ledger_entries_append_only ON ledger_entries;
CREATE TRIGGER ledger_entries_append_only BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION forbid_ledger_mutation();

CREATE TABLE IF NOT EXISTS complaints (
    id                TEXT PRIMARY KEY,                -- CMP_...
    gig_id            TEXT NOT NULL REFERENCES gigs(id),
    reason            TEXT NOT NULL,
    frozen_usdt_micro BIGINT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'open',    -- open, resolved_worker, resolved_client
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at       TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS payouts (
    id                TEXT PRIMARY KEY,                -- PAY_...
    worker_id         TEXT NOT NULL REFERENCES workers(id),
    amount_usdt_micro BIGINT NOT NULL,
    amount_ngn_kobo   BIGINT NOT NULL,
    busha_quote_id    TEXT,
    busha_transfer_id TEXT UNIQUE,
    status            TEXT NOT NULL,                   -- processing, completed, failed
    failure_reason    TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every Busha webhook is stored raw before anything else happens.
-- The unique key makes redelivered webhooks a no-op.
CREATE TABLE IF NOT EXISTS busha_events (
    id           BIGSERIAL PRIMARY KEY,
    event        TEXT NOT NULL,
    reference    TEXT NOT NULL,
    payload      JSONB NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    error        TEXT,
    UNIQUE (event, reference)
);

-- A small Postgres-backed job queue (SELECT ... FOR UPDATE SKIP LOCKED).
CREATE TABLE IF NOT EXISTS jobs (
    id          BIGSERIAL PRIMARY KEY,
    kind        TEXT NOT NULL,
    payload     JSONB NOT NULL,
    dedupe_key  TEXT UNIQUE,
    run_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts    INT NOT NULL DEFAULT 0,
    last_error  TEXT,
    locked_at   TIMESTAMPTZ,
    done_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS jobs_ready_idx ON jobs(run_at) WHERE done_at IS NULL;

CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    gig_id      TEXT,
    worker_id   TEXT,
    actor       TEXT NOT NULL,                         -- client, worker, system, busha
    action      TEXT NOT NULL,
    from_status TEXT,
    to_status   TEXT,
    details     JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_gig_idx ON audit_log(gig_id);
