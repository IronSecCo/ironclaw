// OWNER: T-040

package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// TelegramAdapter must satisfy the Adapter interface.
var _ Adapter = (*TelegramAdapter)(nil)

func strptr(s string) *string { return &s }

func TestTelegramAdapterDelivers(t *testing.T) {
	var gotPath, gotCT string
	var gotBody tgSendMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":4242}}`)
	}))
	defer srv.Close()

	a := NewTelegramAdapter("tg", "TESTTOKEN")
	a.BaseURL = srv.URL

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "hello", PlatformID: strptr("99887766"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "4242" {
		t.Fatalf("id = %q, want 4242 (telegram message_id)", id)
	}
	if gotPath != "/botTESTTOKEN/sendMessage" {
		t.Fatalf("request path = %q, want /botTESTTOKEN/sendMessage", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody.ChatID != "99887766" || gotBody.Text != "hello" {
		t.Errorf("upstream body = %+v", gotBody)
	}
	if gotBody.MessageThreadID != nil {
		t.Errorf("no thread id expected, got %v", *gotBody.MessageThreadID)
	}
}

func TestTelegramAdapterMapsNumericThreadID(t *testing.T) {
	var gotBody tgSendMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	defer srv.Close()

	a := NewTelegramAdapter("tg", "TESTTOKEN")
	a.BaseURL = srv.URL

	// Numeric thread id -> forum topic message_thread_id.
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("1"), ThreadID: strptr("77"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.MessageThreadID == nil || *gotBody.MessageThreadID != 77 {
		t.Fatalf("message_thread_id = %v, want 77", gotBody.MessageThreadID)
	}

	// A non-numeric thread id is omitted (it is not a Telegram topic id).
	gotBody = tgSendMessage{}
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("1"), ThreadID: strptr("ses_abc"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.MessageThreadID != nil {
		t.Fatalf("non-numeric thread id must be omitted, got %v", *gotBody.MessageThreadID)
	}
}

func TestTelegramAdapterErrorsOnNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"chat not found"}`)
	}))
	defer srv.Close()

	a := NewTelegramAdapter("tg", "TESTTOKEN")
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("1")})
	if err == nil {
		t.Fatal("expected an error when ok=false")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("error should carry the description, got %v", err)
	}
}

func TestTelegramAdapterRequiresChatID(t *testing.T) {
	a := NewTelegramAdapter("tg", "TESTTOKEN")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Fatal("expected an error when PlatformID (chat id) is nil")
	}
}

func TestTelegramAdapterRequiresToken(t *testing.T) {
	a := NewTelegramAdapter("tg", "")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("1")}); err == nil {
		t.Fatal("expected an error with no bot token")
	}
}

// TestTelegramAdapterRedactsTokenOnTransportError: a transport failure's error
// embeds the token-bearing URL; the adapter must redact the token.
func TestTelegramAdapterRedactsTokenOnTransportError(t *testing.T) {
	const token = "SUPERSECRET-TOKEN-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close() // force a connection-refused transport error

	a := NewTelegramAdapter("tg", token)
	a.BaseURL = closedURL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("1")})
	if err == nil {
		t.Fatal("expected a transport error against a closed server")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("bot token leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("expected the token to be redacted, got %v", err)
	}
}
