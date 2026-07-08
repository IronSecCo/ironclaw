package scan

import (
	"fmt"
	"sort"
	"strings"
)

// Verdict is a per-dimension outcome, ordered worst-to-best for sorting.
type Verdict string

const (
	VerdictFail    Verdict = "FAIL"    // insecure posture observed
	VerdictUnknown Verdict = "UNKNOWN" // could not determine — scored as FAIL (fail-closed)
	VerdictWarn    Verdict = "WARN"    // partial / weakened posture
	VerdictPass    Verdict = "PASS"    // hardened posture observed
)

// Dimension is one graded containment axis.
type Dimension struct {
	Key     string  `json:"key"`     // stable id, e.g. "user.nonroot"
	Title   string  `json:"title"`   // human label
	Verdict Verdict `json:"verdict"` // PASS|WARN|FAIL|UNKNOWN
	Score   int     `json:"score"`   // points earned
	Max     int     `json:"max"`     // points possible for this dimension
	Detail  string  `json:"detail"`  // evidence / why
}

// Report is the full scorecard for one Spec.
type Report struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Runtime string `json:"runtime,omitempty"`
	// HardenedRuntime names a recognized strong-isolation runtime (gVisor, Kata,
	// Firecracker) when one is detected. Informational ONLY: scoring stays
	// runtime-agnostic (IRO-429), so this awards no points; it surfaces the fact
	// that a userspace-kernel / microVM boundary is in play.
	HardenedRuntime string      `json:"hardenedRuntime,omitempty"`
	Score           int         `json:"score"` // 0..100
	Max             int         `json:"max"`   // always 100
	Grade           string      `json:"grade"` // A..F
	Dimensions      []Dimension `json:"dimensions"`
	Notes           []string    `json:"notes,omitempty"`
	// GeneratedAt is set by the caller (injected for deterministic tests); the
	// pure scorer never reads the clock.
	GeneratedAt string `json:"generatedAt,omitempty"`
	Version     string `json:"version,omitempty"`
}

// scorer grades one dimension of a Spec. Each returns points-earned and a
// verdict+detail; Max is fixed per dimension below. Every scorer treats Unknown
// fail-closed (0 points, UNKNOWN verdict).
type scorer struct {
	key   string
	title string
	max   int
	grade func(Spec) (int, Verdict, string)
}

// scorers is the ordered dimension set. Weights sum to 100. The high weights sit
// on the boundaries whose breach is a full host compromise: dropped capabilities
// (20) and docker.sock exposure (15) each hand out host root when open.
var scorers = []scorer{
	{"user.nonroot", "Non-root user (uid != 0)", 15, gradeNonRoot},
	{"caps.dropped", "Dropped capabilities", 20, gradeCaps},
	{"seccomp", "Seccomp profile", 15, gradeSeccomp},
	{"network.isolated", "Network isolation / egress", 15, gradeNetwork},
	{"rootfs.readonly", "Read-only root filesystem", 10, gradeReadonly},
	{"docker.sock", "No docker.sock exposure", 15, gradeDockerSock},
	{"namespaces.host", "No shared host namespaces", 10, gradeHostNS},
}

// TotalWeight is the maximum achievable score (100 by construction).
const TotalWeight = 100

// Score grades a Spec across every dimension and returns the full Report. It is
// pure: no I/O, no clock, deterministic for a given Spec.
func Score(s Spec) Report {
	r := Report{
		Source:  s.Source,
		Target:  s.Target,
		Runtime: s.Runtime,
		Max:     TotalWeight,
		Notes:   append([]string(nil), s.Notes...),
	}
	sum := 0
	for _, sc := range scorers {
		pts, v, detail := sc.grade(s)
		if pts < 0 {
			pts = 0
		}
		if pts > sc.max {
			pts = sc.max
		}
		sum += pts
		r.Dimensions = append(r.Dimensions, Dimension{
			Key: sc.key, Title: sc.title, Verdict: v, Score: pts, Max: sc.max, Detail: detail,
		})
	}
	r.Score = sum
	r.Grade = grade(sum)

	// Strong-isolation runtime is informational only (IRO-429: scoring is
	// runtime-agnostic). We never award points for a runtime NAME, but we DO
	// surface when a recognized hardened runtime (gVisor/Kata/Firecracker) wraps
	// the workload, since it materially changes the escape story.
	if name, ok := StrongIsolationRuntime(s.Runtime); ok {
		r.HardenedRuntime = name
		r.Notes = append(r.Notes, fmt.Sprintf(
			"hardened runtime detected: %s (userspace-kernel / microVM isolation). Informational only; scoring is runtime-agnostic, so no points are awarded for the runtime name.",
			name))
	}
	return r
}

