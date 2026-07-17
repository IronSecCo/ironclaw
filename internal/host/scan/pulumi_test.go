package scan

import (
	"reflect"
	"testing"
)

// A Pulumi stack export (checkpoint) whose deployment.resources[] carry a
// hardened Kubernetes Deployment and a porous one. Pulumi's kubernetes inputs ARE
// the Kubernetes API object (camelCase apiVersion/kind/metadata/spec).
const pulumiK8sStackExport = `{
  "version": 3,
  "deployment": {
    "manifest": {},
    "resources": [
      {
        "urn": "urn:pulumi:dev::proj::pulumi:pulumi:Stack::proj-dev",
        "type": "pulumi:pulumi:Stack"
      },
      {
        "urn": "urn:pulumi:dev::proj::kubernetes:apps/v1:Deployment::hardened",
        "custom": true,
        "type": "kubernetes:apps/v1:Deployment",
        "inputs": {
          "apiVersion": "apps/v1",
          "kind": "Deployment",
          "metadata": { "name": "hardened" },
          "spec": {
            "template": {
              "spec": {
                "securityContext": { "runAsNonRoot": true, "runAsUser": 1000 },
                "containers": [
                  {
                    "name": "app",
                    "securityContext": {
                      "privileged": false,
                      "readOnlyRootFilesystem": true,
                      "allowPrivilegeEscalation": false,
                      "runAsNonRoot": true,
                      "capabilities": { "drop": ["ALL"] },
                      "seccompProfile": { "type": "RuntimeDefault" }
                    }
                  }
                ]
              }
            }
          }
        }
      },
      {
        "urn": "urn:pulumi:dev::proj::kubernetes:core/v1:Pod::porous",
        "custom": true,
        "type": "kubernetes:core/v1:Pod",
        "inputs": {
          "apiVersion": "v1",
          "kind": "Pod",
          "metadata": { "name": "porous" },
          "spec": {
            "hostNetwork": true,
            "hostPID": true,
            "containers": [
              { "name": "root", "securityContext": { "privileged": true } }
            ]
          }
        }
      }
    ]
  }
}`

// The same hardened Deployment expressed as a `pulumi preview --json` plan
// (steps[].newState carries the resource shape).
const pulumiK8sPreview = `{
  "steps": [
    {
      "op": "create",
      "urn": "urn:pulumi:dev::proj::kubernetes:apps/v1:Deployment::hardened",
      "newState": {
        "urn": "urn:pulumi:dev::proj::kubernetes:apps/v1:Deployment::hardened",
        "type": "kubernetes:apps/v1:Deployment",
        "inputs": {
          "kind": "Deployment",
          "metadata": { "name": "hardened" },
          "spec": {
            "template": {
              "spec": {
                "securityContext": { "runAsNonRoot": true, "runAsUser": 1000 },
                "containers": [
                  {
                    "name": "app",
                    "securityContext": {
                      "privileged": false,
                      "readOnlyRootFilesystem": true,
                      "allowPrivilegeEscalation": false,
                      "runAsNonRoot": true,
                      "capabilities": { "drop": ["ALL"] },
                      "seccompProfile": { "type": "RuntimeDefault" }
                    }
                  }
                ]
              }
            }
          }
        }
      }
    }
  ],
  "changeSummary": { "create": 1 }
}`

func TestSpecsFromPulumi_StackExportK8s(t *testing.T) {
	specs, err := SpecsFromPulumi([]byte(pulumiK8sStackExport))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 workloads, got %d: %v", len(specs), targets(specs))
	}
	for _, s := range specs {
		if s.Source != "pulumi" {
			t.Errorf("source: want pulumi, got %q", s.Source)
		}
	}
	// The Deployment is graded from spec.template.spec; target names the kind.
	if specs[0].Target != "Deployment/hardened" {
		t.Errorf("target: want Deployment/hardened, got %q", specs[0].Target)
	}
	if specs[0].RunAsNonRoot != Yes || specs[0].CapDropAll != Yes || specs[0].ReadonlyRoot != Yes {
		t.Errorf("hardened deployment mis-graded: %+v", specs[0])
	}
	if specs[1].Target != "Pod/porous" {
		t.Errorf("target: want Pod/porous, got %q", specs[1].Target)
	}
	if specs[1].Privileged != Yes || specs[1].HostNetwork != Yes || specs[1].HostPID != Yes {
		t.Errorf("porous pod mis-graded: %+v", specs[1])
	}
}

