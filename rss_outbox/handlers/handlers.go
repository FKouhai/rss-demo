package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-outbox/store"
	"go.opentelemetry.io/otel/attribute"
)

var db store.Store

// tracer is the service-scoped helper for outbox spans.
var tracer = instrumentation.ForService("outbox")

// SetStore simply sets the db driver to use
func SetStore(s store.Store) { db = s }

// HealthzHandler healthz endpoint
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.HealthzHandler")
	defer span.End()
	log.InfoCtx(r.Context(), "connection to /healthz established")
	w.WriteHeader(http.StatusOK)
	span.SetAttributes(attribute.Int("http.status_code", http.StatusOK))
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "healthy"}); err != nil {
		log.ErrorFmtCtx(r.Context(), "failed to encode healthz response: %v", err)
	}
}

// ReadyHandler readiness endpoint
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, span := tracer.StartServer(r.Context(), "handlers.ReadyHandler")
	defer span.End()
	log.InfoCtx(r.Context(), "connection to /ready established")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready"}); err != nil {
		log.ErrorFmtCtx(r.Context(), "failed to encode ready response: %v", err)
	}
}

// OutboxStatusHandler outbox status endpoint
func OutboxStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, span := tracer.StartServer(r.Context(), "handlers.OutboxStatusHandler")
	defer span.End()
	log.InfoCtx(r.Context(), "connection to /outbox/status established")

	st, err := db.Status(r.Context())
	if err != nil {
		instrumentation.Fail(span, err, "failed to get outbox status: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(st); err != nil {
		log.ErrorFmtCtx(r.Context(), "failed to encode outbox status response: %v", err)
	}
}
