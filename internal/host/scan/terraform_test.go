package scan

import (
	"strings"
	"testing"
)

// tfHardenedPlan mimics `terraform show -json` for a plan whose kubernetes_deployment
// is fully hardened (runAsNonRoot, drop ALL, read-only rootfs, RuntimeDefault
// seccomp). The kubernetes provider models every block as a single-element array
// with snake_case keys, and run_as_user as a string.
const tfHardenedPlan = `{
  "format_version": "1.2",
  "planned_values": {
    "root_module": {
      "resources": [
        {
          "address": "kubernetes_deployment.web",
          "type": "kubernetes_deployment",
          "name": "web",
          "values": {
            "metadata": [{"name": "web", "namespace": "prod"}],
            "spec": [{
              "template": [{
                "spec": [{
                  "host_network": false,
                  "host_pid": false,
                  "host_ipc": false,
                  "security_context": [{
                    "run_as_non_root": true,
                    "run_as_user": "1000",
                    "seccomp_profile": [{"type": "RuntimeDefault"}]
                  }],
                  "container": [{
                    "name": "web",
                    "image": "web:1",
                    "security_context": [{
                      "run_as_non_root": true,
                      "read_only_root_filesystem": true,
                      "allow_privilege_escalation": false,
                      "privileged": false,
                      "capabilities": [{"drop": ["ALL"]}],
                      "seccomp_profile": [{"type": "RuntimeDefault"}]
                    }]
                  }]
                }]
              }]
            }]
          }
        }
      ]
    }
  }
}`

// tfMixedPlan pairs a hardened Deployment with a privileged, host-namespace,
// docker.sock Pod in a CHILD module, plus a partly hardened ECS task definition.
// The aggregate must reflect the WEAKEST workload (the porous pod).
const tfMixedPlan = `{
  "format_version": "1.2",
  "planned_values": {
    "root_module": {
      "resources": [
        {
          "address": "kubernetes_deployment.safe",
          "type": "kubernetes_deployment_v1",
          "name": "safe",
          "values": {
            "metadata": [{"name": "safe"}],
            "spec": [{
              "template": [{
                "spec": [{
                  "host_network": false,
                  "host_pid": false,
                  "host_ipc": false,
                  "security_context": [{"run_as_non_root": true, "seccomp_profile": [{"type": "RuntimeDefault"}]}],
                  "container": [{
                    "name": "safe",
                    "security_context": [{
                      "run_as_non_root": true,
                      "read_only_root_filesystem": true,
                      "capabilities": [{"drop": ["ALL"]}],
                      "seccomp_profile": [{"type": "RuntimeDefault"}]
                    }]
                  }]
                }]
              }]
            }]
          }
        },
        {
          "address": "aws_ecs_task_definition.api",
          "type": "aws_ecs_task_definition",
          "name": "api",
          "values": {
            "family": "api",
            "network_mode": "awsvpc",
            "container_definitions": "[{\"name\":\"api\",\"user\":\"1000\",\"readonlyRootFilesystem\":true,\"privileged\":false,\"linuxParameters\":{\"capabilities\":{\"drop\":[\"ALL\"]}}}]"
          }
        }
      ],
      "child_modules": [
        {
          "address": "module.legacy",
          "resources": [
            {
              "address": "module.legacy.kubernetes_pod.porous",
              "type": "kubernetes_pod",
              "name": "porous",
              "values": {
                "metadata": [{"name": "porous"}],
                "spec": [{
                  "host_network": true,
                  "host_pid": true,
                  "container": [{
                    "name": "app",
                    "image": "nginx",
                    "security_context": [{"privileged": true}],
                    "volume_mount": [{"name": "sock", "mount_path": "/var/run/docker.sock"}]
                  }],
                  "volume": [{"name": "sock", "host_path": [{"path": "/var/run/docker.sock"}]}]
                }]
              }
            }
          ]
        }
      ]
    }
  }
}`

