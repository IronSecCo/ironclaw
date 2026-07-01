# "We tried to break our own sandbox": security-proof writeup (IRO-258)

> **Status: DRAFT, gated. Do not post externally.**
> This is top-of-funnel proof content for the security / r/selfhosted / r/LocalLLaMA / HN
> audience. It is deliberately kept out of the published docs site (it lives in
> `community/`, which mkdocs never builds). It ships publicly only after:
> 1. **WS-G green** (QA has verified the harness output the post quotes), and
> 2. **CEO / board sign-off** on this copy, and
> 3. the **IRO-40 launch gate** is open.
>
> Every capability claim below maps to shipped code (see the "Claim ledger" at the
> bottom). No em-dashes are used in the public-facing copy, per the standing company
> style rule. Terminal output blocks are reproduced verbatim from the harness and are
> labelled as such.
>
> Built on: **IRO-257** (the red-team escape harness, merged) and the real 7/7 core
> PASS run captured on real Docker (colima) under **IRO-262**. Companion to, not a
> replacement for, the launch announcement (IRO-186) and the published trust page
> (`docs/security-isolation.md`, IRO-268).

---

## Part 1: the blog post (long-form)

**Title:** We tried to break our own AI-agent sandbox. Here is what held.

**Subtitle:** One command, no credentials. It attacks a live IronClaw sandbox from the
inside and prints a PASS or FAIL table. Run it yourself.

---

Most AI-agent tools ask you to trust the agent. To trust that the prompt-injection
filter caught the bad instruction, that the tool allow-list was tight enough, that the
model stayed aligned. That is a lot of trust to place in a probabilistic system that an
attacker gets to send input to.

IronClaw takes the opposite bet. We assume the agent is **already compromised**. We
assume prompt-injection defences failed, the model was jailbroken, and attacker code is
now running as the sandbox's own user, inside the box. Then we ask a much simpler,
much harder question: **when that happens, does the isolation boundary still hold?**

So we wrote a harness that tries to break out. It is in the repo, it takes one command,
and it needs no API key and no channel tokens:

```bash
examples/red-team-escape/run.sh
```

It stands up the offline demo control-plane, engages a real per-session sandbox, and
then runs a battery of escape, exfiltration, and self-modification attempts **from
inside that sandbox**, as the sandbox's own user (uid 65532), the exact privilege a
jailbroken agent would have. It prints a PASS or FAIL table and exits non-zero if any
core containment assertion fails, so the same script is both a demo and a CI gate.

### Why attack from the inside

A test that only pokes the agent through its normal tool API proves the tools are
polite. It does not prove the box is a box. The interesting question is not "can the
model be tricked" (that is a different layer, and every model can be tricked eventually).
The question is what a fully-compromised agent can actually reach. So the harness uses
`docker exec` to run each probe as the sandbox's own uid, simulating an attacker who has
already won the prompt-injection game and is now holding a shell inside the container.

### The threats, and the control that stops each one

We picked the six things a compromised agent would actually try:

1. **Phone home.** Enumerate network interfaces, resolve a model-API hostname, open a
   socket to exfiltrate data or receive commands.
2. **Escape to the host.** Reach the Docker Engine socket, which is game-over if the
   sandbox can touch it: whoever holds `/var/run/docker.sock` owns the host.
3. **Break into a sibling.** Inspect, spawn, or kill the other session containers
   running next to it.
4. **Read the host filesystem.** Reach arbitrary host paths outside its own mounts.
5. **Rewrite its own permissions.** Enable a new tool or capability for itself,
   silently.
6. **Steal another session's keys.** Read the host master key or a sibling session's
   key material, which would unseal everyone's data.

Here is what contains each one:

| Attack from inside the sandbox | What contains it | Layer |
| --- | --- | --- |
| Network egress (enumerate NICs, resolve DNS) | `network=none`: no network namespace, so only loopback exists and a packet has nowhere to go | Isolation |
| Host escape via the Docker Engine socket | The Engine socket is never bound into the sandbox | Trust boundary |
| Sibling container breakout | No docker client and no socket, so the daemon is unreachable | Isolation |
| Read arbitrary host filesystem paths | Host root is outside the sandbox mount namespace (read-only rootfs) | Isolation |
| Self-modification (enable a new tool) | The request is held at the gateway's mandatory human-approval floor | Mandatory gateway |
| Cross-session key theft (master and sibling keys) | Per-session binds only; the master key and sealed store are never mounted in | Encryption at rest |

