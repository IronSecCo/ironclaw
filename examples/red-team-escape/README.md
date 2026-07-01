# red-team-escape — we tried to break our own sandbox

IronClaw's promise is **isolation you can prove, not just promise.** This example is
the proof. It is a one-command adversarial harness that stands up the offline demo
control-plane, engages a real per-session sandbox, and then runs a battery of
escape / exfiltration / self-modification attempts **from inside that sandbox** and
asserts each one is contained.

No model key. No channel tokens. The same zero-credential path as
[`docker-compose.demo.yml`](../../docker-compose.demo.yml).

```bash
examples/red-team-escape/run.sh
```

It prints a PASS/FAIL table and exits non-zero if any **core** containment assertion
fails, so it works as a CI check as well as a demo.

## The threat model (read this first)

We assume the **worst realistic case**: the agent has been fully jailbroken and can
run **arbitrary code inside its sandbox**. Prompt-injection defences, model alignment,
tool allow-listing — assume all of it failed. The attacker is now a process running as
the sandbox's own user, inside the box.

The harness simulates that by using `docker exec` to run each attack probe inside the
live sandbox container as its uid (65532) — exactly the privilege a compromised agent
would have. The question it answers is **not** "can the model be tricked" (that is a
different layer) but: **when it is, does the isolation boundary still hold?**

This is the honest way to test isolation. A test that only pokes the agent through its
normal tool API proves the tools are polite; it does not prove the *box* is a box.

## What it attacks, and the control that stops each one

| Attack (from inside the sandbox)                         | Control that contains it                              | Layer |
|----------------------------------------------------------|-------------------------------------------------------|-------|
| **Network egress** — enumerate NICs, resolve DNS         | `network=none`: no network namespace / no NIC but `lo`| Isolation |
| **Host escape** — reach the Docker Engine socket         | Engine socket is never mounted into the sandbox       | Trust boundary |
| **Sibling breakout** — inspect / spawn / kill siblings   | No Docker client **and** no Engine socket to reach    | Isolation |
| **Host filesystem** — read arbitrary host paths          | Host root fs is outside the sandbox mount namespace   | Isolation |
| **Self-modification** — enable a new tool / capability   | Mandatory gateway holds it for a human decision       | Mandatory gateway |
| **Cross-session / key custody** — read the master key    | Per-session binds only; trust root never mounted in   | Encryption at rest |

### Network egress — `network=none`

The production posture builds the sandbox OCI spec with **no network namespace at
all** — [`internal/host/isolation/oci.go`](../../internal/host/isolation/oci.go)
literally *refuses to launch* a sandbox with a network stack. The demo runs the runc
fallback, so `docker-compose.demo.yml` sets `IRONCLAW_DOCKER_NETWORK=none` to match.
The probe reads `/sys/class/net` (only `lo` exists) and confirms a DNS lookup of a
model-API hostname fails: there is nowhere for a packet to go.

### Host escape / sibling breakout — the Docker socket

Whoever holds `/var/run/docker.sock` owns every sibling container and the host. The
sandbox never receives it: the control-plane launches siblings by talking to the
Engine socket itself, and hands the sandbox only its queues, key, and the model-proxy
socket — never the Engine socket, and no `docker` client. The probes confirm both are
absent, so there is no path from the sandbox to the daemon.

### Self-modification — the mandatory gateway

A sandbox can *ask* to change its capabilities, but it can never *apply* the change.
The agent's `request_capability_change` tool (always registered, even for the offline
mock agent) emits an envelope onto the encrypted outbound queue; the host turns it into
a `ChangeRequest` that the gateway's floor verifier routes to **mandatory human
approval** before anything is applied. The probe fires that request and then asserts
it is sitting in `GET /v1/changes/pending`, unapplied — the change was *held*, not
silently made.

## How to read the results

- **PASS** — the attack was contained by the control in the table. Good.
- **FAIL** — a **core** containment assertion did not hold. The harness exits non-zero.
  This is a real security regression: do not ship until it is fixed.
- **GAP** — a **known, tracked** relaxation of the *laptop demo* (the runc fallback),
  not the sealed production posture. Printed loudly and honestly; does **not** fail the
  run. The harness supports this verdict for honesty; with the cross-session key-custody
  gap now closed (see below), no attack currently reports `GAP`.

## Honest scope: demo (runc) vs production (gVisor)

The demo trades the *kernel* seal for laptop-friendliness. `docker-compose.demo.yml`
says so out loud: it runs the runc runtime (**shared host kernel**, not gVisor), the
control-plane as root, and mounts the Docker socket **into the control-plane** (not the
sandbox). The **network egress**, **Docker-socket / sibling breakout**, **gateway
self-modification**, and **cross-session key custody** boundaries this harness asserts
hold **identically** on both paths — that is why they are core PASS assertions.

The one thing the demo genuinely relaxes, which **production gVisor closes**:

- **Shared kernel.** runc shares the host kernel; gVisor (`runsc`) interposes a
  user-space kernel with a seccomp-bounded host surface, all caps dropped,
  `no_new_privs`, and a read-only rootfs. The demo cannot demonstrate this on a stock
  laptop without gVisor installed — so the harness does not claim to.

**Cross-session key custody used to be a gap on the demo path (IRO-259) — it is now
closed.** The runc fallback originally bind-mounted the **entire** control-plane state
directory into every sandbox so the per-session queue paths lined up, which also
exposed the host master key and sibling sessions' key material to a compromised
sandbox. The Docker isolator now scopes its binds **per session** — it translates and
mounts only that session's own `sessions/<id>` queue files (read-only inbound,
read-write outbound) and its `keys/<id>/session.key` (read-only), exactly as the
gVisor/OCI posture does — so `host-master.key`, `sealed-keys.json`, and every sibling
`keys/<session>/session.key` are never mounted in and are unreachable from inside the
box. The harness asserts this directly as a **core PASS** row (probing all three), so a
future change that re-widens the bind to the whole state dir would fail the run.

We would rather ship a harness that tells the truth — "here is exactly what held, and
here is the single seal only production gVisor adds" — than one that only ever prints
green.

## Flags

```
run.sh            # build the sandbox image, bring the demo up, attack, tear down
run.sh --keep     # leave the demo running afterwards (inspect it yourself)
run.sh --attach   # attack an already-running demo control-plane, manage nothing
```

Useful env overrides: `IRONCLAW_ADDR`, `IRONCLAW_API_TOKEN`, `IRONCLAW_DEMO_AGENT`,
`SKIP_BUILD=1`, `IRONCLAW_HEALTH_TIMEOUT`, `IRONCLAW_ENGAGE_TIMEOUT`.
