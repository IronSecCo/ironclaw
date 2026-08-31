package channels

import (
    "context"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/IronSecCo/ironclaw/internal/contract"
)

func TestRocketChatAdapter_Deliver(t *testing.T) {
    // Simulate a real captured response body from Rocket.Chat API.
    // Note how "message" is an object containing "_id", which matches our fixed struct.
    fakeSuccessResponse := `{
        "success": true,
        "message": {
            "_id": "abc1234567890",
            "rid": "GENERAL",
            "msg": "Hello",
            "ts": "2023-10-25T12:00:00.000Z",
            "u": {
                "_id": "user123",
                "username": "bot"
            }
        }
    }`

    t.Run("successful delivery returns real message ID", func(t *testing.T) {
        ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            _, _ = io.WriteString(w, fakeSuccessResponse)
        }))
        defer ts.Close()

        adapter := NewRocketChatAdapter("test-rocketchat", ts.URL, "fake-token", "fake-user-id", "#general")

        msg := contract.MessageOut{
            Content: "test message",
        }

        id, err := adapter.Deliver(context.Background(), msg)
        if err != nil {
            t.Fatalf("expected no error, got %v", err)
        }

        if id != "abc1234567890" {
            t.Fatalf("expected message ID 'abc1234567890', got %s", id)
        }
    })

    t.Run("API returns 200 but success=false", func(t *testing.T) {
        failResponse := `{"success": false, "error": "Invalid channel"}`
        ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            _, _ = io.WriteString(w, failResponse)
        }))
        defer ts.Close()

        adapter := NewRocketChatAdapter("test-rocketchat", ts.URL, "fake-token", "fake-user-id", "#general")

        msg := contract.MessageOut{
            Content: "test message",
        }

        _, err := adapter.Deliver(context.Background(), msg)
        if err == nil {
            t.Fatal("expected error for success:false, got nil")
        }
    })

    t.Run("missing auth token returns error before request", func(t *testing.T) {
        adapter := NewRocketChatAdapter("test-rocketchat", "http://localhost", "", "fake-user-id", "#general")

        msg := contract.MessageOut{
            Content: "test message",
        }

        _, err := adapter.Deliver(context.Background(), msg)
        if err == nil {
            t.Fatal("expected error for missing auth token, got nil")
        }
    })
}

func TestRocketChatAdapter_Redaction(t *testing.T) {
    // Simulate a server that echoes the token back in an error response
    token := "super-secret-token-123"
    userID := "user-id-456"

    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Maliciously include the token in the response body
        w.WriteHeader(http.StatusInternalServerError)
        _, _ = io.WriteString(w, `{"error": "unauthorized token: `+token+`"}`)
    }))
    defer ts.Close()

    adapter := NewRocketChatAdapter("test-rocketchat", ts.URL, token, userID, "#general")

    msg := contract.MessageOut{
        Content: "test message",
    }

    _, err := adapter.Deliver(context.Background(), msg)
    if err == nil {
        t.Fatal("expected an error, got nil")
    }

    errStr := err.Error()

    // Assert the actual token is NOT in the error string
    if strings.Contains(errStr, token) {
        t.Errorf("expected token to be redacted from error, but found it: %s", errStr)
    }

    // Assert the actual user ID is NOT in the error string
    if strings.Contains(errStr, userID) {
        t.Errorf("expected user ID to be redacted from error, but found it: %s", errStr)
    }

    // Assert the redaction placeholder IS in the error string
    if !strings.Contains(errStr, "<redacted>") {
        t.Errorf("expected '<redacted>' placeholder in error, got: %s", errStr)
    }
}
