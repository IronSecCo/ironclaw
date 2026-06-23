package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// Ungated server + no token -> WARN with a hardening fix.
	if r := checkAuth(srv.URL); r.Status != checkWarn || r.Fix == "" {
		t.Errorf("checkAuth(ungated) = %s fix=%q, want WARN with fix", r.Status, r.Fix)
	}
}

func TestCheckRuntime(t *testing.T) {
	// `go` is guaranteed on PATH but is not runsc, so it exercises the "found but
	// relaxed runtime" branch: present (not a FAIL) yet flagged WARN because it
	// does not provide gVisor's syscall-interception isolation boundary.
	if r := checkRuntime("go"); r.Status != checkWarn {
		t.Errorf("checkRuntime(go) = %s (%s), want WARN (relaxed)", r.Status, r.Detail)
	} else if !strings.Contains(r.Detail, "relaxed") {
		t.Errorf("checkRuntime(go): detail %q should flag the weaker isolation", r.Detail)
	}
	// A missing runtime is a hard FAIL on Linux (where gVisor is required) but a
	// soft WARN on macOS/Windows, where the production sandbox runs on the Linux
	// host and `runsc` is not expected — so `doctor` should not exit non-zero
	// just for being run on a dev laptop.
	wantMissing := checkFail
	if runtime.GOOS != "linux" {
		wantMissing = checkWarn
	}
	if r := checkRuntime("definitely-not-a-real-binary-xyz123"); r.Status != wantMissing {
		t.Errorf("checkRuntime(missing) on %s = %s, want %s", runtime.GOOS, r.Status, wantMissing)
	} else if r.Fix == "" {
		t.Error("expected an actionable fix when the runtime is missing")
	}
}

func TestCheckModelProxy(t *testing.T) {
	// Missing socket -> WARN.
	missing := filepath.Join(t.TempDir(), "absent.sock")
	if r := checkModelProxy(missing); r.Status != checkWarn {
		t.Errorf("checkModelProxy(missing) = %s, want WARN", r.Status)
	}

	// A real, listening unix socket -> OK.
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
	// Some checks (runtime/model-proxy) will FAIL/WARN in CI, so doctor may return
	// a non-nil error; that's expected. Assert it does not panic and runs the HTTP
	// checks against a live server (no crash, returns).
	_ = run([]string{"--addr", srv.URL, "doctor", "--runtime", "go",
		"--model-proxy-socket", filepath.Join(t.TempDir(), "none.sock")})
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("IRO_TEST_DOCTOR", "")
	if got := envOrDefault("IRO_TEST_DOCTOR", "fallback"); got != "fallback" {
		t.Errorf("empty env: got %q, want fallback", got)
	}
	t.Setenv("IRO_TEST_DOCTOR", "set")
	if got := envOrDefault("IRO_TEST_DOCTOR", "fallback"); got != "set" {
		t.Errorf("set env: got %q, want set", got)
	}
}

func TestCheckModelCredential(t *testing.T) {
	// No credential -> WARN, not FAIL (the mock provider still works).
	none := checkModelCredential(func(string) string { return "" })
	if none.Status != checkWarn {
		t.Errorf("no cred: status = %s, want WARN", none.Status)
	}
	if none.Fix == "" || none.Doc == "" {
		t.Error("no cred: expected a fix hint and a docs link")
	}

	// A configured credential -> OK, named, and the secret VALUE is never echoed.
	secret := "sk-super-secret-value"
	env := map[string]string{"ANTHROPIC_API_KEY": secret}
	ok := checkModelCredential(func(k string) string { return env[k] })
	if ok.Status != checkOK {
		t.Fatalf("with cred: status = %s, want OK", ok.Status)
	}
	if !strings.Contains(ok.Detail, "Anthropic") {
		t.Errorf("with cred: detail %q should name the provider", ok.Detail)
	}
	if strings.Contains(ok.Detail+ok.Fix, secret) {
		t.Error("secret value leaked into doctor output - presence only")
	}
}

func TestCheckChannels(t *testing.T) {
	none := checkChannels(func(string) string { return "" })
	if none.Status != checkWarn {
		t.Errorf("no channel: status = %s, want WARN", none.Status)
	}

	tok := "xoxb-secret-token"
	env := map[string]string{"SLACK_BOT_TOKEN": tok}
	armed := checkChannels(func(k string) string { return env[k] })
	if armed.Status != checkOK {
		t.Fatalf("armed: status = %s, want OK", armed.Status)
	}
	if !strings.Contains(armed.Detail, "slack") {
		t.Errorf("armed: detail %q should name slack", armed.Detail)
	}
	if strings.Contains(armed.Detail, tok) {
		t.Error("channel token value leaked into doctor output - presence only")
	}
}

func TestCheckToolchain(t *testing.T) {
	r := checkToolchain()
	if r.Status != checkOK {
		t.Errorf("toolchain: status = %s, want OK", r.Status)
	}
	if !strings.Contains(r.Detail, "CGO_ENABLED=1") {
		t.Errorf("toolchain: detail %q should state the CGO build expectation", r.Detail)
	}
	if !strings.Contains(r.Detail, runtime.Version()) {
		t.Errorf("toolchain: detail %q should report the Go version", r.Detail)
	}
}

func TestCheckConfig(t *testing.T) {
	dir := t.TempDir()

	// Absent -> WARN (onboard creates it on demand).
	missing := filepath.Join(dir, "absent.env")
	t.Setenv("IRONCLAW_CONFIG", missing)
	if r := checkConfig(); r.Status != checkWarn {
		t.Errorf("absent config: status = %s, want WARN", r.Status)
	}

	// Present and owner-only -> OK.
	secure := filepath.Join(dir, "secure.env")
	if err := os.WriteFile(secure, []byte("IRONCLAW_API_TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IRONCLAW_CONFIG", secure)
	if r := checkConfig(); r.Status != checkOK {
		t.Errorf("0600 config: status = %s, want OK", r.Status)
	}

	// World-readable secret-bearing file -> WARN (POSIX only).
	if runtime.GOOS != "windows" {
		loose := filepath.Join(dir, "loose.env")
		if err := os.WriteFile(loose, []byte("IRONCLAW_API_TOKEN=x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("IRONCLAW_CONFIG", loose)
		r := checkConfig()
		if r.Status != checkWarn {
			t.Errorf("0644 config: status = %s, want WARN", r.Status)
		}
		if !strings.Contains(r.Fix, "chmod 600") {
			t.Errorf("0644 config: fix %q should suggest chmod 600", r.Fix)
		}
	}
}

func TestPrintChecks(t *testing.T) {
	var buf bytes.Buffer
	printChecks(&buf, []checkResult{
		{Name: "ok-check", Status: checkOK, Detail: "fine", Fix: "unused", Doc: "https://example/ok"},
		{Name: "bad-check", Status: checkFail, Detail: "broke", Fix: "do x", Doc: "https://example/fix"},
	})
	out := buf.String()
	// OK lines never print fix/see; failing lines print both.
	if strings.Contains(out, "https://example/ok") || strings.Contains(out, "unused") {
		t.Error("OK check should not print its fix or doc link")
	}
	if !strings.Contains(out, "do x") || !strings.Contains(out, "https://example/fix") {
		t.Error("failing check should print both the fix hint and the doc link")
	}
}
