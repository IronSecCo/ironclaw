package scan

import (
	"strings"
	"testing"
)

// dimByKey returns the graded dimension with the given key.
func dimByKey(r Report, key string) Dimension {
	for _, d := range r.Dimensions {
		if d.Key == key {
			return d
		}
	}
	return Dimension{}
}

// Each dimension scorer is exercised across PASS / FAIL / UNKNOWN, asserting the
// fail-closed rule: an Unknown posture is never scored above zero.

func TestGradeNonRoot(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		want    Verdict
		wantPts int
	}{
		{"nonroot", Spec{RunAsNonRoot: Yes, User: "65532"}, VerdictPass, 15},
		{"root", Spec{RunAsNonRoot: No, User: "0"}, VerdictFail, 0},
		{"unknown", Spec{RunAsNonRoot: Unknown}, VerdictUnknown, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Score(c.spec)
			d := dimByKey(r, "user.nonroot")
			if d.Verdict != c.want || d.Score != c.wantPts {
				t.Fatalf("got %s/%d want %s/%d", d.Verdict, d.Score, c.want, c.wantPts)
			}
		})
	}
}

func TestGradeCaps(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		want    Verdict
		wantPts int
	}{
		{"drop-all", Spec{CapDropAll: Yes}, VerdictPass, 20},
		{"drop-all-readd-one", Spec{CapDropAll: Yes, CapAdd: []string{"NET_BIND_SERVICE"}}, VerdictWarn, 16},
		{"privileged", Spec{Privileged: Yes, CapDropAll: Yes}, VerdictFail, 0},
		{"default-caps", Spec{CapDropAll: No}, VerdictFail, 4},
		{"unknown", Spec{CapDropAll: Unknown}, VerdictUnknown, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := dimByKey(Score(c.spec), "caps.dropped")
			if d.Verdict != c.want || d.Score != c.wantPts {
				t.Fatalf("got %s/%d want %s/%d", d.Verdict, d.Score, c.want, c.wantPts)
			}
		})
	}
}

func TestGradeSeccomp(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		want    Verdict
		wantPts int
	}{
		{"default", Spec{Seccomp: "confined"}, VerdictPass, 15},
		{"runtime-default", Spec{Seccomp: "RuntimeDefault"}, VerdictPass, 15},
		{"custom-path", Spec{Seccomp: "/etc/profile.json"}, VerdictPass, 15},
		{"unconfined", Spec{Seccomp: "unconfined"}, VerdictFail, 0},
		{"privileged", Spec{Privileged: Yes, Seccomp: "confined"}, VerdictFail, 0},
		{"unknown", Spec{Seccomp: ""}, VerdictUnknown, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := dimByKey(Score(c.spec), "seccomp")
			if d.Verdict != c.want || d.Score != c.wantPts {
				t.Fatalf("got %s/%d want %s/%d", d.Verdict, d.Score, c.want, c.wantPts)
			}
		})
	}
}

func TestGradeNetwork(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		want    Verdict
		wantPts int
	}{
		{"none", Spec{NetworkMode: "none"}, VerdictPass, 15},
		{"host", Spec{NetworkMode: "host"}, VerdictFail, 0},
		{"host-flag", Spec{HostNetwork: Yes, NetworkMode: "bridge"}, VerdictFail, 0},
		{"bridge", Spec{NetworkMode: "bridge"}, VerdictWarn, 4},
		{"container", Spec{NetworkMode: "container:abc"}, VerdictWarn, 6},
		{"unknown", Spec{NetworkMode: ""}, VerdictUnknown, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := dimByKey(Score(c.spec), "network.isolated")
			if d.Verdict != c.want || d.Score != c.wantPts {
				t.Fatalf("got %s/%d want %s/%d", d.Verdict, d.Score, c.want, c.wantPts)
			}
		})
	}
}

func TestGradeReadonly(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		want    Verdict
		wantPts int
	}{
		{"readonly", Spec{ReadonlyRoot: Yes}, VerdictPass, 10},
		{"writable", Spec{ReadonlyRoot: No}, VerdictFail, 0},
		{"unknown", Spec{ReadonlyRoot: Unknown}, VerdictUnknown, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := dimByKey(Score(c.spec), "rootfs.readonly")
			if d.Verdict != c.want || d.Score != c.wantPts {
				t.Fatalf("got %s/%d want %s/%d", d.Verdict, d.Score, c.want, c.wantPts)
			}
		})
	}
}

