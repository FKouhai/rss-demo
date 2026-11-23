ALTER TABLE outbox ADD COLUMN attempts INT NOT NULL DEFAULT 0;
ALTER TABLE outbox ADD COLUMN next_retry_at DATETIME NULL;
CREATE INDEX idx_outbox_retry ON outbox(status, next_retry_at);
