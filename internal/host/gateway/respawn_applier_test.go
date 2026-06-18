package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// recordingApplier captures whether it was applied and can inject an error.
type recordingApplier struct {
	applied bool
	err     error
}

func (r *recordingApplier) Apply(_ context.Context, _ contract.ChangeRequest, _ contract.Decision) error {
	r.applied = true
	return r.err
}

// fakeRespawner records the groups it was asked to respawn.
type fakeRespawner struct{ groups []contract.AgentGroupID }

func (f *fakeRespawner) RespawnGroup(id contract.AgentGroupID) int {
	f.groups = append(f.groups, id)
	return 1
}

func TestRespawnApplierRespawnsForLaunchSpecKinds(t *testing.T) {
	for _, kind := range []contract.ChangeKind{
		contract.ChangeEnabledTools, contract.ChangePersona, contract.ChangePackages,
		contract.ChangeMounts, contract.ChangePermissions,
	} {
		next := &recordingApplier{}
		rsp := &fakeRespawner{}
		a := NewRespawnApplier(rsp, next)

		err := a.Apply(context.Background(), contract.ChangeRequest{Kind: kind, AgentGroupID: "grp1"}, contract.Decision{})
		if err != nil {
			t.Fatalf("%s: Apply: %v", kind, err)
		}
		if !next.applied {
			t.Fatalf("%s: inner applier must run first", kind)
		}
		if len(rsp.groups) != 1 || rsp.groups[0] != "grp1" {
			t.Fatalf("%s: expected respawn of grp1, got %v", kind, rsp.groups)
		}
	}
}

func TestRespawnApplierSkipsNonSpecKinds(t *testing.T) {
	// Wiring/create_agent change message routing or create a new group; neither alters
	// a running sandbox's launch spec, so no live session should be bounced.
	for _, kind := range []contract.ChangeKind{contract.ChangeWiring, contract.ChangeCreateAgent} {
		next := &recordingApplier{}
		rsp := &fakeRespawner{}
		a := NewRespawnApplier(rsp, next)

		if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: kind, AgentGroupID: "grp1"}, contract.Decision{}); err != nil {
			t.Fatalf("%s: Apply: %v", kind, err)
		}
		if !next.applied {
			t.Fatalf("%s: inner applier must still run", kind)
		}
		if len(rsp.groups) != 0 {
			t.Fatalf("%s: must not respawn, got %v", kind, rsp.groups)
		}
	}
}

func TestRespawnApplierPropagatesInnerErrorWithoutRespawn(t *testing.T) {
	next := &recordingApplier{err: errors.New("apply failed")}
	rsp := &fakeRespawner{}
	a := NewRespawnApplier(rsp, next)

	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools, AgentGroupID: "grp1"}, contract.Decision{}); err == nil {
		t.Fatal("expected the inner error to propagate")
	}
	if len(rsp.groups) != 0 {
		t.Fatal("must not respawn when the change did not materialize")
	}
}

func TestRespawnApplierNilRespawnerAndLateBinding(t *testing.T) {
	next := &recordingApplier{}
	a := NewRespawnApplier(nil, next) // respawner not wired yet
	// No panic, no respawn — just applies.
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools, AgentGroupID: "grp1"}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// After late binding, it respawns.
	rsp := &fakeRespawner{}
	a.SetRespawner(rsp)
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools, AgentGroupID: "grp1"}, contract.Decision{}); err != nil {
		t.Fatalf("Apply after SetRespawner: %v", err)
	}
	if len(rsp.groups) != 1 {
		t.Fatalf("expected respawn after late binding, got %v", rsp.groups)
	}
}

func TestRespawnApplierEmptyGroupNoRespawn(t *testing.T) {
	next := &recordingApplier{}
	rsp := &fakeRespawner{}
	a := NewRespawnApplier(rsp, next)
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangeEnabledTools}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(rsp.groups) != 0 {
		t.Fatal("must not respawn with an empty agent group id")
	}
}
