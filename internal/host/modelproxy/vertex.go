package modelproxy

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// vertexHostSuffix is the Google Cloud Vertex AI host suffix the injector matches.
// Vertex is served per-region as {location}-aiplatform.googleapis.com and globally
// as aiplatform.googleapis.com — both end in this suffix.
const vertexHostSuffix = "aiplatform.googleapis.com"

// TokenSource yields the current OAuth2 bearer token for Vertex AI. Implementations
// are responsible for refresh and caching; Token is called once per forwarded
// request, so a static source returns a fixed value and an auto-refreshing source
// returns a cached, still-valid token. The token lives only on the host — it is
// injected into the upstream request and never enters the sandbox.
type TokenSource interface {
	Token() (string, error)
}

// VertexInjector returns an Injector that authenticates requests to Google Cloud
// Vertex AI ({location}-aiplatform.googleapis.com) with a host-held OAuth2 bearer
// token obtained from ts. Unlike the static-API-key providers, the credential is a
// short-lived bearer that ts refreshes host-side; the sandbox never holds it. The
// injector self-guards on the Vertex host suffix so it no-ops for any other provider
// — safe to compose through MultiInjector. A token-source error leaves the request
// unauthenticated (the upstream rejects with 401) rather than failing closed inside
// the proxy; the error string is never logged here to avoid leaking token material.
func VertexInjector(ts TokenSource) Injector {
	return func(upstreamHost string, req *http.Request) {
		if ts == nil {
			return
		}
		if !strings.HasSuffix(strings.ToLower(upstreamHost), vertexHostSuffix) {
			return
		}
		tok, err := ts.Token()
		if err != nil || tok == "" {
			return
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
}

// StaticTokenSource is a TokenSource that always returns the same token. Use it when
// the operator supplies a Vertex access token via the environment
// (GOOGLE_VERTEX_ACCESS_TOKEN) and refreshes it out of band (e.g. a sidecar running
// `gcloud auth print-access-token` on a timer). The token expires on Google's
// schedule (~1h); for unattended auto-refresh prefer GcloudTokenSource.
type StaticTokenSource string

// Token returns the fixed token.
func (s StaticTokenSource) Token() (string, error) { return string(s), nil }

// GcloudTokenSource is a TokenSource that obtains a Vertex AI access token from the
// host's Application Default Credentials by execing `gcloud auth print-access-token`
// and caching it until shortly before it would expire. It adds no Go dependency and
// covers the standard local ADC paths (gcloud user login, a service-account key via
// GOOGLE_APPLICATION_CREDENTIALS, or the GCE metadata server). gcloud runs only on
// the host, never in the sandbox.
type GcloudTokenSource struct {
	// TTL bounds how long a fetched token is reused before a refresh. gcloud tokens
	// last ~1h; a conservative default leaves headroom. Zero uses gcloudDefaultTTL.
	TTL time.Duration

	mu      sync.Mutex
	token   string
	fetched time.Time
	// now and run are overridable in tests; nil uses the real clock / exec.
	now func() time.Time
	run func(ctx context.Context) (string, error)
}

const gcloudDefaultTTL = 45 * time.Minute

// Token returns a cached gcloud access token, refreshing it when the cache is empty
// or older than TTL. Concurrent callers serialize on the mutex; a refresh failure
// surfaces to the caller (VertexInjector treats it as "leave unauthenticated").
func (g *GcloudTokenSource) Token() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now
	if g.now != nil {
		now = g.now
	}
	ttl := g.TTL
	if ttl <= 0 {
		ttl = gcloudDefaultTTL
	}
	if g.token != "" && now().Sub(g.fetched) < ttl {
		return g.token, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	run := g.run
	if run == nil {
		run = runGcloudPrintAccessToken
	}
	tok, err := run(ctx)
	if err != nil {
		return "", err
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", fmt.Errorf("host/modelproxy: gcloud returned an empty access token")
	}
	g.token = tok
	g.fetched = now()
	return tok, nil
}

// runGcloudPrintAccessToken execs `gcloud auth print-access-token`. It uses no shell
// (exec.CommandContext, fixed args) so there is no injection surface, and bounds the
// call with ctx. The token is returned to the caller and never logged.
func runGcloudPrintAccessToken(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", fmt.Errorf("host/modelproxy: gcloud auth print-access-token: %w", err)
	}
	return string(out), nil
}
