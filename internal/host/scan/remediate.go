package scan

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// hardenedUID is the non-root uid:gid the remediations pin to. 65532 is the
// distroless "nonroot" identity IronClaw's own sandboxes run as.
const hardenedUID = "65532:65532"

// Remediation is the concrete, copy-pasteable fix for ONE non-PASS dimension,
// rendered for the source runtime that was scanned (docker flags, a compose
// key, or a securityContext field). It is prescriptive: the exact config to set,
// not a description of the problem.
type Remediation struct {
	Key         string  `json:"key"`         // dimension key, e.g. "caps.dropped"
	Title       string  `json:"title"`       // human label
	Verdict     Verdict `json:"verdict"`     // the verdict being remediated (FAIL/WARN/UNKNOWN)
	Fix         string  `json:"fix"`         // exact config to apply for the scanned source
	Explanation string  `json:"explanation"` // one line: why it matters
}

// RemediationPlan is the full prescriptive output of `--fix`: a targeted fix per
// failed/weak dimension, plus one assembled copy-pasteable hardened artifact for
// the source runtime (a `docker run`, a compose service patch, or a k8s
// securityContext block). Deterministic for a given Spec+Report; no I/O, no clock.
type RemediationPlan struct {
	Source  string        `json:"source"`
	Target  string        `json:"target"`
	Grade   string        `json:"grade"`
	Score   int           `json:"score"`
	Items   []Remediation `json:"items"`   // one per non-PASS dimension
	Snippet string        `json:"snippet"` // full copy-pasteable hardened artifact
}

// Remediate turns a graded Report into a prescriptive RemediationPlan: for every
// dimension that did not PASS it emits the concrete fix for the scanned source,
// then assembles a single copy-pasteable hardened artifact that scores A when
// applied. Pure and deterministic — the same fail-closed core the scorers use.
func Remediate(s Spec, r Report) RemediationPlan {
	plan := RemediationPlan{
		Source: r.Source,
		Target: r.Target,
		Grade:  r.Grade,
		Score:  r.Score,
	}
	for _, d := range r.Dimensions {
		if d.Verdict == VerdictPass {
			continue
		}
		fix, expl := dimFix(s, d.Key)
		plan.Items = append(plan.Items, Remediation{
			Key: d.Key, Title: d.Title, Verdict: d.Verdict, Fix: fix, Explanation: expl,
		})
	}
	plan.Snippet = snippet(s)
	return plan
}

