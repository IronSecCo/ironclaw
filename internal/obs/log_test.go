// OWNER: T-101

package obs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewTextFormatWritesKeyValue(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Format: FormatText, Output: &buf})
	l.Info("hello", "user", "alice")

	out := buf.String()
	if !strings.Contains(out, "msg=hello") {
		t.Fatalf("text output missing msg: %q", out)
	}
	if !strings.Contains(out, "user=alice") {
		t.Fatalf("text output missing attr: %q", out)
	}
}

func TestNewJSONFormatIsParseable(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Format: FormatJSON, Output: &buf})
	l.Info("started", "port", 8080)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("JSON output not parseable: %v (%q)", err, buf.String())
	}
	if rec["msg"] != "started" {
		t.Fatalf("msg = %v, want started", rec["msg"])
	}
	if rec["port"].(float64) != 8080 {
		t.Fatalf("port = %v, want 8080", rec["port"])
	}
}

func TestLevelFiltersBelowThreshold(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Output: &buf, Level: slog.LevelWarn})
	l.Info("suppressed")
	l.Warn("kept")

	out := buf.String()
	if strings.Contains(out, "suppressed") {
		t.Fatalf("info record should have been filtered: %q", out)
	}
	if !strings.Contains(out, "kept") {
		t.Fatalf("warn record should be present: %q", out)
	}
}

func TestComponentAddsAttr(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Format: FormatJSON, Output: &buf}).Component("host/gateway")
	l.Info("applied")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["component"] != "host/gateway" {
		t.Fatalf("component = %v, want host/gateway", rec["component"])
	}
}

func TestWithKeepsWrapperType(t *testing.T) {
	var buf bytes.Buffer
	// With must return *Logger so a subsequent Component call compiles/runs.
	l := New(Options{Format: FormatJSON, Output: &buf}).With("request_id", "r-1").Component("host/api")
	l.Info("ok")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["request_id"] != "r-1" || rec["component"] != "host/api" {
		t.Fatalf("attrs lost through With/Component chain: %v", rec)
	}
}

func TestDiscardProducesNoOutput(t *testing.T) {
	l := Discard()
	l.Error("should vanish", "x", 1) // must not panic; nothing to assert on output
}

func TestDefaultIsUsable(t *testing.T) {
	if Default() == nil {
		t.Fatal("Default returned nil")
	}
}
