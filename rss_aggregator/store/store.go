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

// Item is one persisted feed item. ID is the content hash (feed URL +
// link), so redeliveries collapse onto a single row.
type Item struct {
	ID          string    `json:"id"`
	FeedURL     string    `json:"feed_url"`
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	PublishedAt string    `json:"published_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// Store is the engine-agnostic contract every backend must implement.
// Tests target this interface so they can swap a fast in-memory sqlite
// impl in for any real backend without touching call sites.
type Store interface {
	// Save persists an item; duplicate IDs report a duplicate-key error
	// (callers Ack — the row already exists).
	Save(ctx context.Context, i Item) (string, error)
	// List returns newest-first items plus the total match count.
	List(ctx context.Context, q, feed string, limit, offset int) ([]Item, int, error)
	// Feeds returns distinct feed URLs, newest activity first.
	Feeds(ctx context.Context) ([]string, error)
	// Get returns one item by id, or ErrNotFound.
	Get(ctx context.Context, id string) (Item, error)
	// IsEmpty reports whether the store holds no items. A fresh container
	// (ephemeral sqlite) with an existing broker-side durable position
	// would otherwise permanently skip history the previous instance
	// already acked; the caller resets the consumer in that case so the
	// read model rebuilds from the stream.
	IsEmpty(ctx context.Context) (bool, error)
	Close() error
}

// ErrNotFound marks a missing item (rendered as 404).
var ErrNotFound = errors.New("aggregator: item not found")

// aggregatorStore is the single implementation of Store. It is identical
// for every engine; only the injected driver varies (placeholder style).
type aggregatorStore struct {
	db     *sql.DB
	driver db.Driver
}

func (s *aggregatorStore) Save(ctx context.Context, i Item) (string, error) {
	if i.CreatedAt.IsZero() {
		i.CreatedAt = time.Now().UTC()
	}
	const q = `INSERT INTO items (id, feed_url, title, link, published_at, created_at) VALUES (`
	qs := q + db.Placeholders(s.driver, 1, 6) + `)`
	_, err := s.db.ExecContext(ctx, qs,
		i.ID, i.FeedURL, i.Title, i.Link, i.PublishedAt, i.CreatedAt,
	)
	return i.ID, err
}

func (s *aggregatorStore) List(ctx context.Context, q, feed string, limit, offset int) ([]Item, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	p := func(n int) string { return db.Placeholder(s.driver, n) }

	where := `(` + p(1) + ` = '' OR feed_url = ` + p(2) + `)` +
		` AND (` + p(3) + ` = '' OR title LIKE ` + p(4) + ` ESCAPE '!' OR link LIKE ` + p(5) + ` ESCAPE '!')`

	like := "%" + escapeLIKE(q) + "%"

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, feed_url, title, link, published_at, created_at FROM items WHERE `+where+
			` ORDER BY published_at DESC, id ASC LIMIT `+p(6)+` OFFSET `+p(7),
		feed, feed, q, like, like, limit, offset)

	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = rows.Close() }()

	items := make([]Item, 0, limit)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.FeedURL, &it.Title, &it.Link, &it.PublishedAt, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, it)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM items WHERE `+where,
		feed, feed, q, like, like).Scan(&total); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// escapeLIKE escapes a user's search term so %, _, ! match literally.
// '!' is the LIKE escape character (see the ESCAPE '!' clauses): a
// backslash escape breaks MySQL string parsing, so it is avoided.
func escapeLIKE(s string) string {
	s = strings.ReplaceAll(s, `!`, `!!`)
	s = strings.ReplaceAll(s, `%`, `!%`)
	s = strings.ReplaceAll(s, `_`, `!_`)
	return s
}

func (s *aggregatorStore) Feeds(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT feed_url FROM items GROUP BY feed_url ORDER BY max(created_at) DESC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	feeds := make([]string, 0)
	for rows.Next() {
		var feed string
		if err := rows.Scan(&feed); err != nil {
			return nil, err
		}
		feeds = append(feeds, feed)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return feeds, nil
}

func (s *aggregatorStore) Get(ctx context.Context, id string) (Item, error) {
	var it Item

	err := s.db.QueryRowContext(ctx,
		`SELECT id, feed_url, title, link, published_at, created_at FROM items WHERE id = `+db.Placeholder(s.driver, 1),
		id).Scan(&it.ID, &it.FeedURL, &it.Title, &it.Link, &it.PublishedAt, &it.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Item{}, ErrNotFound
		}
		return Item{}, err
	}

	return it, nil
}

func (s *aggregatorStore) IsEmpty(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

func (s *aggregatorStore) Close() error {
	return s.db.Close()
}

// NewConfig builds a Store for the configured driver. sqlite is the
// zero-dependency, in-memory-capable backend used by tests.
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
