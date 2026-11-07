-- Count repeat deliveries of the same Busha webhook, so the admin view can
-- show that duplicates were seen and ignored.
ALTER TABLE busha_events ADD COLUMN IF NOT EXISTS duplicates        INT NOT NULL DEFAULT 0;
ALTER TABLE busha_events ADD COLUMN IF NOT EXISTS last_duplicate_at TIMESTAMPTZ;
