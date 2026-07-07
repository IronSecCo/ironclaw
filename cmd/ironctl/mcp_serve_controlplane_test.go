package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cpInvoke(t *testing.T, b *controlplaneBackend, argsJSON string) (result string, isError bool) {
	t.Helper()
	res, err := b.toolFunc()(context.Background(), json.RawMessage(argsJSON))
	if err != nil {
		t.Fatalf("rpc error: %v", err)
	}
	return res.Content[0].Text, res.IsError
}

// The thin client POSTs command/image/timeout to /v1/sandbox/exec with the bearer
// token and renders the control-plane's structured result as the standard text block.
func TestControlplaneBackendSuccess(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody sandboxExecRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stdout": "hi\n", "stderr": "", "exit_code": 0,
			"image": "alpine:3.20", "containment": "runtime=runsc (gVisor: ...)",
		})
	}))
	defer srv.Close()

	b := &controlplaneBackend{baseURL: srv.URL, token: "s3cret", image: "alpine:3.20", timeoutSec: 30, client: srv.Client()}
	txt, isErr := cpInvoke(t, b, `{"command":"echo hi"}`)
	if isErr {
		t.Fatalf("unexpected tool error: %s", txt)
	}
	if gotPath != "/v1/sandbox/exec" {
		t.Errorf("path = %q, want /v1/sandbox/exec", gotPath)
	}
	if gotAuth != "Bearer s3cret" {
		t.Errorf("auth = %q, want Bearer s3cret", gotAuth)
	}
	if gotBody.Command != "echo hi" || gotBody.Image != "alpine:3.20" || gotBody.Timeout != 30 {
		t.Errorf("body = %+v", gotBody)
	}
	if !strings.Contains(txt, "exit_code: 0") || !strings.Contains(txt, "hi") || !strings.Contains(txt, "containment:") {
		t.Errorf("result missing fields:\n%s", txt)
	}
}

// A launch failure reported by the control-plane becomes a fail-closed tool error.
func TestControlplaneBackendLaunchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"launch_error": "runsc not found", "image": "alpine:3.20", "containment": "box never ran",
		})
	}))
	defer srv.Close()
	b := &controlplaneBackend{baseURL: srv.URL, image: "alpine:3.20", client: srv.Client()}
	txt, isErr := cpInvoke(t, b, `{"command":"true"}`)
	if !isErr {
		t.Fatalf("launch failure should be a tool error")
	}
	if !strings.Contains(txt, "runsc not found") {
		t.Errorf("result should surface the launch error:\n%s", txt)
	}
}

// An unreachable control-plane is a fail-closed tool error, NOT a host fallback.
func TestControlplaneBackendUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // now refuses connections
	b := &controlplaneBackend{baseURL: url, image: "alpine:3.20", client: &http.Client{}}
	txt, isErr := cpInvoke(t, b, `{"command":"true"}`)
	if !isErr {
		t.Fatalf("unreachable control-plane should be a tool error")
	}
	if !strings.Contains(txt, "unreachable") || !strings.Contains(txt, "fail-closed") {
		t.Errorf("result should state fail-closed unreachability:\n%s", txt)
	}
}

// Empty command is rejected before any HTTP call.
func TestControlplaneBackendEmptyCommand(t *testing.T) {
	b := &controlplaneBackend{baseURL: "http://127.0.0.1:0", client: &http.Client{}}
	if _, err := b.toolFunc()(context.Background(), json.RawMessage(`{"command":"  "}`)); err == nil {
		t.Errorf("empty command should error")
	}
}
