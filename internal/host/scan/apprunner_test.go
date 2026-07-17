package scan

import (
	"strings"
	"testing"
)

// appRunnerDescribeService mirrors `aws apprunner describe-service` output: the
// service wrapped under a top-level "Service" key, sourced from an ECR image with a
// default (public) egress config.
const appRunnerDescribeService = `{
  "Service": {
    "ServiceName": "web",
    "ServiceArn": "arn:aws:apprunner:us-east-1:123456789012:service/web/abc123",
    "SourceConfiguration": {
      "ImageRepository": {
        "ImageIdentifier": "public.ecr.aws/nginx/nginx:latest",
        "ImageRepositoryType": "ECR_PUBLIC",
        "ImageConfiguration": { "Port": "8080" }
      }
    },
    "InstanceConfiguration": { "Cpu": "1024", "Memory": "2048" },
    "NetworkConfiguration": {
      "EgressConfiguration": { "EgressType": "DEFAULT" },
      "IngressConfiguration": { "IsPubliclyAccessible": true }
    }
  }
}`

// appRunnerRawService is a raw Service object (no describe-service wrapper), routed
// through a VPC connector for egress.
const appRunnerRawService = `{
  "ServiceName": "api",
  "ServiceArn": "arn:aws:apprunner:eu-west-1:123456789012:service/api/def456",
  "SourceConfiguration": {
    "CodeRepository": { "RepositoryUrl": "https://github.com/acme/api" }
  },
  "InstanceConfiguration": { "Cpu": "2048", "Memory": "4096" },
  "NetworkConfiguration": {
    "EgressConfiguration": { "EgressType": "VPC", "VpcConnectorArn": "arn:aws:apprunner:eu-west-1:123456789012:vpcconnector/c/1/x" }
  }
}`

func gradeAppRunner(t *testing.T, raw string) (Report, Spec) {
	t.Helper()
	specs, err := SpecsFromAppRunner([]byte(raw))
	if err != nil {
		t.Fatalf("SpecsFromAppRunner: %v", err)
	}
	report, worst, err := AggregateAppRunner(specs, "service.json")
	if err != nil {
		t.Fatalf("AggregateAppRunner: %v", err)
	}
	return report, worst
}

func TestAppRunnerHonestCeiling(t *testing.T) {
	// App Runner exposes no securityContext, so non-root / read-only rootfs / caps
	// all grade fail-closed. The managed floors (seccomp 15 + net 4 + sock 15 +
	// host-ns 10 + default-caps-retained 4) sum to the honest 48/100 ceiling.
	report, worst := gradeAppRunner(t, appRunnerDescribeService)
	if report.Score != 48 || report.Grade != "D" {
		t.Fatalf("App Runner service: got %d/100 (grade %s), want 48 (D) — the honest managed-runtime ceiling; notes: %v", report.Score, report.Grade, report.Notes)
	}
	if report.Source != "app-runner" {
		t.Errorf("source = %q, want app-runner", report.Source)
	}
	// Managed-runtime floors must be credited, not fail-closed.
	if worst.Privileged != No || worst.HostPID != No || worst.HostIPC != No || worst.HostNetwork != No {
		t.Errorf("managed floors not applied: priv=%v hostPID=%v hostIPC=%v hostNet=%v", worst.Privileged, worst.HostPID, worst.HostIPC, worst.HostNetwork)
	}
	if worst.DockerSock != No {
		t.Errorf("docker.sock should be impossible on App Runner: %v", worst.DockerSock)
	}
	if worst.Seccomp != "confined" {
		t.Errorf("managed sandbox seccomp = %q, want confined", worst.Seccomp)
	}
	// Unexpressible dimensions: fail-closed.
	if worst.RunAsNonRoot != Unknown {
		t.Errorf("non-root = %v, want Unknown (App Runner cannot express a user)", worst.RunAsNonRoot)
	}
	if worst.ReadonlyRoot != Unknown {
		t.Errorf("read-only rootfs = %v, want Unknown (App Runner has no such field)", worst.ReadonlyRoot)
	}
	// Capabilities are NOT credited as dropped (Fargate retains the default set).
	if worst.CapDropAll != No {
		t.Errorf("caps must NOT be credited as dropped on App Runner (Fargate retains defaults): CapDropAll=%v", worst.CapDropAll)
	}
}

func TestAppRunnerSurfacesFirecracker(t *testing.T) {
	report, worst := gradeAppRunner(t, appRunnerDescribeService)
	if worst.Runtime != "firecracker" {
		t.Errorf("runtime = %q, want firecracker (Fargate microVM)", worst.Runtime)
	}
	if !strings.Contains(strings.ToLower(report.HardenedRuntime), "firecracker") {
		t.Errorf("App Runner should surface Firecracker as a hardened runtime; got %q", report.HardenedRuntime)
	}
}

