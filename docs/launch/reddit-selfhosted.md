# r/selfhosted draft

Audience: people who run their own infrastructure and want to own the stack.
Lead with self-hosting, data ownership, and deployment paths. Keep the security
depth but frame it as "you control this," not "trust us."

---

**Title:** IronClaw: self-hosted runtime for AI agents that sandboxes each one in gVisor, no vendor holds your keys

**Body:**

If you want to run autonomous AI agents but do not want a SaaS vendor holding
your API keys and your conversation data, this is built for that. IronClaw is a
self-hosted agent runtime where the isolation is the product, not an afterthought.

**What you actually run:** a control plane on your box plus a CLI (`ironctl`).
Each conversation spins up its own gVisor (`runsc`) sandbox with `network=none`,
a seccomp syscall allowlist, all Linux capabilities dropped, a non-root user
namespace, and a read-only rootfs. Nothing the agent does reaches your host
kernel directly, and by default it has no network path out.

**Why self-hosters like it:**

- Your keys and your data never leave your infrastructure. Credentials sit behind
  an approval gateway, so the agent proposes an action and you hold the grant.
- Bring your own model, including fully local via Ollama with zero credentials.
- Deploy how you already deploy: there are templates for Fly, Render, and
  Railway, a Helm chart for Kubernetes, Docker, and a Homebrew formula.
- It is AGPLv3 plus commercial, so you can read every line and run it forever.

**The trust part, stated honestly:** the containment boundary ships with a
red-team escape harness that runs a compromised agent through escape,
exfiltration, and self-modification attempts, and that harness runs as a CI gate
on every push. There is also `ironctl scan`, which grades a running container's
isolation from 0 to 100 across seven dimensions, so you can point it at your own
setup and see where it stands.

The sandbox overhead is small and measured on a public CI runner: about +13 ms
warm and +39 ms cold per launch, not per request.

Repo: https://github.com/IronSecCo/ironclaw

I would love feedback from people running this on their own boxes, especially on
the deployment paths and where the first-run experience has friction.

---

**Comment-reply seeds:**

- *How hard is it to stand up?* One control plane plus the CLI. There are one
  command deployment templates for the common PaaS options and a Helm chart for
  k8s. If you hit friction, tell me exactly where and it becomes an issue.
- *Does it phone home?* No. Self-hosted, and the per-conversation sandbox runs
  `network=none` by default, so the agent has no egress unless you grant it.
- *What is the catch with AGPLv3?* If you run a modified version as a network
  service you share your changes. For most self-hosters running it as-is, it just
  means you get the full source and can verify everything. A commercial license
  exists if the AGPL terms do not fit your use.
