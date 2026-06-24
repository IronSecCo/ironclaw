package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// TestRequestCapabilityChange_MCPRegisterRoundTrip proves the in-sandbox tool accepts an
// mcp_register proposal, validates the kind, and re-renders it into the frozen host
// system-action wire format with the action discriminator and payload preserved — the
// exact envelope host delivery parses and routes through the mandatory gateway.
func TestRequestCapabilityChange_MCPRegisterRoundTrip(t *testing.T) {
	tool := NewRequestCapabilityChangeTool()
	payload := `{"name":"weather","transport":"http","url":"https://weather.example.com/mcp","headers":{"Authorization":"${WEATHER_TOKEN}"}}`
	input := json.RawMessage(`{"kind":"mcp_register","payload":` + payload + `,"reason":"need weather data"}`)

	out, err := tool.Invoke(context.Background(), input)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	// The envelope parses and carries the mcp_register kind.
	cc, err := ParseCapabilityChange(out)
	if err != nil {
		t.Fatalf("ParseCapabilityChange: %v", err)
	}
	if cc.Kind != contract.ChangeMCPRegister {
		t.Fatalf("kind = %q, want mcp_register", cc.Kind)
	}

	// ToHostAction renders the host system-action wire format.
	hostAction, err := tool.ToHostAction(out)
	if err != nil {
		t.Fatalf("ToHostAction: %v", err)
	}
	a := contract.ParseSystemAction(hostAction)
	if a.Action != "mcp_register" {
		t.Fatalf("system action = %q, want mcp_register", a.Action)
	}
	if a.Reason != "need weather data" {
		t.Fatalf("reason = %q, want preserved", a.Reason)
	}
	// The proposed definition survives verbatim so the gateway verifier and human
	// approver see the real endpoint.
	var got, want map[string]any
	if err := json.Unmarshal(a.Payload, &got); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(payload), &want); err != nil {
		t.Fatalf("want payload not JSON: %v", err)
	}
	if got["name"] != want["name"] || got["transport"] != want["transport"] || got["url"] != want["url"] {
		t.Fatalf("payload not preserved: got %v", got)
	}
}

// TestRequestCapabilityChange_RejectsUnknownKind guards the in-sandbox allowlist: a kind
// the contract does not define is rejected before it can reach the queue.
func TestRequestCapabilityChange_RejectsUnknownKind(t *testing.T) {
	tool := NewRequestCapabilityChangeTool()
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{"kind":"mcp_takeover","payload":{}}`))
	if err == nil {
		t.Fatal("expected an unknown-kind rejection")
	}
}
