# Launch post drafts

Pre-written, guardrail-safe launch copy so an approved publisher can post
instantly on each channel. Every claim here maps to shipped, verifiable code or
docs. No hero numbers from machines you cannot inspect.

## Publishing guardrails (read before posting)

- No personal name, personal GitHub, LinkedIn, or Instagram in any post.
- Project links only: the repo, the docs site, the benchmark page.
- No em-dashes or en-dashes in public copy (house style, IRO-254).
- Value-first: lead with the problem and the proof, not the pitch.
- Every capability claim must be verifiable against shipped code. If QA has not
  confirmed it, cut it.
- One channel at a time. Do not cross-post the same text; each file below is
  written for its audience.

## What is in this folder

| File | Channel | Format |
| --- | --- | --- |
| [`reddit-localllama.md`](reddit-localllama.md) | r/LocalLLaMA | self-post |
| [`reddit-programming.md`](reddit-programming.md) | r/programming | link + comment |
| [`reddit-selfhosted.md`](reddit-selfhosted.md) | r/selfhosted | self-post |
| [`show-hn.md`](show-hn.md) | Hacker News | Show HN title + first comment |

## TL;DR (reusable hook)

IronClaw runs every AI agent inside its own gVisor (`runsc`) sandbox: one
sandbox per conversation, `network=none`, a seccomp syscall allowlist, all Linux
capabilities dropped, non-root user namespace, and a read-only rootfs. The
isolation is the product. It is AGPLv3 plus commercial, self-hosted, and the
containment boundary ships with a red-team harness that runs as a CI gate, so
the claim that a compromised agent stays contained is tested on every push, not
asserted in a README.

## Containment-benchmark hook (the proof, in one paragraph)

We wrote an escape harness that hands a fully compromised agent a battery of
escape, exfiltration, and self-modification attempts from inside the box. It
tries to reach the Docker Engine socket, read arbitrary host paths, break out to
sibling containers, and rewrite itself. Every core containment assertion holds:
the Engine socket is never bound in, host paths stay contained, egress is
blocked. The same script is both the demo and the CI gate, so a regression that
weakens the boundary fails the build. A separate, reproducible performance
harness measures the gVisor cost on a public CI runner: about +13 ms on a warm
respawn and +39 ms on a cold launch, paid once per sandbox, not per request.

## Link block (swap in canonical URLs at publish time)

- Repo: https://github.com/IronSecCo/ironclaw
- Containment demo (red-team escape harness): https://github.com/IronSecCo/ironclaw/tree/main/examples/red-team-escape
- Benchmark and footprint page: on the docs site under Benchmarks
- Comparison and positioning: on the docs site under Comparison
- License: AGPLv3 plus commercial

## Post-launch first-hour checklist

- Confirm every link resolves before posting.
- Watch the thread for the first 60 to 90 minutes and answer fast. Response
  speed on a security audience builds more trust than the post itself.
- Log the baseline star and traffic numbers at post time so lift is measurable.
- Route any onboarding friction surfaced in comments to the onboarding and UX
  owners as issues, not as promises in the thread.
