package scan

import (
	"strings"
	"testing"
)

// cloudRunHardenedService is a Knative Service whose revision hardens everything a
// Cloud Run spec CAN express: it runs as non-root and mounts a read-only rootfs.
// Combined with Cloud Run's managed-runtime guarantees (no privileged, no host
// namespaces, no docker.sock, sandboxed syscalls), this is the best a Cloud Run
// service reaches — the honest 89/B ceiling (egress is managed, never none).
const cloudRunHardenedService = `
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: web
spec:
  template:
    metadata:
      annotations:
        run.googleapis.com/execution-environment: gen2
    spec:
      containers:
        - image: gcr.io/proj/web
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
          resources:
            limits:
              cpu: "1"
              memory: 512Mi
`

// cloudRunBaselineService declares no securityContext at all: it runs as the
// image default (root) with a writable rootfs. The managed-runtime floors still
// apply (caps restricted, seccomp confined, no privileged/host-ns/docker.sock), so
// it lands mid-band rather than failing.
const cloudRunBaselineService = `
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: legacy
spec:
  template:
    spec:
      containers:
        - image: gcr.io/proj/legacy
`

// cloudRunExportService mirrors `gcloud run services describe --format=export`:
// it carries the gen1 execution-environment annotation and extra metadata the
// grader must tolerate. gen1 wraps every container in gVisor (runsc).
const cloudRunExportService = `
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: api
  annotations:
    run.googleapis.com/ingress: all
spec:
  template:
    metadata:
      annotations:
        run.googleapis.com/execution-environment: gen1
    spec:
      containerConcurrency: 80
      containers:
        - image: gcr.io/proj/api
          securityContext:
            runAsUser: 1000
            readOnlyRootFilesystem: true
`

func gradeCloudRun(t *testing.T, raw string) (Report, Spec) {
	t.Helper()
	specs, err := SpecsFromCloudRun([]byte(raw))
	if err != nil {
		t.Fatalf("SpecsFromCloudRun: %v", err)
	}
	report, worst, err := AggregateCloudRun(specs, "service.yaml")
	if err != nil {
		t.Fatalf("AggregateCloudRun: %v", err)
	}
	return report, worst
}

func TestCloudRunHardenedHitsCeiling(t *testing.T) {
	report, worst := gradeCloudRun(t, cloudRunHardenedService)
	if report.Score != 89 || report.Grade != "B" {
		t.Fatalf("hardened Cloud Run service: got %d/100 (grade %s), want 89 (B) — the managed-egress ceiling; notes: %v", report.Score, report.Grade, report.Notes)
	}
	if report.Source != "cloudrun" {
		t.Errorf("source = %q, want cloudrun", report.Source)
	}
	// Managed-runtime guarantees must be credited, not fail-closed.
	if worst.Privileged != No || worst.HostPID != No || worst.HostIPC != No || worst.HostNetwork != No {
		t.Errorf("managed floors not applied: priv=%v hostPID=%v hostIPC=%v hostNet=%v", worst.Privileged, worst.HostPID, worst.HostIPC, worst.HostNetwork)
	}
	if worst.DockerSock != No {
		t.Errorf("docker.sock should be impossible on Cloud Run: %v", worst.DockerSock)
	}
	if worst.CapDropAll != Yes {
		t.Errorf("managed sandbox should credit dropped caps: %v", worst.CapDropAll)
	}
	if worst.Seccomp != "confined" {
		t.Errorf("managed sandbox seccomp = %q, want confined", worst.Seccomp)
	}
}

func TestCloudRunBaselineManagedFloors(t *testing.T) {
	// No securityContext: non-root and read-only rootfs are unknown (fail-closed),
	// but the managed floors keep the service out of the failing band.
	report, worst := gradeCloudRun(t, cloudRunBaselineService)
	if worst.RunAsNonRoot != Unknown {
		t.Errorf("baseline user = %v, want Unknown (image default may be root)", worst.RunAsNonRoot)
	}
	if worst.ReadonlyRoot != Unknown {
		t.Errorf("baseline rootfs = %v, want Unknown (writable by default)", worst.ReadonlyRoot)
	}
	// 20 (caps) + 15 (seccomp) + 4 (net) + 15 (sock) + 10 (host-ns) = 64.
	if report.Score != 64 || report.Grade != "C" {
		t.Fatalf("baseline Cloud Run service: got %d/100 (grade %s), want 64 (C); notes: %v", report.Score, report.Grade, report.Notes)
	}
}

func TestCloudRunGen1SurfacesGVisor(t *testing.T) {
	report, worst := gradeCloudRun(t, cloudRunExportService)
	if worst.Runtime != "runsc" {
		t.Errorf("gen1 runtime = %q, want runsc (gVisor)", worst.Runtime)
	}
	if !strings.Contains(strings.ToLower(report.HardenedRuntime), "gvisor") {
		t.Errorf("gen1 should surface gVisor as a hardened runtime; got %q", report.HardenedRuntime)
	}
	// runAsUser: 1000 (non-zero) credits the non-root dimension.
	if worst.RunAsNonRoot != Yes {
		t.Errorf("runAsUser 1000 should grade non-root: %v", worst.RunAsNonRoot)
	}
}

