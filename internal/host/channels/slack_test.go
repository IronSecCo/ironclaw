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

// SlackAdapter must satisfy the Adapter interface.
var _ Adapter = (*SlackAdapter)(nil)

func TestSlackAdapterDelivers(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody slackPostMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"ts":"1700000000.000200"}`)
	}))
	defer srv.Close()

	a := NewSlackAdapter("slack", "TESTTOKEN")
	a.BaseURL = srv.URL

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "hello", PlatformID: strptr("C123"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "1700000000.000200" {
		t.Fatalf("id = %q, want the slack ts", id)
	}
	if gotPath != "/chat.postMessage" {
		t.Fatalf("request path = %q, want /chat.postMessage", gotPath)
	}
	if gotAuth != "Bearer TESTTOKEN" {
		t.Fatalf("auth header = %q, want Bearer TESTTOKEN", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody.Channel != "C123" || gotBody.Text != "hello" {
		t.Errorf("upstream body = %+v", gotBody)
	}
	if gotBody.ThreadTS != "" {
		t.Errorf("no thread_ts expected, got %q", gotBody.ThreadTS)
	}
}

func TestSlackAdapterMapsThreadTS(t *testing.T) {
	var gotBody slackPostMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"ok":true,"ts":"1.2"}`)
	}))
	defer srv.Close()

	a := NewSlackAdapter("slack", "TESTTOKEN")
	a.BaseURL = srv.URL

	// A non-empty thread id is passed straight through as thread_ts.
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("C1"), ThreadID: strptr("1699999999.000100"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.ThreadTS != "1699999999.000100" {
		t.Fatalf("thread_ts = %q, want 1699999999.000100", gotBody.ThreadTS)
	}

	// An empty/whitespace thread id is omitted.
	gotBody = slackPostMessage{}
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("C1"), ThreadID: strptr("  "),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.ThreadTS != "" {
		t.Fatalf("blank thread id must be omitted, got %q", gotBody.ThreadTS)
	}
}

func TestSlackAdapterErrorsOnNotOK(t *testing.T) {
	// Slack returns HTTP 200 with ok=false on logical errors.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":false,"error":"channel_not_found"}`)
	}))
	defer srv.Close()

	a := NewSlackAdapter("slack", "TESTTOKEN")
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("C1")})
	if err == nil {
		t.Fatal("expected an error when ok=false")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Fatalf("error should carry the slack error code, got %v", err)
	}
}

func TestSlackAdapterRequiresChannel(t *testing.T) {
	a := NewSlackAdapter("slack", "TESTTOKEN")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Fatal("expected an error when PlatformID (channel) is nil")
	}
}

func TestSlackAdapterRequiresToken(t *testing.T) {
	a := NewSlackAdapter("slack", "")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("C1")}); err == nil {
		t.Fatal("expected an error with no bot token")
	}
}

// TestSlackAdapterRedactsToken: even if an upstream error string were to echo the
// token, the adapter must redact it before returning.
func TestSlackAdapterRedactsToken(t *testing.T) {
	const token = "xoxb-SUPERSECRET-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Contrived upstream that reflects the token in its error description.
		_, _ = io.WriteString(w, `{"ok":false,"error":"bad token `+token+`"}`)
	}))
	defer srv.Close()

	a := NewSlackAdapter("slack", token)
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("C1")})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("bot token leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("expected the token to be redacted, got %v", err)
	}
}
