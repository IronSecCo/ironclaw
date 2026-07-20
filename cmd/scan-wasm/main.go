// Command scan-wasm compiles IronClaw's pure containment scorer to WebAssembly
// so the exact same 7-dimension grade the `ironctl scan` CLI produces can run
// in-browser — no install, no auth, and the pasted content never leaves the
// page (it is graded entirely client-side).
//
// It is a thin adapter over the pure internal/host/scan package: it registers a
// single JS-callable function `ironclawScan(kind, text, service)` on globalThis
// and returns a JSON string. NO grading logic is reimplemented here — the WASM
// binary calls scan.Score / scan.ScoreDockerfile / scan.Remediate directly, so
// the web result can never drift from the CLI.
//
// Build (from repo root):
//
//	GOOS=js GOARCH=wasm go build -o scan.wasm ./cmd/scan-wasm
//
// The build constraint keeps this file out of the normal host build (it imports
// syscall/js, which only compiles under js/wasm).
//
//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"syscall/js"

	"github.com/IronSecCo/ironclaw/internal/host/scan"
	"github.com/IronSecCo/ironclaw/internal/version"

	"gopkg.in/yaml.v3"
)

// result is one graded workload plus everything the page needs to render it and
// build a shareable receipt. Report carries the 0-100 score, A-F grade and the
// per-dimension pass/fail table; Remediation holds the one-line fix hint per
// non-PASS dimension; the Share* URLs reuse the CLI's own renderers so the web
// share link and the CLI share link are byte-identical for the same posture.
type result struct {
	Service     string             `json:"service"`
	Report      scan.Report        `json:"report"`
	Remediation []scan.Remediation `json:"remediation"`
	ShareCard   string             `json:"shareCardUrl"`
	ShareBadge  string             `json:"shareBadgeUrl"`
	ShareReceipt string            `json:"shareReceiptUrl"`
}

// response is the full JSON payload handed back to the page. results holds one
// entry per graded workload (a Dockerfile or k8s manifest yields exactly one; a
// multi-service compose file yields one per service). PrimaryIndex points at the
// weakest — the headline grade, matching the CLI's weakest-rollup convention.
type response struct {
	OK           bool     `json:"ok"`
	Error        string   `json:"error,omitempty"`
	DetectedKind string   `json:"detectedKind"`
	Results      []result `json:"results,omitempty"`
	PrimaryIndex int      `json:"primaryIndex"`
}

// detectKind classifies pasted text as a Dockerfile, docker-compose file, or
// Kubernetes manifest. It is best-effort and only used when the caller passes
// kind "auto" (an explicit kind always wins). k8s and compose are both YAML, so
// order matters: a `kind:`+`apiVersion:` pair is unambiguously k8s; a top-level
// `services:` map is compose; a `FROM` instruction is a Dockerfile.
func detectKind(text string) string {
	lower := strings.ToLower(text)
	hasAPIVersion := lineStartsWith(lower, "apiversion:")
	hasKind := lineStartsWith(lower, "kind:")
	if hasAPIVersion && hasKind {
		return "k8s"
	}
	if lineStartsWith(lower, "services:") {
		return "compose"
	}
	if lineStartsWith(lower, "from ") {
		return "dockerfile"
	}
	// Fall back on remaining YAML signals before giving up.
	if hasAPIVersion || hasKind {
		return "k8s"
	}
	if strings.Contains(lower, "services:") {
		return "compose"
	}
	return "dockerfile"
}

// lineStartsWith reports whether any line of s (after trimming leading spaces)
// begins with prefix. Used for top-level YAML/Dockerfile key detection.
func lineStartsWith(s, prefix string) bool {
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimLeft(ln, " \t"), prefix) {
			return true
		}
	}
	return false
}

// gradeOne builds a full result from a graded Report + its source Spec. The Spec
// is nil for the Dockerfile path (its scorer takes a DockerfileSpec, not a Spec,
// and has no Spec-based remediation), so remediation is only attached when a Spec
// is available (compose / k8s).
func gradeOne(service string, r scan.Report, s *scan.Spec) result {
	r.Version = version.String()
	res := result{
		Service:      service,
		Report:       r,
		ShareCard:    scan.ShareCardURL(r),
		ShareBadge:   scan.ShareBadgeURL(r),
		ShareReceipt: scan.ShareReceiptURL(r),
	}
	if s != nil {
		res.Remediation = scan.Remediate(*s, r).Items
	}
	return res
}

