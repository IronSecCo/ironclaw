package main

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// captureStdouterr runs fn with os.Stdout and os.Stderr redirected to pipes and
// returns whatever each stream received.
func captureStdouterr(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	fn()

	outW.Close()
	errW.Close()
	ob, _ := io.ReadAll(outR)
	eb, _ := io.ReadAll(errR)
	return string(ob), string(eb)
}

func resp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestPrintBodySuccessIsQuiet(t *testing.T) {
	verbose = false
	t.Cleanup(func() { verbose = false })

	// 202 with a JSON body: the body prints, but no raw "HTTP 202" line leaks.
	out, errOut := captureStdouterr(t, func() {
		if err := printBody(resp(http.StatusAccepted, `{"id":"chg_1"}`)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if strings.Contains(out, "HTTP") {
		t.Errorf("success output leaked an HTTP status line: %q", out)
	}
	if !strings.Contains(out, `{"id":"chg_1"}`) {
		t.Errorf("expected body in stdout, got %q", out)
	}
	if errOut != "" {
		t.Errorf("expected empty stderr on success, got %q", errOut)
	}
}

func TestPrintBodyVerboseShowsStatus(t *testing.T) {
	verbose = true
	t.Cleanup(func() { verbose = false })

	out, _ := captureStdouterr(t, func() {
		_ = printBody(resp(http.StatusOK, "[]"))
	})
	if !strings.Contains(out, "HTTP 200") {
		t.Errorf("verbose mode should print the status line, got %q", out)
	}
}

func TestPrintBodyErrorGoesToStderr(t *testing.T) {
	verbose = false
	t.Cleanup(func() { verbose = false })

	var retErr error
	out, errOut := captureStdouterr(t, func() {
		retErr = printBody(resp(http.StatusServiceUnavailable, "model host unreachable"))
	})
	if retErr == nil {
		t.Fatal("expected an error for a 503 response")
	}
	if out != "" {
		t.Errorf("error responses must not write to stdout, got %q", out)
	}
	if !strings.Contains(errOut, "HTTP 503") || !strings.Contains(errOut, "model host unreachable") {
		t.Errorf("expected status + reason on stderr, got %q", errOut)
	}
}
