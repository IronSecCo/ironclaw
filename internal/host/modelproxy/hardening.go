package modelproxy

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file adds the egress-hardening layer: per-session/token rate caps,
// request/response audit records, and secret redaction on responses forwarded
// back into the sandbox. Each feature is opt-in via an Option and a no-op when
// unset, so the bare allowlist-proxy behaviour is unchanged.

// AuditRecord is one forwarded (or rejected) model-egress request. It carries no
// payload bytes — only metadata — so audit logs never become a secret sink.
type AuditRecord struct {
	Time          time.Time     `json:"time"`
	Session       string        `json:"session,omitempty"`
	Method        string        `json:"method"`
	Host          string        `json:"host"`
	Path          string        `json:"path"`
	Status        int           `json:"status"`
	Allowed       bool          `json:"allowed"`
	RateLimited   bool          `json:"rateLimited,omitempty"`
	RequestBytes  int64         `json:"requestBytes"`
	ResponseBytes int64         `json:"responseBytes"`
	Duration      time.Duration `json:"durationNanos"`
}

// AuditSink receives one record per request. Implementations must not block the
// request path for long (write to a buffered logger/file).
type AuditSink func(AuditRecord)

// WithRateCap caps model egress at rps requests/second with a burst of burst,
// keyed by session/token identity (see WithIdentity; default is one bucket for
// the whole proxy, which equals a per-session cap since a proxy serves one
// sandbox). rps <= 0 disables it.
func WithRateCap(rps float64, burst int) Option {
	return func(p *Proxy) {
		if rps > 0 {
			p.limiter = newKeyedLimiter(rps, burst)
		}
	}
}

// WithAudit installs an audit sink invoked once per request (allowed, denied, or
// rate-limited).
func WithAudit(sink AuditSink) Option { return func(p *Proxy) { p.audit = sink } }

// WithIdentity sets how a request maps to a rate-limit / audit key (e.g. a
// session header). Default keys everything to "" (a single per-proxy bucket).
func WithIdentity(keyFn func(*http.Request) string) Option {
	return func(p *Proxy) {
		if keyFn != nil {
			p.identify = keyFn
		}
	}
}

// WithRedactedSecrets registers exact secret strings (e.g. the host API key) to
// scrub from forwarded response bodies and headers, so an upstream that echoes a
// credential cannot leak it into the sandbox. Empty strings are ignored.
func WithRedactedSecrets(secrets ...string) Option {
	return func(p *Proxy) {
		for _, s := range secrets {
			if s != "" {
				p.secrets = append(p.secrets, s)
			}
		}
	}
}

func (p *Proxy) sessionKey(r *http.Request) string {
	if p.identify != nil {
		return p.identify(r)
	}
	return ""
}

func (p *Proxy) emitAudit(rec AuditRecord) {
	if p.audit != nil {
		p.audit(rec)
	}
}

// --- per-key token-bucket limiter ---

// keyedLimiter holds one token bucket per identity key. The key set is bounded;
// if it grows past maxKeys the map is reset (a crude but safe backstop — in
// normal per-session operation there is exactly one key).
type keyedLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rps     float64
	burst   int
	now     func() time.Time // injectable for tests
}

const maxLimiterKeys = 4096

func newKeyedLimiter(rps float64, burst int) *keyedLimiter {
	if burst < 1 {
		burst = 1
	}
	return &keyedLimiter{
		buckets: make(map[string]*bucket),
		rps:     rps,
		burst:   burst,
		now:     time.Now,
	}
}

func (kl *keyedLimiter) allow(key string) bool {
	kl.mu.Lock()
	defer kl.mu.Unlock()
	if len(kl.buckets) > maxLimiterKeys {
		kl.buckets = make(map[string]*bucket)
	}
	b := kl.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(kl.burst), last: kl.now()}
		kl.buckets[key] = b
	}
	now := kl.now()
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * kl.rps
		if b.tokens > float64(kl.burst) {
			b.tokens = float64(kl.burst)
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

type bucket struct {
	tokens float64
	last   time.Time
}

// --- response redaction ---

// maxRedactBody caps how much of a non-streaming response body is buffered for
// secret scrubbing. Larger bodies are passed through unbuffered (and unredacted)
// to avoid unbounded memory use; streaming (text/event-stream) is never buffered
// so SSE keeps flowing.
const maxRedactBody = 1 << 20 // 1 MiB

func (p *Proxy) modifyResponse(resp *http.Response) error {
	// Never let a credential echo back to the sandbox via response headers.
	for _, h := range []string{"X-Api-Key", "Authorization", "Proxy-Authorization", "Set-Cookie"} {
		resp.Header.Del(h)
	}
	if len(p.secrets) == 0 || resp.Body == nil {
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
		// Too large to scrub safely in memory: reattach what we read and stream
		// the rest through untouched.
		resp.Body = readCloser{Reader: io.MultiReader(bytes.NewReader(peek), resp.Body), Closer: resp.Body}
		return nil
	}
	_ = resp.Body.Close()

	redacted := redactBytes(peek, p.secrets)
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

const redactedMarker = "[REDACTED]"

type readCloser struct {
	io.Reader
	io.Closer
}

// --- response capture for audit ---

// countingRW captures the status code and byte count written to the client while
// preserving Flush so the reverse proxy can stream responses.
type countingRW struct {
	http.ResponseWriter
	status int
	n      int64
}

func (c *countingRW) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *countingRW) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	n, err := c.ResponseWriter.Write(b)
	c.n += int64(n)
	return n, err
}

func (c *countingRW) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