// dimFix returns the concrete fix + one-line rationale for a single dimension,
// specialized to the scanned source. It reads the Spec so a fix can name the
// exact offending config (a shared host namespace, an added-back capability).
func dimFix(s Spec, key string) (fix, explanation string) {
	switch key {
	case "user.nonroot":
		explanation = "Pin a non-root uid so a container escape does not begin as host uid 0."
		switch s.Source {
		case "compose":
			return `user: "` + hardenedUID + `"`, explanation
		case "k8s":
			return "securityContext: { runAsNonRoot: true, runAsUser: 65532 }", explanation
		default:
			return "--user " + hardenedUID, explanation
		}

	case "caps.dropped":
		explanation = "Drop every Linux capability; add back only what the workload provably needs."
		priv := s.Privileged == Yes
		added := len(s.CapAdd) > 0
		switch s.Source {
		case "compose":
			fix = "cap_drop: [ALL]"
			if added {
				fix += "  (and remove cap_add: " + strings.Join(s.CapAdd, ", ") + ")"
			}
			if priv {
				fix += "  (and remove privileged: true)"
			}
			return fix, explanation
		case "k8s":
			fix = "securityContext: { capabilities: { drop: [ALL] }, allowPrivilegeEscalation: false }"
			if priv {
				fix += "  (and remove privileged: true)"
			}
			return fix, explanation
		default:
			fix = "--cap-drop=ALL"
			if added {
				fix += " (and drop --cap-add=" + strings.Join(s.CapAdd, ",") + ")"
			}
			if priv {
				fix = "remove --privileged, then " + fix
			}
			return fix, explanation
		}

	case "seccomp":
		explanation = "Keep a seccomp profile active to shrink the reachable syscall surface."
		switch s.Source {
		case "compose":
			return "security_opt: [no-new-privileges:true]  (and remove any seccomp=unconfined)", explanation
		case "k8s":
			return "securityContext: { seccompProfile: { type: RuntimeDefault } }", explanation
		default:
			return "--security-opt=no-new-privileges  (and drop --security-opt seccomp=unconfined; Docker then applies its default profile)", explanation
		}

	case "network.isolated":
		explanation = "Cut egress so a compromised workload cannot reach the network or exfiltrate."
		host := s.HostNetwork == Yes || strings.EqualFold(strings.TrimSpace(s.NetworkMode), "host")
		switch s.Source {
		case "compose":
			fix = "network_mode: none"
			if host {
				fix = "network_mode: none  (replaces network_mode: host)"
			}
			return fix, explanation
		case "k8s":
			fix = "apply a default-deny egress NetworkPolicy for this pod"
			if host {
				fix += "  (and set hostNetwork: false)"
			}
			return fix, explanation
		default:
			fix = "--network=none"
			if host {
				fix = "--network=none  (replaces --network=host)"
			}
			return fix, explanation
		}

	case "rootfs.readonly":
		explanation = "Make the root filesystem read-only to remove the tamper/persistence surface."
		switch s.Source {
		case "compose":
			return "read_only: true", explanation
		case "k8s":
			return "securityContext: { readOnlyRootFilesystem: true }", explanation
		default:
			return "--read-only  (add --tmpfs /tmp for writable scratch)", explanation
		}

	case "docker.sock":
		explanation = "Mounting the container-runtime socket is a one-command host-root escape."
		switch s.Source {
		case "compose":
			return "remove the docker.sock entry from the service's volumes:", explanation
		case "k8s":
			return "remove the hostPath volume and volumeMount for the runtime socket", explanation
		default:
			return "remove the -v /var/run/docker.sock:... bind mount", explanation
		}

	case "namespaces.host":
		explanation = "Do not share host namespaces; each shared namespace erases an isolation boundary."
		shared := sharedNamespaces(s)
		switch s.Source {
		case "compose":
			if len(shared) == 0 {
				return "remove any pid: host / ipc: host (and privileged: true)", explanation
			}
			return "remove " + composeNSKeys(shared), explanation
		case "k8s":
			return "set hostPID: false / hostIPC: false / hostNetwork: false on the pod spec", explanation
		default:
			if len(shared) == 0 {
				return "remove --pid=host / --ipc=host / --network=host / --privileged", explanation
			}
			return "remove " + dockerNSFlags(shared), explanation
		}
	}
	// Unknown dimension key: never silently pass. Point the operator at the docs.
	return "harden this dimension; see https://ironsecco.github.io/ironclaw/scan/", "This dimension did not pass; apply the hardened posture."
}

// sharedNamespaces returns the host namespaces the spec shares, worst-first
// sorted for deterministic output. Privileged implies all boundaries are down.
func sharedNamespaces(s Spec) []string {
	var out []string
	if s.HostPID == Yes {
		out = append(out, "PID")
	}
	if s.HostIPC == Yes {
		out = append(out, "IPC")
	}
	if s.HostNetwork == Yes {
		out = append(out, "network")
	}
	sort.Strings(out)
	return out
}

func dockerNSFlags(shared []string) string {
	m := map[string]string{"PID": "--pid=host", "IPC": "--ipc=host", "network": "--network=host"}
	var flags []string
	for _, ns := range shared {
		flags = append(flags, m[ns])
	}
	return strings.Join(flags, " / ")
}

func composeNSKeys(shared []string) string {
	m := map[string]string{"PID": "pid: host", "IPC": "ipc: host", "network": "network_mode: host"}
	var keys []string
	for _, ns := range shared {
		keys = append(keys, m[ns])
	}
	return strings.Join(keys, " / ")
}

// snippet assembles the full copy-pasteable hardened artifact for the source.
// It always emits the COMPLETE hardened posture (not only the failed
// dimensions), so applying it yields an A-grade workload regardless of which
// dimensions were already passing.
func snippet(s Spec) string {
	switch s.Source {
	case "compose":
		return composeSnippet(s)
	case "k8s":
		return k8sSnippet(s)
	default:
		return dockerSnippet(s)
	}
}

