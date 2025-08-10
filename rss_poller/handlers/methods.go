package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
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

// handleConfigPayload validates the HTTP request and unmarshals the JSON payload.
func handleConfigPayload(r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("the wrong method was used")
	}
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("the request does not contain a JSON payload")
	}
	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		return err
	}

	jReader := strings.NewReader(string(body))
	return json.NewDecoder(jReader).Decode(&cfg)
}

// pollAndNotify contains the core logic for a single polling cycle.
func pollAndNotify(t time.Time) {
	log.InfoFmt("Poller ticker: tick at %v", t)

	// Create a new span for this polling cycle
	cycleCtx, cycleSpan := instrumentation.GetTracer("poller").Start(
		context.Background(),
		"poller.PollAndNotify",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer cycleSpan.End()

	// Fetch the latest feeds
	newFeeds, err := ParseRSS(cycleCtx, cfg.RSSFeeds)
	if err != nil {
		cycleSpan.RecordError(err)
		return
	}

	// Find the new items using diffie
	elementsToNotify := diffie(globalFeed, newFeeds)

	// If there are new items, send a notification
	if len(elementsToNotify) > 0 {
		notificationReceiver := os.Getenv("NOTIFICATION_ENDPOINT")
		notificationService := os.Getenv("NOTIFICATION_SENDER")

		if notificationReceiver == "" || notificationService == "" {
			log.Error("Notification service is misconfigured, skipping notification.")
		} else {
			notify := discordNotification{
				Content:    elementsToNotify,
				WebHookURL: notificationReceiver,
			}
			if _, err := notify.sendNotification(notificationService); err != nil {
				log.ErrorFmt("Failed to send notification: %v", err)
			}
		}
	}

	// Safely update the globalFeed with the latest data
	feedMutex.Lock()
	globalFeed = newFeeds
	feedMutex.Unlock()
}

// startPolling initializes and runs the background poller goroutine.
func startPolling() {
	log.Info("Started long poller")
	ticker = time.NewTicker(30 * time.Second)
	pollCtx, cancel := context.WithCancel(context.Background())
	cancelFn = cancel

	go func() {
		for {
			select {
			case <-pollCtx.Done():
				log.Info("Stopped polling")
				ticker.Stop()
				return
			case t := <-ticker.C:
				pollAndNotify(t)
			}
		}
	}()
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
