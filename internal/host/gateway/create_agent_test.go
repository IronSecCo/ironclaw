package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func TestCreateAgentVerifierPassesOtherKinds(t *testing.T) {
	v := NewCreateAgentVerifier(nil)
	verdict, _, err := v.Verify(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona})
	if err != nil {
		t.Fatal(err)
	}
	if verdict != contract.VerdictPass {
		t.Fatalf("non-create_agent verdict = %v, want pass", verdict)
	}
}

func TestCreateAgentVerifierRequiresHumanForValid(t *testing.T) {
	v := NewCreateAgentVerifier(func(contract.AgentGroupID) bool { return false })
	verdict, _, err := v.Verify(context.Background(), contract.ChangeRequest{
		Kind:  contract.ChangeCreateAgent,
		After: json.RawMessage(`{"name":"Researcher"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if verdict != contract.VerdictRequireHuman {
		t.Fatalf("verdict = %v, want require-human (new principal)", verdict)
	}
}

func TestCreateAgentVerifierRejects(t *testing.T) {
	cases := []struct {
		name   string
		exists AgentExistsFunc
		after  string
	}{
		{"empty payload", nil, ``},
		{"unsafe name traversal", nil, `{"name":"../x"}`},
		{"unsafe name separator", nil, `{"name":"a/b"}`},
		{"unsafe folder", nil, `{"name":"ok","folder":"../etc"}`},
		{"duplicate", func(contract.AgentGroupID) bool { return true }, `{"name":"dup"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewCreateAgentVerifier(tc.exists)
			verdict, _, err := v.Verify(context.Background(), contract.ChangeRequest{
				Kind: contract.ChangeCreateAgent, After: json.RawMessage(tc.after),
			})
			if err != nil {
				t.Fatal(err)
			}
			if verdict != contract.VerdictReject {
				t.Fatalf("verdict = %v, want reject", verdict)
			}
		})
	}
}

// TestCreateAgentEndToEnd asserts a valid create_agent is held for a human and, on
// approval, materialized via the wired creator func with the derived id.
func TestCreateAgentEndToEnd(t *testing.T) {
	var created []contract.AgentGroupID
	createFn := func(id contract.AgentGroupID, name, folder string) error {
		created = append(created, id)
		return nil
	}
	verifier := NewCreateAgentVerifier(func(contract.AgentGroupID) bool { return false })
	applier := NewCreateAgentApplier(createFn, NewLogApplier())
	store := NewMemoryStore()
	gw := New(VerifierChain{verifier}, NewManualApprover(), applier, store)

	errCh := make(chan error, 1)
	go func() {
		_, err := gw.Submit(context.Background(), contract.ChangeRequest{
			ID: "ca1", Kind: contract.ChangeCreateAgent, After: json.RawMessage(`{"name":"Researcher","folder":"research"}`),
		})
		errCh <- err
	}()
	waitPending(t, store, "ca1")
	if err := gw.Decide("ca1", contract.Decision{Outcome: OutcomeApprove, DecidedBy: "owner", DecidedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if st, _ := store.Status("ca1"); st != string(statusApplied) {
		t.Fatalf("status = %q, want applied", st)
	}
	if len(created) != 1 || created[0] != "research" {
		t.Fatalf("created = %v, want [research]", created)
	}
}

func TestCreateAgentApplierDelegatesOtherKinds(t *testing.T) {
	next := NewLogApplier()
	a := NewCreateAgentApplier(func(contract.AgentGroupID, string, string) error {
		t.Fatal("create func must not run for non-create_agent kinds")
		return nil
	}, next)
	if err := a.Apply(context.Background(), contract.ChangeRequest{ID: "x", Kind: contract.ChangePersona}, contract.Decision{}); err != nil {
		t.Fatal(err)
	}
	if len(next.Applied()) != 1 {
		t.Fatalf("delegate applied = %d, want 1", len(next.Applied()))
	}
}

func TestDeriveAgentGroupID(t *testing.T) {
	id, err := DeriveAgentGroupID(json.RawMessage(`{"name":"My Researcher"}`))
	if err != nil {
		t.Fatal(err)
	}
	if id != "my-researcher" {
		t.Fatalf("derived id = %q, want my-researcher", id)
	}
	id, _ = DeriveAgentGroupID(json.RawMessage(`{"name":"X","folder":"custom"}`))
	if id != "custom" {
		t.Fatalf("derived id = %q, want custom (folder wins)", id)
	}
}
