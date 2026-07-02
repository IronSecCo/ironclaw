package modelproxy

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// bedrockHostPrefix / bedrockHostSuffix bound the AWS Bedrock Runtime hosts the
// injector matches. Bedrock is served per-region as
// bedrock-runtime.{region}.amazonaws.com; the region is parsed out of the host and
// used as the SigV4 credential-scope region.
const (
	bedrockHostPrefix = "bedrock-runtime."
	bedrockHostSuffix = ".amazonaws.com"
	// bedrockService is the SigV4 service name for the Bedrock Runtime.
	bedrockService = "bedrock"
)

// Credentials is a host-held AWS credential used to SigV4-sign Bedrock requests.
// SessionToken is set only for temporary credentials (STS / IAM role); it becomes
// the X-Amz-Security-Token header. The credential lives only on the host — it is
// used to sign the outbound request and never enters the sandbox.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// CredentialSource yields the current AWS credential for Bedrock signing.
// Implementations own any refresh/caching (temporary credentials rotate); the
// injector calls it once per forwarded request. The credential never leaves the host.
type CredentialSource interface {
	Credentials() (Credentials, error)
}

// StaticCredentials is a CredentialSource that always returns the same credential.
// Use it when the operator supplies AWS keys via the environment
// (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / optional AWS_SESSION_TOKEN). For
// temporary credentials the operator refreshes them out of band (e.g. a sidecar
// that re-execs the control-plane, or a rotating secret mount).
type StaticCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// Credentials returns the fixed credential.
func (s StaticCredentials) Credentials() (Credentials, error) {
	return Credentials{AccessKeyID: s.AccessKeyID, SecretAccessKey: s.SecretAccessKey, SessionToken: s.SessionToken}, nil
}

// BedrockInjector returns an Injector that authenticates requests to the AWS
// Bedrock Runtime (bedrock-runtime.{region}.amazonaws.com) with AWS Signature
// Version 4, using a host-held credential from cs. The region is parsed from the
// upstream host so a single injector serves whatever region is allowlisted. Unlike
// the static-header providers, SigV4 signs the request body and selected headers, so
// the injector reads and re-buffers the body and stamps X-Amz-Date,
// X-Amz-Content-Sha256, the optional X-Amz-Security-Token, and Authorization.
//
// It self-guards on the Bedrock host so it no-ops for any other provider — safe to
// compose through MultiInjector. It also strips any sandbox-supplied X-Amz-* headers
// before signing so the sandbox cannot inject headers into the signed set. A
// credential-source error or missing key leaves the request unsigned (the upstream
// rejects with 403) rather than failing closed inside the proxy; no credential
// material is ever logged.
func BedrockInjector(cs CredentialSource) Injector {
	return func(upstreamHost string, req *http.Request) {
		if cs == nil {
			return
		}
		region, ok := bedrockRegion(upstreamHost)
		if !ok {
			return
		}
		creds, err := cs.Credentials()
		if err != nil || creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
			return
		}

		// Strip any sandbox-supplied AWS auth so only our signature is present and
		// the sandbox cannot smuggle headers into the signed set.
		for h := range req.Header {
			if strings.HasPrefix(http.CanonicalHeaderKey(h), "X-Amz-") {
				req.Header.Del(h)
			}
		}
		req.Header.Del("Authorization")

		// SigV4 signs the payload, so the whole body must be read and re-buffered
		// (the ReverseProxy still needs to forward it). Requests are bounded model
		// calls, so buffering on the host is acceptable.
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
			_ = req.Body.Close()
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
		}

		// Bedrock (a payload-bearing service) signs the content hash. Set it before
		// signing so it joins the signed X-Amz-* header set.
		req.Header.Set("X-Amz-Content-Sha256", hexSHA256(body))
		signV4(req, body, creds, region, bedrockService, time.Now())
	}
}

