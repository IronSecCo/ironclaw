// OWNER: AGENT2

package parity

import "testing"

// TestSandboxOutboundSeqParity asserts the behavioral contract for the sandbox's
// side of the queue: every message the sandbox writes to the outbound queue is
// assigned an ODD seq (the host writes EVEN), monotonically increasing, so the
// two sides never collide without coordinating a counter (frozen seq parity,
// contract/schema.go). Black-box over the outbound DB once the encrypted binding
// lands; the sandbox poll loop (AGENT2) and the host reader (AGENT1) must both be
// wired for this to run end to end.
func TestSandboxOutboundSeqParity(t *testing.T) {
	t.Skip("AGENT2: implement sandbox outbound seq-parity parity spec (needs encrypted-queue binding)")
}

// TestSandboxAcksProcessing asserts the behavioral contract: when the sandbox
// engages a set of inbound messages it records a processing ack and, on
// completion, a completed ack in the outbound processing_ack table — the host's
// signal to advance inbound status. Black-box over the two queue DBs.
func TestSandboxAcksProcessing(t *testing.T) {
	t.Skip("AGENT2: implement sandbox processing-ack parity spec (needs encrypted-queue binding)")
}
