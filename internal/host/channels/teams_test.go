// OWNER: T-232

package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

var _ Adapter = (*TeamsAdapter)(nil)

func TestTeamsAdapterDelivers(t *testing.T) {
	var gotBody teamsMessage
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, "1")
	}))
	defer srv.Close()

	a := NewTeamsAdapter("teams", srv.URL)
	id, err := a.Deliver(context.Background(), contract.MessageOut{ID: "m1", Content: "hello teams"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "1" {
		t.Errorf("id = %q, want the webhook response body", id)
	}
	if gotBody.Text != "hello teams" {
		t.Errorf("upstream body = %+v", gotBody)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type = %q", gotCT)
	}
}

func TestTeamsAdapterErrorsOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "Bad payload")
	}))
	defer srv.Close()

	a := NewTeamsAdapter("teams", srv.URL)
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Fatal("expected an error on HTTP 400")
	}
}

func TestTeamsAdapterRequiresWebhookAndContent(t *testing.T) {
	if _, err := NewTeamsAdapter("teams", "").Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Error("expected an error with no webhook URL")
	}
	if _, err := NewTeamsAdapter("teams", "https://example.com/hook").Deliver(context.Background(), contract.MessageOut{Content: "  "}); err == nil {
		t.Error("expected an error with empty content")
	}
}

// TestTeamsAdapterRedactsWebhook: the secret webhook URL must never appear in an
// error (it embeds a token).
func TestTeamsAdapterRedactsWebhook(t *testing.T) {
	secret := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// Reflect the request URL (the secret) into the error body.
		_, _ = io.WriteString(w, "error for "+secret)
	}))
	defer srv.Close()
	secret = srv.URL + "/webhook/SECRET-TOKEN"

	a := NewTeamsAdapter("teams", secret)
	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("webhook secret leaked into error: %v", err)
	}
}
