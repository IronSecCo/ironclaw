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
<img src="../../docs/assets/containment.svg" width="760"
     alt="live-containment terminal demo: a fully-jailbroken agent tries three escapes from inside the sandbox and each is BLOCKED — network exfil denied by network=none (only loopback), host filesystem read denied by the mount namespace, and host takeover via the Docker Engine socket denied because the socket is never mounted in — ending with a containment summary that 3 of 3 escape attempts were denied.">
</div>

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
```

It exits non-zero if **any** escape is not contained, so it doubles as a smoke/CI
assertion — [`examples/smoke-matrix.sh`](../smoke-matrix.sh) drives it with `--attach`.

## Where to go deeper

This is a curated, three-escape cut. For the full picture:

- [`examples/red-team-escape/`](../red-team-escape/) — the complete **six-assertion** battery
  (adds sibling-breakout and cross-session key-custody probes) plus a **signed, versioned
  containment report**, and the CI gate that re-proves it on every push.
- [`docs/breaking-our-own-sandbox.md`](../../docs/breaking-our-own-sandbox.md) — the write-up.
- The **runc fallback** used by this laptop demo is deliberately weaker than the production
  posture: each session is sealed with **gVisor** and `network=none` in production.
