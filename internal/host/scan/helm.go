package scan

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// workloadKinds is the set of Kubernetes kinds that carry a pod spec worth
// grading. `helm template` emits many non-workload docs (Service, ConfigMap,
// Secret, Ingress, PVC, ServiceAccount, NetworkPolicy, …); those are skipped.
var workloadKinds = map[string]bool{
	"Pod":                   true,
	"Deployment":            true,
	"StatefulSet":           true,
	"DaemonSet":             true,
	"ReplicaSet":            true,
	"ReplicationController": true,
	"Job":                   true,
	"CronJob":               true,
}

// SpecsFromK8sStream parses a multi-document Kubernetes YAML stream (the output
// of `helm template`, or any `---`-separated manifest set) and returns one Spec
// per gradeable workload, in document order. Non-workload docs and docs with no
// containers are skipped. Empty documents are tolerated. It is pure and
// unit-testable: the caller runs the renderer (I/O) and passes the stream here.
func SpecsFromK8sStream(rendered []byte) ([]Spec, error) {
	dec := yaml.NewDecoder(bytes.NewReader(rendered))
	var specs []Spec
	for {
		var obj k8sObject
		err := dec.Decode(&obj)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse rendered manifest stream: %w", err)
		}
		// Only grade recognized workload kinds. A doc whose kind is blank (an
		// empty document between `---` separators) or non-workload is skipped.
		if !workloadKinds[strings.TrimSpace(obj.Kind)] {
			continue
		}
		ps, ok := obj.podSpecOf()
		if !ok {
			continue
		}
		s := specFromPodSpec("helm", workloadTarget(obj), ps)
		specs = append(specs, s)
	}
	return specs, nil
}

// workloadTarget builds a stable, human-readable target label for a workload:
// "<kind>/<name>" so an aggregate report and SARIF can name exactly which
// rendered workload each finding came from.
func workloadTarget(obj k8sObject) string {
	kind := strings.TrimSpace(obj.Kind)
	name := strings.TrimSpace(obj.Metadata.Name)
	switch {
	case kind != "" && name != "":
		return kind + "/" + name
	case name != "":
		return name
	default:
		return kind
	}
}

// AggregateHelm folds the per-workload specs of a rendered chart into a single
// Report representing the chart's isolation posture. The aggregate is the
// WEAKEST workload (minimum score): a chart is only as isolated as its
// most-exposed pod, so grading the weakest link is the honest, fail-closed
// summary. It returns the aggregate Report and the Spec that produced it (for
// SARIF anchoring). chart names the source chart for the report target. It is
// pure; the caller injects Version/GeneratedAt. Fail-closed: an empty workload
// set is an error (a chart that renders no gradeable workload is not a pass).
func AggregateHelm(specs []Spec, chart string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable workloads found in rendered chart (no Pod/Deployment/StatefulSet/DaemonSet/Job/CronJob with containers)")
	}

	// Score every workload, then pick the weakest. Ties resolve to the first in
	// document order so the choice is deterministic.
	all := make([]scoredWorkload, len(specs))
	worst := 0
	for i, s := range specs {
		all[i] = scoredWorkload{spec: s, report: Score(s)}
		if all[i].report.Score < all[worst].report.Score {
			worst = i
		}
	}

	agg := all[worst].report
	worstSpec := all[worst].spec
	// Re-target the aggregate at the chart while keeping the weakest workload's
	// identity visible in the notes below.
	if strings.TrimSpace(chart) != "" {
		agg.Target = chart
	}
	agg.Source = "helm"

	// Prepend a chart-level summary so the human/JSON output is honest about what
	// was aggregated: how many workloads, which one set the grade, and the full
	// per-workload roll-up. This mirrors the --dockerfile honesty pattern.
	agg.Notes = append(chartSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// scoredWorkload pairs a rendered workload's Spec with its Report.
type scoredWorkload struct {
	spec   Spec
	report Report
}

// chartSummaryNotes builds the chart-level roll-up notes for an aggregate helm
// report: a headline naming the weakest workload and a per-workload score list.
func chartSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d rendered workload(s); the chart grade is the WEAKEST (a chart is only as isolated as its most-exposed pod). Weakest: %s at %d/100 (grade %s).",
			len(all), nz(worst.spec.Target, "workload"), worst.report.Score, worst.report.Grade),
	}
	// Per-workload roll-up, sorted worst-first for a quick scan.
	sorted := make([]scoredWorkload, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].report.Score < sorted[j].report.Score })
	rows := make([]string, len(sorted))
	for i, w := range sorted {
		rows[i] = fmt.Sprintf("%s = %d/100 (%s)", nz(w.spec.Target, "workload"), w.report.Score, w.report.Grade)
	}
	notes = append(notes, "per-workload: "+strings.Join(rows, "; "))
	return notes
}
