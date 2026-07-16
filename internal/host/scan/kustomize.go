package scan

// SpecsFromKustomize parses the multi-document Kubernetes YAML stream produced
// by `kustomize build <dir>` (or `kubectl kustomize <dir>`) and returns one Spec
// per gradeable workload, in document order. It is the kustomize-sourced twin of
// SpecsFromK8sStream: identical multi-doc parsing and workload selection (the
// same overlay-flattening render feeds the same k8s scorer), only the Spec.Source
// label differs ("kustomize") so reports name the input mode. It is pure and
// unit-testable: the caller runs `kustomize build` (I/O) and passes the stream.
func SpecsFromKustomize(rendered []byte) ([]Spec, error) {
	return specsFromK8sStreamSource(rendered, "kustomize")
}

// AggregateKustomize folds the per-workload specs of a rendered kustomization
// into a single Report representing the build's isolation posture. Like
// AggregateHelm it is the WEAKEST workload (minimum score): a kustomization is
// only as isolated as its most-exposed pod, so grading the weakest link is the
// honest, fail-closed summary. target names the source directory for the report.
// It is pure; the caller injects Version/GeneratedAt. Fail-closed: an empty
// workload set is an error (a build that renders no gradeable workload is not a
// pass).
func AggregateKustomize(specs []Spec, target string) (Report, Spec, error) {
	return aggregateWeakestWorkload(specs, target, "kustomize", "kustomize build")
}
