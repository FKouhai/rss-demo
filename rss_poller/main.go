package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/FKouhai/rss-demo/libs/bootstrap"
	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-poller/handlers"
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
		serviceFQDN = "poller:3000"
	}

	if err := bootstrap.Init("poller", serviceFQDN); err != nil {
		log.ErrorFmt("Failed to register poller with locator: %v", err)
	}
}

// hostFromFQDN extracts the host:port from a raw address that may be a full
// URL (e.g. "http://notify.svc:3000") or already a bare host:port.
func hostFromFQDN(fqdn string) string {
	if u, err := url.Parse(fqdn); err == nil && u.Host != "" {
		return u.Host
	}
	return fqdn
}

func discoverNotifyService() {
	notifyFQDN, err := bootstrap.GetServiceFQDN("notify")
	if err != nil {
		log.ErrorFmt("Failed to discover notify service. Falling back to NOTIFICATION_SENDER environment variable. Error: %v", err)
		addr := os.Getenv("NOTIFICATION_SENDER")
		if addr == "" {
			log.Error("NOTIFICATION_SENDER environment variable not set. Notifications will not be sent.")
			return
		}
		log.InfoFmt("Using NOTIFICATION_SENDER from environment as fallback: %s", addr)
		handlers.ConnectNotifyWS(context.Background(), addr)
		return
	}

	log.InfoFmt("Discovered notify service at: %s", notifyFQDN)
	handlers.ConnectNotifyWS(context.Background(), hostFromFQDN(notifyFQDN))
}

func startHeartbeat(tracer trace.Tracer) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, span := tracer.Start(context.Background(), "bootstrap.heartbeat",
			trace.WithSpanKind(trace.SpanKindInternal))
		span.SetAttributes(attribute.String("service", "poller"))
		if err := bootstrap.Init("poller", serviceFQDN); err != nil {
			span.RecordError(err)
			log.ErrorFmt("heartbeat re-registration failed: %v", err)
		}
		span.End()
		_ = ctx
	}
}

func main() {
	tp, err := instrumentation.InitTracer("poller")
	if err != nil {
		log.Error(err.Error())
	}
	defer func() {
		time.Sleep(2 * time.Second)
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Debug(err.Error())
		}
	}()

	tracer := instrumentation.GetTracer("poller")

	// Load persisted config on startup; starts polling immediately if feeds are found.
	handlers.LoadConfig(context.Background())

	// Re-register with the locator on a heartbeat so a locator restart self-heals.
	go startHeartbeat(tracer)

	// Discover notify service in the background with retries, so a slow cluster
	// start doesn't permanently break notifications.
	go func() {
		const maxAttempts = 10
		backoff := 3 * time.Second
		for i := range maxAttempts {
			discoverNotifyService()
			if handlers.WSConnected() {
				return
			}
			log.InfoFmt("notify service not yet discoverable, retrying in %v (attempt %d/%d)", backoff, i+1, maxAttempts)
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
		log.Error("failed to discover notify service after all attempts; notifications will not be sent")
	}()

	http.HandleFunc("/config", handlers.ConfigHandler)
	http.HandleFunc("/config/feeds", handlers.ConfigGetHandler)
	http.HandleFunc("/healthz", handlers.HealthzHandler)
	http.HandleFunc("/ready", handlers.ReadyHandler)
	http.HandleFunc("/rss", handlers.RSSHandler)
	log.InfoFmt("starting server on port %d", 3000)
	// nolint
	http.ListenAndServe(":3000", nil)
}
