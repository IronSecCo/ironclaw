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

// SpecsFromCloudRun parses a Google Cloud Run service specification — a Knative
// Service document (apiVersion: serving.knative.dev/v1, kind: Service), as emitted
// by `gcloud run services describe SVC --format=export` or hand-authored — and
// returns one graded Spec per Service across a multi-document stream.
//
// Cloud Run's revision template carries a Kubernetes-shaped pod spec at
// spec.template.spec.containers[], so we reuse the SAME specFromPodSpec scorer
// path as --k8s/--helm and then fold in Cloud Run's MANAGED-RUNTIME guarantees:
// the platform forbids privileged mode and host namespaces, never mounts a host
// docker socket, and sandboxes every container (gen1 = gVisor, gen2 = microVM)
// so the syscall surface is filtered by default. Those are genuine platform wins
// the raw pod-spec grader cannot see, so we credit them explicitly. What stays the
// user's job — running as non-root and a read-only rootfs — is graded on what the
// spec expresses, fail-closed when absent. Cloud Run's egress is managed (allowed
// by default, restrictable via VPC egress settings) and can never be network=none,
// so a fully hardened service tops out at the same honest 89/B ceiling we document
// for any egress-capable managed runtime.
//
// It is pure and unit-testable: the caller loads the YAML (I/O) and passes the
// bytes here. It fails OPEN on a malformed document (returns the parse error); the
// CLI wrapper turns that into a skip so an opt-in CI step never crashes the build.
func SpecsFromCloudRun(raw []byte) ([]Spec, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var specs []Spec
	for {
		var svc cloudRunService
		err := dec.Decode(&svc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse cloud run service yaml: %w", err)
		}
		if s, ok := specFromCloudRunService(svc); ok {
			specs = append(specs, s)
		}
	}
	return specs, nil
}

// AggregateCloudRun folds the per-service specs of a Cloud Run deployment into a
// single Report. Like AggregateHelm/AggregateNomad, the aggregate is the WEAKEST
// service (minimum score): a deployment is only as isolated as its most-exposed
// service, so grading the weakest link is the honest, fail-closed summary. target
// names the source (file/dir). It returns the aggregate Report and the Spec that
// produced it (for SARIF anchoring). It is pure; the caller injects
// Version/GeneratedAt. Fail-closed: an empty service set is an error (a document
// that declares no gradeable Cloud Run service is not a pass).
func AggregateCloudRun(specs []Spec, target string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable Cloud Run services found (looked for Knative Service documents with spec.template.spec.containers)")
	}

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
	if strings.TrimSpace(target) != "" {
		agg.Target = target
	}
	agg.Source = "cloudrun"
	agg.Notes = append(cloudRunSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// cloudRunSummaryNotes builds the deployment-level roll-up notes for an aggregate
// Cloud Run report: a headline naming the weakest service and a per-service score
// list.
func cloudRunSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d Cloud Run service(s); the grade is the WEAKEST (a deployment is only as isolated as its most-exposed service). Weakest: %s at %d/100 (grade %s).",
			len(all), nz(worst.spec.Target, "service"), worst.report.Score, worst.report.Grade),
	}
	sorted := make([]scoredWorkload, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].report.Score < sorted[j].report.Score })
	rows := make([]string, len(sorted))
	for i, w := range sorted {
		rows[i] = fmt.Sprintf("%s = %d/100 (%s)", nz(w.spec.Target, "service"), w.report.Score, w.report.Grade)
	}
	notes = append(notes, "per-service: "+strings.Join(rows, "; "))
	return notes
}

// --------------------------------------------------------------------------- //
// Cloud Run / Knative Service document model.
// --------------------------------------------------------------------------- //

