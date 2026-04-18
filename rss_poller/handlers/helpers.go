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
	"sync"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/coder/websocket"
	"github.com/mmcdole/gofeed"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var sharedHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: otelhttp.NewTransport(&http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	}),
}

var seenMu sync.Mutex
var seen = make(map[string]bool)

var notificationReceiver string

func setNotificationReceiver(addr string) {
	notificationReceiver = addr
}

// discordNotification is the payload sent to the notify service over WebSocket.
type discordNotification struct {
	Content     []string `json:"feed_url"`
	WebHookURL  string   `json:"webhook_url"`
	Traceparent string   `json:"traceparent,omitempty"`
	Tracestate  string   `json:"tracestate,omitempty"`
}

func (d *discordNotification) sendNotification(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	spanCtx, span := instrumentation.GetTracer("poller").Start(ctx, "helper.sendNotification", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.AddEvent("SENDING_NOTIFICATION")
	span.SetAttributes(attribute.Int("items.count", len(d.Content)))

	if d.Content == nil {
		span.SetAttributes(attribute.Int("http.status", http.StatusNoContent))
		return nil
	}

	// Inject OTEL trace context into the payload.
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(spanCtx, carrier)
	d.Traceparent = carrier["traceparent"]
	d.Tracestate = carrier["tracestate"]

	log.Debug("[TRACE] sendNotification: trace context injection",
		zap.String("trace_id", span.SpanContext().TraceID().String()),
		zap.String("traceparent", carrier["traceparent"]))

	dn, err := json.Marshal(&d)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Hold the lock across the write to prevent a race between a pointer swap and the write.
	wsMu.Lock()
	conn := wsConn
	if conn == nil {
		wsMu.Unlock()
		log.Error("WebSocket not connected to notify, dropping notification")
		return nil
	}
	wsMu.Unlock()

	log.Debug("[TRACE] sendNotification: sending payload to notify service via WebSocket",
		zap.String("trace_id", span.SpanContext().TraceID().String()),
		zap.Int("payload.size", len(dn)))
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = conn.Write(writeCtx, websocket.MessageText, dn)
	if err != nil {
		wsMu.Lock()
		wsConn = nil
		wsMu.Unlock()
		log.ErrorFmt("WebSocket write to notify failed: %v", err)
		span.RecordError(err)
		return err
	}

	log.Debug("[TRACE] sendNotification: WebSocket write successful",
		zap.String("trace_id", span.SpanContext().TraceID().String()))

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
	span.SetStatus(codes.Error, logMsg)
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

func toJSON(ctx context.Context, w io.Writer, feeds []*gofeed.Feed) error {
	var jFeeds []feedsJSON
	lctx, span := instrumentation.GetTracer("poller").Start(ctx, "helper.toJSON", trace.WithSpanKind(trace.SpanKindInternal))
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
		if ep := os.Getenv("NOTIFICATION_ENDPOINT"); ep != "" {
			setNotificationReceiver(ep)
		}
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
	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.ParseRSS", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.AddEvent("PARSING_FEED")
	span.SetAttributes(attribute.Int("feeds.expected", len(feedURL)))

	feeds := make([]*gofeed.Feed, len(feedURL))
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(10)

	for i, v := range feedURL {
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

// setRSSFeeds replaces the active feed list under the config lock.
func setRSSFeeds(feeds []string) {
	cfgMu.Lock()
	cfg.RSSFeeds = feeds
	cfgMu.Unlock()
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

	cfgMu.Lock()
	jReader := strings.NewReader(string(body))
	defer cfgMu.Unlock()
	return json.NewDecoder(jReader).Decode(&cfg)
}

func itemKey(it *gofeed.Item) string {
	if it.GUID != "" {
		return it.GUID
	}
	return it.Link
}

// collectNewLinks scans feeds and returns URLs not yet present in the seen map,
// recording each newly encountered key into seen. Items with empty links are skipped.
// The GUID (or Link when no GUID) is used as the dedup key; only the Link is
// appended to the returned slice so callers always receive real URLs.
// A child span is created so the dedup step is visible inside the PollAndNotify trace.
func collectNewLinks(ctx context.Context, feeds []*gofeed.Feed) []string {
	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.collectNewLinks",
		trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	var toSend []string
	seenMu.Lock()
	defer seenMu.Unlock()
	for _, feed := range feeds {
		for _, it := range feed.Items {
			k := itemKey(it)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			if it.Link != "" {
				toSend = append(toSend, it.Link)
			}
		}
	}
	span.SetAttributes(attribute.Int("new.items", len(toSend)))
	return toSend
}

// pollAndNotify contains the core logic for a single polling cycle.
func pollAndNotify(t time.Time) {
	log.InfoFmt("Poller ticker: tick at %v", t)

	tracer := instrumentation.GetTracer("poller")
	cycleCtx, cycleSpan := tracer.Start(
		context.Background(),
		"poller.PollAndNotify",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	// Always end the cycle span when this function returns so the span
	// lifecycle is deterministic and never leaks.
	defer cycleSpan.End()

	cfgMu.RLock()
	feedsURL := cfg.RSSFeeds
	cfgMu.RUnlock()
	// Fetch the latest feeds.
	feeds, err := ParseRSS(cycleCtx, feedsURL)
	if err != nil {
		cycleSpan.RecordError(err)
		return
	}

	// Deduplicate: collectNewLinks is a child span of PollAndNotify.
	toSend := collectNewLinks(cycleCtx, feeds)
	cycleSpan.SetAttributes(attribute.Int("new.items", len(toSend)))

	// Safely update the globalFeed with the latest data.
	feedMutex.Lock()
	globalFeed = feeds
	feedMutex.Unlock()

	if len(toSend) == 0 {
		return
	}
	if notificationReceiver == "" {
		log.Error("NOTIFICATION_ENDPOINT not set, skipping notification.")
		return
	}

	// Send the notification asynchronously so it does not delay updating
	// globalFeed.  The detached context carries cycleSpan so the goroutine's
	// child span (helper.sendNotification) appears nested in the same trace,
	// even after pollAndNotify has returned and cycleSpan has been exported.
	// OTel parent-child linkage is recorded at child-start time (span IDs are
	// copied), so ending the parent first does not break the trace hierarchy.
	notifCtx := trace.ContextWithSpan(context.Background(), cycleSpan)
	notify := discordNotification{
		Content:    toSend,
		WebHookURL: notificationReceiver,
	}
	go func() {
		if err := notify.sendNotification(notifCtx); err != nil {
			log.ErrorFmt("Failed to send notification: %v", err)
		}
	}()
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
