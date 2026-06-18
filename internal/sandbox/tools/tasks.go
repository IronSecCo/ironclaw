package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// Task-management tools let the agent manage the prompts it has previously
// scheduled (via schedule_task): list them, and cancel / pause / resume / update a
// single one. Like schedule_task they perform NO privileged action and carry NO
// script/command field — each emits a contract.SystemAction the loop forwards to
// the host, which applies it to the host-side scheduling store
// (internal/host/scheduling). Managing a task only changes WHICH prompt the agent
// later reads; any privileged action that prompt then needs still passes through
// the gateway. The host re-authorizes every forwarded action; the sandbox never
// acts on it directly.
//
// The tool names ARE the system-action discriminators and MIRROR the host's
// scheduling.Action* constants. The sandbox cannot import the host package, so the
// strings are duplicated across the seam and pinned by tests on both sides.
const (
	ListTasksToolName  = "list_tasks"
	CancelTaskToolName = "cancel_task"
	PauseTaskToolName  = "pause_task"
	ResumeTaskToolName = "resume_task"
	UpdateTaskToolName = "update_task"
)

// taskManagePayload is the contract.SystemAction.Payload for a task-management
// action. Its JSON shape mirrors host/scheduling.ManagePayload so the host decodes
// it directly.
type taskManagePayload struct {
	TaskID     string `json:"task_id,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	RunAt      string `json:"run_at,omitempty"`
	Recurrence string `json:"recurrence,omitempty"`
}

// marshalTaskAction renders a task-management system-action wire body: a
// contract.SystemAction whose Action is the tool name and whose Payload is the
// JSON-encoded taskManagePayload (omitted when empty, e.g. for list_tasks).
func marshalTaskAction(action string, p taskManagePayload) (string, error) {
	sa := contract.SystemAction{Action: action}
	if p != (taskManagePayload{}) {
		payload, err := json.Marshal(p)
		if err != nil {
			return "", fmt.Errorf("%s: marshal payload: %w", action, err)
		}
		sa.Payload = payload
	}
	return contract.MarshalSystemAction(sa)
}

// forwardTaskAction is the shared HostForwarder body: it confirms the tool output
// is a well-formed system action carrying the expected discriminator, then
// forwards it verbatim for the host to apply.
func forwardTaskAction(expected, toolOutput string) (string, error) {
	if got := contract.SystemActionName(toolOutput); got != expected {
		return "", fmt.Errorf("%s: tool output is not a %s system action (got %q)", expected, expected, got)
	}
	return toolOutput, nil
}

// requireTaskID parses a {"task_id": ...} input and returns the trimmed id,
// rejecting a missing one. Shared by cancel / pause / resume / update.
func requireTaskID(toolName string, input json.RawMessage) (string, taskManagePayload, error) {
	var in taskManagePayload
	if err := json.Unmarshal(input, &in); err != nil {
		return "", taskManagePayload{}, fmt.Errorf("%s: invalid input: %w", toolName, err)
	}
	id := strings.TrimSpace(in.TaskID)
	if id == "" {
		return "", taskManagePayload{}, fmt.Errorf("%s: task_id is required", toolName)
	}
	in.TaskID = id
	return id, in, nil
}

// --- list_tasks ------------------------------------------------------------

// ListTasksTool asks the host to return the agent's live (non-cancelled) scheduled
// tasks. It takes no input. The listing comes back asynchronously as a follow-up
// message from the host (the tool only requests it).
type ListTasksTool struct{}

// NewListTasksTool constructs the list_tasks tool.
func NewListTasksTool() *ListTasksTool { return &ListTasksTool{} }

func (t *ListTasksTool) Name() string { return ListTasksToolName }

func (t *ListTasksTool) Description() string {
	return "List the prompts you have scheduled (via schedule_task) that are still pending, with their ids, " +
		"next run time, recurrence, and whether each is active or paused. Use the ids with cancel_task, " +
		"pause_task, resume_task, or update_task. The list is returned to you as a follow-up message."
}

func (t *ListTasksTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}

func (t *ListTasksTool) Invoke(_ context.Context, _ json.RawMessage) (string, error) {
	return marshalTaskAction(ListTasksToolName, taskManagePayload{})
}

func (t *ListTasksTool) ToHostAction(toolOutput string) (string, error) {
	return forwardTaskAction(ListTasksToolName, toolOutput)
}

var _ HostForwarder = (*ListTasksTool)(nil)

// --- cancel / pause / resume (task_id only) --------------------------------

// taskIDTool implements the three management tools that take only a task_id:
// cancel_task, pause_task, resume_task. Each emits the matching system action.
type taskIDTool struct {
	name string
	desc string
}

func (t *taskIDTool) Name() string        { return t.name }
func (t *taskIDTool) Description() string { return t.desc }
func (t *taskIDTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"task_id":{"type":"string","description":"The id of the scheduled task (from list_tasks)."}` +
		`},"required":["task_id"],"additionalProperties":false}`)
}

