package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/gateway"
)

func newGateway() *gateway.Gateway {
	return gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
}

func TestReadyzDefaultReady(t *testing.T) {
	srv := httptest.NewServer(New(newGateway()).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/readyz status = %d, want 200", resp.StatusCode)
	}
}

func TestReadyzReportsNotReady(t *testing.T) {
	notReady := func() error { return io.ErrUnexpectedEOF }
	srv := httptest.NewServer(New(newGateway()).WithReadiness(notReady).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz status = %d, want 503", resp.StatusCode)
	}
}

func TestReadyzExemptFromAuth(t *testing.T) {
	srv := httptest.NewServer(New(newGateway()).WithToken("s3cret").Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz") // no token
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/readyz with token set and no creds = %d, want 200 (exempt)", resp.StatusCode)
	}
}

func TestMetricsInjectedHandler(t *testing.T) {
	metrics := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ironclaw_up 1\n")
	})
	srv := httptest.NewServer(New(newGateway()).WithMetrics(metrics).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ironclaw_up 1") {
		t.Fatalf("/metrics = %d %q, want 200 with injected body", resp.StatusCode, body)
	}
}

func TestMetricsNotConfigured404(t *testing.T) {
	srv := httptest.NewServer(New(newGateway()).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/metrics with no handler = %d, want 404", resp.StatusCode)
	}
}

func TestBodyLimitRejectsOversize(t *testing.T) {
	// 16-byte body cap; a larger POST body must be rejected by the handler's
	// JSON decode (MaxBytesReader makes the read error -> 400).
	srv := httptest.NewServer(New(newGateway()).WithLimits(16, 0).Handler())
	defer srv.Close()

	big := strings.NewReader(`{"kind":"` + strings.Repeat("x", 256) + `"}`)
	resp, err := http.Post(srv.URL+"/v1/changes", "application/json", big)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversize body status = %d, want 400", resp.StatusCode)
	}
}

func TestRateLimitReturns429(t *testing.T) {
	// 0 refill (well, tiny) with burst 2: the third immediate request is denied.
	srv := httptest.NewServer(New(newGateway()).WithRateLimit(0.0001, 2).Handler())
	defer srv.Close()

	codes := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL + "/v1/changes/pending")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		codes = append(codes, resp.StatusCode)
	}
	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("first two within burst = %v, want 200,200", codes[:2])
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("third over burst = %d, want 429", codes[2])
	}
}

func TestRateLimitExemptsProbes(t *testing.T) {
	srv := httptest.NewServer(New(newGateway()).WithRateLimit(0.0001, 1).Handler())
	defer srv.Close()

	// Exhaust the single token on a real route.
	resp, _ := http.Get(srv.URL + "/v1/changes/pending")
	resp.Body.Close()

	// Probes still answer despite the empty bucket.
	for _, p := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s under exhausted limiter = %d, want 200", p, resp.StatusCode)
		}
	}
}

func TestRateLimiterRefills(t *testing.T) {
	rl := newRateLimiter(10, 1) // 10 tokens/sec, burst 1
	base := time.Unix(0, 0)
	rl.now = func() time.Time { return base }
	rl.lastFill = base

	if !rl.allow() {
		t.Fatal("first call should pass (bucket starts full)")
	}
	if rl.allow() {
		t.Fatal("second immediate call should be denied (bucket empty)")
	}
	// Advance 200ms -> 2 tokens worth at 10/sec, capped at burst 1.
	rl.now = func() time.Time { return base.Add(200 * time.Millisecond) }
	if !rl.allow() {
		t.Fatal("after refill the call should pass")
	}
}
