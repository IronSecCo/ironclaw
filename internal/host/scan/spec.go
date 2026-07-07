// Package scan implements `ironctl scan`: a containment self-audit that grades
// the isolation posture of ANY container, docker-compose service, or Kubernetes
// pod/manifest on a 0-100 scale across the same dimensions IronClaw's own
// containment benchmark checks (IRO-369): non-root user, dropped capabilities,
// seccomp, network isolation, read-only rootfs, docker.sock exposure, and shared
// host namespaces.
//
// The package is deliberately split into a PURE core (this file, score.go,
// render.go) that operates on a normalized Spec, and thin SOURCE ADAPTERS
// (docker.go, compose.go, k8s.go) that extract a Spec from a `docker inspect`
// JSON blob, a compose service, or a pod manifest. That keeps the scorers
// hermetically unit-testable with no Docker or Kubernetes dependency, and lets a
// single grading model serve every runtime.
//
// FAIL-CLOSED is the governing principle: a dimension whose posture cannot be
// determined is scored as if it were INSECURE (Unknown -> the worst verdict),
// never silently passed. A scan that cannot see a boundary must never claim the
// boundary holds.
package scan

// Tristate is a three-valued boolean used for every security posture that a
// source may or may not report. Unlike a plain bool (or *bool), it makes the
// "we could not determine this" case a first-class, non-optional value so the
// scorers can treat unknowns fail-closed instead of defaulting to a safe-looking
// zero value.
type Tristate int

const (
	// Unknown means the source did not report enough to decide. Scored as the
	// worst outcome (fail-closed).
	Unknown Tristate = iota
	// Yes means the posture is present/true.
	Yes
	// No means the posture is absent/false.
	No
)

func (t Tristate) String() string {
	switch t {
	case Yes:
		return "yes"
	case No:
		return "no"
	default:
		return "unknown"
	}
}

// boolTri maps a definitely-known bool to Yes/No. Use it only when the source
// unambiguously reported the field; leave Unknown otherwise.
func boolTri(b bool) Tristate {
	if b {
		return Yes
	}
	return No
}

// Spec is the normalized, source-agnostic containment posture of a single
// workload. Every source adapter produces one of these; every scorer reads one.
// Fields left at their zero value (Unknown / "" / nil) are treated as unknown
// and graded fail-closed.
type Spec struct {
	// Source identity (informational; shown in the report header).
	Source string // "docker" | "compose" | "k8s"
	Target string // container name/id, compose service, or pod name

	// --- user namespace / uid ------------------------------------------------
	// RunAsNonRoot is Yes when the workload is known to run as a uid != 0.
	RunAsNonRoot Tristate
	// User is the raw user spec observed ("65532", "nobody", "0:0", ""), shown
	// as evidence in the detail column.
	User string

	// --- capabilities --------------------------------------------------------
	// CapDropAll is Yes when ALL capabilities are dropped (cap_drop: [ALL] or an
	// empty effective set). Additions in CapAdd weaken this.
	CapDropAll Tristate
	CapAdd     []string // capabilities added back (each one weakens the posture)

	// --- seccomp -------------------------------------------------------------
	// Seccomp is the profile posture: "confined" (default/runtime profile or a
	// custom path), "unconfined" (explicitly disabled), or "" (unknown).
	Seccomp string

	// --- network -------------------------------------------------------------
	// NetworkMode is the raw mode ("none", "host", "bridge", "default",
	// "container:...", or a compose/k8s equivalent). "none" is the only fully
	// egress-isolated mode; "host" is the worst.
	NetworkMode string

	// --- filesystem ----------------------------------------------------------
	ReadonlyRoot Tristate // read-only root filesystem
	// DockerSock is Yes when the Docker/OCI control socket is mounted into the
	// workload — a full host-root escape primitive. Note the polarity: Yes is BAD.
	DockerSock Tristate
	// HostPathMounts lists sensitive host paths bind-mounted in (informational
	// evidence for the docker.sock / host-mount findings).
	HostPathMounts []string

	// --- namespaces / privilege ---------------------------------------------
	Privileged  Tristate // --privileged (disables seccomp, grants all caps)
	HostPID     Tristate // shares the host PID namespace
	HostNetwork Tristate // shares the host network namespace
	HostIPC     Tristate // shares the host IPC namespace

	// --- informational -------------------------------------------------------
	Runtime    string   // OCI runtime ("runc", "runsc", "kata-runtime", …)
	NoNewPrivs Tristate // no-new-privileges set
	Notes      []string // adapter-level notes (e.g. "field absent, assuming insecure")
}