func (t *taskIDTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	_, payload, err := requireTaskID(t.name, input)
	if err != nil {
		return "", err
	}
	return marshalTaskAction(t.name, taskManagePayload{TaskID: payload.TaskID})
}

func (t *taskIDTool) ToHostAction(toolOutput string) (string, error) {
	return forwardTaskAction(t.name, toolOutput)
}

var _ HostForwarder = (*taskIDTool)(nil)

// NewCancelTaskTool constructs the cancel_task tool.
func NewCancelTaskTool() *taskIDTool {
	return &taskIDTool{
		name: CancelTaskToolName,
		desc: "Permanently cancel a scheduled task so it never runs again. Provide its task_id (from list_tasks). " +
			"This only removes a future prompt; it runs no code.",
	}
}

// NewPauseTaskTool constructs the pause_task tool.
func NewPauseTaskTool() *taskIDTool {
	return &taskIDTool{
		name: PauseTaskToolName,
		desc: "Pause a scheduled task so it stops firing until you resume it. Provide its task_id (from list_tasks). " +
			"The schedule is kept; resume_task reactivates it.",
	}
}

// NewResumeTaskTool constructs the resume_task tool.
func NewResumeTaskTool() *taskIDTool {
	return &taskIDTool{
		name: ResumeTaskToolName,
		desc: "Resume a previously paused scheduled task so it fires again on its schedule. " +
			"Provide its task_id (from list_tasks).",
	}
}

// --- update_task -----------------------------------------------------------

// UpdateTaskTool changes a scheduled task's prompt, next run time, and/or
// recurrence. At least one of those must be supplied alongside the task_id.
type UpdateTaskTool struct{}

// NewUpdateTaskTool constructs the update_task tool.
func NewUpdateTaskTool() *UpdateTaskTool { return &UpdateTaskTool{} }

func (t *UpdateTaskTool) Name() string { return UpdateTaskToolName }

func (t *UpdateTaskTool) Description() string {
	return "Update a scheduled task identified by task_id (from list_tasks): change its prompt, its next run time " +
		"(run_at, RFC3339), and/or its recurrence. Supply at least one field to change. This re-queues a future " +
		"prompt only — it runs no code, and any privileged action that prompt later needs still requires approval."
}

func (t *UpdateTaskTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"task_id":{"type":"string","description":"The id of the scheduled task to update (from list_tasks)."},` +
		`"prompt":{"type":"string","description":"New prompt to deliver when the task fires."},` +
		`"run_at":{"type":"string","description":"New first/next run time, RFC3339 (e.g. 2026-06-16T09:00:00Z)."},` +
		`"recurrence":{"type":"string","description":"New cadence: \"hourly\", \"daily\", \"weekly\", or a Go duration like \"15m\"/\"2h\"."}` +
		`},"required":["task_id"],"additionalProperties":false}`)
}

func (t *UpdateTaskTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	_, in, err := requireTaskID(UpdateTaskToolName, input)
	if err != nil {
		return "", err
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	in.RunAt = strings.TrimSpace(in.RunAt)
	in.Recurrence = strings.TrimSpace(in.Recurrence)
	if in.Prompt == "" && in.RunAt == "" && in.Recurrence == "" {
		return "", fmt.Errorf("%s: supply at least one of prompt, run_at, or recurrence to change", UpdateTaskToolName)
	}
	if in.RunAt != "" {
		if _, err := time.Parse(time.RFC3339, in.RunAt); err != nil {
			return "", fmt.Errorf("%s: run_at must be RFC3339 (e.g. 2026-06-16T09:00:00Z): %w", UpdateTaskToolName, err)
		}
	}
	if in.Recurrence != "" {
		if err := validateRecurrence(in.Recurrence); err != nil {
			return "", err
		}
	}
	return marshalTaskAction(UpdateTaskToolName, in)
}

func (t *UpdateTaskTool) ToHostAction(toolOutput string) (string, error) {
	return forwardTaskAction(UpdateTaskToolName, toolOutput)
}

var _ HostForwarder = (*UpdateTaskTool)(nil)

// TaskManagementTools returns the five task-management tools in a stable order,
// ready to register with a Registry.
func TaskManagementTools() []Tool {
	return []Tool{
		NewListTasksTool(),
		NewCancelTaskTool(),
		NewPauseTaskTool(),
		NewResumeTaskTool(),
		NewUpdateTaskTool(),
	}
}
