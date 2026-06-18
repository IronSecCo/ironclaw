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

// MatrixAdapter must satisfy the Adapter interface.
var _ Adapter = (*MatrixAdapter)(nil)

func TestMatrixAdapterDelivers(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody matrixMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"event_id":"$evt123:example.org"}`)
	}))
	defer srv.Close()

	a := NewMatrixAdapter("matrix", srv.URL, "TESTTOKEN")

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "hello matrix", PlatformID: strptr("!room:example.org"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "$evt123:example.org" {
		t.Fatalf("id = %q, want the event_id", id)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method = %q, want PUT", gotMethod)
	}
	if !strings.HasPrefix(gotPath, "/_matrix/client/v3/rooms/") || !strings.Contains(gotPath, "/send/m.room.message/") {
		t.Fatalf("path = %q, want the client-server send endpoint", gotPath)
	}
	// The txn id is the final, non-empty path segment.
	if seg := gotPath[strings.LastIndex(gotPath, "/")+1:]; seg == "" {
		t.Errorf("expected a non-empty transaction id in the path: %q", gotPath)
	}
	if gotAuth != "Bearer TESTTOKEN" {
		t.Fatalf("auth header = %q, want Bearer TESTTOKEN", gotAuth)
	}
	if gotBody.MsgType != "m.text" || gotBody.Body != "hello matrix" {
		t.Errorf("upstream body = %+v", gotBody)
	}
	if gotBody.RelatesTo != nil {
		t.Errorf("no thread relation expected, got %+v", gotBody.RelatesTo)
	}
}

func TestMatrixAdapterMapsThreadRelation(t *testing.T) {
	var gotBody matrixMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"event_id":"$x"}`)
	}))
	defer srv.Close()

	a := NewMatrixAdapter("matrix", srv.URL, "TESTTOKEN")

	// A non-empty thread id becomes an m.thread relation.
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "reply", PlatformID: strptr("!room:example.org"), ThreadID: strptr("$root:example.org"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.RelatesTo == nil || gotBody.RelatesTo.RelType != "m.thread" ||
		gotBody.RelatesTo.EventID != "$root:example.org" {
		t.Fatalf("relates_to = %+v, want an m.thread relation to $root:example.org", gotBody.RelatesTo)
	}

	// A blank thread id omits the relation.
	gotBody = matrixMessage{}
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("!room:example.org"), ThreadID: strptr("  "),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.RelatesTo != nil {
		t.Fatalf("blank thread id must omit the relation, got %+v", gotBody.RelatesTo)
	}
}

func TestMatrixAdapterErrorsOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"errcode":"M_FORBIDDEN","error":"You are not in the room"}`)
	}))
	defer srv.Close()

	a := NewMatrixAdapter("matrix", srv.URL, "TESTTOKEN")

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("!r:example.org")})
	if err == nil {
		t.Fatal("expected an error on a non-2xx response")
	}
	if !strings.Contains(err.Error(), "You are not in the room") {
		t.Fatalf("error should carry the Matrix error, got %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error should carry the HTTP status, got %v", err)
	}
}

func TestMatrixAdapterRequiresRoom(t *testing.T) {
	a := NewMatrixAdapter("matrix", "https://matrix.example.org", "TESTTOKEN")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Fatal("expected an error when PlatformID (room) is nil")
	}
}

func TestMatrixAdapterRequiresToken(t *testing.T) {
	a := NewMatrixAdapter("matrix", "https://matrix.example.org", "")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("!r:example.org")}); err == nil {
		t.Fatal("expected an error with no access token")
	}
}

func TestMatrixAdapterRequiresHomeserver(t *testing.T) {
	a := NewMatrixAdapter("matrix", "", "TESTTOKEN")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("!r:example.org")}); err == nil {
		t.Fatal("expected an error with no homeserver URL")
	}
}

// TestMatrixAdapterRedactsToken: even if an upstream error string were to echo
// the token, the adapter must redact it before returning.
func TestMatrixAdapterRedactsToken(t *testing.T) {
	const token = "syt_SUPERSECRET_123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"errcode":"M_UNKNOWN_TOKEN","error":"bad token `+token+`"}`)
	}))
	defer srv.Close()

	a := NewMatrixAdapter("matrix", srv.URL, token)

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("!r:example.org")})
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