func TestGradeDockerSock(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		want    Verdict
		wantPts int
	}{
		{"absent", Spec{DockerSock: No}, VerdictPass, 15},
		{"exposed", Spec{DockerSock: Yes}, VerdictFail, 0},
		{"unknown", Spec{DockerSock: Unknown}, VerdictUnknown, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := dimByKey(Score(c.spec), "docker.sock")
			if d.Verdict != c.want || d.Score != c.wantPts {
				t.Fatalf("got %s/%d want %s/%d", d.Verdict, d.Score, c.want, c.wantPts)
			}
		})
	}
}

func TestGradeHostNS(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		want    Verdict
		wantPts int
	}{
		{"none-shared", Spec{HostPID: No, HostIPC: No, HostNetwork: No}, VerdictPass, 10},
		{"host-pid", Spec{HostPID: Yes, HostIPC: No, HostNetwork: No}, VerdictFail, 0},
		{"privileged", Spec{Privileged: Yes, HostPID: No, HostIPC: No, HostNetwork: No}, VerdictFail, 0},
		{"unknown", Spec{}, VerdictUnknown, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := dimByKey(Score(c.spec), "namespaces.host")
			if d.Verdict != c.want || d.Score != c.wantPts {
				t.Fatalf("got %s/%d want %s/%d", d.Verdict, d.Score, c.want, c.wantPts)
			}
		})
	}
}

// TestScoreWeightsSumTo100 guards the invariant that a fully-hardened spec earns
// exactly 100 and the weights never drift.
func TestScoreWeightsSumTo100(t *testing.T) {
	hardened := Spec{
		RunAsNonRoot: Yes, User: "65532",
		CapDropAll:   Yes,
		Seccomp:      "confined",
		NetworkMode:  "none",
		ReadonlyRoot: Yes,
		DockerSock:   No,
		HostPID:      No, HostIPC: No, HostNetwork: No,
	}
	r := Score(hardened)
	if r.Score != 100 || r.Max != 100 {
		t.Fatalf("hardened spec scored %d/%d, want 100/100", r.Score, r.Max)
	}
	if r.Grade != "A" {
		t.Fatalf("hardened grade %s, want A", r.Grade)
	}
	sum := 0
	for _, s := range scorers {
		sum += s.max
	}
	if sum != TotalWeight {
		t.Fatalf("dimension weights sum to %d, want %d", sum, TotalWeight)
	}
}

// TestScoreFailClosedEmpty asserts an empty Spec (all Unknown) scores 0/F: an
// auditor that can see nothing must report the worst grade, never a passing one.
func TestScoreFailClosedEmpty(t *testing.T) {
	r := Score(Spec{})
	if r.Score != 0 || r.Grade != "F" {
		t.Fatalf("empty spec scored %d grade %s, want 0/F (fail-closed)", r.Score, r.Grade)
	}
	for _, d := range r.Dimensions {
		if d.Verdict == VerdictPass {
			t.Fatalf("dimension %s passed on an empty spec (not fail-closed)", d.Key)
		}
	}
}

func TestGrades(t *testing.T) {
	cases := map[int]string{100: "A", 90: "A", 89: "B", 75: "B", 74: "C", 50: "C", 49: "D", 25: "D", 24: "F", 0: "F"}
	for score, want := range cases {
		if g := grade(score); g != want {
			t.Errorf("grade(%d)=%s want %s", score, g, want)
		}
	}
}

func TestPrivilegedTanksScore(t *testing.T) {
	// A privileged root container should land in the failing band regardless of
	// any other posture: privilege is the master escape.
	r := Score(Spec{
		Privileged: Yes, RunAsNonRoot: No,
		NetworkMode: "none", ReadonlyRoot: Yes, DockerSock: No,
	})
	if r.Grade == "A" || r.Grade == "B" {
		t.Fatalf("privileged container graded %s (%d); expected a failing band", r.Grade, r.Score)
	}
	if d := dimByKey(r, "caps.dropped"); !strings.Contains(d.Detail, "privileged") {
		t.Fatalf("caps detail did not flag privileged: %q", d.Detail)
	}
}
