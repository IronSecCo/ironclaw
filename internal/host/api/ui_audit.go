package api

import (
	"net/http"
	"strconv"

	"github.com/nivardsec/ironclaw/internal/host/gateway"
)

// uiAuditDefaultLimit / uiAuditMaxLimit bound the read-model response so the
// browser's live-tail polling can't ask the host to read an unbounded log.
const (
	uiAuditDefaultLimit = 200
	uiAuditMaxLimit     = 2000
)

// auditEntryView is the browser-facing projection of a gateway.AuditEntry: the
// same append-only record with the timestamp pre-formatted as RFC 3339 so the
// console renders and sorts it without reparsing. Adds no new contract surface;
// the underlying data is the existing /v1/audit JSONL.
type auditEntryView struct {
	Time     string `json:"time"`
	Stage    string `json:"stage"`
	ChangeID string `json:"changeId"`
	Kind     string `json:"kind,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// uiAuditRoutes registers the audit read-model the logs viewer polls. Wired from
// routes() in api.go. It lives under /v1 so it stays bearer-gated (only the
// static /ui/ shell is auth-exempt).
func (s *Server) uiAuditRoutes() {
	s.mux.HandleFunc("GET /v1/ui/audit", s.handleUIAudit)
}

// handleUIAudit returns the most recent audit entries (newest first, as
// gateway.ReadAudit provides) projected for the console. The optional ?limit=
// query caps the count (default 200, hard-capped at 2000). With no audit path
// attached it returns an empty list — mirroring handleAudit — so the viewer shows
// "no entries" rather than erroring. Filtering, search, and export are done
// client-side over this payload; this endpoint never mutates anything.
func (s *Server) handleUIAudit(w http.ResponseWriter, r *http.Request) {
	if s.auditPath == "" {
		writeJSON(w, http.StatusOK, []auditEntryView{})
		return
	}
	limit := uiAuditDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > uiAuditMaxLimit {
		limit = uiAuditMaxLimit
	}

	entries, err := gateway.ReadAudit(s.auditPath, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]auditEntryView, 0, len(entries))
	for _, e := range entries {
		views = append(views, auditEntryView{
			Time:     e.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Stage:    e.Stage,
			ChangeID: string(e.ChangeID),
			Kind:     string(e.Kind),
			Detail:   e.Detail,
		})
	}
	writeJSON(w, http.StatusOK, views)
}
