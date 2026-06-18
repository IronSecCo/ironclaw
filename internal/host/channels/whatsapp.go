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

// defaultWhatsAppBaseURL is the Meta Graph API host that fronts the WhatsApp
// Cloud API. Overridable (BaseURL) so tests can point at an httptest server.
const defaultWhatsAppBaseURL = "https://graph.facebook.com/v21.0"

// WhatsAppAdapter is a concrete, stdlib-only Adapter that delivers an outbound
// message via the WhatsApp Cloud API (Meta Graph API) `POST {phoneNumberID}/
// messages` endpoint. It follows the SlackAdapter/TelegramAdapter shape: it sits
// behind the Adapter interface and adds no dependency.
//
// The recipient is taken from MessageOut.PlatformID (the wa_id / phone number in
// E.164 form). A non-empty MessageOut.ThreadID maps to the WhatsApp reply
// `context.message_id` so replies quote the prior message. The returned platform
// message id is the WhatsApp `wamid` from `messages[0].id`.
//
// Beyond text, Deliver's sibling SendDocument satisfies the "send a file"
// capability by posting a `document` message (a hosted link). The interface only
// requires text Deliver, so file sending is an extra method on the concrete type
// rather than a contract change.
//
// SECURITY: the access token is sent in the Authorization header (never the URL),
// and the adapter NEVER includes the token in returned errors — error strings are
// redacted first — so a token cannot leak into logs even if a transport error or
// upstream payload were ever to echo it.
type WhatsAppAdapter struct {
	AdapterName string
	// Token is the WhatsApp Cloud API access token (Bearer).
	Token string
	// PhoneNumberID is the sender's WhatsApp phone-number id; it forms the path
	// segment `{PhoneNumberID}/messages`.
	PhoneNumberID string
	// BaseURL defaults to defaultWhatsAppBaseURL; overridable for tests.
	BaseURL string
	Client  *http.Client
}

