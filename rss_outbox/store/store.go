package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/FKouhai/rss-demo/libs/db"
)

// Driver is the SQL backend. Aliased to the shared libs/db definition so
// every service names the same drivers; the consts below preserve the
// service-local spelling.
type Driver = db.Driver

const (
	DriverSQLite   = db.DriverSQLite
	DriverMySQL    = db.DriverMySQL
	DriverPostgres = db.DriverPostgres
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
	PublishedAt string     `json:"published_at"`
	WebhookURL  string     `json:"webhook_url"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	// Attempts counts delivery drives (initial + sweeper re-drives);
	// NextRetryAt gates the sweeper. NULL (pre-retry rows) means due.
	Attempts    int        `json:"attempts"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
}

// Status is the aggregate count of outbox rows per lifecycle state.
type Status struct {
	Pending   int `json:"pending"`
	Delivered int `json:"delivered"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

// Store is the engine-agnostic contract every backend must implement.
// Tests target this interface so they can swap a fast in-memory sqlite
// impl in for any real backend without touching call sites.
type Store interface {
	Save(ctx context.Context, e Entry) (string, error)
	GetPending(ctx context.Context) ([]Entry, error)
	// GetDueForRetry returns up to limit pending rows whose backoff has
	// elapsed (or which predate the retry columns and carry NULL).
	GetDueForRetry(ctx context.Context, now time.Time, limit int) ([]Entry, error)
	// NoteAttempt records one delivery drive: attempts+1, next due at
	// next. Callers compute next via BackoffForAttempt.
	NoteAttempt(ctx context.Context, id string, next time.Time) error
	MarkDelivered(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, errMsg string) error
	// MarkSkipped terminally retires a row that must not be delivered
	// (e.g. notify's stale-item gate); reason is kept in Error.
	MarkSkipped(ctx context.Context, id string, reason string) error
	Status(ctx context.Context) (Status, error)
	Close() error
}

// Retry policy: the first re-drive waits InitialRetryDelay after ingest
// (normal flows ack in seconds, so only lost acks wait it out), then
// exponential backoff per attempt capped at BackoffMax. MaxAttempts
// terminally fails the row. All tuning in one place.
const (
	MaxAttempts       = 10
	SweepInterval     = time.Minute
	SweepBatchLimit   = 100
	BootReplayLimit   = 1000
	InitialRetryDelay = 5 * time.Minute
	BackoffBase       = time.Minute
	BackoffMax        = time.Hour
)

// BackoffForAttempt returns the delay after attempt n (0-based): base
// doubling per attempt, capped. Pure for testability.
func BackoffForAttempt(n int) time.Duration {
	d := BackoffBase
	for i := 0; i < n && d < BackoffMax; i++ {
		d *= 2
		if d > BackoffMax {
			return BackoffMax
		}
	}
	return d
}

// outboxStore is the single implementation of Store. It is identical for
// every engine; only the injected driver varies (placeholder style).
type outboxStore struct {
	db     *sql.DB
	driver db.Driver
}

func (s *outboxStore) Save(ctx context.Context, e Entry) (string, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	next := e.CreatedAt.Add(InitialRetryDelay)
	const q = `INSERT INTO outbox (id, feed_url, title, link, published_at, webhook_url, status, error, created_at, delivered_at, attempts, next_retry_at) VALUES (`
	qs := q + db.Placeholders(s.driver, 1, 12) + `)`
	_, err := s.db.ExecContext(ctx, qs,
		e.ID, e.FeedURL, e.Title, e.Link, e.PublishedAt, e.WebhookURL, e.Status, e.Error, e.CreatedAt, e.DeliveredAt,
		0, next,
	)
	return e.ID, err
}

// scanEntries drains rows into Entries, coercing the nullable
// delivered_at/next_retry_at per engine.
func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var items []Entry
	for rows.Next() {
		var e Entry
		var errMsg sql.NullString
		var deliveredAt sql.NullTime
		var nextRetryAt sql.NullTime
		if err := rows.Scan(&e.ID, &e.FeedURL, &e.Title, &e.Link, &e.PublishedAt, &e.WebhookURL, &e.Status, &errMsg, &e.CreatedAt, &deliveredAt, &e.Attempts, &nextRetryAt); err != nil {
			return nil, err
		}
		e.Error = errMsg.String
		if deliveredAt.Valid {
			v := deliveredAt.Time
			e.DeliveredAt = &v
		}
		if nextRetryAt.Valid {
			v := nextRetryAt.Time
			e.NextRetryAt = &v
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

func (s *outboxStore) GetPending(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, feed_url, title, link, published_at, webhook_url, status, error, created_at, delivered_at, attempts, next_retry_at
		 FROM outbox WHERE status = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanEntries(rows)
}

func (s *outboxStore) GetDueForRetry(ctx context.Context, now time.Time, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = SweepBatchLimit
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, feed_url, title, link, published_at, webhook_url, status, error, created_at, delivered_at, attempts, next_retry_at
		 FROM outbox WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= `+db.Placeholder(s.driver, 1)+`) ORDER BY created_at LIMIT `+db.Placeholder(s.driver, 2),
		now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanEntries(rows)
}

func (s *outboxStore) NoteAttempt(ctx context.Context, id string, next time.Time) error {
	q := `UPDATE outbox SET attempts = attempts + 1, next_retry_at = ` + db.Placeholder(s.driver, 1) + ` WHERE id = ` + db.Placeholder(s.driver, 2)
	_, err := s.db.ExecContext(ctx, q, next, id)
	return err
}

func (s *outboxStore) MarkDelivered(ctx context.Context, id string) error {
	q := `UPDATE outbox SET status = 'delivered', delivered_at = CURRENT_TIMESTAMP, error = NULL WHERE id = ` + db.Placeholder(s.driver, 1)
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

func (s *outboxStore) MarkFailed(ctx context.Context, id string, errMsg string) error {
	q := `UPDATE outbox SET status = 'failed', error = ` + db.Placeholder(s.driver, 1) + ` WHERE id = ` + db.Placeholder(s.driver, 2)
	_, err := s.db.ExecContext(ctx, q, errMsg, id)
	return err
}

func (s *outboxStore) MarkSkipped(ctx context.Context, id string, reason string) error {
	q := `UPDATE outbox SET status = 'skipped', error = ` + db.Placeholder(s.driver, 1) + ` WHERE id = ` + db.Placeholder(s.driver, 2)
	_, err := s.db.ExecContext(ctx, q, reason, id)
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
		case "skipped":
			st.Skipped = int(count)
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
