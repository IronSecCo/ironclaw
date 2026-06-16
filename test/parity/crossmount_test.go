// OWNER: AGENT1

package parity

import "testing"

// TestCrossMountLivePoll asserts the load-bearing behavioral contract: an inbound
// write made AFTER the sandbox has begun polling is observed within one poll
// interval across the bind mount. This validates the encrypted + DELETE-journal +
// mmap_size=0 + reopen-per-poll discipline (design-plan §1, §6). Shared spec; the
// host harness (AGENT1) and the sandbox poll loop (AGENT2) must both be wired.
func TestCrossMountLivePoll(t *testing.T) {
	t.Skip("AGENT1/AGENT2: implement cross-mount live-poll parity spec")
}
