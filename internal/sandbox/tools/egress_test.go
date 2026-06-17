// OWNER: AGENT2

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordingRT captures the request the tool builds and returns a canned response,
// so http_fetch can be tested without a real unix socket/broker.
type recordingRT struct {
	req    *http.Request
	body   string // captured request body
	status int
	header http.Header
	respBd string
}

func (r *recordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.req = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		r.body = string(b)
	}
	h := r.header
	if h == nil {
		h = make(http.Header)
	}
	return &http.Response{
		StatusCode: r.status,
		Status:     fmt.Sprintf("%d %s", r.status, http.StatusText(r.status)),
		Body:       io.NopCloser(strings.NewReader(r.respBd)),
		Header:     h,
	}, nil
}

func fetchToolWith(rt http.RoundTripper) *HTTPFetchTool {
	return &HTTPFetchTool{client: &http.Client{Transport: rt}}
}

func TestHTTPFetchForwardsThroughSocketHop(t *testing.T) {
	rt := &recordingRT{status: 200, respBd: "pong", header: http.Header{"Content-Type": {"application/json"}}}
	tool := fetchToolWith(rt)

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"url":"https://api.example.com/v1/ping"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if rt.req == nil {
		t.Fatal("no request forwarded")
	}
	// Socket hop is plain HTTP; the broker upgrades to HTTPS. Host + path preserved.
	if rt.req.URL.Scheme != "http" {
		t.Fatalf("socket-hop scheme = %q, want http", rt.req.URL.Scheme)
	}
	if rt.req.Host != "api.example.com" {
		t.Fatalf("Host = %q, want api.example.com", rt.req.Host)
	}
	if rt.req.URL.Path != "/v1/ping" {
		t.Fatalf("path = %q, want /v1/ping", rt.req.URL.Path)
	}
	if rt.req.Method != http.MethodGet {
		t.Fatalf("method = %q, want GET (default)", rt.req.Method)
	}
	if !strings.Contains(out, "status: 200") || !strings.Contains(out, "pong") || !strings.Contains(out, "application/json") {
		t.Fatalf("output missing status/body/content-type: %q", out)
	}
}

func TestHTTPFetchSetsMethodHeadersBody(t *testing.T) {
	rt := &recordingRT{status: 201, respBd: "created"}
	tool := fetchToolWith(rt)

	out, err := tool.Invoke(context.Background(), json.RawMessage(
		`{"url":"https://api.example.com/things","method":"post","headers":{"X-Custom":"v"},"body":"{\"a\":1}"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if rt.req.Method != http.MethodPost {
		t.Fatalf("method = %q, want POST", rt.req.Method)
	}
	if rt.req.Header.Get("X-Custom") != "v" {
		t.Fatalf("custom header not set: %v", rt.req.Header)
	}
	if rt.body != `{"a":1}` {
		t.Fatalf("forwarded body = %q, want %q", rt.body, `{"a":1}`)
	}
	if !strings.Contains(out, "status: 201") {
		t.Fatalf("output = %q, want 201", out)
	}
}

func TestHTTPFetchRejectsNonHTTPScheme(t *testing.T) {
	tool := fetchToolWith(&recordingRT{status: 200})
	for _, bad := range []string{`{"url":"ftp://x/y"}`, `{"url":"file:///etc/passwd"}`, `{"url":"notaurl"}`} {
		if _, err := tool.Invoke(context.Background(), json.RawMessage(bad)); err == nil {
			t.Fatalf("expected error for %s", bad)
		}
	}
}

// TestHTTPFetchSurfacesForbidden asserts a broker 403 (host not on allowlist) is
// returned to the agent as a normal response, not a tool error.
func TestHTTPFetchSurfacesForbidden(t *testing.T) {
	rt := &recordingRT{status: http.StatusForbidden, respBd: "egress: destination not on allowlist"}
	tool := fetchToolWith(rt)

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"url":"https://blocked.test/x"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(out, "403") || !strings.Contains(out, "allowlist") {
		t.Fatalf("forbidden response not surfaced: %q", out)
	}
}

func TestHTTPFetchTruncatesLargeBody(t *testing.T) {
	rt := &recordingRT{status: 200, respBd: strings.Repeat("a", maxFetchResponseBytes+100)}
	tool := fetchToolWith(rt)

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"url":"https://api.example.com/big"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(out, "[truncated at") {
		t.Fatal("large body was not truncated")
	}
}

func TestHTTPFetchRegistersNotForbidden(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(NewHTTPFetchTool("/run/ironclaw/egress.sock")); err != nil {
		t.Fatalf("register http_fetch: %v", err)
	}
	if _, ok := reg.Get(HTTPFetchToolName); !ok {
		t.Fatal("http_fetch not registered")
	}
}
