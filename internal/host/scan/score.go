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
	// VerdictNA means the dimension is NOT OBSERVABLE from the artifact that was
	// scanned, so it is not graded at all and carries no points in either
	// direction. It is categorically different from UNKNOWN: UNKNOWN says "this
	// container has a posture and we could not read it", so it is scored
	// fail-closed as insecure; N/A says "the artifact has no such posture to
	// read". Grading a run-time control on an image would be a claim about a
	// container that does not exist (IRO-712), in either direction.
	VerdictNA Verdict = "N/A"
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
//
// Score/Grade/Max are meaningful only when Scored() is true. In ModeImage there
// is no composite at all and the JSON renderer omits those keys outright rather
// than emitting a zero that reads as "failed" (IRO-712). Mode is the positive
// assertion a consumer switches on; never infer the mode from an absent key.
type Report struct {
	// Mode says what was inspected: "container" | "image" | "dockerfile".
	// ALWAYS present in the JSON, in every mode.
	Mode    ScanMode `json:"mode"`
	Source  string   `json:"source"`
	Target  string   `json:"target"`
	Runtime string   `json:"runtime,omitempty"`
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

// Scored reports whether the report carries a meaningful composite score and
// grade. It is false in ModeImage, where six of the seven dimensions are not
// observable and no composite is emitted. Check this before reading Score/Grade
// or rendering a badge: a zero Score in image mode is the ABSENCE of a score,
// not an F.
func (r Report) Scored() bool { return r.Mode != ModeImage }

// Score grades a Spec across every dimension and returns the full Report. It is
// pure: no I/O, no clock, deterministic for a given Spec.
//
// An image-mode Spec is routed to scoreImage, which grades ONLY the dimension an
// image can actually answer and reports the rest as N/A with no composite.
func Score(s Spec) Report {
	if s.Mode == ModeImage {
		return scoreImage(s)
	}
	r := Report{
		Mode:    s.Mode,
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

// --------------------------------------------------------------------------- //
// Image mode (IRO-712).
// --------------------------------------------------------------------------- //

// imageObservableDim is the only graded dimension an image reference can answer:
// the OCI image config's USER. Everything else in the dimension set is decided by
// the `docker run` invocation.
const imageObservableDim = "user.nonroot"

// naFromImage is the detail every unobservable dimension carries in image mode.
const naFromImage = "not observable from an image reference: this is run-time configuration and no container exists"

// scoreImage builds the report for an IMAGE reference. It emits NO composite
// score and NO grade, on purpose:
//
//   - Only user.nonroot (15 of 100 points) is observable from an image config, so
//     any composite would be a number computed almost entirely from things that
//     were not looked at.
//   - Scoring out of the observable total is worse, not better: with one
//     observable dimension it collapses to two values and renders `USER nobody`
//     on an otherwise unhardened image as 15/15, which every badge normalizes to
//     100% / grade A.
//   - The image number is not a bound in either direction. The same image scanned
//     as a container ranges from 15/100 (hostile flags) to 100/100 (hardened),
//     so no wording may call image mode a floor or a ceiling.
//
// The one real finding is kept, and the six run-time dimensions report N/A with
// zero points available so a consumer that sums the dimensions gets 0/0 rather
// than a plausible-looking total.
func scoreImage(s Spec) Report {
	r := Report{
		Mode:   ModeImage,
		Source: s.Source,
		Target: s.Target,
		Notes:  append([]string(nil), s.Notes...),
	}
	for _, sc := range scorers {
		d := Dimension{Key: sc.key, Title: sc.title, Verdict: VerdictNA, Detail: naFromImage}
		if sc.key == imageObservableDim {
			d.Verdict, d.Detail = gradeImageUser(s)
		}
		r.Dimensions = append(r.Dimensions, d)
	}
	r.Notes = append(r.Notes,
		"image mode: no containment score or grade is reported. Six of the seven graded dimensions (capabilities, seccomp, network, read-only rootfs, control-socket exposure, host namespaces) are run-time configuration that an image does not carry, so grading them either way would be a claim about a container that does not exist.",
		"to grade a workload's containment, scan the RUNNING container: `docker run -d --name app <image>` then `ironctl scan app`.",
		"to grade the image build itself (base pinning, USER, baked secrets, ADD, world-writable paths), use the purpose-built static scorer: `ironctl scan --dockerfile Dockerfile`.",
	)
	return r
}

// gradeImageUser grades the image config's USER. It is the image-mode dual of
// gradeNonRoot and awards no points: image mode has no composite. Fail-closed is
// preserved (an unreadable user is UNKNOWN, never a pass).
func gradeImageUser(s Spec) (Verdict, string) {
	switch s.RunAsNonRoot {
	case Yes:
		return VerdictPass, fmt.Sprintf("image config sets USER %s (uid != 0); a container started from it defaults to a non-root uid", nz(s.User, "non-root"))
	case No:
		return VerdictFail, fmt.Sprintf("image config runs as root (USER %s); a container started from it is uid 0 unless the run command overrides it", nz(s.User, "0"))
	default:
		return VerdictUnknown, "image config user not reported; assuming root (fail-closed)"
	}
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
