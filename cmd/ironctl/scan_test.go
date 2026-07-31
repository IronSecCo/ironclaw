package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const cliHardenedCompose = `
services:
  agent:
    image: ironclaw
    user: "65532:65532"
    read_only: true
    network_mode: none
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
`

const cliWeakCompose = `
services:
  web:
    image: nginx
    volumes: ["/var/run/docker.sock:/var/run/docker.sock"]
    pid: host
`

// The min-score gate must pass on a hardened target and fail on a weak one — the
// CI-gate contract, and proof the flags after --compose are actually parsed.
func TestCmdScan_MinScoreGate(t *testing.T) {
	hard := writeTemp(t, "hard.yml", cliHardenedCompose)
	if err := cmdScan([]string{"--compose", hard, "--min-score", "80"}); err != nil {
		t.Errorf("hardened compose should pass min-score 80: %v", err)
	}
	weak := writeTemp(t, "weak.yml", cliWeakCompose)
	err := cmdScan([]string{"--compose", weak, "--min-score", "80"})
	if err == nil || !strings.Contains(err.Error(), "below") {
		t.Errorf("weak compose should fail min-score 80, got: %v", err)
	}
}

// --badge writes a self-contained SVG for the graded target.
func TestCmdScan_BadgeWrite(t *testing.T) {
	hard := writeTemp(t, "hard.yml", cliHardenedCompose)
	badge := filepath.Join(t.TempDir(), "scan.svg")
	if err := cmdScan([]string{"--compose", hard, "--badge", badge, "--json"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(badge)
	if err != nil {
		t.Fatalf("badge not written: %v", err)
	}
	if !strings.HasPrefix(string(b), "<svg") {
		t.Errorf("badge is not SVG: %.40s", b)
	}
}

func TestCmdScan_NoTarget(t *testing.T) {
	if err := cmdScan(nil); err == nil {
		t.Error("expected an error when no target is given")
	}
}

const cliHardenedDockerfile = `FROM gcr.io/distroless/static-debian12@sha256:abc123abc123abc123abc123abc123abc123abc123abc123abc123abc123abcd
COPY --chown=65532:65532 app /app
HEALTHCHECK CMD ["/app","--health"]
USER 65532:65532
ENTRYPOINT ["/app"]
`

const cliWeakDockerfile = `FROM ubuntu:latest
ADD https://example.com/i.sh /tmp/i.sh
ENV API_TOKEN=deadbeefsecret
RUN chmod 777 /app
`

// The --dockerfile mode powers the pre-commit hook (IRO-494): files are passed as
// positionals so a hook can append its matched filenames after fixed flags, and
// --min-score fails the commit if ANY graded file is below the threshold.
func TestCmdScan_DockerfileGate(t *testing.T) {
	good := writeTemp(t, "Dockerfile.good", cliHardenedDockerfile)
	if err := cmdScan([]string{"--dockerfile", good, "--min-score", "90"}); err != nil {
		t.Errorf("hardened Dockerfile should pass min-score 90: %v", err)
	}
	bad := writeTemp(t, "Dockerfile.bad", cliWeakDockerfile)
	// A batch containing a porous Dockerfile must trip the gate (the hook use case).
	err := cmdScan([]string{"--dockerfile", good, bad, "--min-score", "90"})
	if err == nil || !strings.Contains(err.Error(), "below") {
		t.Errorf("a batch with a weak Dockerfile should fail min-score 90, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Dockerfile.bad") {
		t.Errorf("gate error should name the offending file, got: %v", err)
	}
}

func TestCmdScan_DockerfileNoPath(t *testing.T) {
	if err := cmdScan([]string{"--dockerfile"}); err == nil {
		t.Error("expected an error when --dockerfile is given no path")
	}
}

// Every score-bearing output must fail LOUDLY on an image reference rather than
// emit a grade or a placeholder (IRO-712). This exercises the gate directly, so
// it runs with no container runtime present.
func TestRejectScoreBearingOutputs_ImageMode(t *testing.T) {
	const ref = "docker.io/library/haproxy:latest"
	for _, flag := range scoreBearingFlags {
		err := rejectScoreBearingOutputs(ref, map[string]bool{flag: true})
		if err == nil {
			t.Errorf("%s was allowed on an image reference; it needs a composite score", flag)
			continue
		}
		msg := err.Error()
		for _, want := range []string{flag, ref, "--dockerfile", "image reference"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s error missing %q: %v", flag, want, msg)
			}
		}
	}
	// The default table and --json request none of these, so they stay allowed:
	// image mode still reports the USER finding and the N/A dimensions.
	if err := rejectScoreBearingOutputs(ref, nil); err != nil {
		t.Errorf("a plain image scan must be allowed: %v", err)
	}
}

// scoreBearingFlags must stay in sync with the flags runScan actually feeds it;
// a flag that emits a grade but is missing from the list is a silent hole.
func TestScoreBearingFlagsCoverGradeEmittingOutputs(t *testing.T) {
	want := map[string]bool{
		"--badge": true, "--badge-json": true, "--badge-md": true, "--md": true,
		"--share": true, "--sarif": true, "--min-score": true, "--fix": true,
	}
	got := map[string]bool{}
	for _, f := range scoreBearingFlags {
		got[f] = true
	}
	for f := range want {
		if !got[f] {
			t.Errorf("%s emits or gates on a score but is not in scoreBearingFlags", f)
		}
	}
	for f := range got {
		if !want[f] {
			t.Errorf("scoreBearingFlags has an unexpected entry %q", f)
		}
	}
}
