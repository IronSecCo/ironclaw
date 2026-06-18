package sweep

import (
	"sync"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// respawnClock is an injectable, advanceable clock for backoff tests.
type respawnClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *respawnClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *respawnClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestRespawnBackoffDoublesAndCaps(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	clk := &respawnClock{t: t0}
	b := NewRespawnBackoff(time.Second, 8*time.Second, 100) // high ceiling: never park here
	b.now = clk.now

	want := []time.Duration{1, 2, 4, 8, 8, 8}
	for i, d := range want {
		st := b.Fail("s")
		if st.Failures != i+1 {
			t.Fatalf("failure %d: Failures=%d", i+1, st.Failures)
		}
		if st.Parked {
			t.Fatalf("failure %d: unexpectedly parked", i+1)
		}
		wantAt := t0.Add(d * time.Second)
		if !st.RetryAt.Equal(wantAt) {
			t.Fatalf("failure %d: RetryAt=%v, want %v (delay %v)", i+1, st.RetryAt, wantAt, d*time.Second)
		}
	}
}

func TestRespawnMaxRaisedToBase(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	clk := &respawnClock{t: t0}
	// max < base must be raised to base, so every delay equals base.
	b := NewRespawnBackoff(10*time.Second, time.Second, 100)
	b.now = clk.now
	st := b.Fail("s")
	st = b.Fail("s")
	if got, want := st.RetryAt, t0.Add(10*time.Second); !got.Equal(want) {
		t.Fatalf("RetryAt=%v, want %v (max raised to base)", got, want)
	}
}

func TestRespawnAllowWindow(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	clk := &respawnClock{t: t0}
	b := NewRespawnBackoff(10*time.Second, time.Minute, 100)
	b.now = clk.now

	b.Fail("s")
	ok, wait := b.Allow("s")
	if ok {
		t.Fatal("respawn allowed immediately after failure")
	}
	if wait != 10*time.Second {
		t.Fatalf("wait=%v, want 10s", wait)
	}

	clk.advance(9 * time.Second)
	if ok, _ := b.Allow("s"); ok {
		t.Fatal("respawn allowed before backoff elapsed")
	}
	clk.advance(time.Second) // now at t0+10s == retryAt
	if ok, wait := b.Allow("s"); !ok || wait != 0 {
		t.Fatalf("respawn not allowed at retryAt: ok=%v wait=%v", ok, wait)
	}
}

func TestRespawnParkAndEscalateOnce(t *testing.T) {
	var (
		mu       sync.Mutex
		calls    int
		lastID   contract.SessionID
		lastFail int
	)
	b := NewRespawnBackoff(time.Second, time.Minute, 3).OnEscalate(
		func(id contract.SessionID, failures int) {
			mu.Lock()
			calls++
			lastID, lastFail = id, failures
			mu.Unlock()
		})

	var st RespawnStatus
	for i := 0; i < 3; i++ {
		st = b.Fail("s")
	}
	if !st.Parked {
		t.Fatal("session not parked at ceiling")
	}
	if !st.RetryAt.IsZero() {
		t.Fatalf("parked status should have zero RetryAt, got %v", st.RetryAt)
	}
	if !b.Escalated("s") {
		t.Fatal("Escalated should be true after parking")
	}
	if ok, wait := b.Allow("s"); ok || wait != 0 {
		t.Fatalf("parked session must not be allowed: ok=%v wait=%v", ok, wait)
	}

	// Further failures keep counting but do NOT re-escalate.
	b.Fail("s")
	b.Fail("s")
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("escalation fired %d times, want exactly 1", calls)
	}
	if lastID != "s" || lastFail != 3 {
		t.Fatalf("escalation args = (%q,%d), want (s,3)", lastID, lastFail)
	}
	if got := b.Failures("s"); got != 5 {
		t.Fatalf("Failures=%d after 5 fails, want 5", got)
	}
}

func TestRespawnSucceedResets(t *testing.T) {
	b := NewRespawnBackoff(time.Second, time.Minute, 3)
	b.Fail("s")
	b.Fail("s")
	b.Succeed("s")
	if got := b.Failures("s"); got != 0 {
		t.Fatalf("Failures=%d after Succeed, want 0", got)
	}
	if b.Escalated("s") {
		t.Fatal("Escalated should be false after Succeed")
	}
	if ok, wait := b.Allow("s"); !ok || wait != 0 {
		t.Fatalf("respawn not allowed after Succeed: ok=%v wait=%v", ok, wait)
	}
}

func TestRespawnUnknownSessionAllowed(t *testing.T) {
	b := DefaultRespawnBackoff()
	if ok, wait := b.Allow("never-seen"); !ok || wait != 0 {
		t.Fatalf("unknown session not allowed: ok=%v wait=%v", ok, wait)
	}
	if got := b.Failures("never-seen"); got != 0 {
		t.Fatalf("Failures=%d for unknown, want 0", got)
	}
	if b.Escalated("never-seen") {
		t.Fatal("unknown session should not be escalated")
	}
}

func TestRespawnDefaultsApplied(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	clk := &respawnClock{t: t0}
	b := NewRespawnBackoff(0, 0, 0) // all invalid → defaults
	b.now = clk.now

	st := b.Fail("s")
	if !st.RetryAt.Equal(t0.Add(RespawnBaseDelay)) {
		t.Fatalf("default base not applied: RetryAt=%v", st.RetryAt)
	}
	// Default ceiling is RespawnFailureCeiling.
	for i := 1; i < RespawnFailureCeiling; i++ {
		st = b.Fail("s")
	}
	if !st.Parked {
		t.Fatalf("not parked after %d failures (default ceiling)", RespawnFailureCeiling)
	}
}

func TestRespawnConcurrentDistinctSessions(t *testing.T) {
	b := NewRespawnBackoff(time.Second, time.Minute, 1000)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		id := contract.SessionID(string(rune('a' + i%26)))
		wg.Add(1)
		go func(id contract.SessionID) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				b.Fail(id)
				b.Allow(id)
			}
		}(id)
	}
	wg.Wait()
	// 40 goroutines over 26 distinct ids; total failures == 40*25.
	total := 0
	for r := 'a'; r <= 'z'; r++ {
		total += b.Failures(contract.SessionID(string(r)))
	}
	if total != 40*25 {
		t.Fatalf("total failures=%d, want %d", total, 40*25)
	}
}
