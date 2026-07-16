package scan

import (
	"strings"
	"testing"
)

// A hardened ECS task definition expressed in a CloudFormation YAML template:
// non-root user, cap_drop ALL, read-only rootfs, no-new-privileges, awsvpc net.
const cfnYAMLHardened = `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  WebTask:
    Type: AWS::ECS::TaskDefinition
    Properties:
      Family: webapp
      NetworkMode: awsvpc
      ContainerDefinitions:
        - Name: web
          Image: example/web:1.2.3
          User: "1000"
          Privileged: false
          ReadonlyRootFilesystem: true
          LinuxParameters:
            Capabilities:
              Drop: [ALL]
            InitProcessEnabled: true
          DockerSecurityOptions:
            - no-new-privileges
`

// The worst case in a JSON CloudFormation template: a root, privileged,
// host-network, host-PID container mounting the docker control socket.
const cfnJSONPorous = `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Resources": {
    "LegacyTask": {
      "Type": "AWS::ECS::TaskDefinition",
      "Properties": {
        "Family": "legacy",
        "NetworkMode": "host",
        "PidMode": "host",
        "IpcMode": "host",
        "ContainerDefinitions": [
          { "Name": "agent", "User": "0", "Privileged": true }
        ],
        "Volumes": [
          { "Name": "sock", "Host": { "SourcePath": "/var/run/docker.sock" } }
        ]
      }
    }
  }
}`

func TestSpecsFromCloudFormation_YAMLHardened(t *testing.T) {
	specs, intr, err := SpecsFromCloudFormation([]byte(cfnYAMLHardened))
	if err != nil {
		t.Fatal(err)
	}
	if intr {
		t.Errorf("no intrinsics in this template; intr should be false")
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d: %v", len(specs), targets(specs))
	}
	s := specs[0]
	if s.Source != "cloudformation" {
		t.Errorf("source: want cloudformation, got %q", s.Source)
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
	// awsvpc is an egress-capable NIC (not network=none), so a fully hardened ECS
	// task tops out at the honest 89/100 (grade B) ceiling.
	if r := Score(s); r.Score < 89 {
		t.Errorf("hardened task should grade high, got %d/100 (%s)", r.Score, r.Grade)
	}
}

func TestSpecsFromCloudFormation_JSONPorous(t *testing.T) {
	specs, _, err := SpecsFromCloudFormation([]byte(cfnJSONPorous))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d", len(specs))
	}
	s := specs[0]
	if s.Source != "cloudformation" || s.Target != "ecs/legacy/agent" {
		t.Errorf("header: source=%q target=%q", s.Source, s.Target)
	}
	if s.Privileged != Yes {
		t.Errorf("privileged not extracted: %v", s.Privileged)
	}
	if s.RunAsNonRoot != No {
		t.Errorf("user 0 should be root: %v", s.RunAsNonRoot)
	}
	if s.HostNetwork != Yes || s.NetworkMode != "host" {
		t.Errorf("host network not extracted: mode=%q hostNet=%v", s.NetworkMode, s.HostNetwork)
	}
	if s.HostPID != Yes || s.HostIPC != Yes {
		t.Errorf("host namespaces not extracted: pid=%v ipc=%v", s.HostPID, s.HostIPC)
	}
	if s.DockerSock != Yes {
		t.Errorf("docker.sock host volume not detected: %v", s.DockerSock)
	}
	if r := Score(s); r.Score > 20 {
		t.Errorf("worst-case task should grade low, got %d/100 (%s)", r.Score, r.Grade)
	}
}

// TestSpecsFromCloudFormation_IntrinsicsTolerated proves a template full of
// intrinsics still parses fail-open: a graded field behind a !Ref reads as unset
// (fail-closed) and the intrinsic flag is raised, rather than erroring out.
func TestSpecsFromCloudFormation_IntrinsicsTolerated(t *testing.T) {
	const tmpl = `Resources:
  Task:
    Type: AWS::ECS::TaskDefinition
    Properties:
      Family: !Ref FamilyName
      NetworkMode: !Sub "${NetMode}"
      ContainerDefinitions:
        - Name: app
          Image: !Ref ImageUri
          Privileged: !If [IsDev, true, false]
          User: !Ref RunUser
`
	specs, intr, err := SpecsFromCloudFormation([]byte(tmpl))
	if err != nil {
		t.Fatalf("intrinsics must not error (fail-open): %v", err)
	}
	if !intr {
		t.Errorf("intr flag should be true (template uses !Ref/!Sub/!If)")
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d", len(specs))
	}
	s := specs[0]
	// Family unresolved -> the family folds to the "task" fallback; the container
	// name is still literal.
	if s.Target != "ecs/task/app" {
		t.Errorf("target with unresolved family: want ecs/task/app, got %q", s.Target)
	}
	// User behind !Ref is unknown -> fail-closed (not credited as non-root).
	if s.RunAsNonRoot == Yes {
		t.Errorf("unresolved !Ref user must not be credited as non-root: %v", s.RunAsNonRoot)
	}
	// Privileged behind !If is unknown -> triFromPtr(nil) == Unknown.
	if s.Privileged == Yes {
		t.Errorf("unresolved !If privileged should not be Yes: %v", s.Privileged)
	}
}

