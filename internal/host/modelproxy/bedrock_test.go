package modelproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// awsExampleCreds is the credential from the AWS SigV4 documentation / official
// signature test suite. It is a published example key, not a real secret.
var awsExampleCreds = Credentials{
	AccessKeyID:     "AKIDEXAMPLE",
	SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
}

// TestSignV4GetVanilla verifies the SigV4 core against the official AWS
// "get-vanilla" test-suite vector: a GET to example.amazonaws.com in
// us-east-1/service on 2015-08-30T12:36:00Z must produce the documented signature.
// This pins the canonical-request, string-to-sign, and signing-key derivation to a
// third-party oracle so the hand-rolled signer is provably correct without a live
// AWS call.
func TestSignV4GetVanilla(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.amazonaws.com/", nil)
	req.Host = "example.amazonaws.com"
	when := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)

	signV4(req, nil, awsExampleCreds, "us-east-1", "service", when)

	const want = "AWS4-HMAC-SHA256 " +
		"Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
		"SignedHeaders=host;x-amz-date, " +
		"Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization =\n  %q\nwant\n  %q", got, want)
	}
	if got := req.Header.Get("X-Amz-Date"); got != "20150830T123600Z" {
		t.Fatalf("X-Amz-Date = %q, want 20150830T123600Z", got)
	}
}

func TestBedrockRegion(t *testing.T) {
	cases := map[string]struct {
		region string
		ok     bool
	}{
		"bedrock-runtime.us-east-1.amazonaws.com":      {"us-east-1", true},
		"bedrock-runtime.eu-west-1.amazonaws.com:443":  {"eu-west-1", true},
		"BEDROCK-RUNTIME.AP-SOUTHEAST-2.AMAZONAWS.COM": {"ap-southeast-2", true},
		"generativelanguage.googleapis.com":            {"", false},
		"bedrock-runtime.amazonaws.com":                {"", false}, // no region segment
		"bedrock-runtime.us.east.1.amazonaws.com":      {"", false}, // dotted region rejected
		"api.anthropic.com":                            {"", false},
	}
	for host, want := range cases {
		got, ok := bedrockRegion(host)
		if ok != want.ok || got != want.region {
			t.Errorf("bedrockRegion(%q) = (%q,%v), want (%q,%v)", host, got, ok, want.region, want.ok)
		}
	}
}

// TestBedrockInjectorSigns checks the injector signs a Bedrock request: it derives
// the region from the host, sets the SigV4 headers, and produces an Authorization
// whose credential scope carries that region and the bedrock service.
func TestBedrockInjectorSigns(t *testing.T) {
	inj := BedrockInjector(StaticCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret"})
	req := httptest.NewRequest(http.MethodPost,
		"http://bedrock-runtime.us-west-2.amazonaws.com/model/anthropic.claude-3-5-sonnet-20241022-v2:0/invoke",
		strings.NewReader(`{"anthropic_version":"bedrock-2023-05-31"}`))
	req.Host = "bedrock-runtime.us-west-2.amazonaws.com"

	inj("bedrock-runtime.us-west-2.amazonaws.com", req)

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("Authorization = %q, want a SigV4 header", auth)
	}
	if !strings.Contains(auth, "/us-west-2/bedrock/aws4_request") {
		t.Fatalf("Authorization scope = %q, want us-west-2/bedrock", auth)
	}
	if !strings.Contains(auth, "Credential=AKID/") {
		t.Fatalf("Authorization = %q, want the access key id", auth)
	}
	if req.Header.Get("X-Amz-Date") == "" || req.Header.Get("X-Amz-Content-Sha256") == "" {
		t.Fatal("missing X-Amz-Date / X-Amz-Content-Sha256 after signing")
	}
	// The signed content hash covers the actual body, and the body is still forwardable.
	if got := req.Header.Get("X-Amz-Content-Sha256"); got == hexSHA256(nil) {
		t.Fatal("content hash is the empty-body hash; body was not read for signing")
	}
	b, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(b), "bedrock-2023-05-31") {
		t.Fatalf("body after signing = %q, want it preserved for forwarding", b)
	}
}

