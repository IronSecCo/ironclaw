---
title: "Google Gemini (AI Studio key)"
description: Run IronClaw against Google Gemini with a host-side AI Studio key. First-class gemini provider, per-group setup, and how the key stays out of the sandbox.
---

# Google Gemini provider

**`gemini`** runs IronClaw against Google Gemini through the Google Generative
Language API. Use it when you want Google's hosted Gemini models with the same
sandbox isolation and host-side credential handling used by the other cloud
providers.

Gemini uses its own streaming API, but IronClaw hides that behind the common
provider interface. The sandbox still talks only to the host model-proxy socket,
and the proxy injects your Google key on requests to
`generativelanguage.googleapis.com`.

!!! info "Where the credential lives"
    The sandbox has `network=none` and receives no Gemini key. Set the key only in
    the control-plane environment as `GOOGLE_API_KEY` or `GEMINI_API_KEY`. The
    model-proxy stamps it as `x-goog-api-key` only for Gemini requests.

## Prerequisites

- A Google AI Studio API key.
- A Gemini model id, such as `gemini-2.5-pro` or another model supported by the
  Generative Language API.
- A running IronClaw control-plane.

## 1. Enable Gemini on the control-plane

Set the key in the host control-plane environment:

```sh
export GOOGLE_API_KEY=...
```

`GEMINI_API_KEY` is also honored as a fallback. When either variable is set, the
control-plane allowlists `generativelanguage.googleapis.com` and installs the
Gemini key injector on the host model-proxy.

## 2. Point an agent group at Gemini

Pin a group to the `gemini` provider:

```sh
ironctl agent create --yes --id gemini-helper --name "Gemini Helper" \
  --provider gemini --model gemini-2.5-pro
```

Or make Gemini the deployment default for provider-less groups:

```sh
export IRONCLAW_DEV_PROVIDER=gemini
export IRONCLAW_DEV_MODEL=gemini-2.5-pro
```

If you omit `--model`, IronClaw uses its built-in Gemini default.

## 3. Verify

Send the group a small prompt and confirm that the model-proxy audit log records a
successful request to `generativelanguage.googleapis.com`.

Common failures:

| Symptom | Cause | Fix |
|---|---|---|
| `API key not valid` | missing or invalid Gemini key | Set `GOOGLE_API_KEY` or `GEMINI_API_KEY` in the control-plane environment |
| `model not found` | unsupported or misspelled model id | Use a Gemini model id available to your API key |
| `destination not on allowlist` | the key was unset when the control-plane started | Set the key and restart the control-plane |

## Security notes

- **Credential isolation.** The Gemini key stays on the host and is never copied
  into the sandbox image, environment, or filesystem.
- **Least-privilege egress.** Gemini requests are allowlisted only for
  `generativelanguage.googleapis.com`.
- **Provider parity.** Agent groups select Gemini with `--provider gemini`, the
  same way they select `openai`, `openrouter`, `ollama`, or another backend.

See [Choose your model provider](index.md) for the full provider comparison.
