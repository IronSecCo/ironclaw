// OWNER: T-106

package loop

import (
	"errors"
	"math"
	"math/rand"
	"time"
)

// ErrProvider marks a poll failure that originated from the model provider (the
// host proxy or model API), as opposed to a local queue error. Run uses it to
// apply exponential backoff and circuit-breaking so a down model API is not
// hammered at the fixed poll interval (a thundering herd across every sandbox).
var ErrProvider = errors.New("sandbox/loop: provider error")

// providerErr tags err as a provider failure while preserving the original for
// errors.Is/As unwrapping.
func providerErr(err error) error { return errors.Join(ErrProvider, err) }

// Backoff defaults.
const (
	defaultProviderBackoffMax = 60 * time.Second
	defaultBreakerThreshold   = 5
	backoffFactor             = 2.0
)

// defaultJitter returns a pseudo-random fraction in [0,1). The global source is
// auto-seeded since Go 1.20, so no explicit seeding is needed; jitter only
// de-synchronises retries and does not require cryptographic randomness.
func defaultJitter() float64 { return rand.Float64() }

// backoff is an exponential-backoff retry pacer with an integrated circuit
// breaker. It is not safe for concurrent use; Run drives it from a single
// goroutine.
type backoff struct {
	base      time.Duration // first-failure delay (the poll interval)
	max       time.Duration // ceiling on any single delay
	factor    float64       // exponential growth per consecutive failure
	threshold int           // consecutive failures that trip the breaker open
	jitter    func() float64

	failures int
	open     bool
	logged   bool // whether the breaker-open transition has been logged
}

func newBackoff(base, max time.Duration, threshold int, jitter func() float64) *backoff {
	if max < base {
		max = base
	}
	if threshold < 1 {
		threshold = 1
	}
	if jitter == nil {
		jitter = defaultJitter
	}
	return &backoff{base: base, max: max, factor: backoffFactor, threshold: threshold, jitter: jitter}
}

// fail records one consecutive provider failure and returns the delay to wait
// before the next attempt: base * factor^(n-1), capped at max, with equal jitter
// (the delay is randomised within [d/2, d] so retries de-synchronise while
// always waiting at least half the computed interval). Once the failure streak
// reaches threshold the breaker is open.
func (b *backoff) fail() time.Duration {
	b.failures++
	d := float64(b.base) * math.Pow(b.factor, float64(b.failures-1))
	if d > float64(b.max) {
		d = float64(b.max)
	}
	half := d / 2
	delay := time.Duration(half + half*b.jitter())
	if b.failures >= b.threshold {
		b.open = true
	}
	return delay
}

// reset closes the breaker and clears the failure streak after a clean poll.
func (b *backoff) reset() {
	b.failures = 0
	b.open = false
	b.logged = false
}

// tripped reports whether the breaker is currently open.
func (b *backoff) tripped() bool { return b.open }

// consecutiveFailures returns the current provider failure streak length.
func (b *backoff) consecutiveFailures() int { return b.failures }
