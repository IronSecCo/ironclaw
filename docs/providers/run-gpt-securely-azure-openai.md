---
title: "Run GPT securely with Azure OpenAI"
description: Run GPT models through Azure OpenAI inside a sealed gVisor sandbox. Your Azure api-key or Entra token stays host-side and never enters the agent container. Copy-paste setup and a link to the isolation proof.
---

# Run GPT securely with Azure OpenAI

You want to run **GPT** models, but your org reaches OpenAI only through **Azure
OpenAI** (Azure AI Foundry), and you need every agent run isolated from your
network and your subscription key. IronClaw's `azure` provider serves exactly that:
GPT deployments on your Azure resource, called by an agent that runs inside a
per-session **gVisor sandbox** with `network=none`, while your Azure credential
stays on the host.

!!! abstract "Where your Azure credential lives"
    The sandbox holds **no** credential. It reaches Azure only through the host
    **model-proxy** unix socket. The proxy stamps each forwarded request with your
    **`api-key`** header or a Microsoft **Entra** bearer token on the way out. Your
    `AZURE_OPENAI_API_KEY` never enters the sandbox image, its environment, or its
    filesystem. See [Security and isolation](../security-isolation.md).

## 1. See it run with no credentials first

Prove the sandbox loop end to end with the offline demo (one Docker command, no
key) via the
[zero-credential quickstart](../quickstart.md#a-working-chat-in-5-minutes-no-credentials),
then point a group at your Azure deployment below.

## 2. Enable Azure OpenAI host-side

Set your Azure OpenAI **endpoint** plus one credential in the **control-plane**
environment, never the sandbox. A static api-key or a Microsoft Entra ID bearer
token both work:

```bash
export AZURE_OPENAI_ENDPOINT=https://my-resource.openai.azure.com
export AZURE_OPENAI_API_KEY=…                 # or AZURE_OPENAI_ACCESS_TOKEN for Entra
export AZURE_OPENAI_API_VERSION=2024-10-21    # optional; provider default otherwise
```

Only `*.openai.azure.com` endpoints are accepted, so a misconfigured endpoint
cannot silently widen egress to an arbitrary host.

## 3. Point an agent group at Azure

Azure routes by **deployment name**, not model id, so set the group's model to the
name you gave the deployment in the Azure portal:

- **Provider:** `azure`
- **Model:** your Azure **deployment name** (for example `gpt-4o`)

The host and api-version are inherited from the deployment, so you usually do not
set them. For the full reference, including the failure table and Entra token
handling, see the [Azure OpenAI guide](azure.md).

## Why "securely" is the point

Azure OpenAI gives you GPT behind your Azure controls. IronClaw adds the second
half: the agent that calls GPT runs sealed.

- **gVisor per session.** Each conversation gets a fresh sandbox with `network=none`.
  A compromised agent cannot reach your tenant, other Azure services, or the public
  internet.
- **Least-privilege egress.** Only your `<resource>.openai.azure.com` host is
  allowlisted on the model-proxy.
- **Credential custody on the host.** The api-key or Entra token is stamped in the
  proxy. The agent sees a model reply, never the secret.

See the [containment and isolation proof](../security-isolation.md) and the full
[threat model](../threat-model.md).

## See also

- [Azure OpenAI guide](azure.md) - full setup, deployment routing, and failures
- [Choose your model provider](index.md) - capability matrix and decision guide
- [Run Claude in an isolated sandbox (AWS Bedrock)](run-claude-sandbox-bedrock.md)
- [Security and isolation](../security-isolation.md) - why credentials stay host-side
