// Package handlers contains the needed http handlerFunctions and helper functions for the backend logic
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
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
	Transport: otelhttp.NewTransport(&userAgentTransport{rt: &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	}}),
}

// userAgentTransport stamps a descriptive User-Agent on outbound feed
// fetches. Hosts with bot mitigation (notably Reddit: 429 on the default
// Go UA) need an identifying agent before they serve anything at all.
type userAgentTransport struct {
	rt http.RoundTripper
}

func (t *userAgentTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("User-Agent") == "" {
		clone := r.Clone(r.Context())
		clone.Header.Set("User-Agent", "rss-demo-poller/1.0 (+homelab rss reader)")
		r = clone
	}
	return t.rt.RoundTrip(r)
}

var (
	seenMu sync.Mutex
	seen   = make(map[string]bool)
)

// maxItemsPerCycle bounds how many items one tick publishes. Cold starts
// (or long outages) surface the full backlog as unseen; without a cap a
// single tick fans out into hundreds of downstream spans and one
// unreadable trace. The remainder stays unseen and drains over the next
// ticks. Duplicates need no handling here: NATS MsgId dedup plus the
// content-hash primary key downstream already drop replays.
const maxItemsPerCycle = 5

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

// unseenItem pairs a candidate with its dedup key so the caller can
// decide what to publish before anything is marked seen. Marking only
// published items is what lets selectBatch defer the overflow to later
// ticks instead of dropping it.
type unseenItem struct {
	key  string
	item feedItem
}

// collectUnseen scans feeds and returns entries not yet present in the
// seen map, without marking anything. Items with empty links are
// skipped. The GUID (or Link when no GUID) is used as the dedup key.
// A child span is created so the dedup step is visible inside the
// PollCycle trace.
func collectUnseen(ctx context.Context, feeds []parsedFeed) []unseenItem {
	_, span := tracer.Start(ctx, "helper.collectNewItems")
	defer span.End()

	var items []unseenItem
	seenMu.Lock()
	defer seenMu.Unlock()
	// inCycle dedupes repeats within this scan (same item listed under
	// several feeds). Global seen is only written by markSeen, after the
	// caller decides what to publish.
	inCycle := make(map[string]bool)
	for _, pf := range feeds {
		if pf.Feed == nil {
			continue
		}
		for _, it := range pf.Feed.Items {
			k := itemKey(it)
			if k == "" || seen[k] || inCycle[k] {
				continue
			}
			if it.Link == "" {
				// Record-but-skip, as before: unpublishable, so eager
				// marking can't interact with batch selection.
				seen[k] = true
				continue
			}
			inCycle[k] = true
			items = append(items, unseenItem{
				key: k,
				item: feedItem{
					FeedURL:     pf.URL,
					Title:       it.Title,
					Link:        it.Link,
					PublishedAt: publishedAt(it),
				},
			})
		}
	}
	span.SetAttributes(attribute.Int("new.items", len(items)))
	return items
}

// markSeen records keys as seen. Call only for items actually published;
// everything else stays unseen for the next tick.
func markSeen(keys ...string) {
	seenMu.Lock()
	defer seenMu.Unlock()
	for _, k := range keys {
		seen[k] = true
	}
}

// selectBatch returns up to n candidates, newest-first by PublishedAt.
// PublishedAt is RFC3339 (see publishedAt), so lexicographic order is
// chronological. Items with unparseable dates may misorder, but the cap
// still bounds the burst and the remainder drains on later ticks.
func selectBatch(candidates []unseenItem, n int) []unseenItem {
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].item.PublishedAt > candidates[j].item.PublishedAt
	})
	if len(candidates) > n {
		candidates = candidates[:n]
	}
	return candidates
}

// collectNewItems scans feeds and returns entries not yet present in the seen
// map, recording each newly encountered key into seen. Items with empty links
// are skipped. The GUID (or Link when no GUID) is used as the dedup key.
// A child span is created so the dedup step is visible inside the PollCycle trace.
func collectNewItems(ctx context.Context, feeds []parsedFeed) []feedItem {
	items := collectUnseen(ctx, feeds)
	keys := make([]string, 0, len(items))
	out := make([]feedItem, 0, len(items))
	for _, u := range items {
		keys = append(keys, u.key)
		out = append(out, u.item)
	}
	markSeen(keys...)
	return out
}

// pollCycle contains the core logic for a single polling cycle: parse,
// dedupe, and publish each new item to feeds.new.
func pollCycle(t time.Time) {
	log.InfoFmt("Poller ticker: tick at %v", t)

	cycleCtx, cycleSpan := tracer.Start(context.Background(), "poller.PollCycle")
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

	// Deduplicate: collectUnseen is a child span of PollCycle. The
	// batch is capped so cold starts drain over several ticks instead
	// of one mega-fan-out; only published items are marked seen.
	candidates := collectUnseen(cycleCtx, feeds)
	cycleSpan.SetAttributes(attribute.Int("new.items", len(candidates)))
	batch := selectBatch(candidates, maxItemsPerCycle)
	cycleSpan.SetAttributes(attribute.Int("published.items", len(batch)))
	var published []string
	for _, u := range batch {
		published = append(published, u.key)
	}
	markSeen(published...)

	for _, u := range batch {
		// Fresh root trace per item: the cycle span only counts, it
		// doesn't parent. One item's journey stays one readable
		// trace instead of all landing in a single PollCycle
		// monster with hundreds of spans.
		itemCtx, itemSpan := tracer.Start(context.Background(), "poller.PublishItem")
		itemSpan.SetAttributes(
			attribute.String("feed.url", u.item.FeedURL),
			attribute.String("item.link", u.item.Link),
		)
		func() {
			defer itemSpan.End()
			dm := pubsub.RSSItem{
				FeedURL:     u.item.FeedURL,
				Title:       u.item.Title,
				Link:        u.item.Link,
				PublishedAt: u.item.PublishedAt,
				WebhookURL:  resolveWebhook(u.item.FeedURL, snapshot),
			}
			payload, err := json.Marshal(dm)
			if err != nil {
				instrumentation.Fail(itemSpan, err, "failed to marshal item %s: %v", u.item.Link, err)
				return
			}
			if _, err := nc.Publish(itemCtx, pubsub.SubjectFeedsNew, payload, dm.MsgID()); err != nil {
				instrumentation.Fail(itemSpan, err, "failed to publish item %s: %v", u.item.Link, err)
				return
			}
		}()
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
				pollCycle(t)
			}
		}
	}()
}