// StrongIsolationRuntime classifies an OCI runtime identifier (a docker
// HostConfig.Runtime, a podman OCIRuntime, a containerd runtime handler like
// "io.containerd.runsc.v1", or a Kubernetes runtimeClassName) as a recognized
// strong-isolation technology and returns a display name. It is a NAME match
// only and never affects the score — a container can name a hardened runtime and
// still be misconfigured, so the dimension scorers remain authoritative.
func StrongIsolationRuntime(runtime string) (string, bool) {
	r := strings.ToLower(strings.TrimSpace(runtime))
	if r == "" {
		return "", false
	}
	switch {
	case strings.Contains(r, "runsc") || strings.Contains(r, "gvisor"):
		return "gVisor (runsc)", true
	case strings.Contains(r, "kata"):
		return "Kata Containers", true
	case strings.Contains(r, "firecracker") || strings.Contains(r, "fc-runtime") || r == "runc-fc":
		return "Firecracker", true
	}
	return "", false
}

// grade maps a 0..100 score to a letter band.
func grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 50:
		return "C"
	case score >= 25:
		return "D"
	default:
		return "F"
	}
}

// --------------------------------------------------------------------------- //
// Dimension scorers. Each is total for its dimension: full points for a
// hardened posture, partial for a weakened one, zero for insecure OR unknown.
// --------------------------------------------------------------------------- //

func gradeNonRoot(s Spec) (int, Verdict, string) {
	switch s.RunAsNonRoot {
	case Yes:
		u := s.User
		if u == "" {
			u = "non-root"
		}
		return 15, VerdictPass, fmt.Sprintf("runs as %s (uid != 0)", u)
	case No:
		// Rootless / userns remap: even a container-uid-0 process maps to an
		// UNPRIVILEGED host uid, so an escape does not yield host root. That is the
		// single strongest mitigation for this dimension, so it earns near-full
		// credit even though the in-container user is 0.
		if s.Rootless == Yes {
			return 12, VerdictWarn, fmt.Sprintf(
				"runs as root INSIDE the container, but a rootless userns remaps container-uid 0 to unprivileged host uid %s; an escape lands unprivileged",
				nz(s.UserNSHostUID, "!= 0"))
		}
		return 0, VerdictFail, fmt.Sprintf("runs as root (user %q); a container escape starts with host-uid 0", nz(s.User, "0"))
	default:
		if s.Rootless == Yes {
			return 12, VerdictWarn, fmt.Sprintf(
				"in-container user not reported, but a rootless userns remaps container-uid 0 to unprivileged host uid %s; an escape lands unprivileged",
				nz(s.UserNSHostUID, "!= 0"))
		}
		return 0, VerdictUnknown, "user not reported; assuming root (fail-closed)"
	}
}

func gradeCaps(s Spec) (int, Verdict, string) {
	// Privileged grants the full capability set regardless of cap_drop.
	if s.Privileged == Yes {
		return 0, VerdictFail, "privileged: the full capability set is granted"
	}
	switch s.CapDropAll {
	case Yes:
		if len(s.CapAdd) == 0 {
			return 20, VerdictPass, "all capabilities dropped, none added back"
		}
		// Dropped ALL but added some back: partial credit, scaled by how many.
		pts := 20 - 4*len(s.CapAdd)
		if pts < 6 {
			pts = 6
		}
		return pts, VerdictWarn, fmt.Sprintf("dropped ALL but re-added: %s", strings.Join(s.CapAdd, ", "))
	case No:
		if len(s.CapAdd) > 0 {
			return 0, VerdictFail, fmt.Sprintf("default caps retained and extra caps added: %s", strings.Join(s.CapAdd, ", "))
		}
		return 4, VerdictFail, "default capability set retained (includes CAP_NET_RAW, CAP_MKNOD, …)"
	default:
		return 0, VerdictUnknown, "capability set not reported; assuming default (fail-closed)"
	}
}

