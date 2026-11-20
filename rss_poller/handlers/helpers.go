// Package handlers contains the needed http handlerFunctions and helper functions for the backend logic
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/mmcdole/gofeed"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
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

var (
	seenMu sync.Mutex
	seen   = make(map[string]bool)
)

var (
	natsClient *pubsub.Client
	natsMu     sync.RWMutex
)

// SetNATSClient installs the shared NATS connection used for publishing.
// Called once from cmd.Run; a nil client means publish paths are skipped.
func SetNATSClient(c *pubsub.Client) {
	natsMu.Lock()
	defer natsMu.Unlock()
	natsClient = c
}

func getNATSClient() *pubsub.Client {
	natsMu.RLock()
	defer natsMu.RUnlock()
	return natsClient
}

func configFilePath() string {
	if p := os.Getenv("CONFIG_FILE"); p != "" {
		return p
	}
	return "/etc/rss-poller/config.json"
}

// LoadConfig reads feed URLs from the config file on startup.
// If the file is absent it is a no-op; the service waits for the config
// service. If feeds are present, polling starts immediately.
func LoadConfig(ctx context.Context) {
	_, span := tracer.Start(ctx, "poller.LoadConfig")
	defer span.End()

	path := configFilePath()
	span.SetAttributes(attribute.String("config.path", path))

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.InfoCtx(ctx, "no config file found, polling not started")
			return
		}
		instrumentation.Fail(span, err, "failed to read config file: %v", err)
		return
	}

	cfgMu.Lock()
	if err := json.Unmarshal(data, &cfg); err != nil {
		cfgMu.Unlock()
		instrumentation.Fail(span, err, "failed to parse config file: %v", err)
		return
	}
	snapshot := ConfigStruct{RSSFeeds: cfg.RSSFeeds, WebhookURL: cfg.WebhookURL, Feeds: cfg.Feeds}
	log.InfoFmtCtx(ctx, "loaded %d feeds from config file", len(snapshot.RSSFeeds)+len(snapshot.Feeds))
	hasFeeds := len(snapshot.RSSFeeds) > 0 || len(snapshot.Feeds) > 0
	cfgMu.Unlock()

	if hasFeeds {
		startPolling()
	}
}

// feedURLs flattens the legacy URL list and the richer per-feed entries
// into a deduplicated poll list.
func feedURLs(cfg ConfigStruct) []string {
	seenURLs := make(map[string]bool)
	var out []string
	for _, u := range cfg.RSSFeeds {
		if u != "" && !seenURLs[u] {
			seenURLs[u] = true
			out = append(out, u)
		}
	}
	for _, f := range cfg.Feeds {
		if f.URL != "" && !seenURLs[f.URL] {
			seenURLs[f.URL] = true
			out = append(out, f.URL)
		}
	}
	return out
}

// resolveWebhook picks the webhook URL for a feed: per-feed override first,
// then the global default, then the DEFAULT_WEBHOOK_URL environment fallback.
func resolveWebhook(feedURL string, cfg ConfigStruct) string {
	for _, f := range cfg.Feeds {
		if f.URL == feedURL && f.WebhookURL != "" {
			return f.WebhookURL
		}
	}
	if cfg.WebhookURL != "" {
		return cfg.WebhookURL
	}
	return os.Getenv("DEFAULT_WEBHOOK_URL")
}

// parsedFeed pairs a fetched feed with the URL it was polled from, so
// downstream stages never lose the feed↔item association.
type parsedFeed struct {
	URL  string
	Feed *gofeed.Feed
}

// ParseRSS returns the rss feed with all its items, each paired with its
// source URL. Feeds that fail to parse are skipped.
func ParseRSS(ctx context.Context, feedURL []string) ([]parsedFeed, error) {
	_, span := tracer.StartClient(ctx, "helper.ParseRSS")
	defer span.End()
	span.AddEvent("PARSING_FEED")
	span.SetAttributes(attribute.Int("feeds.expected", len(feedURL)))

	feeds := make([]*gofeed.Feed, len(feedURL))
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(10)

	for i, v := range feedURL {
		eg.Go(func() error {
			feedCtx, cancel := context.WithTimeout(egCtx, 8*time.Second)
			defer cancel()
			_, feedSpan := tracer.Start(feedCtx, "helper.ParseSingleFeed")
			feedSpan.SetAttributes(attribute.String("feed.url", v))
			defer feedSpan.End()
			feedParser := gofeed.NewParser()
			feedParser.Client = sharedHTTPClient
			feed, err := feedParser.ParseURLWithContext(v, feedCtx)
			if err != nil {
				span.AddEvent("FAILED_PROCESS_FEED")
				feedSpan.RecordError(err)
				log.DebugCtx(feedCtx, "feed failed, skipping", zap.String("url", v), zap.Error(err))
				return nil
			}
			feeds[i] = feed
			return nil
		})
	}

	_ = eg.Wait() // individual feed errors are already handled per-goroutine above

	var parsed []parsedFeed
	for i, f := range feeds {
		if f != nil {
			parsed = append(parsed, parsedFeed{URL: feedURL[i], Feed: f})
		}
	}
	if len(parsed) == 0 && len(feedURL) > 0 {
		return nil, errors.New("all feeds failed to parse")
	}

	span.SetAttributes(attribute.Int("feeds.parsed", len(parsed)))
	span.AddEvent("got feed")
	log.InfoCtx(ctx, "got feed")
	return parsed, nil
}

