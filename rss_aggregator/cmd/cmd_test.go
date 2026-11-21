package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/FKouhai/rss-aggregator/store"
	db "github.com/FKouhai/rss-demo/libs/db"
	_ "modernc.org/sqlite"
)

// newTestApp starts an in-process aggregator on an ephemeral port backed
// by a temp-file sqlite DB. It skips the tracer so the test stays offline
// and hermetic. Callers must call app.Shutdown.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "aggregator_cmd_test.db")
	app, err := Run(context.Background(), Config{
		HTTPAddr:   "127.0.0.1:0",
		SkipTracer: true,
		DBDriver:   db.DriverSQLite,
		DBDSN:      dsn,
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

func TestApp_Run_StoreIsInitialized(t *testing.T) {
	app := newTestApp(t)
	waitForReady(t, app)

	if app.Store() == nil {
		t.Fatal("Store() is nil; Run should have initialized the store")
	}
	ctx := context.Background()
	id, err := app.Store().Save(ctx, store.Item{
		ID: "x1", FeedURL: "f", Title: "t", Link: "l", PublishedAt: "p",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id != "x1" {
		t.Errorf("Save returned id %q, want x1", id)
	}
}

func TestApp_Run_NATSNilWhenUnreachable(t *testing.T) {
	// Point NATS at a port nothing listens on; Run must not fail — it
	// just logs and continues with a nil connection (tracer also
	// skipped, so this is fully hermetic).
	app, err := Run(context.Background(), Config{
		HTTPAddr:   "127.0.0.1:0",
		SkipTracer: true,
		NATSURL:    "nats://127.0.0.1:1", // unroutable
		DBDriver:   db.DriverSQLite,
		DBDSN:      filepath.Join(t.TempDir(), "aggregator_nats_test.db"),
	})
	if err != nil {
		t.Fatalf("Run with unreachable NATS: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})
	waitForReady(t, app)

	if app.NATS() != nil {
		t.Error("NATS() should be nil when the connection failed")
	}
	// Store + HTTP still work without NATS.
	if app.Store() == nil {
		t.Error("Store() should be non-nil even without NATS")
	}
}

func TestApp_Run_Ready503WhenNATSDown(t *testing.T) {
	// Same unroutable broker as the nil test: boot degrades and the
	// readiness gate must hold at 503 instead of reporting ready.
	app, err := Run(context.Background(), Config{
		HTTPAddr:   "127.0.0.1:0",
		SkipTracer: true,
		NATSURL:    "nats://127.0.0.1:1",
		DBDriver:   db.DriverSQLite,
		DBDSN:      filepath.Join(t.TempDir(), "aggregator_ready_test.db"),
	})
	if err != nil {
		t.Fatalf("Run with unreachable NATS: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})
	waitForReady(t, app)

	resp, err := http.Get("http://" + app.Addr().String() + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/ready status = %d, want 503 while NATS is down", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /ready body: %v", err)
	}
	if body["status"] != "not ready" {
		t.Errorf("/ready body = %v, want not-ready status", body)
	}
}

func TestAggregator_Start_NilNATS(t *testing.T) {
	ag := NewAggregator(nil, nil)
	if err := ag.Start(context.Background()); err == nil {
		t.Error("expected error for nil NATS client, got nil")
	}
}

func TestAggregator_Stop_Empty(t *testing.T) {
	ag := &Aggregator{}
	ag.Stop() // must not panic with no subscriptions
	if len(ag.consumeCtxs) != 0 {
		t.Errorf("consumeCtxs = %v, want empty", ag.consumeCtxs)
	}
}
