package modelproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRateCapReturns429OverBurst(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := New([]string{"api.anthropic.com"},
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
		WithRateCap(0.0001, 2), // burst 2, effectively no refill during the test
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	codes := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
		req.Host = "api.anthropic.com"
		resp, err := http.DefaultClient.Do(req)
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

func TestRateCapKeyedPerSession(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	// Identity keyed by a session header: each session has its own bucket.
	p := New([]string{"api.anthropic.com"},
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
		WithRateCap(0.0001, 1),
		WithIdentity(func(r *http.Request) string { return r.Header.Get("X-Session") }),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	do := func(session string) int {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
		req.Host = "api.anthropic.com"
		req.Header.Set("X-Session", session)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := do("sess-a"); got != 200 {
		t.Fatalf("sess-a first = %d, want 200", got)
	}
	if got := do("sess-a"); got != http.StatusTooManyRequests {
		t.Fatalf("sess-a second = %d, want 429 (bucket exhausted)", got)
	}
	// A different session has an independent bucket.
	if got := do("sess-b"); got != 200 {
		t.Fatalf("sess-b first = %d, want 200 (independent bucket)", got)
	}
}

func TestAuditRecordsForwardedAndDenied(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello-body")
	}))
	defer upstream.Close()

	var mu sync.Mutex
	var records []AuditRecord
	sink := func(rec AuditRecord) {
		mu.Lock()
		records = append(records, rec)
		mu.Unlock()
	}

	p := New([]string{"api.anthropic.com"},
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
		WithAudit(sink),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	// Allowed request.
	req, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
	req.Host = "api.anthropic.com"
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Denied request.
	req2, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
	req2.Host = "evil.example.com"
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 2 {
		t.Fatalf("got %d audit records, want 2", len(records))
	}
	allowed, denied := records[0], records[1]
	if !allowed.Allowed || allowed.Status != http.StatusOK || allowed.Host != "api.anthropic.com" {
		t.Fatalf("allowed record wrong: %+v", allowed)
	}
	if allowed.ResponseBytes != int64(len("hello-body")) {
		t.Fatalf("allowed ResponseBytes = %d, want %d", allowed.ResponseBytes, len("hello-body"))
	}
	if denied.Allowed || denied.Status != http.StatusForbidden {
		t.Fatalf("denied record wrong: %+v", denied)
	}
}

func TestResponseSecretRedaction(t *testing.T) {
	const secret = "host-secret-key-abc123"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A misbehaving upstream that echoes the credential in body and header.
		w.Header().Set("X-Api-Key", secret)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":"bad key `+secret+`"}`)
	}))
	defer upstream.Close()

	p := New([]string{"api.anthropic.com"},
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
		WithRedactedSecrets(secret),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
	req.Host = "api.anthropic.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if strings.Contains(string(body), secret) {
		t.Fatalf("secret leaked in body: %q", string(body))
	}
	if !strings.Contains(string(body), redactedMarker) {
		t.Fatalf("redaction marker absent: %q", string(body))
	}
	if got := resp.Header.Get("X-Api-Key"); got != "" {
		t.Fatalf("credential header forwarded to sandbox: %q", got)
	}
}

func TestStreamingResponseNotBuffered(t *testing.T) {
	// An event-stream body must pass through untouched (no redaction buffering),
	// preserving SSE semantics.
	const secret = "sk-not-present-here"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: chunk-1\n\ndata: chunk-2\n\n")
	}))
	defer upstream.Close()

	p := New([]string{"api.anthropic.com"},
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
		WithRedactedSecrets(secret),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
	req.Host = "api.anthropic.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "chunk-1") || !strings.Contains(string(body), "chunk-2") {
		t.Fatalf("streaming body altered: %q", string(body))
	}
}

func TestKeyedLimiterRefills(t *testing.T) {
	kl := newKeyedLimiter(10, 1) // 10/sec, burst 1
	base := time.Unix(0, 0)
	kl.now = func() time.Time { return base }

	if !kl.allow("k") {
		t.Fatal("first call should pass")
	}
	if kl.allow("k") {
		t.Fatal("second immediate call should be denied")
	}
	kl.now = func() time.Time { return base.Add(200 * time.Millisecond) }
	if !kl.allow("k") {
		t.Fatal("after 200ms refill the call should pass")
	}
}
