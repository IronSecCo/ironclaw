# Channel adapters & their credentials

IronClaw delivers an agent's replies through **channel adapters** in
[`internal/host/channels/`](https://github.com/IronSecCo/ironclaw/tree/main/internal/host/channels). Each adapter implements a
tiny interface (`Name()` + `Deliver()`) and talks to one platform. This page is the
single reference for **what each adapter needs to be configured**.

Two things to know up front:

- **Secrets are host-only.** Every token/password below is held by the
  control-plane and never enters a sandbox. Adapters redact their own credential
  from any error string, so a token can't leak into logs.
- **How an adapter is activated.** Some adapters **auto-register from environment
  variables** when the control-plane starts (the daemon reads the var and registers
  the adapter if it's set). The rest are implemented and ready, but are wired
  programmatically / via the registry rather than from a single env var today — the
  "Auto-registered from env" column says which is which.

## Reference

| Adapter | Credentials / config it needs | Auto-registered from env | Where it comes from |
|---|---|---|---|
| **Slack** | Bot token | `SLACK_BOT_TOKEN` | A Slack app → *OAuth & Permissions* → **Bot User OAuth Token** (`xoxb-…`); needs the `chat:write` scope. |
| **Discord** | Bot token | `DISCORD_BOT_TOKEN` | [Discord Developer Portal](https://discord.com/developers/applications) → your app → *Bot* → **Token**; invite the bot to the server with the *Send Messages* permission. |
| **Telegram** | Bot token | `TELEGRAM_BOT_TOKEN` | Talk to [@BotFather](https://t.me/BotFather) → `/newbot` → the **HTTP API token**. |
| **Microsoft Teams** | Incoming Webhook URL | `IRONCLAW_TEAMS_WEBHOOK_URL` | A Teams channel → *Connectors* / *Workflows* → **Incoming Webhook** → copy the generated URL. |
| **Signal** | signal-cli REST base URL + sender number | `IRONCLAW_SIGNAL_CLI_URL`, `IRONCLAW_SIGNAL_NUMBER` | Run [`signal-cli-rest-api`](https://github.com/bbernhard/signal-cli-rest-api) and register the sender number; point the URL at it (e.g. `http://127.0.0.1:8080`). |
| **iMessage** | (none — macOS Messages bridge) | `IRONCLAW_IMESSAGE_ENABLE=1` *(macOS host only)* | Runs on a macOS host signed in to Messages; no token. Enabled only when the var is `1` and `GOOS=darwin`. |
| **Webhook** | A POST URL | — *(wired explicitly)* | Any HTTPS endpoint that accepts a JSON `POST` of the outbound message — your own service, an automation platform, etc. |
| **WhatsApp** | Cloud API access token + phone-number id | — *(wired explicitly)* | [Meta for Developers](https://developers.facebook.com/) → WhatsApp **Cloud API**: a system-user access token and the sender's **phone-number id**. |
| **Email / SMTP** | SMTP host, port (default `587`), username, password, from-address | — *(wired explicitly)* | Any SMTP submission server (PLAIN auth over STARTTLS) — e.g. `smtp.gmail.com:587` with an app password. |
| **Matrix** | Homeserver URL + access token | — *(wired explicitly)* | Your Matrix account: the homeserver base URL (e.g. `https://matrix.example.org`) and an **access token** for the bot user. |
| **Google Chat** | OAuth2 / service-account access token | — *(wired explicitly)* | A Google Cloud **service account** with the Chat API enabled; the adapter sends the bearer access token. |

> The in-product **web chat** playground (`webchat`) is registered automatically and
> needs no credential — it's the browser-based chat surface, not an external platform.

## Configuring an auto-registered adapter

Set the variable in the control-plane's environment (or in `.env` when running with
[Docker Compose](https://github.com/IronSecCo/ironclaw/blob/main/docker-compose.yml)), then start the daemon:

```sh
export DISCORD_BOT_TOKEN=...      # registers the Discord adapter on boot
# the daemon logs: channel adapter registered  adapter=discord
```

Then wire it to an agent group with the registry — see
[`examples/`](https://github.com/IronSecCo/ironclaw/tree/main/examples) for runnable templates and the
[CLI reference](https://github.com/IronSecCo/ironclaw/blob/main/README.md) for `ironctl registry`.

## Adding a new adapter

Want a platform that isn't here yet? See
[**Writing a channel adapter**](writing-a-channel-adapter.md) for the interface, the
house pattern (stdlib HTTP, test-overridable `BaseURL`, secret redaction, threading),
and the test pattern.
