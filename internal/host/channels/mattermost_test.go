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

// MattermostAdapter must satisfy the Adapter interface.
var _ Adapter = (*MattermostAdapter)(nil)

func TestMattermostAdapterDelivers(t *testing.T) {
	var gotMethod string
	var gotContentType string
	var gotBody mattermostMessage

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")

		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)

		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	a := NewMattermostAdapter("mattermost", srv.URL)

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID:      "m1",
		Content: "hello mattermost",
	})
	if err != nil {
		t.Fatal(err)
	}

	if id != "ok" {
		t.Fatalf("id = %q, want ok", id)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("content-type = %q, want application/json", gotContentType)
	}
	if gotBody.Text != "hello mattermost" {
		t.Fatalf("body text = %q, want hello mattermost", gotBody.Text)
	}
}

func TestMattermostAdapterPostsExpectedBody(t *testing.T) {
	var gotRawBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotRawBody = string(b)

		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	a := NewMattermostAdapter("mattermost", srv.URL)

	_, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "incident update",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := `{"text":"incident update"}`
	if gotRawBody != want {
		t.Fatalf("body = %q, want %q", gotRawBody, want)
	}
}

func TestMattermostAdapterErrorsOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "invalid webhook")
	}))
	defer srv.Close()

	a := NewMattermostAdapter("mattermost", srv.URL)

	_, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hello",
	})
	if err == nil {
		t.Fatal("expected an error on a non-2xx response")
	}
	if !strings.Contains(err.Error(), "invalid webhook") {
		t.Fatalf("error should include upstream message, got %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error should include HTTP status, got %v", err)
	}
}

func TestMattermostAdapterRequiresWebhookURL(t *testing.T) {
	a := NewMattermostAdapter("mattermost", "")

	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hello",
	}); err == nil {
		t.Fatal("expected an error with no webhook url")
	}
}

func TestMattermostAdapterRedactsWebhookURL(t *testing.T) {
	const secretPath = "/hooks/SUPERSECRET"
	var webhookURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "invalid webhook "+webhookURL)
	}))
	defer srv.Close()

	webhookURL = srv.URL + secretPath

	a := NewMattermostAdapter("mattermost", webhookURL)

	_, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hello",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), webhookURL) {
		t.Fatalf("webhook url leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("expected webhook url to be redacted, got %v", err)
	}
}

//test for empty content + empty body fallback

func TestMattermostAdapterRequiresContent(t *testing.T) {
	a := NewMattermostAdapter("mattermost", "https://mattermost.example/hooks/test")

	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: " ",
	}); err == nil {
		t.Fatal("expected an error with empty content")
	}
}

func TestMattermostAdapterUsesFallbackIDOnEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := NewMattermostAdapter("mattermost", srv.URL)

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "delivered" {
		t.Fatalf("id = %q, want delivered", id)
	}
}