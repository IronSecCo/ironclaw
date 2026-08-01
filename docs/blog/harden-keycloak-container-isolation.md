---
title: "How to harden a Keycloak container: keycloak scores 63/100 by default"
description: "keycloak defaults score 63/100 (grade C): full caps, writable rootfs. The exact ironctl scan --fix flags that take an identity server to its honest 89/100 grade B."
---

# How to harden a Keycloak container (and is quay.io/keycloak/keycloak safe as your IdP?)

Keycloak is the front door to every app behind it: it mints the tokens, holds the signing keys, and
stores the user directory that authenticates your whole stack. A stock `docker run
quay.io/keycloak/keycloak` is not the boundary that role deserves. Graded on IronClaw's
seven-dimension containment scale, the default configuration scores **63 of 100, grade C (partial)**.
Higher is safer. A few runtime flags take the same image to **89 of 100, grade B**, one point off an
A, and the one dimension it cannot reach is the one an identity provider needs by definition: every
app and browser must reach its endpoints. Here are the exact gaps and fixes from the scan data.

> Graded from a read-only inspect of a **running container** started from
> `quay.io/keycloak/keycloak` with plain `docker run` defaults, its entrypoint overridden with
> `sleep` purely to keep it alive. The scan itself executes nothing inside the container. It is the
> same data behind its [isolation scorecard](../scores/keycloak.md).
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
quay.io/keycloak/keycloak`, two fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as 1000 (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Keycloak already gets the hardest dimension right by dropping to uid 1000, which is why it starts a
full grade above most identity images. The two that remain are the **full capability set** and the
**writable rootfs**, and for an identity provider the stakes are the whole trust chain. A
protocol-parsing or provider CVE that lands code execution keeps CAP_NET_RAW to craft raw packets and
a writable rootfs to persist, next to the realm signing keys and the user store Keycloak guards.
Whoever holds those keys can mint a valid token for any user of any app behind Keycloak. This is the
same shape as a secrets server: the value of what it holds is what makes containing it
non-negotiable.

## Harden it: the exact `--fix` remediation

`ironctl scan my-keycloak --fix` prints one remediation per failed dimension, then one hardened run.
For `quay.io/keycloak/keycloak`:

- **`--user 65532:65532`** (Non-root user, already +15): the image already drops to uid 1000, so this
  dimension is closed by default. Keep a non-root uid pinned if you override the entrypoint; `--fix`
  emits 65532, the distroless nonroot uid.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; Keycloak serves its
  HTTP and management endpoints on high ports and needs none of the default set.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only. Build a
  configured image with `kc.sh build` ahead of time and point the data directory at a volume, so the
  running container needs no writable root.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for an
  identity provider, every app and browser must reach it. Any named or bridge network scores 4 of 15
  (a WARN, not a fail): a connection path exists. Contain it anyway: put Keycloak on a user-defined
  network scoped to just its database and the reverse proxy that fronts it, with no default route out,
  so a compromised process cannot call arbitrary internet addresses.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name keycloak quay.io/keycloak/keycloak:26.0 start

# After: 89/100, grade B (scoped private network for its database and proxy)
docker run -d --name keycloak-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=auth-internal \
  quay.io/keycloak/keycloak:26.0 start
```

Rescan: `ironctl scan keycloak-hardened` reports `89/100 grade B`. A **26-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because an identity provider exists to be reached by every app it authenticates; `network=none`
would score the last points but leave nothing able to connect. That is the honest ceiling for this
role, and it is a long way from the default C.

## Verify it on your own Keycloak

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-keycloak
ironctl scan my-keycloak --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Keycloak in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [keycloak isolation scorecard &rarr;](../scores/keycloak.md): the full dimension breakdown.
- [How to harden a Vault container &rarr;](harden-vault-container-isolation.md): the secrets server with the same honest ceiling and the same high stakes.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
