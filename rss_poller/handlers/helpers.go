// Package handlers contains the needed http handlerFunctions and helper functions for the backend logic
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/coder/websocket"
	"github.com/mmcdole/gofeed"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var sharedHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	},
}

// discordNotification is the payload sent to the notify service over WebSocket.
type discordNotification struct {
	Content     []string `json:"feed_url"`
	WebHookURL  string   `json:"webhook_url"`
	Traceparent string   `json:"traceparent,omitempty"`
	Ctx         context.Context
}

func (d *discordNotification) sendNotification() error {
	ctx := d.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.sendNotification", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.AddEvent("SENDING_NOTIFICATION")
	span.SetAttributes(attribute.Int("items.count", len(d.Content)))

	if d.Content == nil {
		span.SetAttributes(attribute.Int("http.status", http.StatusNoContent))
		return nil
	}

	// Inject OTEL trace context into the payload.
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	d.Traceparent = carrier["traceparent"]

	dn, err := json.Marshal(&d)
	if err != nil {
		span.RecordError(err)
		return err
	}
	log.InfoFmt("Sending payload to notify service via WebSocket: %s", string(dn))
	span.SetAttributes(attribute.Int("payload.size", len(dn)))

	// Hold the lock across the write to prevent a race between a pointer swap and the write.
	wsMu.Lock()
	conn := wsConn
	if conn == nil {
		wsMu.Unlock()
		log.Error("WebSocket not connected to notify, dropping notification")
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = conn.Write(writeCtx, websocket.MessageText, dn)
	if err != nil {
		wsConn = nil
		wsMu.Unlock()
		log.ErrorFmt("WebSocket write to notify failed: %v", err)
		span.RecordError(err)
		return err
	}
	wsMu.Unlock()

	return nil
}

func processFeeds(ctx context.Context, feeds *gofeed.Feed) []feedsJSON {
	var jFeed feedsJSON
	var jFeeds []feedsJSON
	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.PROCESS_FEEDS", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.AddEvent("INTERNAL::processFeeds")
	span.SetAttributes(attribute.Int("feed.items", len(feeds.Items)))
	for _, v := range feeds.Items {
		jFeed.Title = v.Title
		jFeed.Content = v.Content
		jFeed.Description = v.Description
		jFeed.Link = v.Link
		jFeed.Image = v.Image
		jFeeds = append(jFeeds, jFeed)
	}
	return jFeeds

}

func httpSpanError(span trace.Span, method string, logMsg string, httpCode int) trace.Span {
	log.Error(logMsg)
	span.RecordError(errors.New(logMsg), trace.WithStackTrace(true))
	span.SetStatus(http.StatusInternalServerError, logMsg)
	attributes := spanAttrs{
		httpCode: attribute.Int("http.status", httpCode),
		method:   attribute.String("http.method", method),
	}

	return setSpanAttributes(span, attributes)
}
func setSpanAttributes(span trace.Span, attributes spanAttrs) trace.Span {
	span.SetAttributes(attributes.httpCode, attributes.method)
	return span
}

func toJSON(w io.Writer, feeds []*gofeed.Feed) error {
	var jFeeds []feedsJSON
	lctx, span := instrumentation.GetTracer("poller").Start(context.Background(), "helper.toJSON", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.AddEvent("INTERNAL::toJSON")

	for _, v := range feeds {
		pFeeds := processFeeds(lctx, v)
		jFeeds = append(jFeeds, pFeeds...)
	}

	span.SetAttributes(attribute.Int("items.total", len(jFeeds)))

	err := json.NewEncoder(w).Encode(&jFeeds)
	if err != nil {
		// nolint
		span = httpSpanError(span, "GET", err.Error(), http.StatusBadRequest)
		return err
	}

	return nil
}

func configFilePath() string {
	if p := os.Getenv("CONFIG_FILE"); p != "" {
		return p
	}
	return "/etc/rss-poller/config.json"
}

// LoadConfig reads feed URLs from the config file on startup.
// If the file is absent it is a no-op; the service waits for POST /config.
// If feeds are present, polling starts immediately.
func LoadConfig(ctx context.Context) {
	_, span := instrumentation.GetTracer("poller").Start(ctx, "bootstrap.LoadConfig",
		trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	path := configFilePath()
	span.SetAttributes(attribute.String("config.path", path))

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("no config file found, waiting for POST /config")
			return
		}
		span.RecordError(err)
		log.ErrorFmt("failed to read config file: %v", err)
		return
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		span.RecordError(err)
		log.ErrorFmt("failed to parse config file: %v", err)
		return
	}

	span.SetAttributes(attribute.Int("feeds.count", len(cfg.RSSFeeds)))
	log.InfoFmt("loaded %d feeds from config file", len(cfg.RSSFeeds))

	if len(cfg.RSSFeeds) > 0 {
		startPolling()
	}
}

// persistConfig writes the current cfg to the config file.
// It is best-effort: failure is logged but does not surface to the caller
// since ConfigMap mounts in Kubernetes are read-only by design.
func persistConfig(ctx context.Context) {
	_, span := instrumentation.GetTracer("poller").Start(ctx, "bootstrap.persistConfig",
		trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	path := configFilePath()
	span.SetAttributes(attribute.String("config.path", path))

	data, err := json.Marshal(&cfg)
	if err != nil {
		span.RecordError(err)
		return
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		span.RecordError(err)
		log.InfoFmt("config file not writable (expected in Kubernetes): %v", err)
		return
	}

	span.AddEvent("config persisted")
	log.InfoFmt("persisted config to %s", path)
}

// ParseRSS returns the rss feed with all its items
func ParseRSS(ctx context.Context, feedURL []string) ([]*gofeed.Feed, error) {
	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.ParseRSS", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.AddEvent("PARSING_FEED")
	span.SetAttributes(attribute.Int("feeds.expected", len(feedURL)))

	feeds := make([]*gofeed.Feed, len(feedURL))
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(10)

	for i, v := range feedURL {
		i, v := i, v // capture loop variables
		eg.Go(func() error {
			feedCtx, feedCancel := context.WithTimeout(egCtx, 8*time.Second)
			defer feedCancel()
			_, feedSpan := instrumentation.GetTracer("poller").Start(feedCtx, "helper.ParseSingleFeed", trace.WithSpanKind(trace.SpanKindInternal))
			feedSpan.SetAttributes(attribute.String("feed.url", v))
			defer feedSpan.End()
			feedParser := gofeed.NewParser()
			feedParser.Client = sharedHTTPClient
			feed, err := feedParser.ParseURLWithContext(v, feedCtx)
			if err != nil {
				span.AddEvent("FAILED_PROCESS_FEED")
				feedSpan.RecordError(err)
				log.Debug("feed failed, skipping", zap.String("url", v), zap.Error(err))
				return nil
			}
			feeds[i] = feed
			return nil
		})
	}

	_ = eg.Wait() // individual feed errors are already handled per-goroutine above

	var parsed []*gofeed.Feed
	for _, f := range feeds {
		if f != nil {
			parsed = append(parsed, f)
		}
	}
	if len(parsed) == 0 && len(feedURL) > 0 {
		return nil, errors.New("all feeds failed to parse")
	}

	span.SetAttributes(attribute.Int("feeds.parsed", len(parsed)))
	span.AddEvent("got feed")
	log.Info("got feed", zap.String("trace_id", span.SpanContext().TraceID().String()))
	return parsed, nil
}

// diffie should return either an empty/nil slice or a slice that contains
// the newly added elements
func diffie(ctx context.Context, base []*gofeed.Feed, extra []*gofeed.Feed) []string {
	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.diffie", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.AddEvent("COMPARING_FEEDS")

	var diffs []string

	// Count items in base feeds
	baseItemCount := 0
	for _, feed := range base {
		baseItemCount += len(feed.Items)
	}

	// Count items in extra feeds
	extraItemCount := 0
	for _, feed := range extra {
		extraItemCount += len(feed.Items)
	}

	span.SetAttributes(
		attribute.Int("base.items", baseItemCount),
		attribute.Int("extra.items", extraItemCount),
	)

	// Create a map for O(1) lookups of old item links.
	isOld := make(map[string]bool)
	for _, feed := range base {
		for _, item := range feed.Items {
			isOld[item.Link] = true
		}
	}

	// Iterate through new feeds and find items not in the old map.
	for _, newFeed := range extra {
		for _, newItem := range newFeed.Items {
			if !isOld[newItem.Link] {
				diffs = append(diffs, newItem.Link)
			}
		}
	}

	span.SetAttributes(attribute.Int("new.items", len(diffs)))
	return diffs
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
	// nolint:errcheck
	defer r.Body.Close()
	if err != nil {
		return err
	}
	log.Info(string(body))

	jReader := strings.NewReader(string(body))
	return json.NewDecoder(jReader).Decode(&cfg)
}

// pollAndNotify contains the core logic for a single polling cycle.
func pollAndNotify(t time.Time) {
	log.InfoFmt("Poller ticker: tick at %v", t) // TODO: add trace_id if needed

	// Create a new span for this polling cycle
	tracer := instrumentation.GetTracer("poller")
	cycleCtx, cycleSpan := tracer.Start(
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
	elementsToNotify := diffie(cycleCtx, globalFeed, newFeeds)
	cycleSpan.SetAttributes(attribute.Int("new.items", len(elementsToNotify)))

	// If there are new items, send a notification asynchronously so it does not
	// delay updating globalFeed.
	if len(elementsToNotify) > 0 {
		notificationReceiver := os.Getenv("NOTIFICATION_ENDPOINT")

		if notificationReceiver == "" {
			log.Error("NOTIFICATION_ENDPOINT not set, skipping notification.")
		} else {
			// Build a detached context that carries cycleSpan as its active span
			// but is not cancelled when pollAndNotify returns. This ensures
			// sendNotification's child span is properly nested in the trace.
			notifCtx := trace.ContextWithSpan(context.Background(), cycleSpan)
			notify := discordNotification{
				Content:    elementsToNotify,
				WebHookURL: notificationReceiver,
				Ctx:        notifCtx,
			}
			go func() {
				if err := notify.sendNotification(); err != nil {
					log.ErrorFmt("Failed to send notification: %v", err)
				}
			}()
		}
	}

	// Safely update the globalFeed with the latest data
	feedMutex.Lock()
	globalFeed = newFeeds
	feedMutex.Unlock()
}

// startPolling initializes and runs the background poller goroutine.
// Cancels any previously running poller before starting a new one.
func startPolling() {
	if cancelFn != nil {
		cancelFn()
	}
	if ticker != nil {
		ticker.Stop()
	}

	ticker = time.NewTicker(30 * time.Second)
	localTicker := ticker
	pollCtx, cancel := context.WithCancel(context.Background())
	cancelFn = cancel

	go func() {
		log.Info("Started long poller")
		for {
			select {
			case <-pollCtx.Done():
				log.Info("Stopped polling")
				localTicker.Stop()
				return
			case t := <-localTicker.C:
				pollAndNotify(t)
			}
		}
	}()
}
