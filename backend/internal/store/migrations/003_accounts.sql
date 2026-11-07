-- One account system for both sides. role decides which screens you see.
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,                         -- USR_...
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    phone         TEXT,
    role          TEXT NOT NULL CHECK (role IN ('creator','client')),
    password_hash TEXT NOT NULL,
    worker_id     TEXT UNIQUE REFERENCES workers(id),      -- set after creator onboarding
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE gigs ADD COLUMN IF NOT EXISTS client_user_id TEXT REFERENCES users(id);
CREATE INDEX IF NOT EXISTS gigs_client_user_idx ON gigs(client_user_id);
