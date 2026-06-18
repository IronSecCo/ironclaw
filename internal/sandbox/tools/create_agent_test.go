package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestCreateAgentToolEmitsSystemAction(t *testing.T) {
	tool := NewCreateAgentTool()
	out, err := tool.Invoke(context.Background(), json.RawMessage(
		`{"name":"researcher","folder":"research","persona":{"instructions":"dig"},"enabled_tools":["read_file"],"reason":"need a researcher"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	a := contract.ParseSystemAction(out)
	if a.Action != string(contract.ChangeCreateAgent) {
		t.Fatalf("action = %q, want %q", a.Action, contract.ChangeCreateAgent)
	}
	if a.Reason != "need a researcher" {
		t.Fatalf("reason = %q", a.Reason)
	}
	var payload struct {
		Name         string   `json:"name"`
		Folder       string   `json:"folder"`
		EnabledTools []string `json:"enabled_tools"`
	}
	if err := json.Unmarshal(a.Payload, &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload.Name != "researcher" || payload.Folder != "research" || len(payload.EnabledTools) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestCreateAgentToolRequiresName(t *testing.T) {
	tool := NewCreateAgentTool()
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"name":"  "}`)); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateAgentToolRejectsTraversal(t *testing.T) {
	tool := NewCreateAgentTool()
	for _, bad := range []string{
		`{"name":"../escape"}`,
		`{"name":"ok","folder":"../../etc"}`,
		`{"name":"a/b"}`,
	} {
		if _, err := tool.Invoke(context.Background(), json.RawMessage(bad)); err == nil {
			t.Fatalf("expected rejection for %s", bad)
		}
	}
}

func TestCreateAgentToHostActionRoundTrips(t *testing.T) {
	tool := NewCreateAgentTool()
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"name":"helper"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	fwd, err := tool.ToHostAction(out)
	if err != nil {
		t.Fatalf("ToHostAction: %v", err)
	}
	if fwd != out {
		t.Fatalf("ToHostAction altered the body: %q vs %q", fwd, out)
	}
	if _, err := tool.ToHostAction(`{"action":"persona","payload":{}}`); err == nil {
		t.Fatal("ToHostAction should reject a non-create_agent action")
	}
}

func TestCreateAgentToolRegisters(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(NewCreateAgentTool()); err != nil {
		t.Fatalf("register create_agent: %v", err)
	}
	if _, ok := reg.Get(CreateAgentToolName); !ok {
		t.Fatal("create_agent not registered")
	}
}
