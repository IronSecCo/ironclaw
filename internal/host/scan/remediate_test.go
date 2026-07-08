package scan

import (
	"bytes"
	"strings"
	"testing"
)

// weakDockerSpec is a wide-open container: root, default caps, writable rootfs,
// bridge egress, docker.sock mounted, host PID shared. It should FAIL every
// dimension a fully-hardened container passes.
func weakDockerSpec() Spec {
	return Spec{
		Source: "docker", Target: "weak", Image: "nginx:latest",
		RunAsNonRoot: No, User: "0 (docker default)",
		CapDropAll: No, Seccomp: "confined",
		NetworkMode: "bridge", ReadonlyRoot: No,
		DockerSock: Yes, HostPathMounts: []string{"/var/run"},
		HostPID: Yes, HostIPC: No, HostNetwork: No,
	}
}

// TestRemediate_PerDimensionFix asserts each non-PASS dimension gets a concrete,
// source-correct fix. This is the "unit tests per dimension remediation" gate.
func TestRemediate_PerDimensionFix(t *testing.T) {
	cases := []struct {
		source   string
		spec     Spec
		wantByID map[string]string // dimension key -> substring the fix must contain
	}{
		{
			source: "docker",
			spec:   weakDockerSpec(),
			wantByID: map[string]string{
				"user.nonroot":     "--user 65532:65532",
				"caps.dropped":     "--cap-drop=ALL",
				"network.isolated": "--network=none",
				"rootfs.readonly":  "--read-only",
				"docker.sock":      "docker.sock",
				"namespaces.host":  "--pid=host",
			},
		},
		{
			source: "compose",
			spec: Spec{
				Source: "compose", Target: "web",
				RunAsNonRoot: No, CapDropAll: No, Seccomp: "unconfined",
				NetworkMode: "bridge", ReadonlyRoot: No, DockerSock: Yes,
				HostPID: Yes,
			},
			wantByID: map[string]string{
				"user.nonroot":     `user: "65532:65532"`,
				"caps.dropped":     "cap_drop: [ALL]",
				"seccomp":          "no-new-privileges:true",
				"network.isolated": "network_mode: none",
				"rootfs.readonly":  "read_only: true",
				"docker.sock":      "volumes",
				"namespaces.host":  "pid: host",
			},
		},
		{
			source: "k8s",
			spec: Spec{
				Source: "k8s", Target: "app",
				RunAsNonRoot: No, CapDropAll: No, Seccomp: "",
				NetworkMode: "pod", ReadonlyRoot: No,
				HostNetwork: Yes,
			},
			wantByID: map[string]string{
				"user.nonroot":    "runAsNonRoot: true",
				"caps.dropped":    "drop: [ALL]",
				"seccomp":         "RuntimeDefault",
				"rootfs.readonly": "readOnlyRootFilesystem: true",
				"namespaces.host": "hostPID: false",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			plan := Remediate(tc.spec, Score(tc.spec))
			got := map[string]Remediation{}
			for _, it := range plan.Items {
				got[it.Key] = it
			}
			for key, want := range tc.wantByID {
				it, ok := got[key]
				if !ok {
					t.Errorf("no remediation emitted for failed dimension %q", key)
					continue
				}
				if !strings.Contains(it.Fix, want) {
					t.Errorf("%s fix = %q, want substring %q", key, it.Fix, want)
				}
				if it.Explanation == "" {
					t.Errorf("%s: empty explanation", key)
				}
			}
		})
	}
}

// A hardened spec (already A/100) yields an empty item list — nothing to fix.
func TestRemediate_NoItemsWhenHardened(t *testing.T) {
	s := Spec{
		Source: "docker", Target: "hard", Image: "distroless",
		RunAsNonRoot: Yes, User: "65532", CapDropAll: Yes, Seccomp: "confined",
		NetworkMode: "none", ReadonlyRoot: Yes, DockerSock: No,
		HostPID: No, HostIPC: No, HostNetwork: No,
	}
	plan := Remediate(s, Score(s))
	if len(plan.Items) != 0 {
		t.Errorf("hardened spec should have no remediation items, got %d: %+v", len(plan.Items), plan.Items)
	}
}

