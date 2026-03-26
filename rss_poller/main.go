package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/FKouhai/rss-demo/libs/bootstrap"
	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-poller/handlers"
)

func init() {
	if err := bootstrap.WaitForLocator(); err != nil {
		log.ErrorFmt("Locator service not available: %v", err)
		return
	}

	serviceFQDN := os.Getenv("SERVICE_FQDN")
	if serviceFQDN == "" {
		serviceFQDN = "poller:3000"
	}

	if err := bootstrap.Init("poller", serviceFQDN); err != nil {
		log.ErrorFmt("Failed to register poller with locator: %v", err)
	}
}

func discoverNotifyService() {
	notifyFQDN, err := bootstrap.GetServiceFQDN("notify")
	if err != nil {
		log.ErrorFmt("Failed to discover notify service. Falling back to NOTIFICATION_SENDER environment variable. Error: %v", err)
		notificationServiceURL := os.Getenv("NOTIFICATION_SENDER")
		handlers.SetNotificationServiceURL(notificationServiceURL)
		if notificationServiceURL != "" {
			log.Info("Using NOTIFICATION_SENDER from environment as fallback")
		} else {
			log.Error("NOTIFICATION_SENDER environment variable not set. Notifications will not be sent.")
		}
		return
	}

	notificationServiceURL := fmt.Sprintf("%s/push", notifyFQDN)
	handlers.SetNotificationServiceURL(notificationServiceURL)
	log.InfoFmt("Discovered notify service at: %s", notificationServiceURL)
}

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

	// Discover notify service in the background to give it time to register
	go func() {
		time.Sleep(5 * time.Second)
		discoverNotifyService()
	}()

	http.HandleFunc("/config", handlers.ConfigHandler)
	http.HandleFunc("/healthz", handlers.HealthzHandler)
	http.HandleFunc("/ready", handlers.ReadyHandler)
	http.HandleFunc("/rss", handlers.RSSHandler)
	log.InfoFmt("starting server on port %d", 3000)
	// nolint
	http.ListenAndServe(":3000", nil)
}
