// OWNER: T-220

// Package webui embeds the IronClaw web console — a static, dependency-free SPA
// served by the control-plane at GET /ui/. The assets are compiled into the
// binary via go:embed, so the standard `go build ./...` (and CI) builds the
// console with no Node/npm toolchain. Feature tasks (T-221+) add pages as plain
// files under static/.
package webui

import (
	"embed"
	"io/fs"
)

// staticFS holds the served assets. The `all:` prefix includes files the default
// embed pattern would skip; there are none here today, but it keeps additions
// (e.g. a future dot-file) from silently dropping out of the binary.
//
//go:embed all:static
var staticFS embed.FS

// Assets returns the console's static files rooted at the directory the browser
// sees (so "index.html", "app.js", ... resolve directly). It panics only on a
// build-time embed mistake — the sub-tree is compiled in, so a failure here means
// the binary itself is malformed, which should fail tests, not production.
func Assets() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("webui: embedded static/ tree missing: " + err.Error())
	}
	return sub
}
