package modelproxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestVertexInjectorBearer checks the injector stamps the OAuth bearer for the
// regional and global Vertex hosts and no-ops everywhere else.
func TestVertexInjectorBearer(t *testing.T) {
	inj := VertexInjector(StaticTokenSource("ya29.token"))
	cases := []struct {
		host string
		want string
	}{
		{"us-central1-aiplatform.googleapis.com", "Bearer ya29.token"},
		{"europe-west4-aiplatform.googleapis.com", "Bearer ya29.token"},
		{"aiplatform.googleapis.com", "Bearer ya29.token"},
		{"api.anthropic.com", ""},
		{"generativelanguage.googleapis.com", ""},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest("POST", "http://"+tc.host+"/v1/x", nil)
		inj(tc.host, req)
		if got := req.Header.Get("Authorization"); got != tc.want {
			t.Errorf("host %q: Authorization = %q, want %q", tc.host, got, tc.want)
		}
	}
}

// TestVertexInjectorStripsSandboxAuth verifies the proxy strips a sandbox-forged
// bearer and replaces it with the host-held Vertex token end-to-end.
func TestVertexInjectorStripsSandboxAuth(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	const host = "us-central1-aiplatform.googleapis.com"
	p := New([]string{host},
		WithInjector(VertexInjector(StaticTokenSource("host-token"))),
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/v1/projects/p/locations/us-central1/publishers/google/models/m:streamGenerateContent", nil)
	req.Host = host
	req.Header.Set("Authorization", "Bearer sandbox-forged")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth != "Bearer host-token" {
		t.Fatalf("upstream Authorization = %q, want the host-held token (sandbox bearer stripped)", gotAuth)
	}
}

// TestVertexInjectorTokenErrorLeavesUnauthenticated checks a token-source failure
// does not panic or fail the proxy — the request just goes out unauthenticated and
// the upstream rejects it.
func TestVertexInjectorTokenErrorLeavesUnauthenticated(t *testing.T) {
	inj := VertexInjector(errTokenSource{})
	req, _ := http.NewRequest("POST", "http://us-central1-aiplatform.googleapis.com/v1/x", nil)
	req.Header.Set("Authorization", "Bearer sandbox-forged")
	// Director strips Authorization before injection; simulate that here.
	req.Header.Del("Authorization")
	inj("us-central1-aiplatform.googleapis.com", req)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty when the token source errors", got)
	}
}

// TestGcloudTokenSourceCaches verifies the gcloud source caches within the TTL and
// refreshes after it, without execing real gcloud (the runner is injected).
func TestGcloudTokenSourceCaches(t *testing.T) {
	var calls int
	now := time.Unix(0, 0)
	g := &GcloudTokenSource{
		TTL: time.Hour,
		now: func() time.Time { return now },
		run: func(ctx context.Context) (string, error) {
			calls++
			if calls == 1 {
				return "  token-1\n", nil // trims whitespace
			}
			return "token-2", nil
		},
	}

	tok, err := g.Token()
	if err != nil || tok != "token-1" {
		t.Fatalf("first Token() = %q, %v; want token-1", tok, err)
	}
	// Within TTL: cached, no second exec.
	now = now.Add(30 * time.Minute)
	if tok, _ := g.Token(); tok != "token-1" || calls != 1 {
		t.Fatalf("cached Token() = %q (calls=%d), want token-1 with no refresh", tok, calls)
	}
	// Past TTL: refresh.
	now = now.Add(time.Hour)
	if tok, _ := g.Token(); tok != "token-2" || calls != 2 {
		t.Fatalf("refreshed Token() = %q (calls=%d), want token-2 after refresh", tok, calls)
	}
}

// TestGcloudTokenSourceEmptyIsError checks an empty gcloud output is a clear error
// rather than a silent empty bearer.
func TestGcloudTokenSourceEmptyIsError(t *testing.T) {
	g := &GcloudTokenSource{run: func(ctx context.Context) (string, error) { return "  \n", nil }}
	if _, err := g.Token(); err == nil {
		t.Fatal("Token(): want error on empty gcloud output, got nil")
	}
}

type errTokenSource struct{}

func (errTokenSource) Token() (string, error) { return "", errors.New("no creds") }
