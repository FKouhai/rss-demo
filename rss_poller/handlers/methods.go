package handlers

import (
	"context"
	"encoding/json"
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
	w.WriteHeader(http.StatusOK)
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

// NotifyHandler sends the payload to the notification service
func NotifyHandler(w http.ResponseWriter, r *http.Request) {
	notificationReceiver := os.Getenv("NOTIFICATION_ENDPOINT")
	notificationService := os.Getenv("NOTIFICATION_SENDER")

	ctx := r.Context()
	rctx, span := instrumentation.GetTracer("poller").Start(ctx, "handlers.NotifyHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	attributes := spanAttrs{
		httpCode: attribute.Int("http.status", http.StatusOK),
		method:   attribute.String("http.method", "POST"),
	}

	feeds, err := ParseRSS(rctx, cfg.RSSFeeds)
	if err != nil {
		return
	}

	elementsToNotify := diffie(globalFeed, feeds)
	if elementsToNotify != nil {
		log.Info("nothing to do here")
		attributes := spanAttrs{
			httpCode: attribute.Int("http.status", http.StatusNoContent),
			method:   attribute.String("http.method", "POST"),
		}
		span = setSpanAttributes(span, attributes)
		return
	}
	notify := discordNotification{
		Content:    elementsToNotify,
		WebHookURL: notificationReceiver,
	}
	status, err := notify.sendNotification(notificationService)
	if err != nil {
		attributes := spanAttrs{
			httpCode: attribute.Int("http.status", http.StatusInternalServerError),
			method:   attribute.String("http.method", "POST"),
		}
		span = setSpanAttributes(span, attributes)
		return
	}
	w.WriteHeader(status)

	span = setSpanAttributes(span, attributes)

}
