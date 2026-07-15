---
title: "How to harden a MinIO container: minio/minio scores 48/100 by default"
description: "minio/minio defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take an object store to its honest 89/100 grade B."
---

# How to harden a MinIO container (and is minio/minio safe for untrusted workloads?)

An object store is where your buckets live: backups, uploads, model weights, whatever your apps
stash. A stock `docker run minio/minio` is not the boundary that data deserves. Graded on IronClaw's
seven-dimension containment scale, the default configuration scores **48 of 100, grade D (porous)**.
Higher is safer. A few runtime flags take the same image to **89 of 100, grade B**, one point off an
A, and the one dimension it cannot reach is the one an S3 server needs by definition (its clients
must connect over the network). Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `minio/minio:latest`, the same data
> behind its [isolation scorecard](../scores/minio.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
minio/minio`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

For an object store, the two that should worry you most are **root** and **egress**. A MinIO
process that escapes as root escapes as root on the host, next to every bucket it just served. And a
MinIO process that can reach arbitrary destinations is one that can replicate your buckets out the
moment a bucket-notification target, a signed-URL bug, or an admin-API CVE lands code execution.

## Harden it: the exact `--fix` remediation

`ironctl scan my-minio --fix` prints one remediation per failed dimension, then one hardened run.
For `minio/minio`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. MinIO itself needs none of the defaults.
- **`--user 65532:65532`** (Non-root user, +15): pin a non-root uid so an escape does not begin as
  host uid 0. Point the data directory at a volume this uid owns.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/data` as an explicit writable volume. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for an
  S3 server, its clients must reach the API. Any named or bridge network scores 4 of 15 (a WARN, not
  a fail): a connection path exists. This is the one dimension an object store cannot max out.
  Contain it anyway: attach a user-defined network scoped to just the apps that read and write
  buckets, with no default route out, so a compromised MinIO cannot call arbitrary internet
  addresses.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name minio minio/minio server /data

# After: 89/100, grade B (scoped private network for its client apps)
docker run -d --name minio-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v minio-data:/data \
  --network=minio-internal \
  minio/minio server /data
```

Rescan: `ironctl scan minio-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because an S3 server exists to be connected to; `network=none` would score the last points but
leave nothing able to read a bucket. That is the honest ceiling for an object store, and it is a
long way from the default D.

## Verify it on your own MinIO

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-minio
ironctl scan my-minio --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the MinIO in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [minio isolation scorecard &rarr;](../scores/minio.md): the full dimension breakdown.
- [How to harden a Vault container &rarr;](harden-vault-container-isolation.md): another network service with the same honest ceiling, explained the same way.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
