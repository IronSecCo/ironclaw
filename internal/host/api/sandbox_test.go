package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/sandboxexec"
)

func newSandboxTestServer(cfg *sandboxexec.Config) *Server {
	gw := gateway.New(gateway.VerifierChain{}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())
	s := New(gw)
	if cfg != nil {
		s = s.WithSandboxExec(cfg)
	}
	return s
}

func postExec(s *Server, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/sandbox/exec", strings.NewReader(body))
	s.Handler().ServeHTTP(rr, req)
	return rr
}

// When no backend is configured the endpoint is disabled (501), never a bare host run.
func TestSandboxExecDisabled(t *testing.T) {
	rr := postExec(newSandboxTestServer(nil), `{"command":"echo hi"}`)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body=%s", rr.Code, rr.Body.String())
	}
}

// A successful run returns 200 with the hardened argv actually built and a
// gVisor-labelled containment string.
func TestSandboxExecSuccess(t *testing.T) {
	var gotArgv []string
	cfg := &sandboxexec.Config{
		Image: "alpine:3.20", DockerBin: "docker", Runtime: "runsc", SeccompPath: "/tmp/s.json", TimeoutSec: 30,
		Run: func(_ context.Context, _ string, argv []string) (string, string, int, error) {
			gotArgv = argv
			return "hello\n", "", 0, nil
		},
	}
	rr := postExec(newSandboxTestServer(cfg), `{"command":"echo hello"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var out sandboxExecResp
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Stdout != "hello\n" || out.ExitCode != 0 || out.LaunchError != "" {
		t.Errorf("bad response: %+v", out)
	}
	if !strings.Contains(out.Containment, "runtime=runsc (gVisor") {
		t.Errorf("containment should advertise gVisor: %s", out.Containment)
	}
	joined := strings.Join(gotArgv, " ")
	for _, f := range []string{"--network none", "--cap-drop ALL", "--runtime runsc", "--read-only", "--user 65532:65532"} {
		if !strings.Contains(joined, f) {
			t.Errorf("hardened argv missing %q: %s", f, joined)
		}
	}
	if !strings.Contains(joined, "ic-sbx-mcp-cp-") {
		t.Errorf("box name should be control-plane-scoped: %s", joined)
	}
}

// A launch failure returns 502 with launch_error set — the client must fail closed.
func TestSandboxExecLaunchFailure(t *testing.T) {
	cfg := &sandboxexec.Config{
		Image: "alpine:3.20", DockerBin: "docker", Runtime: "runsc",
		Run: func(_ context.Context, _ string, _ []string) (string, string, int, error) {
			return "", "", -1, context.DeadlineExceeded
		},
	}
	rr := postExec(newSandboxTestServer(cfg), `{"command":"sleep 999"}`)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rr.Code, rr.Body.String())
	}
	var out sandboxExecResp
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out.LaunchError == "" {
		t.Errorf("launch_error should be set: %+v", out)
	}
}

// Invalid input (empty command, hostile image ref) is rejected 400 before any run.
func TestSandboxExecInvalidInput(t *testing.T) {
	called := false
	cfg := &sandboxexec.Config{
		Image: "alpine:3.20", DockerBin: "docker", Runtime: "runsc",
		Run: func(_ context.Context, _ string, _ []string) (string, string, int, error) {
			called = true
			return "", "", 0, nil
		},
	}
	for _, body := range []string{`{"command":"  "}`, `{"command":"true","image":"--volume=/:/host"}`} {
		rr := postExec(newSandboxTestServer(cfg), body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, rr.Code)
		}
	}
	if called {
		t.Errorf("runner must not run for invalid input")
	}
}

// The endpoint sits behind bearer-token auth when a token is configured.
func TestSandboxExecRequiresAuth(t *testing.T) {
	cfg := &sandboxexec.Config{
		Image: "alpine:3.20", DockerBin: "docker", Runtime: "runsc",
		Run: func(_ context.Context, _ string, _ []string) (string, string, int, error) { return "", "", 0, nil },
	}
	s := newSandboxTestServer(cfg).WithToken("s3cret")
	rr := postExec(s, `{"command":"echo hi"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated exec should be 401, got %d", rr.Code)
	}
	// With the token it goes through.
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/sandbox/exec", strings.NewReader(`{"command":"echo hi"}`))
	req.Header.Set("Authorization", "Bearer s3cret")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("authenticated exec should be 200, got %d; body=%s", rr.Code, rr.Body.String())
	}
}
