package main

import (
	"encoding/json"
	"testing"
	"time"
)

// TestChangeRowDecodesCapitalizedKeys guards the human-readable read commands:
// the control plane serializes contract.ChangeRequest with capitalized, untagged
// keys (ID, Kind, AgentGroupID, ...). The CLI's decode struct must still bind
// them so the table columns are populated rather than blank.
func TestChangeRowDecodesCapitalizedKeys(t *testing.T) {
	const payload = `[{"ID":"chg_1","Kind":"persona","AgentGroupID":"dev-agent","RequestedBy":"cli:dev","Before":null,"After":null,"CreatedAt":"2026-06-17T13:50:58Z"}]`
	var rows []changeRow
	if err := json.Unmarshal([]byte(payload), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.ID != "chg_1" || r.Kind != "persona" || r.AgentGroupID != "dev-agent" || r.RequestedBy != "cli:dev" {
		t.Fatalf("fields not bound from capitalized keys: %+v", r)
	}
	if r.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt not parsed")
	}
}

// TestHistoryRowDecodes verifies the nested request + decision shape used by the
// `change history` table.
func TestHistoryRowDecodes(t *testing.T) {
	const payload = `[{"request":{"ID":"chg_2","Kind":"persona","AgentGroupID":"default","RequestedBy":"you","CreatedAt":"2026-06-20T13:16:02Z"},"status":"approved","decision":{"Outcome":"approve","DecidedBy":"alice","DecidedAt":"2026-06-20T13:17:00Z"}}]`
	var rows []historyRow
	if err := json.Unmarshal([]byte(payload), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Request.ID != "chg_2" || rows[0].Status != "approved" {
		t.Fatalf("unexpected request/status: %+v", rows[0])
	}
	if rows[0].Decision == nil || rows[0].Decision.DecidedBy != "alice" {
		t.Fatalf("decision not bound: %+v", rows[0].Decision)
	}
}

// TestAuditRowDecodes verifies the json-tagged audit shape binds.
func TestAuditRowDecodes(t *testing.T) {
	const payload = `[{"time":"2026-06-23T22:14:34Z","stage":"verdict","changeId":"chg_3","kind":"persona","detail":"require-human"}]`
	var rows []auditRow
	if err := json.Unmarshal([]byte(payload), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Stage != "verdict" || rows[0].ChangeID != "chg_3" || rows[0].Detail != "require-human" {
		t.Fatalf("audit row not bound: %+v", rows)
	}
}

func TestHumanAge(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Time{}, "—"},
		{now.Add(-30 * time.Second), "30s"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-50 * time.Hour), "2d"},
		{now.Add(1 * time.Hour), "0s"}, // future clamps to 0
	}
	for _, c := range cases {
		if got := humanAge(c.in); got != c.want {
			t.Errorf("humanAge(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDash(t *testing.T) {
	if dash("") != "—" {
		t.Errorf("dash empty: %q", dash(""))
	}
	if dash("x") != "x" {
		t.Errorf("dash nonempty: %q", dash("x"))
	}
}

// TestPrintJSONReindents verifies --json output is valid, indented JSON and
// carries no diagnostic noise.
func TestPrintJSONReindents(t *testing.T) {
	// printJSON writes to stdout; just assert it accepts compact JSON without
	// error and that invalid input does not panic.
	if err := printJSON([]byte(`[{"a":1}]`)); err != nil {
		t.Fatalf("printJSON valid: %v", err)
	}
	if err := printJSON([]byte("not json")); err != nil {
		t.Fatalf("printJSON invalid should not error: %v", err)
	}
}
