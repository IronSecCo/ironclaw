package egress

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func resp(body, contentType string) *http.Response {
	r := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

func readBody(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestRedactScrubsBody(t *testing.T) {
	r := resp("token=s3cr3t-key-1234 and more", "application/json")
	if err := NewRedactor("s3cr3t-key-1234").Redact(r); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	got := readBody(t, r)
	if strings.Contains(got, "s3cr3t-key-1234") {
		t.Fatalf("secret not scrubbed: %q", got)
	}
	if !strings.Contains(got, redactedMarker) {
		t.Fatalf("masking marker missing: %q", got)
	}
	// Content-Length (header and field) must match the redacted body length.
	if r.ContentLength != int64(len(got)) {
		t.Fatalf("ContentLength %d != body len %d", r.ContentLength, len(got))
	}
	if h := r.Header.Get("Content-Length"); h != strconv.Itoa(len(got)) {
		t.Fatalf("Content-Length header %q != body len %d", h, len(got))
	}
}

func TestRedactStripsCredentialHeaders(t *testing.T) {
	r := resp("ok", "text/plain")
	r.Header.Set("Authorization", "Bearer leak")
	r.Header.Set("X-Api-Key", "leak")
	r.Header.Set("Set-Cookie", "session=leak")
	r.Header.Set("Proxy-Authorization", "leak")
	r.Header.Set("X-Keep", "fine")
	if err := NewRedactor("nothing").Redact(r); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	for _, h := range []string{"Authorization", "X-Api-Key", "Set-Cookie", "Proxy-Authorization"} {
		if v := r.Header.Get(h); v != "" {
			t.Errorf("header %q must be stripped, got %q", h, v)
		}
	}
	if r.Header.Get("X-Keep") != "fine" {
		t.Error("non-credential header must pass through")
	}
}

func TestRedactNoSecretsStillStripsHeaders(t *testing.T) {
	r := resp("body with maybe-secret", "text/plain")
	r.Header.Set("Authorization", "Bearer leak")
	if err := NewRedactor().Redact(r); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if r.Header.Get("Authorization") != "" {
		t.Error("Authorization must be stripped even with no secrets configured")
	}
	if got := readBody(t, r); got != "body with maybe-secret" {
		t.Fatalf("body must be untouched with no secrets, got %q", got)
	}
}

// TestRedactPreservesStreaming documents the modelproxy tradeoff: text/event-stream
// is never buffered, so a secret in a streamed body passes through unredacted (the
// stream keeps flowing). This is intentional and mirrored from T-107.
func TestRedactPreservesStreaming(t *testing.T) {
	r := resp("data: s3cr3t-key-1234\n\n", "text/event-stream")
	if err := NewRedactor("s3cr3t-key-1234").Redact(r); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if got := readBody(t, r); !strings.Contains(got, "s3cr3t-key-1234") {
		t.Fatalf("streaming body must pass through unbuffered, got %q", got)
	}
}

// TestRedactLargeBodyPassthrough asserts an over-large body is not buffered for
// redaction (bounded memory): the secret passes through and the whole body remains
// readable.
func TestRedactLargeBodyPassthrough(t *testing.T) {
	body := "s3cr3t-key-1234" + strings.Repeat("a", maxRedactBody)
	r := resp(body, "application/octet-stream")
	if err := NewRedactor("s3cr3t-key-1234").Redact(r); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	got := readBody(t, r)
	if len(got) != len(body) {
		t.Fatalf("over-large body must stream through whole: got %d, want %d", len(got), len(body))
	}
	if !strings.Contains(got, "s3cr3t-key-1234") {
		t.Fatal("over-large body is passed through unredacted by design (bounded memory)")
	}
}

func TestRedactMultipleSecrets(t *testing.T) {
	r := resp("a=AAA b=BBB c=CCC", "text/plain")
	if err := NewRedactor("AAA", "BBB", "CCC").Redact(r); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	got := readBody(t, r)
	for _, s := range []string{"AAA", "BBB", "CCC"} {
		if strings.Contains(got, s) {
			t.Errorf("secret %q not scrubbed: %q", s, got)
		}
	}
}

func TestRedactNilSafe(t *testing.T) {
	if err := NewRedactor("x").Redact(nil); err != nil {
		t.Fatalf("nil response must be a no-op, got %v", err)
	}
	r := &http.Response{Header: http.Header{}} // nil body
	r.Header.Set("Authorization", "leak")
	if err := NewRedactor("x").Redact(r); err != nil {
		t.Fatalf("nil body: %v", err)
	}
	if r.Header.Get("Authorization") != "" {
		t.Error("headers must be stripped even with a nil body")
	}
}
