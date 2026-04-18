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
