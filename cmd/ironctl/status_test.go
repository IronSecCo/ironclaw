package main

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/api"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/metrics"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// newStatusServer spins up the real control-plane API (registry + metrics) so the
// observability subcommands run end-to-end against the actual endpoints.
func newStatusServer(t *testing.T) (*httptest.Server, *registry.MemRegistry, *gateway.MemoryStore) {
	t.Helper()
	reg := registry.NewMemRegistry()
	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	m := metrics.New()
	srv := httptest.NewServer(api.New(gw).WithRegistry(reg).WithMetrics(m.Handler()).Handler())
	t.Cleanup(srv.Close)
	return srv, reg, store
}

func TestGatherStatus(t *testing.T) {
	token = "" // ungated test server
	srv, reg, store := newStatusServer(t)

	if _, err := reg.ResolveSession("g1", "mg1", nil, contract.SessionShared); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(contract.ChangeRequest{
		ID: "c1", Kind: contract.ChangePersona, AgentGroupID: "g1", RequestedBy: "u1",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	rep := gatherStatus(srv.URL)
	if !rep.Healthy {
		t.Errorf("Healthy = false, want true (%s)", rep.HealthDetail)
	}
	if !rep.Ready {
		t.Errorf("Ready = false, want true")
	}
	if rep.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1", rep.Sessions)
	}
	if rep.PendingApprovals != 1 {
		t.Errorf("PendingApprovals = %d, want 1", rep.PendingApprovals)
	}
}

func TestGatherStatusUnreachable(t *testing.T) {
	token = ""
	rep := gatherStatus("http://127.0.0.1:1")
	if rep.Healthy {
		t.Error("Healthy = true for an unreachable address, want false")
	}
	if rep.HealthDetail == "" {
		t.Error("expected a health detail for the unreachable address")
	}
}

func TestStatusCommandSmoke(t *testing.T) {
	token = ""
	srv, _, _ := newStatusServer(t)
	if err := run([]string{"--addr", srv.URL, "status"}); err != nil {
		t.Fatalf("ironctl status: %v", err)
	}
	if err := run([]string{"--addr", srv.URL, "status", "--json"}); err != nil {
		t.Fatalf("ironctl status --json: %v", err)
	}
}

func TestParseModelUsage(t *testing.T) {
	sample := `# HELP ironclaw_model_calls_total Total model-host calls.
# TYPE ironclaw_model_calls_total counter
ironclaw_model_calls_total 42
ironclaw_model_call_errors_total 3
# TYPE ironclaw_model_call_duration_seconds histogram
ironclaw_model_call_duration_seconds_bucket{le="0.1"} 10
ironclaw_model_call_duration_seconds_bucket{le="+Inf"} 42
ironclaw_model_call_duration_seconds_sum 21
ironclaw_model_call_duration_seconds_count 42
`
	u := parseModelUsage(sample)
	if u.Calls != 42 {
		t.Errorf("Calls = %v, want 42", u.Calls)
	}
	if u.Errors != 3 {
		t.Errorf("Errors = %v, want 3", u.Errors)
	}
	// avg = 21s / 42 = 0.5s = 500ms
	if u.AvgMillis != 500 {
		t.Errorf("AvgMillis = %v, want 500", u.AvgMillis)
	}
}

func TestUsageCommandSmoke(t *testing.T) {
	token = ""
	srv, _, _ := newStatusServer(t)
	if err := run([]string{"--addr", srv.URL, "usage", "--json"}); err != nil {
		t.Fatalf("ironctl usage: %v", err)
	}
}
