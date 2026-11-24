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
	"sync/atomic"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/mmcdole/gofeed"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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

// maxItemsPerCycle bounds how many items one tick publishes. Long
// outages surface their backlog as unseen; without a cap a single tick
// fans out into hundreds of downstream spans and one unreadable trace.
// The remainder stays unseen and drains over the next ticks. Duplicates
// need no handling here: NATS MsgId dedup plus the content-hash primary
// key downstream already drop replays.
const maxItemsPerCycle = 5

// primed tracks whether the poller has completed its first full scan.
// The first cycle after (re)start only marks everything seen and
// publishes nothing: the in-memory seen map is empty at boot, so a
// publishing first tick would re-flood the channel with the full
// backlog of every feed (observed on every recreate). Items appearing
// after the prime publish normally.
var primed atomic.Bool

// inFlight guards against overlapping cycles: with tens of feeds and
// slow origins a cycle can outrun the 30s tick, and the ticker fires
// regardless. Two concurrent cycles select overlapping tasters (each
// collects unseen items before the other marks them seen), silently
// multiplying the per-tick publish cap. Skipping the tick is correct:
// the next tick picks up whatever is still unseen.
var inFlight atomic.Bool

// knownFeeds tracks feed URLs already primed. Feeds appearing mid-run
// (added via config after boot) get the same treatment as the boot
// prime: their backlog is marked seen, nothing published, so adding a
// feed doesn't flood downstream with its history. Only genuinely new
// items after that publish.
var (
	knownFeedsMu sync.Mutex
	knownFeeds   = make(map[string]bool)
)

// Metrics bundles the poller counters. All fields optional; nil entries
// are skipped so tests can attach a subset or nothing at all.
type Metrics struct {
	CycleItems  *prometheus.CounterVec
	FetchErrors *prometheus.CounterVec
}

var (
	metricsMu     sync.Mutex
	pollerMetrics *Metrics
)

// SetMetrics installs the counters; nil-safe so tests can skip it.
func SetMetrics(m *Metrics) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	pollerMetrics = m
}

func addCycle(outcome string, n int) {
	if n <= 0 {
		return
	}
	metricsMu.Lock()
	m := pollerMetrics
	metricsMu.Unlock()
	if m == nil || m.CycleItems == nil {
		return
	}
	m.CycleItems.WithLabelValues(outcome).Add(float64(n))
}

func incFetchError() {
	metricsMu.Lock()
	m := pollerMetrics
	metricsMu.Unlock()
	if m == nil || m.FetchErrors == nil {
		return
	}
	m.FetchErrors.WithLabelValues().Inc()
}

func markKnown(urls ...string) {
	knownFeedsMu.Lock()
	defer knownFeedsMu.Unlock()
	for _, u := range urls {
		knownFeeds[u] = true
	}
}

func unknownFeeds(urls []string) []string {
	knownFeedsMu.Lock()
	defer knownFeedsMu.Unlock()
	var out []string
	for _, u := range urls {
		if !knownFeeds[u] {
			out = append(out, u)
		}
	}
	return out
}

