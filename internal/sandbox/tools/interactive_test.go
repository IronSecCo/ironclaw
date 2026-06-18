package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestAskUserQuestionToolEmitsWire(t *testing.T) {
	tool := NewAskUserQuestionTool()
	out, err := tool.Invoke(context.Background(),
		json.RawMessage(`{"question":"Deploy where?","options":["staging","prod"],"allow_freeform":true}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if contract.SystemActionName(out) != contract.ActionAskUser {
		t.Fatalf("discriminator not %q: %s", contract.ActionAskUser, out)
	}
	req, err := contract.ParseAskUserRequest(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.Question != "Deploy where?" || len(req.Options) != 2 || !req.AllowFreeform {
		t.Fatalf("round-trip lost fields: %+v", req)
	}
	// HostForwarder forwards the body verbatim.
	body, err := tool.ToHostAction(out)
	if err != nil || body != out {
		t.Fatalf("ToHostAction should forward verbatim, got (%q, %v)", body, err)
	}
}

func TestAskUserQuestionValidation(t *testing.T) {
	tool := NewAskUserQuestionTool()
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"question":"   "}`)); err == nil {
		t.Fatal("empty question should error")
	}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`not json`)); err == nil {
		t.Fatal("invalid input should error")
	}
	// Blank options are trimmed out.
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"question":"q","options":["a","  ",""]}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	req, _ := contract.ParseAskUserRequest(out)
	if len(req.Options) != 1 || req.Options[0] != "a" {
		t.Fatalf("blank options should be trimmed, got %+v", req.Options)
	}
}

func TestAskUserQuestionRegistrable(t *testing.T) {
	// It emits chat-like guidance, not a capability change, so it must register.
	reg := NewRegistry()
	if err := reg.Register(NewAskUserQuestionTool()); err != nil {
		t.Fatalf("register: %v", err)
	}
}
