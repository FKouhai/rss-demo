package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func resetConfigState(t *testing.T) {
	t.Helper()
	StopConfigPoll()
	cfgMu.Lock()
	old := cfg
	cfg = ConfigStruct{}
	cfgMu.Unlock()
	configMu.Lock()
	oldHad := configHadFeeds
	configHadFeeds = false
	configMu.Unlock()
	if cancelFn != nil {
		cancelFn()
	}
	t.Cleanup(func() {
		StopConfigPoll()
		cfgMu.Lock()
		cfg = old
		cfgMu.Unlock()
		configMu.Lock()
		configHadFeeds = oldHad
		configMu.Unlock()
	})
}

func waitForFeeds(t *testing.T, want int) ConfigStruct {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cfgMu.RLock()
		got := len(cfg.RSSFeeds) + len(cfg.Feeds)
		snap := cfg
		cfgMu.RUnlock()
		if got == want {
			return snap
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("feeds did not converge to %d", want)
	return ConfigStruct{}
}

func TestConfigPoll_AppliesSnapshot(t *testing.T) {
	resetConfigState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rss_feeds":   []string{"https://example.com/a.rss"},
			"webhook_url": "https://hooks/g",
			"feeds":       []any{map[string]string{"url": "https://example.com/b.rss", "webhook_url": "https://hooks/b"}},
		})
	}))
	defer srv.Close()

	StartConfigPoll(srv.URL, "", 50*time.Millisecond)
	snap := waitForFeeds(t, 2)
	if snap.WebhookURL != "https://hooks/g" || len(snap.Feeds) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestConfigPoll_FailureKeepsLastGood(t *testing.T) {
	resetConfigState(t)
	// Seed last-good directly, then poll a dead server.
	cfgMu.Lock()
	cfg = ConfigStruct{RSSFeeds: []string{"https://example.com/keep.rss"}}
	cfgMu.Unlock()

	StartConfigPoll("http://127.0.0.1:1", "", 20*time.Millisecond)
	time.Sleep(150 * time.Millisecond)

	cfgMu.RLock()
	defer cfgMu.RUnlock()
	if len(cfg.RSSFeeds) != 1 || cfg.RSSFeeds[0] != "https://example.com/keep.rss" {
		t.Fatalf("cfg = %+v, want last-good kept", cfg)
	}
}

func TestConfigPoll_DisabledOnEmptyURL(t *testing.T) {
	resetConfigState(t)
	StartConfigPoll("", "", 0)
	configMu.Lock()
	defer configMu.Unlock()
	if configCancel != nil {
		t.Error("empty base URL must not start the loop")
	}
}
