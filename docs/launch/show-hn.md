# Show HN draft

Audience: Hacker News. Rewards technical honesty, reproducibility, and a founder
who answers hard questions in the thread. Overclaiming gets punished fast here,
so every line maps to shipped code.

---

**Title (must be 80 characters or fewer):**

`Show HN: IronClaw, run each AI agent in its own gVisor sandbox (AGPLv3)`

Alternate titles if the primary reads too long or too dry:

- `Show HN: A self-hosted AI agent runtime that sandboxes each agent in gVisor`
- `Show HN: IronClaw, we red-team our own AI agent sandbox as a CI gate`

**First comment (post right after the submission):**

Hi HN. IronClaw is a self-hosted runtime for autonomous AI agents. The premise
is that the agent itself could be compromised, by prompt injection or a bad tool
result, so it should run behind a boundary you can verify rather than one you
have to trust.

Each conversation runs in its own gVisor (`runsc`) sandbox: `network=none`, a
seccomp syscall allowlist, all Linux capabilities dropped, a non-root user
namespace, and a read-only rootfs. The agent ships as a compiled Go binary, so
there is no source inside the box to rewrite. gVisor matters here because a plain
container shares the host kernel, so a container escape is a host compromise. The
user-space kernel gives you a second, syscall-level boundary.

Two things I want to be judged on:

1. The containment claim is tested, not asserted. There is a red-team escape
   harness that hands a fully compromised agent a battery of escape,
   exfiltration, and self-modification attempts from inside the box: reach the
   Docker Engine socket, read host paths, break out to siblings, rewrite itself,
   open egress. Every core assertion holds, and the same script runs as a CI gate
   on every push, so a regression that weakens the boundary fails the build.

2. The performance numbers are reproducible and measured on a public CI runner,
   not a machine you cannot inspect. The gVisor cost is about +13 ms on a warm
   respawn and +39 ms on a cold launch, paid once per sandbox, not per request.
   The harness is committed so you can run it on your own hardware, and the memory
   footprint is deliberately reported as not-captured on the locked-down CI runner
   rather than faked as zero.

It is AGPLv3 plus commercial. It runs fully local (Ollama with zero credentials)
or with your own model. Credentials sit behind an approval gateway, so the agent
proposes and you grant. There is also `ironctl scan`, which grades a running
container's isolation from 0 to 100 so you can measure your own setup.

Repo: https://github.com/IronSecCo/ironclaw

I will be here to answer questions. The one thing I will not do is claim a
boundary we have not tested, so if you find a gap in the threat model I want to
hear it.

---

**Notes for the publisher:**

- Verify the chosen title is 80 characters or fewer before submitting. The
  primary title above is within the limit.
- HN convention: submit the repo as the URL, then post the first comment
  immediately so the top of the thread is context, not silence.
- Do not front-load adjectives. The interesting claim is that the sandbox is
  red-teamed as a CI gate. Let that carry the post.
- Be present in the thread for the first two hours. On HN, the founder answering
  fast and honestly is a bigger signal than the post copy.
