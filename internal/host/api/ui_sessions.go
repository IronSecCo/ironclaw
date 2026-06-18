package api

import (
	"context"
	"net/http"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// SessionTerminator stops a running sandbox by session id. The session.Manager's
// Stop method satisfies it; it is wired in main.go via WithSessionTerminator. It
// is the ONLY host-control surface the console has — read endpoints never mutate.
type SessionTerminator func(ctx context.Context, id contract.SessionID) error

// sessionView is the read-model the sessions browser renders: a registry Session
// enriched with the human-readable agent-group name. Adds no new contract surface.
type sessionView struct {
	ID               contract.SessionID        `json:"id"`
	AgentGroupID     contract.AgentGroupID     `json:"agentGroupId"`
	AgentGroupName   string                    `json:"agentGroupName,omitempty"`
	MessagingGroupID contract.MessagingGroupID `json:"messagingGroupId"`
	ContainerStatus  string                    `json:"containerStatus"`
	LastActive       string                    `json:"lastActive"`
}

// WithSessionTerminator wires the host action behind POST
// /v1/ui/sessions/{id}/terminate. Without it (the default) the terminate endpoint
// returns 503 and the browser is read-only. Returns the Server for chaining.
func (s *Server) WithSessionTerminator(fn SessionTerminator) *Server {
	s.terminate = fn
	return s
}

// uiSessionsRoutes registers the sessions read-model and the terminate action.
// Wired from routes() in api.go. Both live under /v1 so they stay bearer-gated.
func (s *Server) uiSessionsRoutes() {
	s.mux.HandleFunc("GET /v1/ui/sessions", s.handleUISessions)
	s.mux.HandleFunc("POST /v1/ui/sessions/{id}/terminate", s.handleUITerminateSession)
}

// handleUISessions returns the live sessions projected with resolved agent-group
// names so the browser can render group/status readably.
func (s *Server) handleUISessions(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	sessions, err := s.reg.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]sessionView, 0, len(sessions))
	for _, sess := range sessions {
		v := sessionView{
			ID:               sess.ID,
			AgentGroupID:     sess.AgentGroupID,
			MessagingGroupID: sess.MessagingGroupID,
			ContainerStatus:  sess.ContainerStatus,
			LastActive:       sess.LastActive.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		if g, ok := s.reg.GetAgentGroup(sess.AgentGroupID); ok {
			v.AgentGroupName = g.Name
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, views)
}

// handleUITerminateSession stops a running sandbox via the wired host terminator.
// Stop is idempotent (no-op for an already-stopped session), so a double click is
// safe. Returns 503 when no terminator is wired (e.g. a registry-only test server).
func (s *Server) handleUITerminateSession(w http.ResponseWriter, r *http.Request) {
	id := contract.SessionID(r.PathValue("id"))
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	if s.terminate == nil {
		http.Error(w, "session control not configured", http.StatusServiceUnavailable)
		return
	}
	if err := s.terminate(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "terminated", "id": string(id)})
}
