package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// httpConfig configures a remote (streamable-HTTP) MCP server. The host dials URL
// over HTTPS; the sandbox never does. Headers carry any auth the operator configured
// (e.g. Authorization), already expanded host-side, so credentials live on the host
// and never reach the agent.
type httpConfig struct {
	URL     string
	Headers map[string]string
	Client  *http.Client
}

// maxHTTPResponseBytes caps a single MCP HTTP response so a hostile/broken server
// cannot exhaust host memory.
const maxHTTPResponseBytes = 8 << 20 // 8 MiB

// httpTransport speaks JSON-RPC over the MCP streamable-HTTP transport: each message
// is POSTed to a single endpoint, and a response comes back as either
// application/json (one message) or text/event-stream (SSE events, one of which is
// the matching response). A server-issued Mcp-Session-Id is captured and resent.
type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client

	mu        sync.Mutex
	sessionID string
}

// DialHTTP returns a connected (not yet initialized) Client for a remote endpoint.
func DialHTTP(cfg httpConfig) (*Client, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("mcp: http transport needs a URL")
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return newClient(&httpTransport{url: cfg.URL, headers: cfg.Headers, client: client}), nil
}

func (t *httpTransport) roundTrip(ctx context.Context, id int64, method string, params any) (json.RawMessage, error) {
	resp, err := t.post(ctx, rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	msg, err := t.readMessage(resp, &id)
	if err != nil {
		return nil, err
	}
	if msg.Error != nil {
		return nil, fmt.Errorf("server error %d: %s", msg.Error.Code, msg.Error.Message)
	}
	return msg.Result, nil
}

func (t *httpTransport) notify(ctx context.Context, method string, params any) error {
	resp, err := t.post(ctx, rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	// Drain and discard: a notification has no response (servers typically 202).
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxHTTPResponseBytes))
	_ = resp.Body.Close()
	return nil
}

// post sends one JSON-RPC message and returns the raw HTTP response (caller closes
// the body via readMessage/notify).
func (t *httpTransport) post(ctx context.Context, msg rpcRequest) (*http.Response, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: http request failed: %w", err)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("mcp: server returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}
	return resp, nil
}

// readMessage extracts the JSON-RPC response matching wantID from an HTTP response,
// handling both a single application/json body and a text/event-stream of SSE
// events.
func (t *httpTransport) readMessage(resp *http.Response, wantID *int64) (rpcResponse, error) {
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	body := io.LimitReader(resp.Body, maxHTTPResponseBytes)

	if strings.HasPrefix(ct, "text/event-stream") {
		return readSSE(body, wantID)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return rpcResponse{}, fmt.Errorf("mcp: read response: %w", err)
	}
	var msg rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(data), &msg); err != nil {
		return rpcResponse{}, fmt.Errorf("mcp: decode response: %w", err)
	}
	return msg, nil
}

// readSSE parses an SSE stream and returns the JSON-RPC response whose id matches
// wantID, falling back to the first decodable response when none matches (a server
// that omits the echoed id).
func readSSE(r io.Reader, wantID *int64) (rpcResponse, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxHTTPResponseBytes)
	var dataLines []string
	var first *rpcResponse

	flush := func() (rpcResponse, bool) {
		if len(dataLines) == 0 {
			return rpcResponse{}, false
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		var msg rpcResponse
		if json.Unmarshal([]byte(payload), &msg) != nil {
			return rpcResponse{}, false
		}
		return msg, true
	}

	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if msg, ok := flush(); ok {
				if wantID != nil && msg.ID != nil && *msg.ID == *wantID {
					return msg, nil
				}
				if first == nil {
					m := msg
					first = &m
				}
			}
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			// other SSE fields (event:, id:, retry:) are ignored
		}
	}
	if err := sc.Err(); err != nil {
		return rpcResponse{}, fmt.Errorf("mcp: read sse: %w", err)
	}
	if msg, ok := flush(); ok { // stream ended without a trailing blank line
		if wantID != nil && msg.ID != nil && *msg.ID == *wantID {
			return msg, nil
		}
		if first == nil {
			first = &msg
		}
	}
	if first != nil {
		return *first, nil
	}
	return rpcResponse{}, errors.New("mcp: no JSON-RPC response in event stream")
}

// Close is a no-op for HTTP (no persistent process/connection to tear down).
func (t *httpTransport) Close() error { return nil }
