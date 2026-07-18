package scan

import (
	"reflect"
	"strings"
	"testing"
)

// openShiftManifests mimics an `oc get -o yaml` export: a hardened OpenShift
// DeploymentConfig (which embeds a standard k8s PodSpec at spec.template.spec and
// carries OpenShift-only triggers/strategy fields that must be ignored), a
// non-workload Route that must be skipped, and a porous plain Pod that sets the
// set grade. The DeploymentConfig's pod spec is byte-for-byte a k8s pod template,
// so the openshift path must agree with the raw --k8s stream path on the same YAML.
const openShiftManifests = `
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: web
spec:
  to:
    kind: Service
    name: web
---
apiVersion: apps.openshift.io/v1
kind: DeploymentConfig
metadata:
  name: web
spec:
  replicas: 3
  triggers:
    - type: ConfigChange
    - type: ImageChange
      imageChangeParams:
        automatic: true
        containerNames: [web]
  strategy:
    type: Rolling
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

func TestSpecsFromOpenShift_LabelsSourceGradesDCSkipsNonWorkloads(t *testing.T) {
	specs, err := SpecsFromOpenShift([]byte(openShiftManifests))
	if err != nil {
		t.Fatalf("SpecsFromOpenShift: %v", err)
	}
	// DeploymentConfig + Pod graded; Route skipped.
	if len(specs) != 2 {
		t.Fatalf("want 2 workloads (DeploymentConfig+Pod), got %d: %+v", len(specs), specs)
	}
	if specs[0].Target != "DeploymentConfig/web" {
		t.Errorf("first workload target: want DeploymentConfig/web, got %q", specs[0].Target)
	}
	for _, s := range specs {
		if s.Source != "openshift" {
			t.Errorf("source: want openshift, got %q", s.Source)
		}
	}
}

// TestOpenShift_ParityWithK8sStream is the acceptance parity test: the workloads
// SpecsFromOpenShift extracts must score identically to the same manifests parsed
// by the shared k8s stream path (a DeploymentConfig's embedded pod spec grades
// exactly like the equivalent Deployment). Only the Source label differs.
func TestOpenShift_ParityWithK8sStream(t *testing.T) {
	oSpecs, err := SpecsFromOpenShift([]byte(openShiftManifests))
	if err != nil {
		t.Fatalf("SpecsFromOpenShift: %v", err)
	}
	kSpecs, err := SpecsFromK8sStream([]byte(openShiftManifests))
	if err != nil {
		t.Fatalf("SpecsFromK8sStream: %v", err)
	}
	if len(oSpecs) != len(kSpecs) {
		t.Fatalf("workload count mismatch: openshift=%d k8s-stream=%d", len(oSpecs), len(kSpecs))
	}
	for i := range oSpecs {
		o, k := oSpecs[i], kSpecs[i]
		if o.Target != k.Target {
			t.Errorf("workload %d target mismatch: %q vs %q", i, o.Target, k.Target)
		}
		or, kr := Score(o), Score(k)
		if or.Score != kr.Score || or.Grade != kr.Grade {
			t.Errorf("workload %d score mismatch: openshift=%d/%s k8s=%d/%s",
				i, or.Score, or.Grade, kr.Score, kr.Grade)
		}
		o.Source, k.Source = "", ""
		if !reflect.DeepEqual(o, k) {
			t.Errorf("workload %d spec mismatch (ignoring Source):\n openshift=%+v\n k8s      =%+v", i, o, k)
		}
	}
}

func TestAggregateOpenShift_WeakestWorkloadGovernsAndIdentity(t *testing.T) {
	specs, err := SpecsFromOpenShift([]byte(openShiftManifests))
	if err != nil {
		t.Fatal(err)
	}
	report, worst, err := AggregateOpenShift(specs, "app-manifests")
	if err != nil {
		t.Fatalf("AggregateOpenShift: %v", err)
	}
	if report.Source != "openshift" || report.Target != "app-manifests" {
		t.Errorf("aggregate identity: source=%q target=%q", report.Source, report.Target)
	}
	// The privileged, host-namespace pod must set the set grade, not the hardened DC.
	if worst.Target != "Pod/porous" {
		t.Errorf("weakest workload: want Pod/porous, got %q", worst.Target)
	}
	if report.Grade != "F" {
		t.Errorf("set grade: want F (privileged pod), got %s (score %d)", report.Grade, report.Score)
	}
	joined := strings.Join(report.Notes, "\n")
	if !strings.Contains(joined, "graded 2 rendered workload(s)") {
		t.Errorf("missing workload-count note: %q", joined)
	}
	if !strings.Contains(joined, "manifest set") {
		t.Errorf("roll-up note should name the manifest-set unit: %q", joined)
	}
	if !strings.Contains(joined, "per-workload:") {
		t.Errorf("missing per-workload roll-up: %q", joined)
	}
}

// TestOpenShift_DeploymentConfigParityWithDeployment proves a DeploymentConfig
// grades identically to the equivalent plain Deployment: same embedded pod spec,
// same score, and the OpenShift-only triggers/strategy/replicas fields are ignored.
func TestOpenShift_DeploymentConfigParityWithDeployment(t *testing.T) {
	const dc = `
apiVersion: apps.openshift.io/v1
kind: DeploymentConfig
metadata:
  name: web
spec:
  triggers: [{type: ConfigChange}]
  strategy: {type: Rolling}
  template:
    spec:
      containers:
        - name: web
          image: web:1
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities: {drop: [ALL]}
`
	const deploy = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
        - name: web
          image: web:1
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities: {drop: [ALL]}
`
	dcSpecs, err := SpecsFromOpenShift([]byte(dc))
	if err != nil {
		t.Fatalf("SpecsFromOpenShift(dc): %v", err)
	}
	depSpecs, err := SpecsFromK8sStream([]byte(deploy))
	if err != nil {
		t.Fatalf("SpecsFromK8sStream(deploy): %v", err)
	}
	if len(dcSpecs) != 1 || len(depSpecs) != 1 {
		t.Fatalf("want 1 workload each, got dc=%d deploy=%d", len(dcSpecs), len(depSpecs))
	}
	if Score(dcSpecs[0]).Score != Score(depSpecs[0]).Score {
		t.Errorf("DeploymentConfig and Deployment must score identically: dc=%d deploy=%d",
			Score(dcSpecs[0]).Score, Score(depSpecs[0]).Score)
	}
	// Ignoring Source and Target, the graded pod spec must be identical.
	d, p := dcSpecs[0], depSpecs[0]
	d.Source, p.Source = "", ""
	d.Target, p.Target = "", ""
	if !reflect.DeepEqual(d, p) {
		t.Errorf("DeploymentConfig pod spec != Deployment pod spec:\n dc    =%+v\n deploy=%+v", d, p)
	}
}

func TestAggregateOpenShift_NoWorkloadsIsError(t *testing.T) {
	if _, _, err := AggregateOpenShift(nil, "empty"); err == nil {
		t.Fatal("want error for empty workload set (fail-closed), got nil")
	}
}
