package cmd

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// newTestApp starts an in-process notify on an ephemeral port. It skips
// locator + tracer so the test stays offline and hermetic.
// Callers must call app.Shutdown via t.Cleanup (wired here).
func newTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("LOCATOR_URL", "")
	app, err := Run(context.Background(), Config{
		HTTPAddr:    "127.0.0.1:0",
		SkipLocator: true,
		SkipTracer:  true,
		NATSURL:     "nats://127.0.0.1:1",
	})

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return app
}

func waitForReady(t *testing.T, app *App) {
	t.Helper()
	select {
	case <-app.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("Ready channel did not fire within 2s")
	}
}

func TestApp_Run_ReadyFires(t *testing.T) {
	app := newTestApp(t)
	waitForReady(t, app)
	// Ready is a closed channel — receiving is non-blocking afterwards.
	select {
	case <-app.Ready():
	default:
		t.Fatal("Ready should remain closed after firing")
	}
}

func TestApp_Run_HealthzResponds(t *testing.T) {
	app := newTestApp(t)
	waitForReady(t, app)

	addr := "http://" + app.Addr().String()
	// Use the real client against the bound listener, not httptest recorder,
	// because the server runs in a goroutine behind a real socket.
	resp, err := http.Get(addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("/healthz returned empty body")
	}
}
