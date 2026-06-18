// Package e2e drives the full IronClaw lifecycle end to end with fakes only at
// the edges (a fake Isolator in place of gVisor, a fake model provider, and the
// FakeAdapter channel). Everything in between — the encrypted queue factory, the
// registry, the session Manager, the router, the sandbox poll loop, and host
// delivery — is the real production code, exercised over real encrypted SQLCipher
// queues. The message path validated is:
//
//	inbound event -> router -> session launch -> agent loop -> outbound -> delivery
//
// It runs in CI without runsc; a separate runsc-gated test covers the real
// isolator path and is skipped when runsc is absent.
package e2e

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/host/delivery"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/isolation"
	"github.com/IronSecCo/ironclaw/internal/host/keys"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/router"
	"github.com/IronSecCo/ironclaw/internal/host/session"
	"github.com/IronSecCo/ironclaw/internal/host/types"
	sandboxloop "github.com/IronSecCo/ironclaw/internal/sandbox/loop"
	sandboxqueue "github.com/IronSecCo/ironclaw/internal/sandbox/queue"
)

// fakeProvider is the sandbox-side model backend: it returns a canned reply and
// records the prompt it was asked.
type fakeProvider struct {
	reply      string
	lastPrompt string
}

func (p *fakeProvider) Query(_ context.Context, prompt string) (string, error) {
	p.lastPrompt = prompt
	return p.reply, nil
}

// fakeIsolator stands in for gVisor: it records launches instead of spawning a
// real sandbox (the test drives the sandbox loop directly over the same queues).
type fakeIsolator struct {
	launches int
}

func (f *fakeIsolator) Launch(context.Context, isolation.SandboxSpec) (isolation.Handle, error) {
	f.launches++
	return fakeHandle{}, nil
}

type fakeHandle struct{}

func (fakeHandle) Stop(context.Context) error { return nil }
func (fakeHandle) Alive(context.Context) bool { return true }

