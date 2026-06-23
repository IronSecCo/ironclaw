---
title: Write a custom channel adapter
description: Build, register, and test a new channel adapter end to end — a complete, working Pushover example.
---

# Write a custom channel adapter

IronClaw ships adapters for Slack, Discord, Telegram, WhatsApp, email, Matrix, and more. When your
platform isn't in that list, you add it — and adapters are deliberately small. This tutorial builds
a **complete, working adapter from scratch**: a [Pushover](https://pushover.net) notifier that
delivers an agent's replies as push notifications to your phone.

You'll write the adapter, register it, test it without touching the network, and wire it to an
agent — the same shape every built-in adapter follows. For the reference version of the house
pattern, keep [Writing a channel adapter](../writing-a-channel-adapter.md) open alongside this page.

## What an adapter is

A channel adapter delivers an agent's **outbound** messages to one platform. It satisfies a tiny
interface, lives in
[`internal/host/channels/`](https://github.com/IronSecCo/ironclaw/tree/main/internal/host/channels),
and uses **only the standard library** — no SDKs, no new dependencies.

```go
type Adapter interface {
	Name() string
	Deliver(ctx context.Context, msg contract.MessageOut) (string, error)
}
```

- **`Name()`** returns a stable adapter name (e.g. `"pushover"`).
- **`Deliver()`** sends `msg` to the platform and returns the platform's **message id** (used for
  threading and delivery dedup), or an error.

The `contract.MessageOut` fields you'll use here:

| Field | Use |
| --- | --- |
| `Content` | the message text to send |
| `PlatformID` | the destination on the platform — for Pushover, the recipient **user/group key** (a `*string`) |
| `ThreadID` | the platform's thread key, if the reply should thread (Pushover doesn't thread, so we ignore it) |

## What we're building

Pushover's send API is a single `POST` to `https://api.pushover.net/1/messages.json` with three
fields: your **application token** (the credential), the recipient **user key** (the destination),
and the **message**. On success it returns `{"status":1,"request":"<id>"}`. We map:

- **credential** → the Pushover **application token**, held host-side, supplied via an env var;
- **`msg.PlatformID`** → the recipient **user key**;
- **`msg.Content`** → the notification text;
- **returned message id** → Pushover's `request` id.

## 1. Scaffold the adapter

Create `internal/host/channels/pushover.go`. Start with the type and constructor — copy the shape
from `slack.go` and adapt:

```go
package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// defaultPushoverBaseURL is the Pushover API host. Overridable (BaseURL) so tests
// can point at an httptest server.
const defaultPushoverBaseURL = "https://api.pushover.net"

// PushoverAdapter delivers an agent's replies as Pushover push notifications.
type PushoverAdapter struct {
	AdapterName string
	Token       string       // the Pushover application token — held host-side, never logged
	BaseURL     string       // defaults to defaultPushoverBaseURL; overridable for tests
	Client      *http.Client
}

// NewPushoverAdapter builds an adapter from a name and the application token.
func NewPushoverAdapter(name, token string) *PushoverAdapter {
	if name == "" {
		name = "pushover"
	}
	return &PushoverAdapter{
		AdapterName: name,
		Token:       token,
		BaseURL:     defaultPushoverBaseURL,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *PushoverAdapter) Name() string { return a.AdapterName }

// Compile-time check that we satisfy the interface.
var _ Adapter = (*PushoverAdapter)(nil)
```

## 2. Implement `Deliver`

`Deliver` validates its inputs, builds the platform payload, POSTs with the standard library, caps
the response read, and returns the platform's message id. Add it to the same file:

```go
func (a *PushoverAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	// 1) Validate the credential and the destination.
	if a.Token == "" {
		return "", fmt.Errorf("host/channels: pushover application token not set")
	}
	if msg.PlatformID == nil || *msg.PlatformID == "" {
		return "", fmt.Errorf("host/channels: pushover delivery requires a recipient user key (PlatformID)")
	}

	// 2) Build the platform payload. Pushover takes form fields; the token goes in
	//    the body, never in the URL.
	form := url.Values{}
	form.Set("token", a.Token)
	form.Set("user", *msg.PlatformID)
	form.Set("message", msg.Content)

	// 3) POST with the standard library, honoring context cancellation.
	endpoint := a.BaseURL + "/1/messages.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("host/channels: pushover request: %s", a.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("host/channels: pushover POST failed: %s", a.redact(err.Error()))
	}
	defer resp.Body.Close()

	// 4) Cap the response read and parse the result.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var out struct {
		Status  int      `json:"status"`
		Request string   `json:"request"`
		Errors  []string `json:"errors"`
	}
	_ = json.Unmarshal(body, &out)

	if resp.StatusCode != http.StatusOK || out.Status != 1 {
		if len(out.Errors) > 0 {
			return "", fmt.Errorf("host/channels: pushover rejected message: %s", a.redact(strings.Join(out.Errors, "; ")))
		}
		return "", fmt.Errorf("host/channels: pushover returned status %d", resp.StatusCode)
	}

	// 5) Return the platform message id (Pushover's request id).
	return out.Request, nil
}

// redact strips the credential from any string we might log or return as an error.
func (a *PushoverAdapter) redact(s string) string {
	if a.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, a.Token, "<redacted>")
}
```

