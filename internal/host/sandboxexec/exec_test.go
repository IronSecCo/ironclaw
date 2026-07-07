package sandboxexec

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func indexOf(argv []string, want string) int {
	for i, a := range argv {
		if a == want {
			return i
		}
	}
	return -1
}

// HardenedArgs is the containment contract: if any flag regresses an escape becomes
// possible, so each is asserted explicitly.
func TestHardenedArgs(t *testing.T) {
	c := Config{Image: "alpine:3.20", DockerBin: "docker", Runtime: "runsc", SeccompPath: "/tmp/s.json"}
	argv := HardenedArgs(c, "ic-sbx-mcp-x", "alpine:3.20", "echo hi")
	joined := strings.Join(argv, " ")
	for _, f := range []string{
		"--runtime runsc", "--network none", "--cap-drop ALL",
		"--security-opt no-new-privileges", "--security-opt seccomp=/tmp/s.json",
		"--read-only", "--user 65532:65532", "--pids-limit 256",
		"--memory 512m", "--cpus 1", "--rm", "ic-sbx-mcp-x",
	} {
		if !strings.Contains(joined, f) {
			t.Errorf("hardened argv missing %q\n  got: %s", f, joined)
		}
	}
	sep, img := indexOf(argv, "--"), indexOf(argv, "alpine:3.20")
	if sep < 0 || img < 0 || sep+1 != img {
		t.Errorf("expected '--' immediately before image; sep=%d img=%d", sep, img)
	}
	n := len(argv)
	if argv[n-3] != "sh" || argv[n-2] != "-c" || argv[n-1] != "echo hi" {
		t.Errorf("command tail wrong: %v", argv[n-3:])
	}
}

func TestContainmentSummaryLabelsRuntime(t *testing.T) {
	gv := ContainmentSummary(Config{Runtime: "runsc", SeccompPath: "/tmp/s"})
	if !strings.Contains(gv, "runtime=runsc (gVisor") {
		t.Errorf("runsc must advertise gVisor: %s", gv)
	}
	fb := ContainmentSummary(Config{Runtime: "runc"})
	if strings.Contains(fb, "gVisor:") || !strings.Contains(fb, "NOT gVisor") {
		t.Errorf("runc fallback must be labelled NOT gVisor: %s", fb)
	}
	if !strings.Contains(fb, "seccomp=engine-default") {
		t.Errorf("empty seccomp path must report engine-default: %s", fb)
	}
}

func TestValidateImageRefRejectsFlags(t *testing.T) {
	for _, bad := range []string{"--volume=/:/host", "-v", "--user=0:0"} {
		if err := ValidateImageRef(bad); err == nil {
			t.Errorf("image %q should be rejected", bad)
		}
	}
	if err := ValidateImageRef("alpine:3.20"); err != nil {
		t.Errorf("valid image rejected: %v", err)
	}
}

func TestExecStructuredResult(t *testing.T) {
	var gotArgv []string
	c := Config{
		Image: "alpine:3.20", DockerBin: "docker", Runtime: "runsc", TimeoutSec: 30,
		Run: func(_ context.Context, _ string, argv []string) (string, string, int, error) {
			gotArgv = argv
			return "hello\n", "", 0, nil
		},
	}
	res, err := c.Exec(context.Background(), "echo hello", "", 0, "suf1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.ExitCode != 0 || res.Stdout != "hello\n" || res.LaunchErr != "" || !res.GVisor {
		t.Errorf("bad result: %+v", res)
	}
	if indexOf(gotArgv, "ic-sbx-mcp-suf1") < 0 {
		t.Errorf("box name suffix not applied: %v", gotArgv)
	}
}

func TestExecImageOverrideAndValidation(t *testing.T) {
	c := Config{Image: "alpine:3.20", DockerBin: "docker", Runtime: "runsc",
		Run: func(_ context.Context, _ string, _ []string) (string, string, int, error) { return "", "", 0, nil }}
	// override honored
	res, _ := c.Exec(context.Background(), "true", "python:3.12", 0, "s")
	if res.Image != "python:3.12" {
		t.Errorf("image override not applied: %s", res.Image)
	}
	// hostile override rejected before run
	if _, err := c.Exec(context.Background(), "true", "--volume=/:/x", 0, "s"); err == nil {
		t.Errorf("hostile image should be rejected")
	}
	// empty command rejected
	if _, err := c.Exec(context.Background(), "   ", "", 0, "s"); err == nil {
		t.Errorf("empty command should be rejected")
	}
}

// A launch failure is surfaced via LaunchErr (fail-closed), not a returned error.
func TestExecLaunchFailureIsFailClosed(t *testing.T) {
	c := Config{Image: "alpine:3.20", DockerBin: "docker", Runtime: "runsc",
		Run: func(_ context.Context, _ string, _ []string) (string, string, int, error) {
			return "", "", -1, errors.New("runsc not found")
		}}
	res, err := c.Exec(context.Background(), "true", "", 0, "s")
	if err != nil {
		t.Fatalf("launch failure must not be a returned error: %v", err)
	}
	if res.LaunchErr == "" || res.ExitCode != -1 {
		t.Errorf("launch failure not reported: %+v", res)
	}
	if !strings.Contains(FormatText(res), "containment:") {
		t.Errorf("formatted result should note containment")
	}
}
