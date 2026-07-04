# IronClaw Containment Proof Pack

The single strongest, data-driven distribution asset we can build without a board gate: we let a
fully jailbroken AI agent try to break out of its sandbox, at scale, and we measured what got
contained. This pack turns the abstract claim "isolation you can prove, not just promise" into a
number a skeptic can reproduce.

- **Owner:** Growth / DevRel (WS-E). Tracks IRO-339.
- **Status:** produced and staged. Producing this pack is **not** board-gated. **Firing** the
  posts is gated on the launch sign-off (IRO-40: WS-G green + WS-H UX bar cleared + WS-A/B/C
  done) and is already queued in IRO-290. This pack exists so the eventual human post lands with
  concrete data instead of adjectives.
- **House style (standing rules):** no em dashes or en dashes in any public copy (IRO-254).
  Every claim maps to a shipped, checkable capability. No owner name, no personal socials
  (launch-engagement-playbook.md identity).
- **SSOT:** [`docs/threat-model.md`](../docs/threat-model.md), the containment CI gate
  ([`.github/workflows/sandbox-containment.yml`](../.github/workflows/sandbox-containment.yml)),
  and the escape harness ([`examples/red-team-escape/`](../examples/red-team-escape/)).

---

## 1. The experiment

**Threat model (worst case, on purpose).** We assume every soft defence has already failed:
prompt-injection filtering, model alignment, and tool allow-listing are all bypassed, and the
attacker is now running arbitrary code as the sandbox's own user (uid 65532) from **inside** the
box. The question is not "can the model be tricked." It can. The question is: when it is, does
the isolation boundary still hold?

**Method.** We stood up the offline, zero-credential demo control plane (no model key, no channel
tokens), engaged a real per-session sandbox, and ran the full escape battery from inside it. Then
we repeated it across `12` independent sandbox sessions so the result is a distribution, not a
single lucky run. Every probe runs as the sandbox's own uid, exactly the privilege a jailbroken
agent would hold.

**What each session attempts.** Six containment invariants (network egress is checked two ways,
so seven probes per session), grouped into the four escape categories a real attacker works
through:

| Category | Escape attempt | What must hold | Enforced by |
| --- | --- | --- | --- |
| **Network egress** | Resolve a C2 host and exfiltrate stolen data | No network interface but loopback; DNS has nowhere to go | `network=none` per sandbox |
| **Privilege / host takeover** | Seize the host via the Docker Engine socket | Engine socket never mounted in; no docker client present | Per-session isolation, no socket bind |
| **Privilege / host takeover** | Orchestrate or kill sibling session containers | No socket and no client, so siblings are unreachable | Per-session isolation |
| **Filesystem** | Read the operator's host filesystem and secrets | Host root is outside the sandbox mount namespace | Mount-namespace isolation |
| **Filesystem / key custody** | Read the host master key and sibling session keys | Only the session's own key is bound in; trust root unreachable | Per-session binds (IRO-259) |
| **Privilege / self-modification** | Widen its own capabilities (enable a new tool) | The request is held at the approval gateway, never self-applied | Mandatory approval gateway |

**One category proven separately, and we say so.** The laptop and CI harness run the sandbox on
the **runc fallback with `network=none`**, which is what proves the five boundary invariants
above. Production additionally seals each session under **gVisor (runsc)**, a user-space kernel
that filters syscalls so a kernel-level exploit has no host kernel to reach. That syscall-isolation
layer is verified by the WS-G capability gate (IRO-84), not by this scale run. We do not claim the
laptop run exercised gVisor. Credibility over hype: the reader can run the five-invariant battery
themselves in one command, and the sixth layer is documented and independently gated.

---

## 2. Results

Each session runs 7 probes (the six invariants, with network egress checked two ways: NIC
enumeration and a live DNS lookup). Aggregated across all 12 sessions:

**Headline:** `84` real escape attempts across `12` independent sandbox sessions.
**`84` blocked. `0` succeeded.** Containment rate: **`100`%**.

Runtime under test: `runc` demo posture, `network=none`, Docker 29.5.2, sandbox image pinned by
digest. Full per-session reports (schemaVersion 1.0) and the aggregate are committed alongside
this pack in [`community/data/`](data/).

