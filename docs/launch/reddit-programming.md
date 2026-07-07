# r/programming draft

Audience: general engineers, skeptical of security marketing, reward substance
and reproducibility. This subreddit prefers a link post pointing at something
concrete (the escape harness or the benchmark page) with a plain-language first
comment. Avoid hype words.

---

**Title:** We wrote an escape harness that tries to break our own AI agent sandbox, and made it a CI gate

**Link target (pick one):**

- The red-team escape harness: https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape
- Or the write-up: the "Breaking our own sandbox" page on the docs site

**First comment (post immediately after the link):**

Most "secure agent" claims are a sentence in a README. We wanted ours to be a
failing build if it stops being true, so we wrote the opposite of a happy-path
demo.

The setup: IronClaw runs each AI agent inside its own gVisor (`runsc`) sandbox,
one per conversation, with `network=none`, a seccomp syscall allowlist, all Linux
capabilities dropped, a non-root user namespace, and a read-only rootfs. Instead
of trusting that, we hand a fully compromised agent a battery of attacks from
inside the box:

- reach the Docker Engine socket (host takeover if it works)
- read arbitrary host paths
- break out to sibling containers
- rewrite its own binary
- open network egress to exfiltrate

Every core containment assertion holds: the Engine socket is never bound in, host
paths stay contained, egress is blocked. The important part is that the same
script is both the demo and a CI gate, so a regression that weakens the boundary
fails the build rather than shipping quietly.

Separately, the gVisor cost is measured on a public CI runner, not quoted from a
machine you cannot see: about +13 ms on a warm respawn and +39 ms on a cold
launch, paid once per sandbox launch, not per request. The benchmark harness is
committed so you can reproduce it on your own hardware.

It is self-hosted, AGPLv3 plus commercial, and the agent ships as a compiled Go
binary so there is no source inside the sandbox to modify. Repo:
https://github.com/IronSecCo/ironclaw

Happy to answer hard questions about the threat model. The one thing I will not
do is claim a boundary we have not tested.

---

**Notes for the publisher:**

- r/programming heavily favors link posts over self-posts. Lead with the harness
  or the write-up as the link, then drop the comment above.
- Do not editorialize the title with superlatives. The claim in the title is
  literally true and that is the whole appeal here.
