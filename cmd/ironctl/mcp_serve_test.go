package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/mcp"
)

// The sandbox server must actually register the sandbox_exec tool.
func TestSandboxServerRegistersTool(t *testing.T) {
	srv := newSandboxMCPServer(sandboxExecConfig{image: "alpine:3.20", dockerBin: "docker", run: nopRunner})
	var names []string
	for _, tl := range srv.Tools() {
		names = append(names, tl.Name)
	}
	if len(names) != 1 || names[0] != "sandbox_exec" {
		t.Fatalf("tools = %v, want [sandbox_exec]", names)
	}
}

// Test that sandbox_exec assembles the HARDENED docker argv. This is the containment
// contract: if any flag regresses an escape becomes possible, so each is asserted
// explicitly. Runs without Docker via a fake runner.
func TestSandboxExecHardenedArgs(t *testing.T) {
	var gotBin string
	var gotArgv []string
	cfg := sandboxExecConfig{
		image: "alpine:3.20", dockerBin: "docker", runtime: "runsc",
		seccompPath: "/tmp/ic-mcp-seccomp.json", timeoutSec: 30,
		run: func(_ context.Context, bin string, argv []string) (string, string, int, error) {
			gotBin, gotArgv = bin, argv
			return "hello\n", "", 0, nil
		},
	}
	res := invoke(t, cfg, `{"command":"echo hello"}`)

	if gotBin != "docker" {
		t.Fatalf("runtime binary = %q, want docker", gotBin)
	}
	joined := strings.Join(gotArgv, " ")
	for _, f := range []string{
		"--runtime runsc", "--network none", "--cap-drop ALL",
		"--security-opt no-new-privileges", "--security-opt seccomp=/tmp/ic-mcp-seccomp.json",
		"--read-only", "--user 65532:65532", "--pids-limit 256",
		"--memory 512m", "--cpus 1", "--rm",
	} {
		if !strings.Contains(joined, f) {
			t.Errorf("hardened argv missing %q\n  got: %s", f, joined)
		}
	}
	if !strings.Contains(joined, "ic-sbx-mcp-") {
		t.Errorf("box name not ic-sbx-mcp-*: %s", joined)
	}
	// An "--" end-of-options separator MUST precede the image so a hostile image
	// reference cannot be parsed by docker as a flag (finding #4).
	sep := indexOf(gotArgv, "--")
	img := indexOf(gotArgv, "alpine:3.20")
	if sep < 0 || img < 0 || sep+1 != img {
		t.Errorf("expected '--' immediately before image; sep=%d img=%d argv=%v", sep, img, gotArgv)
	}
	n := len(gotArgv)
	if gotArgv[n-3] != "sh" || gotArgv[n-2] != "-c" || gotArgv[n-1] != "echo hello" {
		t.Errorf("command tail wrong: %v", gotArgv[n-3:])
	}
	if res.IsError {
		t.Errorf("expected success result, got IsError")
	}
	txt := res.Content[0].Text
	if !strings.Contains(txt, "exit_code: 0") || !strings.Contains(txt, "hello") || !strings.Contains(txt, "containment:") {
		t.Errorf("result missing expected fields:\n%s", txt)
	}
	// gVisor must be advertised only when the runtime is actually runsc.
	if !strings.Contains(txt, "runtime=runsc (gVisor") {
		t.Errorf("containment should advertise gVisor for runsc:\n%s", txt)
	}
}

func indexOf(argv []string, want string) int {
	for i, a := range argv {
		if a == want {
			return i
		}
	}
	return -1
}

// Finding #1: a non-runsc runtime must be labelled a fallback, NOT gVisor.
func TestSandboxExecFallbackRuntimeLabelled(t *testing.T) {
	cfg := sandboxExecConfig{
		image: "alpine:3.20", dockerBin: "docker", runtime: "runc",
		run: func(_ context.Context, _ string, argv []string) (string, string, int, error) {
			if indexOf(argv, "--runtime") < 0 || argv[indexOf(argv, "--runtime")+1] != "runc" {
				t.Errorf("runc runtime not passed: %v", argv)
			}
			return "", "", 0, nil
		},
	}
	txt := invoke(t, cfg, `{"command":"true"}`).Content[0].Text
	if strings.Contains(txt, "gVisor:") {
		t.Errorf("runc fallback must NOT be marketed as gVisor:\n%s", txt)
	}
	if !strings.Contains(txt, "NOT gVisor") {
		t.Errorf("runc fallback must be explicitly labelled NOT gVisor:\n%s", txt)
	}
}

// Finding #4: an image parsed as a docker flag is rejected before any runner call.
func TestSandboxExecImageInjectionRejected(t *testing.T) {
	for _, bad := range []string{"--volume=/:/host", "-v", "--user=0:0"} {
		called := false
		cfg := sandboxExecConfig{
			image: "alpine:3.20", dockerBin: "docker", runtime: "runsc",
			run: func(_ context.Context, _ string, _ []string) (string, string, int, error) {
				called = true
				return "", "", 0, nil
			},
		}
		_, err := cfg.toolFunc()(context.Background(), json.RawMessage(`{"command":"true","image":"`+bad+`"}`))
		if err == nil {
			t.Errorf("image %q should be rejected", bad)
		}
		if called {
			t.Errorf("runner must not run for hostile image %q", bad)
		}
	}
}

