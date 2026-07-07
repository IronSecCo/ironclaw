# live-containment — watch IronClaw catch a real escape, in under a minute

This is the 60-second **aha**. One command stands up the offline demo control-plane,
engages a real per-session sandbox, and then plays out a **fully-jailbroken agent trying
to escape** while your terminal shows each attempt being denied at the isolation boundary.
It ends with a containment summary.

No model key. No channel tokens. The same zero-credential path as
[`docker-compose.demo.yml`](../../docker-compose.demo.yml).

```bash
examples/live-containment/run.sh
```

<div align="center">
<picture>
  <source media="(prefers-reduced-motion: reduce)" srcset="../../docs/assets/containment.svg">
  <img src="../../docs/assets/live-containment.gif" width="760"
       alt="live-containment terminal demo: a fully-jailbroken agent tries three escapes from inside the sandbox and each is BLOCKED — network exfil denied by network=none (only loopback), host filesystem read denied by the mount namespace, and host takeover via the Docker Engine socket denied because the socket is never mounted in — ending with a containment summary that 3 of 3 escape attempts were denied.">
</picture>
</div>

<sub>The clip above is a **real recording of this script** (not staged text) — the source cast
is [`docs/assets/live-containment.cast`](../../docs/assets/live-containment.cast). Users who set
`prefers-reduced-motion: reduce` get the static final-frame SVG instead.</sub>

## What you see

A fully-compromised agent — assume prompt-injection defences, model alignment, and tool
allow-listing have **all** failed — running arbitrary code as the sandbox's own user, and
three escapes each hitting a wall:

| The agent tries to…                                  | …and hits                                              |
|------------------------------------------------------|--------------------------------------------------------|
| **Exfiltrate** stolen data to an attacker (DNS/HTTP) | `network=none`: no NIC but `lo`, so a lookup has nowhere to go |
| **Read the host** filesystem (`/host`, host secrets) | the host root is outside the sandbox mount namespace   |
| **Seize the host** via the Docker Engine socket      | the socket is never mounted in, and there is no docker client |

Then: `CONTAINMENT SUMMARY: 3/3 escape attempts DENIED. The box held.`

## How the test is honest

Each attempt runs **inside the live sandbox container** as its own uid (`65532`) via
`docker exec` — exactly the privilege a jailbroken agent has. The question this answers is
not "can the model be tricked" (prompt injection is a different layer) but: **when it is,
does the isolation boundary still hold?** A test that only pokes the agent through its
polite tool API proves the tools are polite; it does not prove the *box* is a box.

## Flags

```bash
examples/live-containment/run.sh            # build + up + demo + tear down
examples/live-containment/run.sh --keep     # leave the demo running afterwards
examples/live-containment/run.sh --attach   # use an already-running demo control-plane
examples/live-containment/run.sh --share    # also emit a shareable containment receipt
```

It exits non-zero if **any** escape is not contained, so it doubles as a smoke/CI
assertion — [`examples/smoke-matrix.sh`](../smoke-matrix.sh) drives it with `--attach`.

## Share your run (`--share`)

Proved your box holds? Post the proof. `--share` freezes a contained run into a clean,
**copy-pasteable receipt** plus a self-contained **SVG badge** you can drop into a README
or share on X / LinkedIn. Every run becomes a small piece of evidence that IronClaw does
what it says.

```bash
examples/live-containment/run.sh --share
```

It writes three files to `./ironclaw-receipt/` (override with `IRONCLAW_RECEIPT_DIR`):

| File | What it is |
|------|-----------|
| `containment-receipt.txt`  | human-readable receipt + the one-liner to copy |
| `containment-receipt.svg`  | a `IronClaw \| 3/3 contained` badge (offline, embeddable) |
| `containment-receipt.json` | the same facts, machine-readable (schemaVersion 1.0) |

The receipt carries only **counts, the ephemeral session id, the public build version, and
the invariants that held** — no secrets, no host paths, no credentials, no PII. It is safe
to publish as-is. The one-liner reads:

> I ran a fully-jailbroken AI agent inside IronClaw and it could NOT escape: 3/3 sandbox
> breakout attempts blocked (network exfil, host filesystem read, Docker socket takeover).
> Isolation you can prove, not just promise. github.com/IronSecCo/ironclaw

The renderer ([`emit-receipt.sh`](emit-receipt.sh)) is a pure, hermetic transform with its
own unit test ([`receipt_test.sh`](receipt_test.sh)) — no Docker or network required to
exercise the artifact shape.

## Re-record the clip

The animated hero is regenerated straight from this script — the terminal **output is
byte-for-byte the real `--attach` run**, no invented or staged text. The only post-step is
timing: `run.sh` streams its whole transcript in under two seconds, so a raw recording renders
as an unreadable dump (a few near-identical frames). [`pace-cast.py`](pace-cast.py) keeps every
character of that real output and only re-times the inter-line reveal so each containment step
holds long enough to read. It discards input timing, so it is idempotent — re-running it on the
committed cast reproduces the committed cast.

Bring the demo up once (`run.sh --keep`), record an `--attach` run, pace it, then render the GIF
([`asciinema`](https://asciinema.org/) + [`agg`](https://github.com/asciinema/agg)):

```bash
examples/live-containment/run.sh --keep                          # bring the demo up

asciinema rec /tmp/live-containment.raw.cast --overwrite \
  --idle-time-limit 1.4 --window-size 92x28 \
  --command "SKIP_BUILD=1 examples/live-containment/run.sh --attach"

# real output, paced for legibility (see the honesty note above)
examples/live-containment/pace-cast.py /tmp/live-containment.raw.cast \
  > docs/assets/live-containment.cast

agg --font-size 16 --fps-cap 10 --speed 1.0 --idle-time-limit 2.5 \
  --last-frame-duration 2 \
  --theme 0b1124,eaf2ff,1d2c57,ff8087,5ad6a0,e3b341,3b82f6,b9a0ff,63a0ff,b9d4ff,3a4a7a,ff9aa0,7ce8bb,f0c460,63a0ff,cdbcff,93b6ff,ffffff \
  docs/assets/live-containment.cast docs/assets/live-containment.gif
```

The static [`containment.svg`](../../docs/assets/containment.svg) stays the reduced-motion
fallback; regenerate it only if the final frame's copy changes.

## Where to go deeper

This is a curated, three-escape cut. For the full picture:

- [`examples/red-team-escape/`](../red-team-escape/) — the complete **six-assertion** battery
  (adds sibling-breakout and cross-session key-custody probes) plus a **signed, versioned
  containment report**, and the CI gate that re-proves it on every push.
- [`docs/breaking-our-own-sandbox.md`](../../docs/breaking-our-own-sandbox.md) — the write-up.
- The **runc fallback** used by this laptop demo is deliberately weaker than the production
  posture: each session is sealed with **gVisor** and `network=none` in production.
- [`docs/assets/live-containment-social/`](../../docs/assets/live-containment-social/) — the
  narrated, captioned social cut of this same demo (landscape + square MP4, GIF preview),
  ready to share.
