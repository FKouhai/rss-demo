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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// internal discordNotification struct used to parse and send the payload that's compliant with the rss_notification service
type discordNotification struct {
	Content    []string `json:"feed_url"`
	WebHookURL string   `json:"webhook_url"`
}

func (d *discordNotification) sendNotification(dst string) (int, error) {
	if d.Content == nil {
		return http.StatusNoContent, nil
	}
	dn, err := json.Marshal(&d)
	if err != nil {
		return 0, err
	}
	log.Info(string(dn))
	req, err := http.NewRequest("POST", dst, bytes.NewReader(dn))
	if err != nil {
		return 0, err
	}
	req.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(req)
	log.InfoFmt("got from notify ep %v", res.StatusCode)
	if err != nil {
		return 0, err
	}
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

func toJSON(feeds []*gofeed.Feed) ([]byte, error) {
	var jFeeds []feedsJSON
	lctx, span := instrumentation.GetTracer("poller").Start(context.Background(), "helper.toJSON", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.AddEvent("INTERNAL::toJSON")

	for _, v := range feeds {
		pFeeds := processFeeds(lctx, v)
		jFeeds = append(jFeeds, pFeeds...)
	}

	b, err := json.Marshal(&jFeeds)
	if err != nil {
		// nolint
		span = httpSpanError(span, "GET", err.Error(), http.StatusBadRequest)
	}

	return b, nil
}

// ParseRSS returns the rss feed with all its items
func ParseRSS(ctx context.Context, feedURL []string) ([]*gofeed.Feed, error) {
	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.ParseRSS", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.AddEvent("PARSING_FEED")
	feedParser := gofeed.NewParser()
	var feeds []*gofeed.Feed
	for _, v := range feedURL {
		feed, err := feedParser.ParseURL(v)
		if err != nil {
			span.AddEvent("FAILED_PROCESS_FEED")
			span.RecordError(err)
			log.Debug(err.Error())
			return nil, err
		}
		feeds = append(feeds, feed)

	}
	log.Info("got feed")
	return feeds, nil
}

// diffie should return either an empty/nil slice or a slice that contains
// the newly added elements
func diffie(base []*gofeed.Feed, extra []*gofeed.Feed) []string {
	var diffs []string

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
