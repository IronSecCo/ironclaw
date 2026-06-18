package integration

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nivardsec/ironclaw/internal/host/egress"
	"github.com/nivardsec/ironclaw/internal/sandbox/tools"
)

// fakeUpstream stands in for the real external search API. The egress broker forwards
// allowlisted requests to it over its (overridden) transport, so the test exercises
// the full path — sandbox tool → unix socket → host broker allowlist → upstream →
// parse — without leaving the machine.
type fakeUpstream struct {
	body    string
	gotHost string
	gotPath string
}

func (f *fakeUpstream) RoundTrip(r *http.Request) (*http.Response, error) {
	f.gotHost = r.URL.Host
	f.gotPath = r.URL.Path
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{"Content-Type": {"application/json"}},
	}, nil
}

// serveBroker binds the broker's handler on a temp unix socket synchronously (so there
// is no listen race) and returns the socket path.
func serveBroker(t *testing.T, b *egress.Broker) string {
	t.Helper()
	// A short socket dir: macOS caps unix socket paths at ~104 bytes, which t.TempDir()
	// (a deep /var/folders path) blows past.
	dir, err := os.MkdirTemp("/tmp", "ic")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "e.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: b.Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close() })
	return sock
}

// TestWebSearchThroughRealBroker drives the real web_search tool through the real
// egress broker over a real unix socket: the allowlisted DuckDuckGo HTML results host
// is forwarded to the fake upstream and the canned HTML page is parsed into results.
func TestWebSearchThroughRealBroker(t *testing.T) {
	up := &fakeUpstream{body: `<html><body>
		<div class="result results_links web-result">
			<h2 class="result__title"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fomer">Omer Zamir</a></h2>
			<a class="result__snippet">Example snippet about the query.</a>
		</div>
		<div class="result results_links web-result">
			<h2 class="result__title"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Frelated">Related thing</a></h2>
			<a class="result__snippet">A second hit.</a>
		</div>
	</body></html>`}
	broker := egress.New([]string{"html.duckduckgo.com"}, egress.WithTransport(up))
	sock := serveBroker(t, broker)

	tool, err := tools.NewWebSearchTool(sock, "duckduckgo")
	if err != nil {
		t.Fatalf("NewWebSearchTool: %v", err)
	}
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"Omer Zamir"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	// The broker selected the allowlisted upstream and upgraded the socket hop to it.
	if up.gotHost != "html.duckduckgo.com" {
		t.Fatalf("upstream host = %q, want html.duckduckgo.com", up.gotHost)
	}
	for _, want := range []string{"Omer Zamir", "Example snippet", "https://example.com/omer", "Related thing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestWebSearchDeniedByBroker asserts that with the backend host NOT on the allowlist,
// the broker returns 403 and the tool surfaces it as guidance (not a crash) — the
// deny-by-default posture reaching the agent intact.
func TestWebSearchDeniedByBroker(t *testing.T) {
	up := &fakeUpstream{body: `{}`}
	broker := egress.New(nil, egress.WithTransport(up)) // empty allowlist: deny all
	sock := serveBroker(t, broker)

	tool, err := tools.NewWebSearchTool(sock, "duckduckgo")
	if err != nil {
		t.Fatalf("NewWebSearchTool: %v", err)
	}
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("Invoke should not error on a broker denial: %v", err)
	}
	if up.gotHost != "" {
		t.Fatalf("denied request must not reach the upstream, but host=%q", up.gotHost)
	}
	if !strings.Contains(out, "403") || !strings.Contains(strings.ToLower(out), "allowlist") {
		t.Fatalf("broker denial not surfaced as guidance: %q", out)
	}
}
