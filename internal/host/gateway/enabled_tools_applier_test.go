package gateway

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
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
