// rss notify service should be able to send to multiple destinations and one destination preferribly via webhook,
// whenever it receives a signal to trigger the event this service should then lookup its config and send the notification
// to the specified destinations either slack,discord
// whenever the endpoint receives a request from the rss-poller service it should fire up a notification
// the notification rate should be configurable since there could be the case that you have many rss sources and you might flood with notifications the end user

package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/FKouhai/rss-demo/libs/bootstrap"
	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-notify/methods"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var serviceFQDN string

func init() {
	if err := bootstrap.WaitForLocator(); err != nil {
		log.ErrorFmt("Locator service not available: %v", err)
		return
	}

	serviceFQDN = os.Getenv("SERVICE_FQDN")
	if serviceFQDN == "" {
		serviceFQDN = "notify:3000"
	}
	if err := bootstrap.Init(context.Background(), "notify", serviceFQDN); err != nil {
		log.ErrorFmt("Failed to register notify service with locator: %v", err)
	}
}

func startHeartbeat(tracer trace.Tracer) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, span := tracer.Start(context.Background(), "bootstrap.heartbeat",
			trace.WithSpanKind(trace.SpanKindInternal))
		span.SetAttributes(attribute.String("service", "notify"))
		if err := bootstrap.Init(ctx, "notify", serviceFQDN); err != nil {
			span.RecordError(err)
			log.ErrorFmt("heartbeat re-registration failed: %v", err)
		}
		span.End()
	}
}

func main() {
	tp, err := instrumentation.InitTracer("notify")
	if err != nil {
		log.Error(err.Error())
	}
	defer func() {
		// Add a small delay to ensure traces are flushed before shutdown
		time.Sleep(2 * time.Second)
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Debug(err.Error())
		}
	}()

	tracer := instrumentation.GetTracer("notify")

	// Re-register with the locator on a heartbeat so a locator restart self-heals.
	go startHeartbeat(tracer)

	http.HandleFunc("/ws", methods.WSHandler)
	http.HandleFunc("/push", methods.DeprecatedPushHandler)
	http.HandleFunc("/healthz", methods.HealthzHandler)
	http.HandleFunc("/ready", methods.ReadyHandler)
	log.InfoFmt("starting server on port %d", 3000)
	// nolint
	http.ListenAndServe(":3000", otelhttp.NewHandler(http.DefaultServeMux, "rss_notify"))
}
