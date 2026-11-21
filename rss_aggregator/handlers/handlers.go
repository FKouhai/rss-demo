package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/FKouhai/rss-aggregator/store"
	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/otel/attribute"
)

var db store.Store

// tracer is the service-scoped helper for aggregator spans.
var tracer = instrumentation.ForService("aggregator")

// SetStore simply sets the db driver to use
func SetStore(s store.Store) { db = s }

// HealthzHandler healthz endpoint
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.HealthzHandler")
	defer span.End()
	log.InfoCtx(r.Context(), "connection to /healthz established")
	w.WriteHeader(http.StatusOK)
	span.SetAttributes(attribute.Int("http.status_code", http.StatusOK))
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "healthy"}); err != nil {
		log.ErrorFmtCtx(r.Context(), "failed to encode healthz response: %v", err)
	}
}

// ReadyHandler readiness endpoint
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, span := tracer.StartServer(r.Context(), "handlers.ReadyHandler")
	defer span.End()
	log.InfoCtx(r.Context(), "connection to /ready established")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ready"}); err != nil {
		log.ErrorFmtCtx(r.Context(), "failed to encode ready response: %v", err)
	}
}

// itemJSON is the wire shape the frontend's aggclient decodes:
// {"id","feed_url","title","link","published_at"}.
type itemJSON struct {
	ID          string `json:"id"`
	FeedURL     string `json:"feed_url"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	PublishedAt string `json:"published_at"`
}

func toJSON(i store.Item) itemJSON {
	return itemJSON{ID: i.ID, FeedURL: i.FeedURL, Title: i.Title, Link: i.Link, PublishedAt: i.PublishedAt}
}

// ItemsHandler serves GET /api/items?q=&feed=&limit=&offset=.
func ItemsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, span := tracer.StartServer(r.Context(), "handlers.ItemsHandler")
	defer span.End()

	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	feed := r.URL.Query().Get("feed")
	span.SetAttributes(attribute.Int("items.limit", limit), attribute.Int("items.offset", offset))

	items, total, err := db.List(r.Context(), q, feed, limit, offset)
	if err != nil {
		instrumentation.Fail(span, err, "items list: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
		return
	}
	out := make([]itemJSON, 0, len(items))
	for _, it := range items {
		out = append(out, toJSON(it))
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": out, "total": total})
}

// FeedsHandler serves GET /api/feeds.
func FeedsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, span := tracer.StartServer(r.Context(), "handlers.FeedsHandler")
	defer span.End()

	feeds, err := db.Feeds(r.Context())
	if err != nil {
		instrumentation.Fail(span, err, "feeds list: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"feeds": feeds})
}

// ItemHandler serves GET /api/items/{id}.
func ItemHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, span := tracer.StartServer(r.Context(), "handlers.ItemHandler")
	defer span.End()

	id := r.PathValue("id")
	span.SetAttributes(attribute.String("item.id", id))

	item, err := db.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
			return
		}
		instrumentation.Fail(span, err, "item get: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
		return
	}

	_ = json.NewEncoder(w).Encode(toJSON(item))
}
