package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/gateway"
)

// newUITestServer builds a Server with the gateway wired and an optional token,
// exercising the same Handler() middleware chain the daemon serves.
func newUITestServer(t *testing.T, token string) http.Handler {
	t.Helper()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	s := New(gw)
	if token != "" {
		s = s.WithToken(token)
	}
	return s.Handler()
}

// TestUIShellServedWithoutToken: the static console shell loads even when a bearer
// token is configured (a browser can't header a navigation), and it is the real
// embedded page.
func TestUIShellServedWithoutToken(t *testing.T) {
	h := newUITestServer(t, "s3cret")

	// FileServer serves index.html for the directory request /ui/ (and 301s a
	// literal /ui/index.html back to /ui/), so the shell lives at /ui/.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/ with token set: got %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "IronClaw") {
		t.Errorf("served shell does not look like the console: %.80q", body)
	}

	// Bare /ui redirects to /ui/ so relative asset URLs resolve.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui", nil))
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("GET /ui: got %d, want 301 redirect", rec.Code)
	}
}

// TestUIDoesNotExemptAPI: the carve-out is strictly the shell. The /v1 API the
// console drives still requires the bearer token — the whole point of the design.
func TestUIDoesNotExemptAPI(t *testing.T) {
	h := newUITestServer(t, "s3cret")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/changes/pending", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/changes/pending without token: got %d, want 401", rec.Code)
	}

	// A /ui prefix must not be smuggleable onto a /v1 path.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/audit", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/audit without token: got %d, want 401", rec.Code)
	}
}

// TestUIServedWhenNoToken: with no token configured (dev/loopback), the shell is
// served the same way through the open API.
func TestUIServedWhenNoToken(t *testing.T) {
	h := newUITestServer(t, "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/app.js", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /ui/app.js: got %d, want 200", rec.Code)
	}
}