// tfCronState mimics `terraform show -json` of a STATE file (values, not
// planned_values) with a kubernetes_cron_job (deepest nesting: job_template ->
// spec -> template -> spec).
const tfCronState = `{
  "format_version": "1.2",
  "values": {
    "root_module": {
      "resources": [
        {
          "address": "kubernetes_cron_job.nightly",
          "type": "kubernetes_cron_job_v1",
          "name": "nightly",
          "values": {
            "metadata": [{"name": "nightly"}],
            "spec": [{
              "job_template": [{
                "spec": [{
                  "template": [{
                    "spec": [{
                      "host_network": false,
                      "host_pid": false,
                      "host_ipc": false,
                      "security_context": [{"run_as_non_root": true, "seccomp_profile": [{"type": "RuntimeDefault"}]}],
                      "container": [{
                        "name": "job",
                        "security_context": [{
                          "run_as_non_root": true,
                          "read_only_root_filesystem": true,
                          "capabilities": [{"drop": ["ALL"]}],
                          "seccomp_profile": [{"type": "RuntimeDefault"}]
                        }]
                      }]
                    }]
                  }]
                }]
              }]
            }]
          }
        }
      ]
    }
  }
}`

func TestSpecsFromTerraform_HardenedDeployment(t *testing.T) {
	specs, err := SpecsFromTerraform([]byte(tfHardenedPlan))
	if err != nil {
		t.Fatalf("SpecsFromTerraform: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 workload, got %d: %+v", len(specs), specs)
	}
	s := specs[0]
	if s.Source != "terraform" {
		t.Errorf("source: want terraform, got %q", s.Source)
	}
	if s.Target != "Deployment/web" {
		t.Errorf("target: want Deployment/web, got %q", s.Target)
	}
	if s.RunAsNonRoot != Yes || s.CapDropAll != Yes || s.ReadonlyRoot != Yes {
		t.Errorf("hardened posture not extracted: %+v", s)
	}
	if s.Seccomp != "confined" {
		t.Errorf("seccomp: want confined (RuntimeDefault), got %q", s.Seccomp)
	}
}

func TestAggregateTerraform_HardenedIsHighGrade(t *testing.T) {
	specs, err := SpecsFromTerraform([]byte(tfHardenedPlan))
	if err != nil {
		t.Fatal(err)
	}
	report, worst, err := AggregateTerraform(specs, "plan.json")
	if err != nil {
		t.Fatalf("AggregateTerraform: %v", err)
	}
	if report.Source != "terraform" || report.Target != "plan.json" {
		t.Errorf("aggregate identity: source=%q target=%q", report.Source, report.Target)
	}
	// Network egress is the honest ceiling (no NetworkPolicy in a pod spec), so a
	// fully hardened workload lands in B, not A — same ceiling as --helm.
	if report.Grade != "B" {
		t.Errorf("hardened plan grade: want B (network ceiling), got %s (score %d)", report.Grade, report.Score)
	}
	if worst.Target != "Deployment/web" {
		t.Errorf("worst spec target: %q", worst.Target)
	}
	joined := strings.Join(report.Notes, "\n")
	if !strings.Contains(joined, "graded 1 terraform workload(s)") {
		t.Errorf("missing workload-count note: %q", joined)
	}
	if !strings.Contains(joined, "per-workload:") {
		t.Errorf("missing per-workload roll-up: %q", joined)
	}
}

func TestSpecsFromTerraform_WeakestWorkloadWinsAcrossModulesAndECS(t *testing.T) {
	specs, err := SpecsFromTerraform([]byte(tfMixedPlan))
	if err != nil {
		t.Fatal(err)
	}
	// Deployment (root) + ECS container (root) + Pod (child module) = 3 workloads.
	if len(specs) != 3 {
		t.Fatalf("want 3 workloads, got %d: %+v", len(specs), targets(specs))
	}
	report, worst, err := AggregateTerraform(specs, "mixed")
	if err != nil {
		t.Fatalf("AggregateTerraform: %v", err)
	}
	if worst.Target != "Pod/porous" {
		t.Errorf("weakest workload: want Pod/porous, got %q (all: %v)", worst.Target, targets(specs))
	}
	if report.Grade != "F" {
		t.Errorf("mixed plan grade: want F (privileged host-ns pod), got %s (score %d)", report.Grade, report.Score)
	}
	// The porous pod's docker.sock + privileged + host namespaces must be seen.
	if worst.DockerSock != Yes || worst.Privileged != Yes || worst.HostNetwork != Yes || worst.HostPID != Yes {
		t.Errorf("porous pod posture not fully extracted: %+v", worst)
	}
}