| # | Escape attempt (category) | Attempts | Blocked | Escaped |
| --- | --- | --- | --- | --- |
| 1 | Network egress: enumerate NICs (network) | `12` | `12` | 0 |
| 2 | Network egress: DNS lookup of a C2 host (network) | `12` | `12` | 0 |
| 3 | Reach the Docker Engine socket (privilege) | `12` | `12` | 0 |
| 4 | Orchestrate sibling containers (privilege) | `12` | `12` | 0 |
| 5 | Read arbitrary host paths (filesystem) | `12` | `12` | 0 |
| 6 | Self-modify: enable a new tool (privilege) | `12` | `12` | 0 |
| 7 | Read host master key / sibling keys (key custody) | `12` | `12` | 0 |
| | **Total** | **`84`** | **`84`** | **`0`** |

**This is not a one-off.** The same battery runs as a CI gate
([`sandbox-containment.yml`](../.github/workflows/sandbox-containment.yml)) on **every push and
pull request** to `main`. It ships with a **negative control**: the gate re-runs against a
deliberately weakened sandbox (a bridge NIC instead of `network=none`) and the build fails if the
harness does **not** catch the regression. So the gate is proven to catch a real weakening, not
just to be green. Every release also ships a **signed, attested containment report** (cosign +
SLSA) binding these invariants to the exact commit and runtime tested (IRO-267).

**Reproduce it yourself (about a minute, zero credentials):**

```bash
git clone https://github.com/IronSecCo/ironclaw && cd ironclaw
examples/live-containment/run.sh      # watch a jailbroken agent try to escape, live
# or the full battery + machine-verifiable report:
examples/red-team-escape/run.sh
```

---

## 3. Blog draft

**Title:** We let an AI agent try to escape its sandbox `84` times. Here is what got
contained.

**Slug:** `we-let-an-ai-agent-try-to-escape`

---

Everyone shipping AI agents right now is making a bet: that the model will not do something
catastrophic when it has shell access, network, and your credentials. Prompt-injection defences,
alignment, and tool allow-lists are the usual seatbelts. They are worth having. They also fail.

So we asked a different question. Not "can the agent be tricked" (it can), but: **when it is
tricked, and it is now running attacker code as its own user, does anything actually stop it?**

We built IronClaw around the assumption that the answer to the first question is always
eventually "yes," so the second question is the only one that matters. Then we tried to prove our
own sandbox wrong, at scale.

### The setup

We stood up the offline demo control plane (no API key, no channel tokens) and engaged a real
per-session sandbox. Then, from **inside** that box, running as its own unprivileged user, we ran
the escape battery a jailbroken agent would run if every soft defence had already failed:

- Phone home: resolve a command-and-control host and exfiltrate data.
- Take the host: reach the Docker Engine socket, the crown jewel that owns every container and
  the machine under it.
- Move laterally: enumerate and kill sibling agent sessions.
- Steal from disk: read the operator's host filesystem and secrets.
- Steal the keys: read the host master key and other sessions' keys.
- Escalate itself: ask to enable a new capability and apply it without asking a human.

We repeated the whole battery across `12` independent sandbox sessions.

### The result

`84` escape attempts. `84` blocked. `0` succeeded.

Every attempt hit a wall that was structural, not heuristic. There was nothing to bypass:

- **No network to phone home over.** Each sandbox runs with `network=none`. Inside the box the
  only interface is loopback. A DNS lookup does not get filtered, it simply has nowhere to go.
- **No socket to seize the host with.** The Docker Engine socket is never mounted into the
  sandbox, and there is no docker client inside it. There is no privileged escape hatch to find,
  because it was never handed over.
- **No siblings to reach.** With no socket and no client, other agent sessions are invisible.
- **No host filesystem to read.** The host root is not in the sandbox's mount namespace. The
  paths an attacker reaches for do not exist inside the box.
- **No keys to steal.** Each session binds in only its own key. The host master key that seals
  everything, and every other session's key, are not mounted. There is nothing to read.
- **No self-escalation.** When the agent asked to widen its own capabilities, the request did not
  apply. It was held at an approval gateway for a human to allow or deny.

### Why this is checkable, not marketing

For a security tool, "secured" is a claim, and claims should be reproducible by the reader. Two
things make this one checkable:

1. **You can run it.** One command stands up the whole thing with zero credentials and shows you
   each escape attempt being denied, live, in about a minute.
