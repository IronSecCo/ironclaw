# r/devops draft

Audience: platform and CI/CD engineers who already gate merges on lint, tests,
and coverage. They think in pipelines, policy-as-code, and blast radius. Lead
with the CI-gate story and the scored-images data, frame isolation as one more
gate you can actually enforce, not a vibe. Keep the security depth but speak
pipeline, not FUD.

---

**Title:** We scored the 54 most-pulled Docker images for container isolation on default settings. 43 of them are a D. Here is the data and the CI gate we built around it.

**Body:**

Most of us gate merges on tests, lint, and coverage, but almost nobody gates on
how isolated the container actually runs. So we scored it. We took the 54
most-pulled public images, ran them as they ship (plain defaults, no hardening),
and graded each one 0 to 100 across seven containment dimensions: user, caps,
seccomp, filesystem, network, PID, and runtime.

The result is a public directory you can search by image name:
https://nivardsec.com/scores

The short version: the average default score is 51 out of 100, the most common
grade is a D (43 of the 54 images), and every one of them jumps to 100 once you
apply the hardening the scorecard tells you to apply. postgres, nginx, redis,
node, mongo, all land at 48 out of 100 on their stock config. The gap is not
exotic, it is the defaults.

The tool that produces those grades is open source and runs in a pipeline:

- `ironctl scan <container>` grades a running container, a compose file, or a k8s
  manifest 0 to 100 and prints the failed dimensions with the exact fix.
- `ironctl scan --fix` rewrites the offending stanzas for docker, compose, or k8s
  so remediation is a diff, not a research project.
- There is a composite GitHub Action that posts a sticky scorecard comment on the
  PR and can fail the build below a minimum score, so isolation becomes a gate
  like any other.
- `--sarif` emits SARIF 2.1.0 so the findings land in GitHub code scanning next
  to your other security results.
- `--badge-json` produces a shields.io endpoint so you can put the score on your
  README and keep yourself honest.

This all falls out of IronClaw, a self-hosted runtime for AI agents where each
agent runs in its own gVisor (`runsc`) sandbox with `network=none`, a seccomp
allowlist, all caps dropped, a non-root user namespace, and a read-only rootfs.
The containment boundary ships with a red-team escape harness that runs as a CI
gate on every push, so the claim that a compromised workload stays contained is
tested, not asserted. But `ironctl scan` works on any container, you do not have
to adopt the runtime to use the scanner.

Deploy paths are the ones you already use: Fly, Render, and Railway templates, a
Helm chart for k8s, Docker, and a Homebrew formula. It is AGPLv3 plus commercial.

Repo: https://github.com/IronSecCo/ironclaw

I would like feedback from people who run this kind of gate at scale: is a 0 to
100 score the right shape for a merge gate, or do you want a pass or fail on
specific dimensions? And which images surprised you on the directory.

---

**Comment-reply seeds:**

- *How is the score computed?* Seven dimensions, each weighted: user namespace and
  non-root, dropped capabilities, seccomp profile, read-only and masked
  filesystem, network egress, PID namespace, and the runtime itself. It grades
  the container as configured, from `docker inspect` or the manifest, so it sees
  what will actually run, not what the image README claims.
- *Does gVisor change the score?* No. The scan is runtime-agnostic by design; it
  grades the isolation posture you configured, and gVisor is a separate defense
  layer, not a free bonus point. We did not want a number that flatters our own
  runtime.
- *Can I gate without adopting the whole runtime?* Yes. `ironctl scan` and the
  Action stand alone. Point them at your existing containers and you get the
  scorecard and the gate without running IronClaw at all.
- *Why do the defaults score so low?* Because base images ship permissive so they
  run anywhere. That is a reasonable default for the image author and a bad
  default for your blast radius. The `--fix` output is the same hardening you
  would apply by hand, just generated for you.