Note the five rules from the [reference](../writing-a-channel-adapter.md#five-rules-that-keep-adapters-safe-and-testable)
in action: standard library only, `BaseURL` overridable, the credential redacted from every error,
and the real message id returned. (Pushover doesn't thread, so we skip `ThreadID` — your platform
may not.)

## 3. Register it

Single-token adapters register from an environment variable. Add one line to the `specs` slice in
`registerChannelAdapters` in
[`cmd/controlplane/main.go`](https://github.com/IronSecCo/ironclaw/blob/main/cmd/controlplane/main.go):

```go
specs := []adapterSpec{
	{"slack", "SLACK_BOT_TOKEN", func(n, t string) channels.Adapter { return channels.NewSlackAdapter(n, t) }},
	{"discord", "DISCORD_BOT_TOKEN", func(n, t string) channels.Adapter { return channels.NewDiscordAdapter(n, t) }},
	{"telegram", "TELEGRAM_BOT_TOKEN", func(n, t string) channels.Adapter { return channels.NewTelegramAdapter(n, t) }},
	{"pushover", "PUSHOVER_APP_TOKEN", func(n, t string) channels.Adapter { return channels.NewPushoverAdapter(n, t) }}, // <-- add
}
```

Now the daemon auto-registers the adapter on boot whenever `PUSHOVER_APP_TOKEN` is set, and logs
`channel adapter registered  adapter=pushover`.

!!! info "Richer config than a single token?"
    If your platform needs more than one value (a URL **and** a number, a webhook, etc.), don't use
    the `specs` slice — follow the explicit `reqExtra(...)` pattern that Teams, Signal, and iMessage
    use in the same function.

## 4. Test it — no network required

Every adapter has an `httptest`-backed unit test. Create
`internal/host/channels/pushover_test.go`:

```go
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

func strptr(s string) *string { return &s }

func TestPushoverAdapterDelivers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/messages.json" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = r.ParseForm()
		if got := r.Form.Get("message"); got != "hello" {
			t.Errorf("message = %q, want hello", got)
		}
		if got := r.Form.Get("user"); got != "uKEY" {
			t.Errorf("user = %q, want uKEY", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":1,"request":"req-123"}`)
	}))
	defer srv.Close()

	a := NewPushoverAdapter("pushover", "APP-TOKEN")
	a.BaseURL = srv.URL

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "hello", PlatformID: strptr("uKEY"),
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if id != "req-123" {
		t.Errorf("message id = %q, want req-123", id)
	}
}

// A transport error must not leak the token.
func TestPushoverAdapterRedactsToken(t *testing.T) {
	a := NewPushoverAdapter("pushover", "SECRET-TOKEN")
	a.BaseURL = "http://127.0.0.1:0" // unroutable → forces a transport error
	_, err := a.Deliver(context.Background(), contract.MessageOut{Content: "x", PlatformID: strptr("uKEY")})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Errorf("error leaked the token: %v", err)
	}
}
```

Run the suite:

```sh
CGO_ENABLED=1 go test ./internal/host/channels/...
```

Both tests exercise the adapter **without any network access** — the success path against a fake
Pushover server, and the redaction guarantee against a forced transport error.

## 5. Wire it to an agent and try it for real

Build, set your real Pushover application token, and start the daemon:

```sh
CGO_ENABLED=1 go build -o bin/ ./cmd/controlplane ./cmd/ironctl
export PUSHOVER_APP_TOKEN=your-pushover-app-token   # held host-side; never enters a sandbox
./bin/controlplane --dev --api-addr 127.0.0.1:8787
```

Then add a delivery destination so an agent can post to your Pushover **user key** (the same
`ironctl registry` flow as any channel — see [Connect IronClaw to Slack](connect-slack.md)):

```sh
ironctl registry destination add --agent default --channel pushover --platform uYOURUSERKEY
```

When that agent replies, the adapter delivers the message as a push notification to your device.

## Recap

You built a complete adapter that is **small, dependency-free, tested, and secret-safe** — the same
shape as every adapter already in the tree:

- one file implementing `Name()` + `Deliver()`,
- the credential held host-side and **redacted from every error**,
- a one-line registration in `registerChannelAdapters`,
- an `httptest`-backed unit test that needs no network,
- and a row you should add to [Channel adapters](../channels.md) documenting the new credential.

## Next steps

- **Reference:** [Writing a channel adapter](../writing-a-channel-adapter.md) — the house pattern in
  brief, with the rules spelled out.
- **The other adapters:** the cleanest templates to crib from are `slack.go`, `discord.go`,
  `whatsapp.go`, and `matrix.go` in
  [`internal/host/channels/`](https://github.com/IronSecCo/ironclaw/tree/main/internal/host/channels).
- **Document your credential:** add a row to [Channel adapters](../channels.md) so operators know
  what your adapter needs.
