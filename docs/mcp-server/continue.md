---
title: "IronClaw MCP Server -- Continue.dev"
description: "Add IronClaw sandbox_exec to Continue.dev (VS Code) in 30 seconds. Continue's agent runs untrusted AI-generated shell inside a gVisor sandbox instead of on your host."
---

# Continue.dev

[Continue](https://continue.dev) is an open-source VS Code coding agent. In
agent mode it runs shell commands and edits files on your behalf. By default
those commands run on your host. Point Continue at IronClaw's `sandbox_exec`
tool and the agent runs untrusted, AI-generated commands inside an isolated
gVisor sandbox instead.

## Why sandbox Continue?

A coding agent executes code the model wrote, not code you reviewed. One bad
command can read your credentials, exfiltrate a token, or delete files. IronClaw
contains each command in an ephemeral gVisor container: no host filesystem, no
unexpected egress, destroyed after the session.

- See it stop real escape attempts: [nivardsec.com/containment](https://nivardsec.com/containment)
- Read the isolation benchmark: [Containment benchmark](../blog/containment-benchmark-docker-gvisor-e2b-daytona.md)

## 1. Install ironctl

```bash
brew install ironsecco/ironclaw/ironclaw
```

Verify:

```bash
ironctl version
```

## 2. Edit the config file

Continue uses YAML config. Open (or create) `~/.continue/config.yaml` and add
the `ironclaw` entry under `mcpServers`:

```yaml
mcpServers:
  - name: ironclaw
    type: stdio
    command: ironctl
    args:
      - mcp
      - serve
```

If you already have other MCP servers, add the `ironclaw` entry alongside them
in the same `mcpServers` list.

!!! note "MCP runs in agent mode"
    Continue only exposes MCP tools in **agent** mode. Switch the Continue chat
    to Agent before expecting `sandbox_exec` to appear.

## 3. Reload

Save the file. Continue reloads its config automatically. If the tool does not
appear, run **Developer: Reload Window** from the VS Code command palette.

## What sandbox_exec does

- Runs commands in an ephemeral container managed by IronClaw
- Uses gVisor for kernel-level isolation -- the AI cannot read your host files
  or make unexpected network calls
- Container is destroyed after the session ends

## Troubleshooting

**`ironctl: command not found`** -- Make sure `ironctl` is on your `$PATH`.
Run `which ironctl` to check. If you installed via Homebrew, run
`brew link ironsecco/ironclaw/ironclaw`.

**Tool does not appear** -- Confirm Continue is in Agent mode, check the
config for YAML indentation errors, and reload the VS Code window.
