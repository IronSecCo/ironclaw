package metrics

import (
	"io"
	"strings"
	"sync"
	"testing"
)

func render(r *Registry) string {
	var b strings.Builder
	_, _ = r.WriteTo(&b)
	return b.String()
}

// Registry satisfies io.WriterTo.
var _ io.WriterTo = (*Registry)(nil)

func TestCounter(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("ironclaw_things_total", "Things.")
	c.Inc()
	c.Add(4)
	if got := c.Value(); got != 5 {
		t.Fatalf("counter value = %d, want 5", got)
	}
	out := render(r)
	for _, want := range []string{
		"# HELP ironclaw_things_total Things.",
		"# TYPE ironclaw_things_total counter",
		"ironclaw_things_total 5",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("exposition missing %q in:\n%s", want, out)
		}
	}
}

func TestLabeledSeriesShareHeader(t *testing.T) {
	r := NewRegistry()
	a := r.NewCounter("ironclaw_decisions_total", "Decisions.", Label{"outcome", "approved"})
	d := r.NewCounter("ironclaw_decisions_total", "Decisions.", Label{"outcome", "rejected"})
	a.Inc()
	a.Inc()
	d.Inc()
	out := render(r)

	if n := strings.Count(out, "# TYPE ironclaw_decisions_total counter"); n != 1 {
		t.Fatalf("expected exactly one TYPE line for the grouped metric, got %d:\n%s", n, out)
	}
	for _, want := range []string{
		`ironclaw_decisions_total{outcome="approved"} 2`,
		`ironclaw_decisions_total{outcome="rejected"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("exposition missing %q in:\n%s", want, out)
		}
	}
}

func TestHistogramExposition(t *testing.T) {
	r := NewRegistry()
	// Bounds + observations chosen to be exactly representable in float64 so the
	// rendered sum is deterministic (0.25 + 0.75 == 1.0 exactly).
	h := r.NewHistogram("ironclaw_latency_seconds", "Latency.", []float64{0.1, 0.5, 1})
	h.Observe(0.25)
	h.Observe(0.75)
	out := render(r)

	for _, want := range []string{
		"# TYPE ironclaw_latency_seconds histogram",
		`ironclaw_latency_seconds_bucket{le="0.1"} 0`,  // neither obs <= 0.1
		`ironclaw_latency_seconds_bucket{le="0.5"} 1`,  // only 0.25
		`ironclaw_latency_seconds_bucket{le="1"} 2`,    // both
		`ironclaw_latency_seconds_bucket{le="+Inf"} 2`, // both
		"ironclaw_latency_seconds_sum 1",
		"ironclaw_latency_seconds_count 2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("exposition missing %q in:\n%s", want, out)
		}
	}
}

func TestHistogramDefaultBucketsAndSorting(t *testing.T) {
	r := NewRegistry()
	// Unsorted input must be sorted; empty would fall back to defaults.
	h := r.NewHistogram("ironclaw_h_seconds", "H.", []float64{0.1, 0.01, 0.05})
	h.Observe(0.02)
	out := render(r)
	want := []string{
		`ironclaw_h_seconds_bucket{le="0.01"} 0`,
		`ironclaw_h_seconds_bucket{le="0.05"} 1`,
		`ironclaw_h_seconds_bucket{le="0.1"} 1`,
	}
	// Ensure buckets render in ascending order.
	idx := -1
	for _, w := range want {
		i := strings.Index(out, w)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
		if i < idx {
			t.Fatalf("buckets not ascending; %q out of order in:\n%s", w, out)
		}
		idx = i
	}
}

func TestEscaping(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("ironclaw_x_total", "help with \\ and \nnewline", Label{"path", `a"b\c` + "\n"})
	c.Inc()
	out := render(r)
	if !strings.Contains(out, `ironclaw_x_total{path="a\"b\\c\n"} 1`) {
		t.Fatalf("label value not escaped in:\n%s", out)
	}
	if !strings.Contains(out, `# HELP ironclaw_x_total help with \\ and \nnewline`) {
		t.Fatalf("help text not escaped in:\n%s", out)
	}
}

func TestConcurrentRecording(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("ironclaw_c_total", "C.")
	h := r.NewHistogram("ironclaw_d_seconds", "D.", []float64{1, 2})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Inc()
				h.Observe(0.5)
			}
		}()
	}
	wg.Wait()
	if got := c.Value(); got != 5000 {
		t.Fatalf("counter = %d, want 5000", got)
	}
	if !strings.Contains(render(r), "ironclaw_d_seconds_count 5000") {
		t.Fatalf("histogram count != 5000:\n%s", render(r))
	}
}