// cloudRunService is the security-relevant subset of a Knative Service document
// (serving.knative.dev/v1). The revision template's spec is a Kubernetes-shaped
// pod spec (containers[].securityContext, resources), so we decode it into the
// SAME podSpec type the k8s/helm adapters use and grade it through specFromPodSpec.
type cloudRunService struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Template struct {
			Metadata struct {
				Annotations map[string]string `yaml:"annotations"`
			} `yaml:"metadata"`
			// The revision template's spec IS a Kubernetes-shaped pod spec, so we
			// decode it into the shared podSpec type and grade it via specFromPodSpec.
			Spec podSpec `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

// isKnativeService reports whether a decoded document is a Cloud Run / Knative
// Service worth grading. It matches on kind (Service) and, when present, the
// Knative serving API group, so an unrelated Kubernetes Service (v1) is not
// mistaken for a Cloud Run revision.
func (svc cloudRunService) isKnativeService() bool {
	if !strings.EqualFold(strings.TrimSpace(svc.Kind), "Service") {
		return false
	}
	api := strings.ToLower(strings.TrimSpace(svc.APIVersion))
	if api == "" {
		// Tolerate an export that omitted apiVersion but clearly carries a Cloud Run
		// revision template (containers under spec.template.spec).
		return len(svc.Spec.Template.Spec.Containers) > 0
	}
	return strings.Contains(api, "serving.knative.dev") || strings.Contains(api, "run.googleapis.com")
}

// specFromCloudRunService maps one Cloud Run / Knative Service to a graded Spec.
// ok is false for a non-Service document or a Service with no revision container.
// It grades the FIRST container's expressible posture via specFromPodSpec, then
// folds in Cloud Run's managed-runtime guarantees.
func specFromCloudRunService(svc cloudRunService) (Spec, bool) {
	if !svc.isKnativeService() {
		return Spec{}, false
	}
	ps := svc.Spec.Template.Spec
	if len(ps.Containers) == 0 {
		return Spec{}, false
	}

	name := strings.TrimSpace(svc.Metadata.Name)
	// Grade the expressible pod-spec posture (non-root, read-only rootfs, any
	// explicit capabilities/seccomp the revision declares) exactly like --k8s.
	s := specFromPodSpec("cloudrun", name, ps)

	// The k8s pod-spec grader appends two conservative notes that are WRONG for
	// Cloud Run: it assumes seccomp is unconfined by default and that egress is
	// governed by an invisible NetworkPolicy. Drop them; Cloud Run's managed
	// runtime gives concrete, honest answers below.
	s.Notes = dropNotesContaining(s.Notes, "seccompProfile set", "NetworkPolicy")

	// --- managed-runtime guarantees (platform-enforced, not spec-visible) -----
	// Cloud Run forbids privileged mode and host namespaces, and never mounts a
	// host docker socket into a revision. These are genuine platform wins the raw
	// pod-spec grader scores fail-closed (Unknown), so credit them explicitly.
	s.Privileged = No
	s.HostPID = No
	s.HostIPC = No
	s.HostNetwork = No
	s.DockerSock = No

	// The managed sandbox restricts the capability surface: a revision cannot run
	// privileged and cannot ADD capabilities, so unless the spec explicitly adds
	// some, the effective set is the restricted managed default. Credit it as a
	// drop-all-equivalent when the spec adds nothing.
	if len(s.CapAdd) == 0 {
		s.CapDropAll = Yes
	}

	// Every Cloud Run container is sandboxed (gen1 = gVisor, gen2 = microVM), which
	// filters the syscall surface by default. Mirror the ECS/docker "default
	// profile = confined" mapping: an unset seccompProfile is confined here, not
	// unconfined. Respect an explicit seccompProfile: Unconfined the spec set.
	if strings.TrimSpace(s.Seccomp) == "" {
		s.Seccomp = "confined"
	}

	// --- network (managed egress → honest 89/B ceiling) -----------------------
	// Cloud Run egress is managed: allowed by default, restrictable via VPC egress
	// settings, but never network=none. Grade it as egress-capable (the scorer's
	// WARN tier), the same honest ceiling we document for any managed runtime.
	s.NetworkMode = "cloudrun (managed egress)"

	// --- managed-runtime + expressible-posture notes --------------------------
	env := cloudRunExecEnv(svc)
	switch env {
	case "gen1":
		// gen1 wraps every container in gVisor (runsc). Surface it informationally
		// via the hardened-runtime path (scoring stays runtime-agnostic per IRO-429).
		s.Runtime = "runsc"
		s.Notes = append(s.Notes, "Cloud Run gen1 execution environment: every container runs in the gVisor (runsc) sandbox (userspace-kernel syscall interception)")
	case "gen2":
		s.Notes = append(s.Notes, "Cloud Run gen2 execution environment: every container runs in a microVM sandbox (hardware-virtualization isolation)")
	default:
		s.Notes = append(s.Notes, "Cloud Run managed sandbox isolates every container (gen1 gVisor / gen2 microVM); privileged mode and host namespaces are not permitted")
	}
	s.Notes = append(s.Notes,
		"managed runtime: Cloud Run forbids privileged mode, host PID/IPC/network namespaces, and host docker.sock mounts — those dimensions pass by construction",
		"network egress is managed (allowed by default, restrictable via VPC egress settings) and can never be network=none, so a fully hardened Cloud Run service tops out at 89/100 (grade B) — the honest ceiling for an egress-capable managed runtime")

	// Resource limits are expressible on a revision but are NOT a containment
	// dimension in the 7-dim scorer; surface them as evidence without awarding
	// points (we do not invent a dimension).
	if lims := cloudRunResourceLimits(svc); lims != "" {
		s.Notes = append(s.Notes, "resource limits declared ("+lims+"); informational — not a containment dimension")
	} else {
		s.Notes = append(s.Notes, "no resources.limits declared; a runaway revision is bounded only by the Cloud Run platform defaults")
	}

	return s, true
}

// cloudRunExecEnv returns the revision's execution-environment ("gen1"/"gen2"), or
// "" when the annotation is absent. Cloud Run carries it as the annotation
// run.googleapis.com/execution-environment on the revision template.
func cloudRunExecEnv(svc cloudRunService) string {
	v := strings.ToLower(strings.TrimSpace(svc.Spec.Template.Metadata.Annotations["run.googleapis.com/execution-environment"]))
	switch v {
	case "gen1", "gen2":
		return v
	default:
		return ""
	}
}

// cloudRunResourceLimits renders the first revision container's resource limits as
// a compact "cpu=1, memory=512Mi" string, or "" when none are declared.
func cloudRunResourceLimits(svc cloudRunService) string {
	conts := svc.Spec.Template.Spec.Containers
	if len(conts) == 0 || conts[0].Resources == nil {
		return ""
	}
	lims := conts[0].Resources.Limits
	if len(lims) == 0 {
		return ""
	}
	keys := make([]string, 0, len(lims))
	for k := range lims {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+lims[k])
	}
	return strings.Join(parts, ", ")
}

// dropNotesContaining returns notes with every entry that contains any of the
// given substrings removed. Used to strip the k8s pod-spec grader's conservative
// seccomp/NetworkPolicy notes, which do not apply to Cloud Run's managed runtime.
func dropNotesContaining(notes []string, subs ...string) []string {
	if len(notes) == 0 {
		return notes
	}
	out := notes[:0:0]
	for _, n := range notes {
		drop := false
		for _, sub := range subs {
			if strings.Contains(n, sub) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, n)
		}
	}
	return out
}
