// Package methods provides the needed handlerFunc for the notifier service
package methods

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// tracer is the service-scoped helper for notify spans.
var tracer = instrumentation.ForService("notify")

// DeprecatedPushHandler returns 410 Gone to signal that the HTTP push endpoint has been replaced.
func DeprecatedPushHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	w.Write([]byte(`{"error":"deprecated"}`)) //nolint:errcheck
}

// ReadyHandler returns 200 when notify is registered with the locator. Returns 503 otherwise.
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, span := tracer.StartServer(r.Context(), "handlers.ReadyHandler")
	defer span.End()
	log.InfoCtx(ctx, "connection to /ready established")

	locatorURL := os.Getenv("LOCATOR_URL")
	if locatorURL == "" {
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready", "note": "LOCATOR_URL not set, skipping registration check"}); err != nil {
			log.ErrorFmt("failed to encode response: %v", err)
		}
		return
	}

	body, err := json.Marshal(map[string]string{"service": "notify"})
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/services", locatorURL), bytes.NewBuffer(body))
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			_ = resp.Body.Close()
		}
		span.SetAttributes(attribute.Int("http.status_code", http.StatusServiceUnavailable))
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": "notify not registered with locator"}); err != nil {
			log.ErrorFmt("failed to encode response: %v", err)
		}
		return
	}
	_ = resp.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", http.StatusOK))
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready"}); err != nil {
		log.ErrorFmt("failed to encode response: %v", err)
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
