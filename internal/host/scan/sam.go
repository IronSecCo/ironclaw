package scan

// SpecsFromSAM grades the container workloads declared in an AWS SAM (Serverless
// Application Model) template. A SAM template carries `Transform: AWS::Serverless-*`
// and, when deployed, is expanded to CloudFormation by the transform macro — but SAM
// is a strict SUPERSET of CloudFormation: any resource that is not an
// AWS::Serverless::* type (an AWS::ECS::TaskDefinition, for instance) passes through
// the transform UNCHANGED, in ordinary CloudFormation form. An ECS/Fargate task
// definition declared in a SAM template is therefore the SAME document node the
// --cloudformation path already grades, so this is a deliberately thin seam over
// SpecsFromCloudFormation: the SAM template bytes decode through the identical shared
// ecsSpec mapper, PascalCase case-insensitive handling, and intrinsic fail-closed
// nullification. No `sam build`/transform step is required to reach the ECS task
// defs — they are already native CloudFormation in the source template.
//
// Keeping it a named entry point (rather than calling SpecsFromCloudFormation directly
// from the CLI) gives the --sam mode a stable seam a parity test can pin against the
// sibling --cloudformation mode: the same template must grade identically through both.
// It mirrors the --cdk -> --cloudformation and --bicep -> --azure seams (route a
// higher-level IaC input through the native template scorer unchanged).
//
// Scope note: the container-isolation scorer grades AWS::ECS::TaskDefinition resources.
// AWS::Serverless::Function resources (including PackageType: Image Lambda-container
// functions) run on the managed Lambda runtime, which exposes none of the ECS
// task-definition isolation fields (privileged, user, readonlyRootFilesystem, host
// namespaces) this scorer grades, so they are not graded here — the real,
// user-controllable container-isolation surface in a SAM app is its ECS/Fargate task
// definitions.
//
// The second return value reports whether any CloudFormation intrinsic was nullified
// (graded fail-closed) so the caller can note it.
func SpecsFromSAM(raw []byte) ([]Spec, bool, error) {
	return SpecsFromCloudFormation(raw)
}

// AggregateSAM folds the per-container specs from one or more SAM templates into a
// single Report. It is the AggregateCloudFormation WEAKEST-container rollup (a template
// is only as isolated as its most-exposed container) with the Source re-labelled "sam"
// so the report names the input mode the user actually ran, plus a note recording that
// the grade is over the SAM template's ECS/Fargate task definitions. It is pure; the
// caller injects Version/GeneratedAt.
func AggregateSAM(specs []Spec, target string) (Report, Spec, error) {
	report, worst, err := AggregateCloudFormation(specs, target)
	if err != nil {
		return Report{}, Spec{}, err
	}
	report.Source = "sam"
	report.Notes = append([]string{
		"input is an AWS SAM template (Transform: AWS::Serverless-*); grading covers its AWS::ECS::TaskDefinition resources through the identical --cloudformation ECS path (SAM is a CloudFormation superset)",
	}, report.Notes...)
	return report, worst, nil
}
