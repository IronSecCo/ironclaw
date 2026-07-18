package scan

// SpecsFromOpenShift parses a multi-document OpenShift manifest stream (an
// `oc get -o yaml` export, a raw manifest file, or a directory of them
// concatenated) and returns one Spec per gradeable workload, in document order.
// It is the OpenShift-sourced twin of SpecsFromK8sStream/SpecsFromKustomize:
// identical multi-doc parsing and workload selection, only the Spec.Source label
// differs ("openshift") so reports name the input mode. An OpenShift
// DeploymentConfig (apiVersion apps.openshift.io/v1) embeds a standard Kubernetes
// PodSpec at spec.template.spec — the exact shape a plain Deployment uses — so
// the shared k8s pod-spec extraction grades it with no new scorer. Plain
// Deployment/StatefulSet/DaemonSet/Job/CronJob/Pod docs in the same stream are
// graded too; OpenShift-only kinds (Route, ImageStream, BuildConfig, …) and the
// DeploymentConfig's own non-pod fields (triggers, strategy, replicas) carry no
// pod spec and are skipped. It is pure and unit-testable: the caller reads the
// manifest(s) (I/O) and passes the stream here.
func SpecsFromOpenShift(rendered []byte) ([]Spec, error) {
	return specsFromK8sStreamSource(rendered, "openshift")
}

// AggregateOpenShift folds the per-workload specs of an OpenShift manifest set
// into a single Report representing its isolation posture. Like AggregateHelm and
// AggregateKustomize it is the WEAKEST workload (minimum score): a manifest set
// is only as isolated as its most-exposed pod, so grading the weakest link is the
// honest, fail-closed summary. target names the source file/directory for the
// report. It is pure; the caller injects Version/GeneratedAt. Fail-closed: an
// empty workload set is an error (a manifest set with no gradeable workload is not
// a pass).
func AggregateOpenShift(specs []Spec, target string) (Report, Spec, error) {
	return aggregateWeakestWorkload(specs, target, "openshift", "manifest set")
}
