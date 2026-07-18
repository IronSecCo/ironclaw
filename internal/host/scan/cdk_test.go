package scan

import (
	"reflect"
	"testing"
)

// TestCDKParityWithCloudFormation locks the core contract of the --cdk mode: because
// `cdk synth` emits a standard CloudFormation template, the SAME synthesized template
// must grade IDENTICALLY through the --cdk entry point (SpecsFromCDK / AggregateCDK)
// and the sibling --cloudformation entry point (SpecsFromCloudFormation /
// AggregateCloudFormation) — the only allowed difference is the reported Source label
// (and the extra "synthesized from CDK" note). If the two ever diverge, the cdk path
// has grown its own scoring behaviour and the parity guarantee is broken.
func TestCDKParityWithCloudFormation(t *testing.T) {
	for _, tc := range []struct {
		name string
		tmpl string
	}{
		{"hardened_yaml", cfnYAMLHardened},
		{"porous_json", cfnJSONPorous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cdkSpecs, cdkIntr, cdkErr := SpecsFromCDK([]byte(tc.tmpl))
			cfnSpecs, cfnIntr, cfnErr := SpecsFromCloudFormation([]byte(tc.tmpl))
			if cdkErr != nil || cfnErr != nil {
				t.Fatalf("parse errors: cdk=%v cfn=%v", cdkErr, cfnErr)
			}
			if cdkIntr != cfnIntr {
				t.Fatalf("intrinsic-skipped flag differs: cdk=%v cfn=%v", cdkIntr, cfnIntr)
			}
			if !reflect.DeepEqual(cdkSpecs, cfnSpecs) {
				t.Fatalf("specs diverge between cdk and cloudformation entry points:\ncdk=%+v\ncfn=%+v", cdkSpecs, cfnSpecs)
			}

			cdkReport, cdkWorst, cdkErr := AggregateCDK(cdkSpecs, "example")
			cfnReport, cfnWorst, cfnErr := AggregateCloudFormation(cfnSpecs, "example")
			if cdkErr != nil || cfnErr != nil {
				t.Fatalf("aggregate errors: cdk=%v cfn=%v", cdkErr, cfnErr)
			}
			if cdkReport.Score != cfnReport.Score || cdkReport.Grade != cfnReport.Grade {
				t.Fatalf("grade diverges: cdk=%d/%s cfn=%d/%s", cdkReport.Score, cdkReport.Grade, cfnReport.Score, cfnReport.Grade)
			}
			if !reflect.DeepEqual(cdkWorst, cfnWorst) {
				t.Fatalf("worst spec diverges between cdk and cloudformation")
			}
			// The one intentional difference: the Source label names the input mode.
			if cdkReport.Source != "cdk" {
				t.Fatalf("cdk report Source = %q, want %q", cdkReport.Source, "cdk")
			}
			if cfnReport.Source != "cloudformation" {
				t.Fatalf("cloudformation report Source = %q, want %q", cfnReport.Source, "cloudformation")
			}
		})
	}
}

// TestCDKAggregateEmptyFailsClosed confirms a synthesized template with no gradeable
// ECS container is an error (fail-closed), not a silent pass.
func TestCDKAggregateEmptyFailsClosed(t *testing.T) {
	if _, _, err := AggregateCDK(nil, "empty"); err == nil {
		t.Fatal("expected an error for zero gradeable containers, got nil")
	}
}

// TestCDKPorousFailClosed pins that a synthesized porous task def (root, privileged,
// host namespaces, docker.sock) grades identically to the same CloudFormation input
// and lands at the fail-closed floor.
func TestCDKPorousGrade(t *testing.T) {
	specs, _, err := SpecsFromCDK([]byte(cfnJSONPorous))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	report, _, err := AggregateCDK(specs, "cdk.out")
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if report.Source != "cdk" {
		t.Fatalf("Source = %q, want cdk", report.Source)
	}
	if report.Grade != "F" {
		t.Fatalf("porous synthesized CDK task def = %d/%s, want grade F", report.Score, report.Grade)
	}
}
