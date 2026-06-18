package loop

import (
	"context"
	"errors"
	"io"
	"log"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func TestBackoffExponentialGrowthAndCap(t *testing.T) {
	// Jitter fixed at 1.0 -> delay == full computed interval d (equal-jitter band
	// upper edge), so growth is easy to assert.
	b := newBackoff(time.Second, 10*time.Second, 100, func() float64 { return 1 })

	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}
	for i, w := range want {
		got := b.fail()
		if got != w {
			t.Fatalf("fail #%d = %s, want %s (capped at max)", i+1, got, w)
		}
	}
}

func TestBackoffEqualJitterLowerBound(t *testing.T) {
	// Jitter 0 -> delay == d/2 (never zero, so retries always pause).
	b := newBackoff(time.Second, time.Minute, 100, func() float64 { return 0 })
	if got := b.fail(); got != 500*time.Millisecond {
		t.Fatalf("first delay with jitter 0 = %s, want 500ms", got)
	}
	if got := b.fail(); got != time.Second {
		t.Fatalf("second delay with jitter 0 = %s, want 1s (2s/2)", got)
	}
}

func TestBreakerTripsAtThresholdAndResets(t *testing.T) {
	b := newBackoff(time.Millisecond, time.Second, 3, func() float64 { return 0.5 })
	for i := 0; i < 2; i++ {
		b.fail()
		if b.tripped() {
			t.Fatalf("breaker tripped early after %d failures", i+1)
		}
	}
	b.fail() // 3rd failure hits threshold
	if !b.tripped() {
		t.Fatal("breaker should be open at threshold")
	}
	if b.consecutiveFailures() != 3 {
		t.Fatalf("consecutiveFailures = %d, want 3", b.consecutiveFailures())
	}
	b.reset()
	if b.tripped() || b.consecutiveFailures() != 0 {
		t.Fatalf("reset did not clear breaker: open=%v failures=%d", b.tripped(), b.consecutiveFailures())
	}
}

func TestBackoffMaxClampedToBase(t *testing.T) {
	// A max below base is raised to base so the first delay is never inverted.
	b := newBackoff(2*time.Second, time.Second, 5, func() float64 { return 1 })
	if got := b.fail(); got != 2*time.Second {
		t.Fatalf("first delay = %s, want 2s (max clamped up to base)", got)
	}
}

// errProvider always fails, counting calls atomically so a concurrent Run can be
// observed without a data race.
type errProvider struct{ calls int64 }

func (e *errProvider) Query(context.Context, string) (string, error) {
	atomic.AddInt64(&e.calls, 1)
	return "", errors.New("model API down")
}

func TestPollClassifiesProviderError(t *testing.T) {
	in := &fakeInbound{pending: []contract.MessageIn{msg("m1", "hi", 1)}}
	out := &fakeOutbound{}
	l, err := New(Config{
		Inbound: in, Outbound: out, Provider: &errProvider{},
		HeartbeatPath: filepath.Join(t.TempDir(), "hb"),
		Clock:         func() time.Time { return time.Unix(0, 0).UTC() },
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	perr := l.poll(context.Background(), false)
	if perr == nil {
		t.Fatal("expected a provider error from poll")
	}
	if !errors.Is(perr, ErrProvider) {
		t.Fatalf("poll error not classified as ErrProvider: %v", perr)
	}
}

func TestRunBacksOffOnProviderError(t *testing.T) {
	in := &fakeInbound{pending: []contract.MessageIn{msg("m1", "hi", 1)}}
	out := &fakeOutbound{}
	prov := &errProvider{}
	l, err := New(Config{
		Inbound: in, Outbound: out, Provider: prov,
		HeartbeatPath:            filepath.Join(t.TempDir(), "hb"),
		Clock:                    func() time.Time { return time.Unix(0, 0).UTC() },
		Logger:                   log.New(io.Discard, "", 0),
		PollInterval:             time.Millisecond,
		ProviderBackoffMax:       50 * time.Millisecond,
		ProviderBreakerThreshold: 2,
		Jitter:                   func() float64 { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = l.Run(ctx); close(done) }()
	time.Sleep(40 * time.Millisecond)
	cancel()
	<-done

	calls := atomic.LoadInt64(&prov.calls)
	if calls < 2 {
		t.Fatalf("expected the loop to retry the provider, got %d call(s)", calls)
	}
	// A fixed 1ms poll interval would yield ~40 calls in 40ms. Exponential
	// backoff must keep it well under that; a generous upper bound stays robust
	// against scheduler jitter while still proving backoff is applied.
	if calls > 25 {
		t.Fatalf("backoff not applied: %d calls in 40ms (fixed interval would be ~40)", calls)
	}
}
