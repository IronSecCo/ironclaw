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

// RocketChatAdapter delivers an outbound message to Rocket.Chat via the REST API endpoint
// (POST /api/v1/chat.postMessage) using auth headers.
//
// SECURITY: X-Auth-Token and X-User-Id are kept secure and redacted from returned errors.
type RocketChatAdapter struct {
	AdapterName string
	BaseURL     string
	AuthToken   string
	UserID      string
	Channel     string
	Client      *http.Client
}

func NewRocketChatAdapter(name, baseURL, authToken, userID, channel string) *RocketChatAdapter {
	if name == "" {
		name = "rocketchat"
	}

	return &RocketChatAdapter{
		AdapterName: name,
		BaseURL:     strings.TrimRight(baseURL, "/"),
		AuthToken:   authToken,
		UserID:      userID,
		Channel:     channel,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *RocketChatAdapter) Name() string {
	return a.AdapterName
}

type rocketChatPayload struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

type rocketChatResponse struct {
	Success   bool   `json:"success"`
	MessageID string `json:"message"`
	Error     string `json:"error,omitempty"`
}

func (a *RocketChatAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if strings.TrimSpace(a.BaseURL) == "" {
		return "", fmt.Errorf("host/channels: rocketchat %q has no base url", a.AdapterName)
	}
	if strings.TrimSpace(a.AuthToken) == "" {
		return "", fmt.Errorf("host/channels: rocketchat %q has no auth token", a.AdapterName)
	}
	if strings.TrimSpace(a.UserID) == "" {
		return "", fmt.Errorf("host/channels: rocketchat %q has no user id", a.AdapterName)
	}
	if strings.TrimSpace(msg.Content) == "" {
		return "", fmt.Errorf("host/channels: rocketchat %q message has empty content", a.AdapterName)
	}

	targetChannel := a.Channel
	if strings.TrimSpace(targetChannel) == "" {
		return "", fmt.Errorf("host/channels: rocketchat %q message has no target channel", a.AdapterName)
	}

	endpoint := a.BaseURL + "/api/v1/chat.postMessage"

	payload := rocketChatPayload{
		Channel: targetChannel,
		Text:    msg.Content,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal rocketchat message: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("host/channels: rocketchat %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-Auth-Token", a.AuthToken)
	req.Header.Set("X-User-Id", a.UserID)

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: rocketchat %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		desc := strings.TrimSpace(string(respBody))
		if desc == "" {
			desc = resp.Status
		}

		return "", fmt.Errorf(
			"host/channels: rocketchat %q send failed (status %d): %s",
			a.AdapterName,
			resp.StatusCode,
			a.redact(desc),
		)
	}

	var apiResp rocketChatResponse
	if err := json.Unmarshal(respBody, &apiResp); err == nil && !apiResp.Success {
		errDesc := apiResp.Error
		if errDesc == "" {
			errDesc = "api returned success=false"
		}
		return "", fmt.Errorf(
			"host/channels: rocketchat %q send failed: %s",
			a.AdapterName,
			a.redact(errDesc),
		)
	}

	id := strings.TrimSpace(apiResp.MessageID)
	if id == "" {
		id = "delivered"
	}
	return id, nil
}

// redact removes auth credentials from strings to prevent accidental token exposure in logs.
func (a *RocketChatAdapter) redact(s string) string {
	if a.AuthToken != "" {
		s = strings.ReplaceAll(s, a.AuthToken, "<redacted>")
	}
	if a.UserID != "" {
		s = strings.ReplaceAll(s, a.UserID, "<redacted>")
	}
	return s
}
