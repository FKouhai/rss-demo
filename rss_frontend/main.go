package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FKouhai/rss-frontend/cmd"
)

// shutdownGrace bounds how long Shutdown may take (OTEL flush + HTTP drain).
const shutdownGrace = 2 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := cmd.Config{
		ServiceFQDN:   os.Getenv("SERVICE_FQDN"),
		HTTPAddr:      envOr("FRONTEND_HTTP_ADDR", ":4321"),
		SkipTracer:    os.Getenv("FRONTEND_SKIP_TRACER") == "1",
		AggregatorURL: envOr("AGGREGATOR_URL", "http://rss_aggregator:3000"),
		OutboxURL:     envOr("OUTBOX_URL", "http://rss_outbox:3000"),
		PollerURL:     envOr("POLLER_URL", "http://rss_poller:3000"),
		NotifyURL:     envOr("NOTIFY_URL", "http://rss_notify:3000"),
		OIDCIssuer:    os.Getenv("OIDC_ISSUER_URL"),
		OIDCClientID:  os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSec: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirect:  os.Getenv("OIDC_REDIRECT_URL"),
		SessionKeyB64: os.Getenv("AUTH_SESSION_KEY"),
	}

	app, err := cmd.Run(ctx, cfg)
	if err != nil {
		os.Exit(1)
	}

	<-ctx.Done()
	// Give the exporter a brief grace window before shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
