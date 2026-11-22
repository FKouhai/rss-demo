package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FKouhai/rss-demo/libs/metrics"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/mmcdole/gofeed"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

const mockRSSFeedContent = `
<rss version="2.0">
<channel>
    <title>Test RSS Feed</title>
    <description>This is a test RSS feed.</description>
    <item>
        <title>Test Item 1</title>
        <description>Description for Test Item 1</description>
        <content:encoded><![CDATA[Content for Test Item 1]]></content:encoded>
        <link>http://example.com/item1</link>
    </item>
    <item>
        <title>Test Item 2</title>
        <description>Description for Test Item 2</description>
        <content:encoded><![CDATA[Content for Test Item 2]]></content:encoded>
        <link>http://example.com/item2</link>
    </item>
</channel>
</rss>
`

func TestMain(m *testing.M) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	exitCode := m.Run()
	os.Exit(exitCode)
}
func TestHealth(t *testing.T) {
	req, err := http.NewRequest("GET", "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(HealthzHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusOK {
		t.Fatal(err)
	}
	var response map[string]string
	want := map[string]string{"status": "healthy"}
	err = json.NewDecoder(requestRecorder.Body).Decode(&response)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(want, response) {
		t.Errorf("Expected %v, got: %v", want, response)
	}
}

func startMockRSSFeedServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/rss+xml")
		// nolint
		w.Write([]byte(mockRSSFeedContent))
	}))
}

