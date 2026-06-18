package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

type fakeAllower struct{ hosts []string }

func (f *fakeAllower) Allow(host string) { f.hosts = append(f.hosts, host) }

type countingApplier struct{ n int }

func (c *countingApplier) Apply(context.Context, contract.ChangeRequest, contract.Decision) error {
	c.n++
	return nil
}

func TestEgressApplierAllowsThenDelegates(t *testing.T) {
	allow := &fakeAllower{}
	next := &countingApplier{}
	a := NewEgressApplier(allow, next)

	after, _ := json.Marshal(map[string]any{"egress": []string{"api.pagerduty.com", " status.example.com "}})
	err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangePermissions, After: after}, contract.Decision{Outcome: "approve"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(allow.hosts) != 2 || allow.hosts[0] != "api.pagerduty.com" || allow.hosts[1] != "status.example.com" {
		t.Fatalf("allowed hosts = %v, want [api.pagerduty.com status.example.com] (trimmed)", allow.hosts)
	}
	if next.n != 1 {
		t.Errorf("next applier not called once (n=%d)", next.n)
	}
}

func TestEgressApplierNoEgressField(t *testing.T) {
	allow := &fakeAllower{}
	next := &countingApplier{}
	a := NewEgressApplier(allow, next)

	after, _ := json.Marshal(map[string]any{"persona": "hi"})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona, After: after}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(allow.hosts) != 0 {
		t.Errorf("no egress field should allow nothing, got %v", allow.hosts)
	}
	if next.n != 1 {
		t.Errorf("next must still be called (n=%d)", next.n)
	}
}

func TestEgressApplierNilSafety(t *testing.T) {
	// nil allower + nil next: a no-op that does not panic or error.
	a := NewEgressApplier(nil, nil)
	after, _ := json.Marshal(map[string]any{"egress": []string{"x.com"}})
	if err := a.Apply(context.Background(), contract.ChangeRequest{After: after}, contract.Decision{}); err != nil {
		t.Fatalf("nil-safe Apply: %v", err)
	}
}
