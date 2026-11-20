// Package methods provides the needed handlerFunc for the notifier service
package methods

import (
	"encoding/json"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/otel/attribute"
)

// tracer is the service-scoped helper for notify spans.
var tracer = instrumentation.ForService("notify")

// ReadyHandler reports readiness. The process only reaches this point after
// NATS is connected and streams are ensured (Run hard-fails otherwise),
// so serving HTTP here means ready.
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, span := tracer.StartServer(r.Context(), "handlers.ReadyHandler")
	defer span.End()
	log.InfoCtx(ctx, "connection to /ready established")

	span.SetAttributes(attribute.Int("http.status_code", http.StatusOK))
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready"}); err != nil {
		instrumentation.Fail(span, err, "failed to encode response: %v", err)
	}
}

// HealthzHandler is the route that exposes a healthcheck
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.HealthzHandler")
	defer span.End()
	log.InfoCtx(r.Context(), "connection to /health established")
	w.WriteHeader(http.StatusOK)
	span.SetAttributes(attribute.Int("http.status_code", http.StatusOK))
	status := map[string]string{"status": "healthy"}
	err := json.NewEncoder(w).Encode(status)
	if err != nil {
		span.AddEvent("FAILED_TRANSACTION")
		instrumentation.Fail(span, err, "%s", err.Error())
		span.SetAttributes(attribute.Int("http.status_code", http.StatusInternalServerError))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
