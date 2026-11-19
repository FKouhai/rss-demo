// Package cmd is the in-process entrypoint for rss_outbox.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the server, store,
// NATS connection, and tracer. main.go is a thin wrapper around Run;
// integration tests call Run directly so the service runs in-process
// (as goroutines) with deterministic readiness.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-demo/libs/service"
	"github.com/FKouhai/rss-outbox/handlers"
	"github.com/FKouhai/rss-outbox/store"
)

// Config configures an outbox instance. Env-derived in main.go;
// integration tests construct it directly.
type Config struct {
	service.BaseConfig
	NATSURL  string
	DBDriver store.Driver
	DBDSN    string
}

// App is a running outbox instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	nc     *pubsub.Client
	store  store.Store
	outbox *Outbox
}

// Run starts the outbox with the given config and returns once the HTTP
// listener is bound. It does not block for the lifetime of the server;
// callers wait on App.Ready() and later call App.Shutdown().
func Run(ctx context.Context, cfg Config) (*App, error) {
	cfg.WithDefaults("outbox")
	if cfg.NATSURL == "" {
		cfg.NATSURL = "nats://127.0.0.1:4222"
	}
	if cfg.DBDriver == "" {
		cfg.DBDriver = store.DriverSQLite
	}
	if cfg.DBDSN == "" {
		cfg.DBDSN = "/var/lib/rss-outbox/outbox.db"
	}

	app := &App{}

	boot, err := cfg.Bootstrap(ctx, "outbox")
	if err != nil {
		return nil, err
	}
	app.Boot = boot

	client, err := pubsub.NewClient(cfg.NATSURL, pubsub.RssOptions{ServiceName: "outbox"})
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
	log.InfoFmt("Outbox store initialized (driver=%s)", cfg.DBDriver)
	handlers.SetStore(s)

	// NATS pub/sub lifecycle lives on the Outbox struct. Wired here (not
	// in main) so Ready — which fires at listen time below — implies the
	// subscriptions are established. Skipped when NATS is unavailable so
	// offline tests and degraded boots still serve HTTP.
	if app.nc != nil {
		ob := NewOutbox(app.nc, s)
		if err := ob.Start(ctx); err != nil {
			app.shutdown(context.Background())
			return nil, fmt.Errorf("outbox start: %w", err)
		}
		app.outbox = ob
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handlers.HealthzHandler)
	mux.HandleFunc("/ready", handlers.ReadyHandler)
	mux.HandleFunc("/outbox/status", handlers.OutboxStatusHandler)

	srv, err := service.ServeHTTP(cfg.HTTPAddr, mux, "rss_outbox")
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
// Consumers stop first: the HTTP handlers are read-only, but NATS intake
// is state-changing, so no new work may enter once teardown begins.
// In-flight handler work completes against the still-open store; anything
// interrupted stays unacked and redelivers on restart.
func (a *App) Shutdown(ctx context.Context) error {
	if a.outbox != nil {
		a.outbox.Stop()
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
	if a.outbox != nil {
		a.outbox.Stop()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
	if a.nc != nil {
		a.nc.Close()
	}
	_ = a.BaseApp.Shutdown(ctx)
}
