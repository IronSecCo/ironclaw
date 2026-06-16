// OWNER: T-084

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// decodeSystemAction parses a tool's wire output into a SystemAction and its
// task-management payload.
func decodeSystemAction(t *testing.T, out string) (contract.SystemAction, taskManagePayload) {
	t.Helper()
	sa := contract.ParseSystemAction(out)
	var p taskManagePayload
	if len(sa.Payload) > 0 {
		if err := json.Unmarshal(sa.Payload, &p); err != nil {
			t.Fatalf("payload is not valid JSON: %v (raw %s)", err, sa.Payload)
		}
	}
	return sa, p
}

func TestTaskToolNamesArePinned(t *testing.T) {
	pins := map[string]string{
		NewListTasksTool().Name():  "list_tasks",
		NewCancelTaskTool().Name(): "cancel_task",
		NewPauseTaskTool().Name():  "pause_task",
		NewResumeTaskTool().Name(): "resume_task",
		NewUpdateTaskTool().Name(): "update_task",
	}
	for got, want := range pins {
		if got != want {
			t.Fatalf("tool name = %q, want %q (mirrors host/scheduling.Action*)", got, want)
		}
	}
}

func TestListTasksEmitsSystemAction(t *testing.T) {
	tool := NewListTasksTool()
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	sa, _ := decodeSystemAction(t, out)
	if sa.Action != ListTasksToolName {
		t.Fatalf("action = %q, want %q", sa.Action, ListTasksToolName)
	}
	if len(sa.Payload) != 0 {
		t.Fatalf("list_tasks should carry no payload, got %s", sa.Payload)
	}
	// HostForwarder forwards verbatim.
	body, err := tool.ToHostAction(out)
	if err != nil || body != out {
		t.Fatalf("ToHostAction = (%q, %v), want verbatim forward", body, err)
	}
}

func TestTaskIDToolsEmitSystemAction(t *testing.T) {
	cases := []struct {
		name string
		tool interface {
			Tool
			HostForwarder
		}
		action string
	}{
		{"cancel", NewCancelTaskTool(), CancelTaskToolName},
		{"pause", NewPauseTaskTool(), PauseTaskToolName},
		{"resume", NewResumeTaskTool(), ResumeTaskToolName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.tool.Invoke(context.Background(), json.RawMessage(`{"task_id":"abc123"}`))
			if err != nil {
				t.Fatalf("invoke: %v", err)
			}
			sa, p := decodeSystemAction(t, out)
			if sa.Action != tc.action {
				t.Fatalf("action = %q, want %q", sa.Action, tc.action)
			}
			if p.TaskID != "abc123" {
				t.Fatalf("task_id = %q, want abc123", p.TaskID)
			}
			body, err := tc.tool.ToHostAction(out)
			if err != nil || body != out {
				t.Fatalf("ToHostAction = (%q, %v), want verbatim forward", body, err)
			}
		})
	}
}

func TestTaskIDToolsRejectMissingID(t *testing.T) {
	tools := []Tool{NewCancelTaskTool(), NewPauseTaskTool(), NewResumeTaskTool(), NewUpdateTaskTool()}
	for _, tool := range tools {
		for _, in := range []string{`{}`, `{"task_id":""}`, `{"task_id":"   "}`} {
			if _, err := tool.Invoke(context.Background(), json.RawMessage(in)); err == nil {
				t.Fatalf("%s: expected rejection for %s", tool.Name(), in)
			}
		}
	}
}

func TestUpdateTaskEmitsSystemAction(t *testing.T) {
	tool := NewUpdateTaskTool()
	out, err := tool.Invoke(context.Background(), json.RawMessage(
		`{"task_id":"t1","prompt":"new prompt","run_at":"2026-06-16T09:00:00Z","recurrence":"daily"}`))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	sa, p := decodeSystemAction(t, out)
	if sa.Action != UpdateTaskToolName {
		t.Fatalf("action = %q, want %q", sa.Action, UpdateTaskToolName)
	}
	if p.TaskID != "t1" || p.Prompt != "new prompt" || p.RunAt != "2026-06-16T09:00:00Z" || p.Recurrence != "daily" {
		t.Fatalf("payload wrong: %+v", p)
	}
	// A single-field update is allowed.
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"task_id":"t1","prompt":"only prompt"}`)); err != nil {
		t.Fatalf("single-field update: %v", err)
	}
}

func TestUpdateTaskRejectsBadInput(t *testing.T) {
	tool := NewUpdateTaskTool()
	bad := []string{
		`{"task_id":"t1"}`,                            // no field to change
		`{"task_id":"t1","run_at":"tomorrow"}`,        // bad run_at
		`{"task_id":"t1","recurrence":"fortnightly"}`, // bad recurrence
		`{"prompt":"x"}`,                              // missing task_id
	}
	for _, in := range bad {
		if _, err := tool.Invoke(context.Background(), json.RawMessage(in)); err == nil {
			t.Fatalf("expected rejection for %s", in)
		}
	}
}

// TestTaskToolsAreForwarders asserts every task tool forwards to the host (so the
// loop writes its action to the outbound queue) rather than acting in-sandbox.
func TestTaskToolsAreForwarders(t *testing.T) {
	for _, tool := range TaskManagementTools() {
		if _, ok := interface{}(tool).(HostForwarder); !ok {
			t.Fatalf("%s must implement HostForwarder", tool.Name())
		}
		if _, ok := interface{}(tool).(OutboundEmitter); ok {
			t.Fatalf("%s must NOT be an OutboundEmitter (task management goes through the host, not chat)", tool.Name())
		}
	}
}

// TestTaskToolsRegisterable guards that the management tools are non-privileged and
// can be registered (none are on the forbidden self-capability list).
func TestTaskToolsRegisterable(t *testing.T) {
	reg := NewRegistry()
	for _, tool := range TaskManagementTools() {
		if IsForbidden(tool.Name()) {
			t.Fatalf("%s must not be forbidden", tool.Name())
		}
		if err := reg.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Name(), err)
		}
	}
	if got := len(reg.Names()); got != 5 {
		t.Fatalf("registered %d task tools, want 5", got)
	}
}
