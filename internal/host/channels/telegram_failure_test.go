package channels

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// TestTelegramAdapterErrorsOnMalformedJSON covers the response-decode failure
// branch: a platform that returns a 200 with a non-JSON body must surface a clear
// decode error (carrying the HTTP status) rather than silently succeeding.
func TestTelegramAdapterErrorsOnMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>gateway error</html>")
	}))
	defer srv.Close()

	a := NewTelegramAdapter("tg", "TESTTOKEN")
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("1")})
	if err == nil {
		t.Fatal("expected a decode error on a non-JSON response")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("error should name the decode failure, got %v", err)
	}
}

// TestTelegramAdapterHonorsContextCancellation covers the transport-error branch
// driven by a cancelled context: an already-cancelled deliver must fail fast (and
// still redact the token-bearing URL).
func TestTelegramAdapterHonorsContextCancellation(t *testing.T) {
	const token = "SECRET-CTX-TOKEN"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // never respond; rely on client-side cancellation
	}))
	defer srv.Close()

	a := NewTelegramAdapter("tg", token)
	a.BaseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the request is made

	_, err := a.Deliver(ctx, contract.MessageOut{Content: "x", PlatformID: strptr("1")})
	if err == nil {
		t.Fatal("expected an error when the context is cancelled")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("bot token leaked into cancellation error: %v", err)
	}
}
