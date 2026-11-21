package handlers

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"

	frontendstatic "github.com/FKouhai/rss-frontend/static"
)

// etags maps each embedded static path to its content hash, computed once.
// embed.FS reports zero modtimes, so http.FileServer emits no validators
// (no ETag, no Last-Modified) and browsers heuristically cache stale
// bundles across redeploys with no revalidation path. Explicit ETags fix
// that: matching If-None-Match gets a 304, anything else the full body.
var (
	etags     = map[string]string{}
	etagsOnce sync.Once
)

func etagFor(name string) string {
	etagsOnce.Do(func() {
		_ = fs.WalkDir(frontendstatic.FS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			b, err := frontendstatic.FS.ReadFile(path)
			if err != nil {
				return nil
			}
			etags[path] = fmt.Sprintf(`"%x"`, sha256.Sum256(b))
			return nil
		})
	})
	return etags[name]
}

func staticHandler() http.Handler {
	fs := http.FileServer(http.FS(frontendstatic.FS))
	return http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// FileServer's embedded paths are slash-clean and relative;
		// the name doubles as the etag map key.
		name := strings.TrimPrefix(r.URL.Path, "/")
		if etag := etagFor(name); etag != "" {
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		fs.ServeHTTP(w, r)
	}))
}
