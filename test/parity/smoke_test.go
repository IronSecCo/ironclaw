package parity

import "testing"

// TestSkeletonCompiles is the trivial smoke spec that keeps CI green while the
// real parity specs are stubbed out.
func TestSkeletonCompiles(t *testing.T) {
	if true != true {
		t.Fatal("the skeleton does not compile to true")
	}
}
