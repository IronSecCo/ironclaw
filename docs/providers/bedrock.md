---
title: AWS Bedrock (Claude, Titan, Nova)
description: Run IronClaw against models served through Amazon Bedrock, with AWS SigV4 signed host-side. The sandbox never holds your AWS credentials.
---

# AWS Bedrock

Many organizations can only reach foundation models through **Amazon Bedrock** — a direct
Anthropic or OpenAI key is not allowed by policy, but a Bedrock-enabled AWS account is. IronClaw's
`bedrock` provider serves exactly that case. The primary target is **Claude on Bedrock**, which
speaks the same Anthropic Messages wire format IronClaw already uses; **Titan** and **Nova** ids
also work for text.

!!! info "Where the credential lives"
    As with every provider, the sandbox has `network=none` and holds **no** credential. It reaches
    Bedrock only through the host **model-proxy** unix socket. The proxy is the sole authenticator:
    it strips any sandbox-supplied auth and signs each forwarded request with **AWS Signature
    Version 4** using a host-held access key. Your `AWS_SECRET_ACCESS_KEY` never enters the sandbox
    image, its environment, or its filesystem.

## How it differs from the Anthropic provider

Claude on Bedrock is the Anthropic Messages API with a different envelope, all handled for you:

| | Anthropic (`anthropic`) | Bedrock (`bedrock`) |
|---|---|---|
| Host | `api.anthropic.com` | `bedrock-runtime.<region>.amazonaws.com` |
| Model id | in the request body | in the URL path (`/model/<id>/invoke`) |
| Schema marker | `anthropic-version` header | `anthropic_version: "bedrock-2023-05-31"` in the body |
| Auth | `x-api-key` header (host-side) | AWS SigV4 signature (host-side) |

The signature is **region-bound**, so the provider requires an explicit model host — there is no
safe default region. The control-plane fills it in for you from the deployment region (below).

## 1. Enable Bedrock on the control-plane

Set standard AWS credentials in the **control-plane** environment (never the sandbox). Long-lived
IAM keys or temporary STS credentials both work:

```sh
export AWS_ACCESS_KEY_ID=AKIA...
export AWS_SECRET_ACCESS_KEY=...
export AWS_SESSION_TOKEN=...          # only for temporary (STS / role) credentials
export AWS_REGION=us-east-1           # or AWS_DEFAULT_REGION; defaults to us-east-1
```

When `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` are both set, the control-plane:

- allowlists `bedrock-runtime.<region>.amazonaws.com` on the model-proxy egress allowlist, and
- installs the SigV4 injector that signs every forwarded Bedrock request with your credential.

Your AWS principal needs `bedrock:InvokeModel` on the target model, and the model must be enabled
in that region's Bedrock console ("Model access").

!!! warning "One region per deployment"
    The control-plane allowlists and signs for a single region (`AWS_REGION`). Point agent groups at
    that region's model ids. To serve a second region, run a second deployment or extend the
    allowlist.

## 2. Point an agent group at Bedrock

Pin a group to the `bedrock` provider and a Bedrock **model id**. The host is inherited from the
deployment region, so you usually do not set it:

- **Provider:** `bedrock`
- **Model:** a Bedrock model id or cross-region inference profile, e.g.
  `anthropic.claude-3-5-sonnet-20241022-v2:0` or `us.anthropic.claude-3-5-sonnet-20241022-v2:0`

Or make Bedrock the deployment default for provider-less groups:

```sh
export IRONCLAW_DEV_PROVIDER=bedrock
export IRONCLAW_DEV_MODEL=anthropic.claude-3-5-sonnet-20241022-v2:0
```

Under the hood the sandbox launches with
`--provider bedrock --model <id> --model-host bedrock-runtime.<region>.amazonaws.com` — the same
flags you can pass directly when running `cmd/sandbox` by hand.

## 3. Verify

Send the group a message. On success you get a normal reply; the model-proxy audit log records a
`200` to `bedrock-runtime.<region>.amazonaws.com`. Common failures:

| Symptom | Cause | Fix |
|---|---|---|
| `403 ... security token ... invalid` | wrong/expired credential or missing `AWS_SESSION_TOKEN` | refresh the credential in the control-plane environment |
| `403 ... not authorized to perform: bedrock:InvokeModel` | IAM policy or model access not granted | grant `bedrock:InvokeModel`; enable the model in the Bedrock console |
| `destination not on allowlist` | group's region differs from `AWS_REGION` | use a model id in the deployment region, or add the region |
| `ValidationException ... model identifier is invalid` | wrong model id for the region | copy the exact id from the Bedrock console / an inference profile |

## Security notes

- **Credential isolation.** The AWS secret key stays on the host. The sandbox is treated as
  potentially compromised and never receives it; the proxy strips any AWS auth the sandbox tries to
  send and injects a fresh SigV4 signature.
- **Least-privilege egress.** Only `bedrock-runtime.<region>.amazonaws.com` is allowlisted. The
  sandbox cannot reach any other AWS service or the public internet.
- **No plaintext secrets in logs.** The signer never logs credential material; a credential error
  leaves the request unsigned (the upstream rejects it) rather than surfacing the key.
