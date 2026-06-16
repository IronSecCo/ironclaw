// OWNER: AGENT1

package sweep

import (
	"context"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

func TestDecideStuckAction(t *testing.T) {
	tests := []struct {
		name      string
		heartbeat int64
		claim     int64
		want      StuckAction
	}{
		{name: "healthy", heartbeat: 1000, claim: 500, want: None},
		{name: "busy but heart-beating", heartbeat: 1000, claim: ClaimStaleMs + 10_000, want: None},
		{name: "heartbeat past ceiling", heartbeat: HeartbeatCeilingMs + 1, claim: 0, want: KillCeiling},
		{name: "ceiling wins over claim", heartbeat: HeartbeatCeilingMs + 1, claim: ClaimStaleMs + 1, want: KillCeiling},
		{name: "stuck claim with stale heartbeat", heartbeat: HeartbeatStaleMs + 1, claim: ClaimStaleMs + 1, want: KillClaim},
		{name: "stale claim but fresh heartbeat", heartbeat: 100, claim: ClaimStaleMs + 1, want: None},
		{name: "stale heartbeat but fresh claim", heartbeat: HeartbeatStaleMs + 1, claim: 100, want: None},
		{name: "unknown ages", heartbeat: -1, claim: -1, want: None},
		{name: "exactly at ceiling not over", heartbeat: HeartbeatCeilingMs, claim: 0, want: None},
		{name: "exactly at claim stale not over", heartbeat: HeartbeatStaleMs + 1, claim: ClaimStaleMs, want: None},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideStuckAction(tt.heartbeat, tt.claim); got != tt.want {
				t.Fatalf("DecideStuckAction(%d,%d) = %v, want %v", tt.heartbeat, tt.claim, got, tt.want)
			}
		})
	}
}

// fakeProber returns per-session liveness readings from a map.
type fakeProber struct {
	hb    map[contract.SessionID]int64
	claim map[contract.SessionID]int64
}

func (f *fakeProber) Probe(id contract.SessionID) (int64, int64, error) {
	return f.hb[id], f.claim[id], nil
}

// fakeKiller records which sessions it was asked to kill.
type fakeKiller struct {
	killed map[contract.SessionID]StuckAction
}

func (f *fakeKiller) Kill(id contract.SessionID, action StuckAction) error {
	if f.killed == nil {
		f.killed = map[contract.SessionID]StuckAction{}
	}
	f.killed[id] = action
	return nil
}

func TestSweepRunKillsStuckLeavesHealthy(t *testing.T) {
	reg := registry.NewMemRegistry()
	healthy, _ := reg.ResolveSession("g1", "m1", strptr("h"), contract.SessionPerThread)
	dead, _ := reg.ResolveSession("g1", "m1", strptr("d"), contract.SessionPerThread)

	prober := &fakeProber{
		hb: map[contract.SessionID]int64{
			healthy.ID: 1000,                      // fresh heartbeat
			dead.ID:    HeartbeatCeilingMs + 1000, // past ceiling => KillCeiling
		},
		claim: map[contract.SessionID]int64{},
	}
	killer := &fakeKiller{}
	s := New(reg, prober, killer)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := killer.killed[healthy.ID]; ok {
		t.Fatal("healthy session should not be killed")
	}
	if act := killer.killed[dead.ID]; act != KillCeiling {
		t.Fatalf("dead session should be killed with KillCeiling, got %v", act)
	}
}

func TestSweepRunKillClaim(t *testing.T) {
	reg := registry.NewMemRegistry()
	stuck, _ := reg.ResolveSession("g1", "m1", nil, contract.SessionShared)
	prober := &fakeProber{
		hb:    map[contract.SessionID]int64{stuck.ID: HeartbeatStaleMs + 1},
		claim: map[contract.SessionID]int64{stuck.ID: ClaimStaleMs + 1},
	}
	killer := &fakeKiller{}
	s := New(reg, prober, killer)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if act := killer.killed[stuck.ID]; act != KillClaim {
		t.Fatalf("stuck-claim session should be killed with KillClaim, got %v", act)
	}
}

func strptr(s string) *string { return &s }
