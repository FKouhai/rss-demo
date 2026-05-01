package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mmcdole/gofeed"
)

func TestConcurrentConfigUpdatesDuringPolling(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()

	cfg.RSSFeeds = []string{mockServer.URL}

	startPolling()
	defer func() {
		if cancelFn != nil {
			cancelFn()
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Now hammer concurrent config updates while the ticker is running.
	// Each POST /config calls handleConfigPayload -> startPolling,
	// which writes to global ticker/cancelFn (line 413-439).
	//
	// This creates a race:
	// - Goroutine 1 (ticker): reads cfg.RSSFeeds in pollAndNotify
	// - Goroutines 2-N (HTTP handlers): write cfg + modify ticker/cancelFn in startPolling
	//
	// The race detector will catch concurrent reads/writes to ticker, cancelFn, and cfg.

	var wg sync.WaitGroup

	for i := range 5 {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()

			payload := map[string][]string{
				"rss_feeds": {mockServer.URL, mockServer.URL + "?v=" + string(rune(iteration))},
			}
			body, _ := json.Marshal(payload)

			req, err := http.NewRequest("POST", "/config", bytes.NewBuffer(body))
			if err != nil {
				t.Logf("failed to create request: %v", err)
				return
			}
			req.Header.Set("Content-Type", "application/json")

			err = handleConfigPayload(req)
			if err != nil {
				// Config errors are OK; we're testing for races, not success
				t.Logf("config update %d returned error (expected): %v", iteration, err)
			}
		}(i)
	}

	// Let the concurrent updates run for a bit while the ticker is active
	time.Sleep(50 * time.Millisecond)

	wg.Wait()

	// If -race flag is used, any concurrent access to ticker/cancelFn/cfg
	// without synchronization will be reported as a data race.
}

// TestRSSHandlerConcurrentWithPollWrite checks that concurrent RSSHandler requests
// do not race with pollAndNotify updating globalFeed under feedMutex.
func TestRSSHandlerConcurrentWithPollWrite(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()

	feedMutex.Lock()
	globalFeed = nil
	feedMutex.Unlock()
	t.Cleanup(func() {
		feedMutex.Lock()
		globalFeed = nil
		feedMutex.Unlock()
	})

	// Writer goroutines simulate pollAndNotify updating globalFeed.
	const writers = 3
	const readers = 8
	var wg sync.WaitGroup

	for range writers {
		wg.Go(func() {
			for range 5 {
				feeds := []*gofeed.Feed{{
					Title: "Race Feed",
					Items: []*gofeed.Item{{Title: "item", Link: "http://example.com/x"}},
				}}
				feedMutex.Lock()
				globalFeed = feeds
				feedMutex.Unlock()
				time.Sleep(2 * time.Millisecond)
			}
		})
	}

	// Reader goroutines simulate concurrent HTTP requests to /rss.
	for range readers {
		wg.Go(func() {
			for range 5 {
				req := httptest.NewRequest(http.MethodGet, "/rss", nil)
				rr := httptest.NewRecorder()
				RSSHandler(rr, req)
				time.Sleep(1 * time.Millisecond)
			}
		})
	}

	wg.Wait()
}

// TestConcurrentSendNotification fires multiple sendNotification goroutines at the
// same time with a live WebSocket connection, exercising the wsConn read + write path
// under concurrent load.
func TestConcurrentSendNotification(t *testing.T) {
	// Spin up a WebSocket server that drains all messages without closing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
		}
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
	t.Cleanup(func() {
		wsMu.Lock()
		wsConn = nil
		wsMu.Unlock()
	})

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			d := &discordNotification{
				Content:    []string{"http://example.com/race-item"},
				WebHookURL: "http://example.com/webhook",
			}
			// Errors from concurrent writes are acceptable; we are checking for
			// data races on wsConn, not for successful delivery.
			_ = d.sendNotification(context.Background())
		})
	}
	wg.Wait()
}

// TestConcurrentSetNotificationReceiverAndPoll verifies that concurrent writes to
// notificationReceiver (via setNotificationReceiver) do not race with pollAndNotify
// reading it via notifyMu.
func TestConcurrentSetNotificationReceiverAndPoll(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()

	seen = make(map[string]bool)
	globalFeed = nil
	setNotificationReceiver("")
	t.Cleanup(func() {
		seen = make(map[string]bool)
		globalFeed = nil
		setNotificationReceiver("")
	})

	var wg sync.WaitGroup

	// Goroutines repeatedly flip the notification receiver while polls run.
	for i := range 5 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for j := range 6 {
				if j%2 == 0 {
					setNotificationReceiver("https://fake-webhook.example.com")
				} else {
					setNotificationReceiver("")
				}
				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	// Goroutines invoke poll cycles concurrently with the receiver mutations.
	for range 4 {
		wg.Go(func() {
			for range 3 {
				pollAndNotify(time.Now())
				time.Sleep(3 * time.Millisecond)
			}
		})
	}

	wg.Wait()
}

// TestConcurrentConfigGetAndSet verifies that ConfigGetHandler reads under cfgMu do
// not race with handleConfigPayload writes.
func TestConcurrentConfigGetAndSet(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()

	t.Cleanup(func() {
		cfgMu.Lock()
		cfg.RSSFeeds = nil
		cfgMu.Unlock()
	})

	var wg sync.WaitGroup

	// Writers call handleConfigPayload concurrently.
	for i := range 5 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for range 4 {
				payload := map[string][]string{"rss_feeds": {mockServer.URL}}
				body, _ := json.Marshal(payload)
				req := httptest.NewRequest(http.MethodPost, "/config", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				_ = handleConfigPayload(req)
				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	// Readers call ConfigGetHandler concurrently.
	for range 5 {
		wg.Go(func() {
			for range 4 {
				req := httptest.NewRequest(http.MethodGet, "/config/feeds", nil)
				rr := httptest.NewRecorder()
				ConfigGetHandler(rr, req)
				time.Sleep(1 * time.Millisecond)
			}
		})
	}

	wg.Wait()
}

// TestRapidPollAndNotifyCycles fires many overlapping pollAndNotify calls to verify
// that the seen map and globalFeed do not exhibit data races under rapid-fire polling.
func TestRapidPollAndNotifyCycles(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()

	seen = make(map[string]bool)
	globalFeed = nil
	setNotificationReceiver("")
	t.Cleanup(func() {
		seen = make(map[string]bool)
		globalFeed = nil
		setNotificationReceiver("")
	})

	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			pollAndNotify(time.Now())
		})
	}
	wg.Wait()
}

// TestWSConnectedConcurrentWithSendNotification verifies that WSConnected() and
// sendNotification() do not race on wsConn when called simultaneously.
func TestWSConnectedConcurrentWithSendNotification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
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
	t.Cleanup(func() {
		wsMu.Lock()
		wsConn = nil
		wsMu.Unlock()
	})

	var wg sync.WaitGroup

	// Goroutines call WSConnected() continuously.
	for range 5 {
		wg.Go(func() {
			for range 10 {
				_ = WSConnected()
				time.Sleep(time.Millisecond)
			}
		})
	}

	// Goroutines call sendNotification() concurrently.
	for range 5 {
		wg.Go(func() {
			d := &discordNotification{
				Content:    []string{"http://example.com/item"},
				WebHookURL: "http://example.com/webhook",
			}
			for range 4 {
				_ = d.sendNotification(context.Background())
				time.Sleep(2 * time.Millisecond)
			}
		})
	}

	wg.Wait()
}
