package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FKouhai/rss-demo/libs/auth/oidc"
	"github.com/FKouhai/rss-frontend/internal/extract"
	"github.com/FKouhai/rss-frontend/views"
)

// fakeStore is an in-memory ItemStore for handler tests.
type fakeStore struct {
	items []views.Item
	err   error
}

func (f fakeStore) List(_ context.Context, q, feed string, limit, offset int) ([]views.Item, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	var out []views.Item
	for _, it := range f.items {
		if q != "" && !strings.Contains(strings.ToLower(it.Title), strings.ToLower(q)) {
			continue
		}
		if feed != "" && it.FeedURL != feed {
			continue
		}
		out = append(out, it)
	}
	total := len(out)
	if offset >= len(out) {
		return nil, total, nil
	}
	out = out[offset:]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f fakeStore) Feeds(context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	seen := map[string]bool{}
	var out []string
	for _, it := range f.items {
		if !seen[it.FeedURL] {
			seen[it.FeedURL] = true
			out = append(out, it.FeedURL)
		}
	}
	return out, nil
}

func (f fakeStore) Get(_ context.Context, id string) (views.Item, error) {
	if f.err != nil {
		return views.Item{}, f.err
	}
	for _, it := range f.items {
		if it.ID == id {
			return it, nil
		}
	}
	return views.Item{}, ErrNotFound
}

func newTestDeps(t *testing.T) Deps {
	t.Helper()
	return Deps{
		Items: fakeStore{items: []views.Item{
			{ID: "a1", FeedURL: "https://a.example/rss", Title: "Alpha kernel", Link: "https://a.example/1", PublishedAt: "2026-11-21"},
			{ID: "b1", FeedURL: "https://b.example/rss", Title: "Beta release", Link: "https://b.example/2", PublishedAt: "2026-11-20"},
		}},
		HTTP:          http.DefaultClient,
		Extractor:     func(_ context.Context, _ string) (extract.Article, error) { return extract.Article{Title: "stub"}, nil },
		PollerURL:     "http://127.0.0.1:1",
		NotifyURL:     "http://127.0.0.1:1",
		OutboxURL:     "http://127.0.0.1:1",
		AggregatorURL: "http://127.0.0.1:1",
	}
}

func serve(t *testing.T, d Deps, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, d)
	req := httptest.NewRequest(method, target, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestFront(t *testing.T) {
	rr := serve(t, newTestDeps(t), http.MethodGet, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"The RSS Gazette", "Alpha kernel", "Beta release", "htmx.min.js", "alpine.min.js", "data-theme"} {
		if !strings.Contains(body, want) {
			t.Errorf("front missing %q", want)
		}
	}
}

func TestFront_AggregatorDown(t *testing.T) {
	d := newTestDeps(t)
	d.Items = fakeStore{err: fmt.Errorf("conn refused")}
	rr := serve(t, d, http.MethodGet, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want degraded 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Aggregator unreachable") {
		t.Errorf("missing degraded notice")
	}
}

func TestItemsPartial_Search(t *testing.T) {
	rr := serve(t, newTestDeps(t), http.MethodGet, "/partials/items?q=alpha&per_page=16")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Alpha kernel") || strings.Contains(body, "Beta release") {
		t.Errorf("search filtering wrong: %q", body)
	}
}

func TestArticle(t *testing.T) {
	d := newTestDeps(t)
	rr := serve(t, d, http.MethodGet, "/items/a1")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Alpha kernel") {
		t.Errorf("article missing title")
	}
	rr = serve(t, d, http.MethodGet, "/items/nope")
	if rr.Code != http.StatusNotFound {
		t.Errorf("missing item code = %d, want 404", rr.Code)
	}
}

func TestArticle_AggregatorDown(t *testing.T) {
	d := newTestDeps(t)
	d.Items = fakeStore{err: fmt.Errorf("conn refused")}
	rr := serve(t, d, http.MethodGet, "/items/a1")
	if rr.Code != http.StatusBadGateway {
		t.Errorf("code = %d, want 503-family", rr.Code)
	}
}

func TestStatus(t *testing.T) {
	rr := serve(t, newTestDeps(t), http.MethodGet, "/partials/status")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"rss-poller", "rss-notify", "rss-outbox", "Degraded"} {
		if !strings.Contains(body, want) {
			t.Errorf("status missing %q", want)
		}
	}
}

