CREATE TABLE IF NOT EXISTS outbox (
    id            VARCHAR(36)   NOT NULL,
    feed_url      VARCHAR(2048) NOT NULL,
    title         VARCHAR(512)  NOT NULL,
    link          VARCHAR(2048) NOT NULL,
    webhook_url   VARCHAR(2048) NOT NULL,
    status        VARCHAR(16)   NOT NULL DEFAULT 'pending',
    error         VARCHAR(1024) NULL,
    created_at    DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at  DATETIME      NULL,
    PRIMARY KEY (id)
);
CREATE INDEX idx_outbox_status ON outbox(status);
