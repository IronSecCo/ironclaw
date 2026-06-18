package skills

import (
	"strings"
	"testing"
)

// knownTools is a representative compiled-sandbox tool registry for tests. In
// production the loader is given the real set (cmd/sandbox/buildTools).
func knownTools() map[string]bool {
	return map[string]bool{
		"http_fetch": true, "send_message": true, "send_file": true,
		"schedule": true, "read_file": true, "write_file": true, "list_dir": true,
	}
}

const validManifest = `
apiVersion: ironclaw.dev/skill/v1
name: incident-triage
version: 1.4.0
description: Triage alerts and draft a status update.
grants:
  persona: |
    You are an on-call triage assistant. Be terse, cite alert IDs.
  tools:
    - http_fetch
    - send_message
    - schedule
  egress:
    - api.pagerduty.com
    - status.example.com
  assets:
    - templates/status-update.md
    - runbooks/sev1.md
signature: minisign:RWxyz...
`

func TestParseValidManifest(t *testing.T) {
	m, err := Parse([]byte(validManifest), knownTools())
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if m.Name != "incident-triage" || m.Version != "1.4.0" {
		t.Errorf("unexpected identity: %+v", m)
	}
	if len(m.Grants.Tools) != 3 || len(m.Grants.Egress) != 2 || len(m.Grants.Assets) != 2 {
		t.Errorf("unexpected grants: %+v", m.Grants)
	}
	if !strings.Contains(m.Grants.Persona, "on-call triage") {
		t.Errorf("persona block scalar not parsed: %q", m.Grants.Persona)
	}
	if m.Signature == "" {
		t.Errorf("signature should be recorded (verification is separate)")
	}
}

func TestRejectsUnknownTool(t *testing.T) {
	manifest := strings.Replace(validManifest, "- schedule", "- install_packages", 1)
	_, err := Parse([]byte(manifest), knownTools())
	if err == nil {
		t.Fatal("expected rejection of a tool outside the compiled registry")
	}
	if !strings.Contains(err.Error(), "install_packages") {
		t.Errorf("error should name the offending tool: %v", err)
	}
}

func TestRejectsBadAPIVersion(t *testing.T) {
	manifest := strings.Replace(validManifest, "ironclaw.dev/skill/v1", "ironclaw.dev/skill/v2", 1)
	if _, err := Parse([]byte(manifest), knownTools()); err == nil {
		t.Fatal("expected rejection of an unsupported apiVersion")
	}
}

func TestRejectsBadEgress(t *testing.T) {
	for _, bad := range []string{"https://api.x.com", "api.x.com:443", "api.x.com/v1", "*.x.com", "api x.com"} {
		manifest := strings.Replace(validManifest, "api.pagerduty.com", bad, 1)
		if _, err := Parse([]byte(manifest), knownTools()); err == nil {
			t.Errorf("expected rejection of egress %q", bad)
		}
	}
}

func TestRejectsBadAsset(t *testing.T) {
	// `""` is the explicit YAML empty-string element (a real empty asset), not a
	// dropped null list item.
	for _, bad := range []string{"/etc/passwd", "../../secrets", "..", `""`} {
		manifest := strings.Replace(validManifest, "templates/status-update.md", bad, 1)
		if _, err := Parse([]byte(manifest), knownTools()); err == nil {
			t.Errorf("expected rejection of asset path %q", bad)
		}
	}
}

func TestRejectsUnknownField(t *testing.T) {
	manifest := validManifest + "\nrunCommand: rm -rf /\n"
	if _, err := Parse([]byte(manifest), knownTools()); err == nil {
		t.Fatal("expected rejection of an unknown top-level field (KnownFields fail-closed)")
	}
}

func TestRejectsMalformedYAML(t *testing.T) {
	if _, err := Parse([]byte("name: [unterminated"), knownTools()); err == nil {
		t.Fatal("expected a parse error on malformed YAML")
	}
}

func TestRejectsMissingNameAndVersion(t *testing.T) {
	manifest := "apiVersion: ironclaw.dev/skill/v1\ndescription: nameless\n"
	_, err := Parse([]byte(manifest), knownTools())
	if err == nil {
		t.Fatal("expected rejection when name and version are missing")
	}
	if !strings.Contains(err.Error(), "name") || !strings.Contains(err.Error(), "version") {
		t.Errorf("error should report both missing fields: %v", err)
	}
}

func TestEmptyGrantsIsValid(t *testing.T) {
	// A skill that only contributes a persona (no tools/egress/assets) is legal.
	manifest := "apiVersion: ironclaw.dev/skill/v1\nname: persona-only\nversion: 0.1.0\ngrants:\n  persona: hello\n"
	if _, err := Parse([]byte(manifest), knownTools()); err != nil {
		t.Fatalf("a persona-only skill should be valid: %v", err)
	}
}
