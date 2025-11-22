// Package handlers contains the needed http handlerFunctions and helper functions for the backend logic
package handlers

import (
	"bytes"
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
	"github.com/mmcdole/gofeed"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// internal discordNotification struct used to parse and send the payload that's compliant with the rss_notification service
type discordNotification struct {
	Content    []string `json:"feed_url"`
	WebHookURL string   `json:"webhook_url"`
	// Add context to propagate tracing
	Ctx context.Context
}

func (d *discordNotification) sendNotification(dst string) (int, error) {
	// Use the context from the discordNotification struct
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
		return http.StatusNoContent, nil
	}

	dn, err := json.Marshal(&d)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	log.InfoFmt("Sending payload to notify service: %s", string(dn)) // TODO: add trace_id
	span.SetAttributes(attribute.Int("payload.size", len(dn)))

	// Create a new request with context
	req, err := http.NewRequest("POST", dst, bytes.NewReader(dn))
	if err != nil {
		span.RecordError(err)
		return 0, err
	}

	// Add context propagation headers for tracing
	req.Header.Add("Content-Type", "application/json")

	// Inject the tracing context into the request headers
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.ErrorFmt("Failed to send request to notify service: %v", err)
		span.RecordError(err)
		return 0, err
	}
	log.InfoFmt("got from notify ep %v", res.StatusCode)
	span.SetAttributes(attribute.Int("http.status", res.StatusCode))
	// nolint
	defer res.Body.Close()
	return res.StatusCode, nil
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

// ParseRSS returns the rss feed with all its items
func ParseRSS(ctx context.Context, feedURL []string) ([]*gofeed.Feed, error) {
	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.ParseRSS", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.AddEvent("PARSING_FEED")
	span.SetAttributes(attribute.Int("feeds.expected", len(feedURL)))

	feeds := make([]*gofeed.Feed, len(feedURL))
	eg, egCtx := errgroup.WithContext(ctx)

	for i, v := range feedURL {
		i, v := i, v // capture loop variables
		eg.Go(func() error {
			feedCtx, feedSpan := instrumentation.GetTracer("poller").Start(egCtx, "helper.ParseSingleFeed", trace.WithSpanKind(trace.SpanKindInternal))
			feedSpan.SetAttributes(attribute.String("feed.url", v))
			defer feedSpan.End()
			feedParser := gofeed.NewParser()
			feed, err := feedParser.ParseURLWithContext(v, feedCtx)
			if err != nil {
				span.AddEvent("FAILED_PROCESS_FEED")
				span.RecordError(err)
				log.Debug(err.Error())
				return err
			}
			feeds[i] = feed
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	span.SetAttributes(attribute.Int("feeds.parsed", len(feeds)))
	log.Info("got feed", zap.String("trace_id", span.SpanContext().TraceID().String()))
	return feeds, nil
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
				Ctx:        cycleCtx, // Pass the context with the span
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
