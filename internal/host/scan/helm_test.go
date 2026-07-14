package scan

import (
	"strings"
	"testing"
)

// helmRenderedHardened mimics `helm template` output for a well-hardened chart:
// multiple gradeable workloads (Deployment + CronJob) plus non-workload docs
// (Service, ConfigMap) that must be skipped.
const helmRenderedHardened = `
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  ports: [{port: 80}]
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  key: val
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
apiVersion: batch/v1
kind: CronJob
metadata:
  name: nightly
spec:
  jobTemplate:
    spec:
      template:
        spec:
          hostPID: false
          hostIPC: false
          hostNetwork: false
          securityContext:
            runAsNonRoot: true
            seccompProfile:
              type: RuntimeDefault
          containers:
            - name: job
              image: job:1
              securityContext:
                runAsNonRoot: true
                readOnlyRootFilesystem: true
                capabilities:
                  drop: [ALL]
`

// helmRenderedMixed pairs a hardened Deployment with a privileged, host-namespace
// Pod: the aggregate must reflect the WEAKEST (the porous pod).
const helmRenderedMixed = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: safe
spec:
  template:
    spec:
      hostPID: false
      hostIPC: false
      hostNetwork: false
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: safe
          image: safe:1
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
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
      volumeMounts:
        - name: sock
          mountPath: /var/run/docker.sock
  volumes:
    - name: sock
      hostPath:
        path: /var/run/docker.sock
`

func TestSpecsFromK8sStream_SkipsNonWorkloadsAndParsesAll(t *testing.T) {
	specs, err := SpecsFromK8sStream([]byte(helmRenderedHardened))
	if err != nil {
		t.Fatalf("SpecsFromK8sStream: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 workloads (Deployment+CronJob), got %d: %+v", len(specs), specs)
	}
	got := map[string]bool{}
	for _, s := range specs {
		got[s.Target] = true
		if s.Source != "helm" {
			t.Errorf("source: want helm, got %q", s.Source)
		}
	}
	for _, want := range []string{"Deployment/web", "CronJob/nightly"} {
		if !got[want] {
			t.Errorf("missing workload %q in %v", want, got)
		}
	}
}

func TestSpecsFromK8sStream_Empty(t *testing.T) {
	specs, err := SpecsFromK8sStream([]byte("apiVersion: v1\nkind: Service\nmetadata:\n  name: only\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("want 0 workloads, got %d", len(specs))
	}
}

func TestAggregateHelm_HardenedChartHighGrade(t *testing.T) {
	specs, err := SpecsFromK8sStream([]byte(helmRenderedHardened))
	if err != nil {
		t.Fatal(err)
	}
	report, worst, err := AggregateHelm(specs, "mychart")
	if err != nil {
		t.Fatalf("AggregateHelm: %v", err)
	}
	if report.Source != "helm" || report.Target != "mychart" {
		t.Errorf("aggregate identity: source=%q target=%q", report.Source, report.Target)
	}
	// Network egress is the honest ceiling (no NetworkPolicy in a pod spec), so a
	// fully hardened chart lands in B, not A.
	if report.Grade != "B" {
		t.Errorf("hardened chart grade: want B (network ceiling), got %s (score %d)", report.Grade, report.Score)
	}
	if report.Score < 75 {
		t.Errorf("hardened chart score too low: %d", report.Score)
	}
	if worst.Target == "" {
		t.Error("worst spec should be identified")
	}
	joined := strings.Join(report.Notes, "\n")
	if !strings.Contains(joined, "graded 2 rendered workload(s)") {
		t.Errorf("missing workload-count note: %q", joined)
	}
	if !strings.Contains(joined, "per-workload:") {
		t.Errorf("missing per-workload roll-up: %q", joined)
	}
}

func TestAggregateHelm_WeakestWorkloadWins(t *testing.T) {
	specs, err := SpecsFromK8sStream([]byte(helmRenderedMixed))
	if err != nil {
		t.Fatal(err)
	}
	report, worst, err := AggregateHelm(specs, "mixed")
	if err != nil {
		t.Fatalf("AggregateHelm: %v", err)
	}
	// The privileged, host-namespace, docker.sock pod must set the chart grade.
	if worst.Target != "Pod/porous" {
		t.Errorf("weakest workload: want Pod/porous, got %q", worst.Target)
	}
	if report.Grade != "F" {
		t.Errorf("mixed chart grade: want F (privileged pod), got %s (score %d)", report.Grade, report.Score)
	}
	if report.Score >= 25 {
		t.Errorf("weakest-link score should be very low, got %d", report.Score)
	}
}

func TestAggregateHelm_NoWorkloadsIsError(t *testing.T) {
	if _, _, err := AggregateHelm(nil, "empty"); err == nil {
		t.Fatal("want error for empty workload set (fail-closed), got nil")
	}
}
