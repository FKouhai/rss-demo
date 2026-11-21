// Package cmd is the in-process entrypoint for rss_aggregator.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the NATS consumer, the
// store, the NATS connection, and the tracer. main.go is a thin wrapper
// around Run; integration tests call Run directly so the service runs
// in-process (as goroutines) with deterministic readiness.
//
// The aggregator is the read model for feed items: it SUBs feeds.new,
// persists every item, and serves the frontend's GET /api/items,
// GET /api/items/{id}, and GET /api/feeds over HTTP.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/FKouhai/rss-aggregator/handlers"
	"github.com/FKouhai/rss-aggregator/store"
	db "github.com/FKouhai/rss-demo/libs/db"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-demo/libs/service"
)

// Config configures an aggregator instance. Env-derived in main.go;
// integration tests construct it directly.
type Config struct {
	service.BaseConfig
	NATSURL string
	// NATSRetryBudget bounds startup reconnect retries (compose sets it;
	// tests leave it 0 for an instant degraded boot). Exhaustion boots
	// degraded with /ready held at 503.
	NATSRetryBudget time.Duration
	DBDriver        db.Driver
	DBDSN           string
}

// App is a running aggregator instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	nc         *pubsub.Client
	wired      atomic.Bool
	store      store.Store
	aggregator *Aggregator
}

// Run starts the aggregator with the given config and returns once the
// HTTP listener is bound. It does not block for the lifetime of the
// server; callers wait on App.Ready() and later call App.Shutdown().
func Run(ctx context.Context, cfg Config) (*App, error) {
	cfg.WithDefaults("aggregator")
	if cfg.NATSURL == "" {
		cfg.NATSURL = "nats://127.0.0.1:4222"
	}
	if cfg.DBDriver == "" {
		cfg.DBDriver = db.DriverSQLite
	}
	if cfg.DBDSN == "" {
		cfg.DBDSN = "/var/lib/rss-aggregator/aggregator.db"
	}

	app := &App{}

	boot, err := cfg.Bootstrap(ctx, "aggregator")
	if err != nil {
		return nil, err
	}
	app.Boot = boot

	client, err := pubsub.DialWithRetry(ctx, cfg.NATSURL, pubsub.RssOptions{ServiceName: "aggregator"}, cfg.NATSRetryBudget)
	if err != nil {
		log.ErrorFmt("Failed to connect to NATS at %s: %v", cfg.NATSURL, err)
	} else {
		log.InfoFmt("Connected to NATS at %s", cfg.NATSURL)
		app.nc = client
	}

	s, err := store.NewConfig(store.Config{Driver: cfg.DBDriver, DSN: cfg.DBDSN})
	if err != nil {
		app.shutdown(context.Background())
		return nil, fmt.Errorf("store init: %w", err)
	}
	app.store = s
	log.InfoFmt("Aggregator store initialized (driver=%s)", cfg.DBDriver)
	handlers.SetStore(s)

	// NATS consume lifecycle lives on the Aggregator struct. Wired here
	// (not in main) so Ready — which fires at listen time below — implies
	// the subscription is established. Skipped when NATS is unavailable so
	// offline tests and degraded boots still serve HTTP.
	if app.nc != nil {
		ag := NewAggregator(app.nc, s)
		if err := ag.Start(ctx); err != nil {
			app.shutdown(context.Background())
			return nil, fmt.Errorf("aggregator start: %w", err)
		}
		app.aggregator = ag
		app.wired.Store(true)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handlers.HealthzHandler)
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
		handlers.ReadyHandler(w, r)
	})
	mux.HandleFunc("/api/items", handlers.ItemsHandler)
	mux.HandleFunc("/api/items/{id}", handlers.ItemHandler)
	mux.HandleFunc("/api/feeds", handlers.FeedsHandler)

	srv, err := service.ServeHTTP(cfg.HTTPAddr, mux, "rss_aggregator")
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

// Store exposes the store for integration tests that seed/observe rows.
func (a *App) Store() store.Store { return a.store }

// Shutdown stops all components and returns the joined error.
// The consumer stops first so no new items enter during teardown;
// in-flight handler work completes against the still-open store;
// anything interrupted stays unacked and redelivers on restart.
func (a *App) Shutdown(ctx context.Context) error {
	if a.aggregator != nil {
		a.aggregator.Stop()
	}
	var errs []error
	if a.store != nil {
		errs = append(errs, a.store.Close())
	}
	if a.nc != nil {
		a.nc.Close()
	}
	errs = append(errs, a.BaseApp.Shutdown(ctx))
	return errors.Join(errs...)
}

func (a *App) shutdown(ctx context.Context) {
	if a.aggregator != nil {
		a.aggregator.Stop()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
	if a.nc != nil {
		a.nc.Close()
	}
	_ = a.BaseApp.Shutdown(ctx)
}
