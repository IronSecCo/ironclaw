// OWNER: AGENT1

package parity

import "testing"

// TestGatewayMandatoryApproval asserts the behavioral contract: every
// control-plane mutation is held pending a human decision under the v1
// AlwaysRequireHuman verifier — there is no file-edit path.
func TestGatewayMandatoryApproval(t *testing.T) {
	t.Skip("AGENT1: implement gateway mandatory-approval parity spec")
}
