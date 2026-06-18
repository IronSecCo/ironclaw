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

var _ Adapter = (*SignalAdapter)(nil)

func TestSignalAdapterDelivers(t *testing.T) {
	var gotPath string
	var gotBody signalSend
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"timestamp":1700000000123}`)
	}))
	defer srv.Close()

	a := NewSignalAdapter("signal", srv.URL, "+15550000000")
	id, err := a.Deliver(context.Background(), contract.MessageOut{Content: "hi signal", PlatformID: strptr("+15551112222")})
	if err != nil {
		t.Fatal(err)
	}
	if id != "1700000000123" {
		t.Errorf("id = %q, want the bridge timestamp", id)
	}
	if gotPath != "/v2/send" {
		t.Errorf("path = %q, want /v2/send", gotPath)
	}
	if gotBody.Message != "hi signal" || gotBody.Number != "+15550000000" ||
		len(gotBody.Recipients) != 1 || gotBody.Recipients[0] != "+15551112222" {
		t.Errorf("upstream body = %+v", gotBody)
	}
}

func TestSignalAdapterRequiresConfig(t *testing.T) {
	if _, err := NewSignalAdapter("signal", "", "+1").Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("+2")}); err == nil {
		t.Error("expected an error with no bridge URL")
	}
	if _, err := NewSignalAdapter("signal", "http://x", "").Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("+2")}); err == nil {
		t.Error("expected an error with no sender number")
	}
	if _, err := NewSignalAdapter("signal", "http://x", "+1").Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Error("expected an error with no recipient")
	}
}

func TestSignalAdapterErrorsOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error":"invalid recipient"}`)
	}))
	defer srv.Close()
	a := NewSignalAdapter("signal", srv.URL, "+15550000000")
	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("+1")})
	if err == nil || !strings.Contains(err.Error(), "invalid recipient") {
		t.Fatalf("expected an HTTP-error carrying the bridge message, got %v", err)
	}
}

// TestSignalAdapterRedactsNumber: the sender number must not leak into errors.
func TestSignalAdapterRedactsNumber(t *testing.T) {
	const number = "+15550009999"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "rejected sender "+number)
	}))
	defer srv.Close()
	a := NewSignalAdapter("signal", srv.URL, number)
	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("+1")})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), number) {
		t.Fatalf("sender number leaked into error: %v", err)
	}
}
