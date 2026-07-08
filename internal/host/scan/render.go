package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// scanDocsURL is the published hardening guide, used as the SARIF driver's
// informationUri and the fallback remediation pointer.
const scanDocsURL = "https://ironsecco.github.io/ironclaw/scan/"

// RenderTable writes the human-readable scorecard to w: a header line with the
// overall score + grade, then one row per dimension.
func RenderTable(w io.Writer, r Report) {
	fmt.Fprintf(w, "IronClaw containment scan\n")
	fmt.Fprintf(w, "  target:  %s (%s)\n", nz(r.Target, "?"), nz(r.Source, "?"))
	if r.Runtime != "" {
		fmt.Fprintf(w, "  runtime: %s\n", r.Runtime)
	}
	if r.HardenedRuntime != "" {
		fmt.Fprintf(w, "  isolation: %s (hardened runtime; informational, no score bonus)\n", r.HardenedRuntime)
	}
	fmt.Fprintf(w, "  score:   %d/%d  grade %s  %s\n\n", r.Score, r.Max, r.Grade, gradeBanner(r.Grade))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DIMENSION\tVERDICT\tSCORE\tDETAIL")
	for _, d := range r.Dimensions {
		fmt.Fprintf(tw, "%s\t%s %s\t%d/%d\t%s\n",
			d.Title, verdictGlyph(d.Verdict), d.Verdict, d.Score, d.Max, d.Detail)
	}
	tw.Flush()

	if len(r.Notes) > 0 {
		fmt.Fprintln(w)
		for _, n := range r.Notes {
			fmt.Fprintf(w, "  note: %s\n", n)
		}
	}
	fmt.Fprintf(w, "\n  Harden your sandbox: https://ironsecco.github.io/ironclaw/scan/\n")
}

func gradeBanner(g string) string {
	switch g {
	case "A":
		return "(hardened)"
	case "B":
		return "(solid, minor gaps)"
	case "C":
		return "(weak, fix the FAILs)"
	case "D":
		return "(porous)"
	default:
		return "(wide open)"
	}
}

func verdictGlyph(v Verdict) string {
	switch v {
	case VerdictPass:
		return "[+]"
	case VerdictWarn:
		return "[~]"
	default: // FAIL / UNKNOWN
		return "[x]"
	}
}

// RenderJSON writes the machine-readable report (schemaVersion 1.0). When a
// non-nil RemediationPlan is passed (from `--fix`), it is spliced in under the
// "remediation" key so the JSON carries the prescriptive fixes too — fail-closed
// parity with the human output.
func RenderJSON(w io.Writer, r Report, plan ...*RemediationPlan) error {
	// json does not honour ",inline"; marshal the report and splice fields in.
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	m["schemaVersion"] = "1.0"
	m["report"] = "ironclaw-containment-scan"
	if len(plan) > 0 && plan[0] != nil {
		m["remediation"] = plan[0]
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(out, '\n'))
	return err
}

// RenderMarkdown returns a shareable markdown block (a README/blog section): the
// headline score plus a dimension table. Public-copy house style: no em/en-dashes.
func RenderMarkdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### IronClaw containment scan: `%s` scored **%d/100 (grade %s)**\n\n",
		nz(r.Target, "target"), r.Score, r.Grade)
	fmt.Fprintf(&b, "| Dimension | Verdict | Score |\n|---|---|---|\n")
	for _, d := range r.Dimensions {
		fmt.Fprintf(&b, "| %s | %s %s | %d/%d |\n", d.Title, mdGlyph(d.Verdict), d.Verdict, d.Score, d.Max)
	}
	fmt.Fprintf(&b, "\nAudit your own sandbox: `ironctl scan <container>` (https://github.com/IronSecCo/ironclaw)\n")
	return b.String()
}

func mdGlyph(v Verdict) string {
	switch v {
	case VerdictPass:
		return "✅"
	case VerdictWarn:
		return "⚠️"
	default:
		return "❌"
	}
}

