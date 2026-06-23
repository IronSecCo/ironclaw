//go:build wsg_verify

package wsg

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
)

// TestG5_Webhook_RoundTrip stands up a real HTTP receiver and delivers through the
// REAL WebhookAdapter, asserting the JSON payload shape and that the adapter
// surfaces the platform message id returned by the receiver.
func TestG5_Webhook_RoundTrip(t *testing.T) {
	type captured struct {
		contentType string
		body        contract.MessageOut
	}
	got := make(chan captured, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg contract.MessageOut
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		_ = json.Unmarshal(raw, &msg)
		got <- captured{contentType: r.Header.Get("Content-Type"), body: msg}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"wh-platform-77"}`)
	}))
	defer srv.Close()

	adapter := channels.NewWebhookAdapter("webhook", srv.URL)
	channel := "webhook"
	msg := contract.MessageOut{ID: "out-1", ChannelType: &channel, Content: "deploy finished ✅"}

	id, err := adapter.Deliver(context.Background(), msg)
	if err != nil {
		t.Fatalf("webhook Deliver: %v", err)
	}
	if id != "wh-platform-77" {
		t.Fatalf("platform id = %q, want wh-platform-77", id)
	}

	select {
	case c := <-got:
		if !strings.HasPrefix(c.contentType, "application/json") {
			t.Fatalf("content-type = %q, want application/json", c.contentType)
		}
		if c.body.ID != "out-1" || c.body.Content != "deploy finished ✅" {
			t.Fatalf("receiver got unexpected payload: %+v", c.body)
		}
		if c.body.ChannelType == nil || *c.body.ChannelType != "webhook" {
			t.Fatalf("receiver payload missing channel type: %+v", c.body)
		}
		t.Logf("G5 webhook: delivered out-1 → receiver, platform id %q", id)
	case <-time.After(5 * time.Second):
		t.Fatal("webhook receiver did not get the POST")
	}
}

// TestG5_Email_SMTP_RoundTrip delivers through the REAL EmailAdapter over the real
// net/smtp client into an in-process SMTP sink (a minimal listener that speaks just
// enough SMTP, no STARTTLS/AUTH). It asserts the envelope, recipient, subject, and
// body all round-trip and that the adapter returns the generated Message-ID.
func TestG5_Email_SMTP_RoundTrip(t *testing.T) {
	sink := startSMTPSink(t)
	defer sink.Close()

	host, portStr, err := net.SplitHostPort(sink.Addr())
	if err != nil {
		t.Fatalf("split sink addr: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	// No username/password → net/smtp.SendMail sends with nil auth, which the sink
	// accepts. This exercises the real SMTP submission path.
	adapter := channels.NewEmailAdapter("email", host, port, "", "", "alerts@ironclaw.test")
	to := "oncall@ironclaw.test"
	channel := "email"
	msg := contract.MessageOut{
		ID:          "out-mail-1",
		ChannelType: &channel,
		PlatformID:  &to,
		Content:     "Disk usage at 92% on prod-1\nInvestigate before the nightly backup.",
	}

	messageID, err := adapter.Deliver(context.Background(), msg)
	if err != nil {
		t.Fatalf("email Deliver: %v", err)
	}
	if messageID == "" {
		t.Fatal("email Deliver returned empty Message-ID")
	}

	rcpt, from, data := sink.wait(t, 5*time.Second)
	if from != "alerts@ironclaw.test" {
		t.Fatalf("MAIL FROM = %q, want alerts@ironclaw.test", from)
	}
	if len(rcpt) != 1 || rcpt[0] != to {
		t.Fatalf("RCPT TO = %v, want [%s]", rcpt, to)
	}
	for _, want := range []string{
		"To: " + to,
		"From: alerts@ironclaw.test",
		"Subject: Disk usage at 92% on prod-1",
		"Message-ID: " + messageID,
		"Disk usage at 92% on prod-1",
	} {
		if !strings.Contains(data, want) {
			t.Fatalf("delivered message missing %q\n---\n%s\n---", want, data)
		}
	}
	t.Logf("G5 email: delivered out-mail-1 → SMTP sink, Message-ID %s", messageID)
}

// --- minimal in-process SMTP sink ----------------------------------------

// smtpSink is a tiny SMTP server that accepts a single message without STARTTLS or
// auth and captures the envelope + DATA. It speaks just the verbs net/smtp.SendMail
// uses (EHLO/HELO, MAIL, RCPT, DATA, QUIT), which is enough for a real round-trip.
type smtpSink struct {
	ln       net.Listener
	mu       sync.Mutex
	rcpt     []string
	from     string
	data     string
	captured chan struct{}
}

func startSMTPSink(t *testing.T) *smtpSink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SMTP sink: %v", err)
	}
	s := &smtpSink{ln: ln, captured: make(chan struct{}, 1)}
	go s.serve()
	return s
}

func (s *smtpSink) Addr() string { return s.ln.Addr().String() }
func (s *smtpSink) Close() error { return s.ln.Close() }

func (s *smtpSink) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	wr := func(format string, a ...interface{}) { fmt.Fprintf(conn, format+"\r\n", a...) }

	wr("220 ironclaw-wsg-sink ready")
	inData := false
	var body strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if inData {
			if strings.TrimRight(line, "\r\n") == "." {
				inData = false
				s.mu.Lock()
				s.data = body.String()
				s.mu.Unlock()
				wr("250 OK queued")
				continue
			}
			body.WriteString(line)
			continue
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			// No STARTTLS / AUTH advertised → SendMail proceeds unauthenticated.
			wr("250-ironclaw-wsg-sink")
			wr("250 OK")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.mu.Lock()
			s.from = extractAddr(line[len("MAIL FROM:"):])
			s.mu.Unlock()
			wr("250 OK")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			s.mu.Lock()
			s.rcpt = append(s.rcpt, extractAddr(line[len("RCPT TO:"):]))
			s.mu.Unlock()
			wr("250 OK")
		case cmd == "DATA":
			inData = true
			wr("354 End data with <CR><LF>.<CR><LF>")
		case cmd == "QUIT":
			wr("221 Bye")
			select {
			case s.captured <- struct{}{}:
			default:
			}
			return
		case cmd == "RSET", cmd == "NOOP":
			wr("250 OK")
		default:
			wr("250 OK")
		}
	}
}

// wait blocks until the sink has captured a full message (QUIT seen) and returns
// the recipients, envelope sender, and raw DATA.
func (s *smtpSink) wait(t *testing.T, d time.Duration) (rcpt []string, from, data string) {
	t.Helper()
	select {
	case <-s.captured:
	case <-time.After(d):
		t.Fatal("SMTP sink did not capture a message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.rcpt...), s.from, s.data
}

// extractAddr pulls the bare address out of a "<addr>" SMTP argument.
func extractAddr(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	if i := strings.IndexByte(s, '>'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
