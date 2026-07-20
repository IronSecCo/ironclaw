package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specsFromFixture parses a k8s manifest fixture into pod specs.
func specsFromFixture(t *testing.T, name string) []Spec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	specs, err := SpecsFromK8sStream(raw)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return specs
}

// TestCheckPolicyInsecure: the fully-insecure fixture breaks EVERY guardrail rule
// — one violation per checkRules key, so the gate rejects it.
func TestCheckPolicyInsecure(t *testing.T) {
	res := CheckPolicy(specsFromFixture(t, "emitpolicy_insecure.yaml"))
	if res.OK() {
		t.Fatal("insecure manifest passed the gate; want failure")
	}
	if res.Workloads != 1 {
		t.Errorf("workloads = %d, want 1", res.Workloads)
	}
	if len(res.Violations) != len(checkRules) {
		t.Errorf("violations = %d, want %d (one per enforceable rule)", len(res.Violations), len(checkRules))
	}
	for _, v := range res.Violations {
		if _, ok := policyRules[v.Key]; !ok {
			t.Errorf("violation key %q has no guardrail rule", v.Key)
		}
		if v.Requirement == "" {
			t.Errorf("violation %q has empty requirement", v.Key)
		}
		if v.Verdict != VerdictFail {
			t.Errorf("violation %q verdict = %q, want FAIL", v.Key, v.Verdict)
		}
	}
}

// TestCheckPolicyHardened: a hardened manifest satisfies every rule and passes the
// gate. This exercises the deny-the-bad polarity: a pod that simply never sets
// hostNetwork/hostPID/hostIPC/runtime-socket is admitted, matching the emitted
// rule (even though the scorer cannot certify network=none from a bare pod spec).
func TestCheckPolicyHardened(t *testing.T) {
	res := CheckPolicy(specsFromFixture(t, "check_hardened.yaml"))
	if !res.OK() {
		t.Fatalf("hardened manifest failed the gate: %+v", res.Violations)
	}
	if len(res.Violations) != 0 {
		t.Errorf("violations = %d, want 0", len(res.Violations))
	}
}

// TestCheckRulesMatchPolicyRules: the gate enforces EXACTLY the dimensions
// --emit-policy generates rules for — the two maps must share their key set so
// generate and enforce never diverge.
func TestCheckRulesMatchPolicyRules(t *testing.T) {
	if len(checkRules) != len(policyRules) {
		t.Fatalf("checkRules has %d keys, policyRules has %d", len(checkRules), len(policyRules))
	}
	for k := range policyRules {
		if _, ok := checkRules[k]; !ok {
			t.Errorf("policyRules has %q but checkRules does not enforce it", k)
		}
	}
}

// TestCheckPolicyParityOnInsecure: for a fully-insecure manifest the controls the
// gate flags are exactly the ones --emit-policy would generate rules for
// (failingDims), confirming the enforce view matches the generate view.
func TestCheckPolicyParityOnInsecure(t *testing.T) {
	specs := specsFromFixture(t, "emitpolicy_insecure.yaml")
	reports := make([]Report, len(specs))
	for i, s := range specs {
		reports[i] = Score(s)
	}
	gotKeys := map[string]bool{}
	for _, v := range CheckPolicy(specs).Violations {
		gotKeys[v.Key] = true
	}
	for _, k := range failingDims(reports) {
		if !gotKeys[k] {
			t.Errorf("--emit-policy generates a rule for %q but the gate did not flag it", k)
		}
	}
	if len(gotKeys) != len(failingDims(reports)) {
		t.Errorf("gate flagged %d controls, --emit-policy would generate %d", len(gotKeys), len(failingDims(reports)))
	}
}

// TestCheckPolicyFailClosedUnknown: for a require-present control (non-root), an
// absent/unknown field is a violation — fail-closed, matching the emitted rule
// that rejects a pod without runAsNonRoot: true.
func TestCheckPolicyFailClosedUnknown(t *testing.T) {
	res := CheckPolicy([]Spec{{Target: "empty"}}) // all fields Unknown/zero
	if res.OK() {
		t.Fatal("empty spec passed the gate; want fail-closed on require-present controls")
	}
	// Require-present controls (non-root, caps, seccomp, rootfs) must all fire;
	// deny-present controls (network, namespaces, docker.sock) admit an absent field.
	present := map[string]bool{}
	for _, v := range res.Violations {
		present[v.Key] = true
	}
	for _, k := range []string{"user.nonroot", "caps.dropped", "seccomp", "rootfs.readonly"} {
		if !present[k] {
			t.Errorf("require-present control %q did not fail closed on an empty spec", k)
		}
	}
	for _, k := range []string{"network.isolated", "namespaces.host", "docker.sock"} {
		if present[k] {
			t.Errorf("deny-present control %q fired on an absent field; should admit", k)
		}
	}
}

func TestRenderCheckTextPass(t *testing.T) {
	var b strings.Builder
	RenderCheckText(&b, CheckResult{Workloads: 2})
	if out := b.String(); !strings.HasPrefix(out, "PASS") {
		t.Errorf("pass output does not start with PASS:\n%s", out)
	}
}

func TestRenderCheckTextFail(t *testing.T) {
	var b strings.Builder
	RenderCheckText(&b, CheckPolicy(specsFromFixture(t, "emitpolicy_insecure.yaml")))
	out := b.String()
	if !strings.HasPrefix(out, "FAIL") {
		t.Errorf("fail output does not start with FAIL:\n%s", out)
	}
	if !strings.Contains(out, "require:") {
		t.Errorf("fail output missing requirement lines:\n%s", out)
	}
}

func TestRenderCheckMarkdown(t *testing.T) {
	md := RenderCheckMarkdown(CheckPolicy(specsFromFixture(t, "emitpolicy_insecure.yaml")))
	if !strings.Contains(md, "**FAIL**") {
		t.Errorf("markdown missing FAIL banner:\n%s", md)
	}
	if !strings.Contains(md, "| Workload | Control | Requirement | Found |") {
		t.Errorf("markdown missing table header:\n%s", md)
	}
	ok := RenderCheckMarkdown(CheckResult{Workloads: 1})
	if !strings.Contains(ok, "**PASS**") {
		t.Errorf("clean markdown missing PASS banner:\n%s", ok)
	}
}
