package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/FKouhai/rss-demo/libs/metrics"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-outbox/store"
	"github.com/prometheus/client_golang/prometheus/testutil"
	_ "modernc.org/sqlite"
)

// newTestApp starts an in-process outbox on an ephemeral port backed by a
// temp-file sqlite DB. It skips the tracer so the test stays offline and
// hermetic. Callers must call app.Shutdown.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "outbox_cmd_test.db")
	app, err := Run(context.Background(), Config{
		HTTPAddr:   "127.0.0.1:0",
		SkipTracer: true,
		DBDriver:   store.DriverSQLite,
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
	// just logs and continues with a nil connection (tracer also
	// skipped, so this is fully hermetic).
	app, err := Run(context.Background(), Config{
		HTTPAddr:   "127.0.0.1:0",
		SkipTracer: true,
		NATSURL:    "nats://127.0.0.1:1", // unroutable
		DBDriver:   store.DriverSQLite,
		DBDSN:      filepath.Join(t.TempDir(), "outbox_nats_test.db"),
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
		DBDriver:   store.DriverSQLite,
		DBDSN:      filepath.Join(t.TempDir(), "outbox_ready_test.db"),
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

func TestHandleAck_Skipped(t *testing.T) {
	ob := newTestOutbox(t)
	ctx := context.Background()
	if _, err := ob.store.Save(ctx, store.Entry{ID: "x3", Status: "pending"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := json.Marshal(pubsub.AckReply{TenantID: "default", OutboxID: "x3", Status: pubsub.StatusSkipped, Error: "older than max item age"})
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
	if st.Skipped != 1 || st.Pending != 0 {
		t.Errorf("Status = %+v, want 1 skipped / 0 pending", st)
	}
}

// TestHandleAck_Metrics proves the ack path is plugged into the registry:
// the terminal ack increments ack_total and releases the pending gauge.
// Lib tests prove the instruments work; this proves outbox calls them.
func TestHandleAck_Metrics(t *testing.T) {
	ob := newTestOutbox(t)
	ctx := context.Background()
	reg := metrics.Init("outbox-test")
	ackTotal := reg.Counter("ack_total", "test acks", "status")
	pendingItems := reg.Gauge("pending_items", "test pending")
	ob.WithMetrics(&Metrics{
		AckTotal:     ackTotal,
		PendingItems: pendingItems,
	})
	if _, err := ob.store.Save(ctx, store.Entry{ID: "m1", Status: "pending"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Mirror the +1 handleFeed would have recorded on Save.
	pendingItems.WithLabelValues().Set(1)
	raw, _ := json.Marshal(pubsub.AckReply{TenantID: "default", OutboxID: "m1", Status: pubsub.StatusDelivered})
	if _, err := ob.handleAck(ctx, pubsub.Message{Data: raw}); err != nil {
		t.Fatalf("handleAck: %v", err)
	}
	if got := testutil.ToFloat64(ackTotal.WithLabelValues("delivered")); got != 1 {
		t.Errorf("ack_total{delivered} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(pendingItems.WithLabelValues()); got != 0 {
		t.Errorf("pending_items = %v, want 0 after terminal ack", got)
	}
}

// TestHandleFeed_PoisonMetrics proves poison Terms are counted, not just
// logged: the feed-unmarshal Term increments poison_total{source=feed}.
func TestHandleFeed_PoisonMetrics(t *testing.T) {
	ob := newTestOutbox(t)
	reg := metrics.Init("outbox-test")
	poisonTotal := reg.Counter("poison_total", "test poison", "source")
	ob.WithMetrics(&Metrics{PoisonTotal: poisonTotal})
	if _, err := ob.handleFeed(context.Background(), pubsub.Message{Data: []byte("not json")}); err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
	if got := testutil.ToFloat64(poisonTotal.WithLabelValues("feed")); got != 1 {
		t.Errorf("poison_total{feed} = %v, want 1", got)
	}
}

func TestHandleFeed_NoteAttemptOnDispatchFailure(t *testing.T) {
	// No JetStream: Save succeeds, Publish fails. The attempt is still
	// recorded so the row's next drive comes from the sweeper, and the
	// redelivery lands on the duplicate-Save branch without recounting.
	s, err := store.NewConfig(store.Config{
		Driver: store.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "outbox_attempt_test.db"),
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	reg := metrics.Init("outbox-test")
	dispatchTotal := reg.Counter("dispatch_total", "test dispatches", "outcome")
	ob := NewOutbox(&pubsub.Client{}, s).WithMetrics(&Metrics{DispatchTotal: dispatchTotal})
	ctx := context.Background()
	raw, _ := json.Marshal(pubsub.RSSItem{
		FeedURL: "https://example.com/rss", Title: "T",
		Link: "https://example.com/1", PublishedAt: "2026-11-22T00:00:00Z",
	})
	decision, err := ob.handleFeed(ctx, pubsub.Message{Data: raw})
	if err == nil {
		t.Fatal("expected publish error (no JetStream), got nil")
	}
	if decision != pubsub.AckDecisionNack {
		t.Errorf("decision = %v, want Nack", decision)
	}
	if got := testutil.ToFloat64(dispatchTotal.WithLabelValues("publish_error")); got != 1 {
		t.Errorf("dispatch_total{publish_error} = %v, want 1", got)
	}
	pending, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	if len(pending) != 1 || pending[0].Attempts != 1 {
		t.Fatalf("row attempts = %+v, want one row at 1", pending)
	}
	if pending[0].NextRetryAt == nil {
		t.Error("NextRetryAt is nil, want InitialRetryDelay scheduled")
	}
	// Redelivery hits the duplicate branch: Ack, no recount, no re-drive.
	decision, err = ob.handleFeed(ctx, pubsub.Message{Data: raw})
	if err != nil {
		t.Fatalf("redelivery handleFeed: %v", err)
	}
	if decision != pubsub.AckDecisionAck {
		t.Errorf("redelivery decision = %v, want Ack", decision)
	}
	pending, _ = s.GetPending(ctx)
	if pending[0].Attempts != 1 {
		t.Errorf("attempts after redelivery = %d, want still 1", pending[0].Attempts)
	}
}

func TestSweepOnce_RedrivesAndExhausts(t *testing.T) {
	s, err := store.NewConfig(store.Config{
		Driver: store.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "outbox_sweep_test.db"),
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	reg := metrics.Init("outbox-test")
	dispatchTotal := reg.Counter("dispatch_total", "test dispatches", "outcome")
	ackTotal := reg.Counter("ack_total", "test acks", "status")
	pendingItems := reg.Gauge("pending_items", "test pending")
	ob := NewOutbox(&pubsub.Client{}, s).WithMetrics(&Metrics{
		DispatchTotal: dispatchTotal, AckTotal: ackTotal, PendingItems: pendingItems,
	})
	ctx := context.Background()
	// One fresh row (due in the past via NoteAttempt) and one exhausted row.
	mustSave := func(id string) {
		t.Helper()
		if _, err := s.Save(ctx, store.Entry{ID: id, Status: "pending", CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}
	mustSave("sweep-due")
	mustSave("sweep-dead")
	past := time.Now().UTC().Add(-time.Minute)
	if err := s.NoteAttempt(ctx, "sweep-due", past); err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}
	for range store.MaxAttempts {
		if err := s.NoteAttempt(ctx, "sweep-dead", past); err != nil {
			t.Fatalf("NoteAttempt: %v", err)
		}
	}
	pendingItems.WithLabelValues().Set(2)

	ob.sweepOnce(ctx)

	// Due row: re-driven (publish fails, no broker) and rescheduled.
	due, err := s.GetPending(ctx)
	if err != nil {
		t.Fatalf("GetPending: %v", err)
	}
	byID := map[string]store.Entry{}
	for _, e := range due {
		byID[e.ID] = e
	}
	if byID["sweep-due"].Attempts != 2 {
		t.Errorf("sweep-due attempts = %d, want 2 (1 setup + 1 sweep)", byID["sweep-due"].Attempts)
	}
	if byID["sweep-due"].NextRetryAt == nil || !byID["sweep-due"].NextRetryAt.After(time.Now().UTC()) {
		t.Errorf("sweep-due next = %v, want future backoff", byID["sweep-due"].NextRetryAt)
	}
	if got := testutil.ToFloat64(dispatchTotal.WithLabelValues("publish_error")); got != 1 {
		t.Errorf("dispatch_total{publish_error} = %v, want 1", got)
	}
	// Exhausted row: terminal failed, out of pending, gauge released.
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Failed != 1 || st.Pending != 1 {
		t.Errorf("Status = %+v, want 1 failed / 1 pending", st)
	}
	if got := testutil.ToFloat64(ackTotal.WithLabelValues("failed")); got != 1 {
		t.Errorf("ack_total{failed} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(pendingItems.WithLabelValues()); got != 1 {
		t.Errorf("pending_items = %v, want 1", got)
	}

	// Second sweep: nothing due (backoff in the future), no-op.
	ob.sweepOnce(ctx)
	if got := testutil.ToFloat64(dispatchTotal.WithLabelValues("publish_error")); got != 1 {
		t.Errorf("dispatch_total after quiet sweep = %v, want still 1", got)
	}
}