// TestSpecsFromCloudFormation_MultiResourceAggregate grades a template with two
// task definitions and confirms the aggregate is the WEAKEST container.
func TestSpecsFromCloudFormation_MultiResourceAggregate(t *testing.T) {
	const tmpl = `Resources:
  HardTask:
    Type: AWS::ECS::TaskDefinition
    Properties:
      Family: hard
      NetworkMode: awsvpc
      ContainerDefinitions:
        - Name: safe
          User: "1000"
          ReadonlyRootFilesystem: true
          LinuxParameters:
            Capabilities:
              Drop: [ALL]
          DockerSecurityOptions: [no-new-privileges]
  SoftTask:
    Type: AWS::ECS::TaskDefinition
    Properties:
      Family: soft
      NetworkMode: host
      ContainerDefinitions:
        - Name: risky
          User: "0"
          Privileged: true
  NotAnEcsResource:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: irrelevant
`
	specs, _, err := SpecsFromCloudFormation([]byte(tmpl))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 containers (S3 ignored), got %d: %v", len(specs), targets(specs))
	}
	report, worst, err := AggregateCloudFormation(specs, "stack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if report.Source != "cloudformation" || report.Target != "stack.yaml" {
		t.Errorf("aggregate header: source=%q target=%q", report.Source, report.Target)
	}
	if worst.Target != "ecs/soft/risky" {
		t.Errorf("weakest container: want ecs/soft/risky, got %q", worst.Target)
	}
	if !strings.Contains(strings.Join(report.Notes, " "), "WEAKEST") {
		t.Errorf("aggregate should note the weakest-link rollup: %v", report.Notes)
	}
}

func TestSpecsFromCloudFormation_NoEcsResourceIsNoSpecs(t *testing.T) {
	const tmpl = `Resources:
  Bucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: some-bucket
`
	specs, _, err := SpecsFromCloudFormation([]byte(tmpl))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("want 0 specs for a template with no ECS task def, got %d", len(specs))
	}
	if _, _, err := AggregateCloudFormation(specs, "empty.yaml"); err == nil {
		t.Errorf("aggregate of no containers should be a fail-closed error")
	}
}

func TestSpecsFromCloudFormation_MalformedIsError(t *testing.T) {
	// A tab-indented YAML mapping is a hard parse error (fail-open surfaces it).
	if _, _, err := SpecsFromCloudFormation([]byte("Resources:\n\t- bad: [unclosed")); err == nil {
		t.Errorf("malformed template should return a parse error")
	}
}

// TestCloudFormationECSParity locks the CFN entrypoint to the live --ecs
// entrypoint: the SAME container contract graded from a CloudFormation template
// (PascalCase properties) and from registered ECS JSON (camelCase) must produce
// identical dimension verdicts and the same score. Only Source differs.
func TestCloudFormationECSParity(t *testing.T) {
	const cfn = `Resources:
  Api:
    Type: AWS::ECS::TaskDefinition
    Properties:
      Family: api
      NetworkMode: awsvpc
      ContainerDefinitions:
        - Name: api
          User: "1000"
          ReadonlyRootFilesystem: true
          LinuxParameters:
            Capabilities:
              Drop: [ALL]
          DockerSecurityOptions: [no-new-privileges]
`
	cfnSpecs, _, err := SpecsFromCloudFormation([]byte(cfn))
	if err != nil {
		t.Fatal(err)
	}
	// The SAME contract expressed as a registered ECS task definition.
	const ecsJSON = `{"taskDefinition":{"family":"api","networkMode":"awsvpc",
		"containerDefinitions":[{"name":"api","user":"1000","readonlyRootFilesystem":true,
		"linuxParameters":{"capabilities":{"drop":["ALL"]}},"dockerSecurityOptions":["no-new-privileges"]}]}}`
	ecsSpecs, err := SpecsFromECS([]byte(ecsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfnSpecs) != 1 || len(ecsSpecs) != 1 {
		t.Fatalf("want 1 spec each, got cfn=%d ecs=%d", len(cfnSpecs), len(ecsSpecs))
	}
	cf, e := cfnSpecs[0], ecsSpecs[0]
	if cf.Source != "cloudformation" || e.Source != "ecs" {
		t.Errorf("source: cfn=%q ecs=%q", cf.Source, e.Source)
	}
	if Score(cf).Score != Score(e).Score {
		t.Errorf("parity broken: cfn=%d ecs=%d", Score(cf).Score, Score(e).Score)
	}
	if cf.RunAsNonRoot != e.RunAsNonRoot || cf.CapDropAll != e.CapDropAll ||
		cf.ReadonlyRoot != e.ReadonlyRoot || cf.NetworkMode != e.NetworkMode ||
		cf.Seccomp != e.Seccomp || cf.NoNewPrivs != e.NoNewPrivs || cf.Target != e.Target {
		t.Errorf("dimension divergence:\n cfn=%+v\n ecs=%+v", cf, e)
	}
}
