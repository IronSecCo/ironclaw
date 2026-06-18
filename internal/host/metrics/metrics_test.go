package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDomainMetricsExposition(t *testing.T) {
	m := New()
	m.ObserveModelCall(0.12, false)
	m.ObserveModelCall(0.30, true) // an errored call
	m.GatewayDecision(true)
	m.GatewayDecision(false)
	m.GatewayDecision(true)
	m.Deliveries.Add(4)
	m.SandboxLaunches.Inc()
	m.SandboxKills.Inc()

	// Scrape through the real HTTP handler.
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain exposition", ct)
	}
	raw, _ := io.ReadAll(resp.Body)
	out := string(raw)

	for _, want := range []string{
		"ironclaw_model_calls_total 2",
		"ironclaw_model_call_errors_total 1",
		"# TYPE ironclaw_model_call_duration_seconds histogram",
		"ironclaw_model_call_duration_seconds_count 2",
		`ironclaw_gateway_decisions_total{decision="approved"} 2`,
		`ironclaw_gateway_decisions_total{decision="rejected"} 1`,
		"ironclaw_deliveries_total 4",
		"ironclaw_sandbox_launches_total 1",
		"ironclaw_sandbox_kills_total 1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("scrape missing %q in:\n%s", want, out)
		}
	}
}

func TestGatewayDecisionSeriesIsolated(t *testing.T) {
	m := New()
	m.GatewayDecision(false)
	if got := m.gatewayRejected.Value(); got != 1 {
		t.Fatalf("rejected = %d, want 1", got)
	}
	if got := m.gatewayApproved.Value(); got != 0 {
		t.Fatalf("approved = %d, want 0", got)
	}
}

func TestRegistryAccessorForExtraMetrics(t *testing.T) {
	m := New()
	extra := m.Registry().NewCounter("ironclaw_extra_total", "Extra.")
	extra.Inc()
	var b strings.Builder
	_, _ = m.Registry().WriteTo(&b)
	if !strings.Contains(b.String(), "ironclaw_extra_total 1") {
		t.Fatalf("extra metric not registered on the shared registry:\n%s", b.String())
	}
}
