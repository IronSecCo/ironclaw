package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func searchToolWith(rt http.RoundTripper, backend searchBackend) *WebSearchTool {
	return &WebSearchTool{client: &http.Client{Transport: rt}, backend: backend}
}

// TestWebSearchDuckDuckGoForwardsAndParses asserts the DDG backend builds the brokered
// request to the keyless HTML results endpoint and parses the real ranked results
// (title, decoded URL, snippet) for the model.
func TestWebSearchDuckDuckGoForwardsAndParses(t *testing.T) {
	body := `<html><body>
		<div class="result results_links results_links_deep web-result">
			<h2 class="result__title">
				<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F&amp;rut=abc">The Go Programming Language</a>
			</h2>
			<a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F">Go is an open source programming language that makes it easy to build simple software.</a>
		</div>
		<div class="result results_links results_links_deep web-result">
			<h2 class="result__title">
				<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fen.wikipedia.org%2Fwiki%2FGo">Go (programming language) - Wikipedia</a>
			</h2>
			<a class="result__snippet">Go is a <b>statically typed</b>, compiled language designed at Google.</a>
		</div>
	</body></html>`
	rt := &recordingRT{status: 200, respBd: body, header: http.Header{"Content-Type": {"text/html"}}}
	tool := searchToolWith(rt, duckDuckGoBackend{})

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"golang"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if rt.req == nil {
		t.Fatal("no request forwarded")
	}
	if rt.req.URL.Scheme != "http" {
		t.Fatalf("socket-hop scheme = %q, want http", rt.req.URL.Scheme)
	}
	if rt.req.Host != duckDuckGoHost {
		t.Fatalf("Host = %q, want %q", rt.req.Host, duckDuckGoHost)
	}
	if rt.req.URL.Path != "/html/" {
		t.Fatalf("path = %q, want /html/", rt.req.URL.Path)
	}
	if got := rt.req.URL.Query().Get("q"); got != "golang" {
		t.Fatalf("q = %q, want golang", got)
	}
	if rt.req.Header.Get("User-Agent") == "" {
		t.Fatal("User-Agent header not set (DDG blocks UA-less clients)")
	}
	// Real result text, the decoded destination URL (not the DDG redirect), and the
	// tag-stripped snippet must all appear.
	for _, want := range []string{
		"The Go Programming Language", "https://golang.org/",
		"Go (programming language) - Wikipedia", "statically typed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	// The DDG redirect wrapper must NOT leak through.
	if strings.Contains(out, "uddg=") || strings.Contains(out, "duckduckgo.com/l/") {
		t.Fatalf("redirect wrapper leaked into output:\n%s", out)
	}
}

// TestWebSearchBraveViaVault asserts the keyed backend addresses the vault by name
// (Host "vault", path /<cred>/<providerPath>) so the host injector attaches the key,
// and parses Brave's web.results shape.
func TestWebSearchBraveViaVault(t *testing.T) {
	body := `{"web":{"results":[
		{"title":"Omer Zamir","url":"https://example.com/omer","description":"Security engineer."},
		{"title":"Second","url":"https://example.com/2","description":"Another hit."}
	]}}`
	rt := &recordingRT{status: 200, respBd: body}
	tool := searchToolWith(rt, vaultSearchBackend{cred: "brave", provider: braveProvider{}})

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"Omer Zamir","max_results":2}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if rt.req.Host != vaultHost {
		t.Fatalf("Host = %q, want %q (vault-addressed)", rt.req.Host, vaultHost)
	}
	if rt.req.URL.Path != "/brave/res/v1/web/search" {
		t.Fatalf("path = %q, want /brave/res/v1/web/search", rt.req.URL.Path)
	}
	if got := rt.req.URL.Query().Get("count"); got != "2" {
		t.Fatalf("count = %q, want 2", got)
	}
	if !strings.Contains(out, "Omer Zamir") || !strings.Contains(out, "https://example.com/omer") {
		t.Fatalf("output missing brave result:\n%s", out)
	}
}

