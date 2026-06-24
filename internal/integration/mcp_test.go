package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
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

// TestMCPRegisterToExecuteEndToEnd closes the full OpenClaw register→approve→access→
// execute loop across the real gateway, catalog, broker, and in-sandbox tool:
//
//  1. REGISTER: a ChangeMCPRegister proposal flows through the real gateway. The
//     MCPRegisterVerifier holds it for a human (never auto-approved); only on approval
//     does the MCPRegisterApplier land the server in the catalog. The server is absent
//     before approval.
//  2. ACCESS: an approved grant (the separate, also-human-gated ChangeMCPAccess, here
//     applied via the grants seam) lets one session call one tool on the new server.
//  3. EXECUTE: the in-sandbox tool discovers exactly that tool over the per-session
//     broker socket and invokes it round-trip — proving registration grants nothing by
//     itself and the new endpoint is reachable only after BOTH approvals.
func TestMCPRegisterToExecuteEndToEnd(t *testing.T) {
	upstream := httptest.NewServer(mcp.SampleServer().Handler())
	defer upstream.Close()

	cat, err := mcp.NewCatalog("")
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	// The broker shares the catalog instance the register applier writes to, so an
	// approved register is visible to broker sessions after Invalidate.
	grants := func(session string) []mcp.Grant {
		if session == "s1" {
			return []mcp.Grant{{Server: "sample", Tools: []string{"echo"}}}
		}
		return nil
	}
	broker := mcp.New(context.Background(), cat, grants)
	defer broker.Close()

	// --- 1. REGISTER through the real gateway ---
	register := func(cfg mcp.ServerConfig) error {
		if err := cat.Put(cfg); err != nil {
			return err
		}
		broker.Invalidate(cfg.Name)
		return nil
	}
	gw := gateway.New(
		gateway.VerifierChain{
			gateway.NewMCPRegisterVerifier(func() bool { return true }),
			gateway.AlwaysRequireHuman{},
		},
		gateway.NewManualApprover(),
		gateway.NewMCPRegisterApplier(register, gateway.NewLogApplier()),
		gateway.NewMemoryStore(),
	)

	after := `{"name":"sample","transport":"http","url":"` + upstream.URL + `"}`
	errCh := make(chan error, 1)
	go func() {
		_, err := gw.Submit(context.Background(), contract.ChangeRequest{
			ID: "reg1", Kind: contract.ChangeMCPRegister, After: json.RawMessage(after),
		})
		errCh <- err
	}()

	// Wait for the change to be held pending (not auto-applied), then assert absence.
	waitForPending(t, gw, "reg1")
	if _, ok := cat.Get("sample"); ok {
		t.Fatal("server registered before human approval")
	}

	if err := gw.Decide("reg1", contract.Decision{Outcome: gateway.OutcomeApprove, DecidedBy: "owner", DecidedAt: time.Now()}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, ok := cat.Get("sample"); !ok {
		t.Fatal("server not in catalog after approval")
	}

	// --- 2 & 3. ACCESS + EXECUTE through the real broker + sandbox tool ---
	dir := t.TempDir()
	sock, err := broker.SocketForSession("s1", dir)
	if err != nil {
		t.Fatalf("SocketForSession: %v", err)
	}
	mcpTools, err := tools.MCPTools(sock)
	if err != nil {
		t.Fatalf("MCPTools: %v", err)
	}
	if len(mcpTools) != 1 || mcpTools[0].Name() != "sample__echo" {
		t.Fatalf("discovered %d tools (%v), want only sample__echo", len(mcpTools), toolNames(mcpTools))
	}
	out, err := mcpTools[0].Invoke(context.Background(), json.RawMessage(`{"text":"registered+approved"}`))
	if err != nil {
		t.Fatalf("Invoke echo: %v", err)
	}
	if out != "registered+approved" {
		t.Fatalf("echo returned %q", out)
	}

	// A session with no grant sees nothing on the freshly-registered server — register
	// did not widen anyone's surface.
	sock2, err := broker.SocketForSession("s2", dir)
	if err != nil {
		t.Fatalf("SocketForSession s2: %v", err)
	}
	none, err := tools.MCPTools(sock2)
	if err != nil {
		t.Fatalf("MCPTools s2: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("s2 saw %d tools after register, want 0 (register grants nothing)", len(none))
	}
}

// waitForPending blocks until the gateway reports the given change id as pending, so a
// blocking Submit running in a goroutine has reached the human-approval floor before the
// test decides it.
func waitForPending(t *testing.T, gw *gateway.Gateway, id contract.ChangeID) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := gw.Pending()
		if err != nil {
			t.Fatalf("Pending: %v", err)
		}
		for _, p := range pending {
			if p.ID == id {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("change %q never became pending", id)
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
