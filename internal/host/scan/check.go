package scan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// PolicyViolation is one enforceable containment control a checked workload
// breaks — the exact guardrail rule `--emit-policy` would generate for the SAME
// dimension, evaluated in place against the manifest instead of emitted as YAML.
type PolicyViolation struct {
	Target      string  `json:"target"`      // workload the violation was found on
	Key         string  `json:"key"`         // scorer dimension key (score.go `scorers`)
	Title       string  `json:"title"`       // human control name
	Requirement string  `json:"requirement"` // what a compliant manifest must set
	Evidence    string  `json:"evidence"`    // why it violates the rule
	Verdict     Verdict `json:"verdict"`     // always FAIL for a rule breach
}

// CheckResult is the outcome of `scan --check`: a Kubernetes manifest evaluated,
// workload by workload, against the SAME guardrail rules `--emit-policy` would
// generate. It is the enforce-in-place half of the generate/enforce loop — a
// self-contained policy-as-code gate that needs no cluster and no admission
// controller. When Violations is empty the manifest satisfies every rule the
// generated policy would enforce and the gate passes.
type CheckResult struct {
	Workloads  int               `json:"workloads"`  // number of pod specs evaluated
	Violations []PolicyViolation `json:"violations"` // broken rules, in canonical order
}

// OK reports whether the manifest passed the gate (no guardrail rule was broken).
func (c CheckResult) OK() bool { return len(c.Violations) == 0 }

// checkRule mirrors, in Go, the admission rule `--emit-policy` emits for one
// dimension (policyRules). `violated` returns true (with evidence) when the pod
// spec BREAKS that rule, using the SAME deny-the-bad semantics as the emitted
// Kyverno/Gatekeeper/VAP rule — NOT the stricter scorer verdict.
//
// The polarity therefore matches the generated policy exactly:
//   - "require-present" controls (non-root, drop-caps, seccomp, read-only rootfs)
//     demand a field be set to a safe value, so an ABSENT field is a violation
//     (fail-closed) — just as the emitted validate: pattern / CEL rejects it.
//   - "deny-present" controls (hostNetwork, host namespaces, runtime socket)
//     only block an explicitly-bad value, so an ABSENT field ADMITS — just as the
//     emitted rule admits a pod that never sets hostNetwork. This is why a clean
//     manifest can pass the gate even though the scorer cannot certify
//     network=none from a bare pod spec.
type checkRule struct {
	title       string
	requirement string
	violated    func(Spec) (bool, string)
}

// checkRules is keyed by the SAME scorer dimension keys as policyRules, so the
// gate enforces exactly the set `--emit-policy` would generate — one map, two
// views (generate vs enforce). Evaluated in canonical scorer order for stable
// output.
var checkRules = map[string]checkRule{
	"user.nonroot": {
		title:       "runs as non-root",
		requirement: "set securityContext.runAsNonRoot: true (at the pod or container level)",
		violated: func(s Spec) (bool, string) {
			if s.RunAsNonRoot == Yes || s.Rootless == Yes {
				return false, ""
			}
			return true, fmt.Sprintf("runAsNonRoot not set to true (user %q)", nz(s.User, "unset"))
		},
	},
	"caps.dropped": {
		title:       "capabilities dropped",
		requirement: "drop ALL Linux capabilities (securityContext.capabilities.drop: [ALL]) and do not run privileged",
		violated: func(s Spec) (bool, string) {
			if s.Privileged == Yes {
				return true, "runs privileged (grants the full capability set)"
			}
			if s.CapDropAll != Yes {
				return true, "does not drop ALL capabilities (securityContext.capabilities.drop: [ALL] absent)"
			}
			return false, ""
		},
	},
	"seccomp": {
		title:       "seccomp confined",
		requirement: "set spec.securityContext.seccompProfile.type to RuntimeDefault or Localhost",
		violated: func(s Spec) (bool, string) {
			if s.Privileged == Yes {
				return true, "privileged: seccomp is disabled"
			}
			if s.Seccomp != "confined" {
				return true, "seccompProfile type is not RuntimeDefault or Localhost"
			}
			return false, ""
		},
	},
	"rootfs.readonly": {
		title:       "read-only root filesystem",
		requirement: "set securityContext.readOnlyRootFilesystem: true on every container",
		violated: func(s Spec) (bool, string) {
			if s.ReadonlyRoot == Yes {
				return false, ""
			}
			return true, "readOnlyRootFilesystem not set to true"
		},
	},
	"network.isolated": {
		title:       "no host network",
		requirement: "do not set hostNetwork: true",
		violated: func(s Spec) (bool, string) {
			if s.HostNetwork == Yes {
				return true, "hostNetwork: true (full host network reachability)"
			}
			return false, ""
		},
	},
	"namespaces.host": {
		title:       "no host namespaces",
		requirement: "do not share the host PID or IPC namespace (hostPID/hostIPC: false)",
		violated: func(s Spec) (bool, string) {
			var shared []string
			if s.HostPID == Yes {
				shared = append(shared, "hostPID")
			}
			if s.HostIPC == Yes {
				shared = append(shared, "hostIPC")
			}
			if len(shared) == 0 {
				return false, ""
			}
			return true, "shares host namespace(s): " + strings.Join(shared, ", ")
		},
	},
	"docker.sock": {
		title:       "no runtime socket mount",
		requirement: "do not mount the container runtime socket (docker.sock / containerd.sock / crio.sock)",
		violated: func(s Spec) (bool, string) {
			if s.DockerSock == Yes {
				return true, "mounts the container runtime socket (trivial host-root escape)"
			}
			return false, ""
		},
	},
}

