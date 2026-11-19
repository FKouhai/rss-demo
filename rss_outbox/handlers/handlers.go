package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-outbox/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var db store.Store

// SetStore simply sets the db driver to use
func SetStore(s store.Store) { db = s }

// HealthzHandler healthz endpoint
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	_, span := instrumentation.GetTracer("outbox").Start(r.Context(), "handlers.HealthzHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	log.Info("connection to /healthz established", zap.String("trace_id", span.SpanContext().TraceID().String()))
	w.WriteHeader(http.StatusOK)
	span.SetAttributes(attribute.Int("http.status_code", http.StatusOK))
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "healthy"}); err != nil {
		log.ErrorFmt("failed to encode healthz response: %v", err)
	}
}

// ReadyHandler readiness endpoint
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, span := instrumentation.GetTracer("outbox").Start(r.Context(), "handlers.ReadyHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	log.Info("connection to /ready established", zap.String("trace_id", span.SpanContext().TraceID().String()))
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready"}); err != nil {
		log.ErrorFmt("failed to encode ready response: %v", err)
	}
}

// OutboxStatusHandler outbox status endpoint
func OutboxStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, span := instrumentation.GetTracer("outbox").Start(r.Context(), "handlers.OutboxStatusHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	log.Info("connection to /outbox/status established", zap.String("trace_id", span.SpanContext().TraceID().String()))

	st, err := db.Status(r.Context())
	if err != nil {
		log.ErrorFmt("failed to get outbox status: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(st); err != nil {
		log.ErrorFmt("failed to encode outbox status response: %v", err)
	}
}
