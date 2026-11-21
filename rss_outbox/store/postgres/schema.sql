CREATE TABLE IF NOT EXISTS outbox (
    id            TEXT        PRIMARY KEY,
    feed_url      TEXT        NOT NULL,
    title         TEXT        NOT NULL,
    link          TEXT        NOT NULL,
    published_at  TEXT        NOT NULL DEFAULT '',
    webhook_url   TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'pending',
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_outbox_status ON outbox(status);
