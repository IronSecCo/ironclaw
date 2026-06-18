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

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// defaultSlackBaseURL is the Slack Web API host. Overridable (BaseURL) so tests
// can point at an httptest server.
const defaultSlackBaseURL = "https://slack.com/api"

// SlackAdapter is a concrete, stdlib-only Adapter that delivers an outbound
// message via the Slack Web API `chat.postMessage` method. It follows the
// TelegramAdapter shape: it sits behind the Adapter interface and adds no
// dependency. The channel is taken from MessageOut.PlatformID; a ThreadID maps
// to Slack's thread_ts so replies thread correctly. The returned platform
// message id is the Slack message `ts`.
//
// SECURITY: the bot token is sent in the Authorization header (not the URL).
// The adapter NEVER includes the token in returned errors — error strings are
// redacted first — so a token cannot leak into logs even if a transport error
// were ever to echo a header.
type SlackAdapter struct {
	AdapterName string
	Token       string
	// BaseURL defaults to defaultSlackBaseURL; overridable for tests.
	BaseURL string
	Client  *http.Client
}

// NewSlackAdapter constructs a SlackAdapter. name defaults to "slack"; the
// client gets a default 15s timeout.
func NewSlackAdapter(name, token string) *SlackAdapter {
	if name == "" {
		name = "slack"
	}
	return &SlackAdapter{
		AdapterName: name,
		Token:       token,
		BaseURL:     defaultSlackBaseURL,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *SlackAdapter) Name() string { return a.AdapterName }

// slackPostMessage is the JSON body of a chat.postMessage call.
type slackPostMessage struct {
	Channel  string `json:"channel"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

// slackResponse is the envelope chat.postMessage returns. Slack replies HTTP 200
// even on a logical failure, so the OK field — not the status — is authoritative.
type slackResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	TS    string `json:"ts"`
}

// Deliver sends msg.Content to the channel in msg.PlatformID via chat.postMessage
// and returns the Slack message ts as the platform message id.
func (a *SlackAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if a.Token == "" {
		return "", fmt.Errorf("host/channels: slack %q has no bot token", a.AdapterName)
	}
	channel := ""
	if msg.PlatformID != nil {
		channel = strings.TrimSpace(*msg.PlatformID)
	}
	if channel == "" {
		return "", fmt.Errorf("host/channels: slack %q message has no channel id (PlatformID)", a.AdapterName)
	}

	payload := slackPostMessage{Channel: channel, Text: msg.Content}
	// Slack thread keys are message timestamps (e.g. "1700000000.000100"); pass a
	// non-empty ThreadID straight through so replies land in-thread.
	if msg.ThreadID != nil {
		if ts := strings.TrimSpace(*msg.ThreadID); ts != "" {
			payload.ThreadTS = ts
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal slack message: %w", err)
	}

	base := a.BaseURL
	if base == "" {
		base = defaultSlackBaseURL
	}
	url := strings.TrimRight(base, "/") + "/chat.postMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("host/channels: slack %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+a.Token)

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: slack %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var sr slackResponse
	if err := json.Unmarshal(respBody, &sr); err != nil {
		return "", fmt.Errorf("host/channels: slack %q decode response (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(err.Error()))
	}
	if !sr.OK {
		desc := sr.Error
		if desc == "" {
			desc = strings.TrimSpace(string(respBody))
		}
		return "", fmt.Errorf("host/channels: slack %q chat.postMessage failed: %s",
			a.AdapterName, a.redact(desc))
	}
	return sr.TS, nil
}

// redact removes the bot token from a string so it can never reach a log or error.
func (a *SlackAdapter) redact(s string) string {
	if a.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Token, "<redacted>")
}
