# r/devops draft

Audience: practitioners who ship and operate containers. Rewards a concrete
finding they can act on Monday, not a product pitch. Lead with the data from the
Container Isolation Scores directory, keep the tool secondary, invite them to
measure their own images. Self-post.

---

**Title:** I scored the default isolation posture of 54 popular Docker images. None got above a C.

**Body:**

I got tired of "is this base image secure" being a vibe, so I built a scanner
that grades a container's runtime isolation from 0 to 100 across seven
dimensions (user namespace, dropped capabilities, seccomp profile, read-only
rootfs, network exposure, and more), then ran it against 54 of the most-pulled
images on their stock `docker run` config.

The results, on defaults:

- 43 of 54 images scored a **D** (48/100). 11 scored a **C**. Nothing scored
  above a C.
- nginx, postgres, redis, node, python, mongo, and mysql are all an identical
  **48/100** out of the box.
- The only images that did better are the handful that ship non-root by default:
  memcached, nginx-unprivileged, and node-exporter land at **63/100 (C)**.

The point is not that these images are "insecure." It is that a default
`docker run` gives you almost none of the isolation the runtime can actually
enforce, and most pipelines never change it. Dropping to non-root alone is worth
15 points here. Adding a seccomp profile, dropping capabilities, and a read-only
rootfs move you a lot further, and none of that requires gVisor or a different
runtime.

Every image has a full per-dimension scorecard published, so you can see exactly
which controls each one is missing and what to change. The scan is reproducible
and the harness is committed, so you can point it at your own images or wire it
into CI as a gate. It runs against Docker, Compose, Kubernetes manifests,
Podman, nerdctl, and containerd.

`ironctl scan <image>` grades one container. `ironctl scan --compose` /
`--k8s` grade a whole stack. There is a GitHub Action that posts the scorecard
on a PR and can fail the build below a minimum score.

Full scores directory and the scanner: https://github.com/IronSecCo/ironclaw

Happy to defend the rubric. If a dimension is weighted wrong I would rather fix
it than argue it.