// TestFullLifecycle drives inbound -> router -> session launch -> agent loop ->
// outbound -> delivery with a fake Isolator + fake provider over real encrypted
// queues, asserting the agent's reply reaches the channel exactly once.
func TestFullLifecycle(t *testing.T) {
	const (
		channelType = "fake"
		platformID  = "C1"
		agentGroup  = "g1"
		reply       = "Hello from the agent."
	)

	// --- Host stack: registry + wiring so an inbound "fake/C1" message engages a
	// shared session bound to agent group g1.
	reg := registry.NewMemRegistry()
	if err := reg.GrantRole(registry.Role{UserID: channelType + ":alice", Role: registry.RoleOwner}); err != nil {
		t.Fatalf("grant role: %v", err)
	}
	mg, err := reg.GetOrCreateMessagingGroup(channelType, platformID, "", true, contract.UnknownPublic)
	if err != nil {
		t.Fatalf("messaging group: %v", err)
	}
	if err := reg.PutWiring(registry.Wiring{
		ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: agentGroup,
		EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionShared, Priority: 1,
	}); err != nil {
		t.Fatalf("wiring: %v", err)
	}

	// --- Real session Manager over the real encrypted queue factory + custodian,
	// launching via the fake Isolator.
	factory := queue.NewFactory(t.TempDir())
	cust, err := keys.New([32]byte{1, 2, 3})
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	iso := &fakeIsolator{}
	mgr, err := session.New(session.Config{
		Factory:          factory,
		Keys:             cust,
		Isolator:         iso,
		Registry:         reg,
		ModelProxySocket: "/run/ironclaw/modelproxy.sock",
		Image:            "test-image",
		KeyDir:           t.TempDir(),
		WorkspaceRoot:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}

	// --- Router writes inbound via the Manager and launches via the Manager.
	r := router.New(reg, mgr.InboundWriter, mgr)

	// --- Delivery reads outbound via the Manager and delivers to the FakeAdapter.
	channelReg := channels.NewRegistry()
	adapter := channels.NewFakeAdapter(channelType)
	if err := channelReg.Register(adapter); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	gw := gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())
	d := delivery.New(channelReg, gw, reg, mgr.OutboundReader)

	// --- 1) inbound -> 2) router (writes inbound + launches the session).
	outcomes, err := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: channelType, PlatformID: platformID, SenderHandle: "alice", Text: "hello",
	})
	if err != nil {
		t.Fatalf("route inbound: %v", err)
	}
	var sessionID contract.SessionID
	for _, o := range outcomes {
		if o.SessionID != "" {
			sessionID = o.SessionID
			break
		}
	}
	if sessionID == "" {
		t.Fatalf("no session resolved from routing: %+v", outcomes)
	}
	// --- 3) session launch happened through the fake Isolator.
	if iso.launches < 1 {
		t.Fatalf("session was not launched (isolator launches=%d)", iso.launches)
	}

	// --- 4) agent loop: run the real sandbox loop over the same encrypted queues
	// the host provisioned, with the fake provider. The host generated the session
	// key during routing; the sandbox opens the queues with it.
	key, ok := cust.Get(sessionID)
	if !ok {
		t.Fatalf("session key not generated for %s", sessionID)
	}
	paths, err := factory.Paths(string(sessionID))
	if err != nil {
		t.Fatalf("session paths: %v", err)
	}
	sbIn, err := sandboxqueue.OpenInbound(paths.Inbound, key)
	if err != nil {
		t.Fatalf("sandbox open inbound: %v", err)
	}
	sbOut, err := sandboxqueue.OpenOutbound(paths.Outbound, key)
	if err != nil {
		t.Fatalf("sandbox open outbound: %v", err)
	}
	prov := &fakeProvider{reply: reply}
	l, err := sandboxloop.New(sandboxloop.Config{
		Inbound:       sbIn,
		Outbound:      sbOut,
		Provider:      prov,
		HeartbeatPath: filepath.Join(t.TempDir(), ".heartbeat"),
		PollInterval:  5 * time.Millisecond,
		Logger:        discardLogger(),
	})
	if err != nil {
		t.Fatalf("sandbox loop.New: %v", err)
	}

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	loopDone := make(chan struct{})
	go func() {
		_ = l.Run(loopCtx)
		close(loopDone)
	}()

	// --- 5) outbound -> 6) delivery: poll until the agent's reply reaches the
	// channel. Transient SQLite contention while the loop writes is retried.
	eventually(t, 5*time.Second, 20*time.Millisecond, func() bool {
		_ = d.Poll(context.Background())
		return len(adapter.Delivered()) >= 1
	})
	cancelLoop()
	<-loopDone
	_ = sbIn.Close()
	_ = sbOut.Close()

	delivered := adapter.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("agent reply must be delivered exactly once, got %d: %+v", len(delivered), delivered)
	}
	if delivered[0].Content != reply {
		t.Fatalf("delivered content = %q, want %q", delivered[0].Content, reply)
	}
	if prov.lastPrompt == "" {
		t.Fatal("provider was never asked a prompt")
	}
}

// TestFullLifecycleRunscGated exercises the real isolation spec path when runsc
// is installed; it is skipped in environments without runsc (e.g. CI). A full
// real launch additionally needs a provisioned rootfs image, which a hermetic
// test does not build — the end-to-end message flow is covered by the
// fake-Isolator test above.
func TestFullLifecycleRunscGated(t *testing.T) {
	if _, err := exec.LookPath("runsc"); err != nil {
		t.Skip("runsc (gVisor) not installed; skipping real-isolator lifecycle variant")
	}
	_ = isolation.NewRunsc() // the real isolator constructs without error
	spec := isolation.HardenedSpec("e2e-runsc", "test-image", "/tmp/in.db", "/tmp/out.db", "/run/ironclaw/modelproxy.sock")
	if _, err := isolation.BuildOCISpec(spec); err != nil {
		t.Fatalf("hardened OCI spec must build for the runsc path: %v", err)
	}
}
