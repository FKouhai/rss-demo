package store

import (
	"context"
	"path/filepath"
	"testing"

	db "github.com/FKouhai/rss-demo/libs/db"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := NewConfig(Config{
		Driver: db.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "config_store_test.db"),
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func TestStore_EmptySnapshot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if empty, err := s.IsEmpty(ctx); err != nil || !empty {
		t.Fatalf("IsEmpty = %v, %v; want true, nil", empty, err)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.RSSFeeds) != 0 || snap.WebhookURL != "" || len(snap.Feeds) != 0 {
		t.Errorf("empty snapshot = %+v, want zero value", snap)
	}
	if def, err := s.GetDefaultWebhook(ctx); err != nil || def != "" {
		t.Errorf("GetDefaultWebhook = %q, %v; want empty", def, err)
	}
}

func TestStore_UpsertListDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertFeed(ctx, "https://example.com/a.rss", ""); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	if err := s.UpsertFeed(ctx, "https://example.com/b.rss", "https://hooks/x"); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}

	feeds, err := s.ListFeeds(ctx)
	if err != nil {
		t.Fatalf("ListFeeds: %v", err)
	}
	if len(feeds) != 2 {
		t.Fatalf("ListFeeds = %d feeds, want 2", len(feeds))
	}

	// Re-push with an override: replaces mapping, stays one row.
	if err := s.UpsertFeed(ctx, "https://example.com/a.rss", "https://hooks/y"); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	feeds, _ = s.ListFeeds(ctx)
	if len(feeds) != 2 {
		t.Fatalf("ListFeeds after re-push = %d, want 2", len(feeds))
	}
	for _, f := range feeds {
		if f.URL == "https://example.com/a.rss" && f.WebhookURL != "https://hooks/y" {
			t.Errorf("override not replaced: %+v", f)
		}
		if !f.Active {
			t.Errorf("upsert should reactivate: %+v", f)
		}
	}

	deleted, err := s.DeleteFeed(ctx, "https://example.com/a.rss")
	if err != nil || !deleted {
		t.Fatalf("DeleteFeed = %v, %v; want true, nil", deleted, err)
	}
	deleted, err = s.DeleteFeed(ctx, "https://example.com/a.rss")
	if err != nil || deleted {
		t.Fatalf("DeleteFeed missing = %v, %v; want false, nil", deleted, err)
	}

	if empty, _ := s.IsEmpty(ctx); empty {
		t.Error("IsEmpty with one feed left, want false")
	}

	if err := s.UpsertFeed(ctx, "", "x"); err == nil {
		t.Error("UpsertFeed empty url: want error")
	}
}

func TestStore_EnsureFeedPreservesOverride(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertFeed(ctx, "https://example.com/a.rss", "https://hooks/keep"); err != nil {
		t.Fatalf("UpsertFeed: %v", err)
	}
	// Legacy list re-push must not clobber the explicit mapping.
	if err := s.EnsureFeed(ctx, "https://example.com/a.rss"); err != nil {
		t.Fatalf("EnsureFeed: %v", err)
	}
	if err := s.EnsureFeed(ctx, "https://example.com/new.rss"); err != nil {
		t.Fatalf("EnsureFeed: %v", err)
	}
	feeds, _ := s.ListFeeds(ctx)
	if len(feeds) != 2 {
		t.Fatalf("ListFeeds = %d, want 2", len(feeds))
	}
	for _, f := range feeds {
		if f.URL == "https://example.com/a.rss" && f.WebhookURL != "https://hooks/keep" {
			t.Errorf("EnsureFeed clobbered override: %+v", f)
		}
	}
}

func TestStore_DefaultWebhook(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetDefaultWebhook(ctx, "https://hooks/global"); err != nil {
		t.Fatalf("SetDefaultWebhook: %v", err)
	}
	if def, _ := s.GetDefaultWebhook(ctx); def != "https://hooks/global" {
		t.Errorf("GetDefaultWebhook = %q, want global", def)
	}
	// Replace, then clear.
	if err := s.SetDefaultWebhook(ctx, "https://hooks/other"); err != nil {
		t.Fatalf("SetDefaultWebhook: %v", err)
	}
	if err := s.SetDefaultWebhook(ctx, ""); err != nil {
		t.Fatalf("SetDefaultWebhook clear: %v", err)
	}
	if def, _ := s.GetDefaultWebhook(ctx); def != "" {
		t.Errorf("GetDefaultWebhook after clear = %q, want empty", def)
	}
}

func TestStore_SnapshotShape(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.UpsertFeed(ctx, "https://example.com/plain.rss", "")
	_ = s.UpsertFeed(ctx, "https://example.com/mapped.rss", "https://hooks/m")
	_ = s.SetDefaultWebhook(ctx, "https://hooks/global")

	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.RSSFeeds) != 2 {
		t.Fatalf("RSSFeeds = %v, want both urls", snap.RSSFeeds)
	}
	if snap.WebhookURL != "https://hooks/global" {
		t.Errorf("WebhookURL = %q", snap.WebhookURL)
	}
	if len(snap.Feeds) != 1 || snap.Feeds[0].URL != "https://example.com/mapped.rss" {
		t.Errorf("Feeds overrides = %+v, want only mapped", snap.Feeds)
	}
}
