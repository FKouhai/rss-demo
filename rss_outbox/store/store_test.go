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
		ID:         id,
		FeedURL:    "https://example.com/feed.xml",
		Title:      "title-" + id,
		Link:       "https://example.com/" + id,
		WebhookURL: "https://hooks.example.com/" + id,
		Status:     status,
		CreatedAt:  createdAt,
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

func TestStore_Status_AllStates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// 3 pending, 2 delivered, 1 failed
	for _, e := range []Entry{
		entry("p1", "pending", now),
		entry("p2", "pending", now.Add(time.Second)),
		entry("p3", "pending", now.Add(2*time.Second)),
		entry("d1", "delivered", now),
		entry("d2", "delivered", now),
		entry("f1", "failed", now),
	} {
		mustSave(t, s, ctx, e)
	}

	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Pending != 3 || st.Delivered != 2 || st.Failed != 1 {
		t.Errorf("Status: want {pending=3 delivered=2 failed=1}, got %+v", st)
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

// --- dialect unit tests (no database needed) ---

func TestQmark(t *testing.T) {
	for _, n := range []int{1, 2, 42} {
		if got := qmark(n); got != "?" {
			t.Errorf("qmark(%d) = %q, want %q", n, got, "?")
		}
	}
}

func TestDollar(t *testing.T) {
	cases := map[int]string{1: "$1", 3: "$3", 12: "$12"}
	for n, want := range cases {
		if got := dollar(n); got != want {
			t.Errorf("dollar(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDialect_Placeholders(t *testing.T) {
	dq := dialect{placeholder: qmark}
	if got := dq.placeholders(1, 9); got != "?, ?, ?, ?, ?, ?, ?, ?, ?" {
		t.Errorf("qmark placeholders(1,9) = %q", got)
	}
	dp := dialect{placeholder: dollar}
	if got := dp.placeholders(1, 3); got != "$1, $2, $3" {
		t.Errorf("dollar placeholders(1,3) = %q", got)
	}
	// Offset start index: pg composes $5, $6, $7 (e.g. after an id placeholder).
	if got := dp.placeholders(5, 3); got != "$5, $6, $7" {
		t.Errorf("dollar placeholders(5,3) = %q", got)
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
