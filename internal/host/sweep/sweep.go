// OWNER: AGENT1

// Package sweep runs the periodic maintenance loop: stale-sandbox detection via
// heartbeat file mtime, due-message wake, recurrence expansion, and orphan reset
// with backoff.
package sweep

import (
	"context"
	"fmt"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/scheduling"
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

// Prober reports the liveness signals for a session: the age (ms) of its
// heartbeat file and the age (ms) of its oldest outstanding message claim. A
// negative age means "unknown / not present". Tests inject a fake; production
// stats the heartbeat file and reads the processing acks.
type Prober interface {
	Probe(contract.SessionID) (heartbeatAgeMs, oldestClaimAgeMs int64, err error)
}

// Killer terminates the sandbox for a session (and lets the host reset orphaned
// claims and respawn). Tests inject a fake; production wires it to host/isolation.
type Killer interface {
	Kill(id contract.SessionID, action StuckAction) error
}

// Waker wakes (launches/signals) the sandbox for a session whose due message has
// come up. Tests inject a fake; production wires it to host/isolation.
type Waker interface {
	Wake(id contract.SessionID) error
}

// DueMessage is a message that has come due for a session. It carries just enough
// for the sweep to wake the session and, if recurring, re-enqueue the next
// occurrence. There is NO script/command field — a due message is only a prompt.
type DueMessage struct {
	SessionID  contract.SessionID
	MessageID  contract.MessageID
	Prompt     string
	RunAt      time.Time
	Recurrence string // "" for one-shot
}

// DueSource reports messages whose process_after <= now across all sessions.
// Tests inject a fake; production reads the per-session inbound queues.
type DueSource interface {
	DueMessages(now time.Time) ([]DueMessage, error)
}

// EnqueueFunc re-enqueues the next occurrence of a recurring due message at its
// computed next run time. Production wires it to host/queue.OpenInbound (writing a
// future MessageIn); tests inject a fake. It carries only a prompt — no execution.
type EnqueueFunc func(sessionID contract.SessionID, prompt string, runAt time.Time, recurrence string) error

// Sweeper runs the periodic maintenance loop over the registry's sessions.
type Sweeper struct {
	reg    registry.Registry
	prober Prober
	killer Killer

	// Optional scheduling hooks. When all three are set, Run also wakes due-message
	// sessions and re-enqueues recurring ones. They are optional so the stale-sandbox
	// sweep works on its own.
	dueSource DueSource
	waker     Waker
	enqueue   EnqueueFunc
}

// New constructs a Sweeper over the registry, prober, and killer.
func New(reg registry.Registry, prober Prober, killer Killer) *Sweeper {
	return &Sweeper{reg: reg, prober: prober, killer: killer}
}

// WithScheduling wires the due-message wake + recurrence hooks. Returns s for
// chaining. All three are required for the scheduling pass to run.
func (s *Sweeper) WithScheduling(due DueSource, waker Waker, enqueue EnqueueFunc) *Sweeper {
	s.dueSource = due
	s.waker = waker
	s.enqueue = enqueue
	return s
}

// Run performs one sweep pass: for every session, probe its liveness, call
// DecideStuckAction, and on KillCeiling/KillClaim kill the sandbox via the
// injected Killer. A healthy session is left alone.
//
// Run is the orchestration unit and is safe to call on a ticker; it returns the
// first error it encounters. (Due-message wake and recurrence expansion attach to
// this same pass once the scheduling tables land.)
func (s *Sweeper) Run(ctx context.Context) error {
	if s.reg == nil || s.prober == nil || s.killer == nil {
		return fmt.Errorf("host/sweep: Run requires a registry, prober, and killer")
	}
	sessions, err := s.reg.ListSessions()
	if err != nil {
		return fmt.Errorf("host/sweep: list sessions: %w", err)
	}
	for _, sess := range sessions {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		hb, claim, err := s.prober.Probe(sess.ID)
		if err != nil {
			return fmt.Errorf("host/sweep: probe %s: %w", sess.ID, err)
		}
		action := DecideStuckAction(hb, claim)
		if action == None {
			continue
		}
		if err := s.killer.Kill(sess.ID, action); err != nil {
			return fmt.Errorf("host/sweep: kill %s: %w", sess.ID, err)
		}
	}
	// Due-message wake + recurrence (only when the scheduling hooks are wired).
	if err := s.processDue(ctx); err != nil {
		return err
	}
	return nil
}

// processDue wakes every session that has a message due now and, for a recurring
// due message, re-enqueues the next occurrence via the enqueue hook. It is a no-op
// unless the scheduling hooks are wired. A due message carries only a prompt — the
// sweep never executes anything; waking the session lets the sandbox pick the
// prompt up as an ordinary inbound message.
func (s *Sweeper) processDue(ctx context.Context) error {
	if s.dueSource == nil || s.waker == nil || s.enqueue == nil {
		return nil
	}
	now := time.Now().UTC()
	due, err := s.dueSource.DueMessages(now)
	if err != nil {
		return fmt.Errorf("host/sweep: due messages: %w", err)
	}
	for _, m := range due {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := s.waker.Wake(m.SessionID); err != nil {
			return fmt.Errorf("host/sweep: wake %s for due message %s: %w", m.SessionID, m.MessageID, err)
		}
		if m.Recurrence == "" {
			continue
		}
		next, ok := scheduling.NextRun(m.RunAt, m.Recurrence)
		if !ok {
			// Invalid/exhausted recurrence: do not re-enqueue.
			continue
		}
		if err := s.enqueue(m.SessionID, m.Prompt, next, m.Recurrence); err != nil {
			return fmt.Errorf("host/sweep: re-enqueue recurring message for %s: %w", m.SessionID, err)
		}
	}
	return nil
}
