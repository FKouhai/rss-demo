// Package methods provides the needed handlerFunc for the notifier service
package methods

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/FKouhai/rss-notify/instrumentation"
	log "github.com/FKouhai/rss-notify/logger"
	webhookpush "github.com/FKouhai/rss-notify/webhookPush"
	"go.opentelemetry.io/otel/trace"
)

// PushNotificationHandler is the handler that is in charge of sending notification to the destination sourceloggers
func PushNotificationHandler(w http.ResponseWriter, r *http.Request) {
	log.Info("connection established")
	ctx := r.Context()
	_, span := instrumentation.GetTracer("notify").Start(ctx, "handlers.PushNotification", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	if r.Method != "POST" {
		w.WriteHeader(http.StatusBadRequest)
		log.Error("wrong method was used")
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		log.Info("not using json")
		span.AddEvent("FAILED_TRANSACTION")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusBadRequest, err.Error())
		log.Error(err.Error())
		return
	}

	var d webhookpush.DiscordNotification
	message, err := d.GetContent(body)
	if err != nil {
		log.Error("fails here")
		w.WriteHeader(http.StatusBadRequest)
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusBadRequest, err.Error())
		log.Error(err.Error())
		return
	}

	httpStatus, err := d.SendNotification(message)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusInternalServerError, err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		log.Error(err.Error())
		return
	}

	w.WriteHeader(httpStatus)

}

// HealthzHandler is the route that exposes a healthcheck
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := instrumentation.GetTracer("notify").Start(ctx, "handlers.HealthzHandler", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	log.Info("connection to /health established")
	w.WriteHeader(http.StatusOK)
	status := map[string]string{"status": "healthy"}
	err := json.NewEncoder(w).Encode(status)
	if err != nil {
		span.AddEvent("FAILED_TRANSACTION")
		span.RecordError(err)
		span.SetStatus(http.StatusInternalServerError, err.Error())
		log.Error(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
