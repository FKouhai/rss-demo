// Package handlers contains the needed http handlerFunctions and helper functions for the backend logic
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"time"

	"github.com/FKouhai/rss-poller/instrumentation"
	log "github.com/FKouhai/rss-poller/logger"
	"github.com/mmcdole/gofeed"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	ticker   *time.Ticker
	cancelFn context.CancelFunc
)

// ConfigHandler reads the config sent via json and stores it in memory
// It also starts a new background poller with the new configuration.
func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := instrumentation.GetTracer("poller").Start(ctx, "handlers.ConfigHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	log.Info("accepted connection")

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
	log.Info("connection to /health established")
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
	rctx, span := instrumentation.GetTracer("poller").Start(ctx, "handlers.RSSHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	feeds := globalFeed
	attributes := spanAttrs{
		httpCode: attribute.Int("http.status", http.StatusOK),
		method:   attribute.String("http.method", "GET"),
	}
	log.Info("connection to /rss established")
	// checks if feeds have already been set, otherwise call ParseRSS and set the feeds locally
	// used as a sanity check to prevent possible race conditions
	if feeds == nil {
		log.Info("got null feeds")
		var err error
		feeds, err = ParseRSS(rctx, cfg.RSSFeeds)
		if err != nil {
			// nolint
			span = httpSpanError(span, r.Method, err.Error(), http.StatusBadRequest)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	body, err := toJSON(feeds)
	if err != nil {
		return
	}
	_, err = w.Write(body)
	if err != nil {
		log.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// nolint
	span = setSpanAttributes(span, attributes)
}
