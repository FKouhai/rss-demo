package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
)

// tracer is the service-scoped helper for poller spans. It fixes the
// service name once; call sites only name the operation and pick a kind.
var tracer = instrumentation.ForService("poller")

// getConfigSnapshot returns a deep copy of the current config under the read lock.
// A shallow copy is not safe: the []string backing array would be shared between
// the snapshot and cfg, so a concurrent json.Decode into cfg could overwrite string
// slots while the snapshot's encoder is still reading them.
func getConfigSnapshot() ConfigStruct {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	feeds := make([]string, len(cfg.RSSFeeds))
	copy(feeds, cfg.RSSFeeds)
	return ConfigStruct{RSSFeeds: feeds}
}

// writeJSON writes status and JSON-encodes body to w, logging any encoding error.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.ErrorFmt("failed to encode response: %v", err)
	}
}
