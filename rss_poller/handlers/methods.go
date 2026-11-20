// Package handlers contains the needed http handlerFunctions and helper functions for the backend logic
package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	log "github.com/FKouhai/rss-demo/libs/logger"
)

// ConfigStruct contains the accepted config fields that this microservice will use.
// RSSFeeds is the legacy plain URL list; Feeds carries per-feed overrides.
// WebhookURL is the global default; per-feed entries win over it, and the
// DEFAULT_WEBHOOK_URL environment variable is the last resort.
type ConfigStruct struct {
	RSSFeeds   []string     `json:"rss_feeds"`
	WebhookURL string       `json:"webhook_url,omitempty"`
	Feeds      []FeedSource `json:"feeds,omitempty"`
}

// FeedSource is a single feed subscription with an optional per-feed
// webhook override. When empty, the global WebhookURL applies.
type FeedSource struct {
	URL        string `json:"url"`
	WebhookURL string `json:"webhook_url,omitempty"`
}

var (
	cfg ConfigStruct
	// Store ticker and cancel func for cleanup
	// nolint:unused // This variable is assigned in helpers.go and used for cleanup
	ticker *time.Ticker
	// config mutex
	cfgMu sync.RWMutex
	// poller mutex
	pollerMu sync.Mutex

	// nolint:unused // This variable is assigned in helpers.go and used for cleanup
	cancelFn context.CancelFunc
)

// ReadyHandler reports readiness. The process only reaches this point after
// NATS is connected and streams are ensured (Run hard-fails otherwise),
// so serving HTTP here means ready.
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, span := tracer.StartServer(r.Context(), "handlers.ReadyHandler")
	defer span.End()
	log.InfoCtx(ctx, "connection to /ready established")

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// HealthzHandler is the route that exposes a healthcheck
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.HealthzHandler")
	defer span.End()
	log.InfoCtx(r.Context(), "connection to /health established")
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}
