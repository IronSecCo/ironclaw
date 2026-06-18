package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// TestRequestApiAccessBuildsWiringEgressEnvelope asserts the tool produces a
// CapabilityChange (kind=wiring, payload {"egress":[...]}) that re-renders to a host
// system action — the same forwarding path as request_capability_change, so an
// approved request lands in the egress applier.
func TestRequestApiAccessBuildsWiringEgressEnvelope(t *testing.T) {
	tool := NewRequestApiAccessTool()
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"hosts":["api.github.com"],"reason":"fetch issues"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	cc, err := ParseCapabilityChange(out)
	if err != nil {
		t.Fatalf("ParseCapabilityChange: %v", err)
	}
	if cc.Kind != contract.ChangeWiring {
		t.Fatalf("kind = %q, want wiring", cc.Kind)
	}
	var p struct {
		Egress []string `json:"egress"`
	}
	if err := json.Unmarshal(cc.Payload, &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if len(p.Egress) != 1 || p.Egress[0] != "api.github.com" {
		t.Fatalf("egress = %v, want [api.github.com]", p.Egress)
	}
	// ToHostAction (HostForwarder) must render a system action the host can parse.
	if _, ok := interface{}(tool).(HostForwarder); !ok {
		t.Fatal("request_api_access must be a HostForwarder")
	}
	sysAction, err := tool.ToHostAction(out)
	if err != nil || strings.TrimSpace(sysAction) == "" {
		t.Fatalf("ToHostAction: %q err %v", sysAction, err)
	}
}

func TestRequestApiAccessNormalizesHosts(t *testing.T) {
	tool := NewRequestApiAccessTool()
	// Tolerates a pasted URL, lowercases, strips path, dedups.
	out, err := tool.Invoke(context.Background(), json.RawMessage(
		`{"hosts":["https://API.Example.com/v1/search?q=x","api.example.com","brave.com"]}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	cc, _ := ParseCapabilityChange(out)
	var p struct {
		Egress []string `json:"egress"`
	}
	_ = json.Unmarshal(cc.Payload, &p)
	want := map[string]bool{"api.example.com": true, "brave.com": true}
	if len(p.Egress) != 2 {
		t.Fatalf("hosts = %v, want 2 deduped/normalized", p.Egress)
	}
	for _, h := range p.Egress {
		if !want[h] {
			t.Fatalf("unexpected host %q in %v", h, p.Egress)
		}
	}
}

func TestRequestApiAccessRejectsEmptyAndBadHosts(t *testing.T) {
	tool := NewRequestApiAccessTool()
	for _, bad := range []string{
		`{"hosts":[]}`,
		`{"hosts":["   "]}`,
		`{"hosts":["has space"]}`,
		`{"hosts":["*.wild.card"]}`,
	} {
		if _, err := tool.Invoke(context.Background(), json.RawMessage(bad)); err == nil {
			t.Fatalf("expected error for %s", bad)
		}
	}
}

func TestRequestApiAccessRegistersNotForbidden(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(NewRequestApiAccessTool()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := reg.Get(RequestApiAccessToolName); !ok {
		t.Fatal("request_api_access not registered")
	}
}
