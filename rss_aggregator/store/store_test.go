package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	db "github.com/FKouhai/rss-demo/libs/db"
)

// newTestStore builds a Store against an isolated on-disk sqlite DB.
// A temp file (not :memory:) is used because database/sql pools
// connections and :memory: would hand each pooled connection its own
// private, empty database. t.TempDir() is cleaned up by the test runtime.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "aggregator_test.db")
	s, err := NewConfig(Config{Driver: db.DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("NewConfig(sqlite): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStore_SaveRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, err := s.Save(ctx, Item{
		ID: "abc", FeedURL: "https://example.com/feed.xml",
		Title: "hello", Link: "https://example.com/1", PublishedAt: "p",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id != "abc" {
		t.Errorf("Save returned id %q, want abc", id)
	}
	// Duplicate IDs violate the primary key — the consumer treats this
	// as "already stored" and Acks.
	if _, err := s.Save(ctx, Item{ID: "abc", FeedURL: "f", Title: "t", Link: "l"}); err == nil {
		t.Error("expected duplicate-key error on second Save with same ID, got nil")
	}
}

// seedItems saves each item, failing the test on the first error.
func seedItems(t *testing.T, s Store, ctx context.Context, items ...Item) {
	t.Helper()
	for _, it := range items {
		if _, err := s.Save(ctx, it); err != nil {
			t.Fatalf("Save(%s): %v", it.ID, err)
		}
	}
}

// itemAt builds an item with explicit CreatedAt and PublishedAt so ordering
// tests are deterministic (Save fills CreatedAt when zero, which would
// tie). Both clocks agree: newer ago means older on both.
func itemAt(id, feed, title, link string, ago time.Duration) Item {
	ts := time.Now().UTC().Add(-ago)
	return Item{
		ID: id, FeedURL: feed, Title: title, Link: link,
		PublishedAt: ts.Format(time.RFC3339),
		CreatedAt:   ts,
	}
}

func TestStore_List_Empty(t *testing.T) {
	s := newTestStore(t)
	items, total, err := s.List(context.Background(), "", "", 20, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestStore_List_NewestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedItems(t, s, ctx,
		itemAt("old", "f", "old news", "l1", 3*time.Hour),
		itemAt("new", "f", "new news", "l2", 1*time.Hour),
		itemAt("mid", "f", "mid news", "l3", 2*time.Hour),
	)
	items, total, err := s.List(ctx, "", "", 20, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	want := []string{"new", "mid", "old"}
	if len(items) != len(want) {
		t.Fatalf("len(items) = %d, want %d", len(items), len(want))
	}
	for i, id := range want {
		if items[i].ID != id {
			t.Errorf("items[%d].ID = %q, want %q", i, items[i].ID, id)
		}
	}
}

func TestStore_List_Filters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedItems(t, s, ctx,
		itemAt("a1", "https://a.example/feed", "golang release", "https://a.example/1", time.Hour),
		itemAt("a2", "https://a.example/feed", "weather today", "https://a.example/golang-tips", 2*time.Hour),
		itemAt("b1", "https://b.example/feed", "golang weekly", "https://b.example/1", 3*time.Hour),
	)

	// q matches title OR link.
	items, total, err := s.List(ctx, "golang", "", 20, 0)
	if err != nil {
		t.Fatalf("List(q): %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3 (a1 title, a2 link, b1 title)", total)
	}
	if len(items) != 3 {
		t.Errorf("len(items) = %d, want 3", len(items))
	}

	// feed is an exact match.
	items, total, err = s.List(ctx, "", "https://a.example/feed", 20, 0)
	if err != nil {
		t.Fatalf("List(feed): %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Errorf("total = %d, len = %d; want 2, 2", total, len(items))
	}

	// q + feed combine.
	items, total, err = s.List(ctx, "golang", "https://b.example/feed", 20, 0)
	if err != nil {
		t.Fatalf("List(q+feed): %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "b1" {
		t.Errorf("got total=%d items=%v, want total=1 [b1]", total, items)
	}
}

func TestStore_List_Pagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedItems(t, s, ctx,
		itemAt("i1", "f", "t1", "l1", 3*time.Hour),
		itemAt("i2", "f", "t2", "l2", 2*time.Hour),
		itemAt("i3", "f", "t3", "l3", time.Hour),
	)

	items, total, err := s.List(ctx, "", "", 2, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3 (total ignores paging)", total)
	}
	if len(items) != 2 || items[0].ID != "i3" || items[1].ID != "i2" {
		t.Errorf("page 1 = %v, want [i3 i2]", ids(items))
	}

	items, _, err = s.List(ctx, "", "", 2, 2)
	if err != nil {
		t.Fatalf("List(offset): %v", err)
	}
	if len(items) != 1 || items[0].ID != "i1" {
		t.Errorf("page 2 = %v, want [i1]", ids(items))
	}

	items, total, err = s.List(ctx, "", "", 2, 10)
	if err != nil {
		t.Fatalf("List(past end): %v", err)
	}
	if len(items) != 0 || total != 3 {
		t.Errorf("past end: len=%d total=%d, want 0, 3", len(items), total)
	}

	// Clamp behavior: non-positive limit defaults, oversize clamps,
	// negative offset counts as zero.
	items, _, err = s.List(ctx, "", "", 0, -5)
	if err != nil {
		t.Fatalf("List(clamp): %v", err)
	}
	if len(items) != 3 {
		t.Errorf("len(items) = %d, want 3 (limit 0 defaults, offset -5 → 0)", len(items))
	}
}

func TestStore_List_EscapesWildcards(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedItems(t, s, ctx,
		itemAt("pct", "f", "100% real", "l1", time.Hour),
		itemAt("decoy", "f", "100x fake", "l2", 2*time.Hour),
	)
	// Without escaping, "100%" as a LIKE pattern would match both rows
	// (% matches "x fake" too). The literal search must match one.
	items, total, err := s.List(ctx, "100%", "", 20, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "pct" {
		t.Errorf("got total=%d items=%v, want total=1 [pct]", total, ids(items))
	}
}

func ids(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestStore_Feeds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	feeds, err := s.Feeds(ctx)
	if err != nil {
		t.Fatalf("Feeds(empty): %v", err)
	}
	if len(feeds) != 0 {
		t.Errorf("Feeds(empty) = %v, want []", feeds)
	}

	seedItems(t, s, ctx,
		itemAt("a1", "https://a.example/feed", "t1", "l1", 3*time.Hour),
		itemAt("a2", "https://a.example/feed", "t2", "l2", time.Hour),
		itemAt("b1", "https://b.example/feed", "t3", "l3", 2*time.Hour),
	)
	feeds, err = s.Feeds(ctx)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}
	// Newest activity first: a.feed (1h ago) before b.feed (2h ago),
	// each listed once.
	want := []string{"https://a.example/feed", "https://b.example/feed"}
	if len(feeds) != len(want) {
		t.Fatalf("Feeds = %v, want %v", feeds, want)
	}
	for i, f := range want {
		if feeds[i] != f {
			t.Errorf("feeds[%d] = %q, want %q", i, feeds[i], f)
		}
	}
}

func TestStore_Get(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedItems(t, s, ctx,
		itemAt("get-me", "https://a.example/feed", "found it", "https://a.example/9", time.Hour),
	)

	got, err := s.Get(ctx, "get-me")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "found it" || got.FeedURL != "https://a.example/feed" || got.Link != "https://a.example/9" {
		t.Errorf("Get = %+v, fields mismatch", got)
	}

	if _, err := s.Get(ctx, "no-such-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestStore_IsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	empty, err := s.IsEmpty(ctx)
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if !empty {
		t.Error("fresh store should be empty")
	}
	seedItems(t, s, ctx, itemAt("e1", "f", "t", "l", time.Hour))
	if empty, err := s.IsEmpty(ctx); err != nil || empty {
		t.Errorf("IsEmpty after save = %v, err = %v; want false, nil", empty, err)
	}
}
