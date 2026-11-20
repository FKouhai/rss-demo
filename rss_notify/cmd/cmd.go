// Package cmd is the in-process entrypoint for rss_notify.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the server and the tracer.
// main.go is a thin wrapper around Run; tests call Run directly so the
// service runs in-process (as goroutines) with deterministic readiness.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/pubsub"
	"github.com/FKouhai/rss-demo/libs/service"
	"github.com/FKouhai/rss-notify/methods"
)

// Config is the notify bootstrap config shared by all Go services.
// It is an alias, not a new struct: notify adds no service-specific
// bootstrap fields, so construction sites stay flat
// (Config{HTTPAddr: ..., SkipLocator: ...}).
type Config struct {
	service.BaseConfig
	NATSURL string
}

// App is a running notify instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	nc       *pubsub.Client
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

	client, err := pubsub.NewClient(cfg.NATSURL, pubsub.RssOptions{ServiceName: "notify"})
	if err != nil {
		log.ErrorFmt("Failed to connect to NATS at %s: %v", cfg.NATSURL, err)
	} else {
		log.InfoFmt("Connected to NATS at %s", cfg.NATSURL)
		app.nc = client
	}

	if app.nc != nil {
		noti := NewNotify(app.nc)
		if err := noti.Start(ctx); err != nil {
			app.shutdown(context.Background())
			return nil, fmt.Errorf("notify start: %w", err)
		}
		app.notifier = noti
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", methods.HealthzHandler)
	mux.HandleFunc("/ready", methods.ReadyHandler)

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
