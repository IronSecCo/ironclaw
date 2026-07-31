package main

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
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

// imageSafeScanFlags classifies every scan flag that is NOT routed through
// rejectScoreBearingOutputs, grouped by the reason it cannot hand an image
// reference a composite score. The reason is the payload: the point of the list
// is to make someone state why a new flag is safe, not to have somewhere to put
// it. Together with scoreBearingFlags it must cover the flags runScan registers.
var imageSafeScanFlags = map[string][]string{
	// Re-renders the same report. In image mode that report already omits
	// score/grade and marks the six unobservable dimensions N/A (IRO-712).
	"representation of the report, which carries no composite in image mode": {
		"--json",
	},
	// Rejected for an image reference too, but on its own early-return path
	// before rejectScoreBearingOutputs is reached (scan.go, runCompare).
	"score-bearing, rejected for image refs on its own path": {
		"--compare",
	},
	// Grades a Dockerfile with a different, authoring-time dimension set and its
	// own scorer; daemon-free, so it never resolves an OCI reference at all.
	"static Dockerfile grading, a separate scorer on a separate path": {
		"--dockerfile",
	},
	// Select a non-runtime input adapter (a manifest, chart, plan or template).
	// The target is a file, never a container or an image reference.
	"selects a manifest/IaC input adapter, so the target is never an OCI ref": {
		"--compose", "--service", "--k8s", "--k8s-admission", "--admission-response",
		"--emit-policy", "--check", "--helm", "--terraform", "--nomad", "--ecs",
		"--cloudrun", "--cloudformation", "--sam", "--pulumi", "--azure",
		"--app-runner", "--bicep", "--cdk", "--kustomize", "--openshift",
	},
	// Name a helper binary or pick the runtime. Inputs to how we inspect, not
	// outputs, so none of them can emit or gate on a number.
	"names a helper binary or the runtime, not an output": {
		"--runtime", "--docker-bin", "--podman-bin", "--nerdctl-bin", "--helm-bin",
		"--terraform-bin", "--nomad-bin", "--bicep-bin", "--az-bin", "--cdk-bin",
		"--kustomize-bin", "--kubectl-bin",
	},
}

// registeredScanFlags parses scan.go and returns every flag runScan registers.
//
// Reading the source is the whole point. The previous version of this test
// compared scoreBearingFlags against a hardcoded copy of itself, so it could
// only fail if someone edited one of the two lists and not the other — a NEW
// score-bearing flag, which is the failure the test exists to prevent, passed
// it silently. Deriving the universe from the fs.* calls means a flag that
// nobody classified is a red test rather than a hole.
func registeredScanFlags(t *testing.T) []string {
	t.Helper()
	fset := gotoken.NewFileSet()
	f, err := parser.ParseFile(fset, "scan.go", nil, 0)
	if err != nil {
		t.Fatalf("parse scan.go: %v", err)
	}
	// Registration methods take the flag name first; the *Var forms take a
	// pointer first and the name second. Everything else on a FlagSet is
	// bookkeeping. A method in neither set is a registration form this test does
	// not understand, and staying quiet about it would reopen the hole.
	nameFirst := map[string]bool{
		"String": true, "Bool": true, "Int": true, "Int64": true, "Uint": true,
		"Uint64": true, "Float64": true, "Duration": true, "Func": true, "BoolFunc": true,
	}
	nameSecond := map[string]bool{
		"StringVar": true, "BoolVar": true, "IntVar": true, "Int64Var": true,
		"UintVar": true, "Uint64Var": true, "Float64Var": true, "DurationVar": true,
		"TextVar": true, "Var": true,
	}
	bookkeeping := map[string]bool{
		"Parse": true, "Parsed": true, "Args": true, "Arg": true, "NArg": true,
		"NFlag": true, "Lookup": true, "Set": true, "Visit": true, "VisitAll": true,
		"PrintDefaults": true, "SetOutput": true, "Output": true, "Init": true,
		"Name": true, "ErrorHandling": true,
	}
	var flags []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != "fs" {
			return true
		}
		method := sel.Sel.Name
		idx := -1
		switch {
		case nameFirst[method]:
			idx = 0
		case nameSecond[method]:
			idx = 1
		case bookkeeping[method]:
			return true
		default:
			t.Errorf("scan.go calls fs.%s, which this test does not recognise as a flag "+
				"registration; teach registeredScanFlags about it or the flag it defines "+
				"goes unclassified", method)
			return true
		}
		if len(call.Args) <= idx {
			t.Errorf("fs.%s called with %d args; cannot read the flag name", method, len(call.Args))
			return true
		}
		lit, ok := call.Args[idx].(*ast.BasicLit)
		if !ok || lit.Kind != gotoken.STRING {
			t.Errorf("fs.%s registers a flag whose name is not a string literal (%v); "+
				"this test cannot classify it", method, call.Args[idx])
			return true
		}
		flags = append(flags, "--"+strings.Trim(lit.Value, `"`))
		return true
	})
	return flags
}

// Every flag scan registers must be classified: either it is score-bearing and
// rejected for an image reference, or it is on imageSafeScanFlags with a stated
// reason. Adding a flag without classifying it fails here.
func TestEveryScanFlagIsClassifiedForImageMode(t *testing.T) {
	registered := registeredScanFlags(t)
	// Non-vacuity guard: if the parse silently matched nothing, every assertion
	// below would pass over an empty set.
	if len(registered) < 40 {
		t.Fatalf("only found %d scan flags (%v); the source parse is not seeing the "+
			"registrations, so this test would pass vacuously", len(registered), registered)
	}

	classified := map[string]string{}
	for _, f := range scoreBearingFlags {
		classified[f] = "score-bearing, rejected by rejectScoreBearingOutputs"
	}
	for reason, flags := range imageSafeScanFlags {
		for _, f := range flags {
			if prev, dup := classified[f]; dup {
				t.Errorf("%s is classified twice: %q and %q", f, prev, reason)
			}
			classified[f] = reason
		}
	}

	seen := map[string]bool{}
	for _, f := range registered {
		seen[f] = true
		if _, ok := classified[f]; !ok {
			t.Errorf("%s is registered by runScan but classified nowhere. If it emits or "+
				"gates on a composite score, add it to scoreBearingFlags; if it cannot, add "+
				"it to imageSafeScanFlags under the reason why", f)
		}
	}
	// The reverse direction catches a rename or a removal leaving a stale entry,
	// which would otherwise quietly shrink the set this test actually checks.
	for f, reason := range classified {
		if !seen[f] {
			t.Errorf("%s is classified (%q) but no longer registered by runScan; "+
				"the classification is stale", f, reason)
		}
	}
}

// --compare claims its own image-ref rejection path in imageSafeScanFlags. That
// claim is only worth making if it is checked: a comment cannot fail.
func TestCompareRejectsImageRefsOnItsOwnPath(t *testing.T) {
	src, err := os.ReadFile("scan.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `scan.UnsupportedForImageRef("--compare"`) {
		t.Error("--compare is classified as rejecting image refs on its own path, but " +
			"scan.go no longer calls scan.UnsupportedForImageRef(\"--compare\", ...); " +
			"either restore the rejection or reclassify the flag")
	}
}
