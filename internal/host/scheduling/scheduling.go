// OWNER: AGENT1

// Package scheduling holds the pure logic for scheduled prompts: validation and
// next-occurrence computation. It is host-internal and deliberately stdlib-only.
//
// SECURITY NOTE — no script field, no RCE. A ScheduledRequest carries ONLY a
// prompt to feed the agent at a future time. Unlike the legacy reference system,
// which let a schedule entry carry an arbitrary shell/script field (an unapproved
// remote-code-execution surface), an IronClaw schedule can never execute anything
// directly. The sweep loop merely re-enqueues the prompt as an ordinary inbound
// message; any privileged action that prompt might request still has to pass
// through the gateway's human-approval choke point. Scheduling adds no new
// execution path.
package scheduling

import (
	"fmt"
	"strings"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// ScheduledRequest is a request to run a prompt at a future time, optionally
// recurring. There is intentionally NO script/command field (see package note).
type ScheduledRequest struct {
	// Prompt is the text fed to the agent when the schedule fires.
	Prompt string
	// RunAt is when the request first becomes due.
	RunAt time.Time
	// Recurrence is "" (one-shot) or one of the named cadences ("hourly", "daily",
	// "weekly") or a Go duration interval like "15m" / "2h".
	Recurrence string
}

// Named recurrence cadences. Pinned in the frozen contract (they cross the seam
// in contract.ScheduleRequest.Recurrence and MessageIn.Recurrence); aliased here
// so the host logic and the wire format can never drift.
const (
	RecurrenceHourly = contract.RecurrenceHourly
	RecurrenceDaily  = contract.RecurrenceDaily
	RecurrenceWeekly = contract.RecurrenceWeekly
)

// Validate checks a ScheduledRequest. It rejects an empty prompt and any
// recurrence that is neither "" (one-shot), a named cadence, nor a parseable Go
// duration (e.g. "15m", "2h"). A non-positive duration is rejected. Crucially,
// validation has nothing to validate beyond the prompt and cadence — there is no
// script to sanitize because there is no script field.
func Validate(req ScheduledRequest) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return fmt.Errorf("host/scheduling: scheduled request requires a non-empty prompt")
	}
	rec := strings.TrimSpace(req.Recurrence)
	switch rec {
	case "", RecurrenceHourly, RecurrenceDaily, RecurrenceWeekly:
		return nil
	}
	d, err := time.ParseDuration(rec)
	if err != nil {
		return fmt.Errorf("host/scheduling: invalid recurrence %q (want \"\", %q, %q, %q, or a duration like \"15m\"/\"2h\"): %w",
			req.Recurrence, RecurrenceHourly, RecurrenceDaily, RecurrenceWeekly, err)
	}
	if d <= 0 {
		return fmt.Errorf("host/scheduling: recurrence duration %q must be positive", req.Recurrence)
	}
	return nil
}

// NextRun computes the next occurrence after prev for the given recurrence. The
// bool is false when the schedule is non-recurring ("") or the recurrence is
// invalid (callers should Validate first; NextRun is conservative and returns
// false rather than a bogus time on a bad cadence).
//
// Named cadences advance by a fixed calendar-ish step (hour/day/week); duration
// cadences advance by the parsed duration. The returned time is prev + step.
func NextRun(prev time.Time, recurrence string) (time.Time, bool) {
	rec := strings.TrimSpace(recurrence)
	switch rec {
	case "":
		return time.Time{}, false
	case RecurrenceHourly:
		return prev.Add(time.Hour), true
	case RecurrenceDaily:
		return prev.Add(24 * time.Hour), true
	case RecurrenceWeekly:
		return prev.Add(7 * 24 * time.Hour), true
	}
	d, err := time.ParseDuration(rec)
	if err != nil || d <= 0 {
		return time.Time{}, false
	}
	return prev.Add(d), true
}
