package api

import (
	"net/http"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// agentView is the read-model the Agents tab renders: a registry AgentGroup
// enriched with the counts the console surfaces (destinations it may post to and
// live sessions). It adds no new contract surface.
type agentView struct {
	ID           contract.AgentGroupID `json:"id"`
	Name         string                `json:"name"`
	Folder       string                `json:"folder,omitempty"`
	Provider     string                `json:"provider,omitempty"`
	Model        string                `json:"model,omitempty"`
	Destinations int                   `json:"destinations"`
	Sessions     int                   `json:"sessions"`
}

// uiAgentsRoutes registers the agents read-model that backs the console's agent
// picker + builder. Wired from routes() in api.go; under /v1 so it stays
// bearer-gated. Creating/editing an agent group still flows through the existing
// PUT /v1/registry/agent-groups/{id}; this is the missing LIST the UI needs.
func (s *Server) uiAgentsRoutes() {
	s.mux.HandleFunc("GET /v1/ui/agents", s.handleUIAgents)
}

// handleUIAgents lists every agent group with the counts the console shows, so the
// Agents tab can render a picker + builder without the operator knowing ids.
func (s *Server) handleUIAgents(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	groups := s.reg.ListAgentGroups()

	// Tally live sessions per agent group once (best-effort; missing on error).
	sessionsByGroup := map[contract.AgentGroupID]int{}
	if sessions, err := s.reg.ListSessions(); err == nil {
		for _, sess := range sessions {
			sessionsByGroup[sess.AgentGroupID]++
		}
	}

	views := make([]agentView, 0, len(groups))
	for _, g := range groups {
		views = append(views, agentView{
			ID:           g.ID,
			Name:         g.Name,
			Folder:       g.Folder,
			Provider:     g.Provider,
			Model:        g.Model,
			Destinations: len(s.reg.ListDestinations(g.ID)),
			Sessions:     sessionsByGroup[g.ID],
		})
	}
	writeJSON(w, http.StatusOK, views)
}
