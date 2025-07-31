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
	payload := []byte(`{"rss_feeds": ["https://example.com/rss"]}`)
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
	want := `[{"title":"Test Item 1","description":"Description for Test Item 1","content":"Content for Test Item 1","link":"http://example.com/item1"},{"title":"Test Item 2","description":"Description for Test Item 2","content":"Content for Test Item 2","link":"http://example.com/item2"}]`
	got := requestRecorder.Body.String()
	if got != want {
		t.Errorf("got: %v , want: %v", got, want)
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
