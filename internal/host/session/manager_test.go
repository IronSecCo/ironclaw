// OWNER: AGENT1

package session

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/isolation"
	"github.com/nivardsec/ironclaw/internal/host/keys"
	"github.com/nivardsec/ironclaw/internal/host/queue"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/sweep"
)

// --- fakes ---

type fakeHandle struct{ stopped int32 }

func (h *fakeHandle) Stop(context.Context) error {
	atomic.AddInt32(&h.stopped, 1)
	return nil
}

type fakeIsolator struct {
	mu       sync.Mutex
	launches int
	failWith error
	last     *fakeHandle
}

func (f *fakeIsolator) Launch(context.Context, isolation.SandboxSpec) (isolation.Handle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launches++
	if f.failWith != nil {
		return nil, f.failWith
	}
	f.last = &fakeHandle{}
	return f.last, nil
}

func (f *fakeIsolator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launches
}

func newTestManager(t *testing.T, iso isolation.Isolator) *Manager {
	t.Helper()
	cust, err := keys.New([32]byte{})
	if err != nil {
		t.Fatalf("keys.New: %v", err)
	}
	fac := queue.NewFactory(t.TempDir())
	m, err := New(Config{
		Factory:       fac,
		Keys:          cust,
		Isolator:      iso,
		Registry:      registry.NewMemRegistry(),
		KeyDir:        t.TempDir(),
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	return m
}

func TestManagerWakeLaunchesOnceAndTracks(t *testing.T) {
	iso := &fakeIsolator{}
	m := newTestManager(t, iso)
	const id contract.SessionID = "ses_a"

	if err := m.Wake(id); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if !m.Running(id) {
		t.Fatal("session should be running after Wake")
	}
	// A second Wake is idempotent — no second launch.
	if err := m.Wake(id); err != nil {
		t.Fatalf("second Wake: %v", err)
	}
	if got := iso.count(); got != 1 {
		t.Fatalf("expected exactly 1 launch, got %d", got)
	}
	// The key file was written for hand-off.
	if _, err := os.Stat(m.keyFilePath(id)); err != nil {
		t.Fatalf("expected key file at %s: %v", m.keyFilePath(id), err)
	}
}

func TestManagerWakeBestEffortOnLaunchFailure(t *testing.T) {
	iso := &fakeIsolator{failWith: errors.New("rootfs not provisioned")}
	m := newTestManager(t, iso)
	const id contract.SessionID = "ses_fail"

	// A launch failure is swallowed (the message is already queued); Wake succeeds
	// but the session is not tracked as running.
	if err := m.Wake(id); err != nil {
		t.Fatalf("Wake should not propagate launch failure: %v", err)
	}
	if m.Running(id) {
		t.Fatal("session must not be tracked as running after a failed launch")
	}
}

func TestManagerKillStopsAndUntracks(t *testing.T) {
	iso := &fakeIsolator{}
	m := newTestManager(t, iso)
	const id contract.SessionID = "ses_k"

	if err := m.Wake(id); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	h := iso.last
	if err := m.Kill(id, sweep.KillCeiling); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if m.Running(id) {
		t.Fatal("session should not be running after Kill")
	}
	if atomic.LoadInt32(&h.stopped) != 1 {
		t.Fatal("handle.Stop should have been called exactly once")
	}
	// Killing an already-stopped session is a no-op.
	if err := m.Kill(id, sweep.KillCeiling); err != nil {
		t.Fatalf("Kill on stopped session: %v", err)
	}
}

func TestManagerProbeUnknownForNotRunning(t *testing.T) {
	m := newTestManager(t, &fakeIsolator{})
	hb, claim, err := m.Probe("never-launched")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if hb != -1 || claim != -1 {
		t.Fatalf("expected (-1,-1) for an untracked session, got (%d,%d)", hb, claim)
	}
}

func TestManagerProbeHeartbeatAge(t *testing.T) {
	iso := &fakeIsolator{}
	m := newTestManager(t, iso)
	const id contract.SessionID = "ses_hb"
	if err := m.Wake(id); err != nil {
		t.Fatalf("Wake: %v", err)
	}

	// Write a heartbeat file aged two minutes in the past.
	hbPath := m.heartbeatPath(id)
	if err := os.WriteFile(hbPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(hbPath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	hb, claim, err := m.Probe(id)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if hb < sweep.HeartbeatStaleMs {
		t.Fatalf("expected heartbeat age >= %d ms, got %d", sweep.HeartbeatStaleMs, hb)
	}
	if claim != -1 {
		t.Fatalf("expected no outstanding claim (-1), got %d", claim)
	}
}

func TestManagerInboundWriterEnsuresSession(t *testing.T) {
	m := newTestManager(t, &fakeIsolator{})
	const id contract.SessionID = "ses_w"

	w, err := m.InboundWriter(id)
	if err != nil {
		t.Fatalf("InboundWriter: %v", err)
	}
	defer w.Close()
	if err := w.WriteMessageIn(contract.MessageIn{ID: "m", Seq: 2, Status: contract.StatusQueued, Content: "hi"}); err != nil {
		t.Fatalf("WriteMessageIn: %v", err)
	}
	// The encrypted files now exist on disk.
	paths, err := m.cfg.Factory.Paths(string(id))
	if err != nil {
		t.Fatalf("Paths: %v", err)
	}
	if _, err := os.Stat(paths.Inbound); err != nil {
		t.Fatalf("inbound file should exist: %v", err)
	}
	// And an outbound reader opens cleanly (bootstrapped by Provision).
	r, err := m.OutboundReader(id)
	if err != nil {
		t.Fatalf("OutboundReader: %v", err)
	}
	r.Close()
}
