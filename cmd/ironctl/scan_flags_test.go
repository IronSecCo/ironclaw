package main

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Image mode has no composite score (IRO-712), so `ironctl scan <image>` rejects
// every output that would have to invent one. The hole that guards against is a
// flag added LATER that emits a grade and is never added to scoreBearingFlags:
// it would sail through the gate and print a number nothing measured.
//
// A hand-maintained expectation cannot catch that — it only catches someone
// editing one copy of the same list. So these tests read the flag universe out
// of scan.go's own registrations and require every flag to be CLASSIFIED: either
// score-bearing, an alias of one, or explicitly image-safe. A new flag fails
// until someone decides which it is.

// scanSourceFile is parsed by the tests below. `go test` runs with the package
// directory as its working directory, so the bare filename resolves.
const scanSourceFile = "scan.go"

// imageSafeFlags is the explicit allowlist: scan flags that carry no composite
// score and so stay legal on an image reference. A flag belongs here for one of
// two reasons:
//
//   - it selects a DIFFERENT input (--compose, --k8s, --dockerfile, --helm, ...),
//     so it never reaches image mode in the first place; or
//   - it is a representation or a helper-binary override that reports only what
//     was actually observed (--json, --runtime, --docker-bin, ...).
//
// This list is not documentation. TestEveryScanFlagIsClassified fails on any
// registered flag that appears in neither this list nor scoreBearingFlags.
var imageSafeFlags = []string{
	"--admission-response", "--app-runner", "--az-bin", "--azure", "--bicep",
	"--bicep-bin", "--cdk", "--cdk-bin", "--check", "--cloudformation",
	"--cloudrun", "--compare", "--compose", "--docker-bin", "--dockerfile",
	"--ecs", "--emit-policy", "--helm", "--helm-bin", "--json", "--k8s",
	"--k8s-admission", "--kubectl-bin", "--kustomize", "--kustomize-bin",
	"--nerdctl-bin", "--nomad", "--nomad-bin", "--openshift", "--podman-bin",
	"--pulumi", "--runtime", "--sam", "--service", "--terraform",
	"--terraform-bin",
}

// scoreBearingAliases maps an alias onto the canonical score-bearing flag it
// stands for. An alias is rejected in image mode exactly like its canonical
// flag, but is tracked separately because the rejection names what the user
// typed (see fixFlagBlame).
var scoreBearingAliases = map[string]string{"--remediate": "--fix"}

// TestEveryScanFlagIsClassified is the defence: it derives the flag universe
// from scan.go's fs.String/fs.Bool/fs.Int registrations, so adding a flag
// without classifying it turns this red.
func TestEveryScanFlagIsClassified(t *testing.T) {
	registered := parseScanFlagRegistrations(t)
	if len(registered) < 40 {
		t.Fatalf("only %d flags parsed out of scan.go; the parser has gone blind and every "+
			"assertion below is vacuous", len(registered))
	}

	classified := map[string]string{}
	for _, f := range scoreBearingFlags {
		classified[f] = "score-bearing"
	}
	for alias, canonical := range scoreBearingAliases {
		if classified[canonical] != "score-bearing" {
			t.Errorf("alias %s points at %s, which is not in scoreBearingFlags", alias, canonical)
		}
		if _, dup := classified[alias]; dup {
			t.Errorf("%s is classified twice", alias)
		}
		classified[alias] = "alias of " + canonical
	}
	for _, f := range imageSafeFlags {
		if prior, dup := classified[f]; dup {
			t.Errorf("%s is on the image-safe allowlist AND %s; it cannot be both", f, prior)
		}
		classified[f] = "image-safe"
	}

	for _, f := range registered {
		if _, ok := classified[f]; !ok {
			t.Errorf("scan flag %s is registered but not classified. Decide: does it emit or "+
				"gate on a composite score? Add it to scoreBearingFlags (and to the "+
				"rejectScoreBearingOutputs call site) if so, or to imageSafeFlags if not.", f)
		}
	}

	inUniverse := map[string]bool{}
	for _, f := range registered {
		inUniverse[f] = true
	}
	for f := range classified {
		if !inUniverse[f] {
			t.Errorf("%s is classified but is not a registered scan flag (renamed or removed?)", f)
		}
	}
}

