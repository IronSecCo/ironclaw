// OWNER: AGENT2

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// ScheduleTaskToolName is the name of the scheduling tool. It matches the host's
// non-privileged schedule action (contract.ActionScheduleTask): the host enqueues
// a future inbound prompt and executes nothing.
const ScheduleTaskToolName = contract.ActionScheduleTask

// ScheduleTaskTool lets the agent schedule a prompt to be delivered back to itself
// at a future time, optionally recurring. It performs NO privileged action and —
// by design — carries no script/command field: it emits a contract.ScheduleRequest
// that the loop forwards to the host as a system message. The host re-validates it
// and enqueues a future inbound message; any privileged action that future prompt
// then requests still passes through the gateway. This is the sanctioned way to
// "do something later" without an execution path.
type ScheduleTaskTool struct{}

// NewScheduleTaskTool constructs the scheduling tool.
func NewScheduleTaskTool() *ScheduleTaskTool { return &ScheduleTaskTool{} }

func (t *ScheduleTaskTool) Name() string { return ScheduleTaskToolName }

func (t *ScheduleTaskTool) Description() string {
	return "Schedule a prompt to be delivered back to you at a future time, optionally recurring. " +
		"This does NOT run any code or command — it only re-queues a prompt for your future self to act on, " +
		"and any privileged action that prompt then needs still requires human approval via the gateway. " +
		"Use it for reminders and recurring check-ins."
}

func (t *ScheduleTaskTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"prompt":{"type":"string","description":"The prompt to deliver to yourself when the schedule fires."},` +
		`"run_at":{"type":"string","description":"When to first run, as an RFC3339 timestamp (e.g. 2026-06-16T09:00:00Z). Omit for now."},` +
		`"recurrence":{"type":"string","description":"Optional cadence: \"hourly\", \"daily\", \"weekly\", or a Go duration like \"15m\"/\"2h\". Omit for one-shot."}` +
		`},"required":["prompt"],"additionalProperties":false}`)
}

type scheduleTaskInput struct {
	Prompt     string `json:"prompt"`
	RunAt      string `json:"run_at"`
	Recurrence string `json:"recurrence"`
}

// Invoke validates the request and returns the contract.ScheduleRequest wire body.
// It mutates nothing: the loop forwards the body to the host (see ToHostAction),
// which re-validates and enqueues. Validation here is early UX feedback; the host
// is the authority.
func (t *ScheduleTaskTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in scheduleTaskInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("%s: invalid input: %w", ScheduleTaskToolName, err)
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return "", fmt.Errorf("%s: prompt is required", ScheduleTaskToolName)
	}
	if rt := strings.TrimSpace(in.RunAt); rt != "" {
		if _, err := time.Parse(time.RFC3339, rt); err != nil {
			return "", fmt.Errorf("%s: run_at must be RFC3339 (e.g. 2026-06-16T09:00:00Z): %w", ScheduleTaskToolName, err)
		}
	}
	if err := validateRecurrence(in.Recurrence); err != nil {
		return "", err
	}
	return contract.MarshalScheduleRequest(contract.ScheduleRequest{
		Prompt:     in.Prompt,
		RunAt:      strings.TrimSpace(in.RunAt),
		Recurrence: strings.TrimSpace(in.Recurrence),
	})
}

// ToHostAction implements HostForwarder: the Invoke output is already the
// schedule_task wire body, so it forwards verbatim (after a parse check).
func (t *ScheduleTaskTool) ToHostAction(toolOutput string) (string, error) {
	if _, err := contract.ParseScheduleRequest(toolOutput); err != nil {
		return "", err
	}
	return toolOutput, nil
}

// validateRecurrence mirrors the host's accepted cadences (host/scheduling.Validate
// is the authority and re-checks). The sandbox cannot import the host package, so
// the small rule is duplicated for early feedback: "" (one-shot), a named cadence,
// or a positive Go duration.
func validateRecurrence(rec string) error {
	switch strings.TrimSpace(rec) {
	case "", contract.RecurrenceHourly, contract.RecurrenceDaily, contract.RecurrenceWeekly:
		return nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(rec))
	if err != nil || d <= 0 {
		return fmt.Errorf("%s: invalid recurrence %q (want \"\", %q, %q, %q, or a positive duration like \"15m\")",
			ScheduleTaskToolName, rec, contract.RecurrenceHourly, contract.RecurrenceDaily, contract.RecurrenceWeekly)
	}
	return nil
}
