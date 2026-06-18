package api

import (
	"net/http"
	"strings"

	webui "github.com/nivardsec/ironclaw/web"
)

// uiPathPrefix is the subtree the embedded web console is served under.
const uiPathPrefix = "/ui/"

// uiAuthExempt reports whether path is the static console shell, which is served
// without the bearer token. A browser cannot attach an Authorization header to a
// top-level navigation, so the shell gets the same carve-out as the /healthz and
// /readyz probes (see auth() in api.go). This does NOT widen the network posture:
// the shell is secret-free static HTML/JS/CSS, the bind address is unchanged, and
// every data read and state change still flows through the bearer-gated /v1 API.
func uiAuthExempt(path string) bool {
	return path == "/ui" || strings.HasPrefix(path, uiPathPrefix)
}

// uiRoutes mounts the embedded web console on the existing mux (so it inherits the
// rate-limit and body-size middleware) at GET /ui/. It is wired from routes() so
// the console ships with the control-plane binary and needs no separate listener,
// port, or process. Auth exemption for the shell is handled in auth() via
// uiAuthExempt; the /v1 API the console drives stays gated.
func (s *Server) uiRoutes() {
	fileServer := http.FileServer(http.FS(webui.Assets()))
	// Bare /ui → /ui/ so relative asset URLs (app.js, style.css) resolve.
	s.mux.HandleFunc("GET /ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, uiPathPrefix, http.StatusMovedPermanently)
	})
	s.mux.Handle("GET "+uiPathPrefix, http.StripPrefix(uiPathPrefix, fileServer))
}