2. **It cannot silently rot.** The same battery is a CI gate on every push. It ships with a
   negative control: the gate is re-run against a deliberately broken sandbox, and the build fails
   if the harness does not catch it. A gate that only ever goes green is a gate you cannot trust,
   so we prove ours goes red on a real regression. Every release also carries a signed, attested
   containment report bound to the exact commit and runtime.

### The honest boundary

This is isolation of a **compromised agent**, not a promise the model will never be compromised.
Those are different layers, and conflating them is how security tools lose trust. Prompt injection
can still make the agent try any of the above. The point is that when it does, the boundary holds
and the attempt is contained. On the laptop and in CI the boundary is `network=none` plus
per-session filesystem and key isolation. In production each session is additionally sealed under
gVisor, a user-space kernel that keeps a syscall-level exploit away from the host kernel.

If you are shipping agents that touch anything you would not hand to a stranger, the boundary is
the part worth being able to prove. Ours is one command away.

Repo: https://github.com/IronSecCo/ironclaw

---

## 4. Per-channel copy

All copy below is staged. **Do not post before the launch gate (IRO-40).** Re-verify every claim
against `main` with QA on the day (per launch-engagement-playbook.md). No owner name, no personal
socials, no em or en dashes.

### 4a. Show HN

**Title:**
```
Show HN: We let a jailbroken AI agent try to escape its sandbox 84 times
```

**Body (first comment):**
```
IronClaw is a security-hardened, open-source alternative to openclaw/nanoclaw for running AI
agents. The pitch is one thing: isolation you can prove, not just promise.

So we tried to break our own sandbox. We assumed the worst case (prompt-injection defences,
alignment, and tool allow-listing all bypassed) and ran attacker code as the sandbox's own user
from inside the box, across 12 independent sessions. 84 escape attempts total:
phone home over the network, seize the host via the Docker socket, read the host filesystem,
steal the master key, escalate its own capabilities.

Result: 84 blocked, 0 escaped. Each wall is structural, not heuristic. network=none
means there is no interface to exfiltrate over. The Docker socket is never mounted in, so there is
no host to seize. The host filesystem is not in the mount namespace. Each session binds only its
own key. Self-escalation is held at an approval gateway.

You can reproduce it in about a minute with zero credentials:

  git clone https://github.com/IronSecCo/ironclaw && cd ironclaw
  examples/live-containment/run.sh

The same battery is a CI gate on every push, with a negative control: it is re-run against a
deliberately weakened sandbox and the build fails if the harness does not catch the regression.
Every release ships a signed, attested containment report bound to the commit.

Honest boundary: this contains a compromised agent, it does not promise the model can never be
compromised. Those are different layers and we do not conflate them. Production additionally seals
each session under gVisor. Happy to go deep on any of it.
```

### 4b. r/programming

**Title:**
```
We let an AI agent try to escape its sandbox 84 times. Here is what got contained.
```

**Body:**
```
We build IronClaw, an open-source runtime for AI agents with the isolation as the whole point.
Instead of asserting it is secure, we ran the experiment: assume the agent is fully compromised
(every soft defence bypassed) and let it run attacker code from inside its sandbox across 12
sessions.

84 escape attempts across four categories (network egress, host takeover,
filesystem, self-escalation). 84 blocked, 0 escaped.

The interesting part is why each one fails. There is nothing to bypass:
- network=none: the only interface inside the box is loopback, so exfiltration has nowhere to go.
- The Docker Engine socket is never mounted in, so there is no host takeover path to find.
- The host filesystem is not in the sandbox mount namespace.
- Each session binds in only its own key, so the master key and sibling keys are unreachable.
- Capability changes are held at an approval gateway instead of self-applied.

Two things make this checkable rather than a marketing claim: you can run the whole thing in about
a minute with zero credentials, and the same battery runs as a CI gate on every push with a
negative control that fails the build if it does not catch a deliberately weakened sandbox.

Repo and one-command demo: https://github.com/IronSecCo/ironclaw
Full writeup with the results table: [blog link]
```

### 4c. r/LocalLLaMA

**Title:**
```
Running local agents with real tool access? We stress-tested sandbox escape 84 times.
```

