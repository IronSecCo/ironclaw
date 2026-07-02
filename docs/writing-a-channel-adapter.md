---
title: Writing a custom channel adapter
description: Build a custom IronClaw channel adapter to deliver agent messages to Slack, Discord, a webhook, or any platform. Learn the interface, the house pattern, and how adapters stay small and isolated.
---

# Writing a channel adapter

A **channel adapter** delivers an agent's outbound messages to one platform (Slack,
Discord, a webhook, …). They live in
[`internal/host/channels/`](https://github.com/IronSecCo/ironclaw/tree/main/internal/host/channels) and are deliberately small and
uniform. This guide is the house pattern; the existing adapters
(`slack.go`, `discord.go`, `whatsapp.go`, `matrix.go` are the cleanest templates) are
your reference implementations.

## The interface

An adapter is anything that satisfies:

```go
type Adapter interface {
	Name() string
	Deliver(ctx context.Context, msg contract.MessageOut) (string, error)
}
```

- **`Name()`** — a stable adapter name (e.g. `"slack"`).
- **`Deliver()`** — send `msg` to the platform and return the platform's **message
  id** (used for threading and delivery dedup), or an error.

The fields of `contract.MessageOut` you'll typically use:

| Field | Use |
|---|---|
| `Content` | the message text to send |
| `PlatformID` | the destination on the platform (channel id, chat id, recipient) |
| `ThreadID` | the platform's thread key, if the reply should land in a thread |
| `ID` / `InReplyTo` | correlation ids if you need them |

## The house pattern

Every adapter follows the same shape. Copy `slack.go` and adapt:

```go
// defaultFooBaseURL is the platform API host. Overridable (BaseURL) so tests can
// point at an httptest server.
const defaultFooBaseURL = "https://api.foo.example"

type FooAdapter struct {
	AdapterName string
	Token       string        // the credential — held host-side, never logged
	BaseURL     string        // defaults to defaultFooBaseURL; overridable for tests
	Client      *http.Client
}

func NewFooAdapter(name, token string) *FooAdapter {
	if name == "" {
		name = "foo"
	}
	return &FooAdapter{
		AdapterName: name,
		Token:       token,
		BaseURL:     defaultFooBaseURL,
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *FooAdapter) Name() string { return a.AdapterName }
```

`Deliver` then, in order:

1. **Validate** the credential is set and the message has a destination
   (`msg.PlatformID`); return a clear error otherwise.
2. **Build the platform payload** from `msg.Content`. If the platform threads, map a
   non-empty `msg.ThreadID` to its thread key.
3. **POST with the standard library** (`net/http`), using `http.NewRequestWithContext`
   so cancellation works. Put the token in an **`Authorization` header — never in the
   URL**.
4. **Cap the response read** (`io.LimitReader`) and parse the platform's result.
5. **Return the platform message id** on success.

### Five rules that keep adapters safe and testable

- **Standard library only.** Use `net/http` + `encoding/json`. No SDKs, no new
  dependencies (a new dependency is a deliberate, separately-reviewed change).
- **`BaseURL` is overridable.** Default to the const, but let a test set
  `a.BaseURL = srv.URL`. This is how every adapter is tested without network access.
- **Redact the credential from every error.** Never interpolate the token into an
  error string. Adapters keep a tiny helper and run any error text through it:

  ```go
  func (a *FooAdapter) redact(s string) string {
  	if a.Token == "" {
  		return s
  	}
  	return strings.ReplaceAll(s, a.Token, "<redacted>")
  }
  // ... return fmt.Errorf("host/channels: foo POST failed: %s", a.redact(err.Error()))
  ```

- **Thread when you can.** If the platform supports threads, pass a non-empty
  `msg.ThreadID` through so replies land in-thread.
- **Return the real message id.** It feeds threading and the delivery loop's dedup.

## Registering it

Adapters are activated in `registerChannelAdapters` in
[`cmd/controlplane/main.go`](https://github.com/IronSecCo/ironclaw/blob/main/cmd/controlplane/main.go). For a single-token adapter,
add it to the env-gated `specs` slice:

```go
{"foo", "FOO_BOT_TOKEN", func(n, t string) channels.Adapter { return channels.NewFooAdapter(n, t) }},
```

For richer config (a URL + a number, a webhook, etc.) follow the `reqExtra(...)`
pattern used by Teams / Signal / iMessage. `cmd/controlplane/main.go` is a shared
entrypoint, so keep the edit minimal and well-scoped. Then add a row to
[`docs/channels.md`](channels.md) documenting the new credential.

## Testing it

Every adapter has an `httptest`-backed unit test (see `slack_test.go`). The pattern:

```go
func TestFooAdapterDelivers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// assert path / Authorization header / JSON body here
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"123"}`)
	}))
	defer srv.Close()

	a := NewFooAdapter("foo", "TESTTOKEN")
	a.BaseURL = srv.URL

	id, err := a.Deliver(context.Background(), contract.MessageOut{
		ID: "m1", Content: "hello", PlatformID: strptr("C123"),
	})
	// assert id, err, and that the returned message id matches the platform's
}
```

Also assert the **interface conformance** (`var _ Adapter = (*FooAdapter)(nil)`) and
that a transport error **does not leak the token**. Run the suite with:

```sh
CGO_ENABLED=1 go test ./internal/host/channels/...
```

That's it — small, dependency-free, tested, and secret-safe, like every adapter
already in the tree.
