package scan

import (
	"fmt"
	"sort"
	"strings"
)

// PolicyEngine names a supported admission-policy backend for --emit-policy.
type PolicyEngine string

const (
	// EngineKyverno emits a single Kyverno ClusterPolicy (kyverno.io/v1) whose
	// rules validate (failureAction: Enforce) the hardening controls the scanned
	// workload failed.
	EngineKyverno PolicyEngine = "kyverno"
	// EngineGatekeeper emits, per failed control, an OPA Gatekeeper
	// ConstraintTemplate (Rego) plus a matching Constraint.
	EngineGatekeeper PolicyEngine = "gatekeeper"
	// EngineVAP emits a native ValidatingAdmissionPolicy (K8s 1.30+ GA) plus a
	// ValidatingAdmissionPolicyBinding. The rules are CEL expressions evaluated by
	// the API server itself, so enforcement needs NO controller installed — the
	// zero-dependency guardrail.
	EngineVAP PolicyEngine = "vap"
)

// ParsePolicyEngine validates an --emit-policy value.
func ParsePolicyEngine(s string) (PolicyEngine, error) {
	switch PolicyEngine(strings.ToLower(strings.TrimSpace(s))) {
	case EngineKyverno:
		return EngineKyverno, nil
	case EngineGatekeeper:
		return EngineGatekeeper, nil
	case EngineVAP:
		return EngineVAP, nil
	default:
		return "", fmt.Errorf("unknown policy engine %q; use kyverno, gatekeeper or vap", s)
	}
}

// policyRule is the per-dimension mapping from a scorer dimension to the guardrail
// it emits for each engine. kyverno is one rule block (a list item under
// spec.rules); gatekeeper is a full ConstraintTemplate + Constraint document pair.
// Every field is a static string constant so the emitted YAML is deterministic and
// golden-file testable — nothing is derived from the scanned manifest except WHICH
// rules are included (the failing set).
type policyRule struct {
	kyverno    string
	gatekeeper string
	// vap is a single validations[] list item (a CEL expression + message) under
	// spec.validations of a ValidatingAdmissionPolicy. The expression returns true
	// when the control is SATISFIED; a false result denies admission.
	vap string
}

// policyRules maps a scorer dimension key (score.go `scorers`) to its guardrail
// bodies. A dimension without an entry (none today) simply emits no rule. The
// emitted policy enforces exactly the delta between the scanned grade and 100/A:
// one FAILED dimension -> one rule.
var policyRules = map[string]policyRule{
	"user.nonroot":     {kyverno: kyvernoRunAsNonRoot, gatekeeper: gkRunAsNonRoot, vap: vapRunAsNonRoot},
	"caps.dropped":     {kyverno: kyvernoDropCaps, gatekeeper: gkDropCaps, vap: vapDropCaps},
	"seccomp":          {kyverno: kyvernoSeccomp, gatekeeper: gkSeccomp, vap: vapSeccomp},
	"rootfs.readonly":  {kyverno: kyvernoReadonly, gatekeeper: gkReadonly, vap: vapReadonly},
	"network.isolated": {kyverno: kyvernoHostNetwork, gatekeeper: gkHostNetwork, vap: vapHostNetwork},
	"namespaces.host":  {kyverno: kyvernoHostNamespaces, gatekeeper: gkHostNamespaces, vap: vapHostNamespaces},
	"docker.sock":      {kyverno: kyvernoDockerSock, gatekeeper: gkDockerSock, vap: vapDockerSock},
}

