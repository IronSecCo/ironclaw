package channels

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// MatrixAdapter is a concrete, stdlib-only Adapter that delivers an outbound
// message via the Matrix client-server API `PUT /_matrix/client/v3/rooms/{roomId}
// /send/m.room.message/{txnId}` endpoint. It follows the SlackAdapter shape: it
// sits behind the Adapter interface and adds no dependency.
//
// The room is taken from MessageOut.PlatformID (e.g. "!abc:example.org"). A
// non-empty MessageOut.ThreadID maps to an `m.relates_to` thread relation so the
// message lands in that thread. Each send uses a fresh transaction id, making
// the PUT idempotent. The returned platform message id is the Matrix `event_id`.
//
// Unlike the fixed-host platforms, Matrix is self-hosted, so HomeserverURL is
// required configuration (there is no default homeserver).
//
// SECURITY: the access token is sent in the Authorization header (never the
// URL), and the adapter NEVER includes the token in returned errors — error
// strings are redacted first — so a token cannot leak into logs even if a
// transport error or upstream payload were ever to echo it.
type MatrixAdapter struct {
	AdapterName string
	// HomeserverURL is the base URL of the Matrix homeserver (e.g.
	// "https://matrix.example.org"). Required. Overridable for tests.
	HomeserverURL string
	// Token is the Matrix access token (Bearer).
	Token  string
	Client *http.Client
}

// NewMatrixAdapter constructs a MatrixAdapter. name defaults to "matrix"; the
// client gets a default 15s timeout.
func NewMatrixAdapter(name, homeserverURL, token string) *MatrixAdapter {
	if name == "" {
		name = "matrix"
	}
	return &MatrixAdapter{
		AdapterName:   name,
		HomeserverURL: homeserverURL,
		Token:         token,
		Client:        &http.Client{Timeout: 15 * time.Second},
	}
}

// Name returns the adapter name.
func (a *MatrixAdapter) Name() string { return a.AdapterName }

// matrixRelatesTo expresses a thread relation (MSC3440, stable since v1.4).
type matrixRelatesTo struct {
	RelType string `json:"rel_type"`
	EventID string `json:"event_id"`
}

// matrixMessage is the event content of an m.room.message send.
type matrixMessage struct {
	MsgType   string           `json:"msgtype"`
	Body      string           `json:"body"`
	RelatesTo *matrixRelatesTo `json:"m.relates_to,omitempty"`
}

// matrixSendResponse is the success envelope of a send (the assigned event id).
type matrixSendResponse struct {
	EventID string `json:"event_id"`
}

// matrixError is the standard Matrix error envelope on a non-2xx response.
type matrixError struct {
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`
}

// Deliver sends msg.Content as an m.text message to the room in msg.PlatformID
// and returns the Matrix event_id as the platform message id.
func (a *MatrixAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	if a.Token == "" {
		return "", fmt.Errorf("host/channels: matrix %q has no access token", a.AdapterName)
	}
	if strings.TrimSpace(a.HomeserverURL) == "" {
		return "", fmt.Errorf("host/channels: matrix %q has no homeserver URL", a.AdapterName)
	}
	room := ""
	if msg.PlatformID != nil {
		room = strings.TrimSpace(*msg.PlatformID)
	}
	if room == "" {
		return "", fmt.Errorf("host/channels: matrix %q message has no room id (PlatformID)", a.AdapterName)
	}

	content := matrixMessage{MsgType: "m.text", Body: msg.Content}
	if msg.ThreadID != nil {
		if id := strings.TrimSpace(*msg.ThreadID); id != "" {
			content.RelatesTo = &matrixRelatesTo{RelType: "m.thread", EventID: id}
		}
	}
	body, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("host/channels: marshal matrix message: %w", err)
	}

	txnID, err := newTxnID()
	if err != nil {
		return "", fmt.Errorf("host/channels: matrix %q txn id: %w", a.AdapterName, err)
	}
	endpoint := strings.TrimRight(a.HomeserverURL, "/") +
		"/_matrix/client/v3/rooms/" + url.PathEscape(room) +
		"/send/m.room.message/" + url.PathEscape(txnID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("host/channels: matrix %q build request: %s", a.AdapterName, a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.Token)

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: matrix %q PUT failed: %s", a.AdapterName, a.redact(err.Error()))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var me matrixError
		desc := ""
		if json.Unmarshal(respBody, &me) == nil && me.Error != "" {
			desc = me.Error
			if me.ErrCode != "" {
				desc = me.ErrCode + ": " + desc
			}
		}
		if desc == "" {
			desc = strings.TrimSpace(string(respBody))
		}
		return "", fmt.Errorf("host/channels: matrix %q send failed (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(desc))
	}

	var sr matrixSendResponse
	if err := json.Unmarshal(respBody, &sr); err != nil {
		return "", fmt.Errorf("host/channels: matrix %q decode response (status %d): %s",
			a.AdapterName, resp.StatusCode, a.redact(err.Error()))
	}
	if sr.EventID == "" {
		return "", fmt.Errorf("host/channels: matrix %q send returned no event id", a.AdapterName)
	}
	return sr.EventID, nil
}

// newTxnID returns a fresh, unique transaction id so the send PUT is idempotent.
func newTxnID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "ic" + hex.EncodeToString(buf[:]), nil
}

// redact removes the access token from a string so it can never reach a log or error.
func (a *MatrixAdapter) redact(s string) string {
	if a.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Token, "<redacted>")
}
