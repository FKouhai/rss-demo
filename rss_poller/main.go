package main

import (
	"net/http"

	"github.com/FKouhai/rss-poller/handlers"
)

func main() {
	http.HandleFunc("/", handlers.RootHandler)
	http.HandleFunc("/config", handlers.ConfigHandler)
	http.HandleFunc("/healthz", handlers.HealthzHandler)
	http.HandleFunc("/rss", handlers.RSSHandler)
	http.ListenAndServe(":3000", nil)
}