// TestRejectScoreBearingOutputsCallSiteIsComplete closes the second half of the
// hole: a flag can be listed in scoreBearingFlags and still never be rejected,
// because the gate only sees the flags the call site actually passes it. The
// keys of that map literal must be exactly scoreBearingFlags.
func TestRejectScoreBearingOutputsCallSiteIsComplete(t *testing.T) {
	passed := parseRejectCallSiteKeys(t)
	if len(passed) == 0 {
		t.Fatal("found no rejectScoreBearingOutputs call site in scan.go; this test is vacuous")
	}

	want := append([]string(nil), scoreBearingFlags...)
	sort.Strings(want)
	sort.Strings(passed)

	wantSet := map[string]bool{}
	for _, f := range want {
		wantSet[f] = true
	}
	passedSet := map[string]bool{}
	for _, f := range passed {
		passedSet[f] = true
	}
	for _, f := range want {
		if !passedSet[f] {
			t.Errorf("%s is score-bearing but the rejectScoreBearingOutputs call site never "+
				"reports whether it was requested, so it is silently allowed on an image ref", f)
		}
	}
	for _, f := range passed {
		if !wantSet[f] {
			t.Errorf("the call site passes %q, which is not in scoreBearingFlags, so the gate "+
				"ignores it", f)
		}
	}
}

// fixFlagBlame decides which of the two remediation spellings the error names.
func TestFixFlagBlame(t *testing.T) {
	for _, tc := range []struct {
		name           string
		fix, remediate bool
		want           string
	}{
		{"neither", false, false, ""},
		{"fix", true, false, "--fix"},
		{"remediate alone is named as itself", false, true, "--remediate"},
		{"both", true, true, "--fix"},
	} {
		if got := fixFlagBlame(tc.fix, tc.remediate); got != tc.want {
			t.Errorf("%s: fixFlagBlame(%v, %v) = %q, want %q", tc.name, tc.fix, tc.remediate, got, tc.want)
		}
	}

	// End to end through the gate: a user who typed --remediate is told
	// --remediate is unsupported, not --fix.
	err := rejectScoreBearingOutputs("docker.io/library/haproxy:latest", map[string]string{
		"--fix": fixFlagBlame(false, true),
	})
	if err == nil {
		t.Fatal("--remediate must be rejected on an image reference")
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "--remediate ") {
		t.Errorf("rejection should open by naming --remediate, got: %v", msg)
	}
}

// registrationMethods are the flag.FlagSet methods that define a flag. Any other
// method called on the scan flag set is rejected below rather than ignored: a
// flag registered through an unrecognised method would be invisible to this
// whole file, which is exactly the blindness these tests exist to prevent.
var registrationMethods = map[string]bool{
	"String": true, "Bool": true, "Int": true,
}

// nonRegistrationMethods are the known flag.FlagSet methods that do NOT define a
// flag. Everything not in either map fails the parse.
var nonRegistrationMethods = map[string]bool{
	"Parse": true, "Args": true, "Arg": true, "NArg": true, "NFlag": true,
	"PrintDefaults": true, "SetOutput": true, "Output": true, "Lookup": true,
	"Visit": true, "VisitAll": true, "Set": true, "Init": true, "Name": true,
	"ErrorHandling": true, "Parsed": true,
}

// parseScanFlagRegistrations returns every flag scan.go registers on its flag
// set, with the "--" prefix, read from the source rather than restated.
func parseScanFlagRegistrations(t *testing.T) []string {
	t.Helper()
	file := parseScanSource(t)

	var flags []string
	ast.Inspect(file, func(n ast.Node) bool {
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
		switch {
		case registrationMethods[method]:
		case nonRegistrationMethods[method]:
			return true
		default:
			t.Errorf("scan.go calls fs.%s, which this test does not recognise. If it registers "+
				"a flag, add it to registrationMethods; otherwise add it to "+
				"nonRegistrationMethods. Leaving it unlisted makes the flag invisible here.", method)
			return true
		}
		if len(call.Args) == 0 {
			t.Errorf("fs.%s called with no arguments", method)
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != gotoken.STRING {
			t.Errorf("fs.%s registers a flag whose name is not a string literal; this test "+
				"cannot see it", method)
			return true
		}
		name, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Errorf("unquote %s: %v", lit.Value, err)
			return true
		}
		flags = append(flags, "--"+name)
		return true
	})
	sort.Strings(flags)
	return flags
}

// parseRejectCallSiteKeys returns the map keys scan.go hands to
// rejectScoreBearingOutputs.
func parseRejectCallSiteKeys(t *testing.T) []string {
	t.Helper()
	file := parseScanSource(t)

	var keys []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok || fn.Name != "rejectScoreBearingOutputs" || len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.CompositeLit)
		if !ok {
			t.Errorf("rejectScoreBearingOutputs is called with a non-literal map; this test " +
				"cannot check which flags it covers")
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			k, ok := kv.Key.(*ast.BasicLit)
			if !ok || k.Kind != gotoken.STRING {
				continue
			}
			name, err := strconv.Unquote(k.Value)
			if err != nil {
				continue
			}
			keys = append(keys, name)
		}
		return true
	})
	return keys
}

func parseScanSource(t *testing.T) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(gotoken.NewFileSet(), scanSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", scanSourceFile, err)
	}
	return file
}
