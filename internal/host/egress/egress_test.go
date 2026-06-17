// OWNER: AGENT1

package egress

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTransport records the forwarded request and returns a canned response,
// standing in for the real upstream so tests need no network.
type fakeTransport struct {
	lastReq *http.Request
	status  int
	body    string
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.lastReq = r
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

func TestBrokerDeniesByDefault(t *testing.T) {
	b := New(nil)
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/v1/x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (deny by default)", rec.Code)
	}
}

func TestBrokerForwardsAllowlistedHost(t *testing.T) {
	ft := &fakeTransport{status: 200, body: "pong"}
	b := New([]string{"api.example.com"}, WithTransport(ft))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/v1/ping", nil)
	req.Header.Set(sessionHeader, "sess-1")
	b.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "pong" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "pong")
	}
	// The broker must force HTTPS to the upstream and target the allowlisted host.
	if ft.lastReq == nil {
		t.Fatal("upstream request was not forwarded")
	}
	if ft.lastReq.URL.Scheme != "https" {
		t.Fatalf("upstream scheme = %q, want https (no cleartext egress)", ft.lastReq.URL.Scheme)
	}
	if ft.lastReq.URL.Host != "api.example.com" {
		t.Fatalf("upstream host = %q, want api.example.com", ft.lastReq.URL.Host)
	}
	// The audit-only session header must never leak to the external API.
	if ft.lastReq.Header.Get(sessionHeader) != "" {
		t.Fatalf("session header leaked to upstream: %q", ft.lastReq.Header.Get(sessionHeader))
	}
}

func TestBrokerRejectsUnapprovedHost(t *testing.T) {
	ft := &fakeTransport{status: 200, body: "should not reach"}
	b := New([]string{"api.example.com"}, WithTransport(ft))

	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://evil.test/x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for unapproved host", rec.Code)
	}
	if ft.lastReq != nil {
		t.Fatal("a denied request must not be forwarded upstream")
	}
}

func TestBrokerAllowDenyMutation(t *testing.T) {
	ft := &fakeTransport{status: 204}
	b := New(nil, WithTransport(ft))

	// Initially denied.
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://svc.example.com/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("pre-Allow status = %d, want 403", rec.Code)
	}

	// After Allow it forwards.
	b.Allow("svc.example.com")
	rec = httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://svc.example.com/", nil))
	if rec.Code != 204 {
		t.Fatalf("post-Allow status = %d, want 204", rec.Code)
	}

	// After Deny it is rejected again.
	b.Deny("svc.example.com")
	rec = httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://svc.example.com/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("post-Deny status = %d, want 403", rec.Code)
	}
}

func TestBrokerAllowlistMatchesHostPort(t *testing.T) {
	b := New([]string{"api.example.com"})
	if !b.Allowed("api.example.com:443") {
		t.Fatal("bare-host allowlist entry should match host:port request")
	}
	if b.Allowed("api.other.com:443") {
		t.Fatal("unrelated host:port must not match")
	}
}

func TestBrokerAuditsAllowedAndDenied(t *testing.T) {
	var records []AuditRecord
	ft := &fakeTransport{status: 201, body: "created"}
	b := New([]string{"api.example.com"}, WithTransport(ft), WithAudit(func(r AuditRecord) {
		records = append(records, r)
	}))

	// One allowed, one denied.
	b.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "http://api.example.com/v1/things", nil))
	b.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://blocked.test/x", nil))

	if len(records) != 2 {
		t.Fatalf("audit records = %d, want 2", len(records))
	}
	allowed, denied := records[0], records[1]
	if !allowed.Allowed || allowed.Host != "api.example.com" || allowed.Method != http.MethodPost || allowed.Status != 201 {
		t.Fatalf("allowed audit = %+v, want allowed POST api.example.com 201", allowed)
	}
	if denied.Allowed || denied.Host != "blocked.test" || denied.Status != http.StatusForbidden {
		t.Fatalf("denied audit = %+v, want denied blocked.test 403", denied)
	}
}