func TestParseRSS(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()
	feed, err := ParseRSS(context.TODO(), []string{mockServer.URL})
	if err != nil {
		t.Error(err)
	}
	for _, v := range feed {
		if v.Feed.Title != "Test RSS Feed" {
			t.Error("title feed is different than expected")
		}
		if v.Feed.Description != "This is a test RSS feed." {
			t.Error("expected different description")
		}
		if v.URL != mockServer.URL {
			t.Errorf("expected feed URL %q to travel with the feed, got %q", mockServer.URL, v.URL)
		}
		if len(v.Feed.Items) != 2 {
			t.Error("Expected 2 ites in the feed")
		}
		expectedItems := []*gofeed.Item{
			{
				Title:       "Test Item 1",
				Description: "Description for Test Item 1",
				Content:     "Content for Test Item 1",
				Link:        "http://example.com/item1",
			},
			{
				Title:       "Test Item 2",
				Description: "Description for Test Item 2",
				Content:     "Content for Test Item 2",
				Link:        "http://example.com/item2",
			},
		}
		for i, item := range v.Feed.Items {
			if item.Title != expectedItems[i].Title {
				t.Errorf("Expected title %s, got: %s", expectedItems[i].Title, item.Title)
			}
			if item.Description != expectedItems[i].Description {
				t.Errorf("Expected description %s, got: %s", expectedItems[i].Description, item.Description)
			}
			if item.Content != expectedItems[i].Content {
				t.Errorf("Expected content %s, got: %s", expectedItems[i].Content, item.Content)
			}
			if item.Link != expectedItems[i].Link {
				t.Errorf("Expected link %s, got: %s", expectedItems[i].Link, item.Link)
			}
		}
	}
}
func TestCollectNewItems(t *testing.T) {
	item1 := &gofeed.Item{Title: "Item 1", Link: "http://example.com/item1"}
	item2 := &gofeed.Item{Title: "Item 2", Link: "http://example.com/item2"}
	item3 := &gofeed.Item{Title: "Item 3", Link: "http://example.com/item3"}
	itemGUID := &gofeed.Item{GUID: "tag:example.com,2024:42", Link: "http://example.com/item4"}
	itemNoLink := &gofeed.Item{GUID: "guid-no-link", Link: ""}

	feedURL := "https://example.com/feed"
	pf := func(items ...*gofeed.Item) []parsedFeed {
		return []parsedFeed{{URL: feedURL, Feed: &gofeed.Feed{Items: items}}}
	}

	t.Cleanup(func() { seen = make(map[string]bool) })

	// First call: both items are new and carry their feed URL.
	t.Run("FirstCallReturnsAllItems", func(t *testing.T) {
		seen = make(map[string]bool)
		got := collectNewItems(context.Background(), pf(item1, item2))
		want := []feedItem{
			{FeedURL: feedURL, Title: "Item 1", Link: "http://example.com/item1"},
			{FeedURL: feedURL, Title: "Item 2", Link: "http://example.com/item2"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("want %v, got %v", want, got)
		}
	})

	// Second call with the same feeds: seen is already populated — nothing returned.
	t.Run("SecondCallReturnEmpty", func(t *testing.T) {
		got := collectNewItems(context.Background(), pf(item1, item2))
		if len(got) != 0 {
			t.Errorf("expected empty on second call, got %v", got)
		}
	})

	// New item added to feed: only the new item appears.
	t.Run("NewItemOnly", func(t *testing.T) {
		got := collectNewItems(context.Background(), pf(item1, item2, item3))
		if len(got) != 1 || got[0].Link != "http://example.com/item3" {
			t.Errorf("want only item3, got %v", got)
		}
	})

	// Item with GUID: dedup key is GUID, but Link is sent.
	t.Run("GUIDKeyedItem", func(t *testing.T) {
		seen = make(map[string]bool)
		got := collectNewItems(context.Background(), pf(itemGUID))
		if len(got) != 1 || got[0].Link != "http://example.com/item4" {
			t.Errorf("want item4, got %v", got)
		}
		if !seen["tag:example.com,2024:42"] {
			t.Error("expected GUID to be recorded in seen, not the Link")
		}
		// Second call: GUID already seen — nothing sent.
		if got2 := collectNewItems(context.Background(), pf(itemGUID)); len(got2) != 0 {
			t.Errorf("expected empty on second call for GUID item, got %v", got2)
		}
	})

	// Item with GUID but no Link: recorded in seen but never returned.
	t.Run("GUIDWithNoLinkNotSent", func(t *testing.T) {
		seen = make(map[string]bool)
		got := collectNewItems(context.Background(), pf(itemNoLink))
		if len(got) != 0 {
			t.Errorf("expected nothing sent for item with no link, got %v", got)
		}
		if !seen["guid-no-link"] {
			t.Error("expected GUID with no link to still be recorded in seen")
		}
	})

	// Same URL across multiple feeds in a single cycle: must not duplicate.
	t.Run("DuplicateAcrossFeedsInOneCycle", func(t *testing.T) {
		seen = make(map[string]bool)
		feeds := []parsedFeed{
			{URL: feedURL, Feed: &gofeed.Feed{Items: []*gofeed.Item{item1}}},
			{URL: "https://example.com/other", Feed: &gofeed.Feed{Items: []*gofeed.Item{item1}}},
		}
		got := collectNewItems(context.Background(), feeds)
		if len(got) != 1 {
			t.Errorf("expected 1 entry for duplicate across feeds, got %v", got)
		}
	})

	// Feed URL travels with the item for the publisher.
	t.Run("FeedURLPreserved", func(t *testing.T) {
		seen = make(map[string]bool)
		feeds := []parsedFeed{{URL: "https://example.com/specific", Feed: &gofeed.Feed{Items: []*gofeed.Item{item1}}}}
		got := collectNewItems(context.Background(), feeds)
		if len(got) != 1 || got[0].FeedURL != "https://example.com/specific" {
			t.Errorf("expected feed URL to travel with item, got %v", got)
		}
	})

	// Nil feed entries are skipped without panicking.
	t.Run("NilFeedSkipped", func(t *testing.T) {
		seen = make(map[string]bool)
		got := collectNewItems(context.Background(), []parsedFeed{{URL: feedURL}, {URL: feedURL, Feed: &gofeed.Feed{}}})
		if len(got) != 0 {
			t.Errorf("expected nothing for nil/empty feeds, got %v", got)
		}
	})
}

func TestSelectBatch(t *testing.T) {
	mk := func(id, publishedAt string) unseenItem {
		return unseenItem{key: id, item: feedItem{FeedURL: "f", Title: "t" + id, Link: "l" + id, PublishedAt: publishedAt}}
	}
	// PublishedAt zero value sorts last (RFC3339 lexicographic order).
	cands := []unseenItem{
		mk("old", "2024-01-01T00:00:00Z"),
		mk("new", "2026-11-21T00:00:00Z"),
		mk("mid", "2025-06-01T00:00:00Z"),
		mk("zero", ""),
	}
	got := selectBatch(cands, 2)
	if len(got) != 2 || got[0].key != "new" || got[1].key != "mid" {
		t.Errorf("want newest 2 [new mid], got %v", idsOf(got))
	}

	// n larger than the input returns everything, newest-first.
	got = selectBatch(cands, 10)
	if len(got) != 4 || got[3].key != "zero" {
		t.Errorf("want all 4 newest-first with zero last, got %v", idsOf(got))
	}

	// Empty input stays empty.
	if got := selectBatch(nil, 5); len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func idsOf(items []unseenItem) []string {
	out := make([]string, 0, len(items))
	for _, u := range items {
		out = append(out, u.key)
	}
	return out
}
func TestConfigFilePath(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		t.Setenv("CONFIG_FILE", "")
		if got := configFilePath(); got != "/etc/rss-poller/config.json" {
			t.Errorf("expected default path, got %q", got)
		}
	})
	t.Run("EnvOverride", func(t *testing.T) {
		t.Setenv("CONFIG_FILE", "/tmp/test-config.json")
		if got := configFilePath(); got != "/tmp/test-config.json" {
			t.Errorf("expected /tmp/test-config.json, got %q", got)
		}
	})
}

