package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"time"

	"github.com/FKouhai/rss-poller/instrumentation"
	log "github.com/FKouhai/rss-poller/logger"
	"github.com/mmcdole/gofeed"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ConfigStruct contains the accepted config fields that this microservice will use
type ConfigStruct struct {
	RSSFeeds string `json:"rss_feeds"`
}
type spanAttrs struct {
	method   attribute.KeyValue
	httpCode attribute.KeyValue
}

var globalFeed *gofeed.Feed
var cfg ConfigStruct

// RootHandler exposes the index api handler
func RootHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := instrumentation.GetTracer("poller").Start(ctx, "handlers.RootHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	log.Info("accepted connection")
	w.WriteHeader(http.StatusOK)
	attributes := spanAttrs{
		httpCode: attribute.Int("http.status", http.StatusOK),
		method:   attribute.String("http.method", "GET"),
	}
	span = setSpanAttributes(span, attributes)
	w.Write([]byte("testing 1 2"))
}

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
		log.Error("the wrong method was used")
		span.RecordError(errors.New("the wrong method was used"), trace.WithStackTrace(true))
		span.SetStatus(http.StatusInternalServerError, "the wrong method was used")
		attributes := spanAttrs{
			httpCode: attribute.Int("http.status", http.StatusInternalServerError),
			method:   attribute.String("http.method", "GET"),
		}
		span = setSpanAttributes(span, attributes)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		log.Error("the request does not contain a JSON payload")
		span.RecordError(errors.New("the request does not contain a JSON payload"), trace.WithStackTrace(true))
		span.SetStatus(http.StatusInternalServerError, "the request does not contain a JSON payload")
		attributes := spanAttrs{
			httpCode: attribute.Int("http.status", http.StatusBadRequest),
			method:   attribute.String("http.method", "POST"),
		}
		span = setSpanAttributes(span, attributes)
		return
	}
	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Error("Unexpected request content")
		span.RecordError(err, trace.WithStackTrace(true))
		span.SetStatus(http.StatusInternalServerError, err.Error())
		attributes := spanAttrs{
			httpCode: attribute.Int("http.status", http.StatusInternalServerError),
			method:   attribute.String("http.method", "POST"),
		}
		span = setSpanAttributes(span, attributes)
		return
	}

	jReader := strings.NewReader(string(body))
	err = json.NewDecoder(jReader).Decode(&cfg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Error(err.Error())
		span.RecordError(err, trace.WithStackTrace(true))
		span.SetStatus(http.StatusBadRequest, err.Error())
		attributes := spanAttrs{
			httpCode: attribute.Int("http.status", http.StatusBadRequest),
			method:   attribute.String("http.method", "POST"),
		}
		span = setSpanAttributes(span, attributes)
		return
	}
	span = setSpanAttributes(span, attributes)
	w.Write([]byte(cfg.RSSFeeds))

	/*
	 polling mechanism:
	 taking advantage of the time ticker we are able to call every 5 minutes the ParseRSS function
	*/
	ticker := time.NewTicker(300 * time.Second)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
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
				globalFeed, err = ParseRSS(pollCtx, cfg.RSSFeeds)
				if err != nil {
					attributes := spanAttrs{
						httpCode: attribute.Int("http.status", http.StatusInternalServerError),
						method:   attribute.String("http.method", "POST"),
					}
					w.WriteHeader(http.StatusInternalServerError)
					pollSpan.RecordError(err, trace.WithStackTrace(true))
					pollSpan = setSpanAttributes(pollSpan, attributes)
					pollSpan.SetStatus(http.StatusInternalServerError, err.Error())
					return
				}
				// Once the call to ParseRSS is done we stop our span
				pollSpan.End()
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
			log.Debug(err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			span.RecordError(err, trace.WithStackTrace(true))
			span.SetStatus(http.StatusInternalServerError, err.Error())
			attributes := spanAttrs{
				httpCode: attribute.Int("http.status", http.StatusInternalServerError),
				method:   attribute.String("http.method", "GET"),
			}
			span = setSpanAttributes(span, attributes)
			return
		}
	}
	for _, v := range feeds.Items {
		w.Write([]byte(v.Title + "\n"))
		w.Write([]byte(v.Description + "\n"))
		w.Write([]byte(v.Content))
	}
	span = setSpanAttributes(span, attributes)
}

// ParseRSS returns the rss feed with all its items
func ParseRSS(ctx context.Context, feedURL string) (*gofeed.Feed, error) {
	_, span := instrumentation.GetTracer("poller").Start(ctx, "helper.ParseRSS", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	span.AddEvent("PARSING_FEED")
	feedParser := gofeed.NewParser()
	feed, err := feedParser.ParseURL(feedURL)
	if err != nil {
		span.AddEvent("FAILED_PROCESS_FEED")
		span.RecordError(err)
		log.Debug(err.Error())
		return nil, err
	}
	log.Info("got feed")
	return feed, nil
}

func setSpanAttributes(span trace.Span, attributes spanAttrs) trace.Span {
	span.SetAttributes(attributes.httpCode, attributes.method)
	return span
}
