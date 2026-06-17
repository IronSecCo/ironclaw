# Spike — Web console architecture + scaffold (T-220)

**Status:** decided + scaffolded
**Owner task:** T-220 (wave 7). Unblocks T-221–T-226 (Approvals inbox, Sessions
browser, Channels/wiring, Logs/audit viewer, Config editor, Chat playground).

This spike picks the stack and the security posture for an IronClaw web console
and lands a minimal, buildable scaffold the feature tasks extend. The hard
invariant: **the UI must never widen the network posture** (cf. openclaw binding
`127.0.0.1:18789`).

---

## 1. Decisions

| Question | Decision | Why |
|---|---|---|
| SPA framework | **Vanilla HTML/CSS/JS, no build step** | Zero new toolchain. The existing `go build ./...` (and CI) builds the whole console — no Node, no npm, no `.github/**` (`lock:ci`) change. Feature tasks add pages as plain files under `web/static/`. A framework (React/Svelte) can be adopted later behind the same embed boundary if a page outgrows vanilla; this spike keeps the dependency surface at zero. |
| Embed vs separate server | **Embedded** into the control-plane binary via `//go:embed`, served on the **existing** API listener at `GET /ui/` | No new port, no new process, no new listener — the network posture is exactly the control-plane's existing one. Shipping the UI is shipping the same binary. |
| Auth reuse of the API token | **API stays bearer-gated; the static shell is auth-exempt; the SPA attaches the bearer to every `/v1` call** | See §2. The browser cannot put an `Authorization` header on a top-level navigation, so the *shell* (non-secret static HTML/JS/CSS) is served exempt — the same mechanism `/healthz` and `/readyz` already use. Every byte of *data* and every *action* still flows through the bearer-gated `/v1/*` API, which the SPA calls with `Authorization: Bearer <token>`. |
| Binding | **Loopback-only by default** for the console's intended use; production binds the API to the tailnet IP as today | The control-plane already binds a single address (`-api-addr`, default `127.0.0.1:8787`). The console reuses it. Operators who expose the API on the tailnet get the console there too — still safe, because the shell carries no secrets and no action works without the token (§2). |

### Stack at a glance

```
web/
  embed.go            package webui — //go:embed all:static, exposes Assets() fs.FS
  embed_test.go       asserts the embedded FS actually contains the shell
  static/             the SPA — plain files, no build artifacts (NB: not dist/, which .gitignore excludes)
    index.html        shell + token-entry + panels
    app.js            fetch wrapper that injects the bearer; renders pending changes + audit
    style.css         styling
  README.md           how to extend it (one file per feature page)

internal/host/api/
  ui.go               mounts GET /ui/ on the existing mux; marks the shell auth-exempt
  ui_test.go          the shell loads without a token; /v1 still 401s without one
  api.go              two one-line hooks: routes() mounts the UI; auth() exempts the shell
```

---

## 2. Security review — serving a UI from the control-plane

The threat model is unchanged from the headless control-plane; the console adds
**no new network surface**. Specifics:

1. **No new listener / port / interface.** The console is served on the existing
   `-api-addr` listener. Whatever the API's reachability is, the console's is the
   same — never broader. There is no second server to misconfigure.

2. **The shell is auth-exempt; everything else is not.** `GET /ui/...` returns
   static, secret-free assets without a bearer token (a browser can't header a
   navigation). This is the exact carve-out `/healthz` and `/readyz` already get.
   **All data reads and all state changes go through `/v1/*`**, which the bearer
   gate (`auth()` in `api.go`, constant-time compare) still fully protects. A
   visitor who can load `/ui/` but lacks the token sees an empty shell that can do
   nothing — no pending changes, no audit, no approve/reject.

3. **No posture widening.** The shell holds no credentials and triggers no
   side-effects. The token is never embedded in the served assets; the operator
   pastes it into the SPA at runtime and it lives only in `sessionStorage`
   (cleared on tab close), sent as `Authorization: Bearer` on `/v1` fetches.

4. **Defense-in-depth preserved.** The mesh/tailnet boundary, the rate limiter,
   the body-size cap, and the bearer gate all remain in front of `/v1`. The UI
   reuses them unchanged.

### Deferred / follow-up (not blocking the scaffold)

- **Token bootstrap UX.** Today the operator pastes the token into the SPA. A
  nicer flow (one-time `?token=` → `HttpOnly` `SameSite=Strict` cookie set by the
  server, then the shell itself can be gated too) is a clean follow-up. It was
  deliberately *not* done here to avoid touching the security-critical `auth()`
  comparison path in a spike; the current carve-out is strictly the static shell.
- **CSRF.** Not applicable while auth is a bearer header (not an ambient cookie).
  If the cookie bootstrap above is adopted, add a CSRF token or keep the bearer
  header for state-changing calls.
- **CSP.** A strict `Content-Security-Policy` (no inline script once the SPA
  grows) is worth adding when a feature page first needs it.

---

## 3. How feature tasks (T-221+) extend this

- Add a page as plain files under `web/static/` (e.g. `web/static/approvals.js`)
  and a nav entry in `index.html`. No build step, no new dependency.
- Add the read-model / action endpoint under `internal/host/api/` (e.g.
  `ui_approvals.go`), reusing the existing gateway/registry — **no new contract
  surface** (`internal/contract/**` stays frozen).
- The SPA calls it through `api()` in `app.js`, which injects the bearer token.

The scaffold deliberately ships a working *Approvals*-style read (pending changes
+ audit) so T-221 has a concrete pattern to follow.
