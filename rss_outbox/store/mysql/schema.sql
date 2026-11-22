CREATE TABLE IF NOT EXISTS outbox (
    id            VARCHAR(36)   NOT NULL,
    feed_url      TEXT          NOT NULL,
    title         TEXT          NOT NULL,
    link          TEXT          NOT NULL,
    published_at  VARCHAR(64)   NOT NULL DEFAULT '',
    webhook_url   TEXT          NOT NULL,
    status        VARCHAR(16)   NOT NULL DEFAULT 'pending',
    error         TEXT          NULL,
    created_at    DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at  DATETIME      NULL,
    attempts      INTEGER       NOT NULL DEFAULT 0,
    next_retry_at DATETIME      NULL,
    PRIMARY KEY (id)
);
CREATE INDEX idx_outbox_status ON outbox(status);
CREATE INDEX idx_outbox_retry ON outbox(status, next_retry_at);