func TestConfig_AggregatorDown(t *testing.T) {
	d := newTestDeps(t)
	d.Items = fakeStore{err: fmt.Errorf("conn refused")}
	rr := serve(t, d, http.MethodGet, "/config")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want degraded 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Aggregator unreachable") {
		t.Errorf("missing degraded notice")
	}
}

func TestImg_Rejects(t *testing.T) {
	for _, target := range []string{"/img", "/img?url=ftp://x/y", "/img?url=http://127.0.0.1/"} {
		rr := serve(t, newTestDeps(t), http.MethodGet, target)
		if rr.Code == http.StatusOK {
			t.Errorf("%s: got 200", target)
		}
	}
}

func TestHealthReady(t *testing.T) {
	d := newTestDeps(t)
	for _, p := range []string{"/healthz", "/ready"} {
		rr := serve(t, d, http.MethodGet, p)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: code = %d", p, rr.Code)
		}
	}
}

func discoveryStub(t *testing.T) string {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			srv.URL, srv.URL+"/auth", srv.URL+"/token", srv.URL+"/keys"); err != nil {
			http.Error(w, "write failed", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newAuthDeps(t *testing.T) Deps {
	t.Helper()
	d := newTestDeps(t)
	oc, err := oidc.NewClient(context.Background(), oidc.Config{
		IssuerURL:    discoveryStub(t),
		ClientID:     "rss-frontend",
		ClientSecret: "secret",
		RedirectURL:  "https://rss.example.com/auth/callback",
		SessionKey:   testSessionKey(),
	})
	if err != nil {
		t.Fatalf("oidc: %v", err)
	}
	d.OIDC = oc
	d.AuthEnabled = true
	return d
}

func testSessionKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 7)
	}
	return k
}

func TestLogin_RedirectsWithStateCookie(t *testing.T) {
	d := newAuthDeps(t)
	mux := http.NewServeMux()
	Register(mux, d)
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("code = %d, want 302", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "http") || !strings.Contains(loc, "code_challenge") {
		t.Errorf("redirect = %q, want PKCE authorize URL", loc)
	}
	var stateCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "__Host-oauth-state" {
			stateCookie = c
		}
	}
	if stateCookie == nil || stateCookie.Value == "" {
		t.Fatal("missing state cookie")
	}
	if !stateCookie.HttpOnly {
		t.Error("state cookie must be HttpOnly")
	}
}

func TestCallback_Rejects(t *testing.T) {
	d := newAuthDeps(t)
	mux := http.NewServeMux()
	Register(mux, d)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/auth/callback?code=x&state=y", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("no-cookie code = %d, want 400", rr.Code)
	}

	req, err := d.OIDC.AuthURL()
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}
	bundle, err := d.OIDC.SealState(req)
	if err != nil {
		t.Fatalf("SealState: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=x&state=wrong", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-oauth-state", Value: bundle})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, r)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("mismatch code = %d, want 400", rr.Code)
	}
}

func TestPages_RequireAuth(t *testing.T) {
	d := newAuthDeps(t)
	mux := http.NewServeMux()
	Register(mux, d)
	for _, p := range []string{"/", "/config", "/status", "/img?url=https://example.com/x.png"} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, p, nil))
		if rr.Code != http.StatusFound {
			t.Errorf("%s: code = %d, want 302 to login", p, rr.Code)
		}
	}
	hx := httptest.NewRequest(http.MethodGet, "/partials/items", nil)
	hx.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, hx)
	if rr.Code != http.StatusUnauthorized || rr.Header().Get("HX-Redirect") == "" {
		t.Errorf("hx: code = %d hx-redirect = %q", rr.Code, rr.Header().Get("HX-Redirect"))
	}
}

func TestStatic_ETagRevalidation(t *testing.T) {
	h := staticHandler()

	plain := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, plain)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET app.css = %d, want 200", rr.Code)
	}
	etag := rr.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag on static response")
	}

	fresh := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	fresh.Header.Set("If-None-Match", etag)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, fresh)
	if rr.Code != http.StatusNotModified {
		t.Errorf("matching If-None-Match = %d, want 304", rr.Code)
	}

	stale := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	stale.Header.Set("If-None-Match", `"stale"`)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, stale)
	if rr.Code != http.StatusOK {
		t.Errorf("mismatched If-None-Match = %d, want 200", rr.Code)
	}
}
