package scan

import (
	"reflect"
	"testing"
)

// TestBicepARMParityWithAzure locks the core contract of the --bicep mode: because
// Bicep transpiles 1:1 to ARM, the SAME compiled ARM document must grade IDENTICALLY
// through the --bicep entry point (SpecsFromBicepARM / AggregateBicep) and the sibling
// --azure entry point (SpecsFromAzure / AggregateAzure) — the only allowed difference
// is the reported Source label. If the two ever diverge, the bicep path has grown its
// own scoring behaviour and the parity guarantee is broken.
func TestBicepARMParityWithAzure(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  string
	}{
		{"hardened", armHardened},
		{"porous_show", aciShowPorous},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bicepSpecs, bicepExpr, berr := SpecsFromBicepARM([]byte(tc.arm))
			azSpecs, azExpr, aerr := SpecsFromAzure([]byte(tc.arm))
			if berr != nil || aerr != nil {
				t.Fatalf("parse errors: bicep=%v azure=%v", berr, aerr)
			}
			if bicepExpr != azExpr {
				t.Fatalf("expression-skipped flag differs: bicep=%v azure=%v", bicepExpr, azExpr)
			}
			if !reflect.DeepEqual(bicepSpecs, azSpecs) {
				t.Fatalf("specs diverge between bicep and azure entry points:\nbicep=%+v\nazure=%+v", bicepSpecs, azSpecs)
			}

			bReport, bWorst, berr := AggregateBicep(bicepSpecs, "example")
			aReport, aWorst, aerr := AggregateAzure(azSpecs, "example")
			if berr != nil || aerr != nil {
				t.Fatalf("aggregate errors: bicep=%v azure=%v", berr, aerr)
			}
			if bReport.Score != aReport.Score || bReport.Grade != aReport.Grade {
				t.Fatalf("grade diverges: bicep=%d/%s azure=%d/%s", bReport.Score, bReport.Grade, aReport.Score, aReport.Grade)
			}
			if !reflect.DeepEqual(bWorst, aWorst) {
				t.Fatalf("worst spec diverges between bicep and azure")
			}
			// The one intentional difference: the Source label names the input mode.
			if bReport.Source != "bicep" {
				t.Fatalf("bicep report Source = %q, want %q", bReport.Source, "bicep")
			}
			if aReport.Source != "azure" {
				t.Fatalf("azure report Source = %q, want %q", aReport.Source, "azure")
			}
		})
	}
}

// TestBicepHardenedGrade pins the concrete grade a fully hardened, compiled-Bicep ACI
// container earns: the same honest 79/100 (grade B) ceiling as --azure — one dimension
// (read-only rootfs, which ACI's securityContext cannot express) below Cloud Run's
// 89/B.
func TestBicepHardenedGrade(t *testing.T) {
	specs, _, err := SpecsFromBicepARM([]byte(armHardened))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	report, _, err := AggregateBicep(specs, "webgroup.bicep")
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if report.Score != 79 || report.Grade != "B" {
		t.Fatalf("hardened compiled-bicep ACI = %d/%s, want 79/B", report.Score, report.Grade)
	}
	if report.Source != "bicep" {
		t.Fatalf("Source = %q, want bicep", report.Source)
	}
}

// TestBicepAggregateEmptyFailsClosed confirms an ARM document with no gradeable ACI
// container is an error (fail-closed), not a silent pass.
func TestBicepAggregateEmptyFailsClosed(t *testing.T) {
	if _, _, err := AggregateBicep(nil, "empty"); err == nil {
		t.Fatal("expected an error for zero gradeable containers, got nil")
	}
}
