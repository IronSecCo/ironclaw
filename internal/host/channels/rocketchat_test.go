package channels

import (
    "context"
    "io"
    "net/http"
    "net/http/httptest"
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
            // Write the raw JSON string instead of marshaling a struct
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
            w.WriteHeader(http.StatusOK) // 200 OK but success is false
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