// RenderBadgeSVG returns a self-contained, shields.io-style flat badge (no
// external fetch), colored by grade. Safe to commit into a README. Deterministic
// given the report score, matching the IRO-367 receipt badge conventions.
func RenderBadgeSVG(r Report) string {
	right := fmt.Sprintf("%d/100 %s", r.Score, r.Grade)
	label := "containment"
	// Rough monospace sizing (7px/char + padding), same math as emit-receipt.sh.
	llw := len(label)*7 + 22
	lw := len(right)*7 + 22
	totalW := llw + lw
	color := gradeColor(r.Grade)
	llx := llw * 10 / 2
	rx := llw*10 + lw*10/2
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
  <title>%s: %s</title>
  <linearGradient id="s" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="%d" height="20" fill="#24292f"/>
    <rect x="%d" width="%d" height="20" fill="%s"/>
    <rect width="%d" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="110" text-rendering="geometricPrecision">
    <text x="%d" y="150" transform="scale(.1)" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="140" transform="scale(.1)">%s</text>
    <text x="%d" y="150" transform="scale(.1)" fill="#010101" fill-opacity=".3">%s</text>
    <text x="%d" y="140" transform="scale(.1)">%s</text>
  </g>
</svg>
`, totalW, label, right, label, right, totalW, llw, llw, lw, color, totalW,
		llx, label, llx, label, rx, right, rx, right)
}

// BadgeEndpoint is the shields.io endpoint response schema (schemaVersion 1).
// See https://shields.io/badges/endpoint-badge.
type BadgeEndpoint struct {
	SchemaVersion int    `json:"schemaVersion"`
	Label         string `json:"label"`
	Message       string `json:"message"`
	Color         string `json:"color"`
}

// RenderBadgeEndpointJSON returns the shields.io endpoint JSON for r. Commit the
// output into your repo and point a shields endpoint badge at its raw URL to get
// a live, self-updating Sandbox Isolation Score badge in your README:
//
//	![Sandbox Isolation](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/OWNER/REPO/BRANCH/PATH.json)
//
// The score is pinned at generation time; this file triggers no live scan (no
// server-side DoS surface). Regenerate with `ironctl scan --badge-json` whenever
// your container config changes.
func RenderBadgeEndpointJSON(r Report) string {
	b := BadgeEndpoint{
		SchemaVersion: 1,
		Label:         "sandbox isolation",
		Message:       fmt.Sprintf("%d/100 %s", r.Score, r.Grade),
		// shields accepts a bare 6-hex color; drop the leading '#'. Same palette
		// as the committed SVG so both surfaces agree.
		Color: strings.TrimPrefix(gradeColor(r.Grade), "#"),
	}
	out, _ := json.MarshalIndent(b, "", "  ")
	return string(out) + "\n"
}

// --------------------------------------------------------------------------- //
// SARIF 2.1.0 — GitHub code-scanning integration.
//
// RenderSARIF emits a Static Analysis Results Interchange Format log so failed
// isolation dimensions surface in a repo's Security > Code scanning tab (upload
// via github/codeql-action/upload-sarif). One rule per graded dimension; one
// result per non-PASS dimension. A clean 100/A target produces zero results.
// --------------------------------------------------------------------------- //

// SARIFOptions carries the source-location context RenderSARIF needs to anchor
// results at the scanned artifact. Both fields are optional: with no ConfigFile
// (a live container scan) results anchor at a logical location naming the
// workload; with ConfigFile but AnchorLine 0 they anchor at file level.
type SARIFOptions struct {
	// ConfigFile is the repo-relative path to the scanned config (a
	// docker-compose.yml, k8s manifest, or posture file). "" for a live
	// container scan, which has no file to point at.
	ConfigFile string
	// AnchorLine is the 1-based line the results point at (the workload's
	// declaration), when derivable. 0 means file-level.
	AnchorLine int
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	ShortDescription     sarifText       `json:"shortDescription"`
	FullDescription      sarifText       `json:"fullDescription"`
	Help                 sarifMsg        `json:"help"`
	DefaultConfiguration sarifRuleConfig `json:"defaultConfiguration"`
	Properties           sarifRuleProps  `json:"properties,omitempty"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifMsg struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown,omitempty"`
}

type sarifRuleConfig struct {
	Level string `json:"level"`
}

