// Package cmd is the in-process entrypoint for rss_frontend.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the server and the
// tracer. main.go is a thin wrapper around Run; tests call Run directly
// so the service runs in-process (as goroutines) with deterministic
// readiness.
//
// The frontend is stateless: items come from the aggregator over HTTP,
// sessions from OIDC cookies. No NATS, no database.
package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/auth/oidc"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/metrics"
	"github.com/FKouhai/rss-demo/libs/service"
	"github.com/FKouhai/rss-frontend/handlers"
	"github.com/FKouhai/rss-frontend/internal/extract"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Config configures a frontend instance. Env-derived in main.go;
// tests construct it directly.
type Config struct {
	service.BaseConfig
	AggregatorURL string
	OutboxURL     string
	PollerURL     string
	NotifyURL     string
	// OIDC (edge login via Pocket-ID/Tiny Auth). Empty Issuer means
	// open mode: all pages public, no session middleware.
	OIDCIssuer    string
	OIDCClientID  string
	OIDCClientSec string
	OIDCRedirect  string
	SessionKeyB64 string
	// Config service wiring (Configure page). Empty ConfigURL keeps
	// the aggregator fallback; empty secret is open mode.
	ConfigURL    string
	ConfigSecret string
	AdminGroups  []string
}

// App is a running frontend instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	oidc *oidc.Client
}

// OIDC exposes the OIDC client (nil in open mode) for tests.
func (a *App) OIDC() *oidc.Client { return a.oidc }

// Run starts the frontend with the given config and returns once the HTTP
// listener is bound. It does not block for the lifetime of the server;
// callers wait on App.Ready() and later call App.Shutdown().
func Run(ctx context.Context, cfg Config) (*App, error) {
	cfg.WithDefaults("frontend")
	defaultURL := func(v, def string) string {
		if v != "" {
			return v
		}
		return def
	}
	cfg.AggregatorURL = defaultURL(cfg.AggregatorURL, "http://rss_aggregator:3000")
	cfg.OutboxURL = defaultURL(cfg.OutboxURL, "http://rss_outbox:3000")
	cfg.PollerURL = defaultURL(cfg.PollerURL, "http://rss_poller:3000")
	cfg.NotifyURL = defaultURL(cfg.NotifyURL, "http://rss_notify:3000")
	if len(cfg.AdminGroups) == 0 {
		cfg.AdminGroups = []string{"admin"}
	}

	app := &App{}

	boot, err := cfg.Bootstrap(ctx, "frontend")
	if err != nil {
		return nil, err
	}
	app.Boot = boot

	// OIDC is optional: configured issuer means login-required mode,
	// empty means open mode (local dev, tests).
	var oc *oidc.Client
	if cfg.OIDCIssuer != "" {
		key, err := base64.StdEncoding.DecodeString(cfg.SessionKeyB64)
		if err != nil || len(key) != 32 {
			app.shutdown(context.Background())
			return nil, fmt.Errorf("decode AUTH_SESSION_KEY (want base64 32B): %w", err)
		}
		var sk [32]byte
		copy(sk[:], key)
		oc, err = oidc.NewClient(ctx, oidc.Config{
			IssuerURL:    cfg.OIDCIssuer,
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSec,
			RedirectURL:  cfg.OIDCRedirect,
			SessionKey:   sk[:],
		})
		if err != nil {
			app.shutdown(context.Background())
			return nil, fmt.Errorf("oidc init: %w", err)
		}
		log.InfoFmt("OIDC login enabled (issuer %s)", cfg.OIDCIssuer)
	} else {
		log.InfoFmt("OIDC not configured: running in open mode")
	}
	app.oidc = oc

	reg := metrics.Init("frontend")
	extract.SetMetrics(&extract.Metrics{
		FetchTotal: reg.Counter("extract_total", "frontend article extractions", "outcome"),
		FetchSecs:  reg.Histogram("extract_duration_seconds", "frontend article extract latency", nil, "outcome"),
	})
	proxyHTTP := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	deps := handlers.Deps{
		Items:         handlers.AggClient(cfg.AggregatorURL, proxyHTTP),
		OIDC:          oc,
		AuthEnabled:   oc != nil,
		HTTP:          proxyHTTP,
		Extractor:     extract.Fetch,
		AggregatorURL: cfg.AggregatorURL,
		OutboxURL:     cfg.OutboxURL,
		PollerURL:     cfg.PollerURL,
		NotifyURL:     cfg.NotifyURL,
		ConfigURL:     cfg.ConfigURL,
		ConfigSecret:  cfg.ConfigSecret,
		AdminGroups:   cfg.AdminGroups,
	}

	mux := http.NewServeMux()
	handlers.Register(mux, deps)

	srv, err := service.ServeHTTP(cfg.HTTPAddr, mux, "rss_frontend", reg.Handler())
	if err != nil {
		app.shutdown(context.Background())
		return nil, fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	app.HTTP = srv

	return app, nil
}

// Shutdown stops all components and returns the joined error.
func (a *App) Shutdown(ctx context.Context) error {
	return a.BaseApp.Shutdown(ctx)
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.BaseApp.Shutdown(ctx)
}
