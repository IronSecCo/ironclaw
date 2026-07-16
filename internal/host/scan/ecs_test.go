package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A hardened container inside the `aws ecs describe-task-definition` wrapper:
// non-root user, cap_drop ALL, read-only rootfs, no-new-privileges, awsvpc net.
const ecsDescribeHardened = `{
  "taskDefinition": {
    "family": "webapp",
    "networkMode": "awsvpc",
    "containerDefinitions": [
      {
        "name": "web",
        "user": "1000",
        "privileged": false,
        "readonlyRootFilesystem": true,
        "linuxParameters": {
          "capabilities": { "drop": ["ALL"] },
          "initProcessEnabled": true
        },
        "dockerSecurityOptions": ["no-new-privileges"]
      }
    ]
  }
}`

// A raw registered task def (no wrapper): a root, privileged, host-network,
// host-PID container mounting the docker control socket — the worst case.
const ecsRawPorous = `{
  "family": "legacy",
  "networkMode": "host",
  "pidMode": "host",
  "ipcMode": "host",
  "containerDefinitions": [
    {
      "name": "agent",
      "user": "0",
      "privileged": true
    }
  ],
  "volumes": [
    { "name": "sock", "host": { "sourcePath": "/var/run/docker.sock" } }
  ]
}`

func TestSpecsFromECS_DescribeWrapperHardened(t *testing.T) {
	specs, err := SpecsFromECS([]byte(ecsDescribeHardened))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d: %v", len(specs), targets(specs))
	}
	s := specs[0]
	if s.Source != "ecs" {
		t.Errorf("source: want ecs, got %q", s.Source)
	}
	if s.Target != "ecs/webapp/web" {
		t.Errorf("target: want ecs/webapp/web, got %q", s.Target)
	}
	if s.RunAsNonRoot != Yes || s.User != "1000" {
		t.Errorf("user: RunAsNonRoot=%v User=%q", s.RunAsNonRoot, s.User)
	}
	if s.CapDropAll != Yes || s.ReadonlyRoot != Yes || s.Privileged != No {
		t.Errorf("caps/rootfs/privileged: %+v", s)
	}
	if s.NetworkMode != "awsvpc" || s.HostNetwork != No {
		t.Errorf("network: %q hostNet=%v (awsvpc is a NIC, not host)", s.NetworkMode, s.HostNetwork)
	}
	if s.NoNewPrivs != Yes {
		t.Errorf("no-new-privileges not extracted: %v", s.NoNewPrivs)
	}
	if s.Seccomp != "confined" {
		t.Errorf("seccomp: want confined (ECS docker default), got %q", s.Seccomp)
	}
	// awsvpc has an egress-capable NIC (not "none"), so network isolation cannot
	// pass — the honest ceiling for a hardened ECS container is high-B, not A.
	if r := Score(s); r.Score < 80 {
		t.Errorf("hardened container should grade high (network is the only gap), got %d/100 (%s)", r.Score, r.Grade)
	}
}

func TestSpecsFromECS_RawRootPorous(t *testing.T) {
	specs, err := SpecsFromECS([]byte(ecsRawPorous))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d", len(specs))
	}
	s := specs[0]
	if s.Target != "ecs/legacy/agent" {
		t.Errorf("target: want ecs/legacy/agent, got %q", s.Target)
	}
	if s.RunAsNonRoot != No || s.Privileged != Yes {
		t.Errorf("root/privileged not caught: RunAsNonRoot=%v Privileged=%v", s.RunAsNonRoot, s.Privileged)
	}
	if s.NetworkMode != "host" || s.HostNetwork != Yes {
		t.Errorf("host network not caught: %q hostNet=%v", s.NetworkMode, s.HostNetwork)
	}
	if s.HostPID != Yes || s.HostIPC != Yes {
		t.Errorf("host pid/ipc not caught: pid=%v ipc=%v", s.HostPID, s.HostIPC)
	}
	if s.DockerSock != Yes {
		t.Errorf("docker.sock host volume not caught: %v", s.DockerSock)
	}
	// No linuxParameters.capabilities => ECS keeps docker's default set (fail-closed).
	if s.CapDropAll != No {
		t.Errorf("missing capabilities should be No (docker default), got %v", s.CapDropAll)
	}
	if r := Score(s); r.Score >= 25 {
		t.Errorf("privileged root host-net container should grade near-zero, got %d/100 (%s)", r.Score, r.Grade)
	}
}

