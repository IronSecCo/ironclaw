package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// TestScheduleTaskInvoke asserts the tool emits a valid schedule_task wire body
// (contract.ScheduleRequest) and that the loop-facing ToHostAction forwards it.
func TestScheduleTaskInvoke(t *testing.T) {
	tool := NewScheduleTaskTool()
	if tool.Name() != contract.ActionScheduleTask {
		t.Fatalf("name = %q, want %q", tool.Name(), contract.ActionScheduleTask)
	}

	out, err := tool.Invoke(context.Background(), json.RawMessage(
		`{"prompt":"check the deploy","run_at":"2026-06-16T09:00:00Z","recurrence":"daily"}`))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	r, err := contract.ParseScheduleRequest(out)
	if err != nil {
		t.Fatalf("output is not a ScheduleRequest: %v", err)
	}
	if r.Action != contract.ActionScheduleTask || r.Prompt != "check the deploy" || r.Recurrence != contract.RecurrenceDaily {
		t.Fatalf("wire body wrong: %+v", r)
	}

	// HostForwarder: the loop forwards the body verbatim to the host.
	body, err := tool.ToHostAction(out)
	if err != nil || body != out {
		t.Fatalf("ToHostAction = (%q, %v), want verbatim forward", body, err)
	}
	if _, ok := interface{}(tool).(HostForwarder); !ok {
		t.Fatal("ScheduleTaskTool must implement HostForwarder")
	}
}

// TestScheduleTaskRejectsBadInput covers the early-feedback validation: a missing
// prompt, a non-RFC3339 run_at, and an invalid recurrence are all refused
// in-sandbox (the host re-validates regardless).
func TestScheduleTaskRejectsBadInput(t *testing.T) {
	tool := NewScheduleTaskTool()
	bad := []string{
		`{"prompt":""}`,
		`{"prompt":"x","run_at":"tomorrow"}`,
		`{"prompt":"x","recurrence":"fortnightly"}`,
	}
	for _, in := range bad {
		if _, err := tool.Invoke(context.Background(), json.RawMessage(in)); err == nil {
			t.Fatalf("expected rejection for %s", in)
		}
	}
}

// TestScheduleToolNotForbidden guards that scheduling is registerable — it is a
// non-privileged host action, unlike the forbidden self-capability tools.
func TestScheduleToolNotForbidden(t *testing.T) {
	if IsForbidden(contract.ActionScheduleTask) {
		t.Fatal("schedule_task must not be on the forbidden list")
	}
	if err := NewRegistry().Register(NewScheduleTaskTool()); err != nil {
		t.Fatalf("register schedule tool: %v", err)
	}
}