// primeSeen marks every candidate seen without publishing. First cycle
// after boot only: restarts go silent instead of replaying history.
func primeSeen(candidates []unseenItem) int {
	keys := make([]string, 0, len(candidates))
	for _, c := range candidates {
		keys = append(keys, c.key)
	}
	markSeen(keys...)
	return len(keys)
}

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
			// Keep the span ctx: the failure log below must carry
			// trace_id so Loki `|= trace_id` finds it next to the
			// "got feed" line (Debug never leaves the collector).
			feedCtx, feedSpan := tracer.Start(feedCtx, "helper.ParseSingleFeed")
			feedSpan.SetAttributes(attribute.String("feed.url", v))
			defer feedSpan.End()
			feedParser := gofeed.NewParser()
			feedParser.Client = sharedHTTPClient
			feed, err := feedParser.ParseURLWithContext(v, feedCtx)
			if err != nil {
				span.AddEvent("FAILED_PROCESS_FEED")
				feedSpan.RecordError(err)
				log.ErrorCtx(feedCtx, "feed failed, skipping", zap.String("url", v), zap.Error(err))
				incFetchError()
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
	// Normalized to RFC3339 so downstream ORDER BY published_at (a
	// plain TEXT column) sorts chronologically. Raw feed dates mix
	// RFC822 and RFC3339, which breaks lexicographic ordering.
	if it.PublishedParsed != nil {
		return it.PublishedParsed.UTC().Format(time.RFC3339)
	}
	if it.UpdatedParsed != nil {
		return it.UpdatedParsed.UTC().Format(time.RFC3339)
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
// dedupe, and publish each new item to feeds.new. New feeds additionally
// hydrate the read model via feeds.backfill (see handleFreshFeeds).
func pollCycle(t time.Time) {
	log.InfoFmt("Poller ticker: tick at %v", t)

	if inFlight.Swap(true) {
		log.Info("Poller ticker: previous cycle still running, skipping tick")
		return
	}
	defer inFlight.Store(false)

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
	urls := feedURLs(snapshot)
	feeds, err := ParseRSS(cycleCtx, urls)
	if err != nil {
		cycleSpan.RecordError(err)
		return
	}

	// Deduplicate: collectUnseen is a child span of PollCycle. The
	// batch is capped so outages drain over several ticks instead
	// of one mega-fan-out; only published items are marked seen.
	// The very first cycle only primes (marks everything, publishes
	// nothing) so restarts don't replay feed history downstream.
	candidates := collectUnseen(cycleCtx, feeds)
	cycleSpan.SetAttributes(attribute.Int("new.items", len(candidates)))
	if !primed.Swap(true) {
		n := primeSeen(candidates)
		markKnown(urls...)
		addCycle("primed", n)
		cycleSpan.SetAttributes(attribute.Int("primed.items", n))
		log.InfoFmt("primed poller with %d known items (nothing published)", n)
		return
	}

	candidates = handleFreshFeeds(cycleSpan, nc, snapshot, urls, candidates)
	batch := selectBatch(candidates, maxItemsPerCycle)
	cycleSpan.SetAttributes(attribute.Int("published.items", len(batch)))

	var published []string
	for _, u := range batch {
		published = append(published, u.key)
	}

	markSeen(published...)

	publishItems(nc, snapshot, batch, pubsub.SubjectFeedsNew, "poller.PublishItem", "published")
}

// resolveFresh splits candidates into backlog/live for newly seen feeds.
// Returns nil fresh when nothing is new, and the candidates untouched.
func resolveFresh(urls []string, candidates []unseenItem) (fresh []string, backlog, live []unseenItem) {
	fresh = unknownFeeds(urls)
	if len(fresh) == 0 {
		return nil, nil, candidates
	}
	freshSet := make(map[string]bool, len(fresh))
	for _, u := range fresh {
		freshSet[u] = true
	}
	for _, c := range candidates {
		if freshSet[c.item.FeedURL] {
			backlog = append(backlog, c)
		} else {
			live = append(live, c)
		}
	}
	return fresh, backlog, live
}

// handleFreshFeeds gives new feeds a taster, not a flood: up to
// maxItemsPerCycle of their newest items publish to feeds.new, the next
// slice hydrates the read model via feeds.backfill (no notify), and the
// rest is dropped (marked seen). Known feeds drain normally within the
// remaining budget, remainder staying unseen for later ticks. Returns
// the candidates with the taster merged into the live batch.
func handleFreshFeeds(cycleSpan trace.Span, nc *pubsub.Client, snapshot ConfigStruct, urls []string, candidates []unseenItem) []unseenItem {
	fresh, backlog, live := resolveFresh(urls, candidates)
	if len(fresh) == 0 {
		return candidates
	}
	taster, backfill, dropped := splitBacklog(backlog)
	addCycle("taster", len(taster))
	addCycle("backfilled", len(backfill))
	addCycle("dropped", len(dropped))
	cycleSpan.SetAttributes(
		attribute.Int("taster.new-feeds", len(taster)),
		attribute.Int("backfilled.new-feeds", len(backfill)),
		attribute.Int("dropped.new-feeds", len(dropped)),
	)
	markKnown(fresh...)
	// Backfill hydrates the read model without notifying: same
	// row ID, different subject, so the aggregator persists it
	// and the notify path never sees it.
	var backfillKeys []string
	for _, u := range backfill {
		backfillKeys = append(backfillKeys, u.key)
	}
	markSeen(backfillKeys...)
	// Dropped items must be marked too: they are terminal (never
	// published, never backfilled), and leaving them unseen would
	// resurface them every tick as live backlog — an endless drain
	// that looks exactly like a flood.
	var dropKeys []string
	for _, u := range dropped {
		dropKeys = append(dropKeys, u.key)
	}
	markSeen(dropKeys...)
	publishItems(nc, snapshot, backfill, pubsub.SubjectFeedsBackfill, "poller.PublishBackfill", "backfilled")
	log.InfoFmt("new feeds %d: taster %d, backfilled %d, dropped %d", len(fresh), len(taster), len(backfill), len(dropped))
	// Taster joins the live batch; total still capped below.
	return append(taster, live...)
}

// publishItems publishes each item to subject with a fresh root trace
// per item: one item's journey stays one readable trace instead of all
// landing in a single PollCycle monster with hundreds of spans.
// okOutcome names successful publishes ("published", "backfilled");
// failures always count "error".
func publishItems(nc *pubsub.Client, snapshot ConfigStruct, items []unseenItem, subject, spanName, okOutcome string) {
	for _, u := range items {
		// Fresh root trace per item: the cycle span only counts, it
		// doesn't parent.
		itemCtx, itemSpan := tracer.Start(context.Background(), spanName)
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
				addCycle("error", 1)
				return
			}
			if _, err := nc.Publish(itemCtx, subject, payload, dm.MsgID()); err != nil {
				instrumentation.Fail(itemSpan, err, "failed to publish item %s: %v", u.item.Link, err)
				addCycle("error", 1)
				return
			}
			addCycle(okOutcome, 1)
		}()
	}
}

const backfillDepthPerFeed = 25

func splitBacklog(backlog []unseenItem) (taster, backfill, dropped []unseenItem) {
	taster = selectBatch(backlog, maxItemsPerCycle)
	taken := make(map[string]bool, len(taster))

	for _, t := range taster {
		taken[t.key] = true
	}

	perFeed := make(map[string]int)
	for _, c := range backlog {
		if taken[c.key] {
			continue
		}
		if perFeed[c.item.FeedURL] < backfillDepthPerFeed {
			backfill = append(backfill, c)
			perFeed[c.item.FeedURL]++
			continue
		}
		dropped = append(dropped, c)
	}
	return taster, backfill, dropped
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
