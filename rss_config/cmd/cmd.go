// Package cmd is the in-process entrypoint for rss_config.
//
// Run returns an *App whose Ready channel fires once the HTTP listener
// is bound, and whose Shutdown gracefully stops the store and the
// tracer. main.go is a thin wrapper around Run; integration tests call
// Run directly so the service runs in-process (as goroutines) with
// deterministic readiness.
//
// The config service is the tenant anchor and the poller's source of
// truth: it persists feed sources + the webhook mapping (DB-backed, so
// restarts lose nothing) and serves the poller's GET /config snapshot.
// It is HTTP-only — no NATS in v1 — and the frontend pushes admin
// changes through POST /hooks/update.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/FKouhai/rss-config/handlers"
	"github.com/FKouhai/rss-config/store"
	"github.com/FKouhai/rss-demo/libs/auth"
	db "github.com/FKouhai/rss-demo/libs/db"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-demo/libs/service"
)

// Config configures a config instance. Env-derived in main.go;
// integration tests construct it directly.
type Config struct {
	service.BaseConfig
	DBDriver db.Driver
	DBDSN    string
	// SeedFile is the poller-shaped JSON imported exactly once when
	// the store is empty (first boot). Empty disables seeding.
	SeedFile string
	// GatewaySecret enables the header-trust middleware. Empty means
	// open mode (local dev): every caller reads and admin checks pass.
	GatewaySecret string
	// AdminGroups lists the OIDC groups allowed to mutate config
	// (frontend forwards them via headers). Default "admin".
	AdminGroups []string
}

// App is a running config instance. Close it with Shutdown.
type App struct {
	service.BaseApp
	store store.Store
}

// Run starts the config service with the given config and returns once
// the HTTP listener is bound. It does not block for the lifetime of
// the server; callers wait on App.Ready() and later call App.Shutdown().
func Run(ctx context.Context, cfg Config) (*App, error) {
	cfg.WithDefaults("config")
	if cfg.DBDriver == "" {
		cfg.DBDriver = db.DriverSQLite
	}
	if cfg.DBDSN == "" {
		cfg.DBDSN = "/var/lib/rss-config/config.db"
	}
	if len(cfg.AdminGroups) == 0 {
		cfg.AdminGroups = []string{"admin"}
	}

	app := &App{}

	boot, err := cfg.Bootstrap(ctx, "config")
	if err != nil {
		return nil, err
	}
	app.Boot = boot

	s, err := store.NewConfig(store.Config{Driver: cfg.DBDriver, DSN: cfg.DBDSN})
	if err != nil {
		app.shutdown(context.Background())
		return nil, fmt.Errorf("store init: %w", err)
	}
	app.store = s
	log.InfoFmt("Config store initialized (driver=%s)", cfg.DBDriver)
	handlers.SetStore(s)
	handlers.SetAuth(cfg.GatewaySecret != "", cfg.AdminGroups)

	if cfg.GatewaySecret == "" {
		log.InfoFmt("CONFIG_GATEWAY_SECRET unset: running in open mode")
	}

	// Seed-once: an empty store imports the seed file so first boot
	// (fresh sqlite) starts from the same feeds as the file-based
	// poller. Afterwards the DB is authoritative; the file is only
	// the poller's fallback when config is unreachable.
	if cfg.SeedFile != "" {
		if err := seedOnce(ctx, s, cfg.SeedFile); err != nil {
			app.shutdown(context.Background())
			return nil, fmt.Errorf("seed: %w", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handlers.HealthzHandler)
	mux.HandleFunc("/ready", handlers.ReadyHandler)
	mux.HandleFunc("/config", handlers.ConfigHandler)
	mux.HandleFunc("/feeds", handlers.FeedsHandler)
	mux.HandleFunc("/config/default-webhook", handlers.DefaultWebhookHandler)
	mux.HandleFunc("/hooks/update", handlers.HookUpdateHandler)

	// Open mode: no secret configured (local dev) skips header-trust
	// and the admin check; the middleware would otherwise fail closed.
	open := cfg.GatewaySecret == ""
	handler := auth.Middleware(auth.Config{GatewaySecret: cfg.GatewaySecret, SkipAuth: open}, mux)

	srv, err := service.ServeHTTP(cfg.HTTPAddr, handler, "rss_config")
	if err != nil {
		app.shutdown(context.Background())
		return nil, fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	app.HTTP = srv

	return app, nil
}

// Store exposes the store for integration tests that seed/observe rows.
func (a *App) Store() store.Store { return a.store }

// Shutdown stops all components and returns the joined error.
func (a *App) Shutdown(ctx context.Context) error {
	var errs []error
	if a.store != nil {
		errs = append(errs, a.store.Close())
	}
	errs = append(errs, a.BaseApp.Shutdown(ctx))
	return errors.Join(errs...)
}

func (a *App) shutdown(ctx context.Context) {
	if a.store != nil {
		_ = a.store.Close()
	}
	_ = a.BaseApp.Shutdown(ctx)
}

// seedFile is the poller-shaped JSON accepted as seed input.
type seedFile struct {
	RSSFeeds   []string `json:"rss_feeds"`
	WebhookURL string   `json:"webhook_url"`
	Feeds      []struct {
		URL        string `json:"url"`
		WebhookURL string `json:"webhook_url"`
	} `json:"feeds"`
}

// seedOnce imports the seed file when the store holds no feeds.
// Missing file is not an error (unseeded boot serves empty config).
func seedOnce(ctx context.Context, s store.Store, path string) error {
	empty, err := s.IsEmpty(ctx)
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.InfoFmt("no seed file at %s, starting empty", path)
			return nil
		}
		return err
	}
	var seed seedFile
	if err := json.Unmarshal(data, &seed); err != nil {
		return fmt.Errorf("parse seed %s: %w", path, err)
	}
	for _, u := range seed.RSSFeeds {
		if strings.TrimSpace(u) == "" {
			continue
		}
		if err := s.EnsureFeed(ctx, u); err != nil {
			return err
		}
	}
	for _, f := range seed.Feeds {
		if strings.TrimSpace(f.URL) == "" {
			continue
		}
		if err := s.UpsertFeed(ctx, f.URL, f.WebhookURL); err != nil {
			return err
		}
	}
	if seed.WebhookURL != "" {
		if err := s.SetDefaultWebhook(ctx, seed.WebhookURL); err != nil {
			return err
		}
	}
	log.InfoFmt("seeded config from %s", path)
	return nil
}