// TestBedrockInjectorSessionToken checks temporary credentials add the security
// token header (and it is part of the signed set).
func TestBedrockInjectorSessionToken(t *testing.T) {
	inj := BedrockInjector(StaticCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret", SessionToken: "SESSIONTOKEN=="})
	req := httptest.NewRequest(http.MethodPost, "http://bedrock-runtime.us-east-1.amazonaws.com/model/m/invoke", strings.NewReader("{}"))
	req.Host = "bedrock-runtime.us-east-1.amazonaws.com"

	inj("bedrock-runtime.us-east-1.amazonaws.com", req)

	if req.Header.Get("X-Amz-Security-Token") != "SESSIONTOKEN==" {
		t.Fatalf("X-Amz-Security-Token = %q, want the session token", req.Header.Get("X-Amz-Security-Token"))
	}
	if !strings.Contains(req.Header.Get("Authorization"), "x-amz-security-token") {
		t.Fatalf("SignedHeaders omit the security token: %q", req.Header.Get("Authorization"))
	}
}

// TestBedrockInjectorNoOpOffHost confirms the injector self-guards: a non-Bedrock
// host is left untouched, so it is safe to compose through MultiInjector.
func TestBedrockInjectorNoOpOffHost(t *testing.T) {
	inj := BedrockInjector(StaticCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret"})
	req := httptest.NewRequest(http.MethodPost, "http://api.anthropic.com/v1/messages", strings.NewReader("{}"))
	req.Host = "api.anthropic.com"

	inj("api.anthropic.com", req)

	if req.Header.Get("Authorization") != "" || req.Header.Get("X-Amz-Date") != "" {
		t.Fatal("injector signed a non-Bedrock host")
	}
}

// TestBedrockInjectorStripsSandboxHeaders confirms a sandbox-supplied X-Amz-* header
// or Authorization cannot smuggle into the signed set — the injector strips them and
// re-signs cleanly.
func TestBedrockInjectorStripsSandboxHeaders(t *testing.T) {
	inj := BedrockInjector(StaticCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret"})
	req := httptest.NewRequest(http.MethodPost, "http://bedrock-runtime.us-east-1.amazonaws.com/model/m/invoke", strings.NewReader("{}"))
	req.Host = "bedrock-runtime.us-east-1.amazonaws.com"
	req.Header.Set("Authorization", "sandbox-forged")
	req.Header.Set("X-Amz-Injected", "evil")

	inj("bedrock-runtime.us-east-1.amazonaws.com", req)

	if req.Header.Get("X-Amz-Injected") != "" {
		t.Fatal("sandbox-supplied X-Amz-* header survived signing")
	}
	auth := req.Header.Get("Authorization")
	if strings.Contains(auth, "sandbox-forged") || !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("Authorization = %q, want a fresh SigV4 header", auth)
	}
	if strings.Contains(auth, "x-amz-injected") {
		t.Fatalf("forged header made it into SignedHeaders: %q", auth)
	}
}

// TestBedrockInjectorNilAndBadCreds confirms a missing credential leaves the request
// unsigned (upstream rejects) rather than failing closed inside the proxy.
func TestBedrockInjectorNilAndBadCreds(t *testing.T) {
	for name, inj := range map[string]Injector{
		"nil source":  BedrockInjector(nil),
		"empty creds": BedrockInjector(StaticCredentials{}),
	} {
		req := httptest.NewRequest(http.MethodPost, "http://bedrock-runtime.us-east-1.amazonaws.com/model/m/invoke", strings.NewReader("{}"))
		req.Host = "bedrock-runtime.us-east-1.amazonaws.com"
		inj("bedrock-runtime.us-east-1.amazonaws.com", req)
		if req.Header.Get("Authorization") != "" {
			t.Fatalf("%s: signed despite no usable credential", name)
		}
	}
}

// TestEscapePathBedrockModelID checks the model id's ':' canonicalizes to %3A, as
// AWS does server-side, so the signature matches the received (literal-':') request.
func TestEscapePathBedrockModelID(t *testing.T) {
	got := escapePath("/model/anthropic.claude-3-5-sonnet-20241022-v2:0/invoke")
	want := "/model/anthropic.claude-3-5-sonnet-20241022-v2%3A0/invoke"
	if got != want {
		t.Fatalf("escapePath = %q, want %q", got, want)
	}
}

// TestBedrockInjectorThroughProxy exercises the real Director -> inject -> forward
// path: a POST with a body goes through Proxy.Handler() with the Bedrock injector
// wired, and the upstream must receive a SigV4 Authorization, the matching content
// hash, and the body intact (proving the injector re-buffers the body it read to
// sign). No live AWS call — a fake upstream stands in.
func TestBedrockInjectorThroughProxy(t *testing.T) {
	const bHost = "bedrock-runtime.us-east-1.amazonaws.com"
	body := `{"anthropic_version":"bedrock-2023-05-31","messages":[]}`

	var gotAuth, gotHash string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHash = r.Header.Get("X-Amz-Content-Sha256")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := New([]string{bHost},
		WithInjector(BedrockInjector(StaticCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret"})),
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/model/m/invoke", strings.NewReader(body))
	req.Host = bHost
	req.Header.Set("Authorization", "sandbox-forged") // must be stripped, not forwarded
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 ") || strings.Contains(gotAuth, "sandbox-forged") {
		t.Fatalf("upstream Authorization = %q, want a fresh SigV4 header", gotAuth)
	}
	if gotHash != hexSHA256([]byte(body)) {
		t.Fatalf("upstream content hash = %q, want the hash of the forwarded body", gotHash)
	}
	if string(gotBody) != body {
		t.Fatalf("upstream body = %q, want it forwarded intact after signing", gotBody)
	}
}
