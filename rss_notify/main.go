// rss notify service should be able to send to multiple destinations and one destination preferribly via webhook,
// whenever it receives a signal to trigger the event this service should then lookup its config and send the notification
// to the specified destinations either slack,discord
// whenever the endpoint receives a request from the rss-poller service it should fire up a notification
// the notification rate should be configurable since there could be the case that you have many rss sources and you might flood with notifications the end user

package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FKouhai/rss-notify/cmd"
)

// shutdownGrace bounds how long Shutdown may take (OTEL flush + HTTP drain).
const shutdownGrace = 2 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := cmd.Config{
		ServiceFQDN: os.Getenv("SERVICE_FQDN"),
		HTTPAddr:    envOr("NOTIFY_HTTP_ADDR", ":3000"),
		SkipTracer:  os.Getenv("NOTIFY_SKIP_TRACER") == "1",
		NATSURL:     os.Getenv("NATS_URL"),
		MaxItemAge:  envDuration("NOTIFY_MAX_ITEM_AGE", 2*time.Hour),
		// Local/compose: give the broker time to appear (DNS + boot).
		// Tests leave it unset for an instant degraded boot.
		NATSRetryBudget: envDuration("NOTIFY_NATS_RETRY_BUDGET", 0),
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

// envMaxAge parses a duration env var; empty or unparseable falls back to
// def so a typo can't silently disable (or zero) the stale gate.
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
