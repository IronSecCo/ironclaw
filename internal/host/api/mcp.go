package api

import (
	"net/http"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/mcp"
)

// The MCP endpoints are split like the rest of the admin surface:
//   - /v1/registry/mcp-servers — operator CRUD over the SERVER CATALOG (infrastructure
//     config, a direct write like messaging-groups). Configuring a server grants no
//     agent anything.
//   - /v1/registry/mcp-servers/{name}/probe — connect + discover a server's tools, so
//     the console can show the operator what to grant.
//   - /v1/ui/mcp-servers — the console read-model: servers (secrets masked) plus which
//     agent groups are granted each.
//
// GRANTING an agent access is NOT here: it is a gateway-approved capability change
// (kind mcp_access) submitted through the existing POST /v1/ui/config/change, so it
// lands on the human-approval floor like every other capability mutation.
//
// All endpoints are guarded on a configured catalog (WithMCP); with MCP disabled they
// return 503, so the default daemon exposes no MCP surface.

// WithMCP attaches the MCP catalog + broker so the MCP endpoints become live. nil
// (the default) leaves MCP disabled. Returns the Server for chaining.
func (s *Server) WithMCP(catalog *mcp.Catalog, broker *mcp.Broker) *Server {
	s.mcpCatalog = catalog
	s.mcpBroker = broker
	return s
}

func (s *Server) mcpRoutes() {
	s.mux.HandleFunc("GET /v1/registry/mcp-servers", s.handleListMCPServers)
	s.mux.HandleFunc("PUT /v1/registry/mcp-servers/{name}", s.handlePutMCPServer)
	s.mux.HandleFunc("GET /v1/registry/mcp-servers/{name}", s.handleGetMCPServer)
	s.mux.HandleFunc("DELETE /v1/registry/mcp-servers/{name}", s.handleDeleteMCPServer)
	s.mux.HandleFunc("POST /v1/registry/mcp-servers/{name}/probe", s.handleProbeMCPServer)
	s.mux.HandleFunc("GET /v1/ui/mcp-servers", s.handleUIMCPServers)
}

// mcpReady reports whether MCP is enabled; if not it writes 503 and returns false.
func (s *Server) mcpReady(w http.ResponseWriter) bool {
	if s.mcpCatalog == nil {
		http.Error(w, "MCP is not enabled on this control-plane (set --mcp-catalog)", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// handleListMCPServers returns every configured server with secrets masked.
func (s *Server) handleListMCPServers(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	servers := s.mcpCatalog.List()
	out := make([]mcp.ServerConfig, 0, len(servers))
	for _, c := range servers {
		out = append(out, c.Public())
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePutMCPServer creates or updates a server (the path name is authoritative). A
// secret value submitted as the mask placeholder is treated as "unchanged" and the
// stored value is preserved, so editing a server without re-typing its secrets is
// safe. The broker's cached connection is invalidated so the next use reconnects with
// the new config.
func (s *Server) handlePutMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	var cfg mcp.ServerConfig
	if !decodeJSON(w, r, &cfg) {
		return
	}
	cfg.Name = r.PathValue("name") // path is authoritative
	if prev, ok := s.mcpCatalog.Get(cfg.Name); ok {
		cfg.Env = mergeMaskedSecrets(cfg.Env, prev.Env)
		cfg.Headers = mergeMaskedSecrets(cfg.Headers, prev.Headers)
	}
	if err := s.mcpCatalog.Put(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.mcpBroker != nil {
		s.mcpBroker.Invalidate(cfg.Name)
	}
	writeJSON(w, http.StatusOK, cfg.Public())
}

// handleGetMCPServer returns one server (secrets masked).
func (s *Server) handleGetMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	c, ok := s.mcpCatalog.Get(r.PathValue("name"))
	if !ok {
		http.Error(w, "mcp server not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, c.Public())
}

// handleDeleteMCPServer removes a server from the catalog. It does NOT revoke
// already-approved grants (those are a separate gateway change); the broker simply
// can no longer reach a removed server.
func (s *Server) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	name := r.PathValue("name")
	if err := s.mcpCatalog.Delete(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.mcpBroker != nil {
		s.mcpBroker.Invalidate(name)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProbeMCPServer connects to a configured server and returns its declared
// tools, for the console's "test connection / discover tools" action. It is an
// operator action on infrastructure — it does not consult grants.
func (s *Server) handleProbeMCPServer(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	if s.mcpBroker == nil {
		http.Error(w, "mcp broker unavailable", http.StatusServiceUnavailable)
		return
	}
	tools, err := s.mcpBroker.Probe(r.Context(), r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if tools == nil {
		tools = []mcp.Tool{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

// mcpServerView is the console read-model: a (masked) server plus the agent groups
// that have a gateway-approved grant to it.
type mcpServerView struct {
	Server mcp.ServerConfig `json:"server"`
	Grants []mcpGrantView   `json:"grants"`
}

type mcpGrantView struct {
	AgentGroupID   contract.AgentGroupID `json:"agentGroupId"`
	AgentGroupName string                `json:"agentGroupName,omitempty"`
	Tools          []string              `json:"tools,omitempty"` // empty = all the server's tools
}

// handleUIMCPServers returns the console read-model: every configured server (secrets
// masked) and the agent groups granted it.
func (s *Server) handleUIMCPServers(w http.ResponseWriter, r *http.Request) {
	if !s.mcpReady(w) {
		return
	}
	grantsByServer := map[string][]mcpGrantView{}
	if s.reg != nil {
		for _, g := range s.reg.ListAgentGroups() {
			for _, gr := range g.GrantedMCP {
				grantsByServer[gr.Server] = append(grantsByServer[gr.Server], mcpGrantView{
					AgentGroupID:   g.ID,
					AgentGroupName: g.Name,
					Tools:          gr.Tools,
				})
			}
		}
	}
	servers := s.mcpCatalog.List()
	out := make([]mcpServerView, 0, len(servers))
	for _, c := range servers {
		out = append(out, mcpServerView{Server: c.Public(), Grants: grantsByServer[c.Name]})
	}
	writeJSON(w, http.StatusOK, out)
}

// mergeMaskedSecrets replaces any value in incoming that equals the mask placeholder
// with the corresponding stored value from prev, so an edit that leaves masked
// secrets untouched preserves them rather than overwriting them with "••••".
func mergeMaskedSecrets(incoming, prev map[string]string) map[string]string {
	if len(incoming) == 0 {
		return incoming
	}
	out := make(map[string]string, len(incoming))
	for k, v := range incoming {
		if v == mcp.MaskString {
			if pv, ok := prev[k]; ok {
				out[k] = pv
				continue
			}
		}
		out[k] = v
	}
	return out
}