func TestCloudRunSeccompUnconfinedRespected(t *testing.T) {
	const svc = `
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: risky
spec:
  template:
    spec:
      containers:
        - image: x
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            seccompProfile:
              type: Unconfined
`
	report, worst := gradeCloudRun(t, svc)
	if worst.Seccomp != "unconfined" {
		t.Fatalf("explicit seccompProfile Unconfined must be respected (fail-closed): %q", worst.Seccomp)
	}
	// 89 ceiling minus the 15-point seccomp dimension = 74.
	if report.Score != 74 {
		t.Errorf("unconfined seccomp: got %d/100, want 74 (89 - seccomp 15)", report.Score)
	}
}

func TestCloudRunAddedCapsPenalized(t *testing.T) {
	const svc = `
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: netadmin
spec:
  template:
    spec:
      containers:
        - image: x
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            capabilities:
              add: [NET_ADMIN]
`
	report, worst := gradeCloudRun(t, svc)
	if worst.CapDropAll != No {
		t.Errorf("an explicit added capability must not be masked by the managed floor: CapDropAll=%v", worst.CapDropAll)
	}
	if len(worst.CapAdd) != 1 || worst.CapAdd[0] != "NET_ADMIN" {
		t.Errorf("added capability not surfaced: %v", worst.CapAdd)
	}
	// 89 ceiling minus the 20-point caps dimension = 69.
	if report.Score != 69 {
		t.Errorf("added caps: got %d/100, want 69 (89 - caps 20)", report.Score)
	}
}

func TestCloudRunWeakestServiceWins(t *testing.T) {
	// A deployment with one hardened and one baseline service must grade the WEAKEST.
	multi := cloudRunHardenedService + "\n---\n" + cloudRunBaselineService
	report, worst := gradeCloudRun(t, multi)
	if report.Score != 64 || report.Grade != "C" {
		t.Fatalf("multi-service deployment: got %d/100 (grade %s), want 64 (C) — the weakest service governs", report.Score, report.Grade)
	}
	if !strings.Contains(worst.Target, "legacy") {
		t.Errorf("weakest service = %q, want the baseline 'legacy'", worst.Target)
	}
	joined := strings.Join(report.Notes, " ")
	if !strings.Contains(joined, "graded 2 Cloud Run service") {
		t.Errorf("expected 2 services graded; notes: %v", report.Notes)
	}
}

func TestCloudRunNonServiceDocsSkipped(t *testing.T) {
	// A plain Kubernetes Service (v1, a load balancer) and a ConfigMap in the same
	// stream must NOT be mistaken for a Cloud Run revision.
	stream := cloudRunHardenedService + `
---
apiVersion: v1
kind: Service
metadata:
  name: k8s-lb
spec:
  ports:
    - port: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  x: "1"
`
	specs, err := SpecsFromCloudRun([]byte(stream))
	if err != nil {
		t.Fatalf("SpecsFromCloudRun: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1 (only the Knative Service is gradeable)", len(specs))
	}
	if specs[0].Target != "web" {
		t.Errorf("graded service = %q, want web", specs[0].Target)
	}
}

func TestCloudRunResourceLimitsNoted(t *testing.T) {
	_, worst := gradeCloudRun(t, cloudRunHardenedService)
	joined := strings.Join(worst.Notes, " ")
	if !strings.Contains(joined, "resource limits declared") || !strings.Contains(joined, "memory=512Mi") {
		t.Errorf("expected a resource-limits note; got: %v", worst.Notes)
	}
	// The misleading k8s conservative notes must be gone.
	if strings.Contains(joined, "NetworkPolicy") {
		t.Errorf("k8s NetworkPolicy note must be dropped for Cloud Run: %v", worst.Notes)
	}
}

func TestCloudRunManagedCeilingDocumented(t *testing.T) {
	report, _ := gradeCloudRun(t, cloudRunHardenedService)
	joined := strings.Join(report.Notes, " ")
	if !strings.Contains(joined, "89/100") {
		t.Errorf("the honest managed-egress ceiling (89/100) should be documented in the notes: %v", report.Notes)
	}
}

func TestCloudRunNoServicesIsError(t *testing.T) {
	// A document with no Cloud Run service has nothing to grade: fail-closed.
	const cfgOnly = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  x: "1"
`
	specs, err := SpecsFromCloudRun([]byte(cfgOnly))
	if err != nil {
		t.Fatalf("SpecsFromCloudRun: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("ConfigMap should be skipped; got %d specs", len(specs))
	}
	if _, _, err := AggregateCloudRun(specs, "svc"); err == nil {
		t.Error("AggregateCloudRun on an empty service set must error (fail-closed)")
	}
}

func TestCloudRunMalformedYAMLErrors(t *testing.T) {
	if _, err := SpecsFromCloudRun([]byte("kind: Service\n\tbad: [indent")); err == nil {
		t.Error("malformed YAML must return a parse error")
	}
}
