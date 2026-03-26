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

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/mmcdole/gofeed"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ConfigStruct contains the accepted config fields that this microservice will use
type ConfigStruct struct {
	RSSFeeds []string `json:"rss_feeds"`
}

type spanAttrs struct {
	method   attribute.KeyValue
	httpCode attribute.KeyValue
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
	// nolint:unused // This variable is assigned in helpers.go and used for cleanup
	cancelFn context.CancelFunc
)

// isRegistered does a single, fast check against the locator to verify a service is registered.
// No retries — readiness probes must be fast and Kubernetes handles the retry cadence.
func isRegistered(ctx context.Context, locatorURL, service string) bool {
	body, err := json.Marshal(map[string]string{"service": service})
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/services", locatorURL), bytes.NewBuffer(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ReadyHandler returns 200 when the poller is registered with the locator and its
// notify dependency is also registered. Returns 503 otherwise.
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	locatorURL := os.Getenv("LOCATOR_URL")
	if locatorURL == "" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ready", "note": "LOCATOR_URL not set, skipping registration check"})
		return
	}

	ctx := r.Context()
	if !isRegistered(ctx, locatorURL, "poller") {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": "poller not registered with locator"})
		return
	}
	if !isRegistered(ctx, locatorURL, "notify") {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": "notify dependency not registered"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// ConfigHandler reads the config sent via json and stores it in memory
// It also starts a new background poller with the new configuration.
func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := instrumentation.GetTracer("poller").Start(ctx, "handlers.ConfigHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	log.Info("accepted connection", zap.String("trace_id", span.SpanContext().TraceID().String()))

	if err := handleConfigPayload(r); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		httpSpanError(span, r.Method, err.Error(), http.StatusBadRequest)
		return
	}

	startPolling()

	span.SetAttributes(
		attribute.Int("http.status", http.StatusOK),
		attribute.String("http.method", "POST"),
	)
	w.WriteHeader(http.StatusOK)
}

// HealthzHandler is the route that exposes a healthcheck
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := instrumentation.GetTracer("poller").Start(ctx, "handlers.HealthzHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	log.Info("connection to /health established", zap.String("trace_id", span.SpanContext().TraceID().String()))
	w.WriteHeader(http.StatusOK)
	status := map[string]string{"status": "healthy"}
	err := json.NewEncoder(w).Encode(status)
	if err != nil {
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusInternalServerError, err.Error())
		log.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

// RSSHandler is the route that exposes the rss feeds that have been polled
func RSSHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:4321")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")
	rctx, span := instrumentation.GetTracer("poller").Start(ctx, "handlers.RSSHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	feeds := globalFeed
	attributes := spanAttrs{
		httpCode: attribute.Int("http.status", http.StatusOK),
		method:   attribute.String("http.method", "GET"),
	}
	log.Info("connection to /rss established", zap.String("trace_id", span.SpanContext().TraceID().String()))
	// checks if feeds have already been set, otherwise call ParseRSS and set the feeds locally
	// used as a sanity check to prevent possible race conditions
	if feeds == nil {
		log.Info("got null feeds", zap.String("trace_id", span.SpanContext().TraceID().String()))
		var err error
		feeds, err = ParseRSS(rctx, cfg.RSSFeeds)
		if err != nil {
			// nolint
			span = httpSpanError(span, r.Method, err.Error(), http.StatusBadRequest)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	err := toJSON(w, feeds)
	if err != nil {
		log.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// nolint
	span = setSpanAttributes(span, attributes)
}
