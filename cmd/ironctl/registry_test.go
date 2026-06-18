package main

import (
	"net/http/httptest"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/api"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// newIronctlServer spins up the real control-plane API over a MemRegistry so the
// ironctl subcommands run end-to-end against the actual endpoints.
func newIronctlServer(t *testing.T) (*httptest.Server, *registry.MemRegistry) {
	t.Helper()
	reg := registry.NewMemRegistry()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	srv := httptest.NewServer(api.New(gw).WithRegistry(reg).Handler())
	t.Cleanup(srv.Close)
	return srv, reg
}

func TestIronctlRegistryAgentGroupAndRoles(t *testing.T) {
	srv, reg := newIronctlServer(t)

	if err := run([]string{"--addr", srv.URL, "registry", "agent-group", "put", "--id", "ag1", "--name", "Alpha", "--folder", "/a"}); err != nil {
		t.Fatalf("agent-group put: %v", err)
	}
	if g, ok := reg.GetAgentGroup("ag1"); !ok || g.Name != "Alpha" || g.Folder != "/a" {
		t.Fatalf("agent group not created via ironctl: %+v ok=%v", g, ok)
	}
	if err := run([]string{"--addr", srv.URL, "registry", "agent-group", "get", "--id", "ag1"}); err != nil {
		t.Fatalf("agent-group get: %v", err)
	}

	// Grant a scoped admin role, confirm access, then revoke it.
	if err := run([]string{"--addr", srv.URL, "registry", "role", "grant", "--user", "slack:u1", "--role", "admin", "--agent", "ag1"}); err != nil {
		t.Fatalf("role grant: %v", err)
	}
	if ok, reason := reg.CanAccess("slack:u1", "ag1"); !ok || reason != "scoped-admin" {
		t.Fatalf("access after grant: ok=%v reason=%q", ok, reason)
	}
	if err := run([]string{"--addr", srv.URL, "registry", "role", "revoke", "--user", "slack:u1", "--role", "admin", "--agent", "ag1"}); err != nil {
		t.Fatalf("role revoke: %v", err)
	}
	if ok, _ := reg.CanAccess("slack:u1", "ag1"); ok {
		t.Fatal("access should be revoked via ironctl")
	}
}

func TestIronctlRegistryMessagingGroupAndWiring(t *testing.T) {
	srv, reg := newIronctlServer(t)

	if err := run([]string{"--addr", srv.URL, "registry", "messaging-group", "create", "--channel", "slack", "--platform", "C1", "--group"}); err != nil {
		t.Fatalf("messaging-group create: %v", err)
	}
	// Resolve the id the create assigned (idempotent by triple).
	mg, err := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownStrict)
	if err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"--addr", srv.URL, "registry", "wiring", "create", "--mg", string(mg.ID), "--agent", "ag1", "--engage", "mention", "--session", "shared", "--priority", "3"}); err != nil {
		t.Fatalf("wiring create: %v", err)
	}
	ws, _ := reg.ListWirings(mg.ID)
	if len(ws) != 1 || ws[0].AgentGroupID != "ag1" || ws[0].EngageMode != contract.EngageMention || ws[0].Priority != 3 {
		t.Fatalf("wiring not created via ironctl: %+v", ws)
	}
}

func TestIronctlRegistryDestinationAndMember(t *testing.T) {
	srv, reg := newIronctlServer(t)

	if err := run([]string{"--addr", srv.URL, "registry", "destination", "add", "--agent", "ag1", "--channel", "slack", "--platform", "C2"}); err != nil {
		t.Fatalf("destination add: %v", err)
	}
	if !reg.IsAllowedDestination("ag1", "slack", "C2") {
		t.Fatal("destination not added via ironctl")
	}
	if err := run([]string{"--addr", srv.URL, "registry", "destination", "check", "--agent", "ag1", "--channel", "slack", "--platform", "C2"}); err != nil {
		t.Fatalf("destination check: %v", err)
	}

	if err := run([]string{"--addr", srv.URL, "registry", "member", "add", "--user", "slack:u2", "--agent", "ag1"}); err != nil {
		t.Fatalf("member add: %v", err)
	}
	if ok, reason := reg.CanAccess("slack:u2", "ag1"); !ok || reason != "member" {
		t.Fatalf("member access via ironctl: ok=%v reason=%q", ok, reason)
	}
	if err := run([]string{"--addr", srv.URL, "registry", "member", "remove", "--user", "slack:u2", "--agent", "ag1"}); err != nil {
		t.Fatalf("member remove: %v", err)
	}
	if ok, _ := reg.CanAccess("slack:u2", "ag1"); ok {
		t.Fatal("member access should be gone via ironctl")
	}
}

func TestIronctlRegistryUsageErrors(t *testing.T) {
	srv, _ := newIronctlServer(t)
	// Missing required flag errors before any network call.
	if err := run([]string{"--addr", srv.URL, "registry", "agent-group", "put"}); err == nil {
		t.Fatal("expected error for missing --id")
	}
	// Unknown resource errors.
	if err := run([]string{"--addr", srv.URL, "registry", "bogus"}); err == nil {
		t.Fatal("expected error for unknown registry resource")
	}
	// No resource errors.
	if err := run([]string{"--addr", srv.URL, "registry"}); err == nil {
		t.Fatal("expected error for missing registry resource")
	}
}
