package scan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// haproxyImageInspect is a trimmed but FAITHFUL `podman image inspect
// docker.io/library/haproxy:latest` blob: the real key set, with the long
// History/RootFS/GraphDriver arrays dropped. What matters is what is NOT here.
// An image inspect has no HostConfig, no Mounts, no Name and no NetworkSettings,
// which is precisely why the container adapter must never be fed one.
const haproxyImageInspect = `[{
  "Id": "sha256:78b71716647e884d0a58c0a111053a8dd151b71907746b8a39d37abf613578e8",
  "RepoTags": ["docker.io/library/haproxy:latest"],
  "RepoDigests": ["docker.io/library/haproxy@sha256:0f0a1cf1e0a5ba7d2a2e4d1c2b3a49586d3b9b1c8f2e5a7c9d0b1e2f3a4b5c6d"],
  "Architecture": "arm64",
  "Os": "linux",
  "Config": {
    "User": "haproxy",
    "Env": ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HAPROXY_VERSION=3.2.7"],
    "Cmd": ["haproxy", "-f", "/usr/local/etc/haproxy/haproxy.cfg"],
    "Entrypoint": ["docker-entrypoint.sh"],
    "WorkingDir": "/",
    "StopSignal": "SIGUSR1"
  }
}]`

// rootImageInspect is an image that declares no USER at all: the common case, and
// the one real finding image mode can make.
const rootImageInspect = `[{
  "Id": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "RepoTags": ["docker.io/library/nginx:latest"],
  "Config": { "Cmd": ["nginx", "-g", "daemon off;"] }
}]`

// The image adapter must leave EVERY run-time posture at Unknown. This is the
// property that makes image mode honest: the scorer can then report those
// dimensions as N/A instead of inventing a verdict for them.
func TestSpecFromImageInspect_LeavesRuntimePosturesUnknown(t *testing.T) {
	s, err := SpecFromImageInspect([]byte(haproxyImageInspect), "podman", "docker.io/library/haproxy:latest")
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != ModeImage {
		t.Fatalf("mode=%v, want ModeImage", s.Mode)
	}
	if s.Target != "docker.io/library/haproxy:latest" {
		t.Errorf("target=%q, want the reference the caller asked for", s.Target)
	}
	if s.RunAsNonRoot != Yes || s.User != "haproxy" {
		t.Errorf("USER haproxy should grade non-root; got RunAsNonRoot=%v user=%q", s.RunAsNonRoot, s.User)
	}
	for _, c := range []struct {
		name string
		got  Tristate
	}{
		{"CapDropAll", s.CapDropAll},
		{"DockerSock", s.DockerSock},
		{"ReadonlyRoot", s.ReadonlyRoot},
		{"HostPID", s.HostPID},
		{"HostIPC", s.HostIPC},
		{"HostNetwork", s.HostNetwork},
		{"Privileged", s.Privileged},
	} {
		if c.got != Unknown {
			t.Errorf("%s = %v from an IMAGE inspect; an image carries no such posture, it must stay Unknown", c.name, c.got)
		}
	}
	if s.Seccomp != "" {
		t.Errorf("Seccomp = %q from an IMAGE inspect; want \"\" (unknown)", s.Seccomp)
	}
	if s.NetworkMode != "" {
		t.Errorf("NetworkMode = %q from an IMAGE inspect; want \"\" (unknown)", s.NetworkMode)
	}
	if s.Runtime != "" {
		t.Errorf("Runtime = %q from an IMAGE inspect; an image has no runtime", s.Runtime)
	}
}

func TestSpecFromImageInspect_NoUserGradesRoot(t *testing.T) {
	s, err := SpecFromImageInspect([]byte(rootImageInspect), "docker", "nginx:latest")
	if err != nil {
		t.Fatal(err)
	}
	if s.RunAsNonRoot != No {
		t.Errorf("an image with no USER instruction must grade as root, got %v", s.RunAsNonRoot)
	}
	if d := dimByKey(Score(s), "user.nonroot"); d.Verdict != VerdictFail {
		t.Errorf("user.nonroot verdict = %s, want FAIL", d.Verdict)
	}
}

