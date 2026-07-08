package scan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sampleReport() Report {
	return Score(Spec{
		Source: "docker", Target: "ic-sbx-demo", Runtime: "runsc",
		RunAsNonRoot: Yes, User: "65532", CapDropAll: Yes, Seccomp: "confined",
		NetworkMode: "none", ReadonlyRoot: Yes, DockerSock: No,
		HostPID: No, HostIPC: No, HostNetwork: No,
	})
}

func TestRenderTable(t *testing.T) {
	var b bytes.Buffer
	RenderTable(&b, sampleReport())
	out := b.String()
	for _, want := range []string{"ic-sbx-demo", "100/100", "grade A", "Non-root user", "Dropped capabilities"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
}

func TestRenderJSON(t *testing.T) {
	var b bytes.Buffer
	if err := RenderJSON(&b, sampleReport()); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["schemaVersion"] != "1.0" {
		t.Errorf("schemaVersion=%v", m["schemaVersion"])
	}
	if m["score"].(float64) != 100 {
		t.Errorf("score=%v", m["score"])
	}
	if _, ok := m["dimensions"].([]any); !ok {
		t.Error("dimensions array missing")
	}
}

// --json must carry the remediation when a plan is passed (--fix), and omit it
// otherwise. Fail-closed parity between the human and machine outputs.
func TestRenderJSON_WithRemediation(t *testing.T) {
	s := weakDockerSpec()
	plan := Remediate(s, Score(s))
	var b bytes.Buffer
	if err := RenderJSON(&b, Score(s), &plan); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	rem, ok := m["remediation"].(map[string]any)
	if !ok {
		t.Fatal("remediation key missing from JSON")
	}
	if _, ok := rem["items"].([]any); !ok {
		t.Error("remediation.items missing")
	}
	if snip, _ := rem["snippet"].(string); !strings.Contains(snip, "docker run") {
		t.Errorf("remediation.snippet not carried: %v", rem["snippet"])
	}

	// Without a plan, no remediation key.
	var b2 bytes.Buffer
	if err := RenderJSON(&b2, sampleReport()); err != nil {
		t.Fatal(err)
	}
	var m2 map[string]any
	_ = json.Unmarshal(b2.Bytes(), &m2)
	if _, present := m2["remediation"]; present {
		t.Error("remediation should be absent when no plan is passed")
	}
}

func TestRenderBadgeSVG(t *testing.T) {
	svg := RenderBadgeSVG(sampleReport())
	if !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "100/100 A") {
		t.Errorf("badge malformed: %s", svg)
	}
	if !strings.Contains(svg, gradeColor("A")) {
		t.Error("badge missing grade color")
	}
	// A failing report must render red, not green.
	fail := RenderBadgeSVG(Score(Spec{}))
	if !strings.Contains(fail, gradeColor("F")) {
		t.Error("failing badge should be red")
	}
}

func TestRenderBadgeEndpointJSON(t *testing.T) {
	var pass BadgeEndpoint
	if err := json.Unmarshal([]byte(RenderBadgeEndpointJSON(sampleReport())), &pass); err != nil {
		t.Fatalf("badge JSON is not valid: %v", err)
	}
	if pass.SchemaVersion != 1 {
		t.Errorf("schemaVersion = %d, want 1", pass.SchemaVersion)
	}
	if pass.Label != "sandbox isolation" {
		t.Errorf("label = %q", pass.Label)
	}
	if pass.Message != "100/100 A" {
		t.Errorf("message = %q, want 100/100 A", pass.Message)
	}
	// Color is the bare hex (no '#') so shields accepts it, and matches the SVG.
	if want := strings.TrimPrefix(gradeColor("A"), "#"); pass.Color != want {
		t.Errorf("color = %q, want %q", pass.Color, want)
	}

	// A failing report must render red, not green.
	var fail BadgeEndpoint
	if err := json.Unmarshal([]byte(RenderBadgeEndpointJSON(Score(Spec{}))), &fail); err != nil {
		t.Fatalf("failing badge JSON invalid: %v", err)
	}
	if want := strings.TrimPrefix(gradeColor("F"), "#"); fail.Color != want {
		t.Errorf("failing badge color = %q, want %q (red)", fail.Color, want)
	}
}

func TestRenderMarkdown(t *testing.T) {
	md := RenderMarkdown(sampleReport())
	for _, want := range []string{"ic-sbx-demo", "100/100", "| Dimension |", "ironctl scan"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n%s", want, md)
		}
	}
	// Public-copy house style: no em/en-dashes (IRO-254).
	if strings.ContainsAny(md, "—–") {
		t.Error("markdown contains an em/en-dash")
	}
}
