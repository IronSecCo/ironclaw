// OWNER: AGENT2

// Package tools holds the in-sandbox tool implementations.
//
// There are deliberately NO self-edit, install_packages, or add_mcp_server tools:
// capability changes are control-plane mutations and happen only via the host
// gateway. A tool that needs privilege emits a gateway ChangeRequest — it never
// acts directly.
package tools

// Tool is an in-sandbox tool the agent may invoke.
type Tool interface {
	Name() string
}
