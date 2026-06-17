// OWNER: AGENT2

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPFetchToolName is the tool the agent calls to reach an operator-approved
// external API through the host egress broker (T-111).
const HTTPFetchToolName = "http_fetch"

// maxFetchResponseBytes caps how much of an external response is read back into
// the agent's context so a large body cannot blow up the turn.
const maxFetchResponseBytes = 256 * 1024

// fetchTimeout bounds a single external call end to end.
const fetchTimeout = 30 * time.Second

// HTTPFetchTool performs an HTTP request to an EXTERNAL API through the host
// egress broker's unix socket. The sandbox has network=none: this tool's only
// reachable endpoint is the bound socket, and the host broker forwards the call
// only if the target host is on its operator-approved allowlist (every call is
// audited host-side). The tool therefore cannot reach anything the operator has
// not explicitly approved — a non-allowlisted host comes back as 403.
type HTTPFetchTool struct {
	client *http.Client
}

// NewHTTPFetchTool builds the tool over the egress broker socket bound into the
// sandbox. The HTTP client dials ONLY that unix socket regardless of the target
// host, so every request necessarily traverses the host broker.
func NewHTTPFetchTool(socketPath string) *HTTPFetchTool {
	return &HTTPFetchTool{
		client: &http.Client{
			Timeout: fetchTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

// Name identifies the tool.
func (t *HTTPFetchTool) Name() string { return HTTPFetchToolName }

// Description frames the tool's boundary for the model in-band.
func (t *HTTPFetchTool) Description() string {
	return "Make an HTTP request to an EXTERNAL API that the operator has explicitly approved. " +
		"Requests are brokered and audited by the host; a host that is not on the approved allowlist " +
		"returns 403. Use it for approved third-party APIs only — it is not general web/browser access."
}

// JSONSchema returns the input schema.
func (t *HTTPFetchTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"url":{"type":"string","description":"Absolute http(s) URL of the approved API endpoint."},` +
		`"method":{"type":"string","description":"HTTP method. Default GET.","enum":["GET","POST","PUT","PATCH","DELETE","HEAD"]},` +
		`"headers":{"type":"object","description":"Optional request headers.","additionalProperties":{"type":"string"}},` +
		`"body":{"type":"string","description":"Optional request body (e.g. JSON)."}` +
		`},"required":["url"],"additionalProperties":false}`)
}

type httpFetchInput struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Invoke validates the request and forwards it over the egress socket. The host
// broker enforces the allowlist; this tool only constructs and relays the call
// and returns the (truncated) response to the agent.
func (t *HTTPFetchTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	var in httpFetchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("http_fetch: invalid input: %w", err)
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil {
		return "", fmt.Errorf("http_fetch: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("http_fetch: url must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("http_fetch: url must include a host")
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}

	// The request travels over the egress unix socket as plain HTTP; the host broker
	// reads the Host to select the upstream and forwards over HTTPS. Force the
	// socket-hop scheme to http and preserve the target host + path/query.
	reqURL := &url.URL{Scheme: "http", Host: u.Host, Path: u.Path, RawQuery: u.RawQuery}
	var body io.Reader
	if in.Body != "" {
		body = strings.NewReader(in.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
	if err != nil {
		return "", fmt.Errorf("http_fetch: build request: %w", err)
	}
	req.Host = u.Host
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http_fetch: request failed (egress broker unreachable or upstream error): %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("http_fetch: read response: %w", err)
	}
	truncated := false
	if len(data) > maxFetchResponseBytes {
		data = data[:maxFetchResponseBytes]
		truncated = true
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "status: %s\n", resp.Status)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		fmt.Fprintf(&sb, "content-type: %s\n", ct)
	}
	sb.WriteString("\n")
	sb.Write(data)
	if truncated {
		fmt.Fprintf(&sb, "\n\n[truncated at %d bytes]", maxFetchResponseBytes)
	}
	return sb.String(), nil
}
