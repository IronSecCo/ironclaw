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

// WhatsAppAdapter must satisfy the Adapter interface.
var _ Adapter = (*WhatsAppAdapter)(nil)

func TestWhatsAppAdapterDelivers(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody waMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"messaging_product":"whatsapp","messages":[{"id":"wamid.TEST123"}]}`)
	}))
	defer srv.Close()

	a := NewWhatsAppAdapter("whatsapp", "TESTTOKEN", "PHONE777")
	a.BaseURL = srv.URL

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "hello there", PlatformID: strptr("15551234567"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "wamid.TEST123" {
		t.Fatalf("id = %q, want the returned wamid", id)
	}
	if gotPath != "/PHONE777/messages" {
		t.Fatalf("request path = %q, want /PHONE777/messages", gotPath)
	}
	if gotAuth != "Bearer TESTTOKEN" {
		t.Fatalf("auth header = %q, want Bearer TESTTOKEN", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody.MessagingProduct != "whatsapp" || gotBody.To != "15551234567" || gotBody.Type != "text" {
		t.Errorf("upstream body envelope = %+v", gotBody)
	}
	if gotBody.Text == nil || gotBody.Text.Body != "hello there" {
		t.Errorf("upstream text = %+v", gotBody.Text)
	}
	if gotBody.Context != nil {
		t.Errorf("no reply context expected, got %+v", gotBody.Context)
	}
}

func TestWhatsAppAdapterMapsReplyContext(t *testing.T) {
	var gotBody waMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"messages":[{"id":"wamid.X"}]}`)
	}))
	defer srv.Close()

	a := NewWhatsAppAdapter("whatsapp", "TESTTOKEN", "PHONE1")
	a.BaseURL = srv.URL

	// A non-empty thread id is sent through as the reply context message_id.
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("15551234567"), ThreadID: strptr("wamid.PRIOR"),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.Context == nil || gotBody.Context.MessageID != "wamid.PRIOR" {
		t.Fatalf("context = %+v, want message_id wamid.PRIOR", gotBody.Context)
	}

	// A blank/whitespace thread id is omitted (no context).
	gotBody = waMessage{}
	if _, err := a.Deliver(context.Background(), contract.MessageOut{
		Content: "hi", PlatformID: strptr("15551234567"), ThreadID: strptr("  "),
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody.Context != nil {
		t.Fatalf("blank thread id must omit context, got %+v", gotBody.Context)
	}
}

func TestWhatsAppAdapterSendsDocument(t *testing.T) {
	var gotBody waMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"messages":[{"id":"wamid.DOC"}]}`)
	}))
	defer srv.Close()

	a := NewWhatsAppAdapter("whatsapp", "TESTTOKEN", "PHONE1")
	a.BaseURL = srv.URL

	id, err := a.SendDocument(context.Background(), "15551234567",
		"https://example.com/report.pdf", "report.pdf", "Q3 report")
	if err != nil {
		t.Fatal(err)
	}
	if id != "wamid.DOC" {
		t.Fatalf("id = %q, want wamid.DOC", id)
	}
	if gotBody.Type != "document" || gotBody.Document == nil {
		t.Fatalf("expected a document message, got %+v", gotBody)
	}
	if gotBody.Document.Link != "https://example.com/report.pdf" ||
		gotBody.Document.Filename != "report.pdf" || gotBody.Document.Caption != "Q3 report" {
		t.Errorf("document body = %+v", gotBody.Document)
	}
}

func TestWhatsAppAdapterSendDocumentRequiresLinkAndRecipient(t *testing.T) {
	a := NewWhatsAppAdapter("whatsapp", "TESTTOKEN", "PHONE1")
	if _, err := a.SendDocument(context.Background(), "", "https://x/y.pdf", "", ""); err == nil {
		t.Error("expected an error with no recipient")
	}
	if _, err := a.SendDocument(context.Background(), "15551234567", "  ", "", ""); err == nil {
		t.Error("expected an error with no link")
	}
}

func TestWhatsAppAdapterErrorsOnHTTPError(t *testing.T) {
	// The Cloud API uses real HTTP status codes with a Graph error envelope.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"(#131030) Recipient not in allowed list","type":"OAuthException","code":131030}}`)
	}))
	defer srv.Close()

	a := NewWhatsAppAdapter("whatsapp", "TESTTOKEN", "PHONE1")
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("15551234567")})
	if err == nil {
		t.Fatal("expected an error on a non-2xx response")
	}
	if !strings.Contains(err.Error(), "Recipient not in allowed list") {
		t.Fatalf("error should carry the Graph API message, got %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error should carry the HTTP status, got %v", err)
	}
}

func TestWhatsAppAdapterErrorsOnNoMessageID(t *testing.T) {
	// A 200 with no messages array is still a failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"messaging_product":"whatsapp","messages":[]}`)
	}))
	defer srv.Close()

	a := NewWhatsAppAdapter("whatsapp", "TESTTOKEN", "PHONE1")
	a.BaseURL = srv.URL

	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("15551234567")}); err == nil {
		t.Fatal("expected an error when no message id is returned")
	}
}

func TestWhatsAppAdapterRequiresRecipient(t *testing.T) {
	a := NewWhatsAppAdapter("whatsapp", "TESTTOKEN", "PHONE1")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Fatal("expected an error when PlatformID (recipient) is nil")
	}
}

func TestWhatsAppAdapterRequiresToken(t *testing.T) {
	a := NewWhatsAppAdapter("whatsapp", "", "PHONE1")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("15551234567")}); err == nil {
		t.Fatal("expected an error with no access token")
	}
}

func TestWhatsAppAdapterRequiresPhoneNumberID(t *testing.T) {
	a := NewWhatsAppAdapter("whatsapp", "TESTTOKEN", "")
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("15551234567")}); err == nil {
		t.Fatal("expected an error with no phone-number id")
	}
}

// TestWhatsAppAdapterRedactsToken: even if an upstream error string were to echo
// the token, the adapter must redact it before returning.
func TestWhatsAppAdapterRedactsToken(t *testing.T) {
	const token = "EAAG-SUPERSECRET-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// Contrived upstream that reflects the token in its error message.
		_, _ = io.WriteString(w, `{"error":{"message":"invalid token `+token+`","type":"OAuthException","code":190}}`)
	}))
	defer srv.Close()

	a := NewWhatsAppAdapter("whatsapp", token, "PHONE1")
	a.BaseURL = srv.URL

	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("15551234567")})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("access token leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("expected the token to be redacted, got %v", err)
	}
}