func gradeSeccomp(s Spec) (int, Verdict, string) {
	// Privileged disables seccomp entirely.
	if s.Privileged == Yes {
		return 0, VerdictFail, "privileged: seccomp is disabled"
	}
	switch strings.ToLower(strings.TrimSpace(s.Seccomp)) {
	case "confined", "default", "runtime/default", "runtimedefault":
		return 15, VerdictPass, "seccomp profile active (syscall surface filtered)"
	case "unconfined", "":
		if s.Seccomp == "" {
			return 0, VerdictUnknown, "seccomp not reported; assuming unconfined (fail-closed)"
		}
		return 0, VerdictFail, "seccomp=unconfined: the full syscall surface is exposed"
	default:
		// A custom profile path — treat as confined (a profile is applied).
		return 15, VerdictPass, fmt.Sprintf("custom seccomp profile: %s", s.Seccomp)
	}
}

func gradeNetwork(s Spec) (int, Verdict, string) {
	m := strings.ToLower(strings.TrimSpace(s.NetworkMode))
	if s.HostNetwork == Yes || m == "host" {
		return 0, VerdictFail, "host network namespace: full host network reachability"
	}
	switch {
	case m == "none":
		return 15, VerdictPass, "network=none: no NIC but loopback, no egress"
	case m == "":
		return 0, VerdictUnknown, "network mode not reported; assuming egress-capable (fail-closed)"
	case strings.HasPrefix(m, "container:"):
		return 6, VerdictWarn, fmt.Sprintf("shares another container's network stack (%s)", s.NetworkMode)
	default:
		// bridge / default / a named network: egress-capable.
		return 4, VerdictWarn, fmt.Sprintf("network=%s: outbound egress is possible; prefer network=none", s.NetworkMode)
	}
}

func gradeReadonly(s Spec) (int, Verdict, string) {
	switch s.ReadonlyRoot {
	case Yes:
		return 10, VerdictPass, "root filesystem is read-only"
	case No:
		return 0, VerdictFail, "root filesystem is writable: tamper/persistence surface"
	default:
		return 0, VerdictUnknown, "root filesystem mode not reported; assuming writable (fail-closed)"
	}
}

func gradeDockerSock(s Spec) (int, Verdict, string) {
	// Polarity: DockerSock == Yes means the socket IS exposed (bad).
	switch s.DockerSock {
	case No:
		return 15, VerdictPass, "no docker.sock / OCI control socket mounted"
	case Yes:
		return 0, VerdictFail, "docker.sock is mounted: trivial host-root escape (docker run --privileged -v /:/host)"
	default:
		return 0, VerdictUnknown, "mounts not reported; cannot rule out docker.sock (fail-closed)"
	}
}

func gradeHostNS(s Spec) (int, Verdict, string) {
	if s.Privileged == Yes {
		return 0, VerdictFail, "privileged: host devices and namespaces are reachable"
	}
	var shared []string
	if s.HostPID == Yes {
		shared = append(shared, "PID")
	}
	if s.HostIPC == Yes {
		shared = append(shared, "IPC")
	}
	if s.HostNetwork == Yes {
		shared = append(shared, "network")
	}
	if len(shared) > 0 {
		sort.Strings(shared)
		return 0, VerdictFail, fmt.Sprintf("shares host namespace(s): %s", strings.Join(shared, ", "))
	}
	// None shared. If we had no signal at all for any of them, that is unknown.
	if s.HostPID == Unknown && s.HostIPC == Unknown && s.HostNetwork == Unknown {
		return 0, VerdictUnknown, "namespace sharing not reported; assuming shared (fail-closed)"
	}
	return 10, VerdictPass, "no host PID/IPC/network namespace sharing"
}

// nz returns v if non-empty, else fallback.
func nz(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
