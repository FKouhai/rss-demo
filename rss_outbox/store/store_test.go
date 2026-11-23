package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore builds a Store against an isolated on-disk sqlite DB.
// A temp file (not :memory:) is used because database/sql pools
// connections and :memory: would hand each pooled connection its own
// private, empty database. t.TempDir() is cleaned up by the test runtime.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "outbox_test.db")
	s, err := NewConfig(Config{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("NewConfig(sqlite): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustSave(t *testing.T, s Store, ctx context.Context, e Entry) {
	t.Helper()
	if _, err := s.Save(ctx, e); err != nil {
		t.Fatalf("Save(%s): %v", e.ID, err)
	}
}

func entry(id string, status string, createdAt time.Time) Entry {
	return Entry{
		ID:          id,
		FeedURL:     "https://example.com/feed.xml",
		Title:       "title-" + id,
		Link:        "https://example.com/" + id,
		PublishedAt: "2025-01-02T03:04:05Z",
		WebhookURL:  "https://hooks.example.com/" + id,
		Status:      status,
		CreatedAt:   createdAt,
	}
}

// --- interface-level behavior (sqlite-backed, but impl-agnostic) ---

func TestStore_SaveAndGetPending(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	mustSave(t, s, ctx, entry("a1", "pending", created))

	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("GetPending: want 1 row, got %d", len(pending))
	}
	got := pending[0]
	want := Entry{
		ID:          "a1",
		FeedURL:     "https://example.com/feed.xml",
		Title:       "title-a1",
		Link:        "https://example.com/a1",
		PublishedAt: "2025-01-02T03:04:05Z",
		WebhookURL:  "https://hooks.example.com/a1",
		Status:      "pending",
		Error:       "", // nullable col, stored empty
		CreatedAt:   created,
		DeliveredAt: nil, // nullable col, NULL for a pending row
	}
	assertEntryEqual(t, got, want)
}

func TestStore_GetPending_OnlyPendingRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mustSave(t, s, ctx, entry("p1", "pending", now))
	mustSave(t, s, ctx, entry("p2", "pending", now.Add(time.Second)))
	mustSave(t, s, ctx, entry("d1", "delivered", now))
	mustSave(t, s, ctx, entry("f1", "failed", now))

	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("GetPending: want 2 pending rows, got %d", len(pending))
	}
	seen := map[string]bool{}
	for _, e := range pending {
		if e.Status != "pending" {
			t.Errorf("GetPending returned non-pending row %q (status=%q)", e.ID, e.Status)
		}
		seen[e.ID] = true
	}
	if !seen["p1"] || !seen["p2"] {
		t.Errorf("GetPending: expected p1 and p2, got %v", seen)
	}
}

func TestStore_GetPending_OrderedByCreatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	// Insert out of order; GetPending must return ascending by created_at.
	mustSave(t, s, ctx, entry("late", "pending", base.Add(2*time.Minute)))
	mustSave(t, s, ctx, entry("early", "pending", base))
	mustSave(t, s, ctx, entry("mid", "pending", base.Add(time.Minute)))

	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("GetPending: want 3 rows, got %d", len(pending))
	}
	got := []string{pending[0].ID, pending[1].ID, pending[2].ID}
	want := []string{"early", "mid", "late"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GetPending order: want %v, got %v", want, got)
		}
	}
}

func TestStore_MarkDelivered(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustSave(t, s, ctx, entry("a1", "pending", time.Now().UTC()))

	if err := s.MarkDelivered(ctx, "a1"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("after MarkDelivered: want 0 pending, got %d", len(pending))
	}
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Delivered != 1 || st.Pending != 0 || st.Failed != 0 {
		t.Errorf("Status: want delivered=1, got %+v", st)
	}
}

func TestStore_MarkFailed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustSave(t, s, ctx, entry("a1", "pending", time.Now().UTC()))

	if err := s.MarkFailed(ctx, "a1", "webhook timeout"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("after MarkFailed: want 0 pending, got %d", len(pending))
	}
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Failed != 1 || st.Pending != 0 || st.Delivered != 0 {
		t.Errorf("Status: want failed=1, got %+v", st)
	}
}

func TestStore_MarkSkipped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustSave(t, s, ctx, entry("a1", "pending", time.Now().UTC()))

	if err := s.MarkSkipped(ctx, "a1", "older than max item age"); err != nil {
		t.Fatalf("MarkSkipped: %v", err)
	}

	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("after MarkSkipped: want 0 pending, got %d", len(pending))
	}
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Skipped != 1 || st.Pending != 0 || st.Delivered != 0 || st.Failed != 0 {
		t.Errorf("Status: want skipped=1, got %+v", st)
	}
}

