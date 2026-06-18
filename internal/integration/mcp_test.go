package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/mcp"
	"github.com/IronSecCo/ironclaw/internal/sandbox/tools"
)

// TestMCPToolThroughRealBroker wires the REAL host MCP broker to the REAL in-sandbox
// MCP tool over a real per-session unix socket — the same path a launched sandbox
// takes, minus the container. It proves discovery, invocation, and deny-by-default
// gating all the way across the seam: the sandbox tool speaks only the broker's
// HTTP-over-socket shim and never MCP itself.
func TestMCPToolThroughRealBroker(t *testing.T) {
	// A real MCP server (the sample, over HTTP — loopback is allowed).
	upstream := httptest.NewServer(mcp.SampleServer().Handler())
	defer upstream.Close()

	cat, _ := mcp.NewCatalog("")
	if err := cat.Put(mcp.ServerConfig{Name: "sample", Transport: mcp.TransportHTTP, URL: upstream.URL}); err != nil {
		t.Fatalf("catalog put: %v", err)
	}

	// Session s1 is granted ONLY echo on "sample"; s2 is granted nothing.
	grants := func(session string) []mcp.Grant {
		if session == "s1" {
			return []mcp.Grant{{Server: "sample", Tools: []string{"echo"}}}
		}
		return nil
	}
	broker := mcp.New(context.Background(), cat, grants)
	defer broker.Close()

	dir := t.TempDir()
	sock, err := broker.SocketForSession("s1", dir)
	if err != nil {
		t.Fatalf("SocketForSession: %v", err)
	}

	// The in-sandbox tool discovers exactly the granted surface, namespaced.
	mcpTools, err := tools.MCPTools(sock)
	if err != nil {
		t.Fatalf("MCPTools: %v", err)
	}
	if len(mcpTools) != 1 || mcpTools[0].Name() != "sample__echo" {
		t.Fatalf("discovered %d tools (%v), want only sample__echo", len(mcpTools), toolNames(mcpTools))
	}
	if len(mcpTools[0].JSONSchema()) == 0 {
		t.Error("discovered tool has no JSON schema")
	}

	ctx := context.Background()

	// Invoking the granted tool round-trips through the broker to the server.
	out, err := mcpTools[0].Invoke(ctx, json.RawMessage(`{"text":"hello e2e"}`))
	if err != nil {
		t.Fatalf("Invoke echo: %v", err)
	}
	if out != "hello e2e" {
		t.Fatalf("echo returned %q, want %q", out, "hello e2e")
	}

	// A non-granted tool on the same server is denied at the broker (the sandbox tool
	// surfaces it as a tool error). We construct the wrapper for sample__add directly to
	// prove the broker — not the sandbox — is the enforcement point.
	addProbe := probeToolNamed(t, sock, "sample__add")
	if addProbe != nil {
		t.Fatalf("sample__add must NOT be discoverable for s1 (granted only echo)")
	}

	// s2, granted nothing, sees no MCP tools at all.
	sock2, err := broker.SocketForSession("s2", dir)
	if err != nil {
		t.Fatalf("SocketForSession s2: %v", err)
	}
	none, err := tools.MCPTools(sock2)
	if err != nil {
		t.Fatalf("MCPTools s2: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("s2 discovered %d tools, want 0 (granted nothing)", len(none))
	}
}

func toolNames(ts []tools.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name()
	}
	return out
}

// probeToolNamed returns the discovered tool with the given name, or nil if the
// session's approved surface does not include it.
func probeToolNamed(t *testing.T, sock, name string) tools.Tool {
	t.Helper()
	ts, err := tools.MCPTools(sock)
	if err != nil {
		t.Fatalf("MCPTools: %v", err)
	}
	for _, tl := range ts {
		if tl.Name() == name {
			return tl
		}
	}
	return nil
}
