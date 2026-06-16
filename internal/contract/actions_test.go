package contract

import (
	"encoding/json"
	"testing"
)

// TestSystemActionWireFormat locks the JSON shape the sandbox writes and the host
// reads: {"action","payload","reason"}, with payload/reason omitted when empty.
func TestSystemActionWireFormat(t *testing.T) {
	s, err := MarshalSystemAction(SystemAction{
		Action:  string(ChangePersona),
		Payload: json.RawMessage(`{"persona":"helpful"}`),
		Reason:  "tone tweak",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"action":"persona","payload":{"persona":"helpful"},"reason":"tone tweak"}`
	if s != want {
		t.Fatalf("wire format drift:\n got %s\nwant %s", s, want)
	}

	a := ParseSystemAction(s)
	if a.Action != "persona" || a.Reason != "tone tweak" {
		t.Fatalf("round-trip lost fields: %+v", a)
	}
	if string(a.Payload) != `{"persona":"helpful"}` {
		t.Fatalf("payload drift: %s", a.Payload)
	}

	// Omitempty: a bare action carries no payload/reason keys.
	bare, _ := MarshalSystemAction(SystemAction{Action: "typing"})
	if bare != `{"action":"typing"}` {
		t.Fatalf("omitempty drift: %s", bare)
	}
}

// TestParseSystemActionTotal asserts parsing never panics and degrades sensibly:
// a JSON object yields its action; a bare string yields itself (trimmed); junk
// yields the trimmed content as the action name.
func TestParseSystemActionTotal(t *testing.T) {
	cases := map[string]string{
		`{"action":"install_packages","payload":{}}`: "install_packages",
		"  typing  ": "typing",
		"{not json":  "{not json",
		"":           "",
	}
	for in, want := range cases {
		if got := SystemActionName(in); got != want {
			t.Fatalf("SystemActionName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScheduleRequestWireFormat locks the schedule_task body: a flat shape sharing
// only "action" with the envelope, carrying a prompt + timing and NO script field.
func TestScheduleRequestWireFormat(t *testing.T) {
	s, err := MarshalScheduleRequest(ScheduleRequest{
		Prompt:     "summarize my inbox",
		RunAt:      "2026-06-16T09:00:00Z",
		Recurrence: RecurrenceDaily,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"action":"schedule_task","prompt":"summarize my inbox","run_at":"2026-06-16T09:00:00Z","recurrence":"daily"}`
	if s != want {
		t.Fatalf("wire format drift:\n got %s\nwant %s", s, want)
	}

	// MarshalScheduleRequest forces the discriminator even if the caller omits it,
	// so host.SystemActionName always routes the body to schedule handling.
	if SystemActionName(s) != ActionScheduleTask {
		t.Fatalf("discriminator not %q: %s", ActionScheduleTask, s)
	}

	r, err := ParseScheduleRequest(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.Prompt != "summarize my inbox" || r.RunAt != "2026-06-16T09:00:00Z" || r.Recurrence != RecurrenceDaily {
		t.Fatalf("round-trip lost fields: %+v", r)
	}
}
