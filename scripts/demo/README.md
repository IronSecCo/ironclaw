# Hero demo asset (`docs/assets/demo.{cast,svg}`)

The animated terminal in the README hero and the docs landing page. It is a faithful
re-enactment of the validated **zero-credential chat demo** (`docs/quickstart.md`,
shipped in IRO-27): one command brings up the offline `mock-agent` control-plane, a
chat message engages the agent — launching a real per-session sandbox container — and
the reply flows back through the encrypted per-session queue.

## Files

| File | What it is |
| --- | --- |
| `gen-cast.mjs` | Deterministically emits `docs/assets/demo.cast` (asciinema v2). Outputs were captured verbatim from a live `docker-compose.demo.yml` run; timing/typing is scripted, no `Date.now`/random, so the cast is reproducible. |
| `gen-profile.mjs` | Emits an iTerm2 `.itermcolors` profile in the IronClaw steel-blue palette (kept for reference / other renderers). |
| `ironclaw-steel.itermcolors` | The generated brand profile. |
| `build.sh` | Full pipeline: cast → `svg-term-cli` → brand recolor → `docs/assets/demo.svg`. |
| `gen-walkthrough.mjs` | Deterministically emits `docs/assets/walkthrough.cast` — the **end-to-end** product walkthrough in three acts (zero-cred demo → connect a real provider → first approved task). Same honesty + determinism rules as `gen-cast.mjs`; ids/flags match the real CLI, secrets are synthetic and redacted. |
| `build-walkthrough.sh` | Full pipeline for the walkthrough: cast → `svg-term-cli` → brand recolor → **prefers-reduced-motion guard** → `docs/assets/walkthrough.svg`. |

### Walkthrough asset (`docs/assets/walkthrough.{cast,svg}`)

A longer, narrated re-enactment for users who want the whole arc, not just the first reply.
It is embedded below the README hero, in the landing `#demo` section, and as a docs
quickstart callout. `build-walkthrough.sh` adds one step over `build.sh`: it injects a
`@media (prefers-reduced-motion: reduce)` rule **into the SVG** so the scroll freezes on
its final frame (the approved-change audit trail). Because the guard travels inside the
asset, it is honored everywhere the SVG is embedded as `<img>` — GitHub and the landing
alike — with no separate poster file.

### Per-example assets (`docs/assets/{hello,redteam}.{cast,svg}`)

The motion assets for the top two gallery examples (IRO-291) — embedded in the README
`## Examples` section and the two featured `docs/examples.md` cards. Same honesty +
determinism + reduced-motion rules as the hero demo:

| File | What it is |
| --- | --- |
| `gen-hello-cast.mjs` | Faithful re-enactment of `examples/hello-ironclaw/run.sh` — the zero-credential end-to-end check (also the CI smoke test): one command → real engage → per-session sandbox → encrypted queue → reply → `PASS`. |
| `gen-redteam-cast.mjs` | Faithful re-enactment of `examples/red-team-escape/run.sh` — the escape battery run from inside a (worst-case) jailbroken sandbox, ending in the `PASS` containment table. The OBSERVED column is condensed for legibility; verdicts, attacks, and semantics match the real script. |
| `build-examples.sh` | Full pipeline for both: cast → `svg-term-cli` → brand recolor → reduced-motion guard → `docs/assets/{hello,redteam}.svg`. Generalizes `build.sh` over a `(name, width, height)` list; the scroll-animation name is matched structurally (svg-term auto-increments the letter per asset), not hardcoded. |

Each cast ends with a short dwell + a benign trailing no-op event so the final `PASS`
frame lingers before the loop restarts (the loop length is the last event's timestamp,
so a trailing wait alone is dropped). That final frame is also the reduced-motion still.

## Regenerate

```sh
bash scripts/demo/build.sh          # hero + walkthrough
bash scripts/demo/build-examples.sh # hello + redteam example demos
# both need node + network (npx fetches svg-term-cli)
```

`svg-term-cli@2.1.1` ignores `--profile` and the cast `theme`, so `build.sh` patches its
four default-theme slots (background, foreground, green, cursor) to the brand palette
with `sed`. The 256-color spans and the window traffic-lights are already on-brand.

## Honesty note

The demo intentionally **relaxes** isolation (runc on a default bridge network), so the
cast does **not** claim `network=none` for the demo itself — it names gVisor +
`network=none` only as the hardened **production** posture. Keep that accurate if you
re-record.
