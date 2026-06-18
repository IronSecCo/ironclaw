package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// WithRegistry attaches the control-plane registry so the /v1/registry admin
// endpoints become live. Without it those endpoints return 503. Returns the
// Server for chaining.
//
// The endpoints expose ONLY the existing registry.Registry methods (no interface
// changes): upsert/get for agent groups, messaging groups, wirings, users; grant/
// revoke for roles; add/remove for members; add + permission-check for
// destinations; list/get for sessions; and an access check. The Registry
// interface has no list-all or delete for agent groups / users / messaging groups
// / wirings / destinations, so those verbs are intentionally absent here (adding
// them is a registry-interface change, out of scope for this task).
func (s *Server) WithRegistry(reg registry.Registry) *Server {
	s.reg = reg
	return s
}

// registryRoutes wires the registry admin endpoints. Handlers guard on a nil
// registry (mirroring the history/audit nil-handling) so the routes are always
// registered but inert until WithRegistry is called.
func (s *Server) registryRoutes() {
	s.mux.HandleFunc("GET /v1/registry/agent-groups", s.handleListAgentGroups)
	s.mux.HandleFunc("PUT /v1/registry/agent-groups/{id}", s.handlePutAgentGroup)
	s.mux.HandleFunc("GET /v1/registry/agent-groups/{id}", s.handleGetAgentGroup)

	s.mux.HandleFunc("POST /v1/registry/messaging-groups", s.handleCreateMessagingGroup)
	s.mux.HandleFunc("GET /v1/registry/messaging-groups/{id}", s.handleGetMessagingGroup)
	s.mux.HandleFunc("GET /v1/registry/messaging-groups/{id}/wirings", s.handleListWirings)

	s.mux.HandleFunc("POST /v1/registry/wirings", s.handleCreateWiring)

	s.mux.HandleFunc("PUT /v1/registry/users/{id}", s.handlePutUser)
	s.mux.HandleFunc("GET /v1/registry/users/{id}", s.handleGetUser)

	s.mux.HandleFunc("POST /v1/registry/roles", s.handleGrantRole)
	s.mux.HandleFunc("POST /v1/registry/roles/revoke", s.handleRevokeRole)

	s.mux.HandleFunc("POST /v1/registry/members", s.handleAddMember)
	s.mux.HandleFunc("POST /v1/registry/members/remove", s.handleRemoveMember)

	s.mux.HandleFunc("POST /v1/registry/destinations", s.handleAddDestination)
	s.mux.HandleFunc("GET /v1/registry/destinations/check", s.handleCheckDestination)

	s.mux.HandleFunc("GET /v1/registry/sessions", s.handleListSessions)
	s.mux.HandleFunc("GET /v1/registry/sessions/{id}", s.handleGetSession)

	s.mux.HandleFunc("GET /v1/registry/access", s.handleAccessCheck)
}

