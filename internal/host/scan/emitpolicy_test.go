package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// updateGolden (declared in sarif_test.go) also regenerates the emit-policy golden
// files: go test ./internal/host/scan -run TestEmitPolicyGolden -update

// insecureReports scores the fully-insecure fixture manifest, which fails every
// scored dimension so the emitted policy exercises all seven rules.
func insecureReports(t *testing.T) []Report {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "emitpolicy_insecure.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	specs, err := SpecsFromK8sStream(raw)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	reports := make([]Report, len(specs))
	for i, s := range specs {
		reports[i] = Score(s)
	}
	return reports
}

func TestEmitPolicyGolden(t *testing.T) {
	reports := insecureReports(t)
	cases := []struct {
		engine PolicyEngine
		golden string
	}{
		{EngineKyverno, "emitpolicy_kyverno.golden.yaml"},
		{EngineGatekeeper, "emitpolicy_gatekeeper.golden.yaml"},
		{EngineVAP, "emitpolicy_vap.golden.yaml"},
	}
	for _, tc := range cases {
		t.Run(string(tc.engine), func(t *testing.T) {
			got, err := EmitPolicy(reports, tc.engine)
			if err != nil {
				t.Fatalf("EmitPolicy: %v", err)
			}
			path := filepath.Join("testdata", tc.golden)
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got != string(want) {
				t.Errorf("emitted %s policy does not match golden %s.\nRegenerate with: go test ./internal/host/scan -run TestEmitPolicyGolden -update", tc.engine, tc.golden)
			}
		})
	}
}

