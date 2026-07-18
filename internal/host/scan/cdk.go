package scan

// SpecsFromCDK grades an AWS CDK application by way of the CloudFormation template
// its `cdk synth` step emits. The AWS CDK is a program (TypeScript/Python/Go/...) that
// SYNTHESIZES a standard CloudFormation template — `cdk synth` writes exactly the
// AWS::ECS::TaskDefinition (and every other) resource the CloudFormation deployer
// consumes, in ordinary CFN JSON/YAML. A CDK app that declares an ECS task definition
// therefore compiles to the SAME CloudFormation document the --cloudformation path
// already grades. This is a deliberately thin seam over SpecsFromCloudFormation: the
// CLI wrapper runs `cdk synth` (the I/O) and passes the synthesized template bytes
// here, which decode through the identical shared ecsSpec mapper, PascalCase
// case-insensitive handling, and intrinsic/token fail-closed nullification.
//
// Keeping it a named entry point (rather than calling SpecsFromCloudFormation directly
// from the CLI) gives the --cdk mode a stable seam a parity test can pin against the
// sibling --cloudformation mode: the same synthesized template must grade identically
// through both. It mirrors the --bicep -> --azure seam (compile a higher-level IaC
// input down to the native template, then reuse the native scorer unchanged).
//
// The second return value reports whether any CloudFormation intrinsic / unresolved
// CDK token was nullified (graded fail-closed) so the caller can note it.
func SpecsFromCDK(synthesizedTemplate []byte) ([]Spec, bool, error) {
	return SpecsFromCloudFormation(synthesizedTemplate)
}

// AggregateCDK folds the per-container specs from one or more synthesized-CDK
// CloudFormation templates into a single Report. It is the AggregateCloudFormation
// WEAKEST-container rollup (a template is only as isolated as its most-exposed
// container) with the Source re-labelled "cdk" so the report names the input mode the
// user actually ran, plus a note recording that the grade is over synthesized CFN. It
// is pure; the caller injects Version/GeneratedAt.
func AggregateCDK(specs []Spec, target string) (Report, Spec, error) {
	report, worst, err := AggregateCloudFormation(specs, target)
	if err != nil {
		return Report{}, Spec{}, err
	}
	report.Source = "cdk"
	report.Notes = append([]string{
		"input synthesized from an AWS CDK app to a CloudFormation template (cdk synth); grading is identical to the --cloudformation ECS path once synthesized",
	}, report.Notes...)
	return report, worst, nil
}
