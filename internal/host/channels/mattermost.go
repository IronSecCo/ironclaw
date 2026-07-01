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

// MattermostAdapter delivers an outbound message to Mattermost via an Incoming
// Webhook URL. It is stdlib-only and follows the TeamsAdapter shape.
//
// SECURITY: the webhook URL embeds a secret token, so it is sent only in the
// request line (never logged) and is ALWAYS redacted from returned errors.
type MattermostAdapter struct {
	AdapterName string
	WebhookURL  string
	Client      *http.Client
}

func NewMattermostAdapter(name, webhookURL string) *MattermostAdapter {
	if name == "" {
		name = "mattermost"
	}

	return &MattermostAdapter{
		AdapterName: name,
		WebhookURL:  webhookURL,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *MattermostAdapter) Name() string {
	return a.AdapterName
}

type mattermostMessage struct {
	Text string `json:"text"`
}

func (a *MattermostAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if strings.TrimSpace(a.WebhookURL) == "" {
		return "", fmt.Errorf("host/channels: mattermost %q has no webhook url", a.AdapterName)
	}
	if strings.TrimSpace(msg.Content) == "" {
		return "", fmt.Errorf("host/channels: mattermost %q message has empty content", a.AdapterName)
	}

	payload := mattermostMessage{
		Text: msg.Content,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal mattermost message: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.WebhookURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("host/channels: mattermost %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: mattermost %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		desc := strings.TrimSpace(string(respBody))
		if desc == "" {
			desc = resp.Status
		}

		return "", fmt.Errorf(
			"host/channels: mattermost %q send failed (status %d): %s",
			a.AdapterName,
			resp.StatusCode,
			a.redact(desc),
		)
	}

	id := strings.TrimSpace(string(respBody))
	if id == "" {
		id = "delivered"
	}
	return id, nil
}

// redact removes the webhook URL from a string so its secret token can never
// reach a log or error.
func (a *MattermostAdapter) redact(s string) string {
	if a.WebhookURL == "" {
		return s
	}

	return strings.ReplaceAll(s, a.WebhookURL, "<redacted>")
}
