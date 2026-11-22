package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FKouhai/rss-config/store"
	"github.com/FKouhai/rss-demo/libs/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	_ "modernc.org/sqlite"
)

func newTestHandlers(t *testing.T) (*metrics.Registry, *prometheus.CounterVec, *prometheus.GaugeVec) {
	t.Helper()
	s, err := store.NewConfig(store.Config{
		Driver: "sqlite",
		DSN:    filepath.Join(t.TempDir(), "config_metrics_test.db"),
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	SetStore(s)
	SetAuth(false, nil)
	reg := metrics.Init("config-test")
	upsertTotal := reg.Counter("upsert_total", "test upserts", "outcome")
	feedsCount := reg.Gauge("feeds_count", "test feeds")
	SetMetrics(&Metrics{UpsertTotal: upsertTotal, FeedsCount: feedsCount})
	t.Cleanup(func() { SetMetrics(nil) })
	return reg, upsertTotal, feedsCount
}

func TestMetrics_UpsertAndGauge(t *testing.T) {
	_, upsertTotal, feedsCount := newTestHandlers(t)

	// Valid upsert counts stored.
	req := httptest.NewRequest(http.MethodPost, "/feeds", strings.NewReader(`{"url":"https://example.com/a.rss"}`))
	rr := httptest.NewRecorder()
	FeedsHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /feeds = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if got := testutil.ToFloat64(upsertTotal.WithLabelValues("stored")); got != 1 {
		t.Errorf("upsert_total{stored} = %v, want 1", got)
	}

	// Invalid URL fails fast, before the store: no success count.
	req = httptest.NewRequest(http.MethodPost, "/feeds", strings.NewReader(`{"url":"nope"}`))
	rr = httptest.NewRecorder()
	FeedsHandler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("POST bad url = %d, want 400", rr.Code)
	}
	if got := testutil.ToFloat64(upsertTotal.WithLabelValues("stored")); got != 1 {
		t.Errorf("upsert_total{stored} = %v, want still 1", got)
	}

	// Snapshot read refreshes the gauge.
	req = httptest.NewRequest(http.MethodGet, "/config", nil)
	rr = httptest.NewRecorder()
	ConfigHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /config = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if got := testutil.ToFloat64(feedsCount.WithLabelValues()); got != 1 {
		t.Errorf("feeds_count = %v, want 1", got)
	}
}
