package handlers

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const importOPML = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0"><head><title>t</title></head><body>
<outline text="News"><outline text="New One" type="rss" xmlUrl="https://new.example/feed.xml"/></outline>
<outline text="Dup" type="rss" xmlUrl="https://cfg.example/a.rss"/>
<outline text="Bad" type="rss" xmlUrl="gopher://bad.example/x"/>
</body></opml>`

func serveMultipart(t *testing.T, d Deps, target, field, filename, content string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	mux := http.NewServeMux()
	Register(mux, d)
	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestConfigImport_Preview(t *testing.T) {
	d, _ := configDeps(t, &fakeConfig{}, newTestDeps(t))
	rr := serveMultipart(t, d, "/config/import", "opml", "feeds.opml", importOPML)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{"1 new", "1 already configured", "1 invalid", "https://new.example/feed.xml", "Import selected"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %q", want)
		}
	}
	if !strings.Contains(body, `type="checkbox" name="url" value="https://new.example/feed.xml"`) {
		t.Error("confirm form missing checked checkbox input")
	}
}

func TestConfigImport_PreviewNotMultipart(t *testing.T) {
	d, _ := configDeps(t, &fakeConfig{}, newTestDeps(t))
	rr := serveReq(t, d, http.MethodPost, "/config/import", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400 for non-multipart", rr.Code)
	}
}

func TestConfigImportConfirm_ForwardsNew(t *testing.T) {
	fc := &fakeConfig{}
	d, _ := configDeps(t, fc, newTestDeps(t))
	form := url.Values{"url": {"https://new.example/feed.xml", "https://other.example/rss"}}
	rr := serveReq(t, d, http.MethodPost, "/config/import/confirm", form)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var hooks string
	for _, h := range fc.hooks {
		hooks += h
	}
	for _, want := range []string{"https://new.example/feed.xml", "https://other.example/rss", "rss_feeds"} {
		if !strings.Contains(hooks, want) {
			t.Errorf("forwarded /hooks/update body missing %q: %s", want, hooks)
		}
	}
	// Toast appends to #toasts; the preview table is left alone by design.
	body := rr.Body.String()
	if strings.Contains(body, "config-import-preview") {
		t.Error("confirm must not touch the preview table")
	}
	for _, want := range []string{`hx-swap-oob="beforeend:#toasts"`, "Imported 2 feeds", `<div class="toast ok" data-autodismiss="6000">`} {
		if !strings.Contains(body, want) {
			t.Errorf("confirm response missing %q", want)
		}
	}
}

func TestConfigImportConfirm_RejectsInvalid(t *testing.T) {
	fc := &fakeConfig{}
	d, _ := configDeps(t, fc, newTestDeps(t))
	form := url.Values{"url": {"gopher://bad.example/x"}}
	rr := serveReq(t, d, http.MethodPost, "/config/import/confirm", form)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400 for invalid url", rr.Code)
	}
	for _, c := range fc.received {
		if c.path == "/hooks/update" {
			t.Error("/hooks/update must not be called for invalid input")
		}
	}
}

func TestConfigImportConfirm_RespectsLimit(t *testing.T) {
	t.Setenv("MAX_IMPORT_FEEDS", "1")
	fc := &fakeConfig{}
	d, _ := configDeps(t, fc, newTestDeps(t))
	form := url.Values{"url": {"https://a.example/1.rss", "https://b.example/2.rss"}}
	rr := serveReq(t, d, http.MethodPost, "/config/import/confirm", form)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400 over the limit", rr.Code)
	}
}
