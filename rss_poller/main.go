package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FKouhai/rss-poller/cmd"
)

// shutdownGrace bounds how long Shutdown may take (OTEL flush + HTTP drain).
const shutdownGrace = 2 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := cmd.Config{
		ServiceFQDN: os.Getenv("SERVICE_FQDN"),
		HTTPAddr:    envOr("POLLER_HTTP_ADDR", ":3000"),
		SkipTracer:  os.Getenv("POLLER_SKIP_TRACER") == "1",
		NATSURL:     os.Getenv("NATS_URL"),
		// Local/compose: give the broker time to appear (DNS + boot).
		// Tests leave it unset for an instant degraded boot.
		NATSRetryBudget: envDuration("POLLER_NATS_RETRY_BUDGET", 0),
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

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
