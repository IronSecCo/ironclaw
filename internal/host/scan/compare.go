package scan

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Side identifies which of the two compared targets won a dimension (or the
// overall grade). "tie" is a real outcome: two equally-hardened targets.
type Side string

const (
	SideA   Side = "A"
	SideB   Side = "B"
	SideTie Side = "tie"
)

// DimensionDelta is the per-dimension diff between the two compared reports. Both
// sides share the same scorer set, so Key/Title/Max are common; only the earned
// score and verdict differ.
type DimensionDelta struct {
	Key      string  `json:"key"`
	Title    string  `json:"title"`
	Max      int     `json:"max"`
	AScore   int     `json:"aScore"`
	BScore   int     `json:"bScore"`
	AVerdict Verdict `json:"aVerdict"`
	BVerdict Verdict `json:"bVerdict"`
	// Winner is the side that scored higher on THIS dimension (SideTie if equal).
	Winner Side `json:"winner"`
}

// Comparison is the full side-by-side diff of two scorecards. It is derived
// purely from two Reports: no I/O, deterministic for a given pair.
type Comparison struct {
	A          Report           `json:"a"`
	B          Report           `json:"b"`
	Dimensions []DimensionDelta `json:"dimensions"`
	// ScoreDelta is A.Score - B.Score (positive means A is more hardened).
	ScoreDelta int `json:"scoreDelta"`
	// Winner is the more-hardened side overall (higher total score; SideTie if
	// the totals match).
	Winner Side `json:"winner"`
	// Verdict is a one-line human summary naming the winner and why.
	Verdict string `json:"verdict"`
}

// Compare diffs two scorecards dimension-by-dimension and picks an overall
// winner. It aligns dimensions by Key (both reports come from the same scorer
// set, so the order already matches, but aligning by key is robust to either
// report being built differently). Ordering follows the canonical scorer order
// for deterministic output.
func Compare(a, b Report) Comparison {
	bByKey := make(map[string]Dimension, len(b.Dimensions))
	for _, d := range b.Dimensions {
		bByKey[d.Key] = d
	}

	c := Comparison{A: a, B: b, ScoreDelta: a.Score - b.Score}
	for _, da := range a.Dimensions {
		db := bByKey[da.Key]
		c.Dimensions = append(c.Dimensions, DimensionDelta{
			Key:      da.Key,
			Title:    da.Title,
			Max:      da.Max,
			AScore:   da.Score,
			BScore:   db.Score,
			AVerdict: da.Verdict,
			BVerdict: db.Verdict,
			Winner:   sideOf(da.Score, db.Score),
		})
	}

	c.Winner = sideOf(a.Score, b.Score)
	c.Verdict = compareVerdict(c)
	return c
}

// sideOf returns which side scored higher (SideTie on equality).
func sideOf(aScore, bScore int) Side {
	switch {
	case aScore > bScore:
		return SideA
	case bScore > aScore:
		return SideB
	default:
		return SideTie
	}
}

// compareVerdict builds the one-line summary: who is more hardened, by how much,
// and the dimensions the winner leads on. The lead list is ordered by point gap
// (largest first), tie-broken by canonical dimension order, and capped so the
// line stays short.
func compareVerdict(c Comparison) string {
	winLabel := func(s Side) string {
		switch s {
		case SideA:
			return fmt.Sprintf("A (`%s`)", nz(c.A.Target, "A"))
		case SideB:
			return fmt.Sprintf("B (`%s`)", nz(c.B.Target, "B"))
		default:
			return "neither"
		}
	}

	if c.Winner == SideTie {
		return fmt.Sprintf("Even match: both score %d/100 (grade %s). No isolation-posture difference at this granularity.",
			c.A.Score, c.A.Grade)
	}

	delta := c.ScoreDelta
	if delta < 0 {
		delta = -delta
	}
	win, lose := c.A, c.B
	if c.Winner == SideB {
		win, lose = c.B, c.A
	}

	leads := winnerLeads(c)
	why := ""
	if len(leads) > 0 {
		why = "; it leads on " + humanJoin(leads) + "."
	} else {
		why = "."
	}
	return fmt.Sprintf("%s is more hardened: %d/100 (grade %s) vs %d/100 (grade %s), %s %d-point lead%s",
		winLabel(c.Winner), win.Score, win.Grade, lose.Score, lose.Grade, article(delta), delta, why)
}

// winnerLeads returns the titles of the dimensions the overall winner strictly
// leads on, ordered by point gap (desc) then canonical order, capped to keep the
// verdict readable.
func winnerLeads(c Comparison) []string {
	const maxLeads = 3
	type lead struct {
		title string
		gap   int
		order int
	}
	var leads []lead
	for i, d := range c.Dimensions {
		gap := d.AScore - d.BScore
		if c.Winner == SideB {
			gap = -gap
		}
		if gap > 0 {
			leads = append(leads, lead{title: d.Title, gap: gap, order: i})
		}
	}
	sort.SliceStable(leads, func(i, j int) bool {
		if leads[i].gap != leads[j].gap {
			return leads[i].gap > leads[j].gap
		}
		return leads[i].order < leads[j].order
	})

	titles := make([]string, 0, len(leads))
	for _, l := range leads {
		titles = append(titles, l.title)
	}
	if len(titles) > maxLeads {
		extra := len(titles) - maxLeads
		titles = titles[:maxLeads]
		titles = append(titles, fmt.Sprintf("and %d more", extra))
	}
	return titles
}

// article returns the indefinite article ("a"/"an") that reads correctly before
// the spoken form of n. Only the leading digit governs the vowel sound: 8, 11,
// and 18 read with a leading vowel ("an eight", "an eleven", "an eighteen").
func article(n int) string {
	if n < 0 {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	switch {
	case s == "11" || s == "18" || strings.HasPrefix(s, "8"):
		return "an"
	default:
		return "a"
	}
}

// humanJoin joins items with commas and a trailing "and". "and N more" (already
// carrying its own conjunction) is appended verbatim.
func humanJoin(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", " + items[len(items)-1]
	}
}

// --------------------------------------------------------------------------- //
// Renderers. Table (default), JSON (--json), Markdown (--md). All three reuse
// the single-scan glyph/color helpers so the surfaces stay consistent.
// --------------------------------------------------------------------------- //

// RenderComparisonTable writes the human-readable side-by-side scorecard to w.
func RenderComparisonTable(w io.Writer, c Comparison) {
	fmt.Fprintf(w, "IronClaw containment scan: comparison\n\n")
	fmt.Fprintf(w, "  A: %s (%s)  %d/%d grade %s\n", nz(c.A.Target, "?"), nz(c.A.Source, "?"), c.A.Score, c.A.Max, c.A.Grade)
	fmt.Fprintf(w, "  B: %s (%s)  %d/%d grade %s\n\n", nz(c.B.Target, "?"), nz(c.B.Source, "?"), c.B.Score, c.B.Max, c.B.Grade)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DIMENSION\tA\tB\tWINNER")
	for _, d := range c.Dimensions {
		fmt.Fprintf(tw, "%s\t%s %d/%d\t%s %d/%d\t%s\n",
			d.Title,
			verdictGlyph(d.AVerdict), d.AScore, d.Max,
			verdictGlyph(d.BVerdict), d.BScore, d.Max,
			winnerMark(d.Winner))
	}
	fmt.Fprintf(tw, "OVERALL\t%d/%d %s\t%d/%d %s\t%s\n",
		c.A.Score, c.A.Max, c.A.Grade, c.B.Score, c.B.Max, c.B.Grade, overallMark(c))
	tw.Flush()

	fmt.Fprintf(w, "\n  Verdict: %s\n", c.Verdict)
	fmt.Fprintf(w, "\n  Harden your sandbox: https://ironsecco.github.io/ironclaw/scan/\n")
}

// winnerMark renders the per-dimension winner column.
func winnerMark(s Side) string {
	switch s {
	case SideA:
		return "< A"
	case SideB:
		return "B >"
	default:
		return "= tie"
	}
}

// overallMark renders the OVERALL winner cell with the signed point delta.
func overallMark(c Comparison) string {
	if c.Winner == SideTie {
		return "= tie"
	}
	delta := c.ScoreDelta
	if delta < 0 {
		delta = -delta
	}
	return fmt.Sprintf("%s (+%d)", c.Winner, delta)
}

// RenderComparisonJSON writes the machine-readable comparison (schemaVersion
// 1.0). It reuses the single-scan Report marshaling for each side, so consumers
// that already parse a scan report get the same shape under "a" and "b".
func RenderComparisonJSON(w io.Writer, c Comparison) error {
	m := map[string]any{
		"schemaVersion": "1.0",
		"report":        "ironclaw-containment-comparison",
		"a":             c.A,
		"b":             c.B,
		"dimensions":    c.Dimensions,
		"scoreDelta":    c.ScoreDelta,
		"winner":        c.Winner,
		"verdict":       c.Verdict,
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(out, '\n'))
	return err
}

// RenderComparisonMarkdown returns a shareable markdown block directly
// embeddable in a blog post or README (the data engine behind "X vs Y container
// isolation" comparison pages). Public-copy house style: no em/en-dashes.
func RenderComparisonMarkdown(c Comparison) string {
	var b strings.Builder
	aName := nz(c.A.Target, "A")
	bName := nz(c.B.Target, "B")
	fmt.Fprintf(&b, "### IronClaw containment scan: `%s` vs `%s`\n\n", aName, bName)
	fmt.Fprintf(&b, "| Dimension | `%s` | `%s` | Winner |\n|---|---|---|---|\n", aName, bName)
	for _, d := range c.Dimensions {
		fmt.Fprintf(&b, "| %s | %s %d/%d | %s %d/%d | %s |\n",
			d.Title,
			mdGlyph(d.AVerdict), d.AScore, d.Max,
			mdGlyph(d.BVerdict), d.BScore, d.Max,
			mdWinner(d.Winner))
	}
	fmt.Fprintf(&b, "| **Overall** | **%d/100 (%s)** | **%d/100 (%s)** | **%s** |\n",
		c.A.Score, c.A.Grade, c.B.Score, c.B.Grade, mdOverall(c))
	fmt.Fprintf(&b, "\n**Verdict:** %s\n", c.Verdict)
	fmt.Fprintf(&b, "\nAudit your own sandbox: `ironctl scan <container>` (https://github.com/IronSecCo/ironclaw)\n")
	return b.String()
}

// mdWinner renders the per-dimension winner cell in markdown.
func mdWinner(s Side) string {
	switch s {
	case SideA:
		return "A"
	case SideB:
		return "B"
	default:
		return "tie"
	}
}

// mdOverall renders the OVERALL winner cell in markdown with the point delta.
func mdOverall(c Comparison) string {
	if c.Winner == SideTie {
		return "tie"
	}
	delta := c.ScoreDelta
	if delta < 0 {
		delta = -delta
	}
	return fmt.Sprintf("%s (+%d)", c.Winner, delta)
}