func TestSpecFromImageInspect_Errors(t *testing.T) {
	if _, err := SpecFromImageInspect([]byte("not json"), "docker", "x"); err == nil {
		t.Error("expected parse error")
	}
	if _, err := SpecFromImageInspect([]byte("[]"), "docker", "x"); err == nil {
		t.Error("expected empty-array error")
	}
	// A single object (not wrapped in an array) is tolerated, as elsewhere.
	if _, err := SpecFromImageInspect([]byte(`{"Id":"sha256:aa","Config":{"User":"1000"}}`), "docker", "x"); err != nil {
		t.Errorf("single object should parse: %v", err)
	}
}

// --------------------------------------------------------------------------- //
// THE SEAM REGRESSION (IRO-711 / IRO-712).
//
// The defect never lived in the scorer. score_test.go asserts Score(Spec{}) is
// fail-closed and passes, but the container adapter NEVER produces a Spec{}:
// specFromDockerLike resolves absence to a concrete value BEFORE the scorer runs.
// That is correct for a container and wrong for an image, so the guarantee has to
// be tested at the adapter seam, not one layer below it.
// --------------------------------------------------------------------------- //

// The hazard pin / negative control. Routing an IMAGE inspect blob through the
// CONTAINER adapter is the original bug, and this test asserts it still produces
// the affirmative false claims. It is deliberately an assertion about BROKEN
// behaviour: it documents why the two adapters must stay separate, and it stops
// anyone "simplifying" the split away on the theory that specFromDockerLike
// handles an image fine. If this test ever needs changing, the mode split is
// being dismantled.
func TestSpecFromDockerLike_OverwritesUnknownWithAffirmativeClaims(t *testing.T) {
	// An image inspect reduces to empty docker-like fields: no HostConfig, no
	// Mounts, no Name.
	s := specFromDockerLike(dockerLikeFields{user: "haproxy"}, "podman")

	if s.Seccomp != "confined" {
		t.Fatalf("Seccomp=%q: the container adapter is expected to assert the default profile", s.Seccomp)
	}
	if s.DockerSock != No || s.CapDropAll != No || s.ReadonlyRoot != No {
		t.Fatalf("the container adapter is expected to resolve absence to a concrete value; got sock=%v caps=%v ro=%v",
			s.DockerSock, s.CapDropAll, s.ReadonlyRoot)
	}
	if s.HostPID != No || s.HostIPC != No || s.HostNetwork != No {
		t.Fatalf("the container adapter is expected to resolve namespaces concretely; got pid=%v ipc=%v net=%v",
			s.HostPID, s.HostIPC, s.HostNetwork)
	}

	// Those overwrites are worth 40 points of affirmative PASS about postures
	// nothing observed. THIS is the fail-open, and it is why image data must not
	// reach here.
	r := Score(s)
	var falsePass int
	for _, key := range []string{"seccomp", "docker.sock", "namespaces.host"} {
		d := dimByKey(r, key)
		if d.Verdict == VerdictPass {
			falsePass += d.Score
		}
	}
	if falsePass != 40 {
		t.Fatalf("expected the documented 40 points of unobserved PASS through this seam, got %d", falsePass)
	}

	// The load-bearing guarantee: whatever it grades, this adapter ALWAYS declares
	// container mode. It has no way to represent an image, so an image must never
	// be routed through it.
	if s.Mode != ModeContainer {
		t.Fatalf("specFromDockerLike mode=%v, want ModeContainer", s.Mode)
	}
}

// The positive control: the same image data through the IMAGE adapter makes none
// of those claims and yields no composite.
func TestScoreImage_NoCompositeAndNoUnobservedClaims(t *testing.T) {
	s, err := SpecFromImageInspect([]byte(haproxyImageInspect), "podman", "docker.io/library/haproxy:latest")
	if err != nil {
		t.Fatal(err)
	}
	r := Score(s)

	if r.Mode != ModeImage {
		t.Fatalf("report mode=%v, want ModeImage", r.Mode)
	}
	if r.Scored() {
		t.Fatal("an image report must not claim to be scored")
	}
	if r.Score != 0 || r.Grade != "" || r.Max != 0 {
		t.Fatalf("image report carries a composite: score=%d grade=%q max=%d", r.Score, r.Grade, r.Max)
	}

	// The one real finding survives.
	if d := dimByKey(r, "user.nonroot"); d.Verdict != VerdictPass || !strings.Contains(d.Detail, "haproxy") {
		t.Errorf("image USER finding lost: %+v", d)
	}

	// Every run-time dimension reports N/A, with no points in either direction.
	for _, key := range []string{"caps.dropped", "seccomp", "network.isolated", "rootfs.readonly", "docker.sock", "namespaces.host"} {
		d := dimByKey(r, key)
		if d.Verdict != VerdictNA {
			t.Errorf("%s verdict = %s, want N/A", key, d.Verdict)
		}
		if d.Score != 0 || d.Max != 0 {
			t.Errorf("%s carries points %d/%d; an unobservable dimension must carry none", key, d.Score, d.Max)
		}
		if !strings.Contains(d.Detail, "not observable from an image reference") {
			t.Errorf("%s detail = %q, want the N/A explanation", key, d.Detail)
		}
	}

	// Summing the dimensions must not produce a plausible-looking total.
	sum, max := 0, 0
	for _, d := range r.Dimensions {
		sum += d.Score
		max += d.Max
	}
	if sum != 0 || max != 0 {
		t.Errorf("dimension totals = %d/%d, want 0/0 so a summing consumer cannot fabricate a score", sum, max)
	}
}

