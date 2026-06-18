package registry

import (
	"fmt"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// SetGrantedMCP records (or updates) an MCP-server access grant on a group, so the
// next sandbox launch exposes that server's approved tools through the host broker.
// It is the host-side seam the gateway's mcp-access applier calls AFTER a human
// approves a ChangeMCPAccess; the sandbox can never reach it. A grant for a server
// already present is replaced in place (one grant per server per group), so
// re-approving with a different tool subset updates rather than duplicates. An empty
// tools slice means "all the server's declared tools". Returns an error if the group
// does not exist or the server name is empty.
func SetGrantedMCP(r Registry, id contract.AgentGroupID, server string, tools []string) error {
	if server == "" {
		return fmt.Errorf("registry: mcp grant requires a server name")
	}
	g, ok := r.GetAgentGroup(id)
	if !ok {
		return fmt.Errorf("registry: agent group %q not found", id)
	}
	grant := GrantedMCPServer{Server: server, Tools: tools}
	for i := range g.GrantedMCP {
		if g.GrantedMCP[i].Server == server {
			g.GrantedMCP[i] = grant // replace in place
			return r.PutAgentGroup(g)
		}
	}
	g.GrantedMCP = append(g.GrantedMCP, grant)
	return r.PutAgentGroup(g)
}

// RevokeGrantedMCP removes a server's grant from a group (idempotent). Returns an
// error only if the group does not exist.
func RevokeGrantedMCP(r Registry, id contract.AgentGroupID, server string) error {
	g, ok := r.GetAgentGroup(id)
	if !ok {
		return fmt.Errorf("registry: agent group %q not found", id)
	}
	out := g.GrantedMCP[:0]
	for _, gr := range g.GrantedMCP {
		if gr.Server != server {
			out = append(out, gr)
		}
	}
	g.GrantedMCP = out
	return r.PutAgentGroup(g)
}
