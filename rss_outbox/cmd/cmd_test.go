package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-demo/libs/service"
	"github.com/FKouhai/rss-outbox/store"
	_ "modernc.org/sqlite"
)

// newTestApp starts an in-process outbox on an ephemeral port backed by a
// temp-file sqlite DB. It skips locator + tracer so the test stays
// offline and hermetic. Callers must call app.Shutdown.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "outbox_cmd_test.db")
	app, err := Run(context.Background(), Config{
		BaseConfig: service.BaseConfig{
			HTTPAddr:    "127.0.0.1:0",
			SkipLocator: true,
			SkipTracer:  true,
		},
		DBDriver: store.DriverSQLite,
		DBDSN:    dsn,
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
	id, err := app.Store().Save(ctx, store.Entry{
		ID: "x1", FeedURL: "f", Title: "t", Link: "l", WebhookURL: "w",
		Status: "pending", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id != "x1" {
		t.Errorf("Save returned id %q, want x1", id)
	}
	st, err := app.Store().Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Pending != 1 {
		t.Errorf("Status.Pending = %d, want 1", st.Pending)
	}
}

func TestApp_Run_NATSNilWhenUnreachable(t *testing.T) {
	// Point NATS at a port nothing listens on; Run must not fail — it
	// just logs and continues with a nil connection (locator + tracer
	// also skipped, so this is fully hermetic).
	app, err := Run(context.Background(), Config{
		BaseConfig: service.BaseConfig{
			HTTPAddr:    "127.0.0.1:0",
			SkipLocator: true,
			SkipTracer:  true,
		},
		NATSURL:  "nats://127.0.0.1:1", // unroutable
		DBDriver: store.DriverSQLite,
		DBDSN:    filepath.Join(t.TempDir(), "outbox_nats_test.db"),
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

// newTestOutbox builds an Outbox over a temp-file sqlite store with no NATS
// connection. Enough for handler paths that never publish (poison input,
// ack application); dispatch paths need a broker (Phase 9).
func newTestOutbox(t *testing.T) *Outbox {
	t.Helper()
	s, err := store.NewConfig(store.Config{
		Driver: store.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "outbox_handler_test.db"),
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewOutbox(nil, s)
}

func TestOutbox_Start_NilNATS(t *testing.T) {
	ob := NewOutbox(nil, nil)
	if err := ob.Start(context.Background()); err == nil {
		t.Error("expected error for nil NATS client, got nil")
	}
}

func TestOutbox_Stop_Empty(t *testing.T) {
	ob := &Outbox{}
	ob.Stop() // must not panic with no subscriptions
	if len(ob.consumeCtxs) != 0 {
		t.Errorf("consumeCtxs = %v, want empty", ob.consumeCtxs)
	}
}

func TestHandleFeed_PoisonTerm(t *testing.T) {
	ob := newTestOutbox(t)
	decision, err := ob.handleFeed(context.Background(), pubsub.Message{Data: []byte("not json")})
	if err == nil {
		t.Error("expected unmarshal error, got nil")
	}
	if decision != pubsub.AckDecisionTerm {
		t.Errorf("decision = %v, want Term (poison must never redeliver)", decision)
	}
	st, err := ob.store.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Pending != 0 {
		t.Errorf("Pending = %d, want 0 (nothing persisted)", st.Pending)
	}
}

func TestHandleAck_PoisonTerm(t *testing.T) {
	ob := newTestOutbox(t)
	decision, err := ob.handleAck(context.Background(), pubsub.Message{Data: []byte("not json")})
	if err == nil {
		t.Error("expected unmarshal error, got nil")
	}
	if decision != pubsub.AckDecisionTerm {
		t.Errorf("decision = %v, want Term", decision)
	}
}

func TestHandleAck_EmptyIDTerm(t *testing.T) {
	ob := newTestOutbox(t)
	raw, _ := json.Marshal(pubsub.AckReply{Status: pubsub.StatusDelivered})
	decision, err := ob.handleAck(context.Background(), pubsub.Message{Data: raw})
	if err == nil {
		t.Error("expected error for missing outbox_id, got nil")
	}
	if decision != pubsub.AckDecisionTerm {
		t.Errorf("decision = %v, want Term (nothing to retry)", decision)
	}
}

func TestHandleAck_Delivered(t *testing.T) {
	ob := newTestOutbox(t)
	ctx := context.Background()
	if _, err := ob.store.Save(ctx, store.Entry{ID: "x1", Status: "pending"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := json.Marshal(pubsub.AckReply{TenantID: "default", OutboxID: "x1", Status: pubsub.StatusDelivered})
	decision, err := ob.handleAck(ctx, pubsub.Message{Data: raw})
	if err != nil {
		t.Fatalf("handleAck: %v", err)
	}
	if decision != pubsub.AckDecisionAck {
		t.Errorf("decision = %v, want Ack", decision)
	}
	st, err := ob.store.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Delivered != 1 || st.Pending != 0 {
		t.Errorf("Status = %+v, want 1 delivered / 0 pending", st)
	}
}

func TestHandleAck_Failed(t *testing.T) {
	ob := newTestOutbox(t)
	ctx := context.Background()
	if _, err := ob.store.Save(ctx, store.Entry{ID: "x2", Status: "pending"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := json.Marshal(pubsub.AckReply{TenantID: "default", OutboxID: "x2", Status: pubsub.StatusFailed, Error: "webhook 500"})
	decision, err := ob.handleAck(ctx, pubsub.Message{Data: raw})
	if err != nil {
		t.Fatalf("handleAck: %v", err)
	}
	if decision != pubsub.AckDecisionAck {
		t.Errorf("decision = %v, want Ack", decision)
	}
	st, err := ob.store.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Failed != 1 || st.Pending != 0 {
		t.Errorf("Status = %+v, want 1 failed / 0 pending", st)
	}
}
