package scan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SpecsFromPulumi parses Pulumi program output and returns one graded Spec per
// container workload it recognizes. It accepts the two JSON shapes a user
// actually has to hand:
//
//   - `pulumi stack export` — a checkpoint whose deployment.resources[] carries
//     every registered resource with its typed inputs/outputs.
//   - `pulumi preview --json` — a plan whose steps[].newState carries the same
//     per-resource shape for what WILL be applied.
//
// A bare {"resources": [...]} array is also accepted so a hand-extracted resource
// list works. It grades two workload classes that carry a container isolation
// posture:
//
//   - the Kubernetes provider's workload resources (kubernetes:*:Pod /
//     Deployment / StatefulSet / DaemonSet / ReplicaSet / ReplicationController /
//     Job / CronJob). Pulumi's kubernetes inputs mirror the Kubernetes API object
//     verbatim (camelCase apiVersion/kind/metadata/spec), so they decode into the
//     SAME k8sObject the --k8s / --helm paths grade and score through the SAME
//     specFromPodSpec mapper.
//   - AWS ECS task definitions (aws:ecs/taskDefinition:TaskDefinition from the
//     classic provider, where containerDefinitions is a JSON-encoded STRING like
//     terraform; and aws-native:ecs:TaskDefinition, where it is a real array like
//     the live/CFN shape). Both fold into the SHARED ecsSpec mapper (ecs.go).
//
// This is the Pulumi counterpart to the terraform adapter (SpecsFromTerraform):
// it reuses the identical scorers so a Pulumi program grades byte-for-byte the
// same as the equivalent terraform/ECS input of the same workload.
//
// It is pure and unit-testable: the caller runs `pulumi stack export` /
// `pulumi preview --json` (I/O) and passes the JSON here. Non-container resources
// are ignored. It fails OPEN on a malformed top-level document (returns the parse
// error); the CLI wrapper turns that into a skip so an opt-in CI step never
// crashes the build.
func SpecsFromPulumi(raw []byte) ([]Spec, error) {
	var doc pulumiDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse pulumi output JSON: %w", err)
	}

	var specs []Spec
	for _, r := range doc.resources() {
		switch {
		case pulumiK8sKind(r.Type) != "":
			if s, ok := specFromPulumiK8s(r); ok {
				specs = append(specs, s)
			}
		case isPulumiECSType(r.Type):
			specs = append(specs, specsFromPulumiECS(r)...)
		}
	}
	return specs, nil
}

// AggregatePulumi folds the per-workload specs from a Pulumi program into a single
// Report representing the program's isolation posture. Like AggregateTerraform,
// the aggregate is the WEAKEST workload (minimum score): a program is only as
// isolated as its most-exposed container, so grading the weakest link is the
// honest, fail-closed summary. target names the source (stack-export/preview file
// base, or a directory rollup label). It returns the aggregate Report and the Spec
// that produced it (for SARIF anchoring). It is pure; the caller injects
// Version/GeneratedAt. Fail-closed: an empty workload set is an error (a program
// that declares no gradeable container workload is not a pass).
func AggregatePulumi(specs []Spec, target string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable container workloads found in pulumi output (looked for kubernetes:* pod/workload resources and aws:ecs/taskDefinition:TaskDefinition)")
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
	agg.Source = "pulumi"
	agg.Notes = append(pulumiSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// pulumiSummaryNotes builds the program-level roll-up notes for an aggregate
// Pulumi report: a headline naming the weakest workload and a per-workload score
// list.
func pulumiSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d pulumi workload(s); the program grade is the WEAKEST (a program is only as isolated as its most-exposed container). Weakest: %s at %d/100 (grade %s).",
			len(all), nz(worst.spec.Target, "workload"), worst.report.Score, worst.report.Grade),
	}
	sorted := make([]scoredWorkload, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].report.Score < sorted[j].report.Score })
	rows := make([]string, len(sorted))
	for i, w := range sorted {
		rows[i] = fmt.Sprintf("%s = %d/100 (%s)", nz(w.spec.Target, "workload"), w.report.Score, w.report.Grade)
	}
	notes = append(notes, "per-workload: "+strings.Join(rows, "; "))
	return notes
}

// --------------------------------------------------------------------------- //
// Pulumi document model (stack export / preview --json)
// --------------------------------------------------------------------------- //

