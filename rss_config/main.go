package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/FKouhai/rss-config/cmd"
	db "github.com/FKouhai/rss-demo/libs/db"
	log "github.com/FKouhai/rss-demo/libs/logger"
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
		ServiceFQDN:   os.Getenv("SERVICE_FQDN"),
		HTTPAddr:      envOr("CONFIG_HTTP_ADDR", ":3000"),
		SkipTracer:    os.Getenv("CONFIG_SKIP_TRACER") == "1",
		DBDriver:      db.Driver(os.Getenv("CONFIG_DB_DRIVER")),
		DBDSN:         os.Getenv("CONFIG_DB_DSN"),
		SeedFile:      os.Getenv("CONFIG_SEED_FILE"),
		GatewaySecret: os.Getenv("CONFIG_GATEWAY_SECRET"),
		AdminGroups:   envList("CONFIG_ADMIN_GROUPS", "admin"),
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

// envList splits a comma-separated env var; empty means the default.
func envList(key, def string) []string {
	v := os.Getenv(key)
	if v == "" {
		v = def
	}
	var out []string
	for s := range strings.SplitSeq(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
