# r/LocalLLaMA draft

Audience: local model runners who already give agents tool access and worry
about what those tools can reach. Lead with the local-first angle and the threat
model, not the marketing.

---

**Title:** I run every local agent in its own gVisor sandbox now, here is the open-source runtime that does it

**Body:**

If you run agents locally, you have probably already handed one shell access, a
file path, or an MCP tool and then thought about what happens if the model gets
prompt-injected into doing something you did not intend. A plain `docker run`
shares the host kernel, so one container escape is a host compromise. That gap
is what pushed me toward IronClaw.

IronClaw is a self-hosted runtime that assumes the agent itself could be
compromised and builds a boundary you can verify. Every conversation gets its
own gVisor (`runsc`) sandbox with:

- `network=none` by default, so no egress unless you grant it
- a seccomp syscall allowlist and all Linux capabilities dropped
- a non-root user namespace and a read-only rootfs
- the agent shipped as a compiled Go binary, so there is no source inside the
  box to rewrite

It runs fully local. You can point it at Ollama with zero credentials
(`localhost:11434`), or bring your own model. No key leaves your box, and the
sandbox has no network path out to leak one anyway.

The part I care about most: the containment claim is tested, not asserted. There
is a red-team escape harness that runs a compromised agent through escape,
exfiltration, and self-modification attempts, and it runs as a CI gate on every
push. If a change ever weakens the boundary, the build fails.

Cost of the sandbox is small and measured on a public CI runner: about +13 ms on
a warm respawn and +39 ms on a cold launch, paid once per sandbox, not per
request. The benchmark harness is committed so you can run it on your own
hardware.

AGPLv3 plus commercial. Feedback from people who actually run local agents is
what I want most, especially on the isolation model and where it is too strict or
not strict enough.

Repo: https://github.com/IronSecCo/ironclaw
Escape harness: https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape

---

**Comment-reply seeds (for likely questions):**

- *Why gVisor and not just Docker?* A plain container shares the host kernel.
  gVisor puts a user-space kernel between the agent and the host, so a syscall
  the sandbox does not allow never reaches the real kernel. It is a second
  boundary, not a replacement for good container hygiene.
- *Performance?* The measured cost is a one-time launch overhead, roughly +13 ms
  warm and +39 ms cold, not a per-request tax. In-sandbox compute overhead is
  near zero for light work. Numbers and the harness are on the benchmark page.
- *Does it work offline?* Yes. Point it at Ollama with no credentials, and with
  `network=none` the sandbox has no way out even if you wanted it to phone home.
