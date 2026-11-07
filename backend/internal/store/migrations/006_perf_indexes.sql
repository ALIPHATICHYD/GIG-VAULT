-- The creator activity feed filters audit_log by worker_id.
CREATE INDEX IF NOT EXISTS audit_worker_idx ON audit_log(worker_id, id DESC);
