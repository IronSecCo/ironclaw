package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestPersonaApplierStoresThenDelegates(t *testing.T) {
	var gotID contract.AgentGroupID
	var gotPersona string
	set := func(id contract.AgentGroupID, persona string) error {
		gotID, gotPersona = id, persona
		return nil
	}
	next := &countingApplier{}
	a := NewPersonaApplier(set, next)

	after, _ := json.Marshal(map[string]string{"persona": "You are a terse on-call assistant."})
	req := contract.ChangeRequest{Kind: contract.ChangePersona, AgentGroupID: "grp-1", After: after}
	if err := a.Apply(context.Background(), req, contract.Decision{Outcome: "approve"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if gotID != "grp-1" || gotPersona != "You are a terse on-call assistant." {
		t.Fatalf("setter got (%q,%q)", gotID, gotPersona)
	}
	if next.n != 1 {
		t.Errorf("next must be called once (n=%d)", next.n)
	}
}

func TestPersonaApplierAcceptsInstructionsPayload(t *testing.T) {
	// The web config editor (ui_config.go) submits persona as {"instructions": ...}.
	var got string
	a := NewPersonaApplier(func(_ contract.AgentGroupID, persona string) error { got = persona; return nil }, &countingApplier{})
	after, _ := json.Marshal(map[string]string{"instructions": "Be terse."})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona, After: after}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != "Be terse." {
		t.Fatalf("instructions payload not applied, got %q", got)
	}
}

func TestPersonaApplierIgnoresOtherKinds(t *testing.T) {
	called := false
	set := func(contract.AgentGroupID, string) error { called = true; return nil }
	next := &countingApplier{}
	a := NewPersonaApplier(set, next)

	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeMounts}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called {
		t.Error("a non-persona kind must not call the persona setter")
	}
	if next.n != 1 {
		t.Errorf("next must still be called (n=%d)", next.n)
	}
}

func TestPersonaApplierPropagatesSetterError(t *testing.T) {
	set := func(contract.AgentGroupID, string) error { return errors.New("group not found") }
	a := NewPersonaApplier(set, &countingApplier{})
	after, _ := json.Marshal(map[string]string{"persona": "x"})
	err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona, After: after}, contract.Decision{})
	if err == nil {
		t.Fatal("a setter error must propagate (the change does not silently succeed)")
	}
}

func TestPersonaApplierRejectsBadPayload(t *testing.T) {
	a := NewPersonaApplier(func(contract.AgentGroupID, string) error { return nil }, nil)
	err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona, After: []byte("{not json")}, contract.Decision{})
	if err == nil {
		t.Fatal("malformed persona payload must error")
	}
}
