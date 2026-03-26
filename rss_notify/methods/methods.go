// Package methods provides the needed handlerFunc for the notifier service
package methods

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	webhookpush "github.com/FKouhai/rss-notify/webhookPush"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ReadyHandler returns 200 when notify is registered with the locator. Returns 503 otherwise.
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, span := instrumentation.GetTracer("notify").Start(ctx, "handlers.ReadyHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	log.Info("connection to /ready established", zap.String("trace_id", span.SpanContext().TraceID().String()))

	locatorURL := os.Getenv("LOCATOR_URL")
	if locatorURL == "" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ready", "note": "LOCATOR_URL not set, skipping registration check"})
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

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		span.SetAttributes(attribute.Int("http.status_code", http.StatusServiceUnavailable))
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": "notify not registered with locator"})
		return
	}
	resp.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", http.StatusOK))
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// PushNotificationHandler is the handler that is in charge of sending notification to the destination sourceloggers
func PushNotificationHandler(w http.ResponseWriter, r *http.Request) {
	// Extract the tracing context from the incoming request headers
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	_, span := instrumentation.GetTracer("notify").Start(ctx, "handlers.PushNotification", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	log.Info("connection established", zap.String("trace_id", span.SpanContext().TraceID().String()))

	if r.Method != "POST" {
		w.WriteHeader(http.StatusBadRequest)
		log.Error("wrong method was used")
		span.SetAttributes(attribute.Int("http.status_code", http.StatusBadRequest))
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		log.Info("not using json")
		span.AddEvent("FAILED_TRANSACTION")
		span.SetAttributes(attribute.Int("http.status_code", http.StatusBadRequest))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusBadRequest, err.Error())
		span.SetAttributes(attribute.Int("http.status_code", http.StatusBadRequest))
		log.Error(err.Error())
		return
	}

	var d webhookpush.DiscordNotification
	message, err := d.GetContent(ctx, body)
	if err != nil {
		log.Error("fails here")
		w.WriteHeader(http.StatusBadRequest)
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusBadRequest, err.Error())
		span.SetAttributes(attribute.Int("http.status_code", http.StatusBadRequest))
		log.Error(err.Error())
		return
	}
	span.SetAttributes(attribute.Int("messages.count", len(message)))

	httpStatus, err := d.SendNotification(ctx, message)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusInternalServerError, err.Error())
		span.SetAttributes(attribute.Int("http.status_code", http.StatusInternalServerError))
		log.Error(err.Error())
		return
	}

	w.WriteHeader(httpStatus)
	span.SetAttributes(attribute.Int("http.status_code", httpStatus), attribute.Int("webhook.status", httpStatus))
}

// HealthzHandler is the route that exposes a healthcheck
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	// Extract the tracing context from the incoming request headers
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	_, span := instrumentation.GetTracer("notify").Start(ctx, "handlers.HealthzHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	log.Info("connection to /health established", zap.String("trace_id", span.SpanContext().TraceID().String()))
	w.WriteHeader(http.StatusOK)
	span.SetAttributes(attribute.Int("http.status_code", http.StatusOK))
	status := map[string]string{"status": "healthy"}
	err := json.NewEncoder(w).Encode(status)
	if err != nil {
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusInternalServerError, err.Error())
		span.SetAttributes(attribute.Int("http.status_code", http.StatusInternalServerError))
		log.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
