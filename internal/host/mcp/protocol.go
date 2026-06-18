// Package mcp is the host-side Model Context Protocol implementation IronClaw uses
// to extend agents with externally-served tools WITHOUT giving the sandbox network
// access or a runtime to run MCP servers in.
//
// It has three parts:
//
//   - protocol.go / client.go / transport_*.go — a minimal MCP CLIENT (JSON-RPC
//     2.0) over two transports: stdio (a local subprocess the host spawns) and
//     streamable HTTP (a remote endpoint the host dials). It speaks just the methods
//     tool-use needs: initialize, notifications/initialized, tools/list, tools/call.
//   - server.go — a tiny reusable MCP SERVER used by cmd/mcp-sample and the tests, so
//     the client can be exercised end to end in-process and a real local server is a
//     runnable artifact.
//   - broker.go / catalog.go — the host BROKER: it owns the upstream client
//     connections (spawn stdio / dial HTTP), a catalog of operator-configured
//     servers, and a per-session deny-by-default HTTP-over-unix-socket shim the
//     sandbox reads its approved tool surface from. MCP never runs in the sandbox;
//     the sandbox stays network=none and only ever talks to the broker socket.
//
// The package is pure stdlib (the whole control-plane tree is), so adding MCP pulls
// in no third-party dependency.
package mcp

import "encoding/json"

// ProtocolVersion is the MCP revision this client advertises in initialize. Servers
// that speak a different revision still interoperate for the tools/* methods used
// here; the field is informational for the handshake.
const ProtocolVersion = "2025-03-26"

// Tool is one tool a server exposes (the tools/list element). InputSchema is the
// JSON Schema for the tool's arguments, passed through verbatim — the model sees the
// server's own schema.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// Content is one block of a tool result. Only "text" is modeled; other block types
// (image, resource) are rendered by the broker as a short placeholder so the agent
// still sees that non-text content was returned.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult is the result of tools/call. IsError reports a tool-level error (the
// JSON-RPC call itself succeeded) so the agent gets the error text rather than a
// failed turn.
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError"`
}

// Text concatenates the text blocks of a result with newlines, substituting a short
// placeholder for any non-text block so the agent always gets a usable string.
func (r ToolResult) Text() string {
	var b []byte
	for i, c := range r.Content {
		if i > 0 {
			b = append(b, '\n')
		}
		switch c.Type {
		case "text", "":
			b = append(b, c.Text...)
		default:
			b = append(b, "["+c.Type+" content]"...)
		}
	}
	return string(b)
}

// ServerInfo is the serverInfo block returned by initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// --- JSON-RPC 2.0 envelopes ---

// rpcRequest is a JSON-RPC request or notification. ID is nil for a notification
// (no response expected).
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC response. Exactly one of Result / Error is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC error object.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// initializeParams / initializeResult are the handshake payloads.
type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ServerInfo     `json:"clientInfo"`
}

type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

// listToolsResult is the tools/list result.
type listToolsResult struct {
	Tools []Tool `json:"tools"`
}

// callToolParams is the tools/call params.
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}
