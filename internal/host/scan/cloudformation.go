package scan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SpecsFromCloudFormation parses an AWS CloudFormation template (YAML or JSON)
// and returns one graded Spec per container definition of every
// AWS::ECS::TaskDefinition resource it declares. It is the CFN counterpart to the
// terraform aws_ecs_task_definition path (SpecsFromTerraform) and the live
// registered-JSON path (SpecsFromECS): all three decode into the SAME ecsTaskDef /
// ecsContainerDef and grade through the SAME ecsSpec mapper, so the entrypoints can
// never diverge.
//
// The reuse is exact: a CloudFormation ECS task definition is the AWS ECS task
// definition shape with PascalCase property names (ContainerDefinitions, Privileged,
// ReadonlyRootFilesystem, User, LinuxParameters, DockerSecurityOptions, NetworkMode,
// PidMode, IpcMode, Volumes...). Go's encoding/json matches object keys
// case-insensitively, so each task-def's Properties subtree is re-marshaled to JSON
// and decoded straight into ecsTaskDef (whose json tags are the camelCase live-ECS
// names) — no parallel PascalCase struct, and byte-for-byte the same grading.
//
// CloudFormation intrinsics (!Ref / !Sub / !GetAtt / Fn::* / the {"Ref": ...} long
// form) are tolerated best-effort: an intrinsic node is unresolvable without the
// full stack context, so it is nullified before decoding. A graded field it covered
// therefore decodes as unset and is graded fail-closed (an unknown posture is
// insecure), and the second return value reports whether any intrinsic was skipped
// so the caller can note it. This keeps the parser fail-OPEN: a template peppered
// with intrinsics still grades the parts it can resolve rather than erroring out.
//
// It is pure and unit-testable: the caller reads the file (I/O) and passes the
// bytes here. It fails OPEN on a malformed top-level document (returns the parse
// error); the CLI wrapper turns that into a skip so an opt-in CI step never crashes
// the build. A template with no AWS::ECS::TaskDefinition resource returns no specs.
func SpecsFromCloudFormation(raw []byte) ([]Spec, bool, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, false, fmt.Errorf("parse cloudformation template: %w", err)
	}
	// yaml.Unmarshal into a Node yields a DocumentNode wrapping the root mapping.
	doc := &root
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil, false, nil
		}
		doc = root.Content[0]
	}

	resources := cfnMappingValue(doc, "Resources")
	if resources == nil || resources.Kind != yaml.MappingNode {
		return nil, false, nil
	}

	var specs []Spec
	skipped := 0
	// A CloudFormation Resources block is a mapping of logical-id -> resource; a
	// template may declare several ECS task definitions, so we pool every one and
	// let AggregateCloudFormation grade the weakest container across the lot.
	for i := 0; i+1 < len(resources.Content); i += 2 {
		resVal := resources.Content[i+1]
		typeNode := cfnMappingValue(resVal, "Type")
		if typeNode == nil || typeNode.Value != "AWS::ECS::TaskDefinition" {
			continue
		}
		props := cfnMappingValue(resVal, "Properties")
		if props == nil {
			continue
		}
		// Nullify unresolvable intrinsics so the typed decode below never fails on a
		// !Ref/Fn:: where a scalar/bool/array was expected (best-effort tolerance).
		skipped += sanitizeCFNIntrinsics(props)

		var m map[string]interface{}
		if err := props.Decode(&m); err != nil {
			// Fail-open per resource: a single undecodable Properties block must not
			// sink the whole template.
			continue
		}
		js, err := json.Marshal(m)
		if err != nil {
			continue
		}
		ss, err := specsFromCFNTaskDef(js)
		if err != nil {
			continue
		}
		specs = append(specs, ss...)
	}
	return specs, skipped > 0, nil
}

// specsFromCFNTaskDef decodes one AWS::ECS::TaskDefinition's Properties (already
// intrinsic-sanitized and re-marshaled to JSON) into the SHARED ecsTaskDef and
// returns a graded Spec per container definition, keyed with source
// "cloudformation". It mirrors SpecsFromECS's body exactly (task-level modes +
// docker.sock host volume fold into every container) so the CFN grade is identical
// to the live/terraform grade of the same task definition.
func specsFromCFNTaskDef(propsJSON []byte) ([]Spec, error) {
	var td ecsTaskDef
	// Case-insensitive key matching maps CloudFormation's PascalCase properties
	// (ContainerDefinitions, Privileged, ...) onto ecsTaskDef's camelCase json tags.
	if err := json.Unmarshal(propsJSON, &td); err != nil {
		return nil, err
	}
	if len(td.ContainerDefinitions) == 0 {
		return nil, nil
	}

	family := nz(td.Family, "task")

	// docker.sock exposure is a task-level property: an ECS host volume whose source
	// path is the control socket is mountable into any container in the task.
	dockerSock := No
	for _, vol := range td.Volumes {
		if vol.Host != nil && isControlSocket(vol.Host.SourcePath) {
			dockerSock = Yes
		}
	}

	modes := ecsTaskModes{NetworkMode: td.NetworkMode, PidMode: td.PidMode, IpcMode: td.IpcMode}
	specs := make([]Spec, 0, len(td.ContainerDefinitions))
	for _, d := range td.ContainerDefinitions {
		specs = append(specs, ecsSpec("cloudformation", family, d, modes, dockerSock))
	}
	return specs, nil
}

