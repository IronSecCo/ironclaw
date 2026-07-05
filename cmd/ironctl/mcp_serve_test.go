package main

import (
	"context"
	"encoding/json"
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
		image: "alpine:3.20", dockerBin: "docker", timeoutSec: 30,
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
		"--network none", "--cap-drop ALL", "--security-opt no-new-privileges",
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
}

// A per-call image override is honored.
func TestSandboxExecImageOverride(t *testing.T) {
	var gotArgv []string
	cfg := sandboxExecConfig{
		image: "alpine:3.20", dockerBin: "docker",
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
