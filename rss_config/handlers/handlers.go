package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"

	"github.com/FKouhai/rss-config/store"
	"github.com/FKouhai/rss-demo/libs/auth"
	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"go.opentelemetry.io/otel/attribute"
)

var db store.Store

// authEnforced flips on when a gateway secret is configured; without
// one the service runs open (local dev) and every caller is treated
// as admin. adminGroups gates the write endpoints when enforced.
var (
	authEnforced bool
	adminGroups  []string
)

// tracer is the service-scoped helper for config spans.
var tracer = instrumentation.ForService("config")

// SetStore simply sets the db driver to use
func SetStore(s store.Store) { db = s }

// SetAuth wires the write-path gate: enforced means requests arrive
// through the libs/auth header-trust middleware; groups lists the
// admin groups allowed to mutate config (CONFIG_ADMIN_GROUPS).
func SetAuth(enforced bool, groups []string) {
	authEnforced = enforced
	adminGroups = groups
}

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

// requireAdmin gates the mutating endpoints. In open mode (no gateway
// secret configured) it passes; otherwise the caller needs a group in
// adminGroups, carried by the header-trust middleware from the
// frontend's OIDC session.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !authEnforced {
		return true
	}
	id, ok := auth.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	for _, g := range id.Groups {
		if slices.Contains(adminGroups, g) {
			return true
		}
	}
	writeErr(w, http.StatusForbidden, "admin group required")
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.ErrorFmt("failed to encode response: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// validFeedURL rejects empty and non-http(s) URLs before they reach
// the store; the poller would skip them anyway, but failing fast
// tells the admin what was wrong.
func validFeedURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// ConfigHandler serves GET /config: the poller snapshot. Webhook URLs
// are returned plain (v1 call); counts — never values — go to logs.
func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, span := tracer.StartServer(r.Context(), "handlers.ConfigHandler")
	defer span.End()
	snap, err := db.Snapshot(ctx)
	if err != nil {
		instrumentation.Fail(span, err, "snapshot: %v", err)
		writeErr(w, http.StatusInternalServerError, "snapshot failed")
		return
	}
	span.SetAttributes(attribute.Int("feeds.count", len(snap.RSSFeeds)))
	writeJSON(w, http.StatusOK, snap)
}

// FeedsHandler serves GET /feeds (list) and POST /feeds (upsert).
func FeedsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ctx, span := tracer.StartServer(r.Context(), "handlers.FeedsList")
		defer span.End()
		feeds, err := db.ListFeeds(ctx)
		if err != nil {
			instrumentation.Fail(span, err, "list feeds: %v", err)
			writeErr(w, http.StatusInternalServerError, "list failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"feeds": feeds})
	case http.MethodPost:
		if !requireAdmin(w, r) {
			return
		}
		ctx, span := tracer.StartServer(r.Context(), "handlers.FeedUpsert")
		defer span.End()
		var body struct {
			URL        string `json:"url"`
			WebhookURL string `json:"webhook_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if !validFeedURL(body.URL) {
			writeErr(w, http.StatusBadRequest, "url must be an http(s) URL")
			return
		}
		if err := db.UpsertFeed(ctx, body.URL, body.WebhookURL); err != nil {
			instrumentation.Fail(span, err, "upsert feed: %v", err)
			writeErr(w, http.StatusInternalServerError, "upsert failed")
			return
		}
		span.SetAttributes(attribute.String("feed.url", body.URL))
		writeJSON(w, http.StatusOK, store.Feed{URL: body.URL, WebhookURL: body.WebhookURL, Active: true})
	case http.MethodDelete:
		if !requireAdmin(w, r) {
			return
		}
		ctx, span := tracer.StartServer(r.Context(), "handlers.FeedDelete")
		defer span.End()
		raw := r.URL.Query().Get("url")
		if raw == "" {
			writeErr(w, http.StatusBadRequest, "missing ?url=")
			return
		}
		deleted, err := db.DeleteFeed(ctx, raw)
		if err != nil {
			instrumentation.Fail(span, err, "delete feed: %v", err)
			writeErr(w, http.StatusInternalServerError, "delete failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// DefaultWebhookHandler serves PUT /config/default-webhook: replace
// (or clear, with "") the global default.
func DefaultWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !requireAdmin(w, r) {
		return
	}
	ctx, span := tracer.StartServer(r.Context(), "handlers.DefaultWebhook")
	defer span.End()
	var body struct {
		WebhookURL string `json:"webhook_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := db.SetDefaultWebhook(ctx, body.WebhookURL); err != nil {
		instrumentation.Fail(span, err, "set default webhook: %v", err)
		writeErr(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"webhook_url": body.WebhookURL})
}

// hookUpdate is the inbound push shape from rss_frontend (admin users):
// the same snapshot the poller pulls, applied as a merge.
type hookUpdate struct {
	RSSFeeds   []string `json:"rss_feeds"`
	WebhookURL *string  `json:"webhook_url"`
	Feeds      []struct {
		URL        string `json:"url"`
		WebhookURL string `json:"webhook_url"`
	} `json:"feeds"`
}

// HookUpdateHandler serves POST /hooks/update. Merge, not replace:
// legacy URLs only ensure presence (never clobber an override),
// explicit entries upsert, and the default changes only when the key
// is present. All-or-nothing per request: validation runs before any
// write, so a bad URL can't leave a half-applied push.
func HookUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !requireAdmin(w, r) {
		return
	}
	ctx, span := tracer.StartServer(r.Context(), "handlers.HookUpdate")
	defer span.End()
	var body hookUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	for _, u := range body.RSSFeeds {
		if !validFeedURL(u) {
			writeErr(w, http.StatusBadRequest, "invalid feed url")
			return
		}
	}
	for _, f := range body.Feeds {
		if !validFeedURL(f.URL) {
			writeErr(w, http.StatusBadRequest, "invalid feed url")
			return
		}
	}
	for _, u := range body.RSSFeeds {
		if err := db.EnsureFeed(ctx, u); err != nil {
			instrumentation.Fail(span, err, "ensure feed: %v", err)
			writeErr(w, http.StatusInternalServerError, "apply failed")
			return
		}
	}
	for _, f := range body.Feeds {
		if err := db.UpsertFeed(ctx, f.URL, f.WebhookURL); err != nil {
			instrumentation.Fail(span, err, "upsert feed: %v", err)
			writeErr(w, http.StatusInternalServerError, "apply failed")
			return
		}
	}
	defaultUpdated := false
	if body.WebhookURL != nil {
		if err := db.SetDefaultWebhook(ctx, *body.WebhookURL); err != nil {
			instrumentation.Fail(span, err, "set default webhook: %v", err)
			writeErr(w, http.StatusInternalServerError, "apply failed")
			return
		}
		defaultUpdated = true
	}
	span.SetAttributes(
		attribute.Int("feeds.applied", len(body.RSSFeeds)+len(body.Feeds)),
		attribute.Bool("default.updated", defaultUpdated),
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"applied":         map[string]int{"feeds": len(body.RSSFeeds) + len(body.Feeds)},
		"default_updated": defaultUpdated,
	})
}