func TestSpecsFromECS_NoUserIsUnknown(t *testing.T) {
	const noUser = `{"family":"f","networkMode":"awsvpc","containerDefinitions":[{"name":"c"}]}`
	specs, err := SpecsFromECS([]byte(noUser))
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].RunAsNonRoot != Unknown {
		t.Errorf("absent user should be Unknown (fail-closed), got %v", specs[0].RunAsNonRoot)
	}
}

func TestSpecsFromECS_SeccompUnconfined(t *testing.T) {
	const unconf = `{"family":"f","containerDefinitions":[
		{"name":"c","user":"1000","dockerSecurityOptions":["seccomp:unconfined"]}]}`
	specs, err := SpecsFromECS([]byte(unconf))
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].Seccomp != "unconfined" {
		t.Errorf("seccomp:unconfined not caught, got %q", specs[0].Seccomp)
	}
}

func TestSpecsFromECS_CapAddWeakens(t *testing.T) {
	const capAdd = `{"family":"f","containerDefinitions":[
		{"name":"c","user":"1000","linuxParameters":{"capabilities":{"drop":["ALL"],"add":["NET_ADMIN","sys_admin"]}}}]}`
	specs, err := SpecsFromECS([]byte(capAdd))
	if err != nil {
		t.Fatal(err)
	}
	s := specs[0]
	if s.CapDropAll != Yes {
		t.Errorf("cap drop ALL not caught: %v", s.CapDropAll)
	}
	// Added caps are upper-cased regardless of source casing.
	if len(s.CapAdd) != 2 || s.CapAdd[0] != "NET_ADMIN" || s.CapAdd[1] != "SYS_ADMIN" {
		t.Errorf("cap_add not normalized: %v", s.CapAdd)
	}
}

func TestSpecsFromECS_MultiContainerAndAggregate(t *testing.T) {
	// One hardened container + one root container in the same task.
	const twoContainers = `{
	  "taskDefinition": {
	    "family": "svc",
	    "networkMode": "awsvpc",
	    "containerDefinitions": [
	      {"name":"good","user":"1000","readonlyRootFilesystem":true,
	       "linuxParameters":{"capabilities":{"drop":["ALL"]}},"dockerSecurityOptions":["no-new-privileges"]},
	      {"name":"bad","user":"0","privileged":true}
	    ]
	  }
	}`
	specs, err := SpecsFromECS([]byte(twoContainers))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 containers, got %d: %v", len(specs), targets(specs))
	}
	report, worst, err := AggregateECS(specs, "svc.json")
	if err != nil {
		t.Fatal(err)
	}
	if report.Source != "ecs" || report.Target != "svc.json" {
		t.Errorf("aggregate header: source=%q target=%q", report.Source, report.Target)
	}
	// Weakest-container rollup: the root/privileged container sets the grade.
	if worst.Target != "ecs/svc/bad" {
		t.Errorf("weakest container: want ecs/svc/bad, got %q", worst.Target)
	}
	if !strings.Contains(strings.Join(report.Notes, " "), "per-container:") {
		t.Errorf("aggregate notes missing per-container roll-up: %v", report.Notes)
	}
}

