---
title: "How to harden a Vault container: hashicorp/vault:1.18 scores 48/100 by default"
description: "hashicorp/vault:1.18 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a secrets server to its honest 89/100 grade B."
---

# How to harden a Vault container (and is hashicorp/vault:1.18 safe for untrusted workloads?)

A secrets manager is the one process in your stack that must never be the weak link, because
everything it holds is, by definition, worth stealing. A stock `docker run hashicorp/vault:1.18` is
not the boundary that job deserves. Graded on IronClaw's seven-dimension containment scale, the
default configuration scores **48 of 100, grade D (porous)**. Higher is safer. A few runtime flags
take the same image to **89 of 100, grade B**, one point off an A, and the one dimension it cannot
reach is the one a secrets server needs by definition (its clients must connect to it). Here are the
exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `hashicorp/vault:1.18`, the same data
> behind its [isolation scorecard](../scores/vault.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
hashicorp/vault:1.18`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

For a secrets server, the two that should worry you most are **root** and **egress**. A Vault
process that escapes as root escapes as root on the host, next to every secret it just unsealed. And
a Vault process that can reach arbitrary network destinations is a Vault process that can quietly
ship its store to one the moment a plugin or auth-method CVE lands code execution.

## Harden it: the exact `--fix` remediation

`ironctl scan my-vault --fix` prints one remediation per failed dimension, then one hardened run.
For `hashicorp/vault:1.18`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. See the mlock note below, the one capability Vault may want back.
- **`--user 65532:65532`** (Non-root user, +15): pin a non-root uid so an escape does not begin as
  host uid 0. Point the storage directory at a volume this uid owns.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/vault/file` as an explicit writable volume. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  secrets server, its clients must be able to reach the API. Any named or bridge network scores 4 of
  15 (a WARN, not a fail): a connection path exists. This is the one dimension a secrets server
  cannot max out. Contain it anyway: attach a user-defined network scoped to just the apps that read
  from Vault, with no default route out, so a compromised Vault cannot call arbitrary internet
  addresses.

### The mlock note

Vault likes to `mlock` its memory so secrets are never swapped to disk, and `mlock` needs the
`IPC_LOCK` capability. With `--cap-drop=ALL` you have two honest choices:

- Add just that one capability back: `--cap-add=IPC_LOCK`. Still a world tighter than the default
  full set.
- Or disable mlock (`disable_mlock = true` in the Vault config) and make sure swap is off on the
  host so nothing hits disk anyway, the standard posture on a dedicated Vault node.

Either is fine; pick per your deployment. The score above reflects dropping the default set.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name vault hashicorp/vault:1.18

# After: 89/100, grade B (scoped private network for its client apps)
docker run -d --name vault-hardened \
  --user 65532:65532 \
  --cap-drop=ALL --cap-add=IPC_LOCK \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v vault-data:/vault/file \
  --network=vault-internal \
  hashicorp/vault:1.18
```

Rescan: `ironctl scan vault-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a secrets server exists to be connected to; `network=none` would score the last points
but leave nothing able to read a secret. That is the honest ceiling for a secrets server, and it is
a long way from the default D.

## Verify it on your own Vault

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-vault
ironctl scan my-vault --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Vault in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [vault:1.18 isolation scorecard &rarr;](../scores/vault.md): the full dimension breakdown.
- [How to harden a RabbitMQ container &rarr;](harden-rabbitmq-container-isolation.md): another network service with the same honest ceiling, explained the same way.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
