package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// GoogleChatAdapter must satisfy the Adapter interface.
var _ Adapter = (*GoogleChatAdapter)(nil)

func TestGoogleChatAdapterDelivers(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody gchatMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"spaces/AAA/messages/MMM.NNN"}`)
	}))
	defer srv.Close()

	a := NewGoogleChatAdapter("googlechat", "TESTTOKEN")
	a.BaseURL = srv.URL

	// A bare space id is normalized to the spaces/ resource form.
	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "hello chat", PlatformID: strptr("AAA"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "spaces/AAA/messages/MMM.NNN" {
		t.Fatalf("id = %q, want the message resource name", id)
	}
	if gotPath != "/v1/spaces/AAA/messages" {
		t.Fatalf("path = %q, want /v1/spaces/AAA/messages", gotPath)
	}
	if gotAuth != "Bearer TESTTOKEN" {
		t.Fatalf("auth header = %q, want Bearer TESTTOKEN", gotAuth)
	}
	if gotBody.Text != "hello chat" {
		t.Errorf("upstream body text = %q", gotBody.Text)
	}
	if gotBody.Thread != nil {
		t.Errorf("no thread expected, got %+v", gotBody.Thread)
	}
}

func TestGoogleChatAdapterAcceptsFullSpaceResource(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"name":"spaces/BBB/messages/X"}`)
	}))
	defer srv.Close()

	a := NewGoogleChatAdapter("googlechat", "TESTTOKEN")
	a.BaseURL = srv.URL

	// An already-qualified "spaces/BBB" must not be double-prefixed.
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("spaces/BBB"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/spaces/BBB/messages" {
		t.Fatalf("path = %q, want /v1/spaces/BBB/messages", gotPath)
	}
}

func TestGoogleChatAdapterMapsThreadKey(t *testing.T) {
	var gotBody gchatMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"name":"spaces/AAA/messages/X"}`)
	}))
	defer srv.Close()

	a := NewGoogleChatAdapter("googlechat", "TESTTOKEN")
	a.BaseURL = srv.URL

	// A non-empty thread id sets thread.threadKey.
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "reply", PlatformID: strptr("AAA"), ThreadID: strptr("topic-7"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.Thread == nil || gotBody.Thread.ThreadKey != "topic-7" {
		t.Fatalf("thread = %+v, want threadKey topic-7", gotBody.Thread)
	}

	// A blank thread id omits the thread.
	gotBody = gchatMessage{}
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("AAA"), ThreadID: strptr("  "),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.Thread != nil {
		t.Fatalf("blank thread id must omit thread, got %+v", gotBody.Thread)
	}
}

func TestGoogleChatAdapterErrorsOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED"}}`)
	}))
	defer srv.Close()

	a := NewGoogleChatAdapter("googlechat", "TESTTOKEN")
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("AAA")})
	if err == nil {
		t.Fatal("expected an error on a non-2xx response")
	}
	if !strings.Contains(err.Error(), "The caller does not have permission") {
		t.Fatalf("error should carry the Google API message, got %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error should carry the HTTP status, got %v", err)
	}
}

func TestGoogleChatAdapterRequiresSpace(t *testing.T) {
	a := NewGoogleChatAdapter("googlechat", "TESTTOKEN")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Fatal("expected an error when PlatformID (space) is nil")
	}
}

func TestGoogleChatAdapterRequiresToken(t *testing.T) {
	a := NewGoogleChatAdapter("googlechat", "")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("AAA")}); err == nil {
		t.Fatal("expected an error with no access token")
	}
}

// TestGoogleChatAdapterRedactsToken: even if an upstream error string were to
// echo the token, the adapter must redact it before returning.
func TestGoogleChatAdapterRedactsToken(t *testing.T) {
	const token = "ya29.SUPERSECRET-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":401,"message":"invalid token `+token+`","status":"UNAUTHENTICATED"}}`)
	}))
	defer srv.Close()

	a := NewGoogleChatAdapter("googlechat", token)
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("AAA")})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("access token leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("expected the token to be redacted, got %v", err)
	}
}
