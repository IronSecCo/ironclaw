package scan

import (
	"reflect"
	"strings"
	"testing"
)

// kustomizeBuilt mimics `kustomize build <dir>` output: a base Deployment plus an
// overlay-patched, host-namespace Pod, with a non-workload Service that must be
// skipped. The flattened stream is byte-for-byte a plain multi-doc manifest, so
// the kustomize path must agree with the raw --k8s path on the same YAML.
const kustomizeBuilt = `
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  ports: [{port: 80}]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: web
          image: web:1
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities:
              drop: [ALL]
---
apiVersion: v1
kind: Pod
metadata:
  name: porous
spec:
  hostPID: true
  hostNetwork: true
  containers:
    - name: app
      image: nginx
      securityContext:
        privileged: true
`

func TestSpecsFromKustomize_LabelsSourceAndSkipsNonWorkloads(t *testing.T) {
	specs, err := SpecsFromKustomize([]byte(kustomizeBuilt))
	if err != nil {
		t.Fatalf("SpecsFromKustomize: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 workloads (Deployment+Pod), got %d: %+v", len(specs), specs)
	}
	for _, s := range specs {
		if s.Source != "kustomize" {
			t.Errorf("source: want kustomize, got %q", s.Source)
		}
	}
}

// TestKustomize_ParityWithK8sStream is the acceptance parity test: the workloads
// SpecsFromKustomize extracts must score identically to the same manifests parsed
// by the shared k8s stream path. Only the Source label differs (kustomize vs
// helm); every graded dimension and the resulting Score/Grade must match.
func TestKustomize_ParityWithK8sStream(t *testing.T) {
	kSpecs, err := SpecsFromKustomize([]byte(kustomizeBuilt))
	if err != nil {
		t.Fatalf("SpecsFromKustomize: %v", err)
	}
	hSpecs, err := SpecsFromK8sStream([]byte(kustomizeBuilt))
	if err != nil {
		t.Fatalf("SpecsFromK8sStream: %v", err)
	}
	if len(kSpecs) != len(hSpecs) {
		t.Fatalf("workload count mismatch: kustomize=%d k8s-stream=%d", len(kSpecs), len(hSpecs))
	}
	for i := range kSpecs {
		k, h := kSpecs[i], hSpecs[i]
		if k.Target != h.Target {
			t.Errorf("workload %d target mismatch: %q vs %q", i, k.Target, h.Target)
		}
		// Every graded field must match; only Source is expected to differ.
		kr, hr := Score(k), Score(h)
		if kr.Score != hr.Score || kr.Grade != hr.Grade {
			t.Errorf("workload %d score mismatch: kustomize=%d/%s k8s=%d/%s",
				i, kr.Score, kr.Grade, hr.Score, hr.Grade)
		}
		k.Source, h.Source = "", ""
		if !reflect.DeepEqual(k, h) {
			t.Errorf("workload %d spec mismatch (ignoring Source):\n kustomize=%+v\n k8s      =%+v", i, k, h)
		}
	}
}

func TestAggregateKustomize_WeakestWorkloadGovernsAndIdentity(t *testing.T) {
	specs, err := SpecsFromKustomize([]byte(kustomizeBuilt))
	if err != nil {
		t.Fatal(err)
	}
	report, worst, err := AggregateKustomize(specs, "overlays-prod")
	if err != nil {
		t.Fatalf("AggregateKustomize: %v", err)
	}
	if report.Source != "kustomize" || report.Target != "overlays-prod" {
		t.Errorf("aggregate identity: source=%q target=%q", report.Source, report.Target)
	}
	// The privileged, host-namespace pod must set the build grade.
	if worst.Target != "Pod/porous" {
		t.Errorf("weakest workload: want Pod/porous, got %q", worst.Target)
	}
	if report.Grade != "F" {
		t.Errorf("build grade: want F (privileged pod), got %s (score %d)", report.Grade, report.Score)
	}
	joined := strings.Join(report.Notes, "\n")
	if !strings.Contains(joined, "graded 2 rendered workload(s)") {
		t.Errorf("missing workload-count note: %q", joined)
	}
	if !strings.Contains(joined, "kustomize build") {
		t.Errorf("roll-up note should name the kustomize build unit: %q", joined)
	}
	if !strings.Contains(joined, "per-workload:") {
		t.Errorf("missing per-workload roll-up: %q", joined)
	}
}

func TestAggregateKustomize_NoWorkloadsIsError(t *testing.T) {
	if _, _, err := AggregateKustomize(nil, "empty"); err == nil {
		t.Fatal("want error for empty workload set (fail-closed), got nil")
	}
}
