package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/FKouhai/rss-poller/instrumentation"
	log "github.com/FKouhai/rss-poller/logger"
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
	dn, err := json.Marshal(&d)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest("POST", dst, bytes.NewReader(dn))
	if err != nil {
		return 0, err
	}
	req.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	// nolint
	defer res.Body.Close()
	return res.StatusCode, nil
}

func processFeeds(feeds *gofeed.Feed, ctx context.Context) []feedsJSON {
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
		pFeeds := processFeeds(v, lctx)
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

	// create a hashmap to see which values are old
	isOld := make(map[string]bool)
	// within that hashmap mark said values as old
	for _, v := range base {
		isOld[v.Link] = true
	}
	// go over the possible new slice and if it's values are not
	// part of the old values append that to the new array and return that
	for _, possibleNewFeed := range extra {
		if !isOld[possibleNewFeed.Link] {
			diffs = append(diffs, possibleNewFeed.Link)
		}
	}

	return diffs
}
