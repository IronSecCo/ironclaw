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

// TeamsAdapter delivers an outbound message to Microsoft Teams via an Incoming
// Webhook URL. It is stdlib-only and follows the SlackAdapter shape. The webhook
// URL is per-channel and configured host-side, so a message's PlatformID is
// advisory only — the destination is the webhook itself. Incoming Webhooks are
// outbound-only (one-way), which is exactly what a delivery adapter needs.
//
// SECURITY: the webhook URL embeds a secret token, so it is sent only in the
// request line (never logged) and is ALWAYS redacted from returned errors.
type TeamsAdapter struct {
	AdapterName string
	WebhookURL  string
	Client      *http.Client
}

// NewTeamsAdapter constructs a TeamsAdapter. name defaults to "teams"; the client
// gets a default 15s timeout.
func NewTeamsAdapter(name, webhookURL string) *TeamsAdapter {
	if name == "" {
		name = "teams"
	}
	return &TeamsAdapter{
		AdapterName: name,
		WebhookURL:  webhookURL,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *TeamsAdapter) Name() string { return a.AdapterName }

// teamsMessage is the minimal Incoming Webhook payload. Teams accepts a bare
// {"text": "..."} body (MarkDown-rendered) on the legacy webhook endpoint.
type teamsMessage struct {
	Text string `json:"text"`
}

// Deliver posts msg.Content to the configured Teams Incoming Webhook. Incoming
// Webhooks return HTTP 200 with the body "1" on success and do not assign a
// retrievable message id, so the trimmed response body is returned as the id.
func (a *TeamsAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if strings.TrimSpace(a.WebhookURL) == "" {
		return "", fmt.Errorf("host/channels: teams %q has no webhook URL", a.AdapterName)
	}
	if strings.TrimSpace(msg.Content) == "" {
		return "", fmt.Errorf("host/channels: teams %q message has empty content", a.AdapterName)
	}
	body, err := json.Marshal(teamsMessage{Text: msg.Content})
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal teams message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("host/channels: teams %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: teams %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("host/channels: teams %q webhook returned %d: %s",
			a.AdapterName, resp.StatusCode, a.redact(strings.TrimSpace(string(respBody))))
	}
	id := strings.TrimSpace(string(respBody))
	if id == "" {
		id = "delivered"
	}
	return id, nil
}

// redact removes the webhook URL from a string so its secret token can never
// reach a log or error.
func (a *TeamsAdapter) redact(s string) string {
	if a.WebhookURL == "" {
		return s
	}
	return strings.ReplaceAll(s, a.WebhookURL, "<redacted>")
}
