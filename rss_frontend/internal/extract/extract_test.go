package extract

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testPage = `<!doctype html><html><head>
<title>Kernel 6.9 released — Example News</title>
<meta property="og:title" content="Kernel 6.9 released">
<meta property="og:site_name" content="Example News">
<meta property="og:image" content="/img/lead.png">
</head><body>
<nav>nav links</nav>
<article>
<h1>Kernel 6.9 released</h1>
<p>First paragraph with <a href="https://example.com/more">a link</a>.</p>
<script>alert("xss")</script>
<img src="/img/body.jpg" alt="photo">
<img src="data:image/gif;base64,R0lGODxhAQABAIAAAP///////yH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==" alt="pixel">
</article>
<footer>footer text</footer>
</body></html>`

func testBase(t *testing.T) *url.URL {
	t.Helper()
	u, err := url.Parse("https://example.com/posts/1")
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	return u
}

func TestParse(t *testing.T) {
	a, err := Parse(testBase(t), strings.NewReader(testPage))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if a.Title != "Kernel 6.9 released" {
		t.Errorf("title = %q", a.Title)
	}
	if a.SiteName != "Example News" {
		t.Errorf("site = %q", a.SiteName)
	}
	if !strings.Contains(a.LeadImage, "/img?url=") || !strings.Contains(a.LeadImage, "lead.png") {
		t.Errorf("lead = %q, want proxied lead.png", a.LeadImage)
	}
	for _, bad := range []string{"<script", "nav links", "footer text"} {
		if strings.Contains(a.HTML, bad) {
			t.Errorf("html contains %q", bad)
		}
	}
	if !strings.Contains(a.HTML, "/img?url=") || !strings.Contains(a.HTML, "body.jpg") {
		t.Errorf("body images not proxied: %q", a.HTML)
	}
	if strings.Contains(a.HTML, "data:image") {
		t.Errorf("data-uri image not stripped")
	}
	if !strings.Contains(a.HTML, "First paragraph") {
		t.Errorf("body text lost")
	}
}

func TestFetch(t *testing.T) {
	dialContextFunc = (&net.Dialer{}).DialContext
	t.Cleanup(func() { dialContextFunc = guardDial })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.rss" {
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write([]byte(`<rss/>`)) //nolint:errcheck
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(testPage)) //nolint:errcheck
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, err := Fetch(ctx, srv.URL+"/posts/1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if a.Title != "Kernel 6.9 released" {
		t.Errorf("title = %q", a.Title)
	}
	if _, err := Fetch(ctx, srv.URL+"/feed.rss"); err == nil {
		t.Error("expected error for non-html content")
	}
}

func TestFetch_Blocked(t *testing.T) {
	dialContextFunc = guardDial
	t.Cleanup(func() { dialContextFunc = guardDial })
	ctx := context.Background()
	for _, raw := range []string{"ftp://example.com/x", "http://127.0.0.1/x", "http://localhost/x"} {
		if _, err := Fetch(ctx, raw); err == nil {
			t.Errorf("%s: expected error", raw)
		}
	}
}
