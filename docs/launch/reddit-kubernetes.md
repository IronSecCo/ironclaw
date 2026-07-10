# r/kubernetes draft

Audience: cluster operators and platform engineers. Rewards a finding that maps
to securityContext / Pod Security they already reason about. Lead with the data,
tie it to workload defaults, keep the tool secondary. Self-post.

---

**Title:** Scored the default isolation of 54 popular container images. The gap maps almost 1:1 to the securityContext fields nobody sets.

**Body:**

I built a scanner that grades a container's runtime isolation from 0 to 100
across seven dimensions and ran it against 54 of the most-pulled images on their
stock config. On defaults, 43 of 54 scored a **D** (48/100), 11 scored a **C**,
and nothing scored above a C. nginx, postgres, redis, node, python, mongo, and
mysql are all an identical 48/100 out of the box.

What makes this a Kubernetes problem specifically: the points these images are
losing line up almost exactly with the `securityContext` and Pod Security fields
that most manifests leave unset.

- `runAsNonRoot` / a non-root UID: the single biggest lever. The only images that
  beat a D in this set (memcached, nginx-unprivileged, node-exporter at 63/C)
  are the ones that already drop root.
- `readOnlyRootFilesystem: true`
- `capabilities.drop: ["ALL"]`
- a `seccompProfile` (RuntimeDefault at minimum)
- `allowPrivilegeEscalation: false`

A cluster running the "restricted" Pod Security Standard already enforces most of
this, but plenty of workloads run "baseline" or unrestricted, and the base image
default is what you inherit when you do.

The scanner reads Kubernetes manifests directly (`ironctl scan --k8s`), so you
can grade a Deployment or a whole namespace's worth of workloads and get a
per-dimension breakdown of which controls are missing. It also has a `--fix`
mode that emits the concrete securityContext stanza to close each failed
dimension, and a GitHub Action that scores manifests on a PR and can gate merges
below a threshold.

Every one of the 54 images has a published scorecard, and the harness is
committed so the numbers are reproducible on your own images.

Scores directory and scanner: https://github.com/IronSecCo/ironclaw

If you think a dimension is weighted wrong for real cluster workloads, tell me,
the rubric is meant to be argued with.