// EmitPolicy renders an admission-policy document that BLOCKS every containment
// control the scanned workload(s) failed, for the given engine. It reuses the
// SAME scorer dimension set as `--k8s` / `--k8s-admission`: a dimension is emitted
// when its verdict is anything other than PASS in ANY of the supplied reports, so
// a multi-workload manifest yields the union of every workload's gaps. The result
// is exactly the delta between the current grade and a hardened 100/A.
//
// It is pure: no I/O, deterministic for a given report set. An already-hardened
// input (no failing dimensions) yields an informational comment document rather
// than an empty, unapplyable policy.
func EmitPolicy(reports []Report, engine PolicyEngine) (string, error) {
	failing := failingDims(reports)
	switch engine {
	case EngineKyverno:
		return emitKyverno(failing), nil
	case EngineGatekeeper:
		return emitGatekeeper(failing), nil
	case EngineVAP:
		return emitVAP(failing), nil
	default:
		return "", fmt.Errorf("unknown policy engine %q; use kyverno, gatekeeper or vap", engine)
	}
}

// failingDims returns the scorer dimension keys that are non-PASS in at least one
// report, in the canonical scorer order (score.go `scorers`) so the emitted rule
// order is stable regardless of input order.
func failingDims(reports []Report) []string {
	fail := map[string]bool{}
	for _, r := range reports {
		for _, d := range r.Dimensions {
			if d.Verdict != VerdictPass {
				fail[d.Key] = true
			}
		}
	}
	var keys []string
	for _, sc := range scorers {
		if fail[sc.key] {
			if _, ok := policyRules[sc.key]; ok {
				keys = append(keys, sc.key)
			}
		}
	}
	// Defensive: keep deterministic even if a failing key had no scorer entry.
	sort.SliceStable(keys, func(i, j int) bool { return scorerIndex(keys[i]) < scorerIndex(keys[j]) })
	return keys
}

func scorerIndex(key string) int {
	for i, sc := range scorers {
		if sc.key == key {
			return i
		}
	}
	return len(scorers)
}

func emitKyverno(failing []string) string {
	if len(failing) == 0 {
		return alreadyHardenedComment("kyverno")
	}
	var b strings.Builder
	b.WriteString(kyvernoHeader)
	for _, key := range failing {
		b.WriteString(policyRules[key].kyverno)
	}
	return b.String()
}

func emitGatekeeper(failing []string) string {
	if len(failing) == 0 {
		return alreadyHardenedComment("gatekeeper")
	}
	docs := make([]string, 0, len(failing))
	for _, key := range failing {
		docs = append(docs, strings.TrimRight(policyRules[key].gatekeeper, "\n"))
	}
	return gatekeeperHeader + strings.Join(docs, "\n---\n") + "\n"
}

func emitVAP(failing []string) string {
	if len(failing) == 0 {
		return alreadyHardenedComment("vap")
	}
	var b strings.Builder
	b.WriteString(vapHeader)
	for _, key := range failing {
		b.WriteString(policyRules[key].vap)
	}
	b.WriteString(vapBinding)
	return b.String()
}

func alreadyHardenedComment(engine string) string {
	return fmt.Sprintf(`# IronClaw scan --emit-policy=%s
# The scanned workload already earns 100/A on the containment scorer.
# There is no gap to enforce, so no guardrail rules were generated.
`, engine)
}

// --------------------------------------------------------------------------- //
// Kyverno ClusterPolicy bodies. The header opens the document; each rule constant
// is a list item under spec.rules (4-space indented) enforcing one control.
// --------------------------------------------------------------------------- //

const kyvernoHeader = `# Generated by: ironctl scan --emit-policy=kyverno
# Enforces the containment controls the scanned workload failed (the delta to a
# 100/A grade). Apply with: kubectl apply -f <this-file>
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: ironclaw-containment
  annotations:
    policies.kyverno.io/title: IronClaw Containment Guardrail
    policies.kyverno.io/category: Pod Security
    policies.kyverno.io/description: >-
      Blocks the exact containment controls a scanned workload failed. Generated
      by ironctl scan --emit-policy=kyverno.
spec:
  background: true
  rules:
`