// CheckPolicy evaluates each pod spec against every guardrail rule and returns the
// violations, in canonical scorer order per workload so the output is
// deterministic regardless of input order. It is the enforce-in-place dual of
// EmitPolicy: same dim->rule map (policyRules / checkRules share keys), but the
// rule is applied to the scanned manifest itself rather than baked into YAML.
//
// It is pure: no I/O, deterministic for a given spec set.
func CheckPolicy(specs []Spec) CheckResult {
	res := CheckResult{Workloads: len(specs)}
	for _, s := range specs {
		target := s.Target
		if target == "" {
			target = "workload"
		}
		for _, sc := range scorers {
			rule, ok := checkRules[sc.key]
			if !ok {
				continue
			}
			broken, evidence := rule.violated(s)
			if !broken {
				continue
			}
			res.Violations = append(res.Violations, PolicyViolation{
				Target:      target,
				Key:         sc.key,
				Title:       rule.title,
				Requirement: rule.requirement,
				Evidence:    evidence,
				Verdict:     VerdictFail,
			})
		}
	}
	return res
}

// RenderCheckText writes a human-readable gate report to w: a PASS banner when the
// manifest satisfies every rule, otherwise one line per broken rule with the
// workload, the requirement, and the evidence. This is the default CLI/CI output.
func RenderCheckText(w io.Writer, res CheckResult) {
	if res.OK() {
		fmt.Fprintf(w, "PASS  policy check: %d workload(s), 0 violations\n", res.Workloads)
		fmt.Fprintln(w, "  every guardrail rule --emit-policy would generate is satisfied in place.")
		return
	}
	fmt.Fprintf(w, "FAIL  policy check: %d violation(s) across %d workload(s)\n\n", len(res.Violations), res.Workloads)
	for _, v := range res.Violations {
		fmt.Fprintf(w, "  %s [%s]\n", v.Title, v.Target)
		fmt.Fprintf(w, "          require: %s\n", v.Requirement)
		if v.Evidence != "" {
			fmt.Fprintf(w, "          found:   %s\n", v.Evidence)
		}
	}
	fmt.Fprintln(w, "\nRun `ironctl scan --k8s <file> --emit-policy=kyverno` to generate the guardrail these rules enforce.")
}

// RenderCheckMarkdown renders the gate result as a Markdown block for a PR comment
// or CI job summary. A clean manifest yields a one-line pass; violations become a
// table so a reviewer sees exactly which rule each workload broke.
func RenderCheckMarkdown(res CheckResult) string {
	var b strings.Builder
	b.WriteString("### IronClaw policy check\n\n")
	if res.OK() {
		fmt.Fprintf(&b, "**PASS** — %d workload(s), 0 violations. Every guardrail rule --emit-policy would generate is satisfied.\n", res.Workloads)
		return b.String()
	}
	fmt.Fprintf(&b, "**FAIL** — %d violation(s) across %d workload(s).\n\n", len(res.Violations), res.Workloads)
	b.WriteString("| Workload | Control | Requirement | Found |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, v := range res.Violations {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			mdCell(v.Target), mdCell(v.Title), mdCell(v.Requirement), mdCell(v.Evidence))
	}
	return b.String()
}

// RenderCheckJSON writes the machine-readable gate result to w (for --json).
func RenderCheckJSON(w io.Writer, res CheckResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

// mdCell escapes a value for a single Markdown table cell (pipes and newlines).
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		return "-"
	}
	return s
}