// composeServiceNames returns the service keys declared in a compose file, in
// stable sorted order, so a multi-service paste can be graded service-by-service.
func composeServiceNames(raw []byte) ([]string, error) {
	var cf struct {
		Services map[string]yaml.Node `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &cf); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	names := make([]string, 0, len(cf.Services))
	for n := range cf.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// scanText is the core: it grades pasted text and returns the JSON response. kind
// is "auto" | "dockerfile" | "compose" | "k8s"; service optionally selects a
// single compose service (empty = grade every service).
func scanText(kind, text, service string) response {
	text = strings.TrimSpace(text)
	if text == "" {
		return response{OK: false, Error: "paste a Dockerfile, docker-compose.yml, or Kubernetes manifest to grade"}
	}
	if kind == "" || kind == "auto" {
		kind = detectKind(text)
	}
	raw := []byte(text)

	switch kind {
	case "dockerfile":
		spec, err := scan.SpecFromDockerfile(raw, "Dockerfile")
		if err != nil {
			return response{OK: false, DetectedKind: kind, Error: err.Error()}
		}
		return response{OK: true, DetectedKind: kind, PrimaryIndex: 0,
			Results: []result{gradeOne("Dockerfile", scan.ScoreDockerfile(spec), nil)}}

	case "k8s":
		spec, err := scan.SpecFromK8s(raw)
		if err != nil {
			return response{OK: false, DetectedKind: kind, Error: err.Error()}
		}
		return response{OK: true, DetectedKind: kind, PrimaryIndex: 0,
			Results: []result{gradeOne(spec.Target, scan.Score(spec), &spec)}}

	case "compose":
		names, err := composeServiceNames(raw)
		if err != nil {
			return response{OK: false, DetectedKind: kind, Error: err.Error()}
		}
		if len(names) == 0 {
			return response{OK: false, DetectedKind: kind, Error: "compose file declares no services"}
		}
		if service != "" {
			names = []string{service}
		}
		var results []result
		for _, n := range names {
			spec, err := scan.SpecFromCompose(raw, n)
			if err != nil {
				return response{OK: false, DetectedKind: kind, Error: err.Error()}
			}
			results = append(results, gradeOne(n, scan.Score(spec), &spec))
		}
		return response{OK: true, DetectedKind: kind, Results: results, PrimaryIndex: weakest(results)}

	default:
		return response{OK: false, Error: fmt.Sprintf("unknown kind %q (want auto|dockerfile|compose|k8s)", kind)}
	}
}

// weakest returns the index of the lowest-scoring result — the headline grade a
// multi-service compose file rolls up to (a stack is only as contained as its
// weakest workload).
func weakest(rs []result) int {
	idx := 0
	for i, r := range rs {
		if r.Report.Score < rs[idx].Report.Score {
			idx = i
		}
	}
	return idx
}

// ironclawScan is the JS entry point: ironclawScan(kind, text, service) -> JSON
// string. It never throws into JS; all errors are reported in the JSON payload.
func ironclawScan(_ js.Value, args []js.Value) any {
	kind, text, service := "auto", "", ""
	if len(args) > 0 {
		kind = args[0].String()
	}
	if len(args) > 1 {
		text = args[1].String()
	}
	if len(args) > 2 && args[2].Type() == js.TypeString {
		service = args[2].String()
	}
	out, err := json.Marshal(scanText(kind, text, service))
	if err != nil {
		b, _ := json.Marshal(response{OK: false, Error: "internal: " + err.Error()})
		return string(b)
	}
	return string(out)
}

func main() {
	js.Global().Set("ironclawScan", js.FuncOf(ironclawScan))
	js.Global().Set("ironclawScanReady", js.ValueOf(true))
	// Block forever: the exported function must stay callable for the page's
	// lifetime (a returning main() would tear the instance down).
	select {}
}