type sarifRuleProps struct {
	Tags []string `json:"tags,omitempty"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation *sarifPhysical `json:"physicalLocation,omitempty"`
	LogicalLocations []sarifLogical `json:"logicalLocations,omitempty"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

type sarifLogical struct {
	Name               string `json:"name"`
	FullyQualifiedName string `json:"fullyQualifiedName,omitempty"`
}

// RenderSARIF writes a SARIF 2.1.0 log for report r (graded from spec s) to w.
// Every graded dimension becomes a rule (with remediation reused from
// remediate.go as help text); every non-PASS dimension becomes a result whose
// level derives from the dimension's severity, anchored at the scanned config
// via opts. Deterministic for a given (report, spec, opts): no clock, no
// randomness, and fingerprints are stable per (rule, file) so GitHub dedupes
// findings across runs.
func RenderSARIF(w io.Writer, r Report, s Spec, opts SARIFOptions) error {
	driver := sarifDriver{
		Name:           "ironctl-scan",
		Version:        r.Version,
		InformationURI: scanDocsURL,
	}
	ruleIndex := make(map[string]int, len(scorers))
	for i, sc := range scorers {
		ruleIndex[sc.key] = i
		fix, expl := dimFix(s, sc.key)
		driver.Rules = append(driver.Rules, sarifRule{
			ID:               sc.key,
			Name:             sarifRuleName(sc.key),
			ShortDescription: sarifText{Text: sc.title},
			FullDescription:  sarifText{Text: expl},
			Help: sarifMsg{
				Text:     fmt.Sprintf("%s\n\nFix: %s\nMore: %s", expl, fix, scanDocsURL),
				Markdown: fmt.Sprintf("%s\n\n**Fix:** `%s`\n\n[Hardening guide](%s)", expl, fix, scanDocsURL),
			},
			DefaultConfiguration: sarifRuleConfig{Level: sarifSeverity(sc.max)},
			Properties:           sarifRuleProps{Tags: []string{"security", "containment", "ironclaw"}},
		})
	}

	// results must marshal as [] (not null) even when empty: SARIF requires the
	// array present, and a clean 100/A scan legitimately has zero results.
	results := []sarifResult{}
	for _, d := range r.Dimensions {
		if d.Verdict == VerdictPass {
			continue
		}
		idx := ruleIndex[d.Key]
		fix, _ := dimFix(s, d.Key)
		results = append(results, sarifResult{
			RuleID:    d.Key,
			RuleIndex: idx,
			Level:     sarifResultLevel(d.Verdict, scorers[idx].max),
			Message: sarifText{Text: fmt.Sprintf("%s (%s): %s. Fix: %s",
				d.Title, d.Verdict, d.Detail, fix)},
			Locations:           []sarifLocation{sarifLoc(opts, s)},
			PartialFingerprints: map[string]string{"ironclawScan/v1": sarifFingerprint(d.Key, opts.ConfigFile)},
		})
	}

	out, err := json.MarshalIndent(sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs:    []sarifRun{{Tool: sarifTool{Driver: driver}, Results: results}},
	}, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(out, '\n'))
	return err
}

// sarifLoc builds the location for a result: a physicalLocation at the scanned
// file (with a region when the workload's line is derivable) when a config file
// was scanned, else a logicalLocation naming the container.
func sarifLoc(opts SARIFOptions, s Spec) sarifLocation {
	if opts.ConfigFile != "" {
		pl := &sarifPhysical{ArtifactLocation: sarifArtifact{URI: opts.ConfigFile}}
		if opts.AnchorLine > 0 {
			pl.Region = &sarifRegion{StartLine: opts.AnchorLine}
		}
		return sarifLocation{PhysicalLocation: pl}
	}
	name := nz(s.Target, "container")
	return sarifLocation{LogicalLocations: []sarifLogical{{
		Name:               name,
		FullyQualifiedName: nz(s.Source, "container") + "/" + name,
	}}}
}

// sarifSeverity maps a dimension's weight to a SARIF level: the heavier
// boundaries (>=15 pts, each a full host-compromise primitive when breached)
// are errors, the rest warnings.
func sarifSeverity(maxWeight int) string {
	if maxWeight >= 15 {
		return "error"
	}
	return "warning"
}

// sarifResultLevel is the level for one result: a WARN (partial/weakened)
// posture never escalates past warning; a FAIL/UNKNOWN inherits the rule's
// severity.
func sarifResultLevel(v Verdict, maxWeight int) string {
	if v == VerdictWarn {
		return "warning"
	}
	return sarifSeverity(maxWeight)
}

// sarifRuleName converts a dotted dimension key ("user.nonroot") to a
// PascalCase rule name ("UserNonroot") for the SARIF rule.name field.
func sarifRuleName(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

// sarifFingerprint is a stable partial fingerprint per (rule, file) so GitHub
// dedupes the same finding across runs and only alerts on genuinely new ones.
func sarifFingerprint(ruleID, file string) string {
	sum := sha256.Sum256([]byte(ruleID + "\x00" + file))
	return hex.EncodeToString(sum[:8])
}

// AnchorLine returns the 1-based line number of the first line in raw that
// mentions target (a compose service or k8s container/pod name) so SARIF
// results anchor at the workload's declaration. Returns 0 (file-level) when
// target is empty or not found.
func AnchorLine(raw []byte, target string) int {
	t := strings.TrimSpace(target)
	if t == "" {
		return 0
	}
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, t) {
			return i + 1
		}
	}
	return 0
}

// gradeColor maps a letter grade to the shields flat-badge palette.
func gradeColor(g string) string {
	switch g {
	case "A":
		return "#3fb950" // green
	case "B":
		return "#9acd32" // yellow-green
	case "C":
		return "#d4a72c" // amber
	case "D":
		return "#e8873a" // orange
	default:
		return "#d1242f" // red
	}
}
