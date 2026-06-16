// OWNER: AGENT1

// Package sweep runs the periodic maintenance loop: stale-sandbox detection via
// heartbeat file mtime, due-message wake, recurrence expansion, and orphan reset
// with backoff.
package sweep

import (
	"context"
	"errors"
)

// StuckAction is the decision the sweep loop takes for a sandbox that may be
// stuck.
type StuckAction int

const (
	// None: the sandbox is healthy; take no action.
	None StuckAction = iota
	// KillCeiling: the heartbeat is older than the absolute ceiling — the sandbox
	// is presumed dead/hung and must be killed regardless of claim state.
	KillCeiling
	// KillClaim: a message claim has been held too long while the heartbeat is
	// also stale — the sandbox is stuck on a single message; kill so the host can
	// reset the claim and respawn.
	KillClaim
)

// Thresholds (milliseconds). A heartbeat older than HeartbeatCeilingMs means the
// sandbox is presumed dead. A claim older than ClaimStaleMs paired with a stale
// (but not yet dead) heartbeat means the sandbox is stuck on one message.
const (
	// HeartbeatCeilingMs is the absolute heartbeat age (30 minutes) past which the
	// sandbox is killed unconditionally.
	HeartbeatCeilingMs int64 = 30 * 60 * 1000
	// ClaimStaleMs is the per-message claim age (60 seconds) that, combined with a
	// stale heartbeat, indicates a stuck message rather than a healthy long task.
	ClaimStaleMs int64 = 60 * 1000
	// HeartbeatStaleMs is the heartbeat age (also 60s) above which a long-held
	// claim is treated as stuck rather than legitimately in-progress.
	HeartbeatStaleMs int64 = 60 * 1000
)

// DecideStuckAction is the pure decision function for the sweep loop. Given the
// age of the sandbox's heartbeat and the age of its oldest outstanding message
// claim (both in milliseconds; a negative age means "unknown / not present"), it
// returns the action to take.
//
// Precedence:
//  1. heartbeat age > HeartbeatCeilingMs            → KillCeiling
//  2. oldest claim age > ClaimStaleMs AND
//     heartbeat age > HeartbeatStaleMs              → KillClaim
//  3. otherwise                                     → None
//
// A healthy sandbox can legitimately hold a claim for a long time as long as it
// keeps heart-beating; only a stale heartbeat distinguishes "stuck" from "busy".
func DecideStuckAction(heartbeatAgeMs, oldestClaimAgeMs int64) StuckAction {
	if heartbeatAgeMs > HeartbeatCeilingMs {
		return KillCeiling
	}
	if oldestClaimAgeMs > ClaimStaleMs && heartbeatAgeMs > HeartbeatStaleMs {
		return KillClaim
	}
	return None
}

// Sweeper runs the periodic maintenance loop.
type Sweeper struct{}

// New constructs a Sweeper.
func New() *Sweeper { return &Sweeper{} }

// Run executes the sweep loop until ctx is cancelled.
//
// Full flow (gated on the central-DB binding): on each tick, for every active
// session — stat the heartbeat file, compute its age and the oldest claim age,
// call DecideStuckAction, and on KillCeiling/KillClaim kill the sandbox via
// host/isolation and reset orphaned claims with backoff; then wake sessions with
// due (scheduled/recurring) messages and expand recurrences.
func (s *Sweeper) Run(ctx context.Context) error {
	return errors.New("host/sweep: Run not implemented — gated on central-DB binding")
}
