// OWNER: AGENT1

package scheduling

import (
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     ScheduledRequest
		wantErr bool
	}{
		{"ok one-shot", ScheduledRequest{Prompt: "do a thing"}, false},
		{"ok hourly", ScheduledRequest{Prompt: "p", Recurrence: "hourly"}, false},
		{"ok daily", ScheduledRequest{Prompt: "p", Recurrence: "daily"}, false},
		{"ok weekly", ScheduledRequest{Prompt: "p", Recurrence: "weekly"}, false},
		{"ok 15m interval", ScheduledRequest{Prompt: "p", Recurrence: "15m"}, false},
		{"ok 2h interval", ScheduledRequest{Prompt: "p", Recurrence: "2h"}, false},
		{"ok trimmed recurrence", ScheduledRequest{Prompt: "p", Recurrence: "  daily  "}, false},
		{"empty prompt", ScheduledRequest{Prompt: ""}, true},
		{"whitespace prompt", ScheduledRequest{Prompt: "   "}, true},
		{"bad recurrence word", ScheduledRequest{Prompt: "p", Recurrence: "fortnightly"}, true},
		{"zero duration", ScheduledRequest{Prompt: "p", Recurrence: "0s"}, true},
		{"negative duration", ScheduledRequest{Prompt: "p", Recurrence: "-5m"}, true},
		{"garbage", ScheduledRequest{Prompt: "p", Recurrence: "abc"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%+v) err = %v, wantErr=%v", tt.req, err, tt.wantErr)
			}
		})
	}
}

func TestNextRun(t *testing.T) {
	base := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		recurrence string
		want       time.Time
		ok         bool
	}{
		{"one-shot", "", time.Time{}, false},
		{"hourly", "hourly", base.Add(time.Hour), true},
		{"daily", "daily", base.Add(24 * time.Hour), true},
		{"weekly", "weekly", base.Add(7 * 24 * time.Hour), true},
		{"15m", "15m", base.Add(15 * time.Minute), true},
		{"2h", "2h", base.Add(2 * time.Hour), true},
		{"trimmed", "  daily ", base.Add(24 * time.Hour), true},
		{"invalid word", "fortnightly", time.Time{}, false},
		{"invalid duration", "xyz", time.Time{}, false},
		{"zero duration", "0s", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NextRun(base, tt.recurrence)
			if ok != tt.ok {
				t.Fatalf("NextRun(%q) ok = %v, want %v", tt.recurrence, ok, tt.ok)
			}
			if ok && !got.Equal(tt.want) {
				t.Fatalf("NextRun(%q) = %v, want %v", tt.recurrence, got, tt.want)
			}
		})
	}
}