// TestWebSearchSurfacesForbidden asserts a broker 403 (backend host not allowlisted)
// is returned as guidance, not a tool error, so the agent can explain the gap.
func TestWebSearchSurfacesForbidden(t *testing.T) {
	rt := &recordingRT{status: http.StatusForbidden, respBd: "egress: destination not on allowlist"}
	tool := searchToolWith(rt, duckDuckGoBackend{})

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("Invoke should not error on 403: %v", err)
	}
	if !strings.Contains(out, "403") || !strings.Contains(strings.ToLower(out), "allowlist") {
		t.Fatalf("403 not surfaced as guidance: %q", out)
	}
}

// TestWebSearchEmptyDuckDuckGoHint asserts an empty DDG result set returns the
// keyless-limitation hint rather than a bare "no results".
func TestWebSearchEmptyDuckDuckGoHint(t *testing.T) {
	rt := &recordingRT{status: 200, respBd: `{"RelatedTopics":[]}`}
	tool := searchToolWith(rt, duckDuckGoBackend{})

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"Some Obscure Person"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(out, "No results") || !strings.Contains(out, "vault") {
		t.Fatalf("expected empty-result hint mentioning the vault, got: %q", out)
	}
}

func TestWebSearchRejectsEmptyQuery(t *testing.T) {
	tool := searchToolWith(&recordingRT{status: 200}, duckDuckGoBackend{})
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"   "}`)); err == nil {
		t.Fatal("expected error for empty query")
	}
}

// TestWebSearchClampsMaxResults asserts max_results above the ceiling is clamped (the
// rendered list never exceeds maxSearchResults).
func TestWebSearchClampsMaxResults(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"web":{"results":[`)
	for i := 0; i < 25; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"title":"T`)
		sb.WriteString(string(rune('A' + i)))
		sb.WriteString(`","url":"https://example.com/`)
		sb.WriteString(string(rune('A' + i)))
		sb.WriteString(`","description":"d"}`)
	}
	sb.WriteString(`]}}`)
	rt := &recordingRT{status: 200, respBd: sb.String()}
	tool := searchToolWith(rt, vaultSearchBackend{cred: "brave", provider: braveProvider{}})

	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"x","max_results":999}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	// Numbered lines "11." .. should never appear (capped at maxSearchResults=10).
	if strings.Contains(out, "\n11.") || strings.Contains(out, "11. ") {
		t.Fatalf("result list exceeded the cap:\n%s", out)
	}
}

func TestParseSearchBackend(t *testing.T) {
	cases := []struct {
		spec    string
		wantErr bool
		label   string
	}{
		{"duckduckgo", false, "duckduckgo"},
		{"ddg", false, "duckduckgo"},
		{"DuckDuckGo", false, "duckduckgo"},
		{"brave", false, "brave (vault:brave)"},
		{"brave:mykey", false, "brave (vault:mykey)"},
		{"", true, ""},
		{"google", true, ""},
	}
	for _, c := range cases {
		b, err := ParseSearchBackend(c.spec)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSearchBackend(%q): expected error", c.spec)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSearchBackend(%q): %v", c.spec, err)
			continue
		}
		if b.label() != c.label {
			t.Errorf("ParseSearchBackend(%q).label() = %q, want %q", c.spec, b.label(), c.label)
		}
	}
}

func TestSearchBackendEgressHost(t *testing.T) {
	if h, err := SearchBackendEgressHost("duckduckgo"); err != nil || h != duckDuckGoHost {
		t.Fatalf("ddg egress host = %q, err %v; want %q", h, err, duckDuckGoHost)
	}
	// A vault-routed backend has no external host to auto-allowlist (injector endpoint
	// is approved via --vault-endpoint).
	if h, err := SearchBackendEgressHost("brave"); err != nil || h != "" {
		t.Fatalf("brave egress host = %q, err %v; want empty", h, err)
	}
	if _, err := SearchBackendEgressHost("nope"); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestWebSearchRegistersNotForbidden(t *testing.T) {
	tool, err := NewWebSearchTool("/run/ironclaw/egress.sock", "duckduckgo")
	if err != nil {
		t.Fatalf("NewWebSearchTool: %v", err)
	}
	reg := NewRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register web_search: %v", err)
	}
	if _, ok := reg.Get(WebSearchToolName); !ok {
		t.Fatal("web_search not registered")
	}
}

func TestNewWebSearchToolRejectsBadBackend(t *testing.T) {
	if _, err := NewWebSearchTool("/sock", "altavista"); err == nil {
		t.Fatal("expected error for unknown backend spec")
	}
}