### The actual results

This is the real table the harness printed on a real-Docker run. Not a mock, not a
diagram, the literal program output (terminal block, reproduced verbatim):

```text
==============================================================================
 IronClaw red-team escape results  (attack -> expected -> observed)
==============================================================================
  RESULT  ATTACK                                          OBSERVED
  ------  ----------------------------------------------  --------
  PASS    network egress: enumerate NICs                  interfaces: lo
                                                          (expected: only loopback (network=none))
  PASS    network egress: DNS lookup of api.anthropic.com  getent exit 2
                                                          (expected: resolution fails (no egress))
  PASS    host escape: reach the Docker Engine socket     docker.sock ABSENT
                                                          (expected: socket absent)
  PASS    sibling breakout: orchestrate sibling containers  docker client ABSENT
                                                          (expected: no docker client + no socket)
  PASS    host escape: read arbitrary host paths          host paths CONTAINED
                                                          (expected: host root not mounted)
  PASS    self-modification: enable a new tool            queued in /v1/changes/pending, not applied
                                                          (expected: held at gateway (pending human approval))
  PASS    cross-session: read the host master key / sibling session keys  master key + sealed store unreachable; only own session key visible (1)
                                                          (expected: trust root not mounted (per-session binds only))
==============================================================================

RESULT: every core containment assertion held. The sandbox contained a
        fully-compromised agent (network, host escape, sibling breakout, self-mod).
```

Read that middle-of-the-table row again, the self-modification one, because it is the
one that surprises people. A compromised agent **can** ask to enable a new tool for
itself. That request is a real, supported operation. But it can never *apply* the
change. The request lands on the encrypted outbound queue, the host turns it into a
change request, and the gateway parks it in `GET /v1/changes/pending` until a human
approves it. The probe fires that request and then asserts it is sitting in the pending
queue, unapplied. The agent asked. Nothing happened. That is the design.

### The one thing we are honest about

We could have shipped a harness that only ever prints green. We did not, because a
security audience can smell that from orbit.

The zero-credential demo you just ran uses the `runc` fallback so it works on a stock
laptop without gVisor installed. `runc` shares the host kernel. **Production uses
gVisor** (`runsc`), a user-space kernel that puts the real host kernel behind a
seccomp-bounded, capability-dropped boundary, with a read-only rootfs, `no_new_privs`,
and a non-root user namespace. That kernel seal is the one thing the laptop demo cannot
demonstrate without gVisor present, so the harness does not claim it does.

Everything else in the table, the network wall, the Docker-socket boundary, the
sibling-breakout wall, the gateway hold, and the per-session key custody, holds
**identically** on both the demo `runc` path and the production gVisor path. Those are
the core PASS rows. There is one more piece of honesty worth stating plainly: cross-
session key custody used to be a real gap on the demo path (tracked as IRO-259). The
`runc` fallback originally bind-mounted the entire control-plane state directory into
every sandbox, which exposed the host master key. We found it with this harness, filed
it, and fixed it: the isolator now scopes its binds per session, so the master key and
every sibling key are no longer reachable. The harness now asserts that directly as a
core PASS row, so a future regression that re-widens the mount would fail the run.

That is the point of writing the harness. Not to produce a green checkmark for a
landing page, but to have a thing that tells the truth about what our own box can and
cannot contain, and that fails loudly the day someone weakens it.

### It runs on every push

