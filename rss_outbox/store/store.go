package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverMySQL    Driver = "mysql"
	DriverPostgres Driver = "postgres"
)

type Config struct {
	Driver Driver
	DSN    string
}

// Entry is a single outbox record, independent of the storage engine.
type Entry struct {
	ID          string     `json:"id"`
	FeedURL     string     `json:"feed_url"`
	Title       string     `json:"title"`
	Link        string     `json:"link"`
	WebhookURL  string     `json:"webhook_url"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}

// Status is the aggregate count of outbox rows per lifecycle state.
type Status struct {
	Pending   int `json:"pending"`
	Delivered int `json:"delivered"`
	Failed    int `json:"failed"`
}

// Store is the engine-agnostic contract every backend must implement.
// Tests target this interface so they can swap a fast in-memory sqlite
// impl in for any real backend without touching call sites.
type Store interface {
	Save(ctx context.Context, e Entry) (string, error)
	GetPending(ctx context.Context) ([]Entry, error)
	MarkDelivered(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, errMsg string) error
	Status(ctx context.Context) (Status, error)
	Close() error
}

// dialect captures the only things that differ across engines: the
// database/sql driver name, the schema DDL, and the placeholder style.
type dialect struct {
	driver      string
	schema      string
	placeholder func(int) string
}

func (d dialect) placeholders(n, total int) string {
	parts := make([]string, total)
	for i := range total {
		parts[i] = d.placeholder(n + i)
	}
	var out strings.Builder
	out.WriteString(parts[0])
	for _, p := range parts[1:] {
		out.WriteString(", ")
		out.WriteString(p)
	}
	return out.String()
}

// outboxStore is the single implementation of Store. It is identical for
// every engine; only the injected dialect varies.
type outboxStore struct {
	db      *sql.DB
	dialect dialect
}

func newOutboxStore(d dialect, dsn string) (*outboxStore, error) {
	db, err := sql.Open(d.driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("%s open: %w", d.driver, err)
	}
	if _, err := db.Exec(d.schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s migrate: %w", d.driver, err)
	}
	return &outboxStore{db: db, dialect: d}, nil
}

func (s *outboxStore) Save(ctx context.Context, e Entry) (string, error) {
	const q = `INSERT INTO outbox (id, feed_url, title, link, webhook_url, status, error, created_at, delivered_at) VALUES (`
	qs := q + s.dialect.placeholders(1, 9) + `)`
	_, err := s.db.ExecContext(ctx, qs,
		e.ID, e.FeedURL, e.Title, e.Link, e.WebhookURL, e.Status, e.Error, e.CreatedAt, e.DeliveredAt,
	)
	return e.ID, err
}

func (s *outboxStore) GetPending(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, feed_url, title, link, webhook_url, status, error, created_at, delivered_at
		 FROM outbox WHERE status = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []Entry
	for rows.Next() {
		var e Entry
		var errMsg sql.NullString
		var deliveredAt sql.NullTime
		if err := rows.Scan(&e.ID, &e.FeedURL, &e.Title, &e.Link, &e.WebhookURL, &e.Status, &errMsg, &e.CreatedAt, &deliveredAt); err != nil {
			return nil, err
		}
		e.Error = errMsg.String
		if deliveredAt.Valid {
			v := deliveredAt.Time
			e.DeliveredAt = &v
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

func (s *outboxStore) MarkDelivered(ctx context.Context, id string) error {
	q := `UPDATE outbox SET status = 'delivered', delivered_at = CURRENT_TIMESTAMP, error = NULL WHERE id = ` + s.dialect.placeholder(1)
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

func (s *outboxStore) MarkFailed(ctx context.Context, id string, errMsg string) error {
	q := `UPDATE outbox SET status = 'failed', error = ` + s.dialect.placeholder(1) + ` WHERE id = ` + s.dialect.placeholder(2)
	_, err := s.db.ExecContext(ctx, q, errMsg, id)
	return err
}

func (s *outboxStore) Status(ctx context.Context) (Status, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM outbox GROUP BY status`)
	if err != nil {
		return Status{}, err
	}
	defer func() { _ = rows.Close() }()

	var st Status
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return Status{}, err
		}
		switch status {
		case "pending":
			st.Pending = int(count)
		case "delivered":
			st.Delivered = int(count)
		case "failed":
			st.Failed = int(count)
		}
	}
	return st, rows.Err()
}

func (s *outboxStore) Close() error {
	return s.db.Close()
}

// NewConfig builds a Store for the configured driver. sqlite is the
// zero-dependency, in-memory-capable backend used by tests.
func NewConfig(cfg Config) (Store, error) {
	switch cfg.Driver {
	case DriverSQLite:
		return newSQLiteStore(cfg.DSN)
	case DriverMySQL:
		return newMySQLStore(cfg.DSN)
	case DriverPostgres:
		return newPostgresStore(cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported driver: %s", cfg.Driver)
	}
}

// qmark renders a positional placeholder for SQLite and MySQL.
func qmark(int) string { return "?" }

// dollar renders a numbered placeholder for PostgreSQL.
func dollar(n int) string { return "$" + strconv.Itoa(n) }
