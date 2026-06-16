// OWNER: T-101

package obs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"key", "Key", "session_key", "sessionKey", "master-key", "apiKey",
		"api_key", "token", "access_token", "AuthToken", "password", "passwd",
		"secret", "client_secret", "Authorization", "bearer", "credential",
		"privateKey", "seed", "otp", "pin",
	}
	for _, k := range sensitive {
		if !IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", k)
		}
	}

	benign := []string{
		"monkey", "keyspace", "user", "port", "msg", "component", "count",
		"latency_ms", "donkey", "tokenizer_name", "pineapple",
	}
	for _, k := range benign {
		if IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = true, want false (false positive)", k)
		}
	}
}

func TestRedactionMasksSensitiveAttrInOutput(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Format: FormatJSON, Output: &buf})
	l.Info("hand-off", "session_key", "deadbeefcafe", "user", "alice")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["session_key"] != Masked {
		t.Fatalf("session_key = %v, want %q", rec["session_key"], Masked)
	}
	if rec["user"] != "alice" {
		t.Fatalf("user was altered: %v", rec["user"])
	}
	if bytes.Contains(buf.Bytes(), []byte("deadbeefcafe")) {
		t.Fatalf("raw secret leaked into output: %q", buf.String())
	}
}

func TestDisableRedactionPassesValueThrough(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Format: FormatJSON, Output: &buf, DisableRedaction: true})
	l.Info("debug", "token", "raw-value")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["token"] != "raw-value" {
		t.Fatalf("token = %v, want raw-value with redaction disabled", rec["token"])
	}
}

func TestSecretHelperMasksRegardlessOfKey(t *testing.T) {
	a := Secret("payload", "super-sensitive")
	if a.Value.String() != Masked {
		t.Fatalf("Secret value = %q, want %q", a.Value.String(), Masked)
	}
	if a.Key != "payload" {
		t.Fatalf("Secret key = %q, want payload", a.Key)
	}
}

func TestSecretInOutputEvenWithBenignKey(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Format: FormatJSON, Output: &buf})
	// "payload" is not a sensitive key, so only the explicit Secret() wrapper
	// keeps the value out of the log.
	l.Info("forward", Secret("payload", "top-secret-body"))

	if bytes.Contains(buf.Bytes(), []byte("top-secret-body")) {
		t.Fatalf("Secret() value leaked: %q", buf.String())
	}
}

func TestRedactionWalksGroups(t *testing.T) {
	var buf bytes.Buffer
	l := New(Options{Format: FormatJSON, Output: &buf})
	l.Info("nested", slog.Group("auth", "token", "abc123", "scheme", "bearer-x"))

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	auth, ok := rec["auth"].(map[string]any)
	if !ok {
		t.Fatalf("auth group missing: %v", rec)
	}
	if auth["token"] != Masked {
		t.Fatalf("nested token = %v, want %q", auth["token"], Masked)
	}
	if bytes.Contains(buf.Bytes(), []byte("abc123")) {
		t.Fatalf("nested secret leaked: %q", buf.String())
	}
}

func TestRedactStringHelper(t *testing.T) {
	if RedactString("anything") != Masked {
		t.Fatalf("RedactString = %q, want %q", RedactString("anything"), Masked)
	}
}
