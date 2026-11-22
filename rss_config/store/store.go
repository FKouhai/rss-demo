package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	db "github.com/FKouhai/rss-demo/libs/db"
)

type Config struct {
	Driver db.Driver
	DSN    string
}

// Feed is one configured source. WebhookURL is the per-feed override
// (empty means the global default applies); Active lets an admin
// disable a feed without losing its mapping.
type Feed struct {
	URL        string    `json:"url"`
	WebhookURL string    `json:"webhook_url,omitempty"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// FeedEntry is the poller-facing shape: only URL + override.
type FeedEntry struct {
	URL        string `json:"url"`
	WebhookURL string `json:"webhook_url,omitempty"`
}

// Snapshot is the exact shape the poller consumes (mirrors its
// config.json): legacy URL list + global default + per-feed overrides.
type Snapshot struct {
	RSSFeeds   []string    `json:"rss_feeds"`
	WebhookURL string      `json:"webhook_url"`
	Feeds      []FeedEntry `json:"feeds"`
}

// Store is the engine-agnostic contract every backend must implement.
type Store interface {
	// UpsertFeed inserts or replaces a feed (reactivating it).
	UpsertFeed(ctx context.Context, url, webhookURL string) error
	// EnsureFeed inserts a feed only when missing; an existing row
	// (and its webhook mapping) is left untouched. Used for the
	// legacy URL list, which carries no override information.
	EnsureFeed(ctx context.Context, url string) error
	// DeleteFeed removes a feed; reports whether a row existed.
	DeleteFeed(ctx context.Context, url string) (bool, error)
	// ListFeeds returns all feeds, most recently updated first.
	ListFeeds(ctx context.Context) ([]Feed, error)
	// GetDefaultWebhook returns the global default ("" when unset).
	GetDefaultWebhook(ctx context.Context) (string, error)
	// SetDefaultWebhook replaces the global default ("" clears it).
	SetDefaultWebhook(ctx context.Context, webhookURL string) error
	// Snapshot renders the poller-facing view (active feeds only).
	Snapshot(ctx context.Context) (Snapshot, error)
	// IsEmpty reports whether no feeds are configured. The caller
	// seeds from the seed file exactly once in that case.
	IsEmpty(ctx context.Context) (bool, error)
	Close() error
}

// ErrNotFound marks a missing row.
var ErrNotFound = errors.New("config: not found")

// settingDefaultWebhook is the settings key for the global default.
const settingDefaultWebhook = "default_webhook_url"

// configStore is the single implementation of Store. It is identical
// for every engine; only the injected driver varies (placeholder style).
type configStore struct {
	db     *sql.DB
	driver db.Driver
}

func (s *configStore) UpsertFeed(ctx context.Context, url, webhookURL string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return errors.New("config: feed url is empty")
	}
	now := time.Now().UTC()
	p := func(n int) string { return db.Placeholder(s.driver, n) }
	// Try update first so created_at survives re-pushes.
	// active is passed as a bool placeholder (not literal 1): postgres
	// BOOLEAN rejects integer expressions, while sqlite/mysql coerce
	// bool true to 1.
	res, err := s.db.ExecContext(ctx,
		`UPDATE feeds SET webhook_url = `+p(1)+`, active = `+p(2)+`, updated_at = `+p(3)+` WHERE url = `+p(4),
		webhookURL, true, now, url)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO feeds (url, webhook_url, active, created_at, updated_at) VALUES (`+
			db.Placeholders(s.driver, 1, 5)+`)`,
		url, webhookURL, true, now, now)
	return err
}

func (s *configStore) EnsureFeed(ctx context.Context, url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return errors.New("config: feed url is empty")
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feeds (url, webhook_url, active, created_at, updated_at) VALUES (`+
			db.Placeholders(s.driver, 1, 5)+`)`,
		url, "", true, now, now)
	if err != nil {
		if db.IsDuplicateKey(err) {
			return nil
		}
		return err
	}
	return nil
}

func (s *configStore) DeleteFeed(ctx context.Context, url string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM feeds WHERE url = `+db.Placeholder(s.driver, 1), url)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *configStore) ListFeeds(ctx context.Context) ([]Feed, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT url, webhook_url, active, created_at, updated_at FROM feeds ORDER BY updated_at DESC, url ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	feeds := make([]Feed, 0)
	for rows.Next() {
		var f Feed
		// active arrives as INTEGER 0/1 on sqlite+mysql but as BOOLEAN
		// on postgres; scan wide and coerce (unit-tested per driver).
		var activeAny any
		if err := rows.Scan(&f.URL, &f.WebhookURL, &activeAny, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		f.Active = sqlBool(activeAny)
		feeds = append(feeds, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return feeds, nil
}

func (s *configStore) GetDefaultWebhook(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = `+db.Placeholder(s.driver, 1),
		settingDefaultWebhook).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

func (s *configStore) SetDefaultWebhook(ctx context.Context, webhookURL string) error {
	p := func(n int) string { return db.Placeholder(s.driver, n) }
	res, err := s.db.ExecContext(ctx,
		`UPDATE settings SET value = `+p(1)+` WHERE key = `+p(2),
		webhookURL, settingDefaultWebhook)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (`+db.Placeholders(s.driver, 1, 2)+`)`,
		settingDefaultWebhook, webhookURL)
	return err
}

func (s *configStore) Snapshot(ctx context.Context) (Snapshot, error) {
	feeds, err := s.ListFeeds(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	def, err := s.GetDefaultWebhook(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{WebhookURL: def}
	for _, f := range feeds {
		if !f.Active {
			continue
		}
		snap.RSSFeeds = append(snap.RSSFeeds, f.URL)
		if f.WebhookURL != "" {
			snap.Feeds = append(snap.Feeds, FeedEntry{URL: f.URL, WebhookURL: f.WebhookURL})
		}
	}
	return snap, nil
}

func (s *configStore) IsEmpty(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM feeds`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

func (s *configStore) Close() error {
	return s.db.Close()
}

// sqlBool coerces a scanned ACTIVE value across engines: sqlite and
// MySQL report INTEGER 0/1 (int64, sometimes []byte), PostgreSQL
// reports BOOLEAN (bool). A direct typed scan breaks on at least one
// engine (seen live: pq bool into int64), so scan wide and convert.
func sqlBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int:
		return t != 0
	case []byte:
		s := strings.ToLower(string(t))
		return s == "1" || s == "true" || s == "t"
	case string:
		s := strings.ToLower(t)
		return s == "1" || s == "true" || s == "t"
	case nil:
		return false
	default:
		return false
	}
}

// NewConfig builds a Store for the configured driver. sqlite is the
// zero-dependency backend used by tests and local compose.
func NewConfig(cfg Config) (Store, error) {
	switch cfg.Driver {
	case db.DriverSQLite:
		return newSQLiteStore(cfg.DSN)
	case db.DriverMySQL:
		return newMySQLStore(cfg.DSN)
	case db.DriverPostgres:
		return newPostgresStore(cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported driver: %s", cfg.Driver)
	}
}
