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
