// rss notify service should be able to send to multiple destinations and one destination preferribly via webhook,
// whenever it receives a signal to trigger the event this service should then lookup its config and send the notification
// to the specified destinations either slack,discord
// whenever the endpoint receives a request from the rss-poller service it should fire up a notification
// the notification rate should be configurable since there could be the case that you have many rss sources and you might flood with notifications the end user

package main

import (
	"context"
	"net/http"

	"github.com/FKouhai/rss-notify/instrumentation"
	log "github.com/FKouhai/rss-notify/logger"
	"github.com/FKouhai/rss-notify/methods"
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
	http.HandleFunc("/push", methods.PushNotificationHandler)
	http.HandleFunc("/healthz", methods.HealthzHandler)
	log.InfoFmt("starting server on port %d", 3001)
	// nolint
	http.ListenAndServe(":3001", nil)
}
