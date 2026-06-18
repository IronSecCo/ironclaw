package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// socketClient builds an HTTP client that dials only the given unix socket, exactly
// like the in-sandbox MCP tool does.
func socketClient(path string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", path)
			},
		},
	}
}

func getTools(t *testing.T, c *http.Client) []toolDescriptor {
	t.Helper()
	resp, err := c.Get("http://mcp/tools")
	if err != nil {
		t.Fatalf("GET /tools: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Tools []toolDescriptor `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /tools: %v", err)
	}
	return body.Tools
}

func postCall(t *testing.T, c *http.Client, name, input string) callResponse {
	t.Helper()
	body, _ := json.Marshal(callRequest{Name: name, Input: json.RawMessage(input)})
	resp, err := c.Post("http://mcp/call", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST /call: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /call status %d: %s", resp.StatusCode, b)
	}
	var out callResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode /call: %v", err)
	}
	return out
}

func TestBroker_GatesAndAudits(t *testing.T) {
	// A remote MCP server (the sample, over httptest — loopback http is allowed).
	upstream := httptest.NewServer(SampleServer().Handler())
	defer upstream.Close()

	cat, _ := NewCatalog("")
	if err := cat.Put(ServerConfig{Name: "sample", Transport: TransportHTTP, URL: upstream.URL}); err != nil {
		t.Fatalf("catalog put: %v", err)
	}

	// Session s1 is granted ONLY the echo tool on "sample"; s2 is granted nothing.
	grants := func(session string) []Grant {
		if session == "s1" {
			return []Grant{{Server: "sample", Tools: []string{"echo"}}}
		}
		return nil
	}

	var (
		auditMu sync.Mutex
		records []AuditRecord
	)
	b := New(context.Background(), cat, grants, WithAudit(func(r AuditRecord) {
		auditMu.Lock()
		records = append(records, r)
		auditMu.Unlock()
	}))
	defer b.Close()

	dir := t.TempDir()
	path, err := b.SocketForSession("s1", dir)
	if err != nil {
		t.Fatalf("SocketForSession: %v", err)
	}
	c := socketClient(path)

	// /tools exposes ONLY the granted echo tool, namespaced.
	tools := getTools(t, c)
	if len(tools) != 1 || tools[0].Name != "sample__echo" {
		t.Fatalf("tools = %+v, want only sample__echo", tools)
	}
	if len(tools[0].InputSchema) == 0 {
		t.Error("tool descriptor missing input schema")
	}

	// Granted call works.
	if res := postCall(t, c, "sample__echo", `{"text":"hello"}`); res.IsError || res.Content != "hello" {
		t.Fatalf("echo call = %+v, want hello", res)
	}

	// A tool on the same server that was NOT granted is denied.
	if res := postCall(t, c, "sample__add", `{"a":1,"b":2}`); !res.IsError || !strings.Contains(res.Content, "not granted tool") {
		t.Fatalf("add call = %+v, want denied", res)
	}

	// A server the session has no grant for is denied.
	if res := postCall(t, c, "other__x", `{}`); !res.IsError || !strings.Contains(res.Content, "not granted server") {
		t.Fatalf("other call = %+v, want server-denied", res)
	}

	// Audit captured the ok call and the two denials.
	auditMu.Lock()
	defer auditMu.Unlock()
	var okCalls, denied int
	for _, r := range records {
		if r.Op == "call" && r.Status == "ok" {
			okCalls++
		}
		if r.Op == "call" && r.Status == "denied" {
			denied++
		}
	}
	if okCalls != 1 || denied != 2 {
		t.Fatalf("audit: ok=%d denied=%d, want 1 and 2 (records: %+v)", okCalls, denied, records)
	}
}

func TestBroker_PerSessionIsolation(t *testing.T) {
	upstream := httptest.NewServer(SampleServer().Handler())
	defer upstream.Close()
	cat, _ := NewCatalog("")
	_ = cat.Put(ServerConfig{Name: "sample", Transport: TransportHTTP, URL: upstream.URL})

	grants := func(session string) []Grant {
		if session == "s1" {
			return []Grant{{Server: "sample"}} // all tools
		}
		return nil // s2 granted nothing
	}
	b := New(context.Background(), cat, grants)
	defer b.Close()
	dir := t.TempDir()

	p1, _ := b.SocketForSession("s1", dir)
	p2, _ := b.SocketForSession("s2", dir)

	// s1 sees both tools; s2 (no grants) sees none — the socket identity, not a header,
	// decides the surface.
	if got := getTools(t, socketClient(p1)); len(got) != 2 {
		t.Fatalf("s1 tools = %d, want 2", len(got))
	}
	if got := getTools(t, socketClient(p2)); len(got) != 0 {
		t.Fatalf("s2 tools = %d, want 0 (granted nothing)", len(got))
	}

	// Closing s2's socket removes it.
	b.CloseSession("s2")
	if _, err := socketClient(p2).Get("http://mcp/tools"); err == nil {
		t.Error("expected s2 socket to be closed")
	}
}

func TestBroker_Probe(t *testing.T) {
	upstream := httptest.NewServer(SampleServer().Handler())
	defer upstream.Close()
	cat, _ := NewCatalog("")
	_ = cat.Put(ServerConfig{Name: "sample", Transport: TransportHTTP, URL: upstream.URL})
	b := New(context.Background(), cat, func(string) []Grant { return nil })
	defer b.Close()

	tools, err := b.Probe(context.Background(), "sample")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	if !names["echo"] || !names["add"] {
		t.Fatalf("Probe tools = %v, want echo+add", names)
	}

	if _, err := b.Probe(context.Background(), "missing"); err == nil {
		t.Error("Probe of an unconfigured server should error")
	}
}
