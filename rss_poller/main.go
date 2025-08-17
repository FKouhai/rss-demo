package main

import (
	"context"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/FKouhai/rss-poller/handlers"
	"github.com/FKouhai/rss-poller/instrumentation"
	log "github.com/FKouhai/rss-poller/logger"
)

const port = 3000

func main() {
	ctx, done := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer done()

	tp, err := instrumentation.InitTracer(ctx)
	if err != nil {
		log.ErrorFmt("[ERROR] Unable to Init Tracer: %w", err)
	}
	defer func(ctx context.Context, cancel context.CancelFunc) {
		if err := tp.Shutdown(ctx); err != nil {
			log.Debug(err.Error())
		}
		cancel()
	}(ctx, done)

	log.InfoFmt("[INFO] Creating server and handler methods", port)

	m := http.NewServeMux()
	m.HandleFunc("/config", handlers.ConfigHandler)
	m.HandleFunc("/healthz", handlers.HealthzHandler)
	m.HandleFunc("/rss", handlers.RSSHandler)

	go func(ctx context.Context, done context.CancelFunc) {
		serv := &http.Server{
			Addr:    "0.0.0.0:80",
			Handler: m,
			BaseContext: func(l net.Listener) context.Context {
				ctx = context.WithValue(ctx, handlers.ServerAddr, l.Addr().String())
				return ctx
			},
			ConnContext: func(ctx context.Context, c net.Conn) context.Context {
				ctx = context.WithValue(ctx, handlers.IncomingAddr, c.LocalAddr().String())
				return ctx
			},
		}
		log.InfoFmt("[INFO] Server starting listening on port %d", port)
		if err := serv.ListenAndServe(); err != http.ErrServerClosed {
			log.ErrorFmt("[ERROR] Error running server: %w", err)
		}
		done()
	}(ctx, done)

	select {
	case <-ctx.Done():
		log.Info("Shutting down...")
		time.Sleep(1 * time.Second) /* Allow for connections to close */
	}
	log.InfoFmt("starting server on port %d", 3000)
}
