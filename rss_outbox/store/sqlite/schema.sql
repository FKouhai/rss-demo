CREATE TABLE IF NOT EXISTS outbox (
    id            TEXT     PRIMARY KEY,
    feed_url      TEXT     NOT NULL,
    title         TEXT     NOT NULL,
    link          TEXT     NOT NULL,
    webhook_url   TEXT     NOT NULL,
    status        TEXT     NOT NULL DEFAULT 'pending',
    error         TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at  DATETIME
);
CREATE INDEX IF NOT EXISTS idx_outbox_status ON outbox(status);
