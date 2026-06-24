package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/mcp"
)

func TestMCPRegisterVerifier(t *testing.T) {
	stdio := `{"name":"local","transport":"stdio","command":"server"}`
	httpsRemote := `{"name":"remote","transport":"http","url":"https://mcp.example.com"}`

	cases := []struct {
		name    string
		enabled bool
		req     contract.ChangeRequest
		want    contract.Verdict
	}{
		{"disabled rejects valid register", false,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(stdio)}, contract.VerdictReject},
		{"valid stdio requires human", true,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(stdio)}, contract.VerdictRequireHuman},
		{"valid https requires human", true,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(httpsRemote)}, contract.VerdictRequireHuman},
		{"empty name rejected", true,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(`{"transport":"stdio","command":"x"}`)}, contract.VerdictReject},
		{"stdio without command rejected", true,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(`{"name":"local","transport":"stdio"}`)}, contract.VerdictReject},
		{"both command and url rejected", true,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(`{"name":"x","transport":"stdio","command":"c","url":"https://e.com"}`)}, contract.VerdictReject},
		{"http non-loopback http url rejected", true,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(`{"name":"x","transport":"http","url":"http://mcp.example.com"}`)}, contract.VerdictReject},
		{"unknown field rejected", true,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(`{"name":"x","transport":"stdio","command":"c","evil":true}`)}, contract.VerdictReject},
		{"unparseable rejected", true,
			contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(`not json`)}, contract.VerdictReject},
		{"other kind passes", true,
			contract.ChangeRequest{Kind: contract.ChangePersona, After: json.RawMessage(`{}`)}, contract.VerdictPass},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewMCPRegisterVerifier(func() bool { return c.enabled })
			got, _, err := v.Verify(context.Background(), c.req)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if got != c.want {
				t.Fatalf("verdict = %v, want %v", got, c.want)
			}
		})
	}
}

// TestMCPRegisterVerifier_NilEnabledFailsClosed asserts a missing enabled wiring is
// treated as disabled (deny-by-default), never as "allowed".
func TestMCPRegisterVerifier_NilEnabledFailsClosed(t *testing.T) {
	v := NewMCPRegisterVerifier(nil)
	got, _, err := v.Verify(context.Background(),
		contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(`{"name":"x","transport":"stdio","command":"c"}`)})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != contract.VerdictReject {
		t.Fatalf("verdict = %v, want VerdictReject (nil enabled must fail closed)", got)
	}
}

func TestMCPRegisterApplier_StoresServer(t *testing.T) {
	var got mcp.ServerConfig
	register := func(cfg mcp.ServerConfig) error { got = cfg; return nil }
	a := NewMCPRegisterApplier(register, nil)

	req := contract.ChangeRequest{
		Kind:  contract.ChangeMCPRegister,
		After: json.RawMessage(`{"name":"local","transport":"stdio","command":"server","args":["--port","0"]}`),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.Name != "local" || got.Transport != mcp.TransportStdio || got.Command != "server" || len(got.Args) != 2 {
		t.Fatalf("registered config = %+v", got)
	}

	// A non-register change passes through without touching the registrar.
	got = mcp.ServerConfig{}
	other := contract.ChangeRequest{Kind: contract.ChangePersona, After: json.RawMessage(`{"instructions":"x"}`)}
	if err := a.Apply(context.Background(), other, contract.Decision{}); err != nil {
		t.Fatalf("Apply persona: %v", err)
	}
	if got.Name != "" {
		t.Fatal("persona change should not invoke the MCP registrar")
	}
}

func TestMCPRegisterApplier_NilRegistrarErrors(t *testing.T) {
	a := NewMCPRegisterApplier(nil, nil)
	req := contract.ChangeRequest{Kind: contract.ChangeMCPRegister, After: json.RawMessage(`{"name":"x","transport":"stdio","command":"c"}`)}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err == nil {
		t.Fatal("expected an error when no registrar is wired")
	}
}

// TestMCPRegisterEndToEnd_GatewayToCatalog drives a register proposal through the real
// gateway: the verifier holds it for a human, and only on approval does the applier land
// the server in a real catalog. It also asserts the server is NOT in the catalog before
// approval (no auto-apply).
func TestMCPRegisterEndToEnd_GatewayToCatalog(t *testing.T) {
	cat, err := mcp.NewCatalog("")
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	register := func(cfg mcp.ServerConfig) error { return cat.Put(cfg) }
	verifier := NewMCPRegisterVerifier(func() bool { return true })
	applier := NewMCPRegisterApplier(register, NewLogApplier())
	store := NewMemoryStore()
	gw := New(VerifierChain{verifier, AlwaysRequireHuman{}}, NewManualApprover(), applier, store)

	errCh := make(chan error, 1)
	go func() {
		_, err := gw.Submit(context.Background(), contract.ChangeRequest{
			ID:    "reg1",
			Kind:  contract.ChangeMCPRegister,
			After: json.RawMessage(`{"name":"weather","transport":"http","url":"https://weather.example.com/mcp"}`),
		})
		errCh <- err
	}()
	waitPending(t, store, "reg1")

	// Before approval, the server must NOT exist in the catalog.
	if _, ok := cat.Get("weather"); ok {
		t.Fatal("server landed in catalog before human approval")
	}

	if err := gw.Decide("reg1", contract.Decision{Outcome: OutcomeApprove, DecidedBy: "owner", DecidedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if st, _ := store.Status("reg1"); st != string(statusApplied) {
		t.Fatalf("status = %q, want applied", st)
	}
	got, ok := cat.Get("weather")
	if !ok {
		t.Fatal("server not in catalog after approval")
	}
	if got.Transport != mcp.TransportHTTP || got.URL != "https://weather.example.com/mcp" {
		t.Fatalf("registered server = %+v", got)
	}
}
