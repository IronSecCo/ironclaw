package main

import (
	"net"
	"path/filepath"
	"testing"
)

func TestCheckAPI(t *testing.T) {
	token = ""
	srv, _, _ := newStatusServer(t)

	if r := checkAPI(srv.URL); r.Status != checkOK {
		t.Errorf("checkAPI(reachable) = %s (%s), want OK", r.Status, r.Detail)
	}
	if r := checkAPI("http://127.0.0.1:1"); r.Status != checkFail {
		t.Errorf("checkAPI(unreachable) = %s, want FAIL", r.Status)
	} else if r.Fix == "" {
		t.Error("expected an actionable fix on FAIL")
	}
}

func TestCheckReadiness(t *testing.T) {
	token = ""
	srv, _, _ := newStatusServer(t)
	if r := checkReadiness(srv.URL); r.Status != checkOK {
		t.Errorf("checkReadiness = %s (%s), want OK", r.Status, r.Detail)
	}
}

func TestCheckAuthUngated(t *testing.T) {
	token = ""
	srv, _, _ := newStatusServer(t)
	// Ungated server + no token → WARN with a hardening fix.
	if r := checkAuth(srv.URL); r.Status != checkWarn || r.Fix == "" {
		t.Errorf("checkAuth(ungated) = %s fix=%q, want WARN with fix", r.Status, r.Fix)
	}
}

func TestCheckRuntime(t *testing.T) {
	// `go` is guaranteed on PATH in the build/test environment.
	if r := checkRuntime("go"); r.Status != checkOK {
		t.Errorf("checkRuntime(go) = %s (%s), want OK", r.Status, r.Detail)
	}
	if r := checkRuntime("definitely-not-a-real-binary-xyz123"); r.Status != checkFail {
		t.Errorf("checkRuntime(missing) = %s, want FAIL", r.Status)
	} else if r.Fix == "" {
		t.Error("expected an install fix when the runtime is missing")
	}
}

func TestCheckModelProxy(t *testing.T) {
	// Missing socket → WARN.
	missing := filepath.Join(t.TempDir(), "absent.sock")
	if r := checkModelProxy(missing); r.Status != checkWarn {
		t.Errorf("checkModelProxy(missing) = %s, want WARN", r.Status)
	}

	// A real, listening unix socket → OK.
	sock := filepath.Join(t.TempDir(), "p.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()
	if r := checkModelProxy(sock); r.Status != checkOK {
		t.Errorf("checkModelProxy(live) = %s (%s), want OK", r.Status, r.Detail)
	}
}

func TestDoctorCommandSmoke(t *testing.T) {
	token = ""
	srv, _, _ := newStatusServer(t)
	// Some checks (runtime/model-proxy) will FAIL/WARN in CI, so doctor returns a
	// non-nil error; that's expected. Assert it does not panic and runs the HTTP
	// checks against a live server (no crash, returns).
	_ = run([]string{"--addr", srv.URL, "doctor", "--runtime", "go",
		"--model-proxy-socket", filepath.Join(t.TempDir(), "none.sock")})
}