func TestReadyHandler(t *testing.T) {
	// Readiness no longer depends on any registry: serving HTTP means the
	// process completed startup (NATS + streams would have hard-failed Run).
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()
	ReadyHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("body = %v, want status=ready", body)
	}
}
func TestParseRSSAllFeedsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ParseRSS(context.Background(), []string{srv.URL})
	if err == nil {
		t.Error("expected error when all feeds fail to parse, got nil")
	}
}

func TestPollCycleNoNATS(t *testing.T) {
	// Without a NATS connection the cycle is a no-op: nothing parsed, nothing seen.
	SetNATSClient(nil)
	seen = make(map[string]bool)
	t.Cleanup(func() { seen = make(map[string]bool) })

	pollCycle(time.Now())

	if len(seen) != 0 {
		t.Errorf("expected empty seen when NATS is down, got %d entries", len(seen))
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{"rss_feeds": ["https://example.com/rss"], "webhook_url": "https://hooks.example.com/x", "feeds": [{"url": "https://example.com/other", "webhook_url": "https://hooks.example.com/y"}]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)

	old := cfg
	t.Cleanup(func() {
		cfgMu.Lock()
		cfg = old
		cfgMu.Unlock()
		seen = make(map[string]bool)
	})

	LoadConfig(context.Background())

	snap := getConfigSnapshot()
	if len(snap.RSSFeeds) != 1 || snap.RSSFeeds[0] != "https://example.com/rss" {
		t.Errorf("RSSFeeds = %v, want [https://example.com/rss]", snap.RSSFeeds)
	}
	if snap.WebhookURL != "https://hooks.example.com/x" {
		t.Errorf("WebhookURL = %q, want global default", snap.WebhookURL)
	}
	if len(snap.Feeds) != 1 || snap.Feeds[0].URL != "https://example.com/other" {
		t.Errorf("Feeds = %v, want the per-feed entry", snap.Feeds)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.json"))
	// Must not fail or start polling; just a no-op.
	LoadConfig(context.Background())
}

func TestResolveWebhook(t *testing.T) {
	t.Setenv("DEFAULT_WEBHOOK_URL", "https://env.example.com/hook")
	cfg := ConfigStruct{
		RSSFeeds:   []string{"https://example.com/a", "https://example.com/b", "https://example.com/c"},
		WebhookURL: "https://global.example.com/hook",
		Feeds:      []FeedSource{{URL: "https://example.com/a", WebhookURL: "https://feed.example.com/hook"}},
	}
	if got := resolveWebhook("https://example.com/a", cfg); got != "https://feed.example.com/hook" {
		t.Errorf("per-feed override = %q, want feed hook", got)
	}
	if got := resolveWebhook("https://example.com/b", cfg); got != "https://global.example.com/hook" {
		t.Errorf("global default = %q, want global hook", got)
	}
	if got := resolveWebhook("https://example.com/c", ConfigStruct{}); got != "https://env.example.com/hook" {
		t.Errorf("env fallback = %q, want env hook", got)
	}
	t.Setenv("DEFAULT_WEBHOOK_URL", "")
	if got := resolveWebhook("https://example.com/c", ConfigStruct{}); got != "" {
		t.Errorf("no webhook anywhere = %q, want empty", got)
	}
}

func TestPublishedAt(t *testing.T) {
	ts := time.Date(2024, 5, 6, 12, 30, 0, 0, time.UTC)
	it := &gofeed.Item{PublishedParsed: &ts}
	if got := publishedAt(it); got != "2024-05-06T12:30:00Z" {
		t.Errorf("parsed date = %q, want RFC3339", got)
	}
	raw := &gofeed.Item{Published: "Mon, 06 May 2024 12:30:00 GMT"}
	if got := publishedAt(raw); got != "Mon, 06 May 2024 12:30:00 GMT" {
		t.Errorf("raw date = %q, want passthrough", got)
	}
	// UpdatedParsed covers feeds that only carry an updated timestamp;
	// without it the raw value leaks into the store and breaks the
	// aggregator's lexicographic date ordering.
	upd := &gofeed.Item{PublishedParsed: nil, UpdatedParsed: &ts, Published: "Mon, 06 May 2024 12:30:00 GMT"}
	if got := publishedAt(upd); got != "2024-05-06T12:30:00Z" {
		t.Errorf("updated date = %q, want RFC3339", got)
	}
}

func TestFeedURLs(t *testing.T) {
	cfg := ConfigStruct{
		RSSFeeds: []string{"https://example.com/a", "https://example.com/a"},
		Feeds:    []FeedSource{{URL: "https://example.com/b"}, {URL: ""}, {URL: "https://example.com/a"}},
	}
	got := feedURLs(cfg)
	want := []string{"https://example.com/a", "https://example.com/b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("feedURLs = %v, want %v (deduped, order kept)", got, want)
	}
}

func TestUserAgentTransport_SetsDefault(t *testing.T) {
	var gotUA string
	stub := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		gotUA = r.Header.Get("User-Agent")
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.com/feed", nil)
	req.Header.Del("User-Agent")
	if _, err := (&userAgentTransport{rt: stub}).RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if gotUA == "" || strings.HasPrefix(gotUA, "Go-http-client") {
		t.Errorf("User-Agent = %q, want identifying agent", gotUA)
	}
}

func TestUserAgentTransport_PreservesExplicit(t *testing.T) {
	var gotUA string
	stub := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		gotUA = r.Header.Get("User-Agent")
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "https://example.com/feed", nil)
	req.Header.Set("User-Agent", "custom/1.0")
	if _, err := (&userAgentTransport{rt: stub}).RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if gotUA != "custom/1.0" {
		t.Errorf("User-Agent = %q, want explicit value preserved", gotUA)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPrimeSeen_MarksWithoutPublishing(t *testing.T) {
	seenMu.Lock()
	old := seen
	seen = make(map[string]bool)
	seenMu.Unlock()
	t.Cleanup(func() {
		seenMu.Lock()
		seen = old
		seenMu.Unlock()
	})

	cands := []unseenItem{{key: "a"}, {key: "b"}, {key: "c"}}
	if n := primeSeen(cands); n != 3 {
		t.Fatalf("primeSeen = %d, want 3", n)
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	for _, k := range []string{"a", "b", "c"} {
		if !seen[k] {
			t.Errorf("key %q not marked seen", k)
		}
	}
}

func TestPrimeSeen_Empty(t *testing.T) {
	if n := primeSeen(nil); n != 0 {
		t.Fatalf("primeSeen(nil) = %d, want 0", n)
	}
}

func TestKnownFeeds_PrimeNewFeeds(t *testing.T) {
	// New feed URLs report unknown until marked; marking is idempotent.
	knownFeedsMu.Lock()
	old := knownFeeds
	knownFeeds = make(map[string]bool)
	knownFeedsMu.Unlock()
	t.Cleanup(func() {
		knownFeedsMu.Lock()
		knownFeeds = old
		knownFeedsMu.Unlock()
	})
	markKnown("https://example.com/a.rss")
	if got := unknownFeeds([]string{"https://example.com/a.rss", "https://example.com/b.rss"}); len(got) != 1 || got[0] != "https://example.com/b.rss" {
		t.Fatalf("unknownFeeds = %v, want [b]", got)
	}
	markKnown("https://example.com/b.rss")
	if got := unknownFeeds([]string{"https://example.com/a.rss", "https://example.com/b.rss"}); len(got) != 0 {
		t.Fatalf("unknownFeeds after mark = %v, want empty", got)
	}
}

func TestPollCycle_Metrics(t *testing.T) {
	// Prime with feed A (2 items, nothing published), then introduce feed B
	// (2 unseen items). The broker client has no JetStream, so every
	// publish fails: the taster is attempted and counted as error.
	// Asserts the wiring: prime/taster/error counters fire per outcome.
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(mockRSSFeedContent))
	}))
	defer srvA.Close()
	feedB := strings.ReplaceAll(strings.ReplaceAll(mockRSSFeedContent, "item1", "item9"), "item2", "item8")
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(feedB))
	}))
	defer srvB.Close()

	// Save globals; pollCycle runs on shared state.
	oldPrimed := primed.Load()
	seenMu.Lock()
	oldSeen := seen
	seen = make(map[string]bool)
	seenMu.Unlock()
	knownFeedsMu.Lock()
	oldKnown := knownFeeds
	knownFeeds = make(map[string]bool)
	knownFeedsMu.Unlock()
	cfgMu.Lock()
	oldCfg := cfg
	cfgMu.Unlock()
	natsMu.Lock()
	oldNc := natsClient
	natsMu.Unlock()
	SetMetrics(nil)
	t.Cleanup(func() {
		if oldPrimed {
			primed.Store(true)
		} else {
			primed.Store(false)
		}
		seenMu.Lock()
		seen = oldSeen
		seenMu.Unlock()
		knownFeedsMu.Lock()
		knownFeeds = oldKnown
		knownFeedsMu.Unlock()
		cfgMu.Lock()
		cfg = oldCfg
		cfgMu.Unlock()
		SetNATSClient(oldNc)
		SetMetrics(nil)
	})
	primed.Store(false)
	cfgMu.Lock()
	cfg = ConfigStruct{RSSFeeds: []string{srvA.URL}}
	cfgMu.Unlock()
	SetNATSClient(&pubsub.Client{})

	reg := metrics.Init("poller-test")
	cycleItems := reg.Counter("cycle_items_total", "test cycle", "outcome")
	fetchErrors := reg.Counter("fetch_error_total", "test fetch")
	SetMetrics(&Metrics{CycleItems: cycleItems, FetchErrors: fetchErrors})

	pollCycle(time.Now())
	if got := testutil.ToFloat64(cycleItems.WithLabelValues("primed")); got != 2 {
		t.Fatalf("primed = %v, want 2", got)
	}
	if got := testutil.ToFloat64(fetchErrors.WithLabelValues()); got != 0 {
		t.Fatalf("fetch_error = %v, want 0", got)
	}

	cfgMu.Lock()
	cfg = ConfigStruct{RSSFeeds: []string{srvA.URL, srvB.URL}}
	cfgMu.Unlock()
	pollCycle(time.Now())
	if got := testutil.ToFloat64(cycleItems.WithLabelValues("taster")); got != 2 {
		t.Errorf("taster = %v, want 2", got)
	}
	if got := testutil.ToFloat64(cycleItems.WithLabelValues("error")); got != 2 {
		t.Errorf("error = %v, want 2 (broker has no JetStream)", got)
	}
	if got := testutil.ToFloat64(cycleItems.WithLabelValues("published")); got != 0 {
		t.Errorf("published = %v, want 0 (all publishes failed)", got)
	}
}
