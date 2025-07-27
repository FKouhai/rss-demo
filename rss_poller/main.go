package main

import (
	"context"
	"net/http"

	"github.com/FKouhai/rss-poller/handlers"
	"github.com/FKouhai/rss-poller/instrumentation"
	log "github.com/FKouhai/rss-poller/logger"
)

func main() {
	tp, err := instrumentation.InitTracer()
	if err != nil {
		log.Error(err.Error())
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Debug(err.Error())
		}
	}()
	http.HandleFunc("/", handlers.RootHandler)
	http.HandleFunc("/config", handlers.ConfigHandler)
	http.HandleFunc("/healthz", handlers.HealthzHandler)
	http.HandleFunc("/rss", handlers.RSSHandler)
	http.ListenAndServe(":3000", nil)
}
