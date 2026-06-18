package sweep

import (
	"context"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
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

// --- scheduling: due-message wake + recurrence ---

// healthyProber reports a fresh heartbeat for any session so the stuck-sweep takes
// no action and we exercise only the due-message pass.
type healthyProber struct{}

func (healthyProber) Probe(contract.SessionID) (int64, int64, error) { return 1000, 0, nil }

// noKiller fails the test if Kill is ever called.
type noKiller struct{ t *testing.T }

func (k noKiller) Kill(id contract.SessionID, action StuckAction) error {
	k.t.Fatalf("unexpected kill of %s (%v)", id, action)
	return nil
}

// fakeDueSource returns a fixed set of due messages.
type fakeDueSource struct{ msgs []DueMessage }

func (f fakeDueSource) DueMessages(time.Time) ([]DueMessage, error) { return f.msgs, nil }

// fakeWaker records which sessions it woke.
type fakeWaker struct{ woke map[contract.SessionID]int }

func (w *fakeWaker) Wake(id contract.SessionID) error {
	if w.woke == nil {
		w.woke = map[contract.SessionID]int{}
	}
	w.woke[id]++
	return nil
}

// fakeEnqueue records re-enqueued occurrences.
type enqueued struct {
	prompt     string
	runAt      time.Time
	recurrence string
}

func TestSweepDueMessageWakesSession(t *testing.T) {
	reg := registry.NewMemRegistry()
	sess, _ := reg.ResolveSession("g1", "m1", nil, contract.SessionShared)

	waker := &fakeWaker{}
	var reenq []enqueued
	enqueue := func(id contract.SessionID, prompt string, runAt time.Time, rec string) error {
		reenq = append(reenq, enqueued{prompt, runAt, rec})
		return nil
	}
	due := fakeDueSource{msgs: []DueMessage{
		{SessionID: sess.ID, MessageID: "m1", Prompt: "ping", RunAt: time.Now().Add(-time.Minute)},
	}}
	s := New(reg, healthyProber{}, noKiller{t}).WithScheduling(due, waker, enqueue)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if waker.woke[sess.ID] != 1 {
		t.Fatalf("expected the due session to be woken once, got %d", waker.woke[sess.ID])
	}
	// One-shot message must not re-enqueue.
	if len(reenq) != 0 {
		t.Fatalf("one-shot due message must not re-enqueue, got %+v", reenq)
	}
}

func TestSweepRecurringReEnqueuesAtNextRun(t *testing.T) {
	reg := registry.NewMemRegistry()
	sess, _ := reg.ResolveSession("g1", "m1", nil, contract.SessionShared)

	waker := &fakeWaker{}
	var reenq []enqueued
	enqueue := func(id contract.SessionID, prompt string, runAt time.Time, rec string) error {
		reenq = append(reenq, enqueued{prompt, runAt, rec})
		return nil
	}
	ranAt := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
	due := fakeDueSource{msgs: []DueMessage{
		{SessionID: sess.ID, MessageID: "m1", Prompt: "daily report", RunAt: ranAt, Recurrence: "daily"},
	}}
	s := New(reg, healthyProber{}, noKiller{t}).WithScheduling(due, waker, enqueue)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if waker.woke[sess.ID] != 1 {
		t.Fatalf("recurring due message should also wake, got %d", waker.woke[sess.ID])
	}
	if len(reenq) != 1 {
		t.Fatalf("expected one re-enqueue, got %d", len(reenq))
	}
	want := ranAt.Add(24 * time.Hour)
	if !reenq[0].runAt.Equal(want) {
		t.Fatalf("re-enqueue runAt = %v, want %v", reenq[0].runAt, want)
	}
	if reenq[0].prompt != "daily report" || reenq[0].recurrence != "daily" {
		t.Fatalf("re-enqueue payload = %+v", reenq[0])
	}
}

func TestSweepNotYetDueDoesNothing(t *testing.T) {
	reg := registry.NewMemRegistry()
	_, _ = reg.ResolveSession("g1", "m1", nil, contract.SessionShared)

	waker := &fakeWaker{}
	enqueue := func(contract.SessionID, string, time.Time, string) error {
		t.Fatal("not-yet-due message must not re-enqueue")
		return nil
	}
	// A DueSource that returns nothing (the message is not yet due).
	due := fakeDueSource{msgs: nil}
	s := New(reg, healthyProber{}, noKiller{t}).WithScheduling(due, waker, enqueue)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(waker.woke) != 0 {
		t.Fatalf("no session should be woken, got %+v", waker.woke)
	}
}

func TestSweepWithoutSchedulingHooksIsNoOp(t *testing.T) {
	// Run without WithScheduling must not touch any scheduling hook and must still
	// perform the stuck-sweep over a healthy session.
	reg := registry.NewMemRegistry()
	_, _ = reg.ResolveSession("g1", "m1", nil, contract.SessionShared)
	s := New(reg, healthyProber{}, noKiller{t})
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}
