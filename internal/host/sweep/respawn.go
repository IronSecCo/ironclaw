// OWNER: AGENT1

package sweep

import (
	"sync"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// Crash-loop backoff defaults. A freshly-crashed session waits RespawnBaseDelay
// before its first respawn, doubling each consecutive failure up to
// RespawnMaxDelay, and is parked (needs-human) after RespawnFailureCeiling
// consecutive failures.
const (
	RespawnBaseDelay      = 1 * time.Second
	RespawnMaxDelay       = 5 * time.Minute
	RespawnFailureCeiling = 5
)

// RespawnStatus is the result of recording a failure via RespawnBackoff.Fail.
type RespawnStatus struct {
	Failures int       // consecutive failures recorded so far
	Parked   bool      // true once Failures >= ceiling: stop auto-respawning (needs-human)
	RetryAt  time.Time // earliest time a respawn is allowed; zero when Parked
}

type respawnState struct {
	failures int
	retryAt  time.Time
	parked   bool
}

// RespawnBackoff applies per-session exponential backoff to sandbox respawns so a
// crash-looping (for example, a misconfigured image) cannot hammer the host, and
// parks a session for human attention after a ceiling of consecutive failures.
//
// Lifecycle, as the host respawn path would use it: on a sandbox crash call Fail;
// before respawning consult Allow (honor the returned wait, and never respawn a
// parked session); after a healthy run call Succeed to reset. It is safe for
// concurrent use.
type RespawnBackoff struct {
	base, max  time.Duration
	ceiling    int
	now        func() time.Time
	onEscalate func(contract.SessionID, int)

	mu       sync.Mutex
	sessions map[contract.SessionID]*respawnState
}

// NewRespawnBackoff builds a backoff with the given first-failure delay, maximum
// delay, and consecutive-failure ceiling. Non-positive base/max or ceiling < 1
// fall back to the Respawn* defaults; max is raised to base when smaller.
func NewRespawnBackoff(base, max time.Duration, ceiling int) *RespawnBackoff {
	if base <= 0 {
		base = RespawnBaseDelay
	}
	if max <= 0 {
		max = RespawnMaxDelay
	}
	if max < base {
		max = base
	}
	if ceiling < 1 {
		ceiling = RespawnFailureCeiling
	}
	return &RespawnBackoff{
		base:     base,
		max:      max,
		ceiling:  ceiling,
		now:      time.Now,
		sessions: make(map[contract.SessionID]*respawnState),
	}
}

// DefaultRespawnBackoff uses the Respawn* default tuning.
func DefaultRespawnBackoff() *RespawnBackoff {
	return NewRespawnBackoff(RespawnBaseDelay, RespawnMaxDelay, RespawnFailureCeiling)
}

// OnEscalate sets a callback fired exactly once when a session first reaches the
// failure ceiling (transitions to parked). Wire it to an alert / needs-human
// sink. Returns b for chaining.
func (b *RespawnBackoff) OnEscalate(fn func(id contract.SessionID, failures int)) *RespawnBackoff {
	b.mu.Lock()
	b.onEscalate = fn
	b.mu.Unlock()
	return b
}

// Fail records a sandbox failure for the session and returns the resulting
// status. Below the ceiling it schedules the next allowed respawn at
// now + backoff(failures); at or above the ceiling it parks the session and (on
// the first crossing) fires the escalation callback.
func (b *RespawnBackoff) Fail(id contract.SessionID) RespawnStatus {
	b.mu.Lock()
	st := b.sessions[id]
	if st == nil {
		st = &respawnState{}
		b.sessions[id] = st
	}
	st.failures++

	var escalate func()
	if st.failures >= b.ceiling {
		if !st.parked && b.onEscalate != nil {
			failures, cb := st.failures, b.onEscalate
			escalate = func() { cb(id, failures) }
		}
		st.parked = true
		st.retryAt = time.Time{}
	} else {
		st.retryAt = b.now().Add(b.delayFor(st.failures))
	}
	status := RespawnStatus{Failures: st.failures, Parked: st.parked, RetryAt: st.retryAt}
	b.mu.Unlock()

	if escalate != nil {
		escalate() // fire outside the lock so the sink can call back in
	}
	return status
}

// Allow reports whether the session's sandbox may be respawned now. It returns
// (true, 0) for a session with no recorded failures or whose backoff has elapsed,
// (false, wait) while backoff is still pending, and (false, 0) when the session is
// parked (needs-human).
func (b *RespawnBackoff) Allow(id contract.SessionID) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.sessions[id]
	if st == nil {
		return true, 0
	}
	if st.parked {
		return false, 0
	}
	now := b.now()
	if !now.Before(st.retryAt) {
		return true, 0
	}
	return false, st.retryAt.Sub(now)
}

// Succeed clears any failure state for the session after a healthy respawn, so
// the next failure starts the backoff over and a parked session is un-parked.
func (b *RespawnBackoff) Succeed(id contract.SessionID) {
	b.mu.Lock()
	delete(b.sessions, id)
	b.mu.Unlock()
}

// Escalated reports whether the session is parked (exceeded the failure ceiling).
func (b *RespawnBackoff) Escalated(id contract.SessionID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.sessions[id]
	return st != nil && st.parked
}

// Failures returns the current consecutive-failure count for the session.
func (b *RespawnBackoff) Failures(id contract.SessionID) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if st := b.sessions[id]; st != nil {
		return st.failures
	}
	return 0
}

// delayFor computes the backoff for the nth consecutive failure (n >= 1):
// base * 2^(n-1), capped at max and guarded against int64 overflow.
func (b *RespawnBackoff) delayFor(n int) time.Duration {
	d := b.base
	for i := 1; i < n; i++ {
		d *= 2
		if d <= 0 || d >= b.max { // overflow or cap reached
			return b.max
		}
	}
	if d > b.max {
		return b.max
	}
	return d
}
