// Package methods provides the needed handlerFunc for the notifier service
package methods

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	webhookpush "github.com/FKouhai/rss-notify/webhookPush"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

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