// regReady reports whether a registry is attached; if not it writes 503 and
// returns false.
func (s *Server) regReady(w http.ResponseWriter) bool {
	if s.reg == nil {
		http.Error(w, "registry not configured", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// decodeJSON decodes the request body into v, writing 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

// --- agent groups ---

func (s *Server) handlePutAgentGroup(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var g registry.AgentGroup
	if !decodeJSON(w, r, &g) {
		return
	}
	g.ID = contract.AgentGroupID(r.PathValue("id")) // path is authoritative
	if err := s.reg.PutAgentGroup(g); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleGetAgentGroup(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	g, ok := s.reg.GetAgentGroup(contract.AgentGroupID(r.PathValue("id")))
	if !ok {
		http.Error(w, "agent group not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, g)
}
func (s *Server) handleListAgentGroups(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	groups := s.reg.ListAgentGroups()
	if groups == nil {
		groups = []registry.AgentGroup{}
	}
	writeJSON(w, http.StatusOK, groups)
}

// --- messaging groups ---

type messagingGroupRequest struct {
	ChannelType string                       `json:"channelType"`
	PlatformID  string                       `json:"platformID"`
	Instance    string                       `json:"instance"`
	IsGroup     bool                         `json:"isGroup"`
	Policy      contract.UnknownSenderPolicy `json:"unknownSenderPolicy"`
}

func (s *Server) handleCreateMessagingGroup(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var req messagingGroupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Policy == "" {
		req.Policy = contract.UnknownStrict
	}
	mg, err := s.reg.GetOrCreateMessagingGroup(req.ChannelType, req.PlatformID, req.Instance, req.IsGroup, req.Policy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, mg)
}

func (s *Server) handleGetMessagingGroup(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	mg, ok := s.reg.GetMessagingGroup(contract.MessagingGroupID(r.PathValue("id")))
	if !ok {
		http.Error(w, "messaging group not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, mg)
}

func (s *Server) handleListWirings(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	ws, err := s.reg.ListWirings(contract.MessagingGroupID(r.PathValue("id")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ws == nil {
		ws = []registry.Wiring{}
	}
	writeJSON(w, http.StatusOK, ws)
}

// --- wirings ---

func (s *Server) handleCreateWiring(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var wr registry.Wiring
	if !decodeJSON(w, r, &wr) {
		return
	}
	// Assign an id when omitted so the response can report it (PutWiring does not
	// return the generated id).
	if wr.ID == "" {
		wr.ID = "wr_" + apiID()
	}
	if err := s.reg.PutWiring(wr); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, wr)
}

// --- users ---

func (s *Server) handlePutUser(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var u registry.User
	if !decodeJSON(w, r, &u) {
		return
	}
	u.ID = contract.UserID(r.PathValue("id"))
	if err := s.reg.PutUser(u); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	u, ok := s.reg.GetUser(contract.UserID(r.PathValue("id")))
	if !ok {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// --- roles ---

func (s *Server) handleGrantRole(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var role registry.Role
	if !decodeJSON(w, r, &role) {
		return
	}
	if err := s.reg.GrantRole(role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

func (s *Server) handleRevokeRole(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var role registry.Role
	if !decodeJSON(w, r, &role) {
		return
	}
	if err := s.reg.RevokeRole(role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- members ---

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var m registry.Member
	if !decodeJSON(w, r, &m) {
		return
	}
	if err := s.reg.AddMember(m); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var m registry.Member
	if !decodeJSON(w, r, &m) {
		return
	}
	if err := s.reg.RemoveMember(m); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- destinations ---

type destinationRequest struct {
	AgentGroupID contract.AgentGroupID `json:"agentGroupID"`
	ChannelType  string                `json:"channelType"`
	PlatformID   string                `json:"platformID"`
}

func (s *Server) handleAddDestination(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	var req destinationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.reg.AddDestination(req.AgentGroupID, req.ChannelType, req.PlatformID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) handleCheckDestination(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	q := r.URL.Query()
	allowed := s.reg.IsAllowedDestination(
		contract.AgentGroupID(q.Get("agentGroupID")), q.Get("channelType"), q.Get("platformID"))
	writeJSON(w, http.StatusOK, map[string]bool{"allowed": allowed})
}

// --- sessions (read-only) ---

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	sessions, err := s.reg.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []registry.Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	sess, ok := s.reg.GetSession(contract.SessionID(r.PathValue("id")))
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// --- access check ---

func (s *Server) handleAccessCheck(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	q := r.URL.Query()
	allowed, reason := s.reg.CanAccess(contract.UserID(q.Get("userID")), contract.AgentGroupID(q.Get("agentGroupID")))
	writeJSON(w, http.StatusOK, map[string]any{"allowed": allowed, "reason": reason})
}

// apiID generates a short, monotonic id suffix for server-assigned wiring ids.
func apiID() string {
	return time.Now().UTC().Format("20060102150405.000000000")
}