// TestSpecsFromECS_DogfoodSample grades the committed sample describe-task-definition
// JSON on disk (a partly-hardened real-world task: a hardened api container plus a
// privileged log-router sidecar). It locks in the dogfood result: the task rolls up
// to the WEAKEST container.
func TestSpecsFromECS_DogfoodSample(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "ecs-describe-taskdef.json"))
	if err != nil {
		t.Fatal(err)
	}
	specs, err := SpecsFromECS(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 containers, got %d: %v", len(specs), targets(specs))
	}
	report, worst, err := AggregateECS(specs, "ecs-describe-taskdef.json")
	if err != nil {
		t.Fatal(err)
	}
	// The privileged log-router sidecar is the weakest link and sets the grade.
	if worst.Target != "ecs/orders-api/log-router" || report.Grade != "F" {
		t.Errorf("weakest-link rollup: worst=%q grade=%q (want ecs/orders-api/log-router / F)", worst.Target, report.Grade)
	}
}

func TestAggregateECS_EmptyIsError(t *testing.T) {
	if _, _, err := AggregateECS(nil, "x"); err == nil {
		t.Fatal("want fail-closed error for empty container set, got nil")
	}
}

func TestSpecsFromECS_NoContainersIsNoSpecs(t *testing.T) {
	const empty = `{"taskDefinition":{"family":"f","networkMode":"awsvpc"}}`
	specs, err := SpecsFromECS([]byte(empty))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("want 0 specs for a task with no containers, got %d", len(specs))
	}
}

func TestSpecsFromECS_MalformedIsError(t *testing.T) {
	if _, err := SpecsFromECS([]byte("not json")); err == nil {
		t.Fatal("want parse error for malformed document, got nil")
	}
}

// TestECSTerraformParity locks the two ECS entrypoints together: the same
// container contract graded via --terraform (container_definitions as a JSON
// STRING) and via --ecs (registered JSON) must produce identical dimension verdicts
// and the same score. Only Source differs.
func TestECSTerraformParity(t *testing.T) {
	// Registered JSON (the --ecs path).
	const ecsJSON = `{"taskDefinition":{"family":"api","networkMode":"awsvpc",
		"containerDefinitions":[{"name":"api","user":"1000","readonlyRootFilesystem":true,
		"linuxParameters":{"capabilities":{"drop":["ALL"]}},"dockerSecurityOptions":["no-new-privileges"]}]}}`
	ecsSpecs, err := SpecsFromECS([]byte(ecsJSON))
	if err != nil {
		t.Fatal(err)
	}
	// The SAME contract expressed in a terraform plan (container_definitions is a
	// JSON-encoded string on aws_ecs_task_definition).
	const tfJSON = `{"planned_values":{"root_module":{"resources":[{
		"address":"aws_ecs_task_definition.api","type":"aws_ecs_task_definition","name":"api",
		"values":{"family":"api","network_mode":"awsvpc",
		"container_definitions":"[{\"name\":\"api\",\"user\":\"1000\",\"readonlyRootFilesystem\":true,\"linuxParameters\":{\"capabilities\":{\"drop\":[\"ALL\"]}},\"dockerSecurityOptions\":[\"no-new-privileges\"]}]"}}]}}}`
	tfSpecs, err := SpecsFromTerraform([]byte(tfJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(ecsSpecs) != 1 || len(tfSpecs) != 1 {
		t.Fatalf("want 1 spec each, got ecs=%d tf=%d", len(ecsSpecs), len(tfSpecs))
	}
	e, tf := ecsSpecs[0], tfSpecs[0]
	if e.Source != "ecs" || tf.Source != "terraform" {
		t.Errorf("source: ecs=%q tf=%q", e.Source, tf.Source)
	}
	if Score(e).Score != Score(tf).Score {
		t.Errorf("parity broken: ecs=%d tf=%d", Score(e).Score, Score(tf).Score)
	}
	// Dimension verdicts must match regardless of source.
	if e.RunAsNonRoot != tf.RunAsNonRoot || e.CapDropAll != tf.CapDropAll ||
		e.ReadonlyRoot != tf.ReadonlyRoot || e.NetworkMode != tf.NetworkMode ||
		e.Seccomp != tf.Seccomp || e.NoNewPrivs != tf.NoNewPrivs {
		t.Errorf("dimension divergence:\n ecs=%+v\n tf =%+v", e, tf)
	}
}
