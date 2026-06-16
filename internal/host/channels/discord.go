// OWNER: T-109b

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

// defaultDiscordBaseURL is the Discord REST API host (v10). Overridable
// (BaseURL) so tests can point at an httptest server.
const defaultDiscordBaseURL = "https://discord.com/api/v10"

// DiscordAdapter is a concrete, stdlib-only Adapter that delivers an outbound
// message via the Discord REST API create-message endpoint. It follows the
// TelegramAdapter shape: it sits behind the Adapter interface and adds no
// dependency. The channel is taken from MessageOut.PlatformID; a numeric
// ThreadID is treated as the id of a message to reply to (message_reference), so
// replies thread; a non-numeric ThreadID is ignored. The returned platform
// message id is the Discord message snowflake id.
//
// SECURITY: the bot token is sent in the Authorization header ("Bot <token>").
// The adapter NEVER includes the token in returned errors — error strings are
// redacted first — so a token cannot leak into logs.
type DiscordAdapter struct {
	AdapterName string
	Token       string
	// BaseURL defaults to defaultDiscordBaseURL; overridable for tests.
	BaseURL string
	Client  *http.Client
}

// NewDiscordAdapter constructs a DiscordAdapter. name defaults to "discord"; the
// client gets a default 15s timeout.
func NewDiscordAdapter(name, token string) *DiscordAdapter {
	if name == "" {
		name = "discord"
	}
	return &DiscordAdapter{
		AdapterName: name,
		Token:       token,
		BaseURL:     defaultDiscordBaseURL,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *DiscordAdapter) Name() string { return a.AdapterName }

// discordCreateMessage is the JSON body of a create-message call.
type discordCreateMessage struct {
	Content          string                   `json:"content"`
	MessageReference *discordMessageReference `json:"message_reference,omitempty"`
}

// discordMessageReference threads a message as a reply to MessageID.
type discordMessageReference struct {
	MessageID string `json:"message_id"`
}

// discordResponse is the relevant subset of the message object on success and the
// error envelope on failure. Discord signals success via the HTTP status (2xx).
type discordResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"` // error message on failure
	Code    int    `json:"code"`    // Discord error code on failure
}

// Deliver sends msg.Content to the channel in msg.PlatformID via create-message
// and returns the Discord message snowflake id as the platform message id.
func (a *DiscordAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if a.Token == "" {
		return "", fmt.Errorf("host/channels: discord %q has no bot token", a.AdapterName)
	}
	channelID := ""
	if msg.PlatformID != nil {
		channelID = strings.TrimSpace(*msg.PlatformID)
	}
	if channelID == "" {
		return "", fmt.Errorf("host/channels: discord %q message has no channel id (PlatformID)", a.AdapterName)
	}

	payload := discordCreateMessage{Content: msg.Content}
	// A numeric ThreadID is a message snowflake to reply to; a non-numeric one
	// (our internal thread key) does not map to Discord and is omitted.
	if msg.ThreadID != nil {
		if ref := strings.TrimSpace(*msg.ThreadID); isSnowflake(ref) {
			payload.MessageReference = &discordMessageReference{MessageID: ref}
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal discord message: %w", err)
	}

	base := a.BaseURL
	if base == "" {
		base = defaultDiscordBaseURL
	}
	url := strings.TrimRight(base, "/") + "/channels/" + channelID + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("host/channels: discord %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+a.Token)

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: discord %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var dr discordResponse
	if err := json.Unmarshal(respBody, &dr); err != nil {
		return "", fmt.Errorf("host/channels: discord %q decode response (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(err.Error()))
	}
	// Discord uses the HTTP status to signal success (200/201 with a message object).
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || dr.ID == "" {
		desc := dr.Message
		if desc == "" {
			desc = strings.TrimSpace(string(respBody))
		}
		return "", fmt.Errorf("host/channels: discord %q create-message failed (status %d, code %d): %s",
			a.AdapterName, resp.StatusCode, dr.Code, a.redact(desc))
	}
	return dr.ID, nil
}

// isSnowflake reports whether s is a non-empty all-digit Discord id.
func isSnowflake(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// redact removes the bot token from a string so it can never reach a log or error.
func (a *DiscordAdapter) redact(s string) string {
	if a.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Token, "<redacted>")
}