// pulumiDoc accepts the three input shapes at once: a `pulumi stack export`
// checkpoint (deployment.resources[]), a `pulumi preview --json` plan
// (steps[].newState), and a bare {"resources": [...]} list.
type pulumiDoc struct {
	Deployment *struct {
		Resources []pulumiResource `json:"resources"`
	} `json:"deployment"`
	Steps []struct {
		NewState *pulumiResource `json:"newState"`
	} `json:"steps"`
	Resources []pulumiResource `json:"resources"`
}

// resources flattens whichever of the three shapes is populated into a single
// resource slice. A stack export (deployment.resources) is preferred, then a
// preview plan (steps[].newState), then a bare resources list.
func (d pulumiDoc) resources() []pulumiResource {
	if d.Deployment != nil && len(d.Deployment.Resources) > 0 {
		return d.Deployment.Resources
	}
	if len(d.Steps) > 0 {
		out := make([]pulumiResource, 0, len(d.Steps))
		for _, s := range d.Steps {
			if s.NewState != nil {
				out = append(out, *s.NewState)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return d.Resources
}

// pulumiResource is one registered/planned Pulumi resource. Type is the Pulumi
// token ("kubernetes:apps/v1:Deployment", "aws:ecs/taskDefinition:TaskDefinition").
// Inputs carries what the program set; Outputs the resolved state — we grade
// Inputs (the authored, pre-deploy posture) and fall back to Outputs only when a
// resource carries no inputs.
type pulumiResource struct {
	URN     string          `json:"urn"`
	Type    string          `json:"type"`
	Inputs  json.RawMessage `json:"inputs"`
	Outputs json.RawMessage `json:"outputs"`
}

// gradeable returns the JSON to grade for a resource: its inputs, or its outputs
// when inputs are absent.
func (r pulumiResource) gradeable() json.RawMessage {
	if len(bytes.TrimSpace(r.Inputs)) > 0 {
		return r.Inputs
	}
	return r.Outputs
}

// --------------------------------------------------------------------------- //
// Kubernetes provider mapping (pulumi-kubernetes)
//
// Pulumi's kubernetes inputs ARE the Kubernetes API object (camelCase
// apiVersion/kind/metadata/spec) — the resource type token supplies the Kind. So
// the inputs decode straight into the SAME k8sObject the --k8s/--helm paths grade
// (yaml.v3 parses JSON), and score through the SAME specFromPodSpec mapper.
// --------------------------------------------------------------------------- //

// pulumiK8sKind maps a Pulumi kubernetes resource token to the Kubernetes Kind it
// represents, or "" when the token is not a gradeable workload. The token shape is
// "kubernetes:<group>/<version>:<Kind>", so the Kind is the segment after the last
// colon.
func pulumiK8sKind(typ string) string {
	if !strings.HasPrefix(typ, "kubernetes:") {
		return ""
	}
	kind := typ[strings.LastIndex(typ, ":")+1:]
	if workloadKinds[kind] {
		return kind
	}
	return ""
}

// specFromPulumiK8s decodes one kubernetes:* resource's inputs into a k8sObject,
// resolves its pod spec (bare Pod / workload template / CronJob jobTemplate), and
// grades it with the shared specFromPodSpec mapper. ok is false when the resource
// has no container to grade.
func specFromPulumiK8s(r pulumiResource) (Spec, bool) {
	raw := r.gradeable()
	if len(bytes.TrimSpace(raw)) == 0 {
		return Spec{}, false
	}
	var obj k8sObject
	if err := yaml.Unmarshal(raw, &obj); err != nil {
		return Spec{}, false
	}
	ps, ok := obj.podSpecOf()
	if !ok {
		return Spec{}, false
	}
	kind := pulumiK8sKind(r.Type)
	name := strings.TrimSpace(obj.Metadata.Name)
	if name == "" {
		name = pulumiURNName(r.URN)
	}
	target := kind
	if name != "" {
		target = kind + "/" + name
	}
	return specFromPodSpec("pulumi", target, ps), true
}

// --------------------------------------------------------------------------- //
// AWS ECS task definition mapping (pulumi-aws / pulumi-aws-native)
//
// The classic aws provider mirrors terraform: containerDefinitions is a
// JSON-ENCODED STRING and the task-level fields are camelCase. The aws-native
// provider mirrors CloudFormation/live ECS: containerDefinitions is a real array
// and fields are PascalCase (matched case-insensitively by encoding/json). Both
// decode their container definitions into the SHARED ecsContainerDef and grade
// through the SHARED ecsSpec mapper, keyed with source "pulumi".
// --------------------------------------------------------------------------- //

// isPulumiECSType reports whether a Pulumi resource token is an ECS task
// definition from either the classic or native AWS provider.
func isPulumiECSType(typ string) bool {
	return typ == "aws:ecs/taskDefinition:TaskDefinition" || typ == "aws-native:ecs:TaskDefinition"
}

// pulumiECSInputs decodes an ECS task definition's inputs from either AWS
// provider. encoding/json matches keys case-insensitively, so the camelCase
// classic properties and the PascalCase aws-native properties both land here.
// ContainerDefinitions stays a RawMessage because the classic provider encodes it
// as a JSON string while aws-native carries a real array.
type pulumiECSInputs struct {
	Family               string          `json:"family"`
	NetworkMode          string          `json:"networkMode"`
	PidMode              string          `json:"pidMode"`
	IpcMode              string          `json:"ipcMode"`
	ContainerDefinitions json.RawMessage `json:"containerDefinitions"`
	Volumes              []pulumiECSVol  `json:"volumes"`
}

// pulumiECSVol carries a task volume from either provider: the classic provider
// models a host volume as {name, hostPath: "<path>"}, while aws-native nests it as
// {name, host: {sourcePath: "<path>"}}.
type pulumiECSVol struct {
	Name     string `json:"name"`
	HostPath string `json:"hostPath"`
	Host     *struct {
		SourcePath string `json:"sourcePath"`
	} `json:"host"`
}

// specsFromPulumiECS decodes one ECS task definition resource and returns a graded
// Spec per container definition. The task-level network/pid/ipc modes and any
// docker.sock host volume apply to every container. Per-container grading is the
// SHARED ecsSpec (ecs.go), keyed with source "pulumi".
func specsFromPulumiECS(r pulumiResource) []Spec {
	raw := r.gradeable()
	var in pulumiECSInputs
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil
	}
	defs, err := pulumiECSContainerDefs(in.ContainerDefinitions)
	if err != nil || len(defs) == 0 {
		return nil
	}

	family := nz(nz(in.Family, pulumiURNName(r.URN)), "task")

	// docker.sock exposure is a task-level property: a host volume whose source
	// path is the control socket is mountable into any container in the task.
	dockerSock := No
	for _, v := range in.Volumes {
		if isControlSocket(v.HostPath) {
			dockerSock = Yes
		}
		if v.Host != nil && isControlSocket(v.Host.SourcePath) {
			dockerSock = Yes
		}
	}

	modes := ecsTaskModes{NetworkMode: in.NetworkMode, PidMode: in.PidMode, IpcMode: in.IpcMode}
	specs := make([]Spec, 0, len(defs))
	for _, d := range defs {
		specs = append(specs, ecsSpec("pulumi", family, d, modes, dockerSock))
	}
	return specs
}

// pulumiECSContainerDefs decodes an ECS containerDefinitions value that is EITHER
// a JSON-encoded string (classic aws provider, like terraform) or a real JSON
// array (aws-native, like CloudFormation/live). It returns nil for an absent or
// empty value.
func pulumiECSContainerDefs(raw json.RawMessage) ([]ecsContainerDef, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	// A leading quote means the classic provider encoded the array as a string;
	// unquote it, then decode the inner JSON.
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		var defs []ecsContainerDef
		if err := json.Unmarshal([]byte(s), &defs); err != nil {
			return nil, err
		}
		return defs, nil
	}
	var defs []ecsContainerDef
	if err := json.Unmarshal(trimmed, &defs); err != nil {
		return nil, err
	}
	return defs, nil
}

// pulumiURNName extracts the resource name from a Pulumi URN
// (urn:pulumi:<stack>::<project>::<type>::<name>): the segment after the last
// "::". Returns "" for an empty URN.
func pulumiURNName(urn string) string {
	if strings.TrimSpace(urn) == "" {
		return ""
	}
	parts := strings.Split(urn, "::")
	return strings.TrimSpace(parts[len(parts)-1])
}
