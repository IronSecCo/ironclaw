package egress

// Defense-in-depth for the vault path: even though the egress broker never holds a
// credential (the injector does — B4-E), an upstream API could reflect an injected
// credential back in its response. This backstop scrubs configured secret values
// from responses on the broker->sandbox hop so such a credential can never echo into
// the sandbox. It is a faithful mirror of
// the model-proxy redaction (internal/host/modelproxy/hardening.go): same
// header stripping, same maxRedactBody streaming tradeoff, same masking marker.
//
// Secrets are registered HOST-SIDE; the sandbox never supplies them. This file is
// the redaction unit; wiring Redact as the broker's ModifyResponse is the
// follow-on integration step.

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// maxRedactBody caps how much of a non-streaming response body is buffered for
// scrubbing. Larger bodies stream through untouched to bound memory; streaming
// responses (text/event-stream) are never buffered. Mirrors modelproxy.
const maxRedactBody = 1 << 20 // 1 MiB

// redactedMarker replaces every occurrence of a configured secret.
const redactedMarker = "[REDACTED]"

// Redactor scrubs configured secret values from responses on the broker->sandbox
// path. A Redactor with no secrets still strips credential-bearing response headers
// (it never forwards them) but does not buffer bodies.
type Redactor struct {
	secrets []string
}

// NewRedactor registers exact secret strings to scrub. Blank entries are dropped.
func NewRedactor(secrets ...string) *Redactor {
	kept := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			kept = append(kept, s)
		}
	}
	return &Redactor{secrets: kept}
}

// Redact rewrites resp in place for the broker->sandbox hop. It unconditionally
// strips credential-bearing headers, then scrubs registered secrets from the body.
// Streaming (text/event-stream) and over-large (> maxRedactBody) bodies are passed
// through WITHOUT buffering — the latter therefore unredacted, the bounded-memory
// tradeoff inherited from modelproxy. Safe to wire as
// httputil.ReverseProxy.ModifyResponse.
func (rd *Redactor) Redact(resp *http.Response) error {
	if resp == nil {
		return nil
	}
	// Never let a credential echo back to the sandbox via response headers.
	for _, h := range []string{"Authorization", "Proxy-Authorization", "X-Api-Key", "Set-Cookie"} {
		resp.Header.Del(h)
	}
	if len(rd.secrets) == 0 || resp.Body == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return nil // preserve streaming; do not buffer
	}

	peek, err := io.ReadAll(io.LimitReader(resp.Body, maxRedactBody+1))
	if err != nil {
		_ = resp.Body.Close()
		return err
	}
	if len(peek) > maxRedactBody {
		// Too large to scrub safely in memory: reattach what we read and stream the
		// rest through untouched rather than buffer unboundedly.
		resp.Body = redactReadCloser{Reader: io.MultiReader(bytes.NewReader(peek), resp.Body), Closer: resp.Body}
		return nil
	}
	_ = resp.Body.Close()

	redacted := redactBytes(peek, rd.secrets)
	resp.Body = io.NopCloser(bytes.NewReader(redacted))
	resp.ContentLength = int64(len(redacted))
	resp.Header.Set("Content-Length", strconv.Itoa(len(redacted)))
	return nil
}

// redactBytes replaces every occurrence of each secret with the masking marker.
func redactBytes(body []byte, secrets []string) []byte {
	out := body
	for _, s := range secrets {
		out = bytes.ReplaceAll(out, []byte(s), []byte(redactedMarker))
	}
	return out
}

type redactReadCloser struct {
	io.Reader
	io.Closer
}
