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
    delivered_at  TIMESTAMPTZ,
    attempts      INTEGER     NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_outbox_status ON outbox(status);
CREATE INDEX IF NOT EXISTS idx_outbox_retry ON outbox(status, next_retry_at);