func TestAppRunnerRawServiceGraded(t *testing.T) {
	// A raw Service object (no describe-service wrapper) with VPC egress must grade
	// identically (App Runner has no config surface to vary the score).
	report, worst := gradeAppRunner(t, appRunnerRawService)
	if report.Score != 48 || report.Grade != "D" {
		t.Fatalf("raw App Runner service: got %d/100 (grade %s), want 48 (D)", report.Score, report.Grade)
	}
	if !strings.Contains(worst.NetworkMode, "VPC") {
		t.Errorf("VPC egress mode not surfaced: %q", worst.NetworkMode)
	}
	if !strings.Contains(worst.Target, "api") {
		t.Errorf("service name not in target: %q", worst.Target)
	}
}

func TestAppRunnerCeilingDocumented(t *testing.T) {
	report, _ := gradeAppRunner(t, appRunnerDescribeService)
	joined := strings.Join(report.Notes, " ")
	if !strings.Contains(joined, "48/100") {
		t.Errorf("the honest ceiling (48/100) should be documented in the notes: %v", report.Notes)
	}
	// The misleading k8s conservative notes must be gone.
	if strings.Contains(joined, "NetworkPolicy") {
		t.Errorf("k8s NetworkPolicy note must be dropped for App Runner: %v", report.Notes)
	}
}

func TestAppRunnerWeakestServiceWins(t *testing.T) {
	// Two services graded together: the aggregate is the weakest (both are 48 here,
	// but the rollup note must reflect a 2-service grade).
	specs, err := SpecsFromAppRunner([]byte(appRunnerDescribeService))
	if err != nil {
		t.Fatalf("SpecsFromAppRunner: %v", err)
	}
	more, err := SpecsFromAppRunner([]byte(appRunnerRawService))
	if err != nil {
		t.Fatalf("SpecsFromAppRunner: %v", err)
	}
	specs = append(specs, more...)
	report, _, err := AggregateAppRunner(specs, "services")
	if err != nil {
		t.Fatalf("AggregateAppRunner: %v", err)
	}
	if !strings.Contains(strings.Join(report.Notes, " "), "graded 2 App Runner service") {
		t.Errorf("expected 2 services graded; notes: %v", report.Notes)
	}
}

func TestAppRunnerNonServiceSkipped(t *testing.T) {
	// A JSON object that is not an App Runner service must not be graded.
	specs, err := SpecsFromAppRunner([]byte(`{"foo": "bar", "Cluster": {"status": "ACTIVE"}}`))
	if err != nil {
		t.Fatalf("SpecsFromAppRunner: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("non-App-Runner JSON should be skipped; got %d specs", len(specs))
	}
	if _, _, err := AggregateAppRunner(specs, "x"); err == nil {
		t.Error("AggregateAppRunner on an empty service set must error (fail-closed)")
	}
}

func TestAppRunnerMalformedJSONErrors(t *testing.T) {
	if _, err := SpecsFromAppRunner([]byte(`{"Service": {`)); err == nil {
		t.Error("malformed JSON must return a parse error")
	}
}

// TestAppRunnerCloudRunManagedFloorParity asserts that App Runner reuses the SAME
// managed-runtime model as the sibling --cloudrun mode: on every dimension the two
// managed runtimes both enforce by construction (no privileged, no host namespaces,
// no docker.sock, a default seccomp profile, managed egress at the WARN tier), an App
// Runner service and a Cloud Run service grade IDENTICALLY. The two intentionally
// diverge only on the dimensions App Runner cannot express (non-root, read-only
// rootfs, capabilities), which is what makes App Runner's honest ceiling lower.
func TestAppRunnerCloudRunManagedFloorParity(t *testing.T) {
	_, ar := gradeAppRunner(t, appRunnerDescribeService)
	_, cr := gradeCloudRun(t, cloudRunBaselineService)

	if ar.Privileged != cr.Privileged {
		t.Errorf("privileged floor diverges: app-runner=%v cloudrun=%v", ar.Privileged, cr.Privileged)
	}
	if ar.HostPID != cr.HostPID || ar.HostIPC != cr.HostIPC || ar.HostNetwork != cr.HostNetwork {
		t.Errorf("host-namespace floor diverges: app-runner(%v/%v/%v) cloudrun(%v/%v/%v)",
			ar.HostPID, ar.HostIPC, ar.HostNetwork, cr.HostPID, cr.HostIPC, cr.HostNetwork)
	}
	if ar.DockerSock != cr.DockerSock {
		t.Errorf("docker.sock floor diverges: app-runner=%v cloudrun=%v", ar.DockerSock, cr.DockerSock)
	}
	if ar.Seccomp != cr.Seccomp {
		t.Errorf("seccomp floor diverges: app-runner=%q cloudrun=%q", ar.Seccomp, cr.Seccomp)
	}
	// Both are egress-capable managed runtimes → the network dimension scores the
	// same WARN-tier points, even though the mode label differs.
	arNet, _, _ := gradeNetwork(ar)
	crNet, _, _ := gradeNetwork(cr)
	if arNet != crNet {
		t.Errorf("managed-egress network score diverges: app-runner=%d cloudrun=%d", arNet, crNet)
	}
}
