---
title: "Run Claude in an isolated sandbox (AWS Bedrock)"
description: Run Anthropic Claude models through AWS Bedrock inside a sealed gVisor sandbox. Your AWS credentials stay host-side and never enter the agent container. Copy-paste setup and a link to the isolation proof.
---

# Run Claude in an isolated sandbox (AWS Bedrock)

You want to run **Anthropic Claude** models, but your org only reaches foundation
models through **AWS Bedrock**, and you want every agent run boxed off from your
network and your credentials. That is exactly what IronClaw's `bedrock` provider
does: it serves Claude (and any Bedrock model) to an agent that runs inside a
per-session **gVisor sandbox** with `network=none`, while your AWS keys stay on the
host.

!!! abstract "Where your AWS credentials live"
    The sandbox holds **no** AWS credential. It reaches Bedrock only through the host
    **model-proxy** unix socket. The proxy is the sole authenticator: it signs each
    forwarded request with **host-side SigV4** using your standard AWS environment
    credentials. `AWS_SECRET_ACCESS_KEY` never enters the sandbox image, its
    environment, or its filesystem. See [Security and isolation](../security-isolation.md)
    for how that boundary is enforced and proved.

!!! info "Bedrock is landing (beta)"
    The `bedrock` provider is in review. Host-side SigV4 signing is validated against
    AWS's reference vectors; requests use the non-stream `InvokeModel` API. This page
    documents the shipping shape. For providers already merged, see the
    [provider decision page](index.md).

## 1. See it run with no credentials first

Before you wire up Bedrock, prove the sandbox loop end to end with the offline
demo (one Docker command, no key). Follow the
[zero-credential quickstart](../quickstart.md#a-working-chat-in-5-minutes-no-credentials),
then come back here to point a group at Claude on Bedrock.

## 2. Set your AWS credentials host-side

Set standard AWS credentials in the **control-plane** environment, never the
sandbox. The region selects the regional
`bedrock-runtime.{region}.amazonaws.com` host:

```bash
export AWS_ACCESS_KEY_ID=AKIA…
export AWS_SECRET_ACCESS_KEY=…
export AWS_SESSION_TOKEN=…                # optional, for temporary credentials
export AWS_REGION=us-east-1               # or AWS_DEFAULT_REGION
```

## 3. Point an agent group at Claude

Opt a group in explicitly (a present credential never forces any group to use it):

- **Provider:** `bedrock`
- **Model:** a Bedrock model id, for example a Claude model id such as
  `anthropic.claude-3-5-sonnet-20241022-v2:0`

Set it on the group's **Provider** and **Model** fields in the web console, or
submit the change with `ironctl` and approve it at the human gateway so the switch
lands on the [audit log](../observability.md) like any other change.

## Why "in an isolated sandbox" is the point

Bedrock gives you Claude behind your AWS controls. IronClaw adds the second half:
the agent that calls Claude runs sealed.

- **gVisor per session.** Each conversation gets a fresh sandbox with a
  user-space kernel and `network=none`. A compromised agent cannot reach your VPC,
  the metadata endpoint, or the public internet.
- **Least-privilege egress.** Only your regional `bedrock-runtime` host is
  allowlisted on the model-proxy. Nothing else is reachable.
- **Credential custody on the host.** SigV4 signing happens in the proxy. The agent
  sees a model reply, never the keys that paid for it.

See the [containment and isolation proof](../security-isolation.md) and the full
[threat model](../threat-model.md).

## See also

- [Choose your model provider](index.md) - full capability matrix and decision guide
- [Run GPT securely (Azure OpenAI)](run-gpt-securely-azure-openai.md)
- [Run any model (OpenRouter)](run-any-model-openrouter.md)
- [Security and isolation](../security-isolation.md) - why credentials stay host-side
