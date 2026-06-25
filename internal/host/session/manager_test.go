package session

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/isolation"
	"github.com/IronSecCo/ironclaw/internal/host/keys"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/sweep"
)

// --- fakes ---

type fakeHandle struct {
	stopped int32
	dead    int32 // when >0, Alive reports false (simulates a vanished container)
}

func (h *fakeHandle) Stop(context.Context) error {
	atomic.AddInt32(&h.stopped, 1)
	return nil
}

func (h *fakeHandle) Alive(context.Context) bool {
	return atomic.LoadInt32(&h.dead) == 0
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

// TestManagerWakeRelaunchesDeadSandbox asserts the liveness (not mere presence)
// check in Wake: a tracked sandbox that has died out-of-band (crash, OOM, a manual
// `docker rm`) is detected, stopped, and relaunched on the next Wake — recovery
// that does not wait for the sweep's 30-minute heartbeat ceiling.
func TestManagerWakeRelaunchesDeadSandbox(t *testing.T) {
	iso := &fakeIsolator{}
	m := newTestManager(t, iso)
	const id contract.SessionID = "ses_dead"

	if err := m.Wake(id); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	first := iso.last
	if first == nil {
		t.Fatal("expected a launched handle")
	}
	// While the sandbox is alive, a second Wake is a no-op (presence == liveness).
	if err := m.Wake(id); err != nil {
		t.Fatalf("second Wake (alive): %v", err)
	}
	if got := iso.count(); got != 1 {
		t.Fatalf("expected no relaunch while alive, got %d launches", got)
	}

	// The container dies out-of-band: the handle is still tracked but not alive.
	atomic.StoreInt32(&first.dead, 1)

	if err := m.Wake(id); err != nil {
		t.Fatalf("Wake after death: %v", err)
	}
	if got := iso.count(); got != 2 {
		t.Fatalf("expected a relaunch after the sandbox died, got %d launches", got)
	}
	if !m.Running(id) {
		t.Fatal("session should be tracked as running after relaunch")
	}
	// The dead handle was stopped (cleaned up) as part of the relaunch...
	if got := atomic.LoadInt32(&first.stopped); got != 1 {
		t.Fatalf("dead handle should have been stopped once, got %d", got)
	}
	// ...and a fresh, live handle now backs the session.
	if iso.last == first {
		t.Fatal("expected a new handle to be tracked after relaunch")
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

// TestManagerRespawnGroupStopsOnlyMatchingSessions asserts RespawnGroup stops exactly
// the live sessions of the target group (so they relaunch with the just-approved
// config on the next message) and leaves other groups' sessions running.
func TestManagerRespawnGroupStopsOnlyMatchingSessions(t *testing.T) {
	iso := &fakeIsolator{}
	m := newTestManager(t, iso)

	// Two sessions in group A (distinct messaging groups), one in group B; all live.
	a1, _ := m.cfg.Registry.ResolveSession("grpA", "mgA1", nil, contract.SessionShared)
	a2, _ := m.cfg.Registry.ResolveSession("grpA", "mgA2", nil, contract.SessionShared)
	b1, _ := m.cfg.Registry.ResolveSession("grpB", "mgB1", nil, contract.SessionShared)
	for _, id := range []contract.SessionID{a1.ID, a2.ID, b1.ID} {
		if err := m.Wake(id); err != nil {
			t.Fatalf("Wake %s: %v", id, err)
		}
	}

	if n := m.RespawnGroup("grpA"); n != 2 {
		t.Fatalf("RespawnGroup stopped %d sessions, want 2", n)
	}
	if m.Running(a1.ID) || m.Running(a2.ID) {
		t.Fatal("group A sessions should be stopped for respawn")
	}
	if !m.Running(b1.ID) {
		t.Fatal("group B session must be left running")
	}

	// A stopped session relaunches (fresh spec) on its next Wake.
	before := iso.count()
	if err := m.Wake(a1.ID); err != nil {
		t.Fatalf("re-Wake: %v", err)
	}
	if iso.count() != before+1 {
		t.Fatalf("stopped session should relaunch on next Wake (launches %d -> %d)", before, iso.count())
	}
}

// TestManagerRespawnGroupEmptyAndUnknown covers the no-op edges: an empty group id and
// a group with no live sessions both stop nothing.
func TestManagerRespawnGroupEmptyAndUnknown(t *testing.T) {
	m := newTestManager(t, &fakeIsolator{})
	if n := m.RespawnGroup(""); n != 0 {
		t.Fatalf("RespawnGroup(\"\") stopped %d, want 0", n)
	}
	if n := m.RespawnGroup("nobody"); n != 0 {
		t.Fatalf("RespawnGroup(unknown) stopped %d, want 0", n)
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

// --- early-exit diagnostic (IRO-171) ---

// exitHandle is a fakeHandle that also implements isolation.EarlyExitReporter, so
// the Manager's post-launch early-exit watcher engages.
type exitHandle struct {
	fakeHandle
	exited  bool
	code    int
	logLine string
	err     error
}

func (h *exitHandle) ExitInfo(context.Context) (bool, int, string, error) {
	return h.exited, h.code, h.logLine, h.err
}

func TestReportEarlyExit(t *testing.T) {
	cases := []struct {
		name        string
		h           *exitHandle
		wantLog     bool
		wantSubstrs []string
	}{
		{
			name:        "non-zero exit logs code, log line, and the file-sharing hint",
			h:           &exitHandle{exited: true, code: 1, logLine: `read session key "...": no such file or directory`},
			wantLog:     true,
			wantSubstrs: []string{"exited early with code 1", "no such file or directory", "ironctl doctor", "file-sharing"},
		},
		{
			name:        "probe error is reported, not swallowed",
			h:           &exitHandle{err: errors.New("docker api down")},
			wantLog:     true,
			wantSubstrs: []string{"could not probe early exit", "docker api down"},
		},
		{
			name:    "still running logs nothing",
			h:       &exitHandle{exited: false},
			wantLog: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newTestManager(t, &fakeIsolator{})
			var buf bytes.Buffer
			m.cfg.Logger = log.New(&buf, "", 0)
			m.reportEarlyExit("ses_x", c.h)
			out := buf.String()
			if c.wantLog && out == "" {
				t.Fatalf("expected a log line, got none")
			}
			if !c.wantLog && out != "" {
				t.Fatalf("expected no log, got %q", out)
			}
			for _, s := range c.wantSubstrs {
				if !strings.Contains(out, s) {
					t.Errorf("log %q missing %q", out, s)
				}
			}
		})
	}
}

// TestWakeWatchesEarlyExit asserts Wake spawns the early-exit watcher for an
// isolator whose handle reports exits, and that the diagnostic eventually lands.
func TestWakeWatchesEarlyExit(t *testing.T) {
	iso := &exitIsolator{h: &exitHandle{exited: true, code: 1, logLine: "boom"}}
	m := newTestManager(t, iso)
	var buf syncBuffer
	m.cfg.Logger = log.New(&buf, "", 0)
	m.cfg.EarlyExitGrace = time.Millisecond // tiny but non-zero

	if err := m.Wake("ses_e"); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "exited early with code 1") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("early-exit diagnostic never logged; got %q", buf.String())
}

type exitIsolator struct{ h *exitHandle }

func (e *exitIsolator) Launch(context.Context, isolation.SandboxSpec) (isolation.Handle, error) {
	return e.h, nil
}

// syncBuffer is a goroutine-safe bytes.Buffer for asserting on async log output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
