-- Invoices: when the client first opened the link.
ALTER TABLE gigs ADD COLUMN IF NOT EXISTS opened_at TIMESTAMPTZ;

-- Disputes: both sides get a window to agree a split before GigVault steps in.
ALTER TABLE complaints ADD COLUMN IF NOT EXISTS raised_by            TEXT NOT NULL DEFAULT 'client';
ALTER TABLE complaints ADD COLUMN IF NOT EXISTS negotiate_until      TIMESTAMPTZ;
ALTER TABLE complaints ADD COLUMN IF NOT EXISTS proposal_creator_bps INT;   -- share of the frozen money proposed for the creator
ALTER TABLE complaints ADD COLUMN IF NOT EXISTS proposed_by          TEXT;  -- creator | client
ALTER TABLE complaints ADD COLUMN IF NOT EXISTS outcome_creator_bps  INT;
ALTER TABLE complaints ADD COLUMN IF NOT EXISTS fee_usdt_micro       BIGINT NOT NULL DEFAULT 0;
