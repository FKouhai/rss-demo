package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FKouhai/rss-config/store"
	db "github.com/FKouhai/rss-demo/libs/db"
	_ "modernc.org/sqlite"
)

// newTestApp starts an in-process config on an ephemeral port backed by
// a temp-file sqlite DB. It skips the tracer so the test stays offline
// and hermetic, and runs in open mode (no gateway secret) unless told
// otherwise. Callers must call app.Shutdown via t.Cleanup (wired here).
func newTestApp(t *testing.T, cfg Config) *App {
	t.Helper()
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.SkipTracer = true
	if cfg.DBDriver == "" {
		cfg.DBDriver = db.DriverSQLite
	}
	if cfg.DBDSN == "" {
		cfg.DBDSN = filepath.Join(t.TempDir(), "config_cmd_test.db")
	}
	app, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return app
}

func waitForReady(t *testing.T, app *App) {
	t.Helper()
	select {
	case <-app.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("Ready channel did not fire within 2s")
	}
}

func get(t *testing.T, app *App, path string, headers map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+app.Addr().String()+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func doJSON(t *testing.T, app *App, method, path string, payload any, headers map[string]string) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req, err := http.NewRequest(method, "http://"+app.Addr().String()+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func TestApp_Run_ReadyFires(t *testing.T) {
	app := newTestApp(t, Config{})
	waitForReady(t, app)
}

func TestApp_Run_HealthzResponds(t *testing.T) {
	app := newTestApp(t, Config{})
	waitForReady(t, app)

	if code, _ := get(t, app, "/healthz", nil); code != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200", code)
	}
	if code, body := get(t, app, "/ready", nil); code != http.StatusOK {
		t.Errorf("/ready status = %d, want 200 (body %s)", code, body)
	}
}

func TestApp_Run_ConfigRoundTrip(t *testing.T) {
	app := newTestApp(t, Config{})
	waitForReady(t, app)

	// Empty snapshot first.
	if code, body := get(t, app, "/config", nil); code != http.StatusOK {
		t.Fatalf("/config status = %d, want 200", code)
	} else {
		var snap store.Snapshot
		if err := json.Unmarshal(body, &snap); err != nil {
			t.Fatalf("decode /config: %v", err)
		}
		if len(snap.RSSFeeds) != 0 {
			t.Fatalf("/config feeds = %v, want empty", snap.RSSFeeds)
		}
	}

	// Upsert feeds (open mode: no auth needed).
	if code, _ := doJSON(t, app, http.MethodPost, "/feeds",
		map[string]string{"url": "https://example.com/a.rss"}, nil); code != http.StatusOK {
		t.Fatalf("POST /feeds status = %d, want 200", code)
	}
	if code, _ := doJSON(t, app, http.MethodPost, "/feeds",
		map[string]string{"url": "https://example.com/b.rss", "webhook_url": "https://hooks/b"}, nil); code != http.StatusOK {
		t.Fatalf("POST /feeds status = %d, want 200", code)
	}
	// Bad URL rejected, nothing written.
	if code, _ := doJSON(t, app, http.MethodPost, "/feeds",
		map[string]string{"url": "not-a-url"}, nil); code != http.StatusBadRequest {
		t.Errorf("POST /feeds bad url status = %d, want 400", code)
	}

	if code, body := get(t, app, "/feeds", nil); code != http.StatusOK {
		t.Fatalf("GET /feeds status = %d", code)
	} else {
		var list struct {
			Feeds []store.Feed `json:"feeds"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatalf("decode /feeds: %v", err)
		}
		if len(list.Feeds) != 2 {
			t.Fatalf("/feeds = %d entries, want 2", len(list.Feeds))
		}
	}

	// Default webhook flows into the snapshot.
	if code, _ := doJSON(t, app, http.MethodPut, "/config/default-webhook",
		map[string]string{"webhook_url": "https://hooks/global"}, nil); code != http.StatusOK {
		t.Fatalf("PUT default-webhook status = %d", code)
	}
	if code, body := get(t, app, "/config", nil); code != http.StatusOK {
		t.Fatalf("/config status = %d", code)
	} else {
		var snap store.Snapshot
		if err := json.Unmarshal(body, &snap); err != nil {
			t.Fatalf("decode /config: %v", err)
		}
		if len(snap.RSSFeeds) != 2 || snap.WebhookURL != "https://hooks/global" {
			t.Errorf("/config = %+v, want 2 feeds + global default", snap)
		}
		if len(snap.Feeds) != 1 {
			t.Errorf("/config overrides = %+v, want 1", snap.Feeds)
		}
	}

	// Delete is idempotent-shaped: second call reports deleted=false.
	if code, body := doJSON(t, app, http.MethodDelete, "/feeds?url=https://example.com/a.rss", nil, nil); code != http.StatusOK {
		t.Fatalf("DELETE status = %d", code)
	} else if !bytes.Contains(body, []byte(`"deleted":true`)) {
		t.Errorf("DELETE body = %s, want deleted:true", body)
	}
}

func TestApp_Run_HookUpdateMerge(t *testing.T) {
	app := newTestApp(t, Config{})
	waitForReady(t, app)

	// Existing override must survive a legacy-list push.
	if code, _ := doJSON(t, app, http.MethodPost, "/feeds",
		map[string]string{"url": "https://example.com/a.rss", "webhook_url": "https://hooks/keep"}, nil); code != http.StatusOK {
		t.Fatalf("setup POST /feeds status = %d", code)
	}

	payload := map[string]any{
		"rss_feeds":   []string{"https://example.com/a.rss", "https://example.com/b.rss"},
		"webhook_url": "https://hooks/global",
		"feeds": []map[string]string{
			{"url": "https://example.com/b.rss", "webhook_url": "https://hooks/b"},
		},
	}
	if code, _ := doJSON(t, app, http.MethodPost, "/hooks/update", payload, nil); code != http.StatusOK {
		t.Fatalf("POST /hooks/update status = %d", code)
	}

	if code, body := get(t, app, "/config", nil); code != http.StatusOK {
		t.Fatalf("/config status = %d", code)
	} else {
		var snap store.Snapshot
		if err := json.Unmarshal(body, &snap); err != nil {
			t.Fatalf("decode /config: %v", err)
		}
		if len(snap.RSSFeeds) != 2 || snap.WebhookURL != "https://hooks/global" {
			t.Fatalf("/config = %+v, want merged", snap)
		}
		got := map[string]string{}
		for _, f := range snap.Feeds {
			got[f.URL] = f.WebhookURL
		}
		if got["https://example.com/a.rss"] != "https://hooks/keep" || got["https://example.com/b.rss"] != "https://hooks/b" {
			t.Errorf("overrides = %v, want both kept", got)
		}
	}

	// Invalid push is rejected before any write.
	bad := map[string]any{"rss_feeds": []string{"https://example.com/c.rss", "bogus"}}
	if code, _ := doJSON(t, app, http.MethodPost, "/hooks/update", bad, nil); code != http.StatusBadRequest {
		t.Fatalf("bad push status = %d, want 400", code)
	}
	if code, body := get(t, app, "/config", nil); code == http.StatusOK {
		var snap store.Snapshot
		_ = json.Unmarshal(body, &snap)
		if len(snap.RSSFeeds) != 2 {
			t.Errorf("rejected push partially applied: %+v", snap)
		}
	}
}

func TestApp_Run_AuthGate(t *testing.T) {
	app := newTestApp(t, Config{
		GatewaySecret: "s3cret",
		AdminGroups:   []string{"admin"},
	})
	waitForReady(t, app)

	anon := map[string]string{}
	reader := map[string]string{
		"X-Gateway-Secret": "s3cret",
		"X-Tenant-ID":      "default",
	}
	admin := map[string]string{
		"X-Gateway-Secret":   "s3cret",
		"X-Forwarded-User":   "boss",
		"X-Forwarded-Groups": "admin",
	}

	// No secret: reads and writes both 401.
	if code, _ := get(t, app, "/config", anon); code != http.StatusUnauthorized {
		t.Errorf("anon /config = %d, want 401", code)
	}
	if code, _ := doJSON(t, app, http.MethodPost, "/feeds",
		map[string]string{"url": "https://example.com/x.rss"}, anon); code != http.StatusUnauthorized {
		t.Errorf("anon POST /feeds = %d, want 401", code)
	}

	// Authenticated non-admin: reads pass, writes 403.
	if code, _ := get(t, app, "/config", reader); code != http.StatusOK {
		t.Errorf("reader /config = %d, want 200", code)
	}
	if code, _ := doJSON(t, app, http.MethodPost, "/feeds",
		map[string]string{"url": "https://example.com/x.rss"}, reader); code != http.StatusForbidden {
		t.Errorf("reader POST /feeds = %d, want 403", code)
	}

	// Admin writes.
	if code, _ := doJSON(t, app, http.MethodPost, "/feeds",
		map[string]string{"url": "https://example.com/x.rss"}, admin); code != http.StatusOK {
		t.Errorf("admin POST /feeds = %d, want 200", code)
	}
}

func TestApp_Run_SeedOnce(t *testing.T) {
	dir := t.TempDir()
	seed := `{"rss_feeds":["https://example.com/seed.rss"],"webhook_url":"https://hooks/seed","feeds":[{"url":"https://example.com/m.rss","webhook_url":"https://hooks/m"}]}`
	seedPath := filepath.Join(dir, "seed.json")
	if err := os.WriteFile(seedPath, []byte(seed), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	dsn := filepath.Join(dir, "config_seed_test.db")

	newSeededApp := func() *App {
		return newTestApp(t, Config{DBDSN: dsn, SeedFile: seedPath})
	}

	first := newSeededApp()
	waitForReady(t, first)
	if code, body := get(t, first, "/config", nil); code != http.StatusOK {
		t.Fatalf("/config status = %d", code)
	} else {
		var snap store.Snapshot
		_ = json.Unmarshal(body, &snap)
		if len(snap.RSSFeeds) != 2 || snap.WebhookURL != "https://hooks/seed" {
			t.Fatalf("seeded /config = %+v", snap)
		}
	}

	// Mutate, restart on the same DB: state survives, seed does not re-run.
	if code, _ := doJSON(t, first, http.MethodPost, "/feeds",
		map[string]string{"url": "https://example.com/added.rss"}, nil); code != http.StatusOK {
		t.Fatalf("POST /feeds status = %d", code)
	}
	second := newSeededApp()
	waitForReady(t, second)
	if code, body := get(t, second, "/config", nil); code == http.StatusOK {
		var snap store.Snapshot
		_ = json.Unmarshal(body, &snap)
		if len(snap.RSSFeeds) != 3 {
			t.Errorf("after restart /config = %+v, want 3 feeds (no reseed)", snap)
		}
	}
}
