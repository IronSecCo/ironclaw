package scan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

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