// NewWhatsAppAdapter constructs a WhatsAppAdapter. name defaults to "whatsapp";
// the client gets a default 15s timeout.
func NewWhatsAppAdapter(name, token, phoneNumberID string) *WhatsAppAdapter {
	if name == "" {
		name = "whatsapp"
	}
	return &WhatsAppAdapter{
		AdapterName:   name,
		Token:         token,
		PhoneNumberID: phoneNumberID,
		BaseURL:       defaultWhatsAppBaseURL,
		Client:        &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *WhatsAppAdapter) Name() string { return a.AdapterName }

// --- Graph API request/response shapes ---

// waText is the text body of a WhatsApp `text` message.
type waText struct {
	Body string `json:"body"`
}

// waDocument is the body of a WhatsApp `document` message. Link points at a
// publicly fetchable file; Filename and Caption are optional.
type waDocument struct {
	Link     string `json:"link,omitempty"`
	Filename string `json:"filename,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

// waContext quotes a prior message so a reply threads correctly.
type waContext struct {
	MessageID string `json:"message_id"`
}

// waMessage is the JSON body of a `POST {phoneNumberID}/messages` call. A text
// message sets Text; a document message sets Document; Context is optional.
type waMessage struct {
	MessagingProduct string      `json:"messaging_product"`
	RecipientType    string      `json:"recipient_type,omitempty"`
	To               string      `json:"to"`
	Type             string      `json:"type"`
	Text             *waText     `json:"text,omitempty"`
	Document         *waDocument `json:"document,omitempty"`
	Context          *waContext  `json:"context,omitempty"`
}

// waMessageID is one element of the `messages` array in a success response.
type waMessageID struct {
	ID string `json:"id"`
}

// waError is the Graph API error envelope returned on a non-2xx response.
type waError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    int    `json:"code"`
}

// waResponse is the envelope a send call returns. On success `messages` carries
// the assigned wamid; on failure `error` carries the Graph API error.
type waResponse struct {
	Messages []waMessageID `json:"messages"`
	Error    *waError      `json:"error"`
}

// Deliver sends msg.Content as a text message to the recipient in msg.PlatformID
// and returns the WhatsApp message id (wamid) as the platform message id.
func (a *WhatsAppAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	to, err := a.recipient(msg.PlatformID)
	if err != nil {
		return "", err
	}
	payload := waMessage{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               to,
		Type:             "text",
		Text:             &waText{Body: msg.Content},
	}
	// A WhatsApp reply quotes a specific prior message id; map a non-empty
	// ThreadID through so replies thread, mirroring the other adapters.
	if msg.ThreadID != nil {
		if id := strings.TrimSpace(*msg.ThreadID); id != "" {
			payload.Context = &waContext{MessageID: id}
		}
	}
	return a.send(ctx, payload)
}

// SendDocument delivers a hosted file (by link) as a WhatsApp `document` message
// to `to`, with an optional filename and caption, and returns the wamid. This is
// the adapter's "send a file" capability; it is not part of the Adapter
// interface (the queue carries text), so callers use the concrete type.
func (a *WhatsAppAdapter) SendDocument(ctx context.Context, to, link, filename, caption string) (string, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return "", fmt.Errorf("host/channels: whatsapp %q document has no recipient", a.AdapterName)
	}
	if strings.TrimSpace(link) == "" {
		return "", fmt.Errorf("host/channels: whatsapp %q document has no link", a.AdapterName)
	}
	payload := waMessage{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               to,
		Type:             "document",
		Document:         &waDocument{Link: link, Filename: filename, Caption: caption},
	}
	return a.send(ctx, payload)
}

// recipient extracts and validates the destination wa_id from PlatformID.
func (a *WhatsAppAdapter) recipient(platformID *string) (string, error) {
	to := ""
	if platformID != nil {
		to = strings.TrimSpace(*platformID)
	}
	if to == "" {
		return "", fmt.Errorf("host/channels: whatsapp %q message has no recipient (PlatformID)", a.AdapterName)
	}
	return to, nil
}

// send marshals payload, POSTs it to the Cloud API, and returns the assigned
// wamid. The access token is redacted from every returned error.
func (a *WhatsAppAdapter) send(ctx context.Context, payload waMessage) (string, error) {
	if a.Token == "" {
		return "", fmt.Errorf("host/channels: whatsapp %q has no access token", a.AdapterName)
	}
	if strings.TrimSpace(a.PhoneNumberID) == "" {
		return "", fmt.Errorf("host/channels: whatsapp %q has no phone-number id", a.AdapterName)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal whatsapp message: %w", err)
	}

	base := a.BaseURL
	if base == "" {
		base = defaultWhatsAppBaseURL
	}
	url := strings.TrimRight(base, "/") + "/" + a.PhoneNumberID + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("host/channels: whatsapp %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+a.Token)

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: whatsapp %q POST failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var wr waResponse
	if err := json.Unmarshal(respBody, &wr); err != nil {
		return "", fmt.Errorf("host/channels: whatsapp %q decode response (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(err.Error()))
	}

	// The Cloud API uses real HTTP status codes: any non-2xx is a failure and
	// carries a Graph API error envelope.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		desc := ""
		if wr.Error != nil {
			desc = wr.Error.Message
		}
		if desc == "" {
			desc = strings.TrimSpace(string(respBody))
		}
		return "", fmt.Errorf("host/channels: whatsapp %q send failed (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(desc))
	}
	if len(wr.Messages) == 0 || wr.Messages[0].ID == "" {
		return "", fmt.Errorf("host/channels: whatsapp %q send returned no message id", a.AdapterName)
	}
	return wr.Messages[0].ID, nil
}

// redact removes the access token from a string so it can never reach a log or error.
func (a *WhatsAppAdapter) redact(s string) string {
	if a.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Token, "<redacted>")
}
