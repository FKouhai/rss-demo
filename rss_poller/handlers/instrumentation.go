package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	"go.opentelemetry.io/otel/trace"
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
	sources := make([]FeedSource, len(cfg.Feeds))
	copy(sources, cfg.Feeds)
	return ConfigStruct{RSSFeeds: feeds, WebhookURL: cfg.WebhookURL, Feeds: sources}
}

// writeJSON writes status and JSON-encodes body to w, recording any
// encoding error on the caller's span (the status line is already
// committed, so the trace is the only place the failure surfaces).
func writeJSON(w http.ResponseWriter, span trace.Span, status int, body any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		instrumentation.Fail(span, err, "failed to encode response: %v", err)
	}
}