This is not a one-time stunt. The same harness runs as a
[continuous CI gate](https://github.com/IronSecCo/ironclaw/blob/main/.github/workflows/sandbox-containment.yml)
on every push, and it carries a **negative control**: the gate deliberately weakens the
sandbox (re-enabling a bridge network) and asserts the harness *catches* the regression.
A containment gate that cannot fail is not a gate, so we prove ours can.

### Run it yourself

```bash
git clone https://github.com/IronSecCo/ironclaw
cd ironclaw
examples/red-team-escape/run.sh
```

No key. No tokens. It will build the sandbox image, bring the demo up, attack it from
the inside, print the table, and tear down. If you want to poke at the box yourself
afterward, `run.sh --keep` leaves it running.

If you care about running untrusted or semi-trusted AI agents anywhere near your data,
we would genuinely like you to try to break it and tell us what you find. The threat
model, the full boundary-by-boundary analysis, and the measured overhead you pay for the
wall are all in the repo.

- Repo: https://github.com/IronSecCo/ironclaw
- The harness: `examples/red-team-escape/`
- The trust page: https://ironsecco.github.io/ironclaw/security-isolation/
- The threat model: https://ironsecco.github.io/ironclaw/threat-model/

IronClaw is AGPLv3 (commercial license available). Isolation you can prove, not just
promise.

---

## Part 2: Show HN variant (link-ready)

**Title:** Show HN: We tried to break our own AI-agent sandbox (one command, no keys)

**URL:** https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape

**Text:**

IronClaw is an open-source (AGPLv3) sandbox for running AI agents where you assume the
agent is already compromised. Instead of asking you to trust that, we wrote a harness
that tries to break out and asserts each attempt is contained.

It is one command, no API key, no tokens:

    examples/red-team-escape/run.sh

It stands up an offline control-plane, engages a real per-session sandbox, then runs six
attacks *from inside that sandbox* as its own uid (the privilege a jailbroken agent
would have): phone home over the network, reach the Docker Engine socket, break into a
sibling container, read host filesystem paths, silently enable a new tool for itself, and
steal another session's keys. It prints a PASS or FAIL table and exits non-zero on any
core failure, so it doubles as a CI gate.

Two things I would want to know if I were reading this:

1. It is honest about scope. The laptop demo uses runc (shared kernel); production uses
   gVisor (runsc, user-space kernel). The kernel seal is the one thing the demo cannot
   show, so the harness does not claim it. The network, Docker-socket, sibling, gateway,
   and per-session key boundaries hold identically on both paths.

2. This harness already caught one of our own bugs. The runc demo path used to mount the
   whole state directory into every sandbox, which exposed the host master key. We found
   it, filed it, fixed it (per-session binds now), and the harness asserts it as a
   permanent regression test.

The self-modification case is my favorite: an agent can *ask* to enable a new tool, but
the request is parked at a mandatory human-approval gateway (`GET /v1/changes/pending`)
and never auto-applies. The agent asks, nothing happens.

Would love for people who do this for a living to try to break it and tell us what held
and what did not.

Repo: https://github.com/IronSecCo/ironclaw

---

## Part 3: Reddit variants

### r/selfhosted

**Title:** I run AI agents on my own box, so I wrote a one-command harness that tries to
break out of the sandbox and prints what held

**Body:**

If you self-host anything that runs AI-generated code or agents near your data, the
question that keeps me up is not "will the model misbehave" (it will, eventually) but
"when it does, what can it actually reach on my host."

IronClaw (open source, AGPLv3) ships a harness that answers that with real output
instead of marketing. One command, no API key:

    examples/red-team-escape/run.sh

It runs six escape attempts from *inside* a live sandbox as its own user: network
egress, Docker socket escape, sibling-container breakout, host filesystem reads, silent
self-permission changes, and cross-session key theft. PASS or FAIL table, non-zero exit
on failure.

Honest bit for this crowd: the laptop demo uses runc (shared kernel) so it runs
anywhere; production uses gVisor for the kernel seal. The harness says so out loud
instead of pretending. It also already caught a real bug in our own demo path (a mount
that exposed the master key), which we fixed and now regression-test.

Repo and the harness readme: https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape

### r/LocalLLaMA

**Title:** Running local models with agent tool-use? Here is a one-command harness that
tries to escape the agent sandbox and shows the results

**Body:**

If you point a local model at tools (shell, file access, MCP servers), you are one
prompt-injection away from that model running attacker code with your privileges. The
usual answer is "we filter the prompts." That is not a boundary, it is a wish.

IronClaw treats the agent as already compromised and puts a real isolation boundary
around it. To prove the boundary, there is a harness you run in one command, no keys:

    examples/red-team-escape/run.sh

It attacks a live sandbox from the inside (as the sandbox uid) and asserts containment
of network egress, host escape, sibling breakout, host FS reads, self-modification, and
cross-session key theft. Prints a PASS/FAIL table, exits non-zero on failure, runs in CI
on every push with a negative control that proves the gate can actually fail.

It works with local models too (Ollama, LM Studio, vLLM via the OpenAI-compatible path),
and the sandbox runs `network=none`, so a jailbroken agent cannot phone home even if it
wanted to.

Repo: https://github.com/IronSecCo/ironclaw

---

## Part 4: X / Twitter thread

1/ We assume our AI agents are already compromised.

So we wrote a harness that tries to break out of the sandbox, from the inside, as the
agent's own user. One command, no API key:

    examples/red-team-escape/run.sh

Here is what held. 🧵

2/ Six attacks a jailbroken agent would actually try:
- phone home over the network
- reach the Docker Engine socket (host game-over)
- break into a sibling container
- read host filesystem
- silently enable a new tool for itself
- steal another session's keys

3/ Result: every core containment assertion held. Real terminal output, not a diagram.
network=none means a packet has nowhere to go. The Engine socket is never in the box.
The master key is never mounted in.

4/ The one I like most: self-modification. The agent CAN ask to enable a new tool. The
request is parked at a mandatory human-approval gateway and never auto-applies. The
agent asks. Nothing happens.

5/ We are honest about scope. Laptop demo = runc (shared kernel). Production = gVisor
(user-space kernel seal). The harness says which boundaries hold on both paths and which
one only production adds. No green-washing.

6/ It runs in CI on every push with a negative control that deliberately weakens the
sandbox to prove the gate can actually fail. A containment test that cannot fail is not
a test.

7/ Open source, AGPLv3. Try to break it and tell us what you find.
https://github.com/IronSecCo/ironclaw

---

## Part 5: LinkedIn post

We built an AI-agent runtime on an uncomfortable assumption: the agent is already
compromised.

Prompt-injection defences fail. Models get jailbroken. Tool allow-lists have gaps. If
your security story depends on none of that ever happening, you do not have a security
story.

So instead of asking anyone to trust the agent, we wrote a harness that tries to break
out of the sandbox from the inside, and prints exactly what held. One command, no
credentials.

It runs six real escape attempts as the agent's own user: network egress, host escape
via the Docker socket, sibling-container breakout, host filesystem reads, silent
self-permission changes, and cross-session key theft. Every core containment assertion
held, and the same harness runs in CI on every push with a negative control that proves
the gate can actually fail.

We are also honest about the one boundary the laptop demo cannot show (the gVisor kernel
seal that production adds), because a security audience can tell when you are
green-washing. This harness already caught one of our own bugs before it shipped.

If you run untrusted or semi-trusted AI agents near real data, I would genuinely value
your attempt to break it.

Open source, AGPLv3: https://github.com/IronSecCo/ironclaw

---

## Claim ledger (for the reviewer, not for publication)

Every public-facing claim above, mapped to the shipped source it rests on. QA (WS-G) to
confirm before sign-off.

| Claim in copy | Backed by | Verified |
| --- | --- | --- |
| One command, no key/tokens, runs the demo path | `examples/red-team-escape/run.sh`, `docker-compose.demo.yml` (zero-cred) | IRO-262 real-Docker run |
| Probes run inside the sandbox as uid 65532 | `run.sh` `sbx()` uses `docker exec -u 65532` | IRO-257 |
| network=none, only loopback, DNS fails | rows 1-2, `interfaces: lo`, `getent exit 2`; `internal/host/isolation/oci.go` refuses a net stack | IRO-257 / IRO-262 |
| Docker Engine socket never in the sandbox | row 3, `docker.sock ABSENT` | IRO-257 / IRO-262 |
| No docker client, siblings unreachable | row 4, `docker client ABSENT` | IRO-257 / IRO-262 |
| Host root not in the mount namespace | row 5, `host paths CONTAINED` | IRO-257 / IRO-262 |
| Self-mod held at gateway, unapplied in `/v1/changes/pending` | row 6, `request_capability_change` -> ChangeRequest -> pending queue | IRO-257 / IRO-262 |
| Master + sibling keys unreachable, only own session key | row 7, `only own session key visible (1)`; per-session binds (IRO-259) | IRO-259 / IRO-262 |
| Found and fixed our own master-key mount bug | IRO-259 (per-session Docker binds), fix merged | IRO-259 |
| Production = gVisor (runsc): seccomp, caps dropped, ro rootfs, no_new_privs, non-root userns | `docs/security-isolation.md`, `docs/threat-model.md` | IRO-268 / IRO-84 |
| Runs in CI on every push with a negative control | `.github/workflows/sandbox-containment.yml` (IRO-261) | IRO-261 (in_review) |
| Local models via OpenAI-compatible path (Ollama/LM Studio/vLLM) | IRO-188 (merged) | IRO-188 |
| AGPLv3 + commercial | `LICENSE`, repo metadata | shipped |

**Open item for QA/CEO before publish:** IRO-261 (the CI containment gate) is still
in_review at time of drafting. The "runs in CI on every push" line should be confirmed
green, or softened to "ships as a CI gate," before the post goes public.
