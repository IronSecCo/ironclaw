---
title: IronClaw — security-first, self-hosted AI agents
description: Security-first, self-hosted AI agents — isolation you can prove, not just promise. Run autonomous agents sealed off from the network, with a human-approval gateway no agent can bypass.
template: home.html
hide:
  - navigation
  - toc
---

<!--
  The homepage is rendered by overrides/home.html (set via `template:` above),
  which replaces the standard content area with the landing page. This Markdown
  body is intentionally a short text mirror of that page so the page still has
  meaningful `page.content` for tooling; the rich layout lives in the template
  and stylesheets/landing.css.
-->

# Security-first AI agents you actually run yourself.

A sandboxed runtime for untrusted AI agents. IronClaw runs autonomous AI assistants on
infrastructure you control, reachable from the chat apps you already use. Every agent runs sealed
off from the network (`network=none` in a gVisor sandbox) and cannot change its own configuration
without a human approving it. Isolation you can prove, not just promise.

- **[Run the zero-credential demo](quickstart.md)** — one command, no model key, no account.
- **[Star it on GitHub](https://github.com/IronSecCo/ironclaw)**, if the threat model earns it.
- **[Why IronClaw / vs. alternatives](comparison.md)** — how it compares, honestly.
- **[Security posture](security.md)** and **[threat model](threat-model.md)** — the trust story.
