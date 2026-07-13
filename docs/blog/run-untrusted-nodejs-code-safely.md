---
title: "How to run untrusted Node.js code safely: node:22-alpine scores 48/100 by default"
description: "node:22-alpine defaults score 48/100 (grade D): root, full caps, open egress. The exact flags that make it a 100/100 grade A sandbox for untrusted JavaScript."
---

# How to run untrusted Node.js code safely in a container

If you execute code you did not write, user-submitted snippets, an AI agent's tool calls, a
plugin, a build step from an untrusted repo, the container is your security boundary, not the
Node.js runtime. And a plain `docker run node:22-alpine` is a weak boundary. Graded on
IronClaw's seven-dimension containment scale it scores **48 of 100, grade D (porous)**. Higher
is safer. Six runtime flags take it to **100 of 100, grade A**. This guide shows the exact gaps
and fixes from the scan data, tuned for the untrusted-code case.

> Every number here comes from a read-only `docker inspect` of `node:22-alpine`, the same data
> behind its [isolation scorecard](../scores/node.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Why the default is not a sandbox

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run node:22-alpine`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Untrusted JavaScript makes every one of these gaps live. Open **egress** means a malicious
`npm install` or a prompt-injected agent can phone home and exfiltrate. **Root plus full
capabilities** means an escape (a native-addon exploit, a `vm`-escape, a kernel CVE) lands as
host root. A **writable rootfs** lets the payload drop persistence. The Node.js `vm` module is
explicitly documented as **not** a security mechanism; the container has to be.

## Harden it: the exact `--fix` remediation

`ironctl scan my-sandbox --fix` prints one remediation per failed dimension and one hardened
run. For untrusted `node:22-alpine`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability. Untrusted code
  should hold none.
- **`--user 65532:65532`** (Non-root user, +15): the official image ships a `node` user; pin a
  fixed non-root uid so an escape is not host root.
- **`--network=none`** (Network isolation, +11): this is the big one for untrusted code. No NIC
  but loopback means no exfiltration and no callback, full stop. If the code genuinely needs a
  specific endpoint, use an egress proxy with an allowlist instead of open `bridge`.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): read-only root with a small writable
  `tmpfs` for scratch. The payload cannot persist.
- **`--pids-limit` and `--memory`**: not scored dimensions, but add them so untrusted code
  cannot fork-bomb or OOM the host.

## Before and after

```bash
# Before: 48/100, grade D
docker run --rm node:22-alpine node untrusted.js

# After: 100/100, grade A
docker run --rm \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  --pids-limit=128 --memory=256m \
  -v "$PWD/untrusted.js:/untrusted.js:ro" \
  node:22-alpine node /untrusted.js
```

Rescan a persistent variant: `ironctl scan node-hardened` reports `100/100 grade A`. A
**48-point swing from a handful of one-line flags**, no image rebuild.

## When flags are not enough

Config flags harden the container, but they still run untrusted code on the **host kernel**. If
the threat model includes kernel exploits, put a user-space kernel under it. In our
[escape-suite benchmark](containment-benchmark-docker-gvisor-e2b-daytona.md), hardened runc
blocked 4 of 5 real escape attempts and gVisor (runsc) blocked 5 of 5. IronClaw runs every
agent session on gVisor with exactly the posture above by default, which is the point of the
project: a real sandbox for code you do not trust.

## Verify it yourself

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your sandbox, then print the fixes
ironctl scan my-sandbox
ironctl scan my-sandbox --fix
```

## Keep going

- [node:22-alpine isolation scorecard &rarr;](../scores/node.md): the full dimension breakdown.
- [Language runtimes, ranked by isolation &rarr;](../scores/collections/language-runtimes.md): how Node compares to Python, Go, Ruby, and the rest.
- [Escape suite vs Docker, gVisor, E2B, Daytona &rarr;](containment-benchmark-docker-gvisor-e2b-daytona.md): what config flags cannot stop.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