// bedrockRegion extracts the AWS region from a Bedrock Runtime host
// (bedrock-runtime.{region}.amazonaws.com), reporting whether host is a Bedrock
// host at all. The host may carry a :port, which is stripped first.
func bedrockRegion(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if !strings.HasPrefix(host, bedrockHostPrefix) || !strings.HasSuffix(host, bedrockHostSuffix) {
		return "", false
	}
	// Reject a host with no room for a region segment between prefix and suffix
	// (e.g. bedrock-runtime.amazonaws.com), where the two would otherwise overlap.
	if len(host) <= len(bedrockHostPrefix)+len(bedrockHostSuffix) {
		return "", false
	}
	region := host[len(bedrockHostPrefix) : len(host)-len(bedrockHostSuffix)]
	if region == "" || strings.Contains(region, ".") {
		return "", false
	}
	return region, true
}

// signV4 stamps an AWS Signature Version 4 authorization onto req for the given
// service/region using creds and the request payload. It sets X-Amz-Date (and
// X-Amz-Security-Token when the credential is temporary), then signs the host header
// plus every X-Amz-* header present — matching the AWS canonicalization so the
// upstream, which recomputes the signature from the received request, agrees. The
// caller sets any X-Amz-Content-Sha256 header it wants signed; the canonical payload
// hash is always taken over body, so an empty body hashes to the SHA-256 of the
// empty string.
func signV4(req *http.Request, body []byte, creds Credentials, region, service string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	payloadHash := hexSHA256(body)
	req.Header.Set("X-Amz-Date", amzDate)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	// Canonical + signed headers: the host header plus every X-Amz-* header present,
	// lowercased and sorted. Bedrock InvokeModel carries no query string, but the
	// canonical query is computed generally for correctness.
	type hv struct{ name, value string }
	headers := []hv{{"host", strings.TrimSpace(host)}}
	for name, vals := range req.Header {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), "X-Amz-") {
			headers = append(headers, hv{strings.ToLower(name), strings.TrimSpace(strings.Join(vals, ","))})
		}
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].name < headers[j].name })

	var canonHeaders strings.Builder
	signedNames := make([]string, 0, len(headers))
	for _, h := range headers {
		canonHeaders.WriteString(h.name)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(h.value)
		canonHeaders.WriteByte('\n')
		signedNames = append(signedNames, h.name)
	}
	signedHeaders := strings.Join(signedNames, ";")

	canonicalRequest := strings.Join([]string{
		req.Method,
		escapePath(req.URL.Path),
		canonicalQuery(req.URL.RawQuery),
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+creds.SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	auth := "AWS4-HMAC-SHA256 " +
		"Credential=" + creds.AccessKeyID + "/" + scope + ", " +
		"SignedHeaders=" + signedHeaders + ", " +
		"Signature=" + signature
	req.Header.Set("Authorization", auth)
}

// escapePath URI-encodes the path for the SigV4 canonical request: unreserved
// characters (RFC 3986) and the segment separator '/' are kept literal, everything
// else is percent-encoded with uppercase hex. This matches AWS's single-encoding for
// non-S3 services, so a Bedrock model id like ...-v2:0 canonicalizes its ':' to
// '%3A' exactly as the upstream does when it recomputes the signature.
func escapePath(path string) string {
	if path == "" {
		return "/"
	}
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		if isUnreserved(c) || c == '/' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex(c >> 4))
		b.WriteByte(upperHex(c & 0x0f))
	}
	return b.String()
}

// canonicalQuery builds the SigV4 canonical query string: each parameter
// URI-encoded and sorted by encoded key (then value). An empty query yields "".
func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	pairs := strings.Split(raw, "&")
	encoded := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p == "" {
			continue
		}
		k, v, hasEq := strings.Cut(p, "=")
		ek := escapeQuery(k)
		if hasEq {
			encoded = append(encoded, ek+"="+escapeQuery(v))
		} else {
			encoded = append(encoded, ek+"=")
		}
	}
	sort.Strings(encoded)
	return strings.Join(encoded, "&")
}

// escapeQuery URI-encodes a query key or value: unreserved characters are kept, all
// else percent-encoded (including '/'), per SigV4.
func escapeQuery(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex(c >> 4))
		b.WriteByte(upperHex(c & 0x0f))
	}
	return b.String()
}

// isUnreserved reports whether c is an RFC 3986 unreserved character
// (A-Z a-z 0-9 - . _ ~), which SigV4 never percent-encodes.
func isUnreserved(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '.' || c == '_' || c == '~':
		return true
	}
	return false
}

func upperHex(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + (n - 10)
}

func hexSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
