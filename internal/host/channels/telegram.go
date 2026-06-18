package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// defaultTelegramBaseURL is the public Bot API host. Overridable (BaseURL) so
// tests can point at an httptest server.
const defaultTelegramBaseURL = "https://api.telegram.org"

// TelegramAdapter is a concrete, stdlib-only Adapter that delivers an outbound
// message via the Telegram Bot API `sendMessage` method. It follows the
// WebhookAdapter shape: it sits behind the Adapter interface and adds no
// dependency. The chat is taken from MessageOut.PlatformID; a numeric ThreadID
// maps to a forum-topic message_thread_id. The returned platform message id is the
// Telegram message_id.
//
// SECURITY: the bot token is part of the request URL (Telegram's design). The
// adapter NEVER includes the token in returned errors — transport errors (whose
// url.Error carries the URL) are redacted first — so a token cannot leak into
// logs.
type TelegramAdapter struct {
	AdapterName string
	Token       string
	// BaseURL defaults to defaultTelegramBaseURL; overridable for tests.
	BaseURL string
	Client  *http.Client
}

// NewTelegramAdapter constructs a TelegramAdapter. name defaults to "telegram";
// the client gets a default 15s timeout.
func NewTelegramAdapter(name, token string) *TelegramAdapter {
	if name == "" {
		name = "telegram"
	}
	return &TelegramAdapter{
		AdapterName: name,
		Token:       token,
		BaseURL:     defaultTelegramBaseURL,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *TelegramAdapter) Name() string { return a.AdapterName }

// tgSendMessage is the JSON body of a Bot API sendMessage call.
type tgSendMessage struct {
	ChatID          string `json:"chat_id"`
	Text            string `json:"text"`
	MessageThreadID *int   `json:"message_thread_id,omitempty"`
}

// tgResponse is the envelope every Bot API method returns.
type tgResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	Result      struct {
		MessageID int64 `json:"message_id"`
	} `json:"result"`
}

// Deliver sends msg.Content to the chat in msg.PlatformID via sendMessage and
// returns the Telegram message_id as the platform message id.
func (a *TelegramAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if a.Token == "" {
		return "", fmt.Errorf("host/channels: telegram %q has no bot token", a.AdapterName)
	}
	chatID := ""
	if msg.PlatformID != nil {
		chatID = strings.TrimSpace(*msg.PlatformID)
	}
	if chatID == "" {
		return "", fmt.Errorf("host/channels: telegram %q message has no chat id (PlatformID)", a.AdapterName)
	}

	payload := tgSendMessage{ChatID: chatID, Text: msg.Content}
	// A numeric ThreadID is a forum-topic id; a non-numeric one (our internal thread
	// key) does not map to Telegram and is omitted.
	if msg.ThreadID != nil {
		if tid, err := strconv.Atoi(strings.TrimSpace(*msg.ThreadID)); err == nil {
			payload.MessageThreadID = &tid
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal telegram message: %w", err)
	}

	base := a.BaseURL
	if base == "" {
		base = defaultTelegramBaseURL
	}
	url := strings.TrimRight(base, "/") + "/bot" + a.Token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		// err may embed the URL (token); redact before returning.
		return "", fmt.Errorf("host/channels: telegram %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// A transport error's url.Error carries the token-bearing URL — redact it.
		return "", fmt.Errorf("host/channels: telegram %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var tr tgResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return "", fmt.Errorf("host/channels: telegram %q decode response (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(err.Error()))
	}
	if !tr.OK {
		desc := tr.Description
		if desc == "" {
			desc = strings.TrimSpace(string(respBody))
		}
		return "", fmt.Errorf("host/channels: telegram %q sendMessage failed (error_code=%d): %s",
			a.AdapterName, tr.ErrorCode, a.redact(desc))
	}
	return strconv.FormatInt(tr.Result.MessageID, 10), nil
}

// redact removes the bot token from a string so it can never reach a log or error.
func (a *TelegramAdapter) redact(s string) string {
	if a.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Token, "<redacted>")
}
