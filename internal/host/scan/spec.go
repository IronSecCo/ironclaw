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

import (
	"encoding/json"
	"fmt"
)

// ScanMode names WHAT the report actually inspected. It exists because the
// docker-family CLIs resolve containers and images out of a SINGLE namespace, so
// `docker inspect <ref>` silently answers for either one (IRO-711): an image ref
// landed in the container adapter and was graded as if a container existed. The
// mode is therefore decided by which explicit inspect subcommand succeeded, never
// inferred from which fields came back.
//
// It is a POSITIVE assertion, always emitted. A consumer must never have to
// detect image mode by an absent key: an absent key and a zero value are
// indistinguishable at the call site, which is the exact failure this field
// exists to close.
type ScanMode int

const (
	// ModeContainer is the zero value: the report grades the run-time
	// configuration of a container, either a live one (docker/podman/nerdctl
	// `container inspect`) or one declared by a manifest (compose, k8s, ECS,
	// Terraform, …). Every dimension is observable, so the report carries a
	// composite score and grade.
	ModeContainer ScanMode = iota
	// ModeImage means only an IMAGE was inspected. An image manifest carries no
	// run-time configuration, so six of the seven graded dimensions are not
	// observable at all and the report deliberately carries NO composite score
	// and NO grade. Scoring them either way would be a claim about a container
	// that does not exist.
	ModeImage
	// ModeDockerfile means a Dockerfile was graded statically against the
	// build-time dimension set (dockerfile_score.go). It has its own composite
	// over its own dimensions.
	ModeDockerfile
)

func (m ScanMode) String() string {
	switch m {
	case ModeImage:
		return "image"
	case ModeDockerfile:
		return "dockerfile"
	default:
		return "container"
	}
}

// MarshalJSON emits the mode as its stable string name.
func (m ScanMode) MarshalJSON() ([]byte, error) { return json.Marshal(m.String()) }

// UnmarshalJSON accepts the stable string names. An unrecognized mode is an
// error rather than a silent fallback to "container", which would re-create the
// fail-open the mode field exists to prevent.
func (m *ScanMode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch s {
	case "container":
		*m = ModeContainer
	case "image":
		*m = ModeImage
	case "dockerfile":
		*m = ModeDockerfile
	default:
		return fmt.Errorf("unknown scan mode %q", s)
	}
	return nil
}

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
	// Mode records what was inspected. It is set by the ADAPTER, which is the
	// only layer that knows: the container adapters leave it at ModeContainer,
	// SpecFromImageInspect sets ModeImage. The scorers read it and refuse to
	// grade run-time dimensions that the inspected artifact cannot carry.
	Mode ScanMode
	// Source identity (informational; shown in the report header).
	Source string // "docker" | "compose" | "k8s"
	Target string // container name/id, compose service, or pod name
	// Image is the container image reference, when the source reports it. Used to
	// render a copy-pasteable hardened `docker run` in remediation output; may be
	// "" (compose/k8s adapters do not populate it).
	Image string

	// --- user namespace / uid ------------------------------------------------
	// RunAsNonRoot is Yes when the workload is known to run as a uid != 0.
	RunAsNonRoot Tristate
	// User is the raw user spec observed ("65532", "nobody", "0:0", ""), shown
	// as evidence in the detail column.
	User string
	// Rootless is Yes when the container runs in a user namespace that remaps
	// container-uid 0 to an UNPRIVILEGED host uid (rootless Podman, or an explicit
	// userns). This is a genuine posture win independent of the runtime name: a
	// container-root escape lands as an unprivileged host user, not host root. It
	// credits the non-root dimension even when the in-container user is 0.
	Rootless Tristate
	// UserNSHostUID is the host uid that container-uid 0 maps to, when known
	// (evidence for the rootless credit; e.g. "100000").
	UserNSHostUID string

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
