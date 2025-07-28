package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

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

func newTestRequest(method, url string, body []byte) *http.Request {
	req, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}
	return req
}

func TestMain(m *testing.M) {
	otel.SetTracerProvider(noop.NewTracerProvider())
	exitCode := m.Run()
	os.Exit(exitCode)
}
func TestRoot(t *testing.T) {
	req := newTestRequest("GET", "/", nil)
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(RootHandler)
	handler.ServeHTTP(requestRecorder, req)
	status := requestRecorder.Code
	if status != http.StatusOK {
		t.Errorf("Handler returned a different code from 200: %v", status)
	}
	want := "testing 1 2"
	got := requestRecorder.Body.String()
	if got != want {
		t.Errorf("The server returned an unexpected body: got %v, want: %v", got, want)
	}
}

func TestConfig(t *testing.T) {
	payload := []byte(`{"rss_feeds": "https://example.com/rss"}`)
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
	want := "https://example.com/rss"
	got := requestRecorder.Body.String()
	if got != want {
		t.Errorf("The server returned an unexpected body: got %v, want: %v", got, want)
	}
}

func TestConfigNotGet(t *testing.T) {
	req, err := http.NewRequest("GET", "/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(ConfigHandler)
	handler.ServeHTTP(requestRecorder, req)
	want := http.StatusBadRequest
	got := requestRecorder.Code
	if got != want {
		t.Errorf("Want: %v, Got: %v", want, got)
	}
}

func TestConfigJSONDecodeError(t *testing.T) {
	payload := []byte(`{"wrong_feeds":1a}`)
	req, err := http.NewRequest("POST", "/config", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(ConfigHandler)
	handler.ServeHTTP(requestRecorder, req)
	want := http.StatusBadRequest
	got := requestRecorder.Code
	if got != want {
		t.Errorf("Want: %v, Got: %v", want, got)
	}
}

func TestConfigWrongContentType(t *testing.T) {
	payload := []byte(`{"rss_feeds": "https://example.com/rss"}`)
	req, err := http.NewRequest("POST", "/config", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(ConfigHandler)
	handler.ServeHTTP(requestRecorder, req)
	want := http.StatusBadRequest
	got := requestRecorder.Code
	if got != want {
		t.Errorf("Want: %v, Got: %v", want, got)
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
		w.Write([]byte(mockRSSFeedContent))
	}))
}

func TestParseRSS(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()
	feed, err := ParseRSS(context.TODO(), mockServer.URL)
	if err != nil {
		t.Error(err)
	}
	if feed.Title != "Test RSS Feed" {
		t.Error("title feed is different than expected")
	}
	if feed.Description != "This is a test RSS feed." {
		t.Error("expected different description")
	}
	if len(feed.Items) != 2 {
		t.Error("Expected 2 ites in the feed")
	}
}

func TestRSSHandler(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()
	cfg.RSSFeeds = mockServer.URL
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
	want := "Test Item 1\nDescription for Test Item 1\nContent for Test Item 1Test Item 2\nDescription for Test Item 2\nContent for Test Item 2"
	got := requestRecorder.Body.String()
	if got != want {
		t.Errorf("got: %v , want: %v", got, want)
	}
}

func TestRSSHandlerError(t *testing.T) {
	cfg.RSSFeeds = "http://example.comm/rss"
	req, err := http.NewRequest("GET", "/rss", nil)
	if err != nil {
		t.Error(err)
	}
	requestRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(RSSHandler)
	handler.ServeHTTP(requestRecorder, req)
	if requestRecorder.Code != http.StatusInternalServerError {
		t.Errorf("got: %v, want: %v", requestRecorder.Code, http.StatusInternalServerError)
	}
	want := ""
	got := requestRecorder.Body.String()
	if got != want {
		t.Errorf("want: %v, got :%v", want, got)
	}
}
