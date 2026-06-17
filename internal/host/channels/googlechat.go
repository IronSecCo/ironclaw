// OWNER: T-231

package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// defaultGoogleChatBaseURL is the Google Chat REST API host. Overridable
// (BaseURL) so tests can point at an httptest server.
const defaultGoogleChatBaseURL = "https://chat.googleapis.com"

// GoogleChatAdapter is a concrete, stdlib-only Adapter that delivers an outbound
// message to a Google Chat space via the REST API
// `POST {base}/v1/{space}/messages`. It follows the SlackAdapter shape: it sits
// behind the Adapter interface and adds no dependency.
//
// The space is taken from MessageOut.PlatformID (a space id such as "AAAA1234"
// or the full resource "spaces/AAAA1234"; a bare id is normalized to the
// "spaces/" form). A non-empty MessageOut.ThreadID maps to the message
// `thread.threadKey` so messages with the same key group into one thread. The
// returned platform message id is the created message resource `name`.
//
// SECURITY: the bot/service-account access token is sent in the Authorization
// header (never the URL), and the adapter NEVER includes the token in returned
// errors — error strings are redacted first — so a token cannot leak into logs
// even if a transport error or upstream payload were ever to echo it.
type GoogleChatAdapter struct {
	AdapterName string
	// Token is the Google Chat bot/service-account OAuth2 access token (Bearer).
	Token string
	// BaseURL defaults to defaultGoogleChatBaseURL; overridable for tests.
	BaseURL string
	Client  *http.Client
}

// NewGoogleChatAdapter constructs a GoogleChatAdapter. name defaults to
// "googlechat"; the client gets a default 15s timeout.
func NewGoogleChatAdapter(name, token string) *GoogleChatAdapter {
	if name == "" {
		name = "googlechat"
	}
	return &GoogleChatAdapter{
		AdapterName: name,
		Token:       token,
		BaseURL:     defaultGoogleChatBaseURL,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *GoogleChatAdapter) Name() string { return a.AdapterName }

// gchatThread groups messages into a thread by key.
type gchatThread struct {
	ThreadKey string `json:"threadKey,omitempty"`
}

// gchatMessage is the body of a create-message call.
type gchatMessage struct {
	Text   string       `json:"text"`
	Thread *gchatThread `json:"thread,omitempty"`
}

// gchatResponse is the success envelope: the created message resource name.
type gchatResponse struct {
	Name string `json:"name"`
}

// gchatError is the standard Google API error envelope on a non-2xx response.
type gchatError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Deliver sends msg.Content to the space in msg.PlatformID and returns the
// created message resource name as the platform message id.
func (a *GoogleChatAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if a.Token == "" {
		return "", fmt.Errorf("host/channels: googlechat %q has no access token", a.AdapterName)
	}
	space := ""
	if msg.PlatformID != nil {
		space = strings.TrimSpace(*msg.PlatformID)
	}
	if space == "" {
		return "", fmt.Errorf("host/channels: googlechat %q message has no space id (PlatformID)", a.AdapterName)
	}
	if !strings.HasPrefix(space, "spaces/") {
		space = "spaces/" + space
	}

	payload := gchatMessage{Text: msg.Content}
	if msg.ThreadID != nil {
		if key := strings.TrimSpace(*msg.ThreadID); key != "" {
			payload.Thread = &gchatThread{ThreadKey: key}
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal googlechat message: %w", err)
	}

	base := a.BaseURL
	if base == "" {
		base = defaultGoogleChatBaseURL
	}
	endpoint := strings.TrimRight(base, "/") + "/v1/" + space + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("host/channels: googlechat %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+a.Token)

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: googlechat %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ge gchatError
		desc := ""
		if json.Unmarshal(respBody, &ge) == nil && ge.Error.Message != "" {
			desc = ge.Error.Message
		}
		if desc == "" {
			desc = strings.TrimSpace(string(respBody))
		}
		return "", fmt.Errorf("host/channels: googlechat %q send failed (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(desc))
	}

	var gr gchatResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return "", fmt.Errorf("host/channels: googlechat %q decode response (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(err.Error()))
	}
	if gr.Name == "" {
		return "", fmt.Errorf("host/channels: googlechat %q send returned no message name", a.AdapterName)
	}
	return gr.Name, nil
}

// redact removes the access token from a string so it can never reach a log or error.
func (a *GoogleChatAdapter) redact(s string) string {
	if a.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Token, "<redacted>")
}