const kyvernoRunAsNonRoot = `    - name: require-run-as-non-root
      match:
        any:
          - resources:
              kinds:
                - Pod
      validate:
        failureAction: Enforce
        message: >-
          Containers must run as non-root (set securityContext.runAsNonRoot: true).
        pattern:
          spec:
            containers:
              - securityContext:
                  runAsNonRoot: true
`

const kyvernoDropCaps = `    - name: require-drop-all-capabilities
      match:
        any:
          - resources:
              kinds:
                - Pod
      validate:
        failureAction: Enforce
        message: >-
          Containers must drop ALL Linux capabilities
          (securityContext.capabilities.drop: [ALL]) and must not run privileged.
        pattern:
          spec:
            containers:
              - securityContext:
                  =(privileged): "false"
                  capabilities:
                    drop:
                      - ALL
`

const kyvernoSeccomp = `    - name: require-seccomp
      match:
        any:
          - resources:
              kinds:
                - Pod
      validate:
        failureAction: Enforce
        message: >-
          A seccompProfile of RuntimeDefault or Localhost is required.
        pattern:
          spec:
            securityContext:
              seccompProfile:
                type: RuntimeDefault | Localhost
`

const kyvernoReadonly = `    - name: require-readonly-rootfs
      match:
        any:
          - resources:
              kinds:
                - Pod
      validate:
        failureAction: Enforce
        message: >-
          Containers must use a read-only root filesystem
          (securityContext.readOnlyRootFilesystem: true).
        pattern:
          spec:
            containers:
              - securityContext:
                  readOnlyRootFilesystem: true
`

const kyvernoHostNetwork = `    - name: disallow-host-network
      match:
        any:
          - resources:
              kinds:
                - Pod
      validate:
        failureAction: Enforce
        message: >-
          hostNetwork is forbidden. NOTE: full egress lockdown (network=none)
          requires a NetworkPolicy, which admission control cannot express.
        pattern:
          spec:
            =(hostNetwork): "false"
`

const kyvernoHostNamespaces = `    - name: disallow-host-namespaces
      match:
        any:
          - resources:
              kinds:
                - Pod
      validate:
        failureAction: Enforce
        message: >-
          Sharing the host PID or IPC namespace is forbidden.
        pattern:
          spec:
            =(hostPID): "false"
            =(hostIPC): "false"
`

const kyvernoDockerSock = `    - name: disallow-runtime-socket-mounts
      match:
        any:
          - resources:
              kinds:
                - Pod
      validate:
        failureAction: Enforce
        message: >-
          Mounting the container runtime socket (docker.sock / containerd.sock /
          crio.sock) is forbidden: it is a trivial host-root escape.
        foreach:
          - list: request.object.spec.volumes[]
            deny:
              conditions:
                any:
                  - key: "{{ element.hostPath.path || '' }}"
                    operator: AnyIn
                    value:
                      - /var/run/docker.sock
                      - /run/docker.sock
                      - /var/run/containerd/containerd.sock
                      - /run/containerd/containerd.sock
                      - /var/run/crio/crio.sock
`

// --------------------------------------------------------------------------- //
// Gatekeeper ConstraintTemplate (Rego) + Constraint pairs. Each constant is a
// complete two-document YAML block (template, then constraint) matching Pods.
// --------------------------------------------------------------------------- //

const gatekeeperHeader = `# Generated by: ironctl scan --emit-policy=gatekeeper
# One ConstraintTemplate + Constraint per containment control the scanned workload
# failed (the delta to a 100/A grade). Apply the templates, then the constraints:
#   kubectl apply -f <this-file>
`

const gkRunAsNonRoot = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequirerunasnonroot
spec:
  crd:
    spec:
      names:
        kind: K8sRequireRunAsNonRoot
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequirerunasnonroot
        violation[{"msg": msg}] {
          c := input.review.object.spec.containers[_]
          not container_nonroot(c)
          not input.review.object.spec.securityContext.runAsNonRoot == true
          msg := sprintf("container %v must set securityContext.runAsNonRoot: true", [c.name])
        }
        container_nonroot(c) {
          c.securityContext.runAsNonRoot == true
        }
