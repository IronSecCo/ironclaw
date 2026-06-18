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

// DiscordAdapter must satisfy the Adapter interface.
var _ Adapter = (*DiscordAdapter)(nil)

func TestDiscordAdapterDelivers(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody discordCreateMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"1234567890"}`)
	}))
	defer srv.Close()

	a := NewDiscordAdapter("discord", "TESTTOKEN")
	a.BaseURL = srv.URL

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "hello", PlatformID: strptr("C42"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "1234567890" {
		t.Fatalf("id = %q, want the discord snowflake", id)
	}
	if gotPath != "/channels/C42/messages" {
		t.Fatalf("request path = %q, want /channels/C42/messages", gotPath)
	}
	if gotAuth != "Bot TESTTOKEN" {
		t.Fatalf("auth header = %q, want Bot TESTTOKEN", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody.Content != "hello" {
		t.Errorf("upstream body = %+v", gotBody)
	}
	if gotBody.MessageReference != nil {
		t.Errorf("no message_reference expected, got %+v", gotBody.MessageReference)
	}
}

func TestDiscordAdapterMapsNumericThreadID(t *testing.T) {
	var gotBody discordCreateMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"id":"1"}`)
	}))
	defer srv.Close()

	a := NewDiscordAdapter("discord", "TESTTOKEN")
	a.BaseURL = srv.URL

	// Numeric thread id -> message_reference (reply threading).
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("C1"), ThreadID: strptr("987654321"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.MessageReference == nil || gotBody.MessageReference.MessageID != "987654321" {
		t.Fatalf("message_reference = %+v, want message_id 987654321", gotBody.MessageReference)
	}

	// A non-numeric thread id is omitted (not a Discord snowflake).
	gotBody = discordCreateMessage{}
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("C1"), ThreadID: strptr("ses_abc"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.MessageReference != nil {
		t.Fatalf("non-numeric thread id must be omitted, got %+v", gotBody.MessageReference)
	}
}

func TestDiscordAdapterErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"Missing Access","code":50001}`)
	}))
	defer srv.Close()

	a := NewDiscordAdapter("discord", "TESTTOKEN")
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("C1")})
	if err == nil {
		t.Fatal("expected an error on a non-2xx status")
	}
	if !strings.Contains(err.Error(), "Missing Access") {
		t.Fatalf("error should carry the discord message, got %v", err)
	}
}

func TestDiscordAdapterRequiresChannel(t *testing.T) {
	a := NewDiscordAdapter("discord", "TESTTOKEN")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Fatal("expected an error when PlatformID (channel) is nil")
	}
}

func TestDiscordAdapterRequiresToken(t *testing.T) {
	a := NewDiscordAdapter("discord", "")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("C1")}); err == nil {
		t.Fatal("expected an error with no bot token")
	}
}

// TestDiscordAdapterRedactsToken: even if an upstream error string echoes the
// token, the adapter must redact it before returning.
func TestDiscordAdapterRedactsToken(t *testing.T) {
	const token = "MTk4N.SECRET.bot-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"bad token `+token+`","code":0}`)
	}))
	defer srv.Close()

	a := NewDiscordAdapter("discord", token)
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