// Finding #3: bind-address defaulting and loopback classification.
func TestNormalizeHTTPAddr(t *testing.T) {
	cases := []struct {
		in       string
		wantAddr string
		wantLoop bool
	}{
		{":9000", "127.0.0.1:9000", true},          // bare port defaults to loopback
		{"127.0.0.1:9000", "127.0.0.1:9000", true}, // explicit loopback
		{"localhost:9000", "localhost:9000", true}, // localhost is loopback
		{"0.0.0.0:9000", "0.0.0.0:9000", false},    // all-interfaces is NOT loopback
		{"10.0.0.5:9000", "10.0.0.5:9000", false},  // routable IP is NOT loopback
		{"[::1]:9000", "[::1]:9000", true},         // IPv6 loopback
	}
	for _, c := range cases {
		gotAddr, gotLoop, err := normalizeHTTPAddr(c.in)
		if err != nil {
			t.Errorf("normalizeHTTPAddr(%q) error: %v", c.in, err)
			continue
		}
		if gotAddr != c.wantAddr || gotLoop != c.wantLoop {
			t.Errorf("normalizeHTTPAddr(%q) = (%q,%v), want (%q,%v)", c.in, gotAddr, gotLoop, c.wantAddr, c.wantLoop)
		}
	}
	if _, _, err := normalizeHTTPAddr("garbage"); err == nil {
		t.Errorf("malformed address should error")
	}
}

// Finding #3: the bearer-auth wrapper rejects missing/wrong tokens and admits the right one.
func TestBearerAuth(t *testing.T) {
	ok := false
	h := bearerAuth("s3cret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ok = true
		w.WriteHeader(http.StatusOK)
	}))
	check := func(hdr string, wantStatus int, wantOK bool) {
		ok = false
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		if hdr != "" {
			req.Header.Set("Authorization", hdr)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != wantStatus || ok != wantOK {
			t.Errorf("hdr=%q -> code=%d ok=%v, want code=%d ok=%v", hdr, rec.Code, ok, wantStatus, wantOK)
		}
	}
	check("", http.StatusUnauthorized, false)
	check("Bearer wrong", http.StatusUnauthorized, false)
	check("s3cret", http.StatusUnauthorized, false)
	check("Bearer s3cret", http.StatusOK, true)
}

// A per-call image override is honored.
func TestSandboxExecImageOverride(t *testing.T) {
	var gotArgv []string
	cfg := sandboxExecConfig{
		image: "alpine:3.20", dockerBin: "docker", runtime: "runsc",
		run: func(_ context.Context, _ string, argv []string) (string, string, int, error) {
			gotArgv = argv
			return "", "", 0, nil
		},
	}
	invoke(t, cfg, `{"command":"true","image":"python:3.12-slim"}`)
	// image is the 4th-from-last arg: <image> sh -c <command>
	if gotArgv[len(gotArgv)-4] != "python:3.12-slim" {
		t.Errorf("image override not applied: %v", gotArgv)
	}
}

// A non-zero exit is a normal result; a launch failure is IsError with a containment note.
func TestSandboxExecExitAndLaunchFailure(t *testing.T) {
	res := invoke(t, sandboxExecConfig{
		image: "alpine:3.20", dockerBin: "docker",
		run: func(_ context.Context, _ string, _ []string) (string, string, int, error) {
			return "", "boom\n", 7, nil
		},
	}, `{"command":"exit 7"}`)
	if res.IsError {
		t.Errorf("non-zero exit should not be a tool error")
	}
	if !strings.Contains(res.Content[0].Text, "exit_code: 7") {
		t.Errorf("exit code not reported: %s", res.Content[0].Text)
	}

	res2 := invoke(t, sandboxExecConfig{
		image: "alpine:3.20", dockerBin: "docker",
		run: func(_ context.Context, _ string, _ []string) (string, string, int, error) {
			return "", "", -1, context.DeadlineExceeded
		},
	}, `{"command":"sleep 999"}`)
	if !res2.IsError {
		t.Errorf("launch failure should be a tool error")
	}
	if !strings.Contains(res2.Content[0].Text, "containment:") {
		t.Errorf("launch-failure result should still note containment: %s", res2.Content[0].Text)
	}
}

// Empty command is rejected before any runner call.
func TestSandboxExecEmptyCommand(t *testing.T) {
	called := false
	cfg := sandboxExecConfig{
		image: "alpine:3.20", dockerBin: "docker",
		run: func(_ context.Context, _ string, _ []string) (string, string, int, error) {
			called = true
			return "", "", 0, nil
		},
	}
	if _, err := cfg.toolFunc()(context.Background(), json.RawMessage(`{"command":"   "}`)); err == nil {
		t.Errorf("empty command should error")
	}
	if called {
		t.Errorf("runner should not run for empty command")
	}
}

func nopRunner(_ context.Context, _ string, _ []string) (string, string, int, error) {
	return "", "", 0, nil
}

// invoke drives the sandbox_exec handler built from cfg with the given JSON args.
func invoke(t *testing.T, cfg sandboxExecConfig, argsJSON string) mcp.ToolResult {
	t.Helper()
	res, err := cfg.toolFunc()(context.Background(), json.RawMessage(argsJSON))
	if err != nil {
		t.Fatalf("sandbox_exec returned rpc error: %v", err)
	}
	return res
}