func TestStore_Status_AllStates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 3 pending, 2 delivered, 1 failed, 1 skipped
	for _, e := range []Entry{
		entry("p1", "pending", now),
		entry("p2", "pending", now.Add(time.Second)),
		entry("p3", "pending", now.Add(2*time.Second)),
		entry("d1", "delivered", now),
		entry("d2", "delivered", now),
		entry("f1", "failed", now),
		entry("s1", "skipped", now),
	} {
		mustSave(t, s, ctx, e)
	}

	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Pending != 3 || st.Delivered != 2 || st.Failed != 1 || st.Skipped != 1 {
		t.Errorf("Status: want {pending=3 delivered=2 failed=1 skipped=1}, got %+v", st)
	}
}

func TestStore_Status_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st != (Status{}) {
		t.Errorf("Status on empty store: want zero value, got %+v", st)
	}
}

func TestStore_Save_DuplicateIDFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e := entry("dup", "pending", time.Now().UTC())

	mustSave(t, s, ctx, e)
	if _, err := s.Save(ctx, e); err == nil {
		t.Fatal("Save with duplicate ID: want error, got nil")
	}
}

func TestStore_Save_PreservesProvidedDeliveredAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	delivered := time.Date(2025, 7, 4, 10, 0, 0, 0, time.UTC)

	// A delivered row carrying a DeliveredAt and Error should still be
	// countable as delivered (the non-pending path).
	e := entry("d1", "delivered", time.Now().UTC())
	e.DeliveredAt = &delivered
	e.Error = "prior transient error"
	mustSave(t, s, ctx, e)

	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Delivered != 1 {
		t.Errorf("Status: want delivered=1, got %+v", st)
	}
}

func assertEntryEqual(t *testing.T, got, want Entry) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.FeedURL != want.FeedURL {
		t.Errorf("FeedURL = %q, want %q", got.FeedURL, want.FeedURL)
	}
	if got.Title != want.Title {
		t.Errorf("Title = %q, want %q", got.Title, want.Title)
	}
	if got.Link != want.Link {
		t.Errorf("Link = %q, want %q", got.Link, want.Link)
	}
	if got.PublishedAt != want.PublishedAt {
		t.Errorf("PublishedAt = %q, want %q", got.PublishedAt, want.PublishedAt)
	}
	if got.WebhookURL != want.WebhookURL {
		t.Errorf("WebhookURL = %q, want %q", got.WebhookURL, want.WebhookURL)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.Error != want.Error {
		t.Errorf("Error = %q, want %q", got.Error, want.Error)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if (got.DeliveredAt == nil) != (want.DeliveredAt == nil) {
		t.Errorf("DeliveredAt nil-ness = %v, want %v", got.DeliveredAt == nil, want.DeliveredAt == nil)
	}
	if got.DeliveredAt != nil && want.DeliveredAt != nil && !got.DeliveredAt.Equal(*want.DeliveredAt) {
		t.Errorf("DeliveredAt = %v, want %v", *got.DeliveredAt, *want.DeliveredAt)
	}
}

func TestBackoffForAttempt(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{0, time.Minute},
		{1, 2 * time.Minute},
		{2, 4 * time.Minute},
		{6, time.Hour},   // 64m capped
		{10, time.Hour},  // way over capped
		{100, time.Hour}, // no overflow, still capped
	}
	for _, c := range cases {
		if got := BackoffForAttempt(c.n); got != c.want {
			t.Errorf("BackoffForAttempt(%d) = %v, want %v", c.n, got, c.want)
		}
	}
}

func TestSave_SetsRetryFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	mustSave(t, s, ctx, entry("r1", "pending", created))

	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 row, got %d", len(pending))
	}
	if pending[0].Attempts != 0 {
		t.Errorf("Attempts = %d, want 0 on save", pending[0].Attempts)
	}
	if pending[0].NextRetryAt == nil {
		t.Fatal("NextRetryAt is nil, want created+InitialRetryDelay")
	}
	if want := created.Add(InitialRetryDelay); !pending[0].NextRetryAt.Equal(want) {
		t.Errorf("NextRetryAt = %v, want %v", pending[0].NextRetryAt, want)
	}
}

