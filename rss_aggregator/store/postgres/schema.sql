CREATE TABLE IF NOT EXISTS items (
    id            TEXT        PRIMARY KEY,
    feed_url      TEXT        NOT NULL,
    title         TEXT        NOT NULL,
    link          TEXT        NOT NULL,
    published_at  TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_items_created ON items(created_at);
CREATE INDEX IF NOT EXISTS idx_items_feed ON items(feed_url);
