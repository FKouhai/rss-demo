// rss notify service should be able to send to multiple destinations and one destination preferribly via webhook,
// whenever it receives a signal to trigger the event this service should then lookup its config and send the notification
// to the specified destinations either slack,discord
// whenever the endpoint receives a request from the rss-poller service it should fire up a notification
// the notification rate should be configurable since there could be the case that you have many rss sources and you might flood with notifications the end user

package main

import (
	"context"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/FKouhai/rss-notify/instrumentation"
	log "github.com/FKouhai/rss-notify/logger"
	"github.com/FKouhai/rss-notify/methods"
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
	m.HandleFunc("/push", methods.PushNotificationHandler)
	m.HandleFunc("/healthz", methods.HealthzHandler)

	go func(ctx context.Context, done context.CancelFunc) {
		serv := &http.Server{
			Addr:    "0.0.0.0:80",
			Handler: m,
			BaseContext: func(l net.Listener) context.Context {
				ctx = context.WithValue(ctx, methods.ServerAddr, l.Addr().String())
				return ctx
			},
			ConnContext: func(ctx context.Context, c net.Conn) context.Context {
				ctx = context.WithValue(ctx, methods.IncomingAddr, c.LocalAddr().String())
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
}
