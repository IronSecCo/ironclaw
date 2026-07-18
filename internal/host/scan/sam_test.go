package scan

import (
	"reflect"
	"testing"
)

// TestSAMParityWithCloudFormation locks the core contract of the --sam mode: because a
// SAM template is a CloudFormation superset (its AWS::ECS::TaskDefinition resources are
// already native CFN), the SAME template must grade IDENTICALLY through the --sam entry
// point (SpecsFromSAM / AggregateSAM) and the sibling --cloudformation entry point
// (SpecsFromCloudFormation / AggregateCloudFormation) — the only allowed difference is
// the reported Source label (and the extra "SAM template" note). If the two ever
// diverge, the sam path has grown its own scoring behaviour and the parity guarantee is
// broken.
func TestSAMParityWithCloudFormation(t *testing.T) {
	for _, tc := range []struct {
		name string
		tmpl string
	}{
		{"hardened_yaml", cfnYAMLHardened},
		{"porous_json", cfnJSONPorous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samSpecs, samIntr, samErr := SpecsFromSAM([]byte(tc.tmpl))
			cfnSpecs, cfnIntr, cfnErr := SpecsFromCloudFormation([]byte(tc.tmpl))
			if samErr != nil || cfnErr != nil {
				t.Fatalf("parse errors: sam=%v cfn=%v", samErr, cfnErr)
			}
			if samIntr != cfnIntr {
				t.Fatalf("intrinsic-skipped flag differs: sam=%v cfn=%v", samIntr, cfnIntr)
			}
			if !reflect.DeepEqual(samSpecs, cfnSpecs) {
				t.Fatalf("specs diverge between sam and cloudformation entry points:\nsam=%+v\ncfn=%+v", samSpecs, cfnSpecs)
			}

			samReport, samWorst, samErr := AggregateSAM(samSpecs, "example")
			cfnReport, cfnWorst, cfnErr := AggregateCloudFormation(cfnSpecs, "example")
			if samErr != nil || cfnErr != nil {
				t.Fatalf("aggregate errors: sam=%v cfn=%v", samErr, cfnErr)
			}
			if samReport.Score != cfnReport.Score || samReport.Grade != cfnReport.Grade {
				t.Fatalf("grade diverges: sam=%d/%s cfn=%d/%s", samReport.Score, samReport.Grade, cfnReport.Score, cfnReport.Grade)
			}
			if !reflect.DeepEqual(samWorst, cfnWorst) {
				t.Fatalf("worst spec diverges between sam and cloudformation")
			}
			// The one intentional difference: the Source label names the input mode.
			if samReport.Source != "sam" {
				t.Fatalf("sam report Source = %q, want %q", samReport.Source, "sam")
			}
			if cfnReport.Source != "cloudformation" {
				t.Fatalf("cloudformation report Source = %q, want %q", cfnReport.Source, "cloudformation")
			}
		})
	}
}

// TestSAMAggregateEmptyFailsClosed confirms a SAM template with no gradeable ECS
// container is an error (fail-closed), not a silent pass.
func TestSAMAggregateEmptyFailsClosed(t *testing.T) {
	if _, _, err := AggregateSAM(nil, "empty"); err == nil {
		t.Fatal("expected an error for zero gradeable containers, got nil")
	}
}

// TestSAMTransformTemplateGrades pins that a SAM template (Transform header +
// AWS::Serverless::Function alongside a raw AWS::ECS::TaskDefinition) grades its ECS
// task definition through the seam: the serverless function is ignored and the porous
// task def lands at the fail-closed floor.
func TestSAMTransformTemplateGrades(t *testing.T) {
	specs, _, err := SpecsFromSAM([]byte(samPorousTemplate))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	report, _, err := AggregateSAM(specs, "template.yaml")
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if report.Source != "sam" {
		t.Fatalf("Source = %q, want sam", report.Source)
	}
	if report.Grade != "F" {
		t.Fatalf("porous SAM ECS task def = %d/%s, want grade F", report.Score, report.Grade)
	}
}

// samPorousTemplate is a SAM template with the AWS::Serverless-2016-10-31 transform, a
// Lambda-container serverless function (which the ECS scorer does not grade), and a
// porous raw AWS::ECS::TaskDefinition (root, privileged, host namespaces, docker.sock)
// that must be graded through the CloudFormation seam and land at the fail-closed floor.
const samPorousTemplate = `AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31
Resources:
  ImageFn:
    Type: AWS::Serverless::Function
    Properties:
      PackageType: Image
      ImageUri: 111122223333.dkr.ecr.us-east-1.amazonaws.com/app:latest
  PorousTask:
    Type: AWS::ECS::TaskDefinition
    Properties:
      NetworkMode: host
      PidMode: host
      IpcMode: host
      ContainerDefinitions:
        - Name: app
          Image: app:latest
          Privileged: true
          User: "0"
          MountPoints:
            - SourceVolume: docker-sock
              ContainerPath: /var/run/docker.sock
      Volumes:
        - Name: docker-sock
          Host:
            SourcePath: /var/run/docker.sock
`
