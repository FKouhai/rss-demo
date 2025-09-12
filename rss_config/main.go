package main

import (
	"context"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
)

func main() {
	tp, err := instrumentation.InitTracer("config")
	if err != nil {
		log.Error(err.Error())
	}

	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Debug(err.Error())
		}
	}()

	log.InfoFmt("starting server on port %d", 3000)
	// nolint
	http.ListenAndServe(":3000", nil)
}