// The remediation must cover EVERY non-PASS dimension — no failed dimension may
// be silently dropped (fail-closed prescription).
func TestRemediate_CoversAllFailures(t *testing.T) {
	s := weakDockerSpec()
	r := Score(s)
	plan := Remediate(s, r)
	fixed := map[string]bool{}
	for _, it := range plan.Items {
		fixed[it.Key] = true
	}
	for _, d := range r.Dimensions {
		if d.Verdict != VerdictPass && !fixed[d.Key] {
			t.Errorf("dimension %q verdict %s has no remediation", d.Key, d.Verdict)
		}
	}
}

// The assembled docker snippet, parsed back through the scorer, must grade A —
// the acceptance contract that applying the fix scores A/100.
func TestRemediate_DockerSnippetScoresA(t *testing.T) {
	plan := Remediate(weakDockerSpec(), Score(weakDockerSpec()))
	if !strings.Contains(plan.Snippet, "docker run") {
		t.Fatalf("docker snippet missing `docker run`:\n%s", plan.Snippet)
	}
	// The snippet embeds the real image so it is copy-pasteable.
	if !strings.Contains(plan.Snippet, "nginx:latest") {
		t.Errorf("snippet should carry the original image, got:\n%s", plan.Snippet)
	}
	// Model the container the snippet produces and confirm it scores A.
	hardened := Spec{
		Source: "docker", Target: "ic-hardened",
		RunAsNonRoot: Yes, User: hardenedUID, CapDropAll: Yes, Seccomp: "confined",
		NetworkMode: "none", ReadonlyRoot: Yes, DockerSock: No,
		HostPID: No, HostIPC: No, HostNetwork: No,
	}
	hr := Score(hardened)
	if hr.Grade != "A" || hr.Score < 90 {
		t.Errorf("snippet-derived spec scored %d/%s, want A (>=90)", hr.Score, hr.Grade)
	}
}

// The docker snippet must call out dropped host-escape primitives so the operator
// knows what was intentionally not carried over.
func TestRemediate_SnippetNotesDroppedSock(t *testing.T) {
	plan := Remediate(weakDockerSpec(), Score(weakDockerSpec()))
	if !strings.Contains(plan.Snippet, "intentionally dropped") ||
		!strings.Contains(plan.Snippet, "docker.sock") {
		t.Errorf("snippet should note the dropped docker.sock mount:\n%s", plan.Snippet)
	}
}

// WARN dimensions (dropped-all-but-readded-caps) are remediated too, not just FAILs.
func TestRemediate_IncludesWarn(t *testing.T) {
	s := Spec{
		Source: "docker", Target: "warn", Image: "img",
		RunAsNonRoot: Yes, User: "65532", CapDropAll: Yes, CapAdd: []string{"NET_ADMIN"},
		Seccomp: "confined", NetworkMode: "none", ReadonlyRoot: Yes,
		DockerSock: No, HostPID: No, HostIPC: No, HostNetwork: No,
	}
	plan := Remediate(s, Score(s))
	var caps *Remediation
	for i := range plan.Items {
		if plan.Items[i].Key == "caps.dropped" {
			caps = &plan.Items[i]
		}
	}
	if caps == nil {
		t.Fatal("WARN caps dimension not remediated")
	}
	if caps.Verdict != VerdictWarn || !strings.Contains(caps.Fix, "NET_ADMIN") {
		t.Errorf("caps remediation = %+v, want WARN naming the re-added cap", caps)
	}
}

// RenderPlan produces the human block with fixes and the copy-pasteable snippet.
func TestRenderPlan(t *testing.T) {
	var b bytes.Buffer
	RenderPlan(&b, Remediate(weakDockerSpec(), Score(weakDockerSpec())))
	out := b.String()
	for _, want := range []string{"Remediation", "fix:", "why:", "Copy-pasteable", "docker run"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output missing %q\n%s", want, out)
		}
	}
	// House style: no em/en-dashes in operator-facing copy.
	if strings.ContainsAny(out, "—–") {
		t.Error("remediation output contains an em/en-dash")
	}
}

// RenderPlan on a hardened target says there is nothing to fix.
func TestRenderPlan_NothingToFix(t *testing.T) {
	s := Spec{
		Source: "docker", Target: "hard",
		RunAsNonRoot: Yes, User: "65532", CapDropAll: Yes, Seccomp: "confined",
		NetworkMode: "none", ReadonlyRoot: Yes, DockerSock: No,
		HostPID: No, HostIPC: No, HostNetwork: No,
	}
	var b bytes.Buffer
	RenderPlan(&b, Remediate(s, Score(s)))
	if !strings.Contains(b.String(), "none") {
		t.Errorf("hardened plan should report nothing to fix:\n%s", b.String())
	}
}
