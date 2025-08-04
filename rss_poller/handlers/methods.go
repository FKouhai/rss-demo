package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := instrumentation.GetTracer("poller").Start(ctx, "handlers.ConfigHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	attributes := spanAttrs{
		httpCode: attribute.Int("http.status", http.StatusOK),
		method:   attribute.String("http.method", "POST"),
	}

	log.Info("accepted connection")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		// nolint
		span = httpSpanError(span, r.Method, "the wrong method was used", http.StatusBadRequest)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		// nolint
		span = httpSpanError(span, r.Method, "the request does not contain a JSON payload", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	//nolint
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		//nolint
		span = httpSpanError(span, r.Method, err.Error(), http.StatusInternalServerError)
		return
	}

	jReader := strings.NewReader(string(body))
	err = json.NewDecoder(jReader).Decode(&cfg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		// nolint
		span = httpSpanError(span, r.Method, err.Error(), http.StatusBadRequest)
		return
	}

	stopPolling()
	// nolint
	span = setSpanAttributes(span, attributes)
	// w.Write([]byte(cfg.RSSFeeds))

	/*
	 polling mechanism:
	 taking advantage of the time ticker we are able to call every 5 minutes the ParseRSS function
	*/
	ticker = time.NewTicker(300 * time.Second)
	pollCtx, cancel := context.WithCancel(context.Background())
	cancelFn = cancel
	go func() {
		for {
			select {
			case <-pollCtx.Done():
				log.Info("Stoped polling")
				ticker.Stop()
				return
			case t := <-ticker.C:
				log.Info(t.String())
				/*
				 creating a new span so in our tracing tool we do not start to see 1 span every 5 minutes
				 making the transaction take an infinite amount of time
				*/
				pollCtx, pollSpan := instrumentation.GetTracer("poller").Start(
					context.Background(),
					"poller.RSSFetchCycle",
					trace.WithSpanKind(trace.SpanKindInternal),
				)
				feeds, err := ParseRSS(pollCtx, cfg.RSSFeeds)
				if err != nil {
					// nolint
					pollSpan = httpSpanError(pollSpan, r.Method, err.Error(), http.StatusInternalServerError)
					pollSpan.End()
					return
				}
				// updating cached feed safely to prevent race conditions
				feedMutex.Lock()
				globalFeed = feeds
				feedMutex.Unlock()
				// Once the call to ParseRSS is done we stop our span
				pollSpan.End()
			}
		}
	}()
}

func stopPolling() {
	if cancelFn != nil {
		cancelFn()
	}
	if ticker != nil {
		ticker.Stop()
	}
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
	// TODO format as a JSON blob the response contents from the RSS feeds.
	// The json blob should contain the title,description,content and source url of where to find the actual content
	// With that endpoint exposed the notification microservice should be contacted to trigger a notification to the end user destination source  (telegram,discord)
	// auth between services should be based on mtls
	// Add a way for user auth and per user rss feeds
	// Auth should be added to /config and /rss
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
