package handlers

import (
	"context"
	"sync"
	"time"

	cfgclient "github.com/FKouhai/rss-demo/libs/config"
	log "github.com/FKouhai/rss-demo/libs/logger"
)

// Config polling pulls the desired feeds from rss_config on an
// interval, so the file is seed + fallback, not the source of truth.
// A failed fetch keeps the last-good snapshot (or the file boot state);
// the next tick retries. Feeds appearing mid-run start the poll loop;
// feeds never shrink it (an empty snapshot just yields idle ticks).

var (
	configMu       sync.Mutex
	configCancel   context.CancelFunc
	configHadFeeds bool
)

// StartConfigPoll begins polling baseURL for GET /config snapshots.
// Empty baseURL disables polling (file-only mode, used by tests).
// The first fetch runs immediately; failures log and keep last-good.
func StartConfigPoll(baseURL, secret string, interval time.Duration) {
	if baseURL == "" {
		return
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	StopConfigPoll()

	client := cfgclient.NewClient(baseURL, secret)
	ctx, cancel := context.WithCancel(context.Background())
	configMu.Lock()
	configCancel = cancel
	configMu.Unlock()

	fetch := func() {
		snap, err := client.Snapshot(ctx)
		if err != nil {
			log.ErrorFmt("config poll %s: %v (keeping last-good)", baseURL, err)
			return
		}
		applySnapshot(snap)
	}
	fetch()

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fetch()
			}
		}
	}()
}

// StopConfigPoll halts the config fetch loop, if running.
func StopConfigPoll() {
	configMu.Lock()
	defer configMu.Unlock()
	if configCancel != nil {
		configCancel()
		configCancel = nil
	}
}

// applySnapshot swaps the active feed config for a fetched one.
// Polling starts on the empty→non-empty transition only; later
// snapshots (including empty ones) just take effect on the next tick.
func applySnapshot(snap cfgclient.Snapshot) {
	cfgMu.Lock()
	cfg = ConfigStruct{
		RSSFeeds:   snap.RSSFeeds,
		WebhookURL: snap.WebhookURL,
	}
	cfg.Feeds = cfg.Feeds[:0]
	for _, f := range snap.Feeds {
		cfg.Feeds = append(cfg.Feeds, FeedSource{URL: f.URL, WebhookURL: f.WebhookURL})
	}
	hasFeeds := len(cfg.RSSFeeds) > 0 || len(cfg.Feeds) > 0
	cfgMu.Unlock()

	log.InfoFmt("config updated from service: %d feeds", len(snap.RSSFeeds)+len(snap.Feeds))

	configMu.Lock()
	wasEmpty := !configHadFeeds
	configHadFeeds = hasFeeds
	configMu.Unlock()
	if wasEmpty && hasFeeds {
		startPolling()
	}
}