---
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequireRunAsNonRoot
metadata:
  name: ironclaw-require-run-as-non-root
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`

const gkDropCaps = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredropallcaps
spec:
  crd:
    spec:
      names:
        kind: K8sRequireDropAllCaps
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredropallcaps
        violation[{"msg": msg}] {
          c := input.review.object.spec.containers[_]
          not drops_all(c)
          msg := sprintf("container %v must drop ALL capabilities", [c.name])
        }
        drops_all(c) {
          c.securityContext.capabilities.drop[_] == "ALL"
        }
---
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequireDropAllCaps
metadata:
  name: ironclaw-require-drop-all-caps
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`

const gkSeccomp = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequireseccomp
spec:
  crd:
    spec:
      names:
        kind: K8sRequireSeccomp
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequireseccomp
        allowed := {"RuntimeDefault", "Localhost"}
        violation[{"msg": msg}] {
          not pod_seccomp
          msg := "spec.securityContext.seccompProfile.type must be RuntimeDefault or Localhost"
        }
        pod_seccomp {
          allowed[input.review.object.spec.securityContext.seccompProfile.type]
        }
---
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequireSeccomp
metadata:
  name: ironclaw-require-seccomp
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`

const gkReadonly = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequirereadonlyrootfs
spec:
  crd:
    spec:
      names:
        kind: K8sRequireReadOnlyRootFs
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequirereadonlyrootfs
        violation[{"msg": msg}] {
          c := input.review.object.spec.containers[_]
          ro := object.get(c, ["securityContext", "readOnlyRootFilesystem"], false)
          ro != true
          msg := sprintf("container %v must set readOnlyRootFilesystem: true", [c.name])
        }
---
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequireReadOnlyRootFs
metadata:
  name: ironclaw-require-readonly-rootfs
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`

const gkHostNetwork = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8sdisallowhostnetwork
spec:
  crd:
    spec:
      names:
        kind: K8sDisallowHostNetwork
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowhostnetwork
        violation[{"msg": msg}] {
          input.review.object.spec.hostNetwork == true
          msg := "hostNetwork is not allowed (egress lockdown also needs a NetworkPolicy)"
        }
---
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sDisallowHostNetwork
metadata:
  name: ironclaw-disallow-host-network
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`

const gkHostNamespaces = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8sdisallowhostnamespaces
spec:
  crd:
    spec:
      names:
        kind: K8sDisallowHostNamespaces
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowhostnamespaces
        violation[{"msg": msg}] {
          input.review.object.spec.hostPID == true
          msg := "hostPID is not allowed"
        }
        violation[{"msg": msg}] {
          input.review.object.spec.hostIPC == true
          msg := "hostIPC is not allowed"
        }
---
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sDisallowHostNamespaces
metadata:
  name: ironclaw-disallow-host-namespaces
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`

const gkDockerSock = `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8sdisallowruntimesocket
spec:
  crd:
    spec:
      names:
        kind: K8sDisallowRuntimeSocket
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowruntimesocket
        sockets := {
          "/var/run/docker.sock",
          "/run/docker.sock",
          "/var/run/containerd/containerd.sock",
          "/run/containerd/containerd.sock",
          "/var/run/crio/crio.sock",
        }
        violation[{"msg": msg}] {
          v := input.review.object.spec.volumes[_]
          sockets[v.hostPath.path]
          msg := sprintf("volume %v mounts a container runtime socket (%v)", [v.name, v.hostPath.path])
        }
---
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sDisallowRuntimeSocket
metadata:
  name: ironclaw-disallow-runtime-socket
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`

