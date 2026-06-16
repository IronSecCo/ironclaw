// OWNER: AGENT1

package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestWebhookAdapterDelivers(t *testing.T) {
	var gotCT string
	var gotBody contract.MessageOut
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"platform-123"}`)
	}))
	defer srv.Close()

	a := NewWebhookAdapter("hook", srv.URL)
	id, err := a.Deliver(context.Background(), contract.MessageOut{ID: "m1", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "platform-123" {
		t.Fatalf("id = %q, want platform-123 (from response body)", id)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody.Content != "hello" {
		t.Errorf("upstream got content %q", gotBody.Content)
	}
}

func TestWebhookAdapterErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	a := NewWebhookAdapter("hook", srv.URL)
	if _, err := a.Deliver(context.Background(), contract.MessageOut{ID: "m1"}); err == nil {
		t.Fatal("expected error on 502, got nil")
	}
}

func TestWebhookAdapterFallsBackToMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // no id in body or header
	}))
	defer srv.Close()

	a := NewWebhookAdapter("hook", srv.URL)
	id, err := a.Deliver(context.Background(), contract.MessageOut{ID: "m-fallback"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "m-fallback" {
		t.Fatalf("id = %q, want fallback to message id", id)
	}
}

// WebhookAdapter must satisfy the Adapter interface.
var _ Adapter = (*WebhookAdapter)(nil)