func dockerSnippet(s Spec) string {
	image := strings.TrimSpace(s.Image)
	if image == "" {
		image = "<IMAGE>"
	}
	var b strings.Builder
	b.WriteString("docker run -d --name ic-hardened \\\n")
	b.WriteString("  --user " + hardenedUID + " \\\n")
	b.WriteString("  --cap-drop=ALL \\\n")
	b.WriteString("  --security-opt=no-new-privileges \\\n")
	b.WriteString("  --read-only --tmpfs /tmp \\\n")
	b.WriteString("  --network=none \\\n")
	b.WriteString("  " + image + "\n")
	// Call out what was intentionally NOT carried over from the original run.
	var dropped []string
	if s.DockerSock == Yes {
		dropped = append(dropped, "the docker.sock bind mount (host-root escape)")
	}
	if s.Privileged == Yes {
		dropped = append(dropped, "--privileged")
	}
	if ns := sharedNamespaces(s); len(ns) > 0 {
		dropped = append(dropped, dockerNSFlags(ns))
	}
	for _, p := range s.HostPathMounts {
		if !isControlSocket(p) {
			dropped = append(dropped, "the host bind mount "+p)
		}
	}
	if len(dropped) > 0 {
		b.WriteString("# intentionally dropped from the original run: " + strings.Join(dropped, ", ") + "\n")
	}
	return b.String()
}

func composeSnippet(s Spec) string {
	target := nz(s.Target, "<service>")
	var b strings.Builder
	fmt.Fprintf(&b, "# Minimal hardened patch for service %q (merge into your compose file):\n", target)
	b.WriteString("services:\n")
	fmt.Fprintf(&b, "  %s:\n", target)
	b.WriteString("    user: \"" + hardenedUID + "\"\n")
	b.WriteString("    read_only: true\n")
	b.WriteString("    network_mode: none\n")
	b.WriteString("    cap_drop:\n      - ALL\n")
	b.WriteString("    security_opt:\n      - no-new-privileges:true\n")
	b.WriteString("    # remove: privileged, pid: host, ipc: host, and any docker.sock volume\n")
	return b.String()
}

func k8sSnippet(s Spec) string {
	target := nz(s.Target, "<container>")
	var b strings.Builder
	b.WriteString("# Set on the pod spec and the graded container:\n")
	b.WriteString("spec:\n")
	b.WriteString("  hostPID: false\n")
	b.WriteString("  hostIPC: false\n")
	b.WriteString("  hostNetwork: false\n")
	b.WriteString("  securityContext:\n")
	b.WriteString("    runAsNonRoot: true\n")
	b.WriteString("    runAsUser: 65532\n")
	b.WriteString("  containers:\n")
	fmt.Fprintf(&b, "    - name: %s\n", target)
	b.WriteString("      securityContext:\n")
	b.WriteString("        allowPrivilegeEscalation: false\n")
	b.WriteString("        readOnlyRootFilesystem: true\n")
	b.WriteString("        runAsNonRoot: true\n")
	b.WriteString("        runAsUser: 65532\n")
	b.WriteString("        capabilities:\n          drop: [ALL]\n")
	b.WriteString("        seccompProfile:\n          type: RuntimeDefault\n")
	b.WriteString("# Isolate egress with a default-deny NetworkPolicy (not expressible in the pod spec).\n")
	b.WriteString("# Remove any hostPath volume/mount for the container-runtime socket (docker.sock).\n")
	return b.String()
}

// RenderPlan writes the human-readable remediation to w: one prescriptive fix
// per non-PASS dimension, then the assembled copy-pasteable hardened artifact.
func RenderPlan(w io.Writer, plan RemediationPlan) {
	if len(plan.Items) == 0 {
		fmt.Fprintf(w, "\nRemediation: none. %s already scores %d/100 (grade %s).\n",
			nz(plan.Target, "target"), plan.Score, plan.Grade)
		return
	}
	fmt.Fprintf(w, "\nRemediation (%d dimension(s) to harden, %s currently %d/100 grade %s):\n\n",
		len(plan.Items), nz(plan.Target, "target"), plan.Score, plan.Grade)
	for _, it := range plan.Items {
		fmt.Fprintf(w, "  [%s] %s (%s)\n", it.Key, it.Title, it.Verdict)
		fmt.Fprintf(w, "      fix: %s\n", it.Fix)
		fmt.Fprintf(w, "      why: %s\n", it.Explanation)
	}
	label := "hardened docker run"
	switch plan.Source {
	case "compose":
		label = "hardened compose patch"
	case "k8s":
		label = "hardened securityContext"
	}
	fmt.Fprintf(w, "\nCopy-pasteable %s (scores A/100 when applied):\n\n%s\n", label, plan.Snippet)
}
