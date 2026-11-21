// Package cmd is the in-process entrypoint for rss_notify.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the server and the tracer.
// main.go is a thin wrapper around Run; tests call Run directly so the
// service runs in-process (as goroutines) with deterministic readiness.
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
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-demo/libs/service"
	"github.com/FKouhai/rss-notify/methods"
)

// Config carries notify's env-derived settings. MaxItemAge bounds Discord
// posts to fresh items (NOTIFY_MAX_ITEM_AGE, e.g. "2h"); older deliveries
// are acked skipped, never posted.
type Config struct {
	service.BaseConfig
	NATSURL string
	// MaxItemAge bounds Discord posts to fresh items; older deliveries
	// are acked skipped, never posted. Zero disables the gate.
	MaxItemAge time.Duration
	// NATSRetryBudget bounds startup reconnect retries (compose sets it;
	// tests leave it 0 for an instant degraded boot). Exhaustion boots
	// degraded with /ready held at 503.
	NATSRetryBudget time.Duration
}

// App is a running notify instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	nc       *pubsub.Client
	wired    atomic.Bool
	notifier *Notify
}

// Run starts the notify service with the given config and returns once the
// HTTP listener is bound. It does not block for the lifetime of the server;
// callers wait on App.Ready() and later call App.Shutdown().
func Run(ctx context.Context, cfg Config) (*App, error) {
	cfg.WithDefaults("notify")

	boot, err := cfg.Bootstrap(ctx, "notify")
	if err != nil {
		return nil, err
	}
	app := &App{}
	app.Boot = boot

	client, err := pubsub.DialWithRetry(ctx, cfg.NATSURL, pubsub.RssOptions{ServiceName: "notify"}, cfg.NATSRetryBudget)
	if err != nil {
		log.ErrorFmt("Failed to connect to NATS at %s: %v", cfg.NATSURL, err)
	} else {
		log.InfoFmt("Connected to NATS at %s", cfg.NATSURL)
		app.nc = client
	}

	if app.nc != nil {
		noti := NewNotify(app.nc, cfg.MaxItemAge)
		if err := noti.Start(ctx); err != nil {
			app.shutdown(context.Background())
			return nil, fmt.Errorf("notify start: %w", err)
		}
		app.notifier = noti
		app.wired.Store(true)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", methods.HealthzHandler)
	// Readiness means wired: HTTP serving alone is not enough for a
	// NATS-backed service. K8s readiness probes hold the pod out of
	// endpoints until subscriptions are established.
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if !app.wired.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": "nats unavailable"})
			return
		}
		methods.ReadyHandler(w, r)
	})

	srv, err := service.ServeHTTP(cfg.HTTPAddr, mux, "rss_notify")
	if err != nil {
		app.shutdown(context.Background())
		return nil, fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	app.HTTP = srv

	return app, nil
}

// NATS exposes the connection (nil if NATS was unavailable) for
// integration tests that drive the broker directly.
func (a *App) NATS() *pubsub.Client { return a.nc }

// Shutdown stops all components and returns the joined error.
// The notifier stops first: in-flight deliveries complete against the
// still-open connection, and anything interrupted stays unacked and
// redelivers on restart.
func (a *App) Shutdown(ctx context.Context) error {
	if a.notifier != nil {
		a.notifier.Stop()
	}
	var errs []error
	if a.nc != nil {
		a.nc.Close()
	}
	errs = append(errs, a.BaseApp.Shutdown(ctx))
	return errors.Join(errs...)
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.Shutdown(ctx)
}
