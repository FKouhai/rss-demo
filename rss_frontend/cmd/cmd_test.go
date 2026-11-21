package cmd

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// newTestApp starts an in-process frontend on an ephemeral port backed by
// no services at all: the aggregator URLs point at closed ports so every
// backend call fails fast, exercising the degraded-mode paths. Tests stay
// offline and hermetic (no OIDC, no NATS, no DB). Callers must call
// app.Shutdown.
func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := Run(context.Background(), Config{
		HTTPAddr:      "127.0.0.1:0",
		SkipTracer:    true,
		AggregatorURL: "http://127.0.0.1:1",
		PollerURL:     "http://127.0.0.1:1",
		NotifyURL:     "http://127.0.0.1:1",
		OutboxURL:     "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	<-app.Ready()
	return app
}

func get(t *testing.T, app *App, path string) (int, string) {
	t.Helper()
	resp, err := http.Get("http://" + app.Addr().String() + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 1<<20)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n])
}

func TestApp_Run_ServesFrontDegraded(t *testing.T) {
	app := newTestApp(t)
	code, _ := get(t, app, "/healthz")
	if code != http.StatusOK {
		t.Errorf("healthz = %d", code)
	}
	code, body := get(t, app, "/")
	if code != http.StatusOK {
		t.Errorf("front = %d, want degraded 200", code)
	}
	if !strings.Contains(body, "Aggregator unreachable") {
		t.Errorf("missing degraded notice")
	}
	if app.OIDC() != nil {
		t.Error("expected open mode (nil OIDC)")
	}
}