func itemKey(it *gofeed.Item) string {
	if it.GUID != "" {
		return it.GUID
	}
	return it.Link
}

// feedItem is a deduplicated entry ready to publish, carrying the feed it
// came from so publishers never lose the feed↔item association.
type feedItem struct {
	FeedURL     string
	Title       string
	Link        string
	PublishedAt string
}

func publishedAt(it *gofeed.Item) string {
	if it.PublishedParsed != nil {
		return it.PublishedParsed.UTC().Format(time.RFC3339)
	}
	return it.Published
}

// collectNewItems scans feeds and returns entries not yet present in the seen
// map, recording each newly encountered key into seen. Items with empty links
// are skipped. The GUID (or Link when no GUID) is used as the dedup key.
// A child span is created so the dedup step is visible inside the PollAndNotify trace.
func collectNewItems(ctx context.Context, feeds []parsedFeed) []feedItem {
	_, span := tracer.Start(ctx, "helper.collectNewItems")
	defer span.End()

	var items []feedItem
	seenMu.Lock()
	defer seenMu.Unlock()
	for _, pf := range feeds {
		if pf.Feed == nil {
			continue
		}
		for _, it := range pf.Feed.Items {
			k := itemKey(it)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			if it.Link == "" {
				continue
			}
			items = append(items, feedItem{
				FeedURL:     pf.URL,
				Title:       it.Title,
				Link:        it.Link,
				PublishedAt: publishedAt(it),
			})
		}
	}
	span.SetAttributes(attribute.Int("new.items", len(items)))
	return items
}

// pollAndNotify contains the core logic for a single polling cycle: parse,
// dedupe, and publish each new item to feeds.new.
func pollAndNotify(t time.Time) {
	log.InfoFmt("Poller ticker: tick at %v", t)

	cycleCtx, cycleSpan := tracer.Start(context.Background(), "poller.PollAndNotify")
	// Always end the cycle span when this function returns so the span
	// lifecycle is deterministic and never leaks.
	defer cycleSpan.End()

	nc := getNATSClient()
	if nc == nil {
		cycleSpan.AddEvent("nats unavailable, skipping cycle")
		log.ErrorCtx(cycleCtx, "NATS not connected, skipping poll cycle")
		return
	}

	snapshot := getConfigSnapshot()
	feeds, err := ParseRSS(cycleCtx, feedURLs(snapshot))
	if err != nil {
		cycleSpan.RecordError(err)
		return
	}

	// Deduplicate: collectNewItems is a child span of PollAndNotify.
	items := collectNewItems(cycleCtx, feeds)
	cycleSpan.SetAttributes(attribute.Int("new.items", len(items)))

	for _, it := range items {
		dm := pubsub.RSSItem{
			FeedURL:     it.FeedURL,
			Title:       it.Title,
			Link:        it.Link,
			PublishedAt: it.PublishedAt,
			WebhookURL:  resolveWebhook(it.FeedURL, snapshot),
		}
		payload, err := json.Marshal(dm)
		if err != nil {
			cycleSpan.RecordError(err)
			log.ErrorFmtCtx(cycleCtx, "failed to marshal item %s: %v", it.Link, err)
			continue
		}
		if _, err := nc.Publish(cycleCtx, pubsub.SubjectFeedsNew, payload, dm.MsgID()); err != nil {
			cycleSpan.RecordError(err)
			log.ErrorFmtCtx(cycleCtx, "failed to publish item %s: %v", it.Link, err)
			continue
		}
	}
}

// startPolling initializes and runs the background poller goroutine.
// Cancels any previously running poller before starting a new one.
func startPolling() {
	pollerMu.Lock()
	defer pollerMu.Unlock()
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
