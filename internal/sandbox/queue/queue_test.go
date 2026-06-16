// OWNER: AGENT2

package queue

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestNextOddSeq(t *testing.T) {
	cases := []struct {
		name    string
		maxSeq  int64
		present bool
		want    int64
	}{
		{"empty table", 0, false, 1},
		{"after odd 1", 1, true, 3},
		{"after odd 3", 3, true, 5},
		{"after odd 5", 5, true, 7},
		{"defensive even max 0", 0, true, 1},
		{"defensive even max 2", 2, true, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextOddSeq(tc.maxSeq, tc.present); got != tc.want {
				t.Fatalf("nextOddSeq(%d,%v) = %d, want %d", tc.maxSeq, tc.present, got, tc.want)
			}
			if got := nextOddSeq(tc.maxSeq, tc.present); tc.present && got%2 == 0 {
				t.Fatalf("nextOddSeq(%d,%v) = %d is even; sandbox must write odd seq", tc.maxSeq, tc.present, got)
			}
		})
	}
}

func TestTimeRoundTrip(t *testing.T) {
	want := time.Date(2026, 6, 16, 12, 34, 56, 123456789, time.UTC)
	s := formatTime(want)

	got, err := parseTime(sql.NullString{String: s, Valid: true})
	if err != nil {
		t.Fatalf("parseTime: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("round trip: got %v, want %v", got, want)
	}
}

func TestParseTimeEmpty(t *testing.T) {
	got, err := parseTime(sql.NullString{Valid: false})
	if err != nil {
		t.Fatalf("parseTime(null): %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("parseTime(null) = %v, want zero", got)
	}
}

func TestParseNullTime(t *testing.T) {
	if got, err := parseNullTime(sql.NullString{Valid: false}); err != nil || got != nil {
		t.Fatalf("parseNullTime(null) = (%v,%v), want (nil,nil)", got, err)
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := parseNullTime(sql.NullString{String: formatTime(want), Valid: true})
	if err != nil {
		t.Fatalf("parseNullTime: %v", err)
	}
	if got == nil || !got.Equal(want) {
		t.Fatalf("parseNullTime = %v, want %v", got, want)
	}
}

func TestIsCorruption(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not a database", errors.New("file is not a database"), true},
		{"malformed", errors.New("database disk image is malformed"), true},
		{"encrypted", errors.New("file is encrypted or is not a database"), true},
		{"binding pending is not corruption", contract.ErrCryptoBindingPending, false},
		{"unrelated", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCorruption(tc.err); got != tc.want {
				t.Fatalf("isCorruption(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestNullStr(t *testing.T) {
	if got := nullStr(sql.NullString{Valid: false}); got != nil {
		t.Fatalf("nullStr(invalid) = %v, want nil", got)
	}
	got := nullStr(sql.NullString{String: "x", Valid: true})
	if got == nil || *got != "x" {
		t.Fatalf("nullStr(valid) = %v, want *\"x\"", got)
	}
}

// TestOpenPropagatesBindingPending asserts the queue surfaces the pending crypto
// binding unchanged rather than mis-classifying it as corruption.
func TestOpenPropagatesBindingPending(t *testing.T) {
	_, err := OpenOutbound("/tmp/does-not-matter.db", contract.SessionKey{})
	if !errors.Is(err, contract.ErrCryptoBindingPending) {
		t.Fatalf("OpenOutbound err = %v, want ErrCryptoBindingPending", err)
	}

	// OpenInbound swallows the pending binding so it can retry per poll; it must
	// return a usable reader and not the error.
	r, err := OpenInbound("/tmp/does-not-matter.db", contract.SessionKey{})
	if err != nil {
		t.Fatalf("OpenInbound err = %v, want nil (binding pending is retried per poll)", err)
	}
	if r == nil {
		t.Fatal("OpenInbound returned nil reader")
	}
	if _, err := r.PendingMessages(true); !errors.Is(err, contract.ErrCryptoBindingPending) {
		t.Fatalf("PendingMessages err = %v, want ErrCryptoBindingPending", err)
	}
}
