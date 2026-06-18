package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// transport carries JSON-RPC messages to one upstream server. Implementations:
// stdioTransport (a spawned subprocess) and httpTransport (a remote endpoint). A
// transport is used serially by Client (Client guards it with a mutex), so an
// implementation need not be internally concurrency-safe.
type transport interface {
	// roundTrip sends a request with the given method/params and returns the raw
	// result, or an error (transport failure or a JSON-RPC error from the server).
	roundTrip(ctx context.Context, id int64, method string, params any) (json.RawMessage, error)
	// notify sends a notification (no id, no response).
	notify(ctx context.Context, method string, params any) error
	// Close releases the transport (kills the subprocess / closes idle conns).
	Close() error
}

// Client is a connected MCP client for one upstream server. It is safe for
// concurrent use: every call is serialized on a mutex, matching the
// single-connection request/response model of both transports.
type Client struct {
	mu        sync.Mutex
	t         transport
	nextID    int64
	ready     bool
	server    ServerInfo
	closeOnce sync.Once
}

// newClient wraps a transport. Callers use DialStdio / DialHTTP.
func newClient(t transport) *Client {
	return &Client{t: t}
}

// id returns the next monotonically-increasing JSON-RPC id. Caller holds c.mu.
func (c *Client) id() int64 {
	c.nextID++
	return c.nextID
}

// Initialize performs the MCP handshake: an initialize request followed by the
// notifications/initialized notification. It is idempotent — a second call is a
// no-op once the connection is ready. It must complete before ListTools / CallTool.
func (c *Client) Initialize(ctx context.Context) (ServerInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready {
		return c.server, nil
	}
	raw, err := c.t.roundTrip(ctx, c.id(), "initialize", initializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      ServerInfo{Name: "ironclaw", Version: "1"},
	})
	if err != nil {
		return ServerInfo{}, fmt.Errorf("mcp: initialize: %w", err)
	}
	var res initializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return ServerInfo{}, fmt.Errorf("mcp: decode initialize result: %w", err)
	}
	// Best-effort: a server that does not accept the initialized notification (or a
	// transport that drops it) should not fail the handshake.
	_ = c.t.notify(ctx, "notifications/initialized", map[string]any{})
	c.ready = true
	c.server = res.ServerInfo
	return c.server, nil
}

// ListTools returns the tools the server exposes. Initialize must have run.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready {
		return nil, fmt.Errorf("mcp: ListTools before Initialize")
	}
	raw, err := c.t.roundTrip(ctx, c.id(), "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp: tools/list: %w", err)
	}
	var res listToolsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("mcp: decode tools/list: %w", err)
	}
	return res.Tools, nil
}

// CallTool invokes a tool by name with the given JSON arguments object. A nil/empty
// args sends an empty object. A tool-level error comes back as a ToolResult with
// IsError true (not a Go error); a Go error means the RPC/transport failed.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready {
		return ToolResult{}, fmt.Errorf("mcp: CallTool before Initialize")
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	raw, err := c.t.roundTrip(ctx, c.id(), "tools/call", callToolParams{Name: name, Arguments: args})
	if err != nil {
		return ToolResult{}, fmt.Errorf("mcp: tools/call %q: %w", name, err)
	}
	var res ToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return ToolResult{}, fmt.Errorf("mcp: decode tools/call %q result: %w", name, err)
	}
	return res, nil
}

// Close releases the underlying transport. Safe to call more than once.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.t.Close() })
	return err
}
