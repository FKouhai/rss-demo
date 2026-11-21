// Package static holds the embedded static assets (JS, CSS, favicon)
// served at /static/.
package static

import "embed"

// FS is the embedded static tree.
//
//go:embed js/* *.css *.svg
var FS embed.FS
