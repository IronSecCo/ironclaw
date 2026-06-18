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

// WebhookAdapter is a concrete, stdlib-only Adapter that delivers an outbound
// message by POSTing it as JSON to a configured URL. It is the reference
// HTTP-based channel: platform-specific adapters (Slack, Discord, ...) can follow
// the same shape. The platform message ID is taken from the response (a JSON
// {"id": "..."} body, or the X-Message-Id header), falling back to a synthetic id.
type WebhookAdapter struct {
	AdapterName string
	URL         string
	Client      *http.Client
}

// NewWebhookAdapter constructs a WebhookAdapter. name defaults to "webhook"; a nil
// client gets a default with a 15s timeout.
func NewWebhookAdapter(name, url string) *WebhookAdapter {
	if name == "" {
		name = "webhook"
	}
	return &WebhookAdapter{
		AdapterName: name,
		URL:         url,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *WebhookAdapter) Name() string { return a.AdapterName }

// Deliver POSTs msg as JSON to the configured URL.
func (a *WebhookAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if a.URL == "" {
		return "", fmt.Errorf("host/channels: webhook %q has no URL", a.AdapterName)
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: webhook POST: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("host/channels: webhook %q returned %d: %s",
			a.AdapterName, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// Prefer an id from the response body, then a header, then a synthetic id.
	var parsed struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(respBody, &parsed) == nil && parsed.ID != "" {
		return parsed.ID, nil
	}
	if h := resp.Header.Get("X-Message-Id"); h != "" {
		return h, nil
	}
	return string(msg.ID), nil
}
