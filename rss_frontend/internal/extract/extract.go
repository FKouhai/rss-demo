// Package extract fetches a linked article server-side and returns
// sanitized reader HTML with images rewritten through the /img proxy.
// It is the "image embedding from the server side": no hotlinking,
// no inline page scripts, trackers stripped at fetch time.
package extract

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/microcosm-cc/bluemonday"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	maxBody     = 2 << 20 // 2MB cap per article fetch
	fetchUA     = "rss-frontend/1.0 (+reader)"
	imgProxyRaw = "/img?url=%s"
)

// Metrics bundles the extract counters. All fields optional; nil entries
// are skipped so tests can attach a subset or nothing at all.
type Metrics struct {
	FetchTotal *prometheus.CounterVec
	FetchSecs  *prometheus.HistogramVec
}

var extractMetrics *Metrics

// SetMetrics installs the counters; nil-safe so tests can skip it.
func SetMetrics(m *Metrics) { extractMetrics = m }

func countFetch(outcome string) {
	if extractMetrics == nil || extractMetrics.FetchTotal == nil {
		return
	}
	extractMetrics.FetchTotal.WithLabelValues(outcome).Inc()
}

func observeFetch(outcome string, secs float64) {
	if extractMetrics == nil || extractMetrics.FetchSecs == nil {
		return
	}
	extractMetrics.FetchSecs.WithLabelValues(outcome).Observe(secs)
}

// Article is a fetched, sanitized page.
type Article struct {
	Title     string
	SiteName  string
	LeadImage string // proxied /img URL, "" when none
	HTML      string // sanitized fragment
}

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialContextFunc(ctx, network, addr)
		},
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// dialContextFunc is guardDial in production; tests swap it to reach
// httptest servers without weakening the production guard.
var dialContextFunc = guardDial

// guardDial blocks SSRF to non-public addresses (loopback, RFC1918,
// link-local, multicast) before connecting.
func guardDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("extract: no addresses for %s", host)
	}
	for _, ip := range ips {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() {
			return nil, fmt.Errorf("extract: non-public host blocked")
		}
	}
	return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(host, port))
}

// Fetch downloads rawURL and returns the extracted article.
func Fetch(ctx context.Context, rawURL string) (art Article, err error) {
	start := time.Now()
	outcome := "ok"
	defer func() {
		observeFetch(outcome, time.Since(start).Seconds())
		countFetch(outcome)
	}()
	fail := func(e error) (Article, error) {
		outcome = "error"
		return Article{}, e
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fail(err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fail(fmt.Errorf("extract: unsupported scheme %q", u.Scheme))
	}
	if u.User != nil {
		return fail(fmt.Errorf("extract: credentials in URL blocked"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("User-Agent", fetchUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("extract: status %d", resp.StatusCode))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" &&
		!strings.Contains(ct, "html") && !strings.Contains(ct, "text") {
		return fail(fmt.Errorf("extract: not an article page (%s)", ct))
	}
	art, err = Parse(u, io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fail(err)
	}
	return art, nil
}

// FetchBytes downloads rawURL with the SSRF guard, enforcing a size cap
// and an allowlist of content-type prefixes. It returns the body.
func FetchBytes(ctx context.Context, rawURL string, maxBytes int64, allowCT ...string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, "", fmt.Errorf("extract: unsupported scheme %q", u.Scheme)
	}
	if u.User != nil {
		return nil, "", fmt.Errorf("extract: credentials in URL blocked")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", fetchUA)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("extract: status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if len(allowCT) > 0 {
		ok := false
		for _, p := range allowCT {
			if strings.Contains(ct, p) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, "", fmt.Errorf("extract: content type %q not allowed", ct)
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("extract: body exceeds %d bytes", maxBytes)
	}
	return body, ct, nil
}

// Parse extracts title, lead image, and sanitized body from HTML.
func Parse(base *url.URL, r io.Reader) (Article, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return Article{}, err
	}
	var a Article
	a.Title = strings.TrimSpace(metaContent(doc, `meta[property="og:title"]`))
	if a.Title == "" {
		a.Title = strings.TrimSpace(doc.Find("title").First().Text())
	}
	a.SiteName = strings.TrimSpace(metaContent(doc, `meta[property="og:site_name"]`))
	if a.SiteName == "" && base != nil {
		a.SiteName = base.Host
	}
	lead := strings.TrimSpace(metaContent(doc, `meta[property="og:image"]`))
	if lead != "" {
		lead = absURL(base, lead)
	}

	body := doc.Find("article").First()
	if body.Length() == 0 {
		body = doc.Find("main").First()
	}
	if body.Length() == 0 {
		body = doc.Find("body").First()
	}
	body.Find("script, style, nav, header, footer, form, aside, iframe, noscript").Remove()
	body.Find("*").RemoveAttr("onclick").RemoveAttr("onerror").RemoveAttr("onload")

	body.Find("img").Each(func(_ int, img *goquery.Selection) {
		src, ok := img.Attr("src")
		if !ok {
			return
		}
		src = strings.TrimSpace(src)
		if src == "" || strings.HasPrefix(src, "data:") {
			img.Remove()
			return
		}
		abs := absURL(base, src)
		if abs == "" {
			img.Remove()
			return
		}
		if lead == "" {
			lead = proxyURL(abs)
		}
		img.SetAttr("src", proxyURL(abs))
		img.RemoveAttr("srcset").RemoveAttr("sizes")
	})

	html, err := body.Html()
	if err != nil {
		return Article{}, err
	}
	a.HTML = policy().Sanitize(html)
	if lead != "" {
		a.LeadImage = proxyURL(lead)
	}
	return a, nil
}

func metaContent(doc *goquery.Document, sel string) string {
	if v, ok := doc.Find(sel).First().Attr("content"); ok {
		return v
	}
	return ""
}

func absURL(base *url.URL, ref string) string {
	if ref == "" || base == nil {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return base.ResolveReference(u).String()
}

func proxyURL(abs string) string {
	return fmt.Sprintf(imgProxyRaw, url.QueryEscape(abs))
}

func policy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowRelativeURLs(true)
	p.AllowAttrs("src", "alt", "title", "loading").OnElements("img")
	p.AllowElements("figure", "figcaption", "picture", "time")
	return p
}
