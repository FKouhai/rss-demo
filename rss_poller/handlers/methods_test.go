package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mmcdole/gofeed"
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

const mockRSSFeedContentWithNewItem = `
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
    <item>
        <title>Test Item 3</title>
        <description>Description for Test Item 3</description>
        <content:encoded><![CDATA[Content for Test Item 3]]></content:encoded>
        <link>http://example.com/item3</link>
    </item>
</channel>
</rss>
`

func TestMain(m *testing.M) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	exitCode := m.Run()
	os.Exit(exitCode)
}

func TestConfig(t *testing.T) {
	payload := []byte(`{"rss_feeds": ["https://example.com/rss"]}`)
	req, err := http.NewRequest("POST", "/config", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(ConfigHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusOK {
		t.Errorf("Handler returned a different code from 200: %v", requestRecorder.Code)
	}

	expectedFeeds := []string{"https://example.com/rss"}
	if !reflect.DeepEqual(cfg.RSSFeeds, expectedFeeds) {
		t.Errorf("Expected feeds: %v, got: %v", expectedFeeds, cfg.RSSFeeds)
	}
}

func TestConfigErrors(t *testing.T) {
	testCases := []struct {
		name           string
		request        *http.Request
		expectedStatus int
	}{
		{
			name:           "InvalidMethod",
			request:        httptest.NewRequest("GET", "/config", nil),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "InvalidJSON",
			request:        httptest.NewRequest("POST", "/config", bytes.NewBufferString(`{"wrong_feeds":1a}`)),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "WrongContentType",
			request:        httptest.NewRequest("POST", "/config", bytes.NewBufferString(`{"rss_feeds": ["https://example.com/rss"]}`)),
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "WrongContentType" {
				tc.request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				tc.request.Header.Set("Content-Type", "application/json")
			}
			requestRecorder := httptest.NewRecorder()
			handler := http.HandlerFunc(ConfigHandler)
			handler.ServeHTTP(requestRecorder, tc.request)
			if requestRecorder.Code != tc.expectedStatus {
				t.Errorf("Expected status %v, got: %v", tc.expectedStatus, requestRecorder.Code)
			}
		})
	}
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
		if v.Title != "Test RSS Feed" {
			t.Error("title feed is different than expected")
		}
		if v.Description != "This is a test RSS feed." {
			t.Error("expected different description")
		}
		if len(v.Items) != 2 {
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
		for i, item := range v.Items {
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

func TestRSSHandler(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()
	cfg.RSSFeeds = []string{mockServer.URL}
	req, err := http.NewRequest("GET", "/rss", nil)
	if err != nil {
		t.Error(err)
	}
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(RSSHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusOK {
		t.Errorf("expected %v, got: %v", http.StatusOK, requestRecorder.Code)
	}

	var feeds []feedsJSON
	err = json.NewDecoder(requestRecorder.Body).Decode(&feeds)
	if err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	expectedFeeds := []feedsJSON{
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

	if !reflect.DeepEqual(feeds, expectedFeeds) {
		t.Errorf("Expected feeds: %v, got: %v", expectedFeeds, feeds)
	}
}

func TestProcessFeeds(t *testing.T) {
	feed := &gofeed.Feed{
		Items: []*gofeed.Item{
			{
				Title:       "Test Item 1",
				Description: "Test Description 1",
				Content:     "Test Content 1",
				Link:        "http://example.com/1",
			},
			{
				Title:       "Test Item 2",
				Description: "Test Description 2",
				Content:     "Test Content 2",
				Link:        "http://example.com/2",
			},
		},
	}

	expectedFeeds := []feedsJSON{
		{
			Title:       "Test Item 1",
			Description: "Test Description 1",
			Content:     "Test Content 1",
			Link:        "http://example.com/1",
		},
		{
			Title:       "Test Item 2",
			Description: "Test Description 2",
			Content:     "Test Content 2",
			Link:        "http://example.com/2",
		},
	}

	feeds := processFeeds(context.Background(), feed)

	if !reflect.DeepEqual(feeds, expectedFeeds) {
		t.Errorf("Expected feeds: %v, got: %v", expectedFeeds, feeds)
	}
}

func TestToJSON(t *testing.T) {
	feed := []*gofeed.Feed{
		{
			Title:       "Test Feed",
			Description: "Test Description",
			Items: []*gofeed.Item{
				{
					Title:       "Test Item 1",
					Description: "Test Description 1",
					Content:     "Test Content 1",
					Link:        "http://example.com/1",
				},
				{
					Title:       "Test Item 2",
					Description: "Test Description 2",
					Content:     "Test Content 2",
					Link:        "http://example.com/2",
				},
			},
		},
	}

	expectedJSON := `[{"title":"Test Item 1","description":"Test Description 1","content":"Test Content 1","link":"http://example.com/1"},{"title":"Test Item 2","description":"Test Description 2","content":"Test Content 2","link":"http://example.com/2"}]
`

	var buf bytes.Buffer
	err := toJSON(context.Background(), &buf, feed)
	if err != nil {
		t.Fatalf("toJSON returned an error: %v", err)
	}

	if buf.String() != expectedJSON {
		t.Errorf("Expected JSON: %s, got: %s", expectedJSON, buf.String())
	}
}

func TestRSSHandlerError(t *testing.T) {
	cfg.RSSFeeds = []string{"http://example.com/rss", "http://notarealrssfeed.com/rss"}
	req, err := http.NewRequest("GET", "/rss", nil)
	if err != nil {
		t.Error(err)
	}
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(RSSHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusBadRequest {
		t.Errorf("got: %v, want: %v", requestRecorder.Code, http.StatusBadRequest)
	}
	want := ""
	got := requestRecorder.Body.String()
	if got != want {
		t.Errorf("want: %v, got :%v", want, got)
	}
}
func TestHandleConfigPayload(t *testing.T) {
	// Test case 1: Valid request
	t.Run("ValidRequest", func(t *testing.T) {
		jsonBody := `{"rss_feeds": ["http://example.com/feed1", "http://example.com/feed2"]}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		if err := handleConfigPayload(req); err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
	})

	// Test case 2: Invalid method
	t.Run("InvalidMethod", func(t *testing.T) {
		jsonBody := `{"rss_feeds": ["http://example.com/feed1"]}`
		req := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		if err := handleConfigPayload(req); err == nil {
			t.Fatal("Expected an error for invalid method, but got none")
		}
	})

	// Test case 3: Invalid content type
	t.Run("InvalidContentType", func(t *testing.T) {
		jsonBody := `{"rss_feeds": ["http://example.com/feed1"]}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(jsonBody))
		req.Header.Set("Content-Type", "text/plain")

		if err := handleConfigPayload(req); err == nil {
			t.Fatal("Expected an error for invalid content type, but got none")
		}
	})

	// Test case 4: Malformed JSON
	t.Run("MalformedJSON", func(t *testing.T) {
		jsonBody := `{rss_feeds: ["http://example.com/feed1"]}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		if err := handleConfigPayload(req); err == nil {
			t.Fatal("Expected an error for malformed JSON, but got none")
		}
	})
}

func TestCollectNewLinks(t *testing.T) {
	item1 := &gofeed.Item{Link: "http://example.com/item1"}
	item2 := &gofeed.Item{Link: "http://example.com/item2"}
	item3 := &gofeed.Item{Link: "http://example.com/item3"}
	itemGUID := &gofeed.Item{GUID: "tag:example.com,2024:42", Link: "http://example.com/item4"}
	itemNoLink := &gofeed.Item{GUID: "guid-no-link", Link: ""}

	feeds := []*gofeed.Feed{{Items: []*gofeed.Item{item1, item2}}}

	t.Cleanup(func() { seen = make(map[string]bool) })

	// First call: both items are new — toSend must contain exactly their Links.
	t.Run("FirstCallReturnsAllLinks", func(t *testing.T) {
		seen = make(map[string]bool)
		got := collectNewLinks(context.Background(), feeds)
		want := []string{"http://example.com/item1", "http://example.com/item2"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("want %v, got %v", want, got)
		}
	})

	// Second call with the same feeds: seen is already populated — toSend must be empty.
	t.Run("SecondCallReturnEmpty", func(t *testing.T) {
		got := collectNewLinks(context.Background(), feeds)
		if len(got) != 0 {
			t.Errorf("expected empty toSend on second call, got %v", got)
		}
	})

	// New item added to feed: only the new link appears in toSend.
	t.Run("NewItemOnlyInToSend", func(t *testing.T) {
		feeds2 := []*gofeed.Feed{{Items: []*gofeed.Item{item1, item2, item3}}}
		got := collectNewLinks(context.Background(), feeds2)
		want := []string{"http://example.com/item3"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("want %v, got %v", want, got)
		}
	})

	// Item with GUID: dedup key is GUID, but Link is sent in toSend.
	t.Run("GUIDKeyedItemSendsLink", func(t *testing.T) {
		seen = make(map[string]bool)
		feeds3 := []*gofeed.Feed{{Items: []*gofeed.Item{itemGUID}}}
		got := collectNewLinks(context.Background(), feeds3)
		want := []string{"http://example.com/item4"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("want %v, got %v", want, got)
		}
		if !seen["tag:example.com,2024:42"] {
			t.Error("expected GUID to be recorded in seen, not the Link")
		}
		// Second call: GUID already seen — nothing sent.
		got2 := collectNewLinks(context.Background(), feeds3)
		if len(got2) != 0 {
			t.Errorf("expected empty on second call for GUID item, got %v", got2)
		}
	})

	// Item with GUID but no Link: recorded in seen but never appended to toSend.
	t.Run("GUIDWithNoLinkNotSent", func(t *testing.T) {
		seen = make(map[string]bool)
		feeds4 := []*gofeed.Feed{{Items: []*gofeed.Item{itemNoLink}}}
		got := collectNewLinks(context.Background(), feeds4)
		if len(got) != 0 {
			t.Errorf("expected nothing sent for item with no link, got %v", got)
		}
		if !seen["guid-no-link"] {
			t.Error("expected GUID with no link to still be recorded in seen")
		}
	})

	// Same URL across multiple feeds in a single cycle: must not duplicate in toSend.
	t.Run("DuplicateAcrossFeedsInOneCycle", func(t *testing.T) {
		seen = make(map[string]bool)
		feeds5 := []*gofeed.Feed{
			{Items: []*gofeed.Item{item1}},
			{Items: []*gofeed.Item{item1}},
		}
		got := collectNewLinks(context.Background(), feeds5)
		if len(got) != 1 {
			t.Errorf("expected 1 entry for duplicate across feeds, got %v", got)
		}
	})
}

func TestPollAndNotifySeenDedup(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()

	seen = make(map[string]bool)
	globalFeed = nil
	cfg.RSSFeeds = []string{mockServer.URL}
	if err := os.Unsetenv("NOTIFICATION_ENDPOINT"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		globalFeed = nil
		seen = make(map[string]bool)
	})

	pollAndNotify(time.Now())

	firstSeenCount := len(seen)
	if firstSeenCount == 0 {
		t.Fatal("expected seen to contain items after first poll")
	}

	for _, url := range []string{"http://example.com/item1", "http://example.com/item2"} {
		if !seen[url] {
			t.Errorf("expected seen[%q] to be true after first poll", url)
		}
	}

	pollAndNotify(time.Now())

	if len(seen) != firstSeenCount {
		t.Errorf("expected seen count to stay %d after second poll, got %d", firstSeenCount, len(seen))
	}
}

func TestPollAndNotifyTriggersNotificationForNewItem(t *testing.T) {
	// currentFeed is swapped between polls to simulate a new item appearing.
	currentFeed := mockRSSFeedContent
	rssServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(currentFeed))
	}))
	defer rssServer.Close()

	seen = make(map[string]bool)
	globalFeed = nil
	cfg.RSSFeeds = []string{rssServer.URL}
	setNotificationReceiver("") // suppress notifications on first poll
	t.Cleanup(func() {
		globalFeed = nil
		seen = make(map[string]bool)
		setNotificationReceiver("")
		wsMu.Lock()
		wsConn = nil
		wsMu.Unlock()
	})

	// First poll: items 1 and 2 are marked seen; no notification sent because receiver is empty.
	pollAndNotify(time.Now())
	if !seen["http://example.com/item1"] || !seen["http://example.com/item2"] {
		t.Fatal("expected item1 and item2 to be in seen after first poll")
	}

	// Simulate a new item appearing in the feed.
	currentFeed = mockRSSFeedContentWithNewItem

	// Mock WebSocket server captures the first notification message.
	msgCh := make(chan []byte, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("websocket.Accept: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Logf("conn.Read: %v", err)
			return
		}
		msgCh <- msg
	}))
	defer wsServer.Close()

	// Dial the mock server and store the client connection so sendNotification uses it.
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial mock WS server: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	wsMu.Lock()
	wsConn = conn
	wsMu.Unlock()

	setNotificationReceiver("https://fake-discord-webhook.example.com")

	// Second poll: item3 is new — pollAndNotify should detect it and fire a notification.
	pollAndNotify(time.Now())

	if !seen["http://example.com/item3"] {
		t.Error("expected item3 to be in seen after second poll (new item not detected)")
	}

	select {
	case msg := <-msgCh:
		var notification discordNotification
		if err := json.Unmarshal(msg, &notification); err != nil {
			t.Fatalf("failed to unmarshal notification payload: %v", err)
		}
		found := false
		for _, link := range notification.Content {
			if link == "http://example.com/item3" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected notification to contain item3 link, got: %v", notification.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification to be sent for new item")
	}
}

func TestConfigGetHandler(t *testing.T) {
	cfgMu.Lock()
	cfg.RSSFeeds = []string{"http://example.com/feed1", "http://example.com/feed2"}
	cfgMu.Unlock()
	t.Cleanup(func() {
		cfgMu.Lock()
		cfg.RSSFeeds = nil
		cfgMu.Unlock()
	})

	req := httptest.NewRequest(http.MethodGet, "/config/feeds", nil)
	rr := httptest.NewRecorder()
	ConfigGetHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	var got ConfigStruct
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	want := []string{"http://example.com/feed1", "http://example.com/feed2"}
	if !reflect.DeepEqual(got.RSSFeeds, want) {
		t.Errorf("expected feeds %v, got %v", want, got.RSSFeeds)
	}
}

func TestWSConnected(t *testing.T) {
	wsMu.Lock()
	wsConn = nil
	wsMu.Unlock()
	t.Cleanup(func() {
		wsMu.Lock()
		wsConn = nil
		wsMu.Unlock()
	})

	if WSConnected() {
		t.Error("expected WSConnected false when wsConn is nil")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial WS server: %v", err)
	}
	defer func() { _ = clientConn.Close(websocket.StatusNormalClosure, "") }()

	wsMu.Lock()
	wsConn = clientConn
	wsMu.Unlock()

	if !WSConnected() {
		t.Error("expected WSConnected true when wsConn is set")
	}
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

func TestIsRegistered(t *testing.T) {
	t.Run("ReturnsTrueFor200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		if !isRegistered(context.Background(), srv.URL, "poller") {
			t.Error("expected true for 200 response")
		}
	})
	t.Run("ReturnsFalseForNon200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		if isRegistered(context.Background(), srv.URL, "poller") {
			t.Error("expected false for 404 response")
		}
	})
	t.Run("ReturnsFalseOnConnectionError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close() // immediately close to force a connection refused error
		if isRegistered(context.Background(), srv.URL, "poller") {
			t.Error("expected false on connection error")
		}
	})
}

func TestReadyHandler(t *testing.T) {
	t.Run("LOCATORURLNotSet", func(t *testing.T) {
		t.Setenv("LOCATOR_URL", "")
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		ReadyHandler(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
	t.Run("PollerNotRegistered", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		t.Setenv("LOCATOR_URL", srv.URL)
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		ReadyHandler(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})
	t.Run("NotifyNotRegistered", func(t *testing.T) {
		// First call (poller check) → 200; second call (notify check) → 404.
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()
		t.Setenv("LOCATOR_URL", srv.URL)
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		ReadyHandler(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})
	t.Run("AllRegistered", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		t.Setenv("LOCATOR_URL", srv.URL)
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		ReadyHandler(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

func TestToJSONEdgeCases(t *testing.T) {
	t.Run("NilFeeds", func(t *testing.T) {
		var buf bytes.Buffer
		if err := toJSON(context.Background(), &buf, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s := strings.TrimSpace(buf.String()); s != "null" {
			t.Errorf("expected null, got %q", s)
		}
	})
	t.Run("EmptyFeeds", func(t *testing.T) {
		var buf bytes.Buffer
		if err := toJSON(context.Background(), &buf, []*gofeed.Feed{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s := strings.TrimSpace(buf.String()); s != "null" {
			t.Errorf("expected null for empty feeds slice, got %q", s)
		}
	})
	t.Run("FeedWithNoItems", func(t *testing.T) {
		var buf bytes.Buffer
		if err := toJSON(context.Background(), &buf, []*gofeed.Feed{{Title: "Empty Feed"}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s := strings.TrimSpace(buf.String()); s != "null" {
			t.Errorf("expected null for feed with no items, got %q", s)
		}
	})
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

func TestPollAndNotifyParseFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	seen = make(map[string]bool)
	globalFeed = nil
	cfg.RSSFeeds = []string{srv.URL}
	t.Cleanup(func() {
		seen = make(map[string]bool)
		globalFeed = nil
	})

	pollAndNotify(time.Now())

	feedMutex.RLock()
	gf := globalFeed
	feedMutex.RUnlock()
	if gf != nil {
		t.Errorf("expected globalFeed to remain nil after parse failure, got %v", gf)
	}

	seenMu.Lock()
	count := len(seen)
	seenMu.Unlock()
	if count != 0 {
		t.Errorf("expected seen to remain empty after parse failure, got %d entries", count)
	}
}

func TestRSSHandlerCORSHeaders(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()

	feedMutex.Lock()
	globalFeed = nil
	feedMutex.Unlock()
	cfg.RSSFeeds = []string{mockServer.URL}
	t.Cleanup(func() {
		feedMutex.Lock()
		globalFeed = nil
		feedMutex.Unlock()
	})

	req := httptest.NewRequest(http.MethodGet, "/rss", nil)
	rr := httptest.NewRecorder()
	RSSHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	headers := []struct{ name, want string }{
		{"Access-Control-Allow-Origin", "http://localhost:4321"},
		{"Access-Control-Allow-Methods", "GET, POST, OPTIONS"},
		{"Access-Control-Allow-Headers", "Content-Type"},
		{"Content-Type", "application/json"},
	}
	for _, h := range headers {
		if got := rr.Header().Get(h.name); got != h.want {
			t.Errorf("header %q: want %q, got %q", h.name, h.want, got)
		}
	}
}

func TestSendNotification(t *testing.T) {
	// Test Case 1: No content in the notification — drops gracefully
	t.Run("NoContent", func(t *testing.T) {
		d := &discordNotification{Content: nil}
		err := d.sendNotification(context.Background())
		if err != nil {
			t.Fatalf("Expected no error for nil content, but got: %v", err)
		}
	})

	// Test Case 2: No WebSocket connection — drops gracefully (logs and returns nil)
	t.Run("NoWSConnection", func(t *testing.T) {
		wsMu.Lock()
		wsConn = nil
		wsMu.Unlock()

		d := &discordNotification{
			Content:    []string{"http://example.com/new-article"},
			WebHookURL: "http://example.com/webhook",
		}
		err := d.sendNotification(context.Background())
		if err != nil {
			t.Fatalf("Expected nil when WS is not connected, but got: %v", err)
		}
	})
}
