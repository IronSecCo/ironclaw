# IronClaw web console (`web/`)

A static, dependency-free SPA embedded into the control-plane binary via
`//go:embed` (see `embed.go`) and served at `GET /ui/`. There is **no build step**
and **no Node/npm toolchain**: the standard `go build ./...` compiles the assets in
`static/` straight into the binary, and CI builds it for free.

Architecture and security rationale: [`../.agents/spikes/web-console.md`](../.agents/spikes/web-console.md).

## Run it

```bash
CGO_ENABLED=1 go run ./cmd/controlplane --dev          # no token → console open on loopback
# then open http://127.0.0.1:8787/ui/
```

With a token set (`IRONCLAW_API_TOKEN=…`), the shell still loads (it is
auth-exempt, like `/healthz`); paste the token into the console's **Connect** box —
it is kept only in the tab's `sessionStorage` and sent as `Authorization: Bearer`
on every `/v1` call. The API itself stays fully bearer-gated.

## Add a feature page (T-221+)

1. Add a file under `static/` (e.g. `static/approvals.js`) and a nav entry +
   panel in `static/index.html`.
2. Add the read-model / action endpoint under `internal/host/api/` (e.g.
   `ui_approvals.go`), reusing the existing gateway/registry. **No new contract
   surface** — `internal/contract/**` is frozen.
3. Call it from the SPA through `api()` in `static/app.js`, which injects the
   bearer token and handles errors.

## Conventions

- Assets live in `static/` — **not** `dist/`, which `.gitignore` excludes (the
  embed would silently drop them). `embed_test.go` guards against this.
- Keep it dependency-free until a page genuinely outgrows vanilla JS; if a
  framework is adopted later, keep it behind the same `go:embed` boundary so the
  network posture and the no-extra-port guarantee are preserved.
