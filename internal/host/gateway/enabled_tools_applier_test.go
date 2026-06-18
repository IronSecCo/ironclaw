package gateway

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func TestEnabledToolsApplierStoresThenDelegates(t *testing.T) {
	var gotID contract.AgentGroupID
	var gotTools []string
	set := func(id contract.AgentGroupID, tools []string) error {
		gotID, gotTools = id, tools
		return nil
	}
	next := &countingApplier{}
	a := NewEnabledToolsApplier(set, next)

	after, _ := json.Marshal(map[string][]string{"tools": {"http_fetch", "send_message"}})
	req := contract.ChangeRequest{Kind: contract.ChangeEnabledTools, AgentGroupID: "grp-1", After: after}
	if err := a.Apply(context.Background(), req, contract.Decision{Outcome: "approve"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if gotID != "grp-1" || !reflect.DeepEqual(gotTools, []string{"http_fetch", "send_message"}) {
		t.Fatalf("setter got (%q, %v)", gotID, gotTools)
	}
	if next.n != 1 {
		t.Errorf("next must be called once (n=%d)", next.n)
	}
}

func TestEnabledToolsApplierAcceptsBareArray(t *testing.T) {
	// The web config editor (ui_config.go) submits enabled_tools as a bare JSON array.
	var got []string
	a := NewEnabledToolsApplier(func(_ contract.AgentGroupID, tools []string) error { got = tools; return nil }, &countingApplier{})
	after, _ := json.Marshal([]string{"http_fetch", "send_message"})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools, After: after}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"http_fetch", "send_message"}) {
		t.Fatalf("bare-array payload not applied, got %v", got)
	}
}

func TestEnabledToolsApplierIgnoresOtherKinds(t *testing.T) {
	called := false
	a := NewEnabledToolsApplier(func(contract.AgentGroupID, []string) error { called = true; return nil }, &countingApplier{})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called {
		t.Error("a non-enabled_tools kind must not call the setter")
	}
}

func TestEnabledToolsApplierBadPayload(t *testing.T) {
	a := NewEnabledToolsApplier(func(contract.AgentGroupID, []string) error { return nil }, nil)
	err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools, After: []byte("nope")}, contract.Decision{})
	if err == nil {
		t.Fatal("malformed enabled_tools payload must error")
	}
}

// TestEnabledToolsApplierAddUnionsIntoRestrictedSet covers the agent's additive form:
// {"add":[...]} unions into the group's current restricted set without clobbering it.
func TestEnabledToolsApplierAddUnionsIntoRestrictedSet(t *testing.T) {
	var got []string
	a := NewEnabledToolsApplier(
		func(_ contract.AgentGroupID, tools []string) error { got = tools; return nil },
		&countingApplier{},
	).WithCurrentTools(func(contract.AgentGroupID) []string { return []string{"read_file", "send_message"} })

	after, _ := json.Marshal(map[string][]string{"add": {"web_search", "read_file"}})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools, AgentGroupID: "g", After: after}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Union preserves current order, appends the genuinely-new tool, dedups read_file.
	if !reflect.DeepEqual(got, []string{"read_file", "send_message", "web_search"}) {
		t.Fatalf("add union = %v, want [read_file send_message web_search]", got)
	}
}

// TestEnabledToolsApplierAddOnPermissiveIsNoOp asserts adding a tool to a permissive
// group (empty set = all tools) does NOT collapse it to a one-tool restriction.
func TestEnabledToolsApplierAddOnPermissiveIsNoOp(t *testing.T) {
	called := false
	a := NewEnabledToolsApplier(
		func(contract.AgentGroupID, []string) error { called = true; return nil },
		&countingApplier{},
	).WithCurrentTools(func(contract.AgentGroupID) []string { return nil }) // permissive

	after, _ := json.Marshal(map[string][]string{"add": {"web_search"}})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools, After: after}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called {
		t.Fatal("adding a tool to a permissive group must be a no-op, not a restriction")
	}
}

// TestEnabledToolsApplierAddNeedsReader asserts the additive form errors when no
// current-tools reader is wired (rather than silently mis-applying).
func TestEnabledToolsApplierAddNeedsReader(t *testing.T) {
	a := NewEnabledToolsApplier(func(contract.AgentGroupID, []string) error { return nil }, nil)
	after, _ := json.Marshal(map[string][]string{"add": {"web_search"}})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools, After: after}, contract.Decision{}); err == nil {
		t.Fatal("additive payload without a current-tools reader must error")
	}
}
