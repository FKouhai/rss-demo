package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-outbox/cmd"
	"github.com/FKouhai/rss-outbox/store"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// shutdownGrace bounds how long Shutdown may take (OTEL flush + HTTP drain).
const shutdownGrace = 2 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := cmd.Config{
		ServiceFQDN: os.Getenv("SERVICE_FQDN"),
		HTTPAddr:    envOr("OUTBOX_HTTP_ADDR", ":3000"),
		SkipTracer:  os.Getenv("OUTBOX_SKIP_TRACER") == "1",
		NATSURL:     os.Getenv("NATS_URL"),
		// Local/compose: give the broker time to appear (DNS + boot).
		// Tests leave it unset for an instant degraded boot.
		NATSRetryBudget: envDuration("OUTBOX_NATS_RETRY_BUDGET", 0),
		DBDriver:        store.Driver(os.Getenv("OUTBOX_DB_DRIVER")),
		DBDSN:           os.Getenv("OUTBOX_DB_DSN"),
	}

	app, err := cmd.Run(ctx, cfg)
	if err != nil {
		log.ErrorFmt("run: %v", err)
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
