package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/api"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
)

func testGateway() *gateway.Gateway {
	return gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
}

// TestReadinessGateNotReadyUntilAllSubsystems is the core wiring contract: the
// gate the controlplane hands to api.WithReadiness must report not-ready until
// every tracked subsystem has signalled, then flip to ready.
func TestReadinessGateNotReadyUntilAllSubsystems(t *testing.T) {
	g := newReadinessGate("model-proxy", "delivery", "sweep")

	if err := g.check(); err == nil {
		t.Fatal("fresh gate should be not-ready (nothing has started)")
	}

	g.markReady("model-proxy")
	g.markReady("delivery")
	if err := g.check(); err == nil {
		t.Fatal("gate should still be not-ready with sweep pending")
	} else if !strings.Contains(err.Error(), "sweep") {
		t.Fatalf("not-ready reason = %q, want it to name the pending sweep subsystem", err)
	}

	g.markReady("sweep")
	if err := g.check(); err != nil {
		t.Fatalf("gate should be ready once all subsystems signalled, got %v", err)
	}
}

// TestReadinessGateEmptyIsReady: a gate tracking nothing is ready immediately.
func TestReadinessGateEmptyIsReady(t *testing.T) {
	if err := newReadinessGate().check(); err != nil {
		t.Fatalf("empty gate should be ready, got %v", err)
	}
}

// TestReadinessGateIgnoresUnknownAndDuplicate: signalling an untracked or
// already-ready subsystem is harmless and never makes the gate go backwards.
func TestReadinessGateIgnoresUnknownAndDuplicate(t *testing.T) {
	g := newReadinessGate("model-proxy")
	g.markReady("bogus") // unknown — ignored
	g.markReady("model-proxy")
	g.markReady("model-proxy") // duplicate — still ready
	if err := g.check(); err != nil {
		t.Fatalf("gate should be ready, got %v", err)
	}
}

// TestReadyzGateWiredThroughServer exercises the gate exactly as main wires it:
// through api.WithReadiness. /readyz must be 503 until the subsystems signal,
// then 200 — proving the controlplane's readiness wiring composes end to end.
func TestReadyzGateWiredThroughServer(t *testing.T) {
	g := newReadinessGate("model-proxy", "delivery", "sweep")
	srv := httptest.NewServer(api.New(testGateway()).WithReadiness(g.check).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz before subsystems up = %d, want 503", resp.StatusCode)
	}

	g.markReady("model-proxy")
	g.markReady("delivery")
	g.markReady("sweep")

	resp, err = http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/readyz after subsystems up = %d, want 200", resp.StatusCode)
	}
}

// TestHardeningOptionsCompose verifies the full hardening chain main builds —
// rate limit + body cap + readiness gate — enforces limits while keeping probes
// exempt, matching the production wiring.
func TestHardeningOptionsCompose(t *testing.T) {
	g := newReadinessGate("model-proxy")
	g.markReady("model-proxy") // ready, so /readyz is 200 and not the thing under test
	srv := httptest.NewServer(
		api.New(testGateway()).
			WithRateLimit(0.0001, 1).
			WithLimits(16, 0).
			WithReadiness(g.check).
			Handler(),
	)
	defer srv.Close()

	// Body cap: an oversize POST is rejected (MaxBytesReader -> decode error -> 400).
	big := strings.NewReader(`{"kind":"` + strings.Repeat("x", 256) + `"}`)
	resp, err := http.Post(srv.URL+"/v1/changes", "application/json", big)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversize body = %d, want 400", resp.StatusCode)
	}

	// Probes stay exempt from the (now-exhausted) rate limiter.
	resp, err = http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/readyz under hardening = %d, want 200 (probe exempt + ready)", resp.StatusCode)
	}
}
