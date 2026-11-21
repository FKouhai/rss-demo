package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/FKouhai/rss-demo/libs/auth/oidc"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-frontend/internal/extract"
	"github.com/FKouhai/rss-frontend/views"
)

// ExtractorFunc fetches an article for the reader view. It is a field
// (not a direct call) so tests can stub the network.
type ExtractorFunc func(ctx context.Context, rawURL string) (extract.Article, error)

// Deps wires a handler set. OIDC nil means open mode.
type Deps struct {
	Items         ItemStore
	OIDC          *oidc.Client
	AuthEnabled   bool
	HTTP          *http.Client
	Extractor     ExtractorFunc
	AggregatorURL string
	OutboxURL     string
	PollerURL     string
	NotifyURL     string
}

// Register mounts all routes. Everything except health/readiness,
// static assets, and the login endpoints requires a session when
// AuthEnabled.
func Register(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /healthz", HealthzHandler)
	mux.HandleFunc("GET /ready", ReadyHandler)
	mux.Handle("GET /static/", staticHandler())

	guard := func(h http.Handler) http.Handler {
		if d.AuthEnabled && d.OIDC != nil {
			return d.OIDC.SessionMiddleware(h)
		}
		return h
	}

	mux.Handle("GET /", guard(http.HandlerFunc(d.frontHandler)))
	mux.Handle("GET /partials/items", guard(http.HandlerFunc(d.itemsPartial)))
	mux.Handle("GET /items/{id}", guard(http.HandlerFunc(d.articleHandler)))
	mux.Handle("GET /status", guard(http.HandlerFunc(d.statusHandler)))
	mux.Handle("GET /partials/status", guard(http.HandlerFunc(d.statusPartial)))
	mux.Handle("GET /config", guard(http.HandlerFunc(d.configHandler)))
	mux.Handle("GET /img", guard(http.HandlerFunc(d.imgHandler)))

	if d.AuthEnabled && d.OIDC != nil {
		mux.HandleFunc("GET /auth/login", d.loginHandler)
		mux.HandleFunc("GET /auth/callback", d.callbackHandler)
		mux.HandleFunc("GET /auth/logout", d.logoutHandler)
	}
}

// HealthzHandler reports liveness.
func HealthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"healthy"}`)) //nolint:errcheck
}

// ReadyHandler reports readiness. Serving HTTP means the frontend is up;
// the aggregator is best-effort (pages degrade to an unavailable notice).
func ReadyHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ready"}`)) //nolint:errcheck
}

const defaultPerPage = 16

func pageParams(r *http.Request) (q, feed string, page, perPage int) {
	qs := r.URL.Query()
	q = qs.Get("q")
	feed = qs.Get("feed")
	page, _ = strconv.Atoi(qs.Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ = strconv.Atoi(qs.Get("per_page"))
	if perPage != 16 && perPage != 32 && perPage != 64 {
		perPage = defaultPerPage
	}
	return q, feed, page, perPage
}

// renderErr logs template write errors. By the time Render fails the
// status line is committed, so there is nothing useful left to send.
func renderErr(err error) {
	if err != nil {
		log.ErrorFmt("render: %v", err)
	}
}

func (d Deps) frontHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	q, feed, page, perPage := pageParams(r)
	ctx := r.Context()
	items, total, aggOK := d.listGraceful(ctx, q, feed, perPage, (page-1)*perPage)
	var feeds []string
	if aggOK {
		feeds, _ = d.Items.Feeds(ctx)
	}
	user := userLabel(r)
	renderErr(views.Front(views.FrontParams{
		Title: "The RSS Gazette", User: user, AuthEnabled: d.AuthEnabled,
		Items: items, Total: total, Q: q, Feed: feed, Feeds: feeds,
		Page: page, PerPage: perPage, AggOK: aggOK,
	}).Render(ctx, w))
}

func (d Deps) itemsPartial(w http.ResponseWriter, r *http.Request) {
	q, feed, page, perPage := pageParams(r)
	ctx := r.Context()
	items, total, aggOK := d.listGraceful(ctx, q, feed, perPage, (page-1)*perPage)
	renderErr(views.ItemsList(views.FrontParams{
		Items: items, Total: total, Q: q, Feed: feed,
		Page: page, PerPage: perPage, AggOK: aggOK,
	}).Render(ctx, w))
}

// listGraceful degrades to an empty list when the aggregator is down
// instead of failing the page.
func (d Deps) listGraceful(ctx context.Context, q, feed string, limit, offset int) ([]views.Item, int, bool) {
	items, total, err := d.Items.List(ctx, q, feed, limit, offset)
	if err != nil {
		log.ErrorFmt("aggregator list: %v", err)
		return nil, 0, false
	}
	return items, total, true
}

func (d Deps) articleHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	item, err := d.Items.Get(ctx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.ErrorFmt("aggregator get: %v", err)
		http.Error(w, "aggregator unavailable", http.StatusBadGateway)
		return
	}
	article, fetchErr := d.Extractor(ctx, item.Link)
	if fetchErr != nil {
		log.ErrorFmt("extract %s: %v", item.Link, fetchErr)
	}
	renderErr(views.Article(views.ArticleParams{
		Title: item.Title, User: userLabel(r), AuthEnabled: d.AuthEnabled,
		Item: item, Article: article,
		FetchErr: fetchErr != nil,
	}).Render(ctx, w))
}

func (d Deps) configHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	feeds, err := d.Items.Feeds(ctx)
	aggOK := err == nil
	if err != nil {
		log.ErrorFmt("aggregator feeds: %v", err)
		feeds = nil
	}
	renderErr(views.Config(views.ConfigParams{
		Title: "Configure", User: userLabel(r), AuthEnabled: d.AuthEnabled,
		Feeds: feeds, AggOK: aggOK,
	}).Render(ctx, w))
}

func (d Deps) imgHandler(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	if raw == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	body, ct, err := extract.FetchBytes(ctx, raw, 5<<20, "image/")
	if err != nil {
		log.ErrorFmt("img %s: %v", raw, err)
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(body) //nolint:errcheck
}
