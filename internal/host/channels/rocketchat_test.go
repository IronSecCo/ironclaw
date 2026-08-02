package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func TestRocketChatAdapter_Deliver_Success(t *testing.T) {
	authToken := "secret-auth-token"
	userID := "user-id-123"
	channel := "#general"
	content := "Hello world"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/chat.postMessage" {
			t.Errorf("expected /api/v1/chat.postMessage, got %s", r.URL.Path)
		}

		if gotToken := r.Header.Get("X-Auth-Token"); gotToken != authToken {
			t.Errorf("expected X-Auth-Token %q, got %q", authToken, gotToken)
		}
		if gotUID := r.Header.Get("X-User-Id"); gotUID != userID {
			t.Errorf("expected X-User-Id %q, got %q", userID, gotUID)
		}

		body, _ := io.ReadAll(r.Body)
		var reqPayload rocketChatPayload
		if err := json.Unmarshal(body, &reqPayload); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		if reqPayload.Channel != channel {
			t.Errorf("expected channel %q, got %q", channel, reqPayload.Channel)
		}
		if reqPayload.Text != content {
			t.Errorf("expected text %q, got %q", content, reqPayload.Text)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(rocketChatResponse{
			Success:   true,
			MessageID: "msg-999",
		})
	}))
	defer ts.Close()

	adapter := NewRocketChatAdapter("test-rc", ts.URL, authToken, userID, channel)

	msgID, err := adapter.Deliver(context.Background(), contract.MessageOut{
		Content: content,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msgID != "msg-999" {
		t.Errorf("expected msgID 'msg-999', got %q", msgID)
	}
}

func TestRocketChatAdapter_RedactsSecretsInError(t *testing.T) {
	authToken := "super-secret-token"
	userID := "super-secret-user"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid token: " + authToken + " for user: " + userID))
	}))
	defer ts.Close()

	adapter := NewRocketChatAdapter("test-rc", ts.URL, authToken, userID, "general")

	_, err := adapter.Deliver(context.Background(), contract.MessageOut{
		Content: "test message",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errStr := err.Error()
	if strings.Contains(errStr, authToken) {
		t.Errorf("error leaked authToken: %s", errStr)
	}
	if strings.Contains(errStr, userID) {
		t.Errorf("error leaked userID: %s", errStr)
	}
}