func TestGetDueForRetry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Due: next_retry_at in the past.
	past := entry("due-past", "pending", now.Add(-time.Hour))
	mustSave(t, s, ctx, past)
	if err := s.NoteAttempt(ctx, "due-past", now.Add(-time.Minute)); err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}
	// Not due: next_retry_at in the future.
	future := entry("due-future", "pending", now.Add(-time.Hour))
	mustSave(t, s, ctx, future)
	if err := s.NoteAttempt(ctx, "due-future", now.Add(time.Hour)); err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}
	// Terminal: delivered rows never come due.
	mustSave(t, s, ctx, entry("done", "pending", now.Add(-time.Hour)))
	if err := s.MarkDelivered(ctx, "done"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	// Legacy: pre-retry rows carry NULL next_retry_at and are due.
	raw, ok := s.(*outboxStore)
	if !ok {
		t.Fatal("test store is not *outboxStore")
	}
	if _, err := raw.db.ExecContext(ctx,
		`INSERT INTO outbox (id, feed_url, title, link, published_at, webhook_url, status, created_at, attempts) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, 0)`,
		"due-legacy", "f", "t", "l", "", "w", now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("legacy insert: %v", err)
	}

	due, err := s.GetDueForRetry(ctx, now, 10)
	if err != nil {
		t.Fatalf("GetDueForRetry: %v", err)
	}
	got := map[string]bool{}
	for _, e := range due {
		got[e.ID] = true
	}
	if !got["due-past"] || !got["due-legacy"] {
		t.Errorf("due = %v, want due-past and due-legacy", got)
	}
	if got["due-future"] || got["done"] {
		t.Errorf("due = %v, want neither due-future nor done", got)
	}
}

func TestNoteAttempt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustSave(t, s, ctx, entry("n1", "pending", time.Now().UTC()))

	next := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if err := s.NoteAttempt(ctx, "n1", next); err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}
	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if pending[0].Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", pending[0].Attempts)
	}
	if pending[0].NextRetryAt == nil || pending[0].NextRetryAt.Before(next.Add(-time.Minute)) {
		t.Errorf("NextRetryAt = %v, want ~%v", pending[0].NextRetryAt, next)
	}
	if err := s.NoteAttempt(ctx, "n1", next); err != nil {
		t.Fatalf("NoteAttempt again: %v", err)
	}
	pending, _ = s.GetPending(ctx)
	if pending[0].Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", pending[0].Attempts)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	// Opening the same file twice must tolerate existing columns.
	dsn := filepath.Join(t.TempDir(), "outbox_migrate_test.db")
	for range 2 {
		s, err := NewConfig(Config{Driver: DriverSQLite, DSN: dsn})
		if err != nil {
			t.Fatalf("NewConfig: %v", err)
		}
		_ = s.Close()
	}
}

func TestMigrate_PreRetryTable(t *testing.T) {
	// Prod failure mode: a table created by the pre-retry schema has no
	// attempts/next_retry_at columns AND no retry index. Opening it with
	// the new code must add the columns BEFORE the schema's CREATE INDEX
	// references them (pq 42703 otherwise), preserving existing rows.
	dsn := filepath.Join(t.TempDir(), "outbox_preretry_test.db")
	raw, err := NewConfig(Config{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	s, ok := raw.(*outboxStore)
	if !ok {
		_ = raw.Close()
		t.Fatal("store is not *outboxStore")
	}
	// Strip back to the pre-retry shape: no version table (the old code
	// never created one), old table, legacy row.
	if _, err := s.db.Exec(`DROP TABLE outbox`); err != nil {
		_ = raw.Close()
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.db.Exec(`CREATE TABLE outbox (
	    id TEXT PRIMARY KEY, feed_url TEXT NOT NULL, title TEXT NOT NULL,
	    link TEXT NOT NULL, published_at TEXT NOT NULL DEFAULT '',
	    webhook_url TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
	    error TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	    delivered_at DATETIME
	)`); err != nil {
		_ = raw.Close()
		t.Fatalf("old schema: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO outbox (id, feed_url, title, link, webhook_url, status, created_at)
		VALUES ('legacy-1', 'f', 't', 'l', 'w', 'pending', '2026-01-01T00:00:00Z')`); err != nil {
		_ = raw.Close()
		t.Fatalf("legacy insert: %v", err)
	}
	if _, err := s.db.Exec(`DROP TABLE schema_migrations`); err != nil {
		_ = raw.Close()
		t.Fatalf("drop versions: %v", err)
	}
	_ = raw.Close()

	reopened, err := NewConfig(Config{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("NewConfig on pre-retry table: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	ctx := context.Background()
	due, err := reopened.GetDueForRetry(ctx, time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC), 10)
	if err != nil {
		t.Fatalf("GetDueForRetry: %v", err)
	}
	if len(due) != 1 || due[0].ID != "legacy-1" || due[0].Attempts != 0 {
		t.Fatalf("due = %+v, want legacy-1 at 0 attempts", due)
	}
}
