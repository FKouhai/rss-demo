// Package cmd is the in-process entrypoint for rss_notify.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the server and the tracer.
// main.go is a thin wrapper around Run; tests call Run directly so the
// service runs in-process (as goroutines) with deterministic readiness.
package cmd

import (
	"context"
	"fmt"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/service"
	"github.com/FKouhai/rss-notify/methods"
)

// Config is the notify bootstrap config shared by all Go services.
// It is an alias, not a new struct: notify adds no service-specific
// bootstrap fields, so construction sites stay flat
// (Config{HTTPAddr: ..., SkipLocator: ...}).
type Config = service.BaseConfig

// App is a running notify instance. Close it with Shutdown.
type App struct {
	service.BaseApp
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

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", methods.WSHandler)
	mux.HandleFunc("/push", methods.DeprecatedPushHandler)
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

func (a *App) shutdown(ctx context.Context) {
	_ = a.Shutdown(ctx)
}