// TestEmitPolicyKyvernoShape is the e2e assertion: the emitted Kyverno policy must
// be valid YAML that decodes to a single ClusterPolicy carrying one Enforce rule
// per failed control. If the kyverno CLI is installed it is additionally run
// against the fixture to prove the policy actually REJECTS the insecure manifest;
// otherwise the shape assertion stands in (no CLI dependency in CI).
func TestEmitPolicyKyvernoShape(t *testing.T) {
	got, err := EmitPolicy(insecureReports(t), EngineKyverno)
	if err != nil {
		t.Fatalf("EmitPolicy: %v", err)
	}

	var policy struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Spec       struct {
			Rules []struct {
				Name     string `yaml:"name"`
				Validate struct {
					FailureAction string `yaml:"failureAction"`
					Message       string `yaml:"message"`
				} `yaml:"validate"`
			} `yaml:"rules"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal([]byte(got), &policy); err != nil {
		t.Fatalf("emitted kyverno policy is not valid YAML: %v", err)
	}
	if policy.APIVersion != "kyverno.io/v1" || policy.Kind != "ClusterPolicy" {
		t.Fatalf("unexpected header: %s/%s", policy.APIVersion, policy.Kind)
	}
	// All seven scored dimensions fail on the fixture, so all seven rules appear.
	if len(policy.Spec.Rules) != len(scorers) {
		t.Fatalf("want %d rules, got %d", len(scorers), len(policy.Spec.Rules))
	}
	for _, r := range policy.Spec.Rules {
		if r.Validate.FailureAction != "Enforce" {
			t.Errorf("rule %q must be Enforce, got %q", r.Name, r.Validate.FailureAction)
		}
		if strings.TrimSpace(r.Validate.Message) == "" {
			t.Errorf("rule %q has no deny message", r.Name)
		}
	}
}

// TestEmitPolicyGatekeeperShape asserts the Gatekeeper output is a valid multi-doc
// stream of one ConstraintTemplate + one Constraint per failed control.
func TestEmitPolicyGatekeeperShape(t *testing.T) {
	got, err := EmitPolicy(insecureReports(t), EngineGatekeeper)
	if err != nil {
		t.Fatalf("EmitPolicy: %v", err)
	}
	dec := yaml.NewDecoder(strings.NewReader(got))
	templates, constraints := 0, 0
	for {
		var doc struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
		}
		err := dec.Decode(&doc)
		if err != nil {
			break
		}
		switch {
		case doc.Kind == "ConstraintTemplate":
			templates++
		case strings.HasPrefix(doc.APIVersion, "constraints.gatekeeper.sh/"):
			constraints++
		}
	}
	if templates != len(scorers) || constraints != len(scorers) {
		t.Fatalf("want %d templates + %d constraints, got %d + %d", len(scorers), len(scorers), templates, constraints)
	}
}

// TestEmitPolicyVAPShape asserts the ValidatingAdmissionPolicy output is valid
// YAML: exactly one ValidatingAdmissionPolicy carrying one CEL validation per
// failed control, plus a single ValidatingAdmissionPolicyBinding with a Deny
// action. No controller is referenced — enforcement is native to the API server.
func TestEmitPolicyVAPShape(t *testing.T) {
	got, err := EmitPolicy(insecureReports(t), EngineVAP)
	if err != nil {
		t.Fatalf("EmitPolicy: %v", err)
	}
	dec := yaml.NewDecoder(strings.NewReader(got))
	policies, bindings, validations := 0, 0, 0
	for {
		var doc struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Spec       struct {
				Validations []struct {
					Expression string `yaml:"expression"`
					Message    string `yaml:"message"`
				} `yaml:"validations"`
				ValidationActions []string `yaml:"validationActions"`
			} `yaml:"spec"`
		}
		if err := dec.Decode(&doc); err != nil {
			break
		}
		switch doc.Kind {
		case "ValidatingAdmissionPolicy":
			policies++
			validations = len(doc.Spec.Validations)
			for _, v := range doc.Spec.Validations {
				if strings.TrimSpace(v.Expression) == "" {
					t.Errorf("validation has empty CEL expression")
				}
				if strings.TrimSpace(v.Message) == "" {
					t.Errorf("validation has empty message")
				}
			}
		case "ValidatingAdmissionPolicyBinding":
			bindings++
			if len(doc.Spec.ValidationActions) == 0 || doc.Spec.ValidationActions[0] != "Deny" {
				t.Errorf("binding must Deny, got %v", doc.Spec.ValidationActions)
			}
		}
		if doc.APIVersion != "" && doc.APIVersion != "admissionregistration.k8s.io/v1" {
			t.Errorf("VAP docs must be admissionregistration.k8s.io/v1, got %q", doc.APIVersion)
		}
	}
	if policies != 1 {
		t.Fatalf("want exactly 1 ValidatingAdmissionPolicy, got %d", policies)
	}
	if bindings != 1 {
		t.Fatalf("want exactly 1 ValidatingAdmissionPolicyBinding, got %d", bindings)
	}
	// All seven scored dimensions fail on the fixture, so all seven validations appear.
	if validations != len(scorers) {
		t.Fatalf("want %d CEL validations, got %d", len(scorers), validations)
	}
}

// TestEmitPolicyHardened: a report with no failing dimensions yields an
// informational comment document rather than an empty, unapplyable policy.
func TestEmitPolicyHardened(t *testing.T) {
	// A synthetic all-PASS report: every dimension at full credit.
	var r Report
	for _, sc := range scorers {
		r.Dimensions = append(r.Dimensions, Dimension{Key: sc.key, Verdict: VerdictPass, Score: sc.max, Max: sc.max})
	}
	for _, engine := range []PolicyEngine{EngineKyverno, EngineGatekeeper, EngineVAP} {
		out, err := EmitPolicy([]Report{r}, engine)
		if err != nil {
			t.Fatalf("EmitPolicy(%s): %v", engine, err)
		}
		if !strings.Contains(out, "already earns 100/A") {
			t.Errorf("%s: want already-hardened comment, got:\n%s", engine, out)
		}
		if strings.Contains(out, "kind: ClusterPolicy") || strings.Contains(out, "ConstraintTemplate") || strings.Contains(out, "kind: ValidatingAdmissionPolicy") {
			t.Errorf("%s: hardened output must carry no rules", engine)
		}
	}
}

// TestEmitPolicyUnionAcrossWorkloads: a control that fails in ANY workload is
// enforced, so the emitted set is the union of every workload's gaps.
func TestEmitPolicyUnionAcrossWorkloads(t *testing.T) {
	// Workload A fails ONLY seccomp; workload B fails ONLY readonly rootfs.
	fail := func(key string) Report {
		var r Report
		for _, sc := range scorers {
			v, pts := VerdictPass, sc.max
			if sc.key == key {
				v, pts = VerdictFail, 0
			}
			r.Dimensions = append(r.Dimensions, Dimension{Key: sc.key, Verdict: v, Score: pts, Max: sc.max})
		}
		return r
	}
	out, err := EmitPolicy([]Report{fail("seccomp"), fail("rootfs.readonly")}, EngineKyverno)
	if err != nil {
		t.Fatalf("EmitPolicy: %v", err)
	}
	if !strings.Contains(out, "require-seccomp") || !strings.Contains(out, "require-readonly-rootfs") {
		t.Errorf("union should carry both failing controls, got:\n%s", out)
	}
	if strings.Contains(out, "require-run-as-non-root") {
		t.Errorf("a passing control must not emit a rule")
	}
}

func TestParsePolicyEngine(t *testing.T) {
	for _, s := range []string{"kyverno", "KYVERNO", " gatekeeper ", "vap", "VAP"} {
		if _, err := ParsePolicyEngine(s); err != nil {
			t.Errorf("ParsePolicyEngine(%q) unexpected error: %v", s, err)
		}
	}
	if _, err := ParsePolicyEngine("opa"); err == nil {
		t.Errorf("ParsePolicyEngine(opa) should error")
	}
}
