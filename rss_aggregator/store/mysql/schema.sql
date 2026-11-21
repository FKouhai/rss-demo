CREATE TABLE IF NOT EXISTS items (
    id            VARCHAR(64)   NOT NULL,
    feed_url      VARCHAR(2048) NOT NULL,
    title         VARCHAR(512)  NOT NULL,
    link          VARCHAR(2048) NOT NULL,
    published_at  VARCHAR(64)   NOT NULL DEFAULT '',
    created_at    DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id)
);
CREATE INDEX idx_items_created ON items(created_at);
CREATE INDEX idx_items_feed ON items(feed_url);
