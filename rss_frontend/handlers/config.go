package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/FKouhai/rss-demo/libs/auth"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-frontend/views"
)

// configSnapshot is the rss_config GET /config shape the frontend
// decodes. It mirrors libs/config.Snapshot without importing it in
// the views layer (templates share views types, not lib types).
func (d Deps) configSnapshot(ctx context.Context) (views.ConfigData, bool) {
	var data views.ConfigData
	if d.ConfigURL == "" {
		return data, false
	}
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	fetch, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(fetch, http.MethodGet, d.ConfigURL+"/config", nil)
	if err != nil {
		return data, false
	}
	if d.ConfigSecret != "" {
		req.Header.Set("X-Gateway-Secret", d.ConfigSecret)
	}
	req.Header.Set("X-Tenant-ID", "default")
	resp, err := client.Do(req)
	if err != nil {
		log.ErrorFmt("config snapshot: %v", err)
		return data, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		log.ErrorFmt("config snapshot: status %s", resp.Status)
		return data, false
	}
	var snap struct {
		RSSFeeds   []string `json:"rss_feeds"`
		WebhookURL string   `json:"webhook_url"`
		Feeds      []struct {
			URL        string `json:"url"`
			WebhookURL string `json:"webhook_url"`
		} `json:"feeds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		log.ErrorFmt("config snapshot decode: %v", err)
		return data, false
	}
	overrides := map[string]string{}
	for _, f := range snap.Feeds {
		overrides[f.URL] = f.WebhookURL
	}
	for _, u := range snap.RSSFeeds {
		data.Feeds = append(data.Feeds, views.ConfigFeed{URL: u, WebhookURL: overrides[u]})
	}
	data.DefaultWebhook = snap.WebhookURL
	return data, true
}

// isAdmin gates config mutations. Open mode (no OIDC) allows
// everything; otherwise the session's groups must intersect AdminGroups
// (CONFIG_ADMIN_GROUPS, default "admin").
func (d Deps) isAdmin(r *http.Request) bool {
	if !d.AuthEnabled {
		return true
	}
	id, ok := auth.FromContext(r.Context())
	if !ok {
		return false
	}
	for _, g := range id.Groups {
		if slices.Contains(d.AdminGroups, g) {
			return true
		}
	}
	return false
}

// proxyConfig forwards a mutation to rss_config with the service
// credential plus the caller's identity (the config service enforces
// its own admin check; the frontend pre-checks for a clean 403 page).
func (d Deps) proxyConfig(r *http.Request, method, path string, payload any) (int, []byte) {
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	var buf bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			return 0, nil
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, d.ConfigURL+path, &buf)
	if err != nil {
		return 0, nil
	}
	if d.ConfigSecret != "" {
		req.Header.Set("X-Gateway-Secret", d.ConfigSecret)
	}
	// Propagate the OIDC session identity; open mode sends machine only.
	if id, ok := auth.FromContext(r.Context()); ok {
		if id.User != "" {
			req.Header.Set("X-Forwarded-User", id.User)
		}
		if id.Email != "" {
			req.Header.Set("X-Forwarded-Email", id.Email)
		}
		for _, g := range id.Groups {
			if prev := req.Header.Get("X-Forwarded-Groups"); prev == "" {
				req.Header.Set("X-Forwarded-Groups", g)
			} else {
				req.Header.Set("X-Forwarded-Groups", prev+","+g)
			}
		}
		req.Header.Set("X-Tenant-ID", id.TenantID)
	} else {
		req.Header.Set("X-Tenant-ID", "default")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// renderConfigTable re-renders the managed feed table after a mutation.
// Failures degrade to the full page (the next GET /config shows truth).
func (d Deps) renderConfigTable(w http.ResponseWriter, r *http.Request, spanCtx context.Context) {
	data, ok := d.configSnapshot(spanCtx)
	if !ok {
		http.Error(w, "config unavailable", http.StatusBadGateway)
		return
	}
	renderErr(nil, views.ConfigTable(views.ConfigParams{
		User: userLabel(r), AuthEnabled: d.AuthEnabled,
		ConfigOK: true, IsAdmin: d.isAdmin(r),
		ConfigFeeds: data.Feeds, DefaultWebhook: data.DefaultWebhook,
	}).Render(spanCtx, w))
}

func (d Deps) configHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.StartServer(r.Context(), "handlers.configHandler")
	defer span.End()
	data, configOK := d.configSnapshot(ctx)
	feeds, aggOK := []string(nil), false
	if !configOK {
		// Config unreachable: fall back to the aggregator's observed
		// feeds (previous behavior) so the page stays useful.
		var err error
		feeds, err = d.Items.Feeds(ctx)
		aggOK = err == nil
		if err != nil {
			log.ErrorFmt("aggregator feeds: %v", err)
			feeds = nil
		}
	}
	renderErr(span, views.Config(views.ConfigParams{
		Title: "Configure", User: userLabel(r), AuthEnabled: d.AuthEnabled,
		Feeds: feeds, AggOK: aggOK,
		ConfigOK: configOK, IsAdmin: d.isAdmin(r),
		ConfigFeeds: data.Feeds, DefaultWebhook: data.DefaultWebhook,
	}).Render(ctx, w))
}

// configAddHandler proxies POST /config/feeds (add or update a feed).
// HTMX posts the row form; the updated table fragment is the response.
func (d Deps) configAddHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.configAddHandler")
	defer span.End()
	if !d.isAdmin(r) {
		http.Error(w, "admin group required", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	feedURL := r.FormValue("url")
	if feedURL == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	if u, err := url.Parse(feedURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "url must be an http(s) URL", http.StatusBadRequest)
		return
	}
	code, _ := d.proxyConfig(r, http.MethodPost, "/feeds", map[string]string{
		"url": feedURL, "webhook_url": r.FormValue("webhook_url"),
	})
	if code == 0 {
		http.Error(w, "config unavailable", http.StatusBadGateway)
		return
	}
	if code != http.StatusOK {
		http.Error(w, "config rejected the feed", code)
		return
	}
	d.renderConfigTable(w, r, r.Context())
}

// configDeleteHandler proxies DELETE /feeds?url= (hx-delete per row).
func (d Deps) configDeleteHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.configDeleteHandler")
	defer span.End()
	if !d.isAdmin(r) {
		http.Error(w, "admin group required", http.StatusForbidden)
		return
	}
	raw := r.URL.Query().Get("url")
	if raw == "" {
		http.Error(w, "missing ?url=", http.StatusBadRequest)
		return
	}
	code, _ := d.proxyConfig(r, http.MethodDelete, "/feeds?url="+url.QueryEscape(raw), nil)
	if code == 0 {
		http.Error(w, "config unavailable", http.StatusBadGateway)
		return
	}
	if code != http.StatusOK {
		http.Error(w, "config rejected the delete", code)
		return
	}
	d.renderConfigTable(w, r, r.Context())
}

// configDefaultHandler proxies the global default webhook form.
func (d Deps) configDefaultHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.configDefaultHandler")
	defer span.End()
	if !d.isAdmin(r) {
		http.Error(w, "admin group required", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	code, _ := d.proxyConfig(r, http.MethodPut, "/config/default-webhook", map[string]string{
		"webhook_url": r.FormValue("webhook_url"),
	})
	if code == 0 {
		http.Error(w, "config unavailable", http.StatusBadGateway)
		return
	}
	if code != http.StatusOK {
		http.Error(w, "config rejected the default", code)
		return
	}
	d.renderConfigTable(w, r, r.Context())
}
