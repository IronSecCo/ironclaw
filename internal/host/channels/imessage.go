// OWNER: T-232

package channels

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// IMessageAdapter delivers an outbound message via Apple Messages on a macOS
// host, by driving Messages.app through AppleScript (`osascript`). It is the only
// adapter that bridges a LOCAL app rather than an HTTP API, so it runs only when
// the control-plane host is macOS with Messages signed in. The recipient (a phone
// number or Apple ID) is taken from MessageOut.PlatformID.
//
// There is no network token; the trust surface is the local Messages account.
type IMessageAdapter struct {
	AdapterName string
	// runOsascript runs the AppleScript and returns combined output; overridable in
	// tests so delivery logic can be exercised without sending a real iMessage.
	runOsascript func(ctx context.Context, script string) (string, error)
	// goos is the platform guard; defaults to runtime.GOOS, overridable in tests.
	goos string
}

// NewIMessageAdapter constructs an IMessageAdapter. name defaults to "imessage".
func NewIMessageAdapter(name string) *IMessageAdapter {
	if name == "" {
		name = "imessage"
	}
	return &IMessageAdapter{AdapterName: name, runOsascript: defaultRunOsascript, goos: runtime.GOOS}
}

// Name returns the adapter name.
func (a *IMessageAdapter) Name() string { return a.AdapterName }

// Deliver sends msg.Content to the recipient in msg.PlatformID via Messages.app.
// It fails fast off macOS — the bridge cannot work without Messages.app.
func (a *IMessageAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	goos := a.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "darwin" {
		return "", fmt.Errorf("host/channels: imessage %q requires a macOS host (got %s)", a.AdapterName, goos)
	}
	recipient := ""
	if msg.PlatformID != nil {
		recipient = strings.TrimSpace(*msg.PlatformID)
	}
	if recipient == "" {
		return "", fmt.Errorf("host/channels: imessage %q message has no recipient (PlatformID)", a.AdapterName)
	}
	if strings.TrimSpace(msg.Content) == "" {
		return "", fmt.Errorf("host/channels: imessage %q message has empty content", a.AdapterName)
	}

	run := a.runOsascript
	if run == nil {
		run = defaultRunOsascript
	}
	if _, err := run(ctx, buildIMessageScript(recipient, msg.Content)); err != nil {
		return "", fmt.Errorf("host/channels: imessage %q osascript failed: %v", a.AdapterName, err)
	}
	// Messages.app does not return a retrievable message id from AppleScript.
	return "delivered", nil
}

// buildIMessageScript renders the AppleScript that sends text to recipient via
// the iMessage service. Both inputs are escaped for an AppleScript string literal
// so a quote or backslash in the message cannot break out of the script.
func buildIMessageScript(recipient, text string) string {
	return strings.Join([]string{
		`tell application "Messages"`,
		`set targetService to 1st account whose service type = iMessage`,
		fmt.Sprintf(`set targetBuddy to participant "%s" of targetService`, escapeAppleScript(recipient)),
		fmt.Sprintf(`send "%s" to targetBuddy`, escapeAppleScript(text)),
		`end tell`,
	}, "\n")
}

// escapeAppleScript escapes a Go string for embedding in an AppleScript double-
// quoted literal: backslash first, then the double quote.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// defaultRunOsascript executes the script via the osascript binary.
func defaultRunOsascript(ctx context.Context, script string) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
