ALTER TABLE outbox ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE outbox ADD COLUMN next_retry_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_outbox_retry ON outbox(status, next_retry_at);
