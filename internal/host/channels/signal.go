// OWNER: T-232

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

// SignalAdapter delivers an outbound message to Signal via a signal-cli REST
// bridge (e.g. bbernhard/signal-cli-rest-api) running host-side. Signal has no
// official bot API, so the host operates a registered signal-cli number and the
// adapter POSTs to its /v2/send endpoint. It is stdlib-only and follows the
// SlackAdapter shape. The recipient is taken from MessageOut.PlatformID (a phone
// number or group id); the sender is the adapter's configured Number.
type SignalAdapter struct {
	AdapterName string
	// BaseURL is the signal-cli-rest-api base (e.g. http://127.0.0.1:8080).
	BaseURL string
	// Number is the registered sender number (E.164, e.g. +15551234567).
	Number string
	Client *http.Client
}

// NewSignalAdapter constructs a SignalAdapter. name defaults to "signal"; the
// client gets a default 15s timeout.
func NewSignalAdapter(name, baseURL, number string) *SignalAdapter {
	if name == "" {
		name = "signal"
	}
	return &SignalAdapter{
		AdapterName: name,
		BaseURL:     baseURL,
		Number:      number,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *SignalAdapter) Name() string { return a.AdapterName }

// signalSend is the /v2/send request body.
type signalSend struct {
	Message    string   `json:"message"`
	Number     string   `json:"number"`
	Recipients []string `json:"recipients"`
}

// signalResponse is the /v2/send reply; signal-cli-rest-api returns a timestamp.
type signalResponse struct {
	Timestamp json.Number `json:"timestamp"`
}

// Deliver sends msg.Content to the recipient in msg.PlatformID via the bridge's
// /v2/send and returns the message timestamp as the platform message id.
func (a *SignalAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if strings.TrimSpace(a.BaseURL) == "" {
		return "", fmt.Errorf("host/channels: signal %q has no bridge URL", a.AdapterName)
	}
	if strings.TrimSpace(a.Number) == "" {
		return "", fmt.Errorf("host/channels: signal %q has no sender number", a.AdapterName)
	}
	recipient := ""
	if msg.PlatformID != nil {
		recipient = strings.TrimSpace(*msg.PlatformID)
	}
	if recipient == "" {
		return "", fmt.Errorf("host/channels: signal %q message has no recipient (PlatformID)", a.AdapterName)
	}

	body, err := json.Marshal(signalSend{Message: msg.Content, Number: a.Number, Recipients: []string{recipient}})
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal signal message: %w", err)
	}
	url := strings.TrimRight(a.BaseURL, "/") + "/v2/send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("host/channels: signal %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: signal %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("host/channels: signal %q /v2/send returned %d: %s",
			a.AdapterName, resp.StatusCode, a.redact(strings.TrimSpace(string(respBody))))
	}
	var sr signalResponse
	if err := json.Unmarshal(respBody, &sr); err == nil && sr.Timestamp.String() != "" {
		return sr.Timestamp.String(), nil
	}
	// The bridge accepted it but returned no parseable timestamp.
	return "sent", nil
}

// redact removes the sender number from a string so it cannot leak into a log.
func (a *SignalAdapter) redact(s string) string {
	if a.Number == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Number, "<redacted>")
}
