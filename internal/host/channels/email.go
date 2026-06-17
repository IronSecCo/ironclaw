// OWNER: T-229

package channels

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// emailSendFunc is the seam used to send an assembled RFC 5322 message. It
// matches net/smtp.SendMail (the production default) so tests can inject a fake
// transport — the idiomatic way to unit-test net/smtp code without standing up a
// real mail server.
type emailSendFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

// EmailAdapter is a concrete, stdlib-only Adapter that delivers an outbound
// message as email over SMTP (net/smtp). It works with any SMTP submission
// server, including Gmail (smtp.gmail.com:587 with an app password) — satisfying
// the "Email / Gmail" channel via the universal protocol rather than a
// provider-specific API.
//
// The recipient is MessageOut.PlatformID (an email address). The subject is
// derived from the first line of the content; a non-empty MessageOut.ThreadID is
// echoed into the In-Reply-To / References headers so replies thread in the
// recipient's client. Deliver returns the generated RFC 5322 Message-ID as the
// platform message id (SMTP itself returns none).
//
// Inbound ingestion (IMAP / Gmail Pub-Sub) is intentionally out of scope here —
// the task lists it as optional — so this adapter is send-only, matching the
// Adapter interface.
//
// SECURITY: the SMTP password is never placed in the message and is redacted
// from every returned error, so it cannot leak into logs.
type EmailAdapter struct {
	AdapterName string
	// Host and Port address the SMTP submission server (e.g. smtp.gmail.com:587).
	Host string
	Port int
	// Username / Password authenticate to the server (PLAIN over STARTTLS).
	Username string
	Password string
	// From is the envelope + header sender address.
	From string
	// send defaults to smtp.SendMail; overridable for tests.
	send emailSendFunc
}

// NewEmailAdapter constructs an EmailAdapter. name defaults to "email"; port
// defaults to 587 (the submission port) when non-positive.
func NewEmailAdapter(name, host string, port int, username, password, from string) *EmailAdapter {
	if name == "" {
		name = "email"
	}
	if port <= 0 {
		port = 587
	}
	return &EmailAdapter{
		AdapterName: name,
		Host:        host,
		Port:        port,
		Username:    username,
		Password:    password,
		From:        from,
		send:        smtp.SendMail,
	}
}

// Name returns the adapter name.
func (a *EmailAdapter) Name() string { return a.AdapterName }

// Deliver sends msg.Content as an email to the address in msg.PlatformID and
// returns the generated Message-ID as the platform message id.
func (a *EmailAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Host) == "" {
		return "", fmt.Errorf("host/channels: email %q has no SMTP host", a.AdapterName)
	}
	if strings.TrimSpace(a.From) == "" {
		return "", fmt.Errorf("host/channels: email %q has no From address", a.AdapterName)
	}
	to := ""
	if msg.PlatformID != nil {
		to = strings.TrimSpace(*msg.PlatformID)
	}
	if to == "" {
		return "", fmt.Errorf("host/channels: email %q message has no recipient (PlatformID)", a.AdapterName)
	}

	messageID := a.newMessageID()
	raw := a.compose(to, messageID, msg)

	addr := a.Host + ":" + strconv.Itoa(a.Port)
	var auth smtp.Auth
	if a.Username != "" || a.Password != "" {
		auth = smtp.PlainAuth("", a.Username, a.Password, a.Host)
	}

	send := a.send
	if send == nil {
		send = smtp.SendMail
	}
	if err := send(addr, auth, a.From, []string{to}, []byte(raw)); err != nil {
		return "", fmt.Errorf("host/channels: email %q SMTP send failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	return messageID, nil
}

// compose builds an RFC 5322 message (CRLF-terminated headers + body). A
// non-empty ThreadID is reflected into In-Reply-To / References so the message
// threads in the recipient's client.
func (a *EmailAdapter) compose(to, messageID string, msg contract.MessageOut) string {
	var b strings.Builder
	writeHeader := func(k, v string) {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	writeHeader("From", a.From)
	writeHeader("To", to)
	writeHeader("Subject", subjectFromContent(msg.Content))
	writeHeader("Date", time.Now().UTC().Format(time.RFC1123Z))
	writeHeader("Message-ID", messageID)
	if msg.ThreadID != nil {
		if ref := strings.TrimSpace(*msg.ThreadID); ref != "" {
			writeHeader("In-Reply-To", ref)
			writeHeader("References", ref)
		}
	}
	writeHeader("MIME-Version", "1.0")
	writeHeader("Content-Type", "text/plain; charset=utf-8")
	b.WriteString("\r\n")
	// Normalize the body to CRLF and dot-stuff-safe content is handled by net/smtp.
	b.WriteString(strings.ReplaceAll(msg.Content, "\n", "\r\n"))
	return b.String()
}

// newMessageID generates an RFC 5322 Message-ID of the form
// "<hex@domain>", using the From domain (or a stable fallback).
func (a *EmailAdapter) newMessageID() string {
	var buf [16]byte
	domain := "ironclaw.local"
	if at := strings.LastIndex(a.From, "@"); at >= 0 && at < len(a.From)-1 {
		domain = a.From[at+1:]
	}
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand should never fail; fall back to a time-based id so Deliver
		// still returns a usable, unique-enough id rather than erroring.
		return "<" + strconv.FormatInt(time.Now().UTC().UnixNano(), 16) + "@" + domain + ">"
	}
	return "<" + hex.EncodeToString(buf[:]) + "@" + domain + ">"
}

// subjectFromContent derives a one-line subject from the first line of content,
// truncated to a reasonable length.
func subjectFromContent(content string) string {
	line := content
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "(no subject)"
	}
	const max = 120
	if len(line) > max {
		return line[:max] + "…"
	}
	return line
}

// redact removes the SMTP password from a string so it can never reach a log or error.
func (a *EmailAdapter) redact(s string) string {
	if a.Password == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Password, "<redacted>")
}