**Body:**
```
If you run local models as agents with shell and tool access, the sandbox is the part that
matters the day a prompt injection lands. We build IronClaw (open source, self-hosted) and wanted
hard data, so we let a fully jailbroken agent try to escape its sandbox across 12 sessions:
84 attempts, 84 blocked, 0 escaped.

It runs fully local and zero-credential: the demo uses a mock provider, no API key, no cloud. Same
isolation applies whether the model behind it is Ollama on your box or a hosted API. network=none
per sandbox, Docker socket never mounted in, per-session filesystem and key isolation, approval
gateway for capability changes.

One command reproduces it in about a minute:
  git clone https://github.com/IronSecCo/ironclaw && cd ironclaw
  examples/live-containment/run.sh

Repo: https://github.com/IronSecCo/ironclaw
```

### 4d. r/selfhosted

**Title:**
```
Self-hosting AI agents: we tried to escape our own sandbox 84 times and logged what held
```

**Body:**
```
For anyone self-hosting AI agents, the trust question is simple: when the agent gets compromised,
what stops it from reaching the rest of your box? We build IronClaw (open source, self-hosted,
one-command demo) and measured it instead of asserting it.

We assumed the agent is fully compromised and ran attacker code from inside its sandbox across 12
sessions. 84 escape attempts: exfiltrate over the network, seize the host via the
Docker socket, read the host filesystem, steal keys, self-escalate. 84 blocked, 0
escaped.

Every wall is structural: network=none, no Docker socket in the box, host filesystem outside the
mount namespace, per-session key binds, approval gateway for capability changes. Reproduce it with
zero credentials in about a minute:

  git clone https://github.com/IronSecCo/ironclaw && cd ironclaw
  examples/live-containment/run.sh

The same tests run as a CI gate on every push with a negative control, and every release ships a
signed containment report. Repo: https://github.com/IronSecCo/ironclaw
```

### 4e. X / Mastodon thread

```
1/
We let a fully jailbroken AI agent try to escape its sandbox 84 times.

84 blocked. 0 escaped.

Not heuristics. Structural walls. Here is what it tried and why each attempt hit a dead end. 🧵

2/
The setup: assume the worst. Prompt-injection defences, alignment, tool allow-lists all bypassed.
The agent is now running attacker code as its own user, inside the box.

The only question left: does the boundary hold?

3/
It tried to phone home.
network=none means the sandbox has no interface but loopback. A DNS lookup is not filtered, it
just has nowhere to go. No exfiltration path exists.

4/
It tried to seize the host via the Docker socket, the crown jewel.
The Engine socket is never mounted into the sandbox and there is no docker client inside it. There
is no privileged escape hatch to find, because it was never handed over.

5/
It tried to read the host filesystem and steal keys.
The host root is not in the sandbox mount namespace. Each session binds in only its own key. The
master key and sibling keys are simply not there to read.

6/
It tried to escalate itself by enabling a new capability.
The request did not apply. It was held at an approval gateway for a human to allow or deny.

7/
Why believe any of this? Because you can run it.
One command, zero credentials, about a minute. Watch each escape attempt get denied live:

  examples/live-containment/run.sh

8/
And it cannot silently rot. The same battery is a CI gate on every push, with a negative control
that fails the build if it does not catch a deliberately weakened sandbox. Every release ships a
signed containment report.

9/
Honest boundary: this contains a compromised agent. It does not promise the model can never be
compromised. Different layers. Production also seals each session under gVisor.

IronClaw, open source: https://github.com/IronSecCo/ironclaw
```

---

## 5. Visual (UXDesigner coordination)

The animated live-containment demo (`docs/assets/containment.svg` and the asciinema recording from
IRO-322) is the embed for the blog and the X thread lead. Coordination is filed as a child issue
to UXDesigner: produce or confirm a short animated cut of the `live-containment` run (each escape
attempt denied, ending on the containment summary) sized for social embed. Until that lands, the
blog embeds the existing `containment.svg`.

---

## 6. Provenance

- Data: `12` scale runs of `examples/red-team-escape/run.sh` against the offline demo control
  plane, aggregated from the per-run signed-report JSON (schemaVersion 1.0). Raw run logs and
  reports captured under the IRO-339 work session.
- Every invariant traces to `docs/threat-model.md` (STRIDE by boundary) and the assertion that
  proves it in `examples/red-team-escape/run.sh`.
- Verification owner for launch-day re-check: QA (WS-G).
