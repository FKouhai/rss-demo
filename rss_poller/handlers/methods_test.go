package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

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
	err := toJSON(&buf, feed)
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

func TestDiffie(t *testing.T) {
	// Sample gofeed.Item structs for testing
	feedItem1 := &gofeed.Item{Link: "http://example.com/post1"}
	feedItem2 := &gofeed.Item{Link: "http://example.com/post2"}
	feedItem3 := &gofeed.Item{Link: "http://example.com/post3"}

	// Create a base feed with two items and an extra feed with a new item
	baseFeeds := []*gofeed.Feed{
		{
			Items: []*gofeed.Item{feedItem1, feedItem2},
		},
	}
	extraFeeds := []*gofeed.Feed{
		{
			Items: []*gofeed.Item{feedItem1, feedItem2, feedItem3},
		},
	}

	// Test case 1: New elements are found
	t.Run("NewElementsFound", func(t *testing.T) {
		diff := diffie(context.Background(), baseFeeds, extraFeeds)

		if len(diff) != 1 {
			t.Fatalf("Expected 1 new element, but got %d", len(diff))
		}
		if diff[0] != feedItem3.Link {
			t.Fatalf("Expected the new link to be %s, but got %s", feedItem3.Link, diff[0])
		}
	})

	// Test case 2: No new elements are found
	t.Run("NoNewElements", func(t *testing.T) {
		baseFeeds := []*gofeed.Feed{
			{
				Items: []*gofeed.Item{feedItem1, feedItem2},
			},
		}
		extraFeeds := []*gofeed.Feed{
			{
				Items: []*gofeed.Item{feedItem1, feedItem2},
			},
		}

		diff := diffie(context.Background(), baseFeeds, extraFeeds)

		if len(diff) != 0 {
			t.Fatalf("Expected 0 new elements, but got %d", len(diff))
		}
	})

	// Test case 3: Base feed is empty
	t.Run("EmptyBaseFeed", func(t *testing.T) {
		var baseFeeds []*gofeed.Feed
		extraFeeds := []*gofeed.Feed{
			{
				Items: []*gofeed.Item{feedItem1, feedItem2},
			},
		}

		diff := diffie(context.Background(), baseFeeds, extraFeeds)

		if len(diff) != 2 {
			t.Fatalf("Expected 2 new elements, but got %d", len(diff))
		}
	})

}

func TestSendNotification(t *testing.T) {
	// Test Case 1: Successful notification with valid content
	t.Run("ValidContent", func(t *testing.T) {
		// Set up a mock HTTP server. This server will handle the POST request
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Validate the request sent by sendNotificatio
			if r.Method != http.MethodPost {
				t.Errorf("Expected POST method, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			}

			// Read and validate the request body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("Failed to read request body: %v", err)
			}
			var receivedData discordNotification
			if err := json.Unmarshal(body, &receivedData); err != nil {
				t.Fatalf("Failed to unmarshal request body: %v", err)
			}

			expectedContent := []string{"http://example.com/new-article"}
			if len(receivedData.Content) != len(expectedContent) || receivedData.Content[0] != expectedContent[0] {
				t.Errorf("Received content mismatch. Got %v, expected %v", receivedData.Content, expectedContent)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Success"))
		}))
		defer server.Close() // Make sure the server is closed after the test

		// Create a notification object with valid data
		d := &discordNotification{
			Content:    []string{"http://example.com/new-article"},
			WebHookURL: "http://example.com/webhook",
		}

		// Call the function under test, using the mock server's URL
		status, err := d.sendNotification(server.URL)

		// Assert the results
		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		if status != http.StatusOK {
			t.Errorf("Expected status %d, but got %d", http.StatusOK, status)
		}
	})

	// Test Case 2: No content in the notification
	t.Run("NoContent", func(t *testing.T) {
		// This test should not make an HTTP request, so we don't need a mock server
		d := &discordNotification{Content: nil}
		status, err := d.sendNotification("http://should-not-be-called.com")

		if err != nil {
			t.Fatalf("Expected no error, but got: %v", err)
		}
		if status != http.StatusNoContent {
			t.Errorf("Expected status %d, but got %d", http.StatusNoContent, status)
		}
	})

	// Test Case 3: Error from the HTTP client
	t.Run("HTTPClientError", func(t *testing.T) {
		// Use an invalid URL to simulate a network error
		d := &discordNotification{
			Content:    []string{"http://example.com/new-article"},
			WebHookURL: "http://example.com/webhook",
		}
		status, err := d.sendNotification("://invalid-url")

		// We expect an error and a zero status code
		if err == nil {
			t.Fatal("Expected an error, but got none")
		}
		if status != 0 {
			t.Errorf("Expected status 0, but got %d", status)
		}
	})

}
