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
	"errors"
	"fmt"
	"net/http"

	log "github.com/FKouhai/rss-demo/libs/logger"
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
}

// App is a running poller instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	nc *pubsub.Client
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

	client, err := pubsub.NewClient(cfg.NATSURL, pubsub.RssOptions{ServiceName: "poller"})
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
	}

	// Load persisted config on startup; starts polling immediately if feeds are found.
	handlers.LoadConfig(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handlers.HealthzHandler)
	mux.HandleFunc("/ready", handlers.ReadyHandler)

	srv, err := service.ServeHTTP(cfg.HTTPAddr, mux, "rss_poller")
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
