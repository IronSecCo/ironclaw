package channels

import (
	"context"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

var _ Adapter = (*IMessageAdapter)(nil)

func TestIMessageDeliversViaOsascript(t *testing.T) {
	var gotScript string
	a := NewIMessageAdapter("imessage")
	a.goos = "darwin" // pretend we're on macOS regardless of the test host
	a.runOsascript = func(ctx context.Context, script string) (string, error) {
		gotScript = script
		return "", nil
	}
	id, err := a.Deliver(context.Background(), contract.MessageOut{Content: "hello", PlatformID: strptr("+15551112222")})
	if err != nil {
		t.Fatal(err)
	}
	if id != "delivered" {
		t.Errorf("id = %q, want delivered", id)
	}
	if !strings.Contains(gotScript, "+15551112222") || !strings.Contains(gotScript, "hello") {
		t.Errorf("script missing recipient/text: %q", gotScript)
	}
	if !strings.Contains(gotScript, `tell application "Messages"`) {
		t.Errorf("script is not AppleScript for Messages: %q", gotScript)
	}
}

func TestIMessageRefusesOffMacOS(t *testing.T) {
	a := NewIMessageAdapter("imessage")
	a.goos = "linux"
	a.runOsascript = func(context.Context, string) (string, error) { return "", nil }
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("+1")}); err == nil {
		t.Fatal("expected an error off macOS")
	}
}

func TestIMessageRequiresRecipientAndContent(t *testing.T) {
	a := NewIMessageAdapter("imessage")
	a.goos = "darwin"
	a.runOsascript = func(context.Context, string) (string, error) { return "", nil }
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x"}); err == nil {
		t.Error("expected an error with no recipient")
	}
	if _, err := a.Deliver(context.Background(), contract.MessageOut{Content: "  ", PlatformID: strptr("+1")}); err == nil {
		t.Error("expected an error with empty content")
	}
}

// TestIMessageEscapesScript: a quote or backslash in the message must not break
// out of the AppleScript string literal.
func TestIMessageEscapesScript(t *testing.T) {
	script := buildIMessageScript(`bob"; do shell script "rm -rf /`, `he said "hi" \ bye`)
	// The raw, unescaped injection sequence must not appear; escaped forms must.
	if strings.Contains(script, `"; do shell script "`) {
		t.Errorf("unescaped quote allowed injection: %q", script)
	}
	if !strings.Contains(script, `\"`) || !strings.Contains(script, `\\`) {
		t.Errorf("expected escaped quote and backslash in: %q", script)
	}
}
