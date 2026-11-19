package logger

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// withObserved replaces the package logger with an observer-backed one and
// returns the observed logs. Restores the original on cleanup.
func withObserved(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	old := zapLog
	core, observed := observer.New(zapcore.InfoLevel)
	zapLog = zap.New(core)
	t.Cleanup(func() { zapLog = old })
	return observed
}

// spanCtx returns a context carrying a valid (fake) span context.
func spanCtx() context.Context {
	return trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: 0x01,
	}))
}

func fieldsMap(entry observer.LoggedEntry) map[string]any {
	return entry.ContextMap()
}

func TestInfoCtx_AttachesTraceFields(t *testing.T) {
	observed := withObserved(t)
	InfoCtx(spanCtx(), "hello")
	if observed.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", observed.Len())
	}
	m := fieldsMap(observed.All()[0])
	if m["trace_id"] != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("trace_id = %v, want 0102…0f10", m["trace_id"])
	}
	if m["span_id"] != "0102030405060708" {
		t.Errorf("span_id = %v, want 0102030405060708", m["span_id"])
	}
}

func TestInfoCtx_NoSpanNoFields(t *testing.T) {
	observed := withObserved(t)
	InfoCtx(context.Background(), "hello")
	if observed.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", observed.Len())
	}
	m := fieldsMap(observed.All()[0])
	if _, ok := m["trace_id"]; ok {
		t.Errorf("unexpected trace_id on spanless log: %v", m)
	}
}

func TestInfoCtx_KeepsCallerFields(t *testing.T) {
	observed := withObserved(t)
	InfoCtx(spanCtx(), "hello", zap.String("k", "v"))
	m := fieldsMap(observed.All()[0])
	if m["k"] != "v" {
		t.Errorf("caller field lost: %v", m)
	}
	if m["trace_id"] == nil {
		t.Errorf("trace_id missing alongside caller fields: %v", m)
	}
}

func TestErrorFmtCtx_AttachesTraceFields(t *testing.T) {
	observed := withObserved(t)
	ErrorFmtCtx(spanCtx(), "boom %d", 7)
	if observed.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", observed.Len())
	}
	e := observed.All()[0]
	if e.Message != "boom 7" {
		t.Errorf("message = %q, want boom 7", e.Message)
	}
	if fieldsMap(e)["trace_id"] == nil {
		t.Errorf("trace_id missing on formatted log: %v", fieldsMap(e))
	}
}

func TestSpanFields_NilOnInvalidSpan(t *testing.T) {
	if got := SpanFields(trace.SpanFromContext(context.Background())); got != nil {
		t.Errorf("expected nil fields for invalid span, got %v", got)
	}
}

func TestTraceFields_NilOnBackground(t *testing.T) {
	if got := TraceFields(context.Background()); got != nil {
		t.Errorf("expected nil fields for background ctx, got %v", got)
	}
}
