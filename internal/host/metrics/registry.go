// Package metrics is a dependency-free metrics registry for the IronClaw control
// plane. It implements just enough of the Prometheus text exposition format
// (counters + histograms) to serve a /metrics endpoint without pulling in the
// Prometheus client library — keeping the host's dependency surface minimal.
//
// The low-level primitives (Registry, Counter, Histogram) live here; the
// pre-wired domain metrics the control plane records (model calls, gateway
// decisions, deliveries, sandbox launches/kills) live in metrics.go.
package metrics

import (
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Label is a single metric label (dimension). Labels on a metric are ordered so
// the exposition output is deterministic.
type Label struct {
	Name  string
	Value string
}

// Counter is a monotonically increasing unsigned counter. It is safe for
// concurrent use.
type Counter struct {
	name   string
	help   string
	labels []Label
	val    atomic.Uint64
}

// Inc adds one.
func (c *Counter) Inc() { c.val.Add(1) }

// Add adds n.
func (c *Counter) Add(n uint64) { c.val.Add(n) }

// Value returns the current count.
func (c *Counter) Value() uint64 { return c.val.Load() }

// Histogram accumulates observations into cumulative buckets plus a running sum
// and count, matching the Prometheus histogram exposition. It is safe for
// concurrent use.
type Histogram struct {
	name    string
	help    string
	buckets []float64 // sorted upper bounds; the implicit +Inf bucket equals count

	mu     sync.Mutex
	counts []uint64 // cumulative count per bucket (len == len(buckets))
	sum    float64
	count  uint64
}

// Observe records a single value.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	for i, ub := range h.buckets {
		if v <= ub {
			h.counts[i]++
		}
	}
	h.sum += v
	h.count++
	h.mu.Unlock()
}

// snapshot returns a consistent copy of the histogram state.
func (h *Histogram) snapshot() (counts []uint64, sum float64, count uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	counts = make([]uint64, len(h.counts))
	copy(counts, h.counts)
	return counts, h.sum, h.count
}

// DefaultLatencyBuckets is a reasonable default for second-valued latencies.
var DefaultLatencyBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// Registry holds registered counters and histograms and renders them in the
// Prometheus text exposition format. Register at construction; reads are
// concurrency-safe.
type Registry struct {
	mu         sync.Mutex
	counters   []*Counter
	histograms []*Histogram
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{} }

// NewCounter registers and returns a counter. The same name may be registered
// more than once with different label sets (one series per call).
func (r *Registry) NewCounter(name, help string, labels ...Label) *Counter {
	c := &Counter{name: name, help: help, labels: labels}
	r.mu.Lock()
	r.counters = append(r.counters, c)
	r.mu.Unlock()
	return c
}

// NewHistogram registers and returns a histogram. If buckets is empty,
// DefaultLatencyBuckets is used. Buckets are sorted ascending.
func (r *Registry) NewHistogram(name, help string, buckets []float64) *Histogram {
	if len(buckets) == 0 {
		buckets = DefaultLatencyBuckets
	}
	bs := make([]float64, len(buckets))
	copy(bs, buckets)
	sort.Float64s(bs)
	h := &Histogram{name: name, help: help, buckets: bs, counts: make([]uint64, len(bs))}
	r.mu.Lock()
	r.histograms = append(r.histograms, h)
	r.mu.Unlock()
	return h
}

// Handler returns an http.Handler that serves the registry in Prometheus text
// exposition format — mount it at /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = r.WriteTo(w)
	})
}

// WriteTo renders all metrics in Prometheus text exposition format, satisfying
// io.WriterTo. Series that share a metric name emit a single HELP/TYPE header
// followed by one line per label set.
func (r *Registry) WriteTo(w io.Writer) (int64, error) {
	r.mu.Lock()
	counters := append([]*Counter(nil), r.counters...)
	histograms := append([]*Histogram(nil), r.histograms...)
	r.mu.Unlock()

	var b strings.Builder

	// Group counters by name, preserving first-seen order.
	order := []string{}
	byName := map[string][]*Counter{}
	for _, c := range counters {
		if _, seen := byName[c.name]; !seen {
			order = append(order, c.name)
		}
		byName[c.name] = append(byName[c.name], c)
	}
	for _, name := range order {
		series := byName[name]
		b.WriteString("# HELP " + name + " " + escapeHelp(series[0].help) + "\n")
		b.WriteString("# TYPE " + name + " counter\n")
		for _, c := range series {
			b.WriteString(name)
			writeLabels(&b, c.labels)
			b.WriteString(" " + strconv.FormatUint(c.Value(), 10) + "\n")
		}
	}

	for _, h := range histograms {
		counts, sum, count := h.snapshot()
		b.WriteString("# HELP " + h.name + " " + escapeHelp(h.help) + "\n")
		b.WriteString("# TYPE " + h.name + " histogram\n")
		for i, ub := range h.buckets {
			b.WriteString(h.name + "_bucket{le=\"" + formatFloat(ub) + "\"} " + strconv.FormatUint(counts[i], 10) + "\n")
		}
		b.WriteString(h.name + "_bucket{le=\"+Inf\"} " + strconv.FormatUint(count, 10) + "\n")
		b.WriteString(h.name + "_sum " + formatFloat(sum) + "\n")
		b.WriteString(h.name + "_count " + strconv.FormatUint(count, 10) + "\n")
	}

	n, err := io.WriteString(w, b.String())
	return int64(n), err
}

// writeLabels appends a Prometheus label block {k="v",...} when labels are present.
func writeLabels(b *strings.Builder, labels []Label) {
	if len(labels) == 0 {
		return
	}
	b.WriteByte('{')
	for i, l := range labels {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(l.Name)
		b.WriteString(`="`)
		b.WriteString(escapeLabelValue(l.Value))
		b.WriteByte('"')
	}
	b.WriteByte('}')
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// escapeLabelValue escapes backslash, double-quote, and newline per the exposition format.
func escapeLabelValue(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(s)
}

// escapeHelp escapes backslash and newline in HELP text per the exposition format.
func escapeHelp(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	return r.Replace(s)
}
