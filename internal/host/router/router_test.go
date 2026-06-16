// OWNER: AGENT1

package router

import (
	"strings"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestNamespaceUserID(t *testing.T) {
	tests := []struct {
		channel string
		handle  string
		want    contract.UserID
	}{
		{"slack", "alice", "slack:alice"},
		{"discord", "bob#1234", "discord:bob#1234"},
		// Spoofing attempt: a colon in the handle must not forge a different pair.
		{"slack", "other:owner", "slack:other"},
		{"slack", "a:b:c", "slack:a"},
		{" slack ", " alice ", "slack:alice"},
		{"slack", "", "slack:"},
	}
	for _, tt := range tests {
		got := NamespaceUserID(tt.channel, tt.handle)
		if got != tt.want {
			t.Errorf("NamespaceUserID(%q,%q) = %q, want %q", tt.channel, tt.handle, got, tt.want)
		}
	}
}

func TestNamespaceUserIDNeverContainsSpoof(t *testing.T) {
	// The result must always be channel + ":" + first-segment-of-handle.
	got := string(NamespaceUserID("slack", "other:owner"))
	if got != "slack:other" {
		t.Fatalf("spoof not stripped: %q", got)
	}
	if strings.Count(got, ":") != 1 {
		t.Fatalf("unexpected colon count in %q", got)
	}
}

func TestEvaluateEngage(t *testing.T) {
	tests := []struct {
		name      string
		mode      contract.EngageMode
		pattern   string
		text      string
		mentioned bool
		want      bool
		wantErr   bool
	}{
		{name: "pattern match-all dot", mode: contract.EngagePattern, pattern: ".", text: "anything", want: true},
		{name: "pattern match-all dot empty text", mode: contract.EngagePattern, pattern: ".", text: "", want: true},
		{name: "pattern matches", mode: contract.EngagePattern, pattern: "(?i)deploy", text: "please DEPLOY now", want: true},
		{name: "pattern no match", mode: contract.EngagePattern, pattern: "deploy", text: "hello", want: false},
		{name: "invalid regex no panic", mode: contract.EngagePattern, pattern: "(unclosed", text: "x", want: false},
		{name: "mention true", mode: contract.EngageMention, mentioned: true, want: true},
		{name: "mention false", mode: contract.EngageMention, mentioned: false, want: false},
		{name: "mention-sticky true", mode: contract.EngageMentionSticky, mentioned: true, want: true},
		{name: "mention-sticky false", mode: contract.EngageMentionSticky, mentioned: false, want: false},
		{name: "unknown mode errors", mode: contract.EngageMode("bogus"), wantErr: true},
		// A catastrophic-looking pattern: RE2 handles it in linear time, and the
		// input is capped — assert it returns without hanging or panicking. With a
		// leading ^ anchor and a trailing non-matching char, the answer is a clean
		// false; the real point is that this returns at all (no backtracking blowup).
		{name: "redos-looking pattern bounded", mode: contract.EngagePattern, pattern: "^(a+)+$", text: strings.Repeat("a", 50000) + "!", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateEngage(tt.mode, tt.pattern, tt.text, tt.mentioned)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
