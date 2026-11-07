-- Creator onboarding and client pay links.

ALTER TABLE workers ADD COLUMN IF NOT EXISTS craft      TEXT;
ALTER TABLE workers ADD COLUMN IF NOT EXISTS id_type    TEXT;                -- bvn | nin
ALTER TABLE workers ADD COLUMN IF NOT EXISTS id_last4   TEXT;                -- we never store the full BVN/NIN
ALTER TABLE workers ADD COLUMN IF NOT EXISTS kyc_status TEXT NOT NULL DEFAULT 'not_started';
ALTER TABLE workers ADD COLUMN IF NOT EXISTS kyc_link   TEXT;
ALTER TABLE workers ADD COLUMN IF NOT EXISTS bank_code  TEXT;

-- A gig is created by the creator first; the client fills in their details on the pay link.
ALTER TABLE gigs ALTER COLUMN client_name  DROP NOT NULL;
ALTER TABLE gigs ALTER COLUMN client_email DROP NOT NULL;
ALTER TABLE gigs ADD COLUMN IF NOT EXISTS client_token TEXT;              -- secret in the pay link
ALTER TABLE gigs ADD COLUMN IF NOT EXISTS note         TEXT;
ALTER TABLE gigs ADD COLUMN IF NOT EXISTS pay_currency TEXT;              -- NGN | USDT, chosen by client
ALTER TABLE gigs ALTER COLUMN pay_in_method DROP NOT NULL;
ALTER TABLE gigs ALTER COLUMN pay_in_method DROP DEFAULT;
