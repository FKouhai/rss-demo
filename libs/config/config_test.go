package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSnapshot_OK(t *testing.T) {
	var gotSecret, gotTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config" {
			t.Errorf("path = %s, want /config", r.URL.Path)
		}
		gotSecret, gotTenant = r.Header.Get("X-Gateway-Secret"), r.Header.Get("X-Tenant-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rss_feeds":["https://example.com/a.rss"],"webhook_url":"https://hooks/g","feeds":[{"url":"https://example.com/a.rss","webhook_url":"https://hooks/a"}]}`))
	}))
	defer srv.Close()

	snap, err := NewClient(srv.URL, "s3cret").Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if gotSecret != "s3cret" || gotTenant != "default" {
		t.Errorf("headers secret=%q tenant=%q, want s3cret/default", gotSecret, gotTenant)
	}
	if len(snap.RSSFeeds) != 1 || snap.WebhookURL != "https://hooks/g" || len(snap.Feeds) != 1 {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestSnapshot_Errors(t *testing.T) {
	ctx := context.Background()
	if _, err := NewClient("", "").Snapshot(ctx); err == nil {
		t.Error("empty base URL: want error")
	}

	for name, handler := range map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"body":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{nope")) },
	} {
		srv := httptest.NewServer(handler)
		if _, err := NewClient(srv.URL, "").Snapshot(ctx); err == nil {
			t.Errorf("%s: want error", name)
		}
		srv.Close()
	}

	// Unreachable host: error, so callers keep last-good.
	if _, err := NewClient("http://127.0.0.1:1", "").Snapshot(ctx); err == nil {
		t.Error("unreachable: want error")
	}
}
