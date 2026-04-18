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
	"github.com/mmcdole/gofeed"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ConfigStruct contains the accepted config fields that this microservice will use
type ConfigStruct struct {
	RSSFeeds []string `json:"rss_feeds"`
}

type feedsJSON struct {
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	Content     string        `json:"content,omitempty"`
	Link        string        `json:"link,omitempty"`
	Image       *gofeed.Image `json:"image,omitempty"`
}

var (
	globalFeed []*gofeed.Feed
	cfg        ConfigStruct
	feedMutex  sync.RWMutex
	// Store ticker and cancel func for cleanup
	// nolint:unused // This variable is assigned in helpers.go and used for cleanup
	ticker *time.Ticker
	// config mutex
	cfgMu sync.RWMutex
	// notify mutex
	notifyMu sync.RWMutex
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
	callCtx, span := startSpan(ctx, "helper.isRegistered", trace.SpanKindClient)
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

	ctx, span := startSpan(r.Context(), "handlers.ReadyHandler", trace.SpanKindServer)
	defer span.End()
	log.Info("connection to /ready established", zap.String("trace_id", span.SpanContext().TraceID().String()))

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

// ConfigHandler reads the config sent via json and stores it in memory.
// It also starts a new background poller with the new configuration.
func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := startSpan(ctx, "handlers.ConfigHandler", trace.SpanKindServer)
	defer span.End()
	log.Info("accepted connection", zap.String("trace_id", span.SpanContext().TraceID().String()))

	if err := handleConfigPayload(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		httpSpanError(span, r.Method, err.Error(), http.StatusBadRequest)
		return
	}

	persistConfig(ctx)
	startPolling()

	recordHTTPSpan(span, r.Method, http.StatusOK)
	span.SetAttributes(attribute.Int("feeds.count", len(getConfigSnapshot().RSSFeeds)))
	w.WriteHeader(http.StatusOK)
}

// ConfigGetHandler returns the feed URLs currently being polled.
// The frontend uses this to stay in sync with the active config.
func ConfigGetHandler(w http.ResponseWriter, r *http.Request) {
	_, span := startSpan(r.Context(), "handlers.ConfigGetHandler", trace.SpanKindServer)
	defer span.End()
	log.Info("connection to GET /config/feeds established", zap.String("trace_id", span.SpanContext().TraceID().String()))

	snapshot := getConfigSnapshot()
	recordHTTPSpan(span, r.Method, http.StatusOK)
	span.SetAttributes(attribute.Int("feeds.count", len(snapshot.RSSFeeds)))

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusOK, snapshot)
}

// HealthzHandler is the route that exposes a healthcheck
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	_, span := startSpan(r.Context(), "handlers.HealthzHandler", trace.SpanKindServer)
	defer span.End()
	log.Info("connection to /health established", zap.String("trace_id", span.SpanContext().TraceID().String()))
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// RSSHandler is the route that exposes the rss feeds that have been polled
func RSSHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:4321")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	rctx, span := startSpan(ctx, "handlers.RSSHandler", trace.SpanKindServer)
	defer span.End()
	log.Info("connection to /rss established", zap.String("trace_id", span.SpanContext().TraceID().String()))

	feedMutex.RLock()
	feeds := globalFeed
	feedMutex.RUnlock()

	// Use cached feeds when available; fall back to a live parse on first request.
	if feeds == nil {
		log.Info("got null feeds", zap.String("trace_id", span.SpanContext().TraceID().String()))
		var err error
		feeds, err = ParseRSS(rctx, getConfigSnapshot().RSSFeeds)
		if err != nil {
			httpSpanError(span, r.Method, err.Error(), http.StatusBadRequest)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	if err := toJSON(rctx, w, feeds); err != nil {
		log.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	recordHTTPSpan(span, r.Method, http.StatusOK)
}