// --------------------------------------------------------------------------- //
// ValidatingAdmissionPolicy (K8s 1.30+ GA) bodies. vapHeader opens a single
// ValidatingAdmissionPolicy whose spec.validations carries one CEL expression per
// failed control; vapBinding closes the stream with the matching
// ValidatingAdmissionPolicyBinding. Each expression returns true when the control
// is SATISFIED, so a false result denies admission. Enforcement is done by the API
// server itself — NO controller to install.
//
// Expressions are double-quoted YAML scalars; CEL string literals use single
// quotes, so no escaping is needed. They read `object.spec.*` on a Pod.
// --------------------------------------------------------------------------- //

const vapHeader = `# Generated by: ironctl scan --emit-policy=vap
# Native ValidatingAdmissionPolicy (Kubernetes 1.30+ GA): enforced by the API
# server itself, with NO admission controller to install. One CEL validation per
# containment control the scanned workload failed (the delta to a 100/A grade).
# Apply with: kubectl apply -f <this-file>
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: ironclaw-containment
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
      - apiGroups:   [""]
        apiVersions: ["v1"]
        operations:  ["CREATE", "UPDATE"]
        resources:   ["pods"]
  validations:
`

const vapBinding = `---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: ironclaw-containment-binding
spec:
  policyName: ironclaw-containment
  validationActions:
    - Deny
`

const vapRunAsNonRoot = `    - expression: "object.spec.containers.all(c, (has(c.securityContext) && has(c.securityContext.runAsNonRoot) && c.securityContext.runAsNonRoot == true) || (has(object.spec.securityContext) && has(object.spec.securityContext.runAsNonRoot) && object.spec.securityContext.runAsNonRoot == true))"
      message: "Every container must run as non-root: set securityContext.runAsNonRoot: true at the pod or container level."
      reason: Forbidden
`

const vapDropCaps = `    - expression: "object.spec.containers.all(c, has(c.securityContext) && has(c.securityContext.capabilities) && has(c.securityContext.capabilities.drop) && c.securityContext.capabilities.drop.exists(d, d == 'ALL') && (!has(c.securityContext.privileged) || c.securityContext.privileged != true))"
      message: "Every container must drop ALL Linux capabilities (securityContext.capabilities.drop: [ALL]) and must not run privileged."
      reason: Forbidden
`

const vapSeccomp = `    - expression: "has(object.spec.securityContext) && has(object.spec.securityContext.seccompProfile) && (object.spec.securityContext.seccompProfile.type == 'RuntimeDefault' || object.spec.securityContext.seccompProfile.type == 'Localhost')"
      message: "A seccompProfile of RuntimeDefault or Localhost is required (spec.securityContext.seccompProfile.type)."
      reason: Forbidden
`

const vapReadonly = `    - expression: "object.spec.containers.all(c, has(c.securityContext) && has(c.securityContext.readOnlyRootFilesystem) && c.securityContext.readOnlyRootFilesystem == true)"
      message: "Every container must use a read-only root filesystem (securityContext.readOnlyRootFilesystem: true)."
      reason: Forbidden
`

const vapHostNetwork = `    - expression: "!has(object.spec.hostNetwork) || object.spec.hostNetwork != true"
      message: "hostNetwork is forbidden. NOTE: full egress lockdown (network=none) requires a NetworkPolicy, which admission control cannot express."
      reason: Forbidden
`

const vapHostNamespaces = `    - expression: "(!has(object.spec.hostPID) || object.spec.hostPID != true) && (!has(object.spec.hostIPC) || object.spec.hostIPC != true)"
      message: "Sharing the host PID or IPC namespace is forbidden."
      reason: Forbidden
`

const vapDockerSock = `    - expression: "!has(object.spec.volumes) || object.spec.volumes.all(v, !has(v.hostPath) || !(v.hostPath.path in ['/var/run/docker.sock', '/run/docker.sock', '/var/run/containerd/containerd.sock', '/run/containerd/containerd.sock', '/var/run/crio/crio.sock']))"
      message: "Mounting the container runtime socket (docker.sock / containerd.sock / crio.sock) is forbidden: it is a trivial host-root escape."
      reason: Forbidden
`
