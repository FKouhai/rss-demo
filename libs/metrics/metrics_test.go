package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCounter_Increments(t *testing.T) {
	r := Init("mock")
	cv := r.Counter("ops_total", "test_counter", "outcome")
	cv.WithLabelValues("ok").Inc()
	if got := testutil.ToFloat64(cv.WithLabelValues("ok")); got != 1 {
		t.Fatalf("counter %v, want 1", got)
	}
}

func TestRegistries_Isolated(t *testing.T) {
	a := Init("mock")
	b := Init("mock")
	ca := a.Counter("ops_total", "test_counter", "outcome")
	cb := b.Counter("ops_total", "test_counter", "outcome")

	ca.WithLabelValues("ok").Inc()
	if got := testutil.ToFloat64(cb.WithLabelValues("ok")); got != 0 {
		t.Fatalf("registries share state: b = %v, want 0", got)
	}
}

func TestHandler_Serve(t *testing.T) {
	r := Init("mock")
	cv := r.Counter("ops_total", "test_counter", "outcome")
	h := r.Histogram("historic", "test_histogram", nil, "outcome")
	gv := r.Gauge("pending", "test gauge", "outcome")

	cv.WithLabelValues("ok").Inc()
	h.WithLabelValues("ok").Observe(0.1)
	gv.WithLabelValues("ok").Set(3)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrcs", nil)

	r.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, want := range []string{"rss_mock_ops_total", "rss_mock_pending", "rss_mock_historic"} {
		if !strings.Contains(body, want) {
			t.Fatalf("scrape missing %q", want)
		}
	}

}
