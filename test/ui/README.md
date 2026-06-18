# Web-console browser tests (`test/ui/`)

Real UI-interaction tests for the IronClaw console, driven with
[Playwright](https://playwright.dev). They click the actual SPA in a browser —
navigate every panel, create an agent through the form, approve/reject changes,
and send a chat message a real launched sandbox answers — asserting on the DOM
and failing on any console error.

This is an **optional dev harness**. It is NOT part of the Go module, the
published binaries, or the embedded console (which stays dependency-free). It
uses your **system Google Chrome** (`channel: "chrome"`), so no Playwright
browser download is needed.

## Run

Point it at a control-plane already serving `/ui/` (e.g. `--dev` on
`127.0.0.1:8788`; agent chat requires `--runtime docker` + a model gateway):

```sh
cd test/ui
npm install                 # installs @playwright/test only (no browser download)
npx playwright test         # runs against http://127.0.0.1:8788 by default
# or target another instance:
IRONCLAW_UI_URL=http://127.0.0.1:8789 npx playwright test
```

## Chat tests

There are two chat tests, both driving a real launched sandbox container:

- **`chat (mock-agent)`** runs by default. It uses the seeded `mock-agent`
  (the offline `mock` provider), so it needs only `--runtime docker` — no model
  credential — and never flakes on an upstream token. It proves the full
  pipeline: UI → gateway → encrypted inbound queue → sandbox → provider →
  encrypted outbound queue → UI.
- **`chat (dev-agent, live model)`** is opt-in. It exercises the real model
  path (sandbox → host proxy → provider, e.g. Codex via OneCLI), which depends
  on a credential that can expire/be revoked. Enable it with a working token:

  ```sh
  IRONCLAW_LIVE_MODEL=1 npx playwright test
  ```

Every other test needs only the API + console.
