package api

import (
	"net/http"

	"github.com/IronSecCo/ironclaw/internal/host/catalog"
)

// uiCatalogRoutes registers the read-only catalog endpoints that let the console
// (and any client) DISCOVER what an agent can be made of without knowing internal
// tool names: the built-in tools and the starter templates. Both are static,
// derived from the compiled tool set — they need no registry and carry no secret —
// but live under /v1 so they stay bearer-gated like every other data read.
func (s *Server) uiCatalogRoutes() {
	s.mux.HandleFunc("GET /v1/ui/tools", s.handleUITools)
	s.mux.HandleFunc("GET /v1/ui/templates", s.handleUITemplates)
	s.mux.HandleFunc("GET /v1/ui/persona-sections", s.handleUIPersonaSections)
}

// handleUITools returns the friendly catalog of built-in tools so the agent builder
// can render a pick-list with titles, descriptions, categories, and the egress /
// mandatory badges — instead of asking the operator to type tool names.
func (s *Server) handleUITools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog.Tools())
}

// handleUITemplates returns the starter presets (persona + recommended toolset) so
// the builder can offer one-click starting points.
func (s *Server) handleUITemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog.Templates())
}

// handleUIPersonaSections returns the persona documents an agent is composed from
// (identity/soul/instructions) with their display copy, so the builder renders the
// right labeled fields instead of one opaque textarea.
func (s *Server) handleUIPersonaSections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog.PersonaSections())
}
