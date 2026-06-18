package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// mcpAccessPayload is the After shape of a ChangeMCPAccess: the named server plus the
// named subset of its tools (empty Tools = all the server's currently-declared tools).
type mcpAccessPayload struct {
	Server string   `json:"server"`
	Tools  []string `json:"tools"`
}

// SetGrantedMCPFunc records an approved MCP-access grant on a group. Satisfied
// host-side by registry.SetGrantedMCP (cmd/controlplane); a seam so the gateway stays
// decoupled from the registry package.
type SetGrantedMCPFunc func(id contract.AgentGroupID, server string, tools []string) error

// MCPAccessApplier materializes an approved ChangeMCPAccess by recording the grant on
// the target group, so the next sandbox launch exposes that server's approved tools
// through the per-session broker socket. A ChangeMCPAccess whose payload names no
// server passes through untouched, as does every other kind.
type MCPAccessApplier struct {
	set  SetGrantedMCPFunc
	next contract.Applier
}

// NewMCPAccessApplier wraps next. set may be nil (a recognized grant then errors
// rather than silently dropping); next may be nil.
func NewMCPAccessApplier(set SetGrantedMCPFunc, next contract.Applier) *MCPAccessApplier {
	return &MCPAccessApplier{set: set, next: next}
}

// Apply records an approved MCP grant, then delegates.
func (a *MCPAccessApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if req.Kind == contract.ChangeMCPAccess && len(req.After) > 0 {
		var p mcpAccessPayload
		// Best-effort: only a payload that names a server is an MCP grant; anything else
		// passes through.
		if err := json.Unmarshal(req.After, &p); err == nil && strings.TrimSpace(p.Server) != "" {
			if a.set == nil {
				return fmt.Errorf("mcp access apply: no grant setter wired")
			}
			if err := a.set(req.AgentGroupID, p.Server, p.Tools); err != nil {
				return fmt.Errorf("mcp access apply: %w", err)
			}
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}

// MCPServerKnown reports whether a server name is configured in the host MCP catalog.
type MCPServerKnown func(server string) bool

// MCPServerVerifier rejects a ChangeMCPAccess that names a server the host has not
// configured — a grant for an unknown server could never work and is almost certainly
// a mistake or an injection attempt. It is deterministic (an in-memory catalog
// membership check, no I/O) and additive like every verifier: it runs before the
// human floor and only ever ADDS a rejection. Tool-level validity is NOT checked here
// (that needs a live probe and is not deterministic); the broker enforces it at call
// time by refusing to call any tool the server does not declare.
type MCPServerVerifier struct {
	known MCPServerKnown
}

// NewMCPServerVerifier constructs the verifier. A nil known check passes every server
// (the catalog is then unenforced — used only where no catalog is wired).
func NewMCPServerVerifier(known MCPServerKnown) MCPServerVerifier {
	return MCPServerVerifier{known: known}
}

// Name identifies the verifier.
func (MCPServerVerifier) Name() string { return "mcp-server" }

// Verify rejects a grant for an unconfigured server. It applies only to
// ChangeMCPAccess; other kinds pass through untouched.
func (v MCPServerVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if req.Kind != contract.ChangeMCPAccess {
		return contract.VerdictPass, "", nil
	}
	var p mcpAccessPayload
	if err := json.Unmarshal(req.After, &p); err != nil || strings.TrimSpace(p.Server) == "" {
		return contract.VerdictReject, "mcp_access payload must name a server", nil
	}
	if v.known != nil && !v.known(p.Server) {
		return contract.VerdictReject, fmt.Sprintf("mcp server %q is not configured", p.Server), nil
	}
	return contract.VerdictPass, "mcp server configured", nil
}