func TestSpecsFromTerraform_ECSTaskDefinition(t *testing.T) {
	specs, err := SpecsFromTerraform([]byte(tfMixedPlan))
	if err != nil {
		t.Fatal(err)
	}
	var ecs *Spec
	for i := range specs {
		if strings.HasPrefix(specs[i].Target, "ecs/") {
			ecs = &specs[i]
		}
	}
	if ecs == nil {
		t.Fatalf("no ECS workload found in %v", targets(specs))
	}
	if ecs.Target != "ecs/api/api" {
		t.Errorf("ecs target: want ecs/api/api, got %q", ecs.Target)
	}
	if ecs.RunAsNonRoot != Yes || ecs.User != "1000" {
		t.Errorf("ecs user not extracted: RunAsNonRoot=%v User=%q", ecs.RunAsNonRoot, ecs.User)
	}
	if ecs.CapDropAll != Yes || ecs.ReadonlyRoot != Yes {
		t.Errorf("ecs caps/rootfs not extracted: %+v", ecs)
	}
	// awsvpc is an egress-capable NIC, not host: not "none", not "host".
	if ecs.NetworkMode != "awsvpc" || ecs.HostNetwork != No {
		t.Errorf("ecs network mode: %q hostNet=%v", ecs.NetworkMode, ecs.HostNetwork)
	}
	// ECS applies Docker's default seccomp profile: confined by default.
	if ecs.Seccomp != "confined" {
		t.Errorf("ecs seccomp: want confined (docker default), got %q", ecs.Seccomp)
	}
}

func TestSpecsFromTerraform_CronJobStateNesting(t *testing.T) {
	specs, err := SpecsFromTerraform([]byte(tfCronState))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 workload from state, got %d: %v", len(specs), targets(specs))
	}
	if specs[0].Target != "CronJob/nightly" {
		t.Errorf("cronjob target: want CronJob/nightly, got %q", specs[0].Target)
	}
	if specs[0].CapDropAll != Yes || specs[0].RunAsNonRoot != Yes {
		t.Errorf("cronjob deepest-nested pod spec not extracted: %+v", specs[0])
	}
}

func TestAggregateTerraform_NoWorkloadsIsError(t *testing.T) {
	// A plan with only non-container resources yields no gradeable workload.
	const noWorkloads = `{"planned_values":{"root_module":{"resources":[
	  {"type":"aws_s3_bucket","name":"b","values":{"bucket":"x"}}]}}}`
	specs, err := SpecsFromTerraform([]byte(noWorkloads))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("want 0 workloads, got %d", len(specs))
	}
	if _, _, err := AggregateTerraform(specs, "empty"); err == nil {
		t.Fatal("want error for empty workload set (fail-closed), got nil")
	}
}

func TestSpecsFromTerraform_MalformedIsError(t *testing.T) {
	if _, err := SpecsFromTerraform([]byte("not json")); err == nil {
		t.Fatal("want parse error for malformed document, got nil")
	}
}

func TestEcsUserIsRoot(t *testing.T) {
	cases := map[string]bool{
		"0":          true,
		"root":       true,
		"ROOT":       true,
		"0:0":        true,
		"root:wheel": true,
		"1000":       false,
		"1000:1000":  false,
		"appuser":    false,
		"":           false, // empty handled separately (unknown), not root here
	}
	for in, want := range cases {
		if got := ecsUserIsRoot(in); got != want {
			t.Errorf("ecsUserIsRoot(%q) = %v, want %v", in, got, want)
		}
	}
}

// targets is a test helper: the Target of each spec, for readable failures.
func targets(specs []Spec) []string {
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i] = s.Target
	}
	return out
}
