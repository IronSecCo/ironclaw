package scan

import (
	"strings"
	"testing"
)

// hardenedSpec grades to 100/A; laxSpec grades near the floor. They give Compare
// a maximally-separated pair to diff.
func hardenedSpec(target string) Spec {
	return Spec{
		Source: "docker", Target: target,
		RunAsNonRoot: Yes, User: "65532",
		CapDropAll:   Yes,
		Seccomp:      "default",
		NetworkMode:  "none",
		ReadonlyRoot: Yes,
		DockerSock:   No,
		HostPID:      No, HostIPC: No, HostNetwork: No,
	}
}

func laxSpec(target string) Spec {
	return Spec{
		Source: "docker", Target: target,
		RunAsNonRoot: No, User: "0",
		CapDropAll:   No,
		Seccomp:      "unconfined",
		NetworkMode:  "host", HostNetwork: Yes,
		ReadonlyRoot: No,
		DockerSock:   Yes,
		HostPID:      Yes,
	}
}

func TestCompareWinnerAndDelta(t *testing.T) {
	a := Score(hardenedSpec("distroless"))
	b := Score(laxSpec("alpine"))

	c := Compare(a, b)

	if a.Score != 100 {
		t.Fatalf("precondition: hardened should be 100, got %d", a.Score)
	}
	if c.Winner != SideA {
		t.Fatalf("winner: got %s want A", c.Winner)
	}
	if c.ScoreDelta != a.Score-b.Score {
		t.Fatalf("scoreDelta: got %d want %d", c.ScoreDelta, a.Score-b.Score)
	}
	if c.ScoreDelta <= 0 {
		t.Fatalf("hardened should lead: delta %d", c.ScoreDelta)
	}
}

func TestCompareDimensionAlignmentAndWinners(t *testing.T) {
	a := Score(hardenedSpec("distroless"))
	b := Score(laxSpec("alpine"))
	c := Compare(a, b)

	if len(c.Dimensions) != len(scorers) {
		t.Fatalf("dimensions: got %d want %d", len(c.Dimensions), len(scorers))
	}
	// Deterministic ordering: comparison dimensions follow the canonical scorer
	// order exactly.
	for i, d := range c.Dimensions {
		if d.Key != scorers[i].key {
			t.Fatalf("order[%d]: got %s want %s", i, d.Key, scorers[i].key)
		}
	}
	// Every dimension: hardened >= lax, and hardened wins the ones it leads.
	for _, d := range c.Dimensions {
		if d.AScore < d.BScore {
			t.Fatalf("%s: hardened %d < lax %d", d.Key, d.AScore, d.BScore)
		}
		if d.AScore > d.BScore && d.Winner != SideA {
			t.Fatalf("%s: A leads but winner=%s", d.Key, d.Winner)
		}
	}
}

func TestCompareTie(t *testing.T) {
	a := Score(hardenedSpec("img-a"))
	b := Score(hardenedSpec("img-b"))
	c := Compare(a, b)

	if c.Winner != SideTie {
		t.Fatalf("winner: got %s want tie", c.Winner)
	}
	if c.ScoreDelta != 0 {
		t.Fatalf("scoreDelta: got %d want 0", c.ScoreDelta)
	}
	if !strings.Contains(c.Verdict, "Even match") {
		t.Fatalf("tie verdict should say even match: %q", c.Verdict)
	}
	// Per-dimension winners are all ties.
	for _, d := range c.Dimensions {
		if d.Winner != SideTie {
			t.Fatalf("%s: winner=%s want tie", d.Key, d.Winner)
		}
	}
}

func TestCompareBWins(t *testing.T) {
	a := Score(laxSpec("alpine"))
	b := Score(hardenedSpec("distroless"))
	c := Compare(a, b)

	if c.Winner != SideB {
		t.Fatalf("winner: got %s want B", c.Winner)
	}
	if c.ScoreDelta >= 0 {
		t.Fatalf("A is lax so delta should be negative, got %d", c.ScoreDelta)
	}
	if !strings.Contains(c.Verdict, "`distroless`") {
		t.Fatalf("verdict should name the winner target: %q", c.Verdict)
	}
}

func TestCompareVerdictNamesLeads(t *testing.T) {
	a := Score(hardenedSpec("distroless"))
	b := Score(laxSpec("alpine"))
	c := Compare(a, b)
	// Highest-weight dimension the winner leads on (caps, 20 pts) should surface
	// in the one-line verdict.
	if !strings.Contains(c.Verdict, "Dropped capabilities") {
		t.Fatalf("verdict should cite the largest-gap lead: %q", c.Verdict)
	}
	if !strings.Contains(c.Verdict, "more hardened") {
		t.Fatalf("verdict phrasing: %q", c.Verdict)
	}
}

func TestArticle(t *testing.T) {
	cases := map[int]string{1: "a", 5: "a", 8: "an", 11: "an", 18: "an", 80: "an", 84: "an", 90: "a", 96: "a", 100: "a"}
	for n, want := range cases {
		if got := article(n); got != want {
			t.Fatalf("article(%d): got %q want %q", n, got, want)
		}
	}
}

func TestRenderComparisonMarkdownDeterministic(t *testing.T) {
	a := Score(hardenedSpec("distroless"))
	b := Score(laxSpec("alpine"))
	c := Compare(a, b)

	md1 := RenderComparisonMarkdown(c)
	md2 := RenderComparisonMarkdown(c)
	if md1 != md2 {
		t.Fatal("markdown render not deterministic")
	}
	for _, want := range []string{
		"`distroless` vs `alpine`",
		"| Dimension |",
		"| **Overall** |",
		"**Verdict:**",
	} {
		if !strings.Contains(md1, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md1)
		}
	}
	// No em/en-dashes in public copy.
	if strings.ContainsAny(md1, "—–") {
		t.Fatalf("markdown contains an em/en-dash: %q", md1)
	}
}

func TestRenderComparisonJSONRoundTrip(t *testing.T) {
	a := Score(hardenedSpec("distroless"))
	b := Score(laxSpec("alpine"))
	c := Compare(a, b)

	var sb strings.Builder
	if err := RenderComparisonJSON(&sb, c); err != nil {
		t.Fatalf("render json: %v", err)
	}
	out := sb.String()
	for _, want := range []string{
		`"report": "ironclaw-containment-comparison"`,
		`"scoreDelta"`,
		`"winner": "A"`,
		`"dimensions"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("json missing %q:\n%s", want, out)
		}
	}
}

func TestRenderComparisonTableHasWinnerColumn(t *testing.T) {
	a := Score(hardenedSpec("distroless"))
	b := Score(laxSpec("alpine"))
	c := Compare(a, b)

	var sb strings.Builder
	RenderComparisonTable(&sb, c)
	out := sb.String()
	for _, want := range []string{"DIMENSION", "WINNER", "OVERALL", "Verdict:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table missing %q:\n%s", want, out)
		}
	}
}
