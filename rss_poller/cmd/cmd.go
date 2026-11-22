// Package cmd is the in-process entrypoint for rss_poller.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the server, the NATS
// connection, and the tracer. main.go is a thin wrapper around Run;
// tests call Run directly so the service runs in-process (as goroutines)
// with deterministic readiness.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/metrics"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-demo/libs/service"
	"github.com/FKouhai/rss-poller/handlers"
)

// Config configures a poller instance. Env-derived in main.go;
// tests construct it directly.
type Config struct {
	service.BaseConfig
	// NATSURL is the broker address. Empty means the local default.
	NATSURL string
	// NATSRetryBudget bounds startup reconnect retries (compose sets it;
	// tests leave it 0 for an instant degraded boot). Exhaustion boots
	// degraded with /ready held at 503.
	NATSRetryBudget time.Duration
	// ConfigURL is the rss_config base URL (e.g. http://rss_config:3000).
	// Empty disables service polling: the file is the only source.
	// ConfigSecret is the gateway secret for config (empty = open mode).
	// ConfigPollInterval bounds the refresh loop (default 60s).
	ConfigURL          string
	ConfigSecret       string
	ConfigPollInterval time.Duration
}

// App is a running poller instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	nc    *pubsub.Client
	wired atomic.Bool
}

// Run starts the poller with the given config and returns once the HTTP
// listener is bound. It does not block for the lifetime of the server;
// callers wait on App.Ready() and later call App.Shutdown().
func Run(ctx context.Context, cfg Config) (*App, error) {
	cfg.WithDefaults("poller")
	if cfg.NATSURL == "" {
		cfg.NATSURL = "nats://127.0.0.1:4222"
	}

	boot, err := cfg.Bootstrap(ctx, "poller")
	if err != nil {
		return nil, err
	}
	app := &App{}
	app.Boot = boot

	client, err := pubsub.DialWithRetry(ctx, cfg.NATSURL, pubsub.RssOptions{ServiceName: "poller"}, cfg.NATSRetryBudget)
	if err != nil {
		log.ErrorFmt("Failed to connect to NATS at %s: %v", cfg.NATSURL, err)
	} else {
		log.InfoFmt("Connected to NATS at %s", cfg.NATSURL)
		if _, err := client.EnsureStream(ctx, pubsub.FeedsStreamConfig()); err != nil {
			client.Close()
			app.shutdown(context.Background())
			return nil, fmt.Errorf("ensure feeds stream: %w", err)
		}
		app.nc = client
		handlers.SetNATSClient(client)
		app.wired.Store(true)
	}

	// Load persisted config on startup; starts polling immediately if feeds are found.
	handlers.LoadConfig(ctx)
	// Service config overlays the file: same shape, polled, last-good
	// kept while config is unreachable.
	handlers.StartConfigPoll(cfg.ConfigURL, cfg.ConfigSecret, cfg.ConfigPollInterval)

	reg := metrics.Init("poller")
	handlers.SetMetrics(&handlers.Metrics{
		CycleItems:  reg.Counter("cycle_items_total", "poller items per cycle", "outcome"),
		FetchErrors: reg.Counter("fetch_error_total", "poller feed fetch failures"),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handlers.HealthzHandler)
	// Readiness means wired: HTTP serving alone is not enough for a
	// NATS-backed service. K8s readiness probes hold the pod out of
	// endpoints until the broker is reachable and streams are ensured.
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if !app.wired.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": "nats unavailable"})
			return
		}
		handlers.ReadyHandler(w, r)
	})

	srv, err := service.ServeHTTP(cfg.HTTPAddr, mux, "rss_poller", reg.Handler())
	if err != nil {
		app.shutdown(context.Background())
		return nil, fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	app.HTTP = srv

	return app, nil
}

// NATS exposes the connection (nil if NATS was unavailable) for tests.
func (a *App) NATS() *pubsub.Client { return a.nc }

// Shutdown stops all components and returns the joined error.
func (a *App) Shutdown(ctx context.Context) error {
	handlers.StopConfigPoll()
	var errs []error
	if a.nc != nil {
		a.nc.Close()
	}
	errs = append(errs, a.BaseApp.Shutdown(ctx))
	return errors.Join(errs...)
}

func (a *App) shutdown(ctx context.Context) {
	if a.nc != nil {
		a.nc.Close()
	}
	_ = a.BaseApp.Shutdown(ctx)
}
