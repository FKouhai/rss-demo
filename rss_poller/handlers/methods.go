// Package handlers contains the needed http handlerFunctions and helper functions for the backend logic
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
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
	// locatorClient is a shared HTTP client for locator calls with otelhttp instrumentation.
	locatorClient = &http.Client{
		Timeout: 2 * time.Second,
		Transport: otelhttp.NewTransport(&http.Transport{
			MaxIdleConns:        5,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     30 * time.Second,
		}),
	}
)

// isRegistered does a single, fast check against the locator to verify a service is registered.
// No retries — readiness probes must be fast and Kubernetes handles the retry cadence.
// A child span is created and its context is injected as W3C traceparent headers so the
// locator's RequestTracing middleware can parent its own spans to this trace.
func isRegistered(ctx context.Context, locatorURL, service string) bool {
	callCtx, span := tracer.StartClient(ctx, "helper.isRegistered")
	defer span.End()
	span.SetAttributes(
		attribute.String("locator.service", service),
		attribute.String("locator.url", locatorURL),
	)

	body, err := json.Marshal(map[string]string{"service": service})
	if err != nil {
		span.RecordError(err)
		return false
	}
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost,
		fmt.Sprintf("%s/services", locatorURL), bytes.NewBuffer(body))
	if err != nil {
		span.RecordError(err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	// otelhttp.NewTransport in locatorClient injects traceparent automatically
	resp, err := locatorClient.Do(req)
	if err != nil {
		span.RecordError(err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
	return resp.StatusCode == http.StatusOK
}

// ReadyHandler returns 200 when the poller is registered with the locator and its
// notify dependency is also registered. Returns 503 otherwise.
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, span := tracer.StartServer(r.Context(), "handlers.ReadyHandler")
	defer span.End()
	log.InfoCtx(ctx, "connection to /ready established")

	locatorURL := os.Getenv("LOCATOR_URL")
	if locatorURL == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "note": "LOCATOR_URL not set, skipping registration check"})
		return
	}
	if !isRegistered(ctx, locatorURL, "poller") {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready", "reason": "poller not registered with locator"})
		return
	}
	if !isRegistered(ctx, locatorURL, "notify") {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready", "reason": "notify dependency not registered"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// HealthzHandler is the route that exposes a healthcheck
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.HealthzHandler")
	defer span.End()
	log.InfoCtx(r.Context(), "connection to /health established")
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}
