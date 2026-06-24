package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/mcp"
)

// MCPRegisterEnabled reports whether MCP is enabled host-side (a catalog is
// configured). With MCP disabled there is nowhere for a registered server to live, so
// a register proposal is deny-by-default. Satisfied by a closure over the daemon's MCP
// catalog (cmd/controlplane).
type MCPRegisterEnabled func() bool

// MCPRegisterVerifier handles a ChangeMCPRegister — an agent's proposal to add a
// BRAND-NEW MCP server endpoint to the host catalog (RFC-0007). It is deny-by-default
// and never auto-approves:
//
//   - MCP disabled (no catalog wired) → reject. There is nowhere to register a server,
//     and an agent should not be able to turn MCP on.
//   - malformed definition (empty/invalid name, not exactly one of command/url for the
//     transport, non-https url to a non-loopback host, …) → reject, reusing the
//     catalog's own ServerConfig.Validate so the gateway and the catalog never disagree
//     on what a valid server is.
//   - otherwise → VerdictRequireHuman. Registering a server introduces a new
//     code-execution/egress surface, so a human must approve the EXACT command/args/
//     image or url/headers before it lands. It is NEVER auto-approved.
//
// Like every verifier it is additive: it runs before AlwaysRequireHuman and only ever
// adds a rejection. Other kinds pass through untouched. It is deterministic (a parse +
// in-memory policy check, no I/O).
type MCPRegisterVerifier struct {
	enabled MCPRegisterEnabled
}

// NewMCPRegisterVerifier constructs the verifier. A nil enabled check is treated as
// "MCP disabled" so a missing wiring fails closed (every register is rejected).
func NewMCPRegisterVerifier(enabled MCPRegisterEnabled) MCPRegisterVerifier {
	return MCPRegisterVerifier{enabled: enabled}
}

// Name identifies the verifier.
func (MCPRegisterVerifier) Name() string { return "mcp-register" }

// Verify enforces the deny-by-default / validate / require-human policy for a
// ChangeMCPRegister. Other kinds pass through untouched.
func (v MCPRegisterVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if req.Kind != contract.ChangeMCPRegister {
		return contract.VerdictPass, "", nil
	}
	if v.enabled == nil || !v.enabled() {
		return contract.VerdictReject, "mcp registration is disabled (no MCP catalog configured)", nil
	}
	cfg, err := parseMCPRegister(req.After)
	if err != nil {
		return contract.VerdictReject, fmt.Sprintf("mcp_register payload is malformed: %v", err), nil
	}
	if err := cfg.Validate(); err != nil {
		return contract.VerdictReject, err.Error(), nil
	}
	return contract.VerdictRequireHuman, "new MCP server endpoint — human must approve the exact command/url", nil
}

// MCPRegisterFunc lands an APPROVED MCP server in the host catalog and drops any cached
// broker connection for that name so the next use reconnects with the new config.
// Satisfied host-side by a closure over the catalog + broker (cmd/controlplane); a seam
// so the gateway stays decoupled from concrete catalog/broker wiring.
type MCPRegisterFunc func(cfg mcp.ServerConfig) error

// MCPRegisterApplier materializes an approved ChangeMCPRegister by storing the proposed
// server in the catalog. It deliberately does NOT grant the proposing agent access to
// the new server's tools — that stays the separate, also-human-gated ChangeMCPAccess
// path (least-privilege: registering an endpoint and being allowed to call it are two
// independent approvals). Every other kind, and a ChangeMCPRegister with an empty
// payload, passes through untouched.
type MCPRegisterApplier struct {
	register MCPRegisterFunc
	next     contract.Applier
}

// NewMCPRegisterApplier wraps next. register may be nil (a recognized register then
// errors rather than silently dropping); next may be nil.
func NewMCPRegisterApplier(register MCPRegisterFunc, next contract.Applier) *MCPRegisterApplier {
	return &MCPRegisterApplier{register: register, next: next}
}

// Apply stores an approved MCP server, then delegates.
func (a *MCPRegisterApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if req.Kind == contract.ChangeMCPRegister && len(req.After) > 0 {
		cfg, err := parseMCPRegister(req.After)
		if err != nil {
			return fmt.Errorf("mcp register apply: %w", err)
		}
		if a.register == nil {
			return fmt.Errorf("mcp register apply: no registrar wired")
		}
		if err := a.register(cfg); err != nil {
			return fmt.Errorf("mcp register apply: %w", err)
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}

// parseMCPRegister decodes a ChangeMCPRegister After payload into a server config.
// Disallows unknown fields so a typo'd or padded definition is a loud error rather than
// a silently-ignored field — the human approves exactly what is parsed.
func parseMCPRegister(after json.RawMessage) (mcp.ServerConfig, error) {
	var cfg mcp.ServerConfig
	dec := json.NewDecoder(bytes.NewReader(after))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return mcp.ServerConfig{}, err
	}
	return cfg, nil
}
