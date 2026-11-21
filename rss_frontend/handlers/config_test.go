package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeConfig is a stub rss_config backend: serves a fixed snapshot and
// records mutations with the headers the frontend forwarded.
type fakeConfig struct {
	urls     []string
	received []receivedCall
	secret   string
}

type receivedCall struct {
	method, path, secret, user, groups string
}

func (f *fakeConfig) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		call := receivedCall{
			method: r.Method, path: r.URL.RequestURI(),
			secret: r.Header.Get("X-Gateway-Secret"),
			user:   r.Header.Get("X-Forwarded-User"),
			groups: r.Header.Get("X-Forwarded-Groups"),
		}
		f.received = append(f.received, call)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/config":
			_, _ = w.Write([]byte(`{"rss_feeds":["https://cfg.example/a.rss"],"webhook_url":"https://hooks/g","feeds":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/feeds":
			f.urls = append(f.urls, r.FormValue("url"))
			_, _ = w.Write([]byte(`{"url":"x"}`))
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"deleted":true}`))
		case r.Method == http.MethodPut:
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func configDeps(t *testing.T, fc *fakeConfig, d Deps) (Deps, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(fc.handler())
	t.Cleanup(srv.Close)
	d.ConfigURL = srv.URL
	d.ConfigSecret = fc.secret
	d.AdminGroups = []string{"admin"}
	return d, srv
}

func serveReq(t *testing.T, d Deps, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, d)
	var body *strings.Reader
	ct := ""
	if form != nil {
		body = strings.NewReader(form.Encode())
		ct = "application/x-www-form-urlencoded"
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestConfig_ManagedList(t *testing.T) {
	d, _ := configDeps(t, &fakeConfig{}, newTestDeps(t))
	rr := serve(t, d, http.MethodGet, "/config")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"https://cfg.example/a.rss", "https://hooks/g", "Add feed", "Remove", "formrow", "btn danger"} {
		if !strings.Contains(body, want) {
			t.Errorf("config page missing %q", want)
		}
	}
	if strings.Contains(body, "rss_config service (pending)") {
		t.Error("pending notice must be gone once config serves")
	}
}

func TestConfig_AddProxies(t *testing.T) {
	fc := &fakeConfig{secret: "s3cret"}
	d, _ := configDeps(t, fc, newTestDeps(t))
	form := url.Values{"url": {"https://cfg.example/new.rss"}, "webhook_url": {"https://hooks/n"}}
	rr := serveReq(t, d, http.MethodPost, "/config/feeds", form)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if len(fc.received) == 0 || fc.received[0].method != http.MethodPost || fc.received[0].secret != "s3cret" {
		t.Fatalf("backend calls = %+v", fc.received)
	}
	if !strings.Contains(rr.Body.String(), "config-feeds") {
		t.Error("want updated table fragment")
	}
}

func TestConfig_DeleteProxies(t *testing.T) {
	fc := &fakeConfig{}
	d, _ := configDeps(t, fc, newTestDeps(t))
	rr := serveReq(t, d, http.MethodDelete, "/config/feeds?url=https://cfg.example/a.rss", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	if len(fc.received) == 0 || fc.received[0].method != http.MethodDelete {
		t.Fatalf("backend calls = %+v", fc.received)
	}
}

func TestConfig_NonAdminForbidden(t *testing.T) {
	d, _ := configDeps(t, &fakeConfig{}, newTestDeps(t))
	d.AuthEnabled = true // session present but no admin group → 403
	for _, tc := range []struct{ method, target string }{
		{http.MethodPost, "/config/feeds"},
		{http.MethodDelete, "/config/feeds?url=x"},
		{http.MethodPost, "/config/default-webhook"},
	} {
		rr := serveReq(t, d, tc.method, tc.target, url.Values{"url": {"https://x.example/"}})
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", tc.method, tc.target, rr.Code)
		}
	}
}

func TestConfig_BackendDownFallsBack(t *testing.T) {
	d := newTestDeps(t)
	d.ConfigURL = "http://127.0.0.1:1" // nothing there
	rr := serve(t, d, http.MethodGet, "/config")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want degraded 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "read-only") {
		t.Error("want aggregator fallback notice")
	}
	if !strings.Contains(body, "https://a.example/rss") {
		t.Error("want aggregator feeds as fallback")
	}
}
