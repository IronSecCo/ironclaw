// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

import (
	"encoding/json"
	"strings"
)

// This file pins the cross-seam wire formats that the sandbox WRITES to the
// outbound queue and the host READS back — and the queue status vocabulary both
// sides exchange. Before RFC-0002 these shapes lived informally in
// internal/host/delivery and internal/sandbox/tools: the host defined them and
// the sandbox reverse-engineered them, so a field rename on either side compiled
// cleanly and failed silently at runtime (a dropped system action, an
// unrecognized status). Pinning them here removes that drift class and lets the
// sandbox build against a spec instead of waiting to observe the host.

// --- System-action envelope ------------------------------------------------

// SystemAction is the wire envelope a sandbox writes as the Content of a
// KindSystem outbound message when it wants the host to act on its behalf
// (re-authorize a capability change, schedule a future prompt). The sandbox NEVER
// performs the action itself; the host re-authorizes every system action and
// routes any privileged one through the mandatory gateway.
//
// Action is the discriminator. For a capability-change request it is the string
// value of the requested ChangeKind (e.g. "persona"); for scheduling it is
// ActionScheduleTask. Payload carries the structured proposed config for a
// capability change (recorded as ChangeRequest.After so verifiers and the human
// approver see the real diff). Reason is an optional human-facing justification.
//
// A scheduling request uses ScheduleRequest, a flat shape that shares only the
// "action" field with this envelope; ParseSystemAction reads just the
// discriminator and so decodes the prefix of either shape.
type SystemAction struct {
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Reason  string          `json:"reason,omitempty"`
}

// ActionScheduleTask is the one system-action name that is NOT a ChangeKind. It
// enqueues a future inbound prompt (see ScheduleRequest); it is a non-privileged
// host action because it carries only a prompt and executes nothing — there is no
// script/command field, by design. All other canonical action names are the
// string values of ChangeKind (ChangePersona, ChangeEnabledTools, …).
const ActionScheduleTask = "schedule_task"

// MarshalSystemAction renders a SystemAction as the JSON body a sandbox writes
// into a KindSystem outbound message.
func MarshalSystemAction(a SystemAction) (string, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseSystemAction decodes a KindSystem message body into a SystemAction. The
// body is normally a JSON object; a bare, non-object body is treated as an action
// name with no payload (so a host that receives a plain "typing" still routes
// correctly). Parsing is total: an unparseable object yields the trimmed content
// as the Action.
func ParseSystemAction(content string) SystemAction {
	c := strings.TrimSpace(content)
	if strings.HasPrefix(c, "{") {
		var a SystemAction
		if err := json.Unmarshal([]byte(c), &a); err == nil && a.Action != "" {
			return a
		}
	}
	return SystemAction{Action: c}
}

// SystemActionName returns just the discriminator from a system-message body —
// the host's deterministic re-authorization keys on this.
func SystemActionName(content string) string { return ParseSystemAction(content).Action }

// --- Scheduling request ----------------------------------------------------

// ScheduleRequest is the body of an ActionScheduleTask system message: a request
// to feed Prompt to the agent at a future time, optionally recurring. It carries
// ONLY a prompt plus timing — there is intentionally NO script/command field, so
// scheduling can never become an unapproved execution path. RunAt is RFC3339;
// empty means "now". Recurrence is "" (one-shot), a named cadence
// (RecurrenceHourly/Daily/Weekly), or a Go duration like "15m".
type ScheduleRequest struct {
	Action     string `json:"action"`
	Prompt     string `json:"prompt"`
	RunAt      string `json:"run_at"`
	Recurrence string `json:"recurrence,omitempty"`
}

// Named recurrence cadences carried in ScheduleRequest.Recurrence and in
// MessageIn.Recurrence. Pinned here because both cross the seam.
const (
	RecurrenceHourly = "hourly"
	RecurrenceDaily  = "daily"
	RecurrenceWeekly = "weekly"
)

// MarshalScheduleRequest renders a ScheduleRequest as the JSON body a sandbox
// writes into an ActionScheduleTask system message. Action is forced to
// ActionScheduleTask so the host's discriminator matches.
func MarshalScheduleRequest(r ScheduleRequest) (string, error) {
	r.Action = ActionScheduleTask
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseScheduleRequest decodes an ActionScheduleTask message body.
func ParseScheduleRequest(content string) (ScheduleRequest, error) {
	var r ScheduleRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &r); err != nil {
		return ScheduleRequest{}, err
	}
	return r, nil
}

// --- Queue status vocabulary -----------------------------------------------

// Inbound and outbound rows carry a freeform Status string; these are the values
// the host and sandbox must agree on. The host is the sole writer of inbound
// status (StatusQueued/StatusScheduled) and the delivered marker (StatusDelivered);
// the sandbox is the sole writer of the outbound processing acks
// (StatusProcessing/StatusCompleted). Each side reads the other's, so the strings
// are pinned here.
const (
	// StatusQueued — host-written inbound: an immediate message, process now
	// (process_after is NULL).
	StatusQueued = "queued"
	// StatusScheduled — host-written inbound: a message that becomes processable
	// once its process_after is reached.
	StatusScheduled = "scheduled"
	// StatusProcessing — sandbox-written outbound ack: the message is in flight.
	StatusProcessing = "processing"
	// StatusCompleted — sandbox-written outbound ack: the message is done.
	StatusCompleted = "completed"
	// StatusDelivered — host-written inbound delivered marker, used for delivery
	// dedup (the host never writes outbound).
	StatusDelivered = "delivered"
)
