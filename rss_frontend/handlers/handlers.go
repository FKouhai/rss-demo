package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/FKouhai/rss-demo/libs/auth/oidc"
	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-frontend/internal/extract"
	"github.com/FKouhai/rss-frontend/views"
	"go.opentelemetry.io/otel/trace"
)

// tracer is the service-scoped helper for frontend spans.
var tracer = instrumentation.ForService("frontend")

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
	// ConfigURL is the rss_config base URL (empty = Configure page
	// falls back to the aggregator list, previous behavior).
	ConfigURL string
	// ConfigSecret is the gateway secret forwarded to rss_config.
	ConfigSecret string
	// AdminGroups lists the OIDC groups allowed to mutate config
	// (CONFIG_ADMIN_GROUPS, default "admin").
	AdminGroups []string
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
	mux.Handle("POST /config/feeds", guard(http.HandlerFunc(d.configAddHandler)))
	mux.Handle("POST /config/import/confirm", guard(http.HandlerFunc(d.configImportConfirmHandler)))
	mux.Handle("DELETE /config/feeds", guard(http.HandlerFunc(d.configDeleteHandler)))
	mux.Handle("POST /config/default-webhook", guard(http.HandlerFunc(d.configDefaultHandler)))
	mux.Handle("GET /img", guard(http.HandlerFunc(d.imgHandler)))
	mux.Handle("POST /config/import", guard(http.HandlerFunc(d.configImportHandler)))

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

// renderErr records template write errors on the caller's span. By the
// time Render fails the status line is committed, so the trace is the
// only place the failure surfaces.
func renderErr(span trace.Span, err error) {
	if err != nil {
		instrumentation.Fail(span, err, "render: %v", err)
	}
}

func (d Deps) frontHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	q, feed, page, perPage := pageParams(r)
	ctx, span := tracer.StartServer(r.Context(), "handlers.frontHandler")
	defer span.End()
	items, total, aggOK := d.listGraceful(ctx, q, feed, perPage, (page-1)*perPage)
	var feeds []string
	if aggOK {
		feeds, _ = d.Items.Feeds(ctx)
	}
	user := userLabel(r)
	renderErr(span, views.Front(views.FrontParams{
		Title: "The RSS Gazette", User: user, AuthEnabled: d.AuthEnabled,
		Items: items, Total: total, Q: q, Feed: feed, Feeds: feeds,
		Page: page, PerPage: perPage, AggOK: aggOK,
	}).Render(ctx, w))
}

func (d Deps) itemsPartial(w http.ResponseWriter, r *http.Request) {
	q, feed, page, perPage := pageParams(r)
	ctx, span := tracer.StartServer(r.Context(), "handlers.itemsPartial")
	defer span.End()
	items, total, aggOK := d.listGraceful(ctx, q, feed, perPage, (page-1)*perPage)
	renderErr(span, views.ItemsList(views.FrontParams{
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
	ctx, span := tracer.StartServer(r.Context(), "handlers.articleHandler")
	defer span.End()
	item, err := d.Items.Get(ctx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		instrumentation.Fail(span, err, "aggregator get: %v", err)
		http.Error(w, "aggregator unavailable", http.StatusBadGateway)
		return
	}
	article, fetchErr := d.Extractor(ctx, item.Link)
	if fetchErr != nil {
		log.ErrorFmt("extract %s: %v", item.Link, fetchErr)
	}
	renderErr(span, views.Article(views.ArticleParams{
		Title: item.Title, User: userLabel(r), AuthEnabled: d.AuthEnabled,
		Item: item, Article: article,
		FetchErr: fetchErr != nil,
	}).Render(ctx, w))
}

func (d Deps) imgHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.StartServer(r.Context(), "handlers.imgHandler")
	defer span.End()
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
		instrumentation.Fail(span, err, "img %s: %v", raw, err)
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(body) //nolint:errcheck
}
