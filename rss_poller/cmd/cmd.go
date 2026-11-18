// Package cmd is the in-process entrypoint for rss_poller.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the server, the notify
// discovery loop, and the tracer. main.go is a thin wrapper around Run;
// tests call Run directly so the service runs in-process (as goroutines)
// with deterministic readiness.
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/FKouhai/rss-demo/libs/bootstrap"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/service"
	"github.com/FKouhai/rss-poller/handlers"
)

// Config is the poller bootstrap config shared by all Go services.
// It is an alias, not a new struct: the poller adds no service-specific
// bootstrap fields, so construction sites stay flat
// (Config{HTTPAddr: ..., SkipLocator: ...}).
type Config = service.BaseConfig

// App is a running poller instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	cancel context.CancelFunc
}

// Run starts the poller with the given config and returns once the HTTP
// listener is bound. It does not block for the lifetime of the server;
// callers wait on App.Ready() and later call App.Shutdown().
func Run(ctx context.Context, cfg Config) (*App, error) {
	cfg.WithDefaults("poller")

	boot, err := cfg.Bootstrap(ctx, "poller")
	if err != nil {
		return nil, err
	}

	discoverCtx, cancel := context.WithCancel(context.Background())
	app := &App{cancel: cancel}
	app.Boot = boot

	// Load persisted config on startup; starts polling immediately if feeds are found.
	handlers.LoadConfig(ctx)

	// Discover notify service in the background with retries, so a slow cluster
	// start doesn't permanently break notifications.
	go discoverNotifyLoop(discoverCtx)

	mux := http.NewServeMux()
	mux.HandleFunc("/config", handlers.ConfigHandler)
	mux.HandleFunc("/config/feeds", handlers.ConfigGetHandler)
	mux.HandleFunc("/healthz", handlers.HealthzHandler)
	mux.HandleFunc("/ready", handlers.ReadyHandler)
	mux.HandleFunc("/rss", handlers.RSSHandler)

	srv, err := service.ServeHTTP(cfg.HTTPAddr, mux, "rss_poller")
	if err != nil {
		app.shutdown(context.Background())
		return nil, fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	app.HTTP = srv

	return app, nil
}

// Shutdown stops the discovery loop and all components,
// returning the joined error.
func (a *App) Shutdown(ctx context.Context) error {
	a.cancel()
	return a.BaseApp.Shutdown(ctx)
}

func (a *App) shutdown(ctx context.Context) {
	a.cancel()
	_ = a.BaseApp.Shutdown(ctx)
}

// discoverNotifyLoop retries notify discovery until the WebSocket connects,
// the attempts run out, or ctx ends (Shutdown).
func discoverNotifyLoop(ctx context.Context) {
	const maxAttempts = 10
	backoff := 3 * time.Second
	for i := range maxAttempts {
		discoverNotifyService()
		if handlers.WSConnected() {
			return
		}
		log.InfoFmt("notify service not yet discoverable, retrying in %v (attempt %d/%d)", backoff, i+1, maxAttempts)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	log.Error("failed to discover notify service after all attempts; notifications will not be sent")
}

func discoverNotifyService() {
	notifyFQDN, err := bootstrap.GetServiceFQDN(context.Background(), "notify")
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

// hostFromFQDN extracts the host:port from a raw address that may be a full
// URL (e.g. "http://notify.svc:3000") or already a bare host:port.
func hostFromFQDN(fqdn string) string {
	if u, err := url.Parse(fqdn); err == nil && u.Host != "" {
		return u.Host
	}
	return fqdn
}
