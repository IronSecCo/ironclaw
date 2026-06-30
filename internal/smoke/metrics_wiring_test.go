package smoke

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/host/delivery"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/isolation"
	"github.com/IronSecCo/ironclaw/internal/host/keys"
	"github.com/IronSecCo/ironclaw/internal/host/metrics"
	hostqueue "github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/session"
)

// nopHandle is a launched-sandbox handle that reports alive forever and stops
// cleanly — enough to drive the Manager's launch path without a real runtime.
type nopHandle struct{}

func (nopHandle) Stop(context.Context) error { return nil }
func (nopHandle) Alive(context.Context) bool { return true }

// nopIsolator hands back a nopHandle on every launch.
type nopIsolator struct{}

func (nopIsolator) Launch(context.Context, isolation.SandboxSpec) (isolation.Handle, error) {
	return nopHandle{}, nil
}

// passVerifier lets a change through with no human, so an auto-approve decision
// is recorded without an out-of-band approval step.
type passVerifier struct{}

func (passVerifier) Name() string { return "pass" }
func (passVerifier) Verify(context.Context, contract.ChangeRequest) (contract.Verdict, string, error) {
	return contract.VerdictPass, "ok", nil
}

// rejectVerifier rejects every change, recording a reject decision.
type rejectVerifier struct{}

func (rejectVerifier) Name() string { return "reject" }
func (rejectVerifier) Verify(context.Context, contract.ChangeRequest) (contract.Verdict, string, error) {
	return contract.VerdictReject, "nope", nil
}

// TestMetricsWiringMovesOffZero is the end-to-end proof for IRO-217: it wires ONE
// *metrics.Metrics through the three real subsystems exactly as the daemon does
// (gateway .SetMetrics, delivery .WithMetrics, session OnLaunch), drives one real
// event into each, then scrapes the live /metrics HTTP handler and asserts the
// three previously-dead series have moved off 0. A fake isolator/adapter stands in
// for gVisor/Docker — the wiring, not the runtime, is what this guards.
func TestMetricsWiringMovesOffZero(t *testing.T) {
	m := metrics.New()

	// --- Gateway: one auto-approve + one reject decision ---
	approveGW := gateway.New(gateway.VerifierChain{passVerifier{}},
		gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore()).SetMetrics(m)
	if _, err := approveGW.Submit(context.Background(), contract.ChangeRequest{ID: "ok1", Kind: contract.ChangePersona}); err != nil {
		t.Fatalf("approve submit: %v", err)
	}
	rejectGW := gateway.New(gateway.VerifierChain{rejectVerifier{}},
		gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore()).SetMetrics(m)
	if _, err := rejectGW.Submit(context.Background(), contract.ChangeRequest{ID: "no1"}); err != nil {
		t.Fatalf("reject submit: %v", err)
	}

	// --- Delivery: one successful channel send ---
	reg := registry.NewMemRegistry()
	mg, _ := reg.GetOrCreateMessagingGroup("fake", "C1", "", true, contract.UnknownPublic)
	// ResolveSession registers the session in reg so the delivery Poll lists it.
	if _, err := reg.ResolveSession("g1", mg.ID, nil, contract.SessionShared); err != nil {
		t.Fatalf("resolve session: %v", err)
	}
	st := hostqueue.NewMemStore()
	hostView := hostqueue.NewMemOutbound(st)
	sandboxWriter := hostqueue.NewMemOutbound(st)
	channelReg := channels.NewRegistry()
	if err := channelReg.Register(channels.NewFakeAdapter("fake")); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	deliverer := delivery.New(channelReg, approveGW, reg,
		func(id contract.SessionID) (contract.OutboundReader, error) { return hostView, nil }).
		WithMetrics(m.Deliveries)
	ct, pid := "fake", "C1"
	if err := sandboxWriter.WriteMessageOut(contract.MessageOut{ID: "o1", Seq: 1, Kind: contract.KindChat, ChannelType: &ct, PlatformID: &pid, Content: "hi"}); err != nil {
		t.Fatalf("write outbound: %v", err)
	}
	if err := deliverer.Poll(context.Background()); err != nil {
		t.Fatalf("delivery poll: %v", err)
	}

	// --- Session launch: one real launch via the Manager's Wake path ---
	cust, err := keys.New([32]byte{})
	if err != nil {
		t.Fatalf("keys.New: %v", err)
	}
	mgr, err := session.New(session.Config{
		Factory:       hostqueue.NewFactory(t.TempDir()),
		Keys:          cust,
		Isolator:      nopIsolator{},
		Registry:      registry.NewMemRegistry(),
		KeyDir:        t.TempDir(),
		WorkspaceRoot: t.TempDir(),
		OnLaunch:      m.SandboxLaunches.Inc,
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	if err := mgr.Wake("ses_metric"); err != nil {
		t.Fatalf("wake: %v", err)
	}

	// --- Scrape the live /metrics handler and assert the dead series moved ---
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := string(raw)

	for _, want := range []string{
		`ironclaw_gateway_decisions_total{decision="approved"} 1`,
		`ironclaw_gateway_decisions_total{decision="rejected"} 1`,
		"ironclaw_deliveries_total 1",
		"ironclaw_sandbox_launches_total 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("scrape missing %q (series still 0 / wrong value) in:\n%s", want, out)
		}
	}
}
