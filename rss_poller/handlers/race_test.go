package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
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

	for i := 0; i < 5; i++ {
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

func TestCfgRSSFeedsRaceDuringPoll(t *testing.T) {
	mockServer := startMockRSSFeedServer()
	defer mockServer.Close()

	cfg.RSSFeeds = []string{mockServer.URL}
	globalFeed = nil

	startPolling()
	defer func() {
		if cancelFn != nil {
			cancelFn()
		}
	}()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			setRSSFeeds([]string{mockServer.URL, mockServer.URL + "?v=" + string(rune(idx))})
			time.Sleep(5 * time.Millisecond)
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	wg.Wait()
}