// The single highest-value line of this change: an image scan must never print
// "no docker.sock / OCI control socket mounted", nor a grade.
func TestRenderTable_ImageModeEmitsNoGradeAndNoFalsePass(t *testing.T) {
	s, _ := SpecFromImageInspect([]byte(haproxyImageInspect), "podman", "docker.io/library/haproxy:latest")
	var b bytes.Buffer
	RenderTable(&b, Score(s))
	out := b.String()

	for _, forbidden := range []string{
		"no docker.sock / OCI control socket mounted",
		"seccomp profile active",
		"no host PID/IPC/network namespace sharing",
		"grade A", "grade B", "grade C", "grade D", "grade F",
		"(hardened)", "(wide open)", "(weak, fix the FAILs)",
		"15/15",
		"/100",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("image-mode table contains %q:\n%s", forbidden, out)
		}
	}
	for _, want := range []string{"N/A", "image reference", "--dockerfile", "USER haproxy"} {
		if !strings.Contains(out, want) {
			t.Errorf("image-mode table missing %q:\n%s", want, out)
		}
	}
}

// Schema 1.1: mode is positive in BOTH modes; the composite keys are absent only
// in image mode.
func TestRenderJSON_ImageModeOmitsCompositeCarriesMode(t *testing.T) {
	s, _ := SpecFromImageInspect([]byte(haproxyImageInspect), "podman", "docker.io/library/haproxy:latest")
	var b bytes.Buffer
	if err := RenderJSON(&b, Score(s)); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["schemaVersion"] != "1.1" {
		t.Errorf("schemaVersion=%v, want 1.1", m["schemaVersion"])
	}
	if m["mode"] != "image" {
		t.Errorf("mode=%v, want image", m["mode"])
	}
	for _, k := range []string{"score", "grade", "max"} {
		if _, ok := m[k]; ok {
			t.Errorf("image-mode JSON carries %q=%v; an image has no composite", k, m[k])
		}
	}
	// runtime is legitimately absent (an image has no runtime), which is exactly
	// why it can never be the mode signal.
	if _, ok := m["runtime"]; ok {
		t.Errorf("image-mode JSON carries runtime=%v", m["runtime"])
	}
}

// ScanMode round-trips by NAME, and an unrecognized mode is an error rather than
// a silent fallback to "container".
func TestScanModeJSONRoundTrip(t *testing.T) {
	for _, m := range []ScanMode{ModeContainer, ModeImage, ModeDockerfile} {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var got ScanMode
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != m {
			t.Errorf("round-trip %s -> %v", b, got)
		}
	}
	var m ScanMode
	if err := json.Unmarshal([]byte(`"sandbox"`), &m); err == nil {
		t.Error("an unknown mode must be an error, not a silent fallback to container")
	}
}

// Every other adapter keeps declaring container mode, so the JSON gains "mode"
// without any of them changing meaning.
func TestNonImageAdaptersDeclareContainerMode(t *testing.T) {
	for name, raw := range map[string]string{"docker": weakInspect, "hardened": hardenedInspect} {
		s, err := SpecFromDockerInspect([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if s.Mode != ModeContainer || Score(s).Mode != ModeContainer {
			t.Errorf("%s: mode=%v, want ModeContainer", name, s.Mode)
		}
		if !Score(s).Scored() {
			t.Errorf("%s: a container report must be scored", name)
		}
	}
}
