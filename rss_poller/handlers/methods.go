package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	log "github.com/FKouhai/rss-poller/logger"
	"github.com/mmcdole/gofeed"
)

// ConfigStruct contains the accepted config fields that this microservice will use
type ConfigStruct struct {
	RSSFeeds string `json:"rss_feeds"`
}

var cfg ConfigStruct

// RootHandler exposes the index api handler
func RootHandler(w http.ResponseWriter, r *http.Request) {
	log.Info("accepted connection")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("testing 1 2"))
}

// ConfigHandler reads the config sent via json and stores it in memory
func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	log.Info("accepted connection")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		log.Error("the wrong method was used")
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		log.Error("the request does not contain a JSON payload")
		return
	}
	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Error("Unexpected request content")
		return
	}

	jReader := strings.NewReader(string(body))
	err = json.NewDecoder(jReader).Decode(&cfg)
	if err != nil {
		log.Error(err.Error())
	}
	w.Write([]byte(cfg.RSSFeeds))
}

// HealthzHandler is the route that exposes a healthcheck
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	log.Info("connection to /health established")
	w.WriteHeader(http.StatusOK)
	status := map[string]string{"status": "healthy"}
	err := json.NewEncoder(w).Encode(status)
	if err != nil {
		log.Error(err.Error())
	}
}

// RSSHandler is the route that exposes the rss feeds that has been polled
func RSSHandler(w http.ResponseWriter, r *http.Request) {
	log.Info("connection to /rss established")
	feeds, err := ParseRSS(cfg.RSSFeeds)
	if err != nil {
		log.Debug(err.Error())
	}
	for _, v := range feeds.Items {
		w.Write([]byte(v.Title))
		w.Write([]byte(v.Description))
		w.Write([]byte(v.Content))
	}
}

// ParseRSS returns the rss feed with all its items
func ParseRSS(feedURL string) (*gofeed.Feed, error) {
	feedParser := gofeed.NewParser()
	feed, err := feedParser.ParseURL(feedURL)
	if err != nil {
		log.Debug(err.Error())
		return nil, err
	}
	log.Info("got feed")
	return feed, nil
}