func TestAggregatePulumi_WeakestGoverns(t *testing.T) {
	specs, err := SpecsFromPulumi([]byte(pulumiK8sStackExport))
	if err != nil {
		t.Fatal(err)
	}
	report, worst, err := AggregatePulumi(specs, "stack.json")
	if err != nil {
		t.Fatal(err)
	}
	if report.Source != "pulumi" {
		t.Errorf("aggregate source: want pulumi, got %q", report.Source)
	}
	if report.Target != "stack.json" {
		t.Errorf("aggregate target: want stack.json, got %q", report.Target)
	}
	// The porous Pod is the weakest link and must govern the program grade.
	if worst.Target != "Pod/porous" {
		t.Errorf("weakest workload: want Pod/porous, got %q", worst.Target)
	}
	hardened := Score(specs[0])
	if report.Score >= hardened.Score {
		t.Errorf("program grade %d should be the WEAKEST, below the hardened deployment's %d", report.Score, hardened.Score)
	}
}

// Parity: the preview plan and the stack export of the SAME hardened Deployment
// must produce byte-for-byte the same Spec.
func TestSpecsFromPulumi_PreviewMatchesStackExport(t *testing.T) {
	fromExport, err := SpecsFromPulumi([]byte(pulumiK8sStackExport))
	if err != nil {
		t.Fatal(err)
	}
	fromPreview, err := SpecsFromPulumi([]byte(pulumiK8sPreview))
	if err != nil {
		t.Fatal(err)
	}
	if len(fromPreview) != 1 {
		t.Fatalf("preview: want 1 workload, got %d", len(fromPreview))
	}
	if !reflect.DeepEqual(fromExport[0], fromPreview[0]) {
		t.Errorf("preview vs stack export diverged:\n export=%+v\npreview=%+v", fromExport[0], fromPreview[0])
	}
}

