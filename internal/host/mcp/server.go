package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// ToolFunc implements one server tool: it receives the raw JSON arguments and
// returns a result. A returned error becomes an IsError ToolResult carrying the
// error text (a tool error, not an RPC error).
type ToolFunc func(ctx context.Context, args json.RawMessage) (ToolResult, error)

// Server is a minimal MCP server: a tool set plus the initialize/tools handshake,
// served over stdio (ServeStdio) or HTTP (Handler). It backs cmd/mcp-sample and the
// tests, so the client is exercised against a real server in-process. It is NOT the
// broker — the broker is a CLIENT of servers like this.
type Server struct {
	info     ServerInfo
	mu       sync.RWMutex
	order    []string
	tools    map[string]Tool
	handlers map[string]ToolFunc
}

// NewServer constructs an empty server with the given identity.
func NewServer(name, version string) *Server {
	return &Server{
		info:     ServerInfo{Name: name, Version: version},
		tools:    map[string]Tool{},
		handlers: map[string]ToolFunc{},
	}
}

// AddTool registers a tool and its handler. A duplicate name replaces the prior one.
func (s *Server) AddTool(t Tool, fn ToolFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tools[t.Name]; !exists {
		s.order = append(s.order, t.Name)
	}
	s.tools[t.Name] = t
	s.handlers[t.Name] = fn
}

// Tools returns the registered tool metadata in registration order. It is a
// read-only snapshot used by callers and tests to inspect the exposed tool set.
func (s *Server) Tools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tool, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.tools[n])
	}
	return out
}

// dispatch routes one request method to a JSON result. ok=false means "no response"
// (a notification). An error is a JSON-RPC error to return to the caller.
func (s *Server) dispatch(ctx context.Context, method string, params json.RawMessage) (result any, ok bool, err *rpcError) {
	switch method {
	case "initialize":
		return initializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      s.info,
		}, true, nil
	case "notifications/initialized":
		return nil, false, nil
	case "ping":
		return map[string]any{}, true, nil
	case "tools/list":
		s.mu.RLock()
		list := make([]Tool, 0, len(s.order))
		for _, n := range s.order {
			list = append(list, s.tools[n])
		}
		s.mu.RUnlock()
		return listToolsResult{Tools: list}, true, nil
	case "tools/call":
		var p callToolParams
		if e := json.Unmarshal(params, &p); e != nil {
			return nil, true, &rpcError{Code: -32602, Message: "invalid params: " + e.Error()}
		}
		s.mu.RLock()
		fn, found := s.handlers[p.Name]
		s.mu.RUnlock()
		if !found {
			return nil, true, &rpcError{Code: -32601, Message: "unknown tool: " + p.Name}
		}
		res, e := fn(ctx, p.Arguments)
		if e != nil {
			return ToolResult{Content: []Content{{Type: "text", Text: e.Error()}}, IsError: true}, true, nil
		}
		return res, true, nil
	default:
		return nil, true, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

// ServeStdio runs the newline-delimited JSON-RPC loop over in/out until in reaches
// EOF or ctx is cancelled. It is the local-server transport the broker spawns.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	enc := json.NewEncoder(out)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var req rpcRequest
			if json.Unmarshal(line, &req) == nil && req.Method != "" {
				result, ok, rerr := s.dispatch(ctx, req.Method, rawParams(req.Params))
				if req.ID != nil { // a request (not a notification) expects a response
					resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
					if rerr != nil {
						resp.Error = rerr
					} else if ok {
						resp.Result = mustRaw(result)
					}
					if err := enc.Encode(resp); err != nil {
						return err
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// Handler serves the streamable-HTTP transport: a POSTed JSON-RPC message in, a
// single application/json response out (202 for a notification).
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, maxHTTPResponseBytes)).Decode(&req); err != nil {
			http.Error(w, "invalid JSON-RPC", http.StatusBadRequest)
			return
		}
		result, ok, rerr := s.dispatch(r.Context(), req.Method, rawParams(req.Params))
		if req.ID == nil { // notification: no body
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rerr != nil {
			resp.Error = rerr
		} else if ok {
			resp.Result = mustRaw(result)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// rawParams normalizes a decoded params value back to json.RawMessage for dispatch.
func rawParams(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	b, _ := json.Marshal(v)
	return b
}

// mustRaw marshals a result value to json.RawMessage (best-effort; a marshal failure
// yields null, which decodes cleanly on the client).
func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// TextResult is a convenience for a tool that returns a single text block.
func TextResult(text string) ToolResult {
	return ToolResult{Content: []Content{{Type: "text", Text: text}}}
}

// SampleServer returns a tiny MCP server with two pure tools — echo and add — used
// by cmd/mcp-sample and the tests as a credential-free, network-free local server.
func SampleServer() *Server {
	s := NewServer("ironclaw-mcp-sample", "1.0.0")
	s.AddTool(Tool{
		Name:        "echo",
		Description: "Echo back the provided text.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string","description":"Text to echo."}},"required":["text"],"additionalProperties":false}`),
	}, func(_ context.Context, args json.RawMessage) (ToolResult, error) {
		var in struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return ToolResult{}, fmt.Errorf("echo: invalid arguments: %w", err)
		}
		return TextResult(in.Text), nil
	})
	s.AddTool(Tool{
		Name:        "add",
		Description: "Add two numbers and return the sum.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"],"additionalProperties":false}`),
	}, func(_ context.Context, args json.RawMessage) (ToolResult, error) {
		var in struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return ToolResult{}, fmt.Errorf("add: invalid arguments: %w", err)
		}
		return TextResult(fmt.Sprintf("%v", in.A+in.B)), nil
	})
	return s
}
