package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/FKouhai/rss-demo/libs/instrumentation"
	log "github.com/FKouhai/rss-demo/libs/logger"
	"github.com/FKouhai/rss-frontend/internal/opml"
	"github.com/FKouhai/rss-frontend/views"
	"go.uber.org/zap"
)

// validFeedURL accepts only http(s) URLs with a host. Shared by the
// manual add form and the OPML import preview so both enforce the
// same rule the backend does.
func validFeedURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// ConfigImportHandler multi part upload handler
func (d Deps) configImportHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.StartServer(r.Context(), "handlers.ConfigImport")
	defer span.End()

	// set 1MB max reader cap
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		log.InfoCtx(ctx, "import rejected: not multipart")
		http.Error(w, "expected multipart upload", http.StatusBadRequest)
		return
	}

	var file io.Reader

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "malformed upload", http.StatusBadRequest)
			return
		}
		if part.FormName() == "opml" {
			file = part
			break
		}
	}
	if file == nil {
		http.Error(w, "missing opml file", http.StatusBadRequest)
		return
	}

	feeds, err := opml.Parse(file, maxImportFeeds())
	if err != nil {
		instrumentation.Fail(span, err, "opml parse: %v", err)
		http.Error(w, "invalid OPML file: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Diff against the same merged snapshot the page renders: legacy
	// URLs plus overrides. Unmarshalling the raw snapshot here would
	// drop rss_feeds (views.ConfigData has no field for it).
	data, ok := d.configSnapshot(ctx)
	if !ok {
		instrumentation.Fail(span, fmt.Errorf("config snapshot unavailable"), "snapshot unavailable")
		http.Error(w, "config unavailable", http.StatusBadGateway)
		return
	}

	existing := make(map[string]bool, len(data.Feeds))
	for _, f := range data.Feeds {
		existing[f.URL] = true
	}

	var preview views.ImportPreview
	for _, f := range feeds {
		if !validFeedURL(f.URL) {
			preview.Invalid = append(preview.Invalid, f.URL)
			continue
		}

		if existing[f.URL] {
			preview.Existing = append(preview.Existing, f.URL)
			continue
		}

		preview.New = append(preview.New, views.ImportFeed{URL: f.URL, Title: f.Title})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderErr(span, views.ConfigImportPreview(preview).Render(ctx, w))
}

func maxImportFeeds() int {
	n, _ := strconv.Atoi(os.Getenv("MAX_IMPORT_FEEDS"))
	return n
}

// configImportConfirmHandler forwards the preview-approved URL list to
// rss_config's merge endpoint and refreshes the feed table, exactly
// like the other mutating handlers. Every URL is re-validated: the
// preview is advisory, this is the enforcement point.
func (d Deps) configImportConfirmHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.StartServer(r.Context(), "handlers.ConfigImportConfirm")
	defer span.End()
	if !d.isAdmin(r) {
		http.Error(w, "admin group required", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	max := maxImportFeeds()
	if max <= 0 {
		max = opml.DefaultMaxFeeds
	}
	var urls []string
	for _, u := range r.Form["url"] {
		if !validFeedURL(u) {
			http.Error(w, "invalid url in import", http.StatusBadRequest)
			return
		}
		urls = append(urls, u)
	}
	if len(urls) == 0 {
		http.Error(w, "nothing to import", http.StatusBadRequest)
		return
	}
	if len(urls) > max {
		http.Error(w, "import exceeds limit", http.StatusBadRequest)
		return
	}
	code, _ := d.proxyConfig(r, http.MethodPost, "/hooks/update", map[string]any{"rss_feeds": urls})
	if code == 0 {
		instrumentation.Fail(span, fmt.Errorf("config unreachable"), "hooks/update: %v", code)
		http.Error(w, "config unavailable", http.StatusBadGateway)
		return
	}
	if code != http.StatusOK {
		instrumentation.Fail(span, fmt.Errorf("config rejected import: %d", code), "hooks/update: %v", code)
		http.Error(w, "config rejected the import", code)
		return
	}
	log.InfoCtx(ctx, "import confirmed",
		zap.String("user", userLabel(r)),
		zap.Int("feeds", len(urls)))
	d.renderConfigTable(w, r, ctx)
	// Out-of-band toast where the eye is. The styled div nests inside
	// a plain OOB wrapper: htmx inserts the wrapper content, so the
	// attributes that matter (class, data-autodismiss) must live on the
	// inner element. A second OOB div clears the now-stale preview so
	// a re-import starts clean; the updated feed list is the record.
	if _, err := fmt.Fprintf(w, `<div hx-swap-oob="beforeend:#toasts"><div class="toast ok" data-autodismiss="6000">Imported %d feeds</div></div><div id="config-import-preview" hx-swap-oob="true"></div>`, len(urls)); err != nil {
		instrumentation.Fail(span, err, "confirm toast write: %v", err)
	}
}
