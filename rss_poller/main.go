package main

import (
	"context"
	"net/http"
	"time"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-poller/handlers"
)

func main() {
	tp, err := instrumentation.InitTracer("poller")
	if err != nil {
		log.Error(err.Error())
	}
	defer func() {
		// Add a small delay to ensure traces are flushed before shutdown
		time.Sleep(2 * time.Second)
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Debug(err.Error())
		}
	}()
	http.HandleFunc("/config", handlers.ConfigHandler)
	http.HandleFunc("/healthz", handlers.HealthzHandler)
	http.HandleFunc("/rss", handlers.RSSHandler)
	log.InfoFmt("starting server on port %d", 3000)
	// nolint
	http.ListenAndServe(":3000", nil)
}