// AggregateCloudFormation folds the per-container specs from a CloudFormation
// template's ECS task definitions into a single Report. Like AggregateECS /
// AggregateTerraform, the aggregate is the WEAKEST container (minimum score): a
// template is only as isolated as its most-exposed container, so grading the
// weakest link is the honest, fail-closed summary. target names the source
// (template file base, or a directory rollup label). It returns the aggregate
// Report and the Spec that produced it (for SARIF anchoring). It is pure; the
// caller injects Version/GeneratedAt. Fail-closed: an empty container set is an
// error (a template that declares no gradeable ECS container is not a pass).
func AggregateCloudFormation(specs []Spec, target string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable container definitions found in CloudFormation template(s) (looked for AWS::ECS::TaskDefinition resources with ContainerDefinitions)")
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
	agg.Source = "cloudformation"
	agg.Notes = append(cfnSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// cfnSummaryNotes builds the template-level roll-up notes for an aggregate
// CloudFormation report: a headline naming the weakest container and a
// per-container score list.
func cfnSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d CloudFormation ECS container(s); the template grade is the WEAKEST (a template is only as isolated as its most-exposed container). Weakest: %s at %d/100 (grade %s).",
			len(all), nz(worst.spec.Target, "container"), worst.report.Score, worst.report.Grade),
	}
	sorted := make([]scoredWorkload, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].report.Score < sorted[j].report.Score })
	rows := make([]string, len(sorted))
	for i, w := range sorted {
		rows[i] = fmt.Sprintf("%s = %d/100 (%s)", nz(w.spec.Target, "container"), w.report.Score, w.report.Grade)
	}
	notes = append(notes, "per-container: "+strings.Join(rows, "; "))
	return notes
}

// --------------------------------------------------------------------------- //
// CloudFormation intrinsic tolerance
//
// CloudFormation templates carry unresolvable references — !Ref, !Sub, !GetAtt,
// the Fn::* family, and their {"Ref": ...} / {"Fn::Sub": ...} long forms. Without
// the deployed stack we cannot resolve them, so we nullify each intrinsic node
// before the typed decode. A graded field an intrinsic covered then reads as unset
// and is graded fail-closed. This keeps decoding robust (a !Ref where a bool/array
// was expected never errors) and honest (an unknown posture scores as insecure).
// --------------------------------------------------------------------------- //

// cfnMappingValue returns the value node for key in a YAML mapping node, or nil.
func cfnMappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// sanitizeCFNIntrinsics walks a YAML node tree and replaces every CloudFormation
// intrinsic node (short-tag !Ref/!Sub/... on any node, or a single-key {"Ref": ...}
// / {"Fn::*": ...} / {"Condition": ...} mapping) with a null scalar. It returns the
// number of intrinsics nullified so the caller can note that some values were
// skipped. Nullifying (rather than erroring) is what makes the CFN parser fail-open.
func sanitizeCFNIntrinsics(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	if isIntrinsicNode(n) {
		nullifyNode(n)
		return 1
	}
	count := 0
	for _, c := range n.Content {
		count += sanitizeCFNIntrinsics(c)
	}
	return count
}

// isIntrinsicNode reports whether a node is a CloudFormation intrinsic: a scalar or
// collection carrying a short-form intrinsic tag (!Ref, !Sub, !If, ...), or a
// single-key mapping whose key is an intrinsic (Ref, Condition, or Fn::*).
func isIntrinsicNode(n *yaml.Node) bool {
	if isIntrinsicTag(n.Tag) {
		return true
	}
	if n.Kind == yaml.MappingNode && len(n.Content) == 2 &&
		n.Content[0].Kind == yaml.ScalarNode && isIntrinsicKey(n.Content[0].Value) {
		return true
	}
	return false
}

// isIntrinsicTag reports whether a YAML tag is a CloudFormation short-form
// intrinsic (!Ref, !Sub, !GetAtt, !If, ...). Standard resolved YAML tags are the
// "!!str"/"!!int"/"!!bool"/"!!null"/"!!map"/"!!seq" double-bang forms; a single
// leading "!" that is not "!!" is a custom (intrinsic) tag.
func isIntrinsicTag(tag string) bool {
	return len(tag) > 1 && tag[0] == '!' && tag[1] != '!'
}

// isIntrinsicKey reports whether a mapping key is a CloudFormation long-form
// intrinsic (Ref, Condition, or any Fn::* function). This covers JSON templates
// and YAML long-form intrinsics alike.
func isIntrinsicKey(k string) bool {
	return k == "Ref" || k == "Condition" || strings.HasPrefix(k, "Fn::")
}

// nullifyNode rewrites a node in place to the YAML null scalar, discarding any
// intrinsic value. A null decodes to a zero value (empty string / nil pointer / nil
// slice), i.e. the "unset" posture the scorer grades fail-closed.
func nullifyNode(n *yaml.Node) {
	n.Kind = yaml.ScalarNode
	n.Tag = "!!null"
	n.Value = ""
	n.Content = nil
}
