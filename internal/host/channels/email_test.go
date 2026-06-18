package channels

import (
	"context"
	"errors"
	"net/smtp"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// EmailAdapter must satisfy the Adapter interface.
var _ Adapter = (*EmailAdapter)(nil)

// captureSend is a fake emailSendFunc that records its arguments.
type captureSend struct {
	addr string
	auth smtp.Auth
	from string
	to   []string
	msg  string
	err  error
}

func (c *captureSend) fn(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
	c.addr, c.auth, c.from, c.to, c.msg = addr, a, from, to, string(msg)
	return c.err
}

func TestEmailAdapterDelivers(t *testing.T) {
	cap := &captureSend{}
	// port 0 must default to 587.
	a := NewEmailAdapter("email", "smtp.example.com", 0, "bot@bots.example.com", "APPPASS", "bot@bots.example.com")
	a.send = cap.fn

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "Hello world\nsecond line", PlatformID: strptr("alice@example.com"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected a non-empty Message-ID as the platform id")
	}
	if cap.addr != "smtp.example.com:587" {
		t.Fatalf("addr = %q, want smtp.example.com:587 (default port)", cap.addr)
	}
	if cap.auth == nil {
		t.Error("expected non-nil SMTP auth when credentials are set")
	}
	if cap.from != "bot@bots.example.com" {
		t.Errorf("envelope from = %q", cap.from)
	}
	if len(cap.to) != 1 || cap.to[0] != "alice@example.com" {
		t.Errorf("envelope recipients = %v", cap.to)
	}
	// Headers + body.
	for _, want := range []string{
		"From: bot@bots.example.com\r\n",
		"To: alice@example.com\r\n",
		"Subject: Hello world\r\n", // derived from the first line, not "second line"
		"Message-ID: " + id + "\r\n",
		"MIME-Version: 1.0\r\n",
	} {
		if !strings.Contains(cap.msg, want) {
			t.Errorf("composed message missing %q\n--- got ---\n%s", want, cap.msg)
		}
	}
	if !strings.Contains(cap.msg, "Hello world\r\nsecond line") {
		t.Errorf("body not CRLF-normalized / missing:\n%s", cap.msg)
	}
	if !strings.Contains(id, "@bots.example.com>") {
		t.Errorf("message id domain = %q, want the From domain", id)
	}
}

func TestEmailAdapterThreadingHeaders(t *testing.T) {
	cap := &captureSend{}
	a := NewEmailAdapter("email", "smtp.example.com", 587, "u", "p", "bot@x.com")
	a.send = cap.fn

	// A non-empty thread id sets In-Reply-To + References.
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "re: hi", PlatformID: strptr("alice@example.com"), ThreadID: strptr("<prior@x.com>"),
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.msg, "In-Reply-To: <prior@x.com>\r\n") ||
		!strings.Contains(cap.msg, "References: <prior@x.com>\r\n") {
		t.Errorf("expected threading headers, got:\n%s", cap.msg)
	}

	// A blank thread id omits them.
	cap.msg = ""
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("alice@example.com"), ThreadID: strptr("   "),
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cap.msg, "In-Reply-To") {
		t.Errorf("blank thread id must omit In-Reply-To, got:\n%s", cap.msg)
	}
}

func TestEmailAdapterSubjectFallback(t *testing.T) {
	cap := &captureSend{}
	a := NewEmailAdapter("email", "smtp.example.com", 587, "u", "p", "bot@x.com")
	a.send = cap.fn
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "", PlatformID: strptr("alice@example.com"),
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.msg, "Subject: (no subject)\r\n") {
		t.Errorf("empty content should yield a fallback subject, got:\n%s", cap.msg)
	}
}

func TestEmailAdapterRequiresRecipient(t *testing.T) {
	cap := &captureSend{}
	a := NewEmailAdapter("email", "smtp.example.com", 587, "u", "p", "bot@x.com")
	a.send = cap.fn
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Fatal("expected an error when PlatformID (recipient) is nil")
	}
	if cap.addr != "" {
		t.Error("send must not be called when validation fails")
	}
}

func TestEmailAdapterRequiresHostAndFrom(t *testing.T) {
	noHost := NewEmailAdapter("email", "", 587, "u", "p", "bot@x.com")
	if _, err := noHost.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("a@x.com")}); err == nil {
		t.Error("expected an error with no SMTP host")
	}
	noFrom := NewEmailAdapter("email", "smtp.example.com", 587, "u", "p", "")
	if _, err := noFrom.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("a@x.com")}); err == nil {
		t.Error("expected an error with no From address")
	}
}

// TestEmailAdapterRedactsPassword: even if the SMTP layer's error echoes the
// password, the adapter must redact it before returning.
func TestEmailAdapterRedactsPassword(t *testing.T) {
	const pass = "SUPER-SECRET-APPPASS"
	cap := &captureSend{err: errors.New("535 auth failed for password " + pass)}
	a := NewEmailAdapter("email", "smtp.example.com", 587, "bot@x.com", pass, "bot@x.com")
	a.send = cap.fn

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("a@x.com")})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), pass) {
		t.Fatalf("password leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("expected the password to be redacted, got %v", err)
	}
}