// Parity: a Pulumi kubernetes resource must grade IDENTICALLY to the same pod spec
// scanned as a raw Kubernetes manifest via SpecFromK8s (the shared scorer). Only
// the Source label and Target differ.
func TestSpecsFromPulumi_K8sParityWithSpecFromK8s(t *testing.T) {
	const rawManifest = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hardened
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
      containers:
        - name: app
          securityContext:
            privileged: false
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            runAsNonRoot: true
            capabilities:
              drop: ["ALL"]
            seccompProfile:
              type: RuntimeDefault
`
	k8sSpec, err := SpecFromK8s([]byte(rawManifest))
	if err != nil {
		t.Fatal(err)
	}
	pulumiSpecs, err := SpecsFromPulumi([]byte(pulumiK8sStackExport))
	if err != nil {
		t.Fatal(err)
	}
	got := pulumiSpecs[0]

	// Normalize the fields that legitimately differ by input mode.
	got.Source, k8sSpec.Source = "", ""
	got.Target, k8sSpec.Target = "", ""
	if !reflect.DeepEqual(got, k8sSpec) {
		t.Errorf("pulumi k8s spec != SpecFromK8s spec:\n pulumi=%+v\n    k8s=%+v", got, k8sSpec)
	}
	// And the scores must be equal by construction.
	if a, b := Score(pulumiSpecs[0]).Score, Score(k8sSpec).Score; a != b {
		t.Errorf("scores diverged: pulumi=%d k8s=%d", a, b)
	}
}

// --------------------------------------------------------------------------- //
// AWS ECS parity
// --------------------------------------------------------------------------- //

// A classic aws-provider ECS task definition in a Pulumi stack export:
// containerDefinitions is a JSON-ENCODED STRING (like terraform), fields camelCase.
const pulumiEcsClassic = `{
  "deployment": {
    "resources": [
      {
        "urn": "urn:pulumi:dev::proj::aws:ecs/taskDefinition:TaskDefinition::app",
        "type": "aws:ecs/taskDefinition:TaskDefinition",
        "inputs": {
          "family": "webapp",
          "networkMode": "awsvpc",
          "containerDefinitions": "[{\"name\":\"web\",\"user\":\"1000\",\"privileged\":false,\"readonlyRootFilesystem\":true,\"linuxParameters\":{\"capabilities\":{\"drop\":[\"ALL\"]}},\"dockerSecurityOptions\":[\"no-new-privileges\"]}]"
        }
      }
    ]
  }
}`

// The equivalent LIVE/registered task def (containerDefinitions is a real array)
// that --ecs grades. Grading the classic Pulumi form and this must be identical.
const ecsEquivRegistered = `{
  "family": "webapp",
  "networkMode": "awsvpc",
  "containerDefinitions": [
    {
      "name": "web",
      "user": "1000",
      "privileged": false,
      "readonlyRootFilesystem": true,
      "linuxParameters": { "capabilities": { "drop": ["ALL"] } },
      "dockerSecurityOptions": ["no-new-privileges"]
    }
  ]
}`

// A native aws-provider ECS task definition: containerDefinitions is a real ARRAY
// with PascalCase keys (matched case-insensitively), volume nests host.sourcePath.
const pulumiEcsNative = `{
  "deployment": {
    "resources": [
      {
        "urn": "urn:pulumi:dev::proj::aws-native:ecs:TaskDefinition::app",
        "type": "aws-native:ecs:TaskDefinition",
        "inputs": {
          "Family": "webapp",
          "NetworkMode": "awsvpc",
          "ContainerDefinitions": [
            {
              "Name": "web",
              "User": "1000",
              "Privileged": false,
              "ReadonlyRootFilesystem": true,
              "LinuxParameters": { "Capabilities": { "Drop": ["ALL"] } },
              "DockerSecurityOptions": ["no-new-privileges"]
            }
          ]
        }
      }
    ]
  }
}`

func TestSpecsFromPulumi_ECSParityWithSpecsFromECS(t *testing.T) {
	pulumiSpecs, err := SpecsFromPulumi([]byte(pulumiEcsClassic))
	if err != nil {
		t.Fatal(err)
	}
	if len(pulumiSpecs) != 1 {
		t.Fatalf("want 1 ECS container, got %d", len(pulumiSpecs))
	}
	ecsSpecs, err := SpecsFromECS([]byte(ecsEquivRegistered))
	if err != nil {
		t.Fatal(err)
	}
	got, want := pulumiSpecs[0], ecsSpecs[0]
	// Only the Source label differs; every graded dimension must be identical.
	got.Source, want.Source = "", ""
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pulumi ECS spec != SpecsFromECS spec:\n pulumi=%+v\n    ecs=%+v", got, want)
	}
	if pulumiSpecs[0].Source != "pulumi" {
		t.Errorf("source: want pulumi, got %q", pulumiSpecs[0].Source)
	}
}

// The classic (string-encoded) and native (array) Pulumi ECS forms must grade
// identically to each other.
func TestSpecsFromPulumi_ECSNativeMatchesClassic(t *testing.T) {
	classic, err := SpecsFromPulumi([]byte(pulumiEcsClassic))
	if err != nil {
		t.Fatal(err)
	}
	native, err := SpecsFromPulumi([]byte(pulumiEcsNative))
	if err != nil {
		t.Fatal(err)
	}
	if len(native) != 1 {
		t.Fatalf("native: want 1 container, got %d", len(native))
	}
	if !reflect.DeepEqual(classic[0], native[0]) {
		t.Errorf("native vs classic diverged:\nclassic=%+v\n native=%+v", classic[0], native[0])
	}
}

// A task-level host volume mounting the docker control socket must be detected on
// the classic (hostPath string) form.
func TestSpecsFromPulumi_ECSDockerSock(t *testing.T) {
	const withSock = `{
  "resources": [
    {
      "urn": "urn:pulumi:dev::proj::aws:ecs/taskDefinition:TaskDefinition::app",
      "type": "aws:ecs/taskDefinition:TaskDefinition",
      "inputs": {
        "family": "legacy",
        "containerDefinitions": "[{\"name\":\"agent\",\"privileged\":true}]",
        "volumes": [{ "name": "sock", "hostPath": "/var/run/docker.sock" }]
      }
    }
  ]
}`
	specs, err := SpecsFromPulumi([]byte(withSock))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d", len(specs))
	}
	if specs[0].DockerSock != Yes {
		t.Errorf("docker.sock host volume not detected: %+v", specs[0])
	}
}

func TestAggregatePulumi_Empty(t *testing.T) {
	// A document with no gradeable container workload is a fail-closed error.
	const noWorkloads = `{ "deployment": { "resources": [
    { "type": "aws:s3/bucket:Bucket", "inputs": { "bucket": "data" } }
  ] } }`
	specs, err := SpecsFromPulumi([]byte(noWorkloads))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("want 0 specs from non-container resources, got %d", len(specs))
	}
	if _, _, err := AggregatePulumi(specs, "empty.json"); err == nil {
		t.Error("want fail-closed error for a program with no gradeable workload, got nil")
	}
}

func TestSpecsFromPulumi_MalformedFailsOpen(t *testing.T) {
	if _, err := SpecsFromPulumi([]byte(`{not json`)); err == nil {
		t.Error("want parse error on malformed JSON")
	}
}
