---
title: "Choose your model provider"
description: One page to pick the right model backend for IronClaw. Compare auth, streaming, and credential handling across mock, local, Anthropic, OpenAI, OpenRouter, Codex, Gemini, Vertex, Bedrock, and Azure, with a short decision guide and copy-paste setup.
---

# Choose your model provider

IronClaw runs your agent behind a sealed sandbox and talks to whatever model you
point it at. **You pick the backend per agent group** — the rest of the system does
not change. This page helps you choose one, then links you straight to setup.

!!! abstract "The one invariant, whatever you choose"
    Every provider credential is held **host-side** and injected by the host
    model-proxy on the way out. **No key ever enters a sandbox.** The agent sees a
    model reply, never the secret that paid for it. That is true for a static API
    key, an OAuth bearer, or AWS SigV4 credentials alike — the difference between
    providers is *where the credential comes from*, not *whether it stays host-side*.
    See [Security and isolation](../security-isolation.md).

## Pick in 15 seconds

<div class="grid cards" markdown>

-   :material-lan-disconnect: __Local / offline / zero credential__

    No cloud, no key, nothing leaves the box.

    → **`mock`** for a demo or CI, **`local`** (Ollama / LM Studio / vLLM) for a
    real model on your own hardware.

-   :material-cloud-outline: __Hosted, fastest to first token__

    You have an API key and want the strongest model now.

    → **`anthropic`** (default), **`openai`**, **`openrouter`**, or **`gemini`**.

-   :material-office-building-outline: __Enterprise / governed cloud__

    Billing, IAM, and data boundary must live in your cloud account.

    → **`azure`** (Azure OpenAI), **`bedrock`** (AWS), or **`vertex`** (Google Cloud).

-   :material-account-key-outline: __Reuse an existing subscription__

    You already pay for ChatGPT or a gateway-fronted credential.

    → **`codex`** (ChatGPT/Codex OAuth) via a local credential gateway.

</div>

## Set up in three steps

Pick a backend below. You get the exact host-side config, the `agent create` command, and a one-line isolation check, each ready to copy. Every provider follows the same shape: **set one credential, point a group at it, verify the seal.**

<div class="pp">
<div class="pp-live" aria-live="polite" role="status"></div>
<div class="pp-chips" role="radiogroup" aria-label="Model provider">
<p class="pp-cat">Local / offline</p>
<label class="pp-chip"><input type="radio" name="pp-provider" value="mock" data-panel="pp-panel-mock" checked> Mock</label>
<label class="pp-chip"><input type="radio" name="pp-provider" value="local" data-panel="pp-panel-local"> Local / Ollama</label>
<p class="pp-cat">Hosted (API key)</p>
<label class="pp-chip"><input type="radio" name="pp-provider" value="anthropic" data-panel="pp-panel-anthropic"> Anthropic</label>
<label class="pp-chip"><input type="radio" name="pp-provider" value="openai" data-panel="pp-panel-openai"> OpenAI</label>
<label class="pp-chip"><input type="radio" name="pp-provider" value="openrouter" data-panel="pp-panel-openrouter"> OpenRouter</label>
<label class="pp-chip"><input type="radio" name="pp-provider" value="gemini" data-panel="pp-panel-gemini"> Gemini</label>
<p class="pp-cat">Enterprise cloud (your billing / IAM)</p>
<label class="pp-chip"><input type="radio" name="pp-provider" value="azure" data-panel="pp-panel-azure"> Azure</label>
<label class="pp-chip"><input type="radio" name="pp-provider" value="bedrock" data-panel="pp-panel-bedrock"> Bedrock</label>
<label class="pp-chip"><input type="radio" name="pp-provider" value="vertex" data-panel="pp-panel-vertex"> Vertex AI</label>
<p class="pp-cat">Reuse a subscription</p>
<label class="pp-chip"><input type="radio" name="pp-provider" value="codex" data-panel="pp-panel-codex"> ChatGPT / Codex</label>
</div>
<div class="pp-panels">
<section class="pp-panel pp-active" id="pp-panel-mock" aria-label="Mock (offline, zero credential) setup">
<h4>Mock (offline, zero credential)</h4>
<p class="pp-panel-sub">Deterministic canned replies. No key, no network — the fastest way to see the full sandbox path.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code># no credential — the mock provider is fully offline</code></pre><button type="button" class="pp-copy" data-copy-label="the mock credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Mock Bot&quot; --provider mock --model mock-1 --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the mock agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-local" aria-label="Local model (Ollama, LM Studio, vLLM) setup">
<h4>Local model (Ollama, LM Studio, vLLM)</h4>
<p class="pp-panel-sub">Run a real model on your own hardware. Nothing leaves the box; most local servers need no key.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export IRONCLAW_LOCAL_MODEL_URL=http://localhost:11434/v1<span class="pp-comment">   # Ollama&#x27;s OpenAI-compatible endpoint</span></code></pre><button type="button" class="pp-copy" data-copy-label="the local credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Local Bot&quot; --provider local --model llama3.2 --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the local agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-anthropic" aria-label="Anthropic (default) setup">
<h4>Anthropic (default)</h4>
<p class="pp-panel-sub">The strongest default backend, with first-class tool use. One key and a restart.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export ANTHROPIC_API_KEY=sk-ant-...</code></pre><button type="button" class="pp-copy" data-copy-label="the anthropic credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Research Bot&quot; --provider anthropic --model claude-sonnet-4-5 --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the anthropic agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-openai" aria-label="OpenAI setup">
<h4>OpenAI</h4>
<p class="pp-panel-sub">GPT-class models over the OpenAI API.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export OPENAI_API_KEY=sk-...</code></pre><button type="button" class="pp-copy" data-copy-label="the openai credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;GPT Bot&quot; --provider openai --model gpt-4o --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the openai agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-openrouter" aria-label="OpenRouter setup">
<h4>OpenRouter</h4>
<p class="pp-panel-sub">One key, many models — route to any model OpenRouter fronts.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export OPENROUTER_API_KEY=sk-or-...</code></pre><button type="button" class="pp-copy" data-copy-label="the openrouter credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Router Bot&quot; --provider openrouter --model anthropic/claude-3.5-sonnet --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the openrouter agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-gemini" aria-label="Google Gemini (AI Studio) setup">
<h4>Google Gemini (AI Studio)</h4>
<p class="pp-panel-sub">Hosted Google models with a generous free tier.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export GOOGLE_API_KEY=...<span class="pp-comment">   # GEMINI_API_KEY is honored as a fallback</span></code></pre><button type="button" class="pp-copy" data-copy-label="the gemini credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Gemini Bot&quot; --provider gemini --model gemini-1.5-pro --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the gemini agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-azure" aria-label="Azure OpenAI setup">
<h4>Azure OpenAI</h4>
<p class="pp-panel-sub">GPT-class models under your Azure billing and identity. Model = your deployment name.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export AZURE_OPENAI_ENDPOINT=https://my-resource.openai.azure.com
export AZURE_OPENAI_API_KEY=...<span class="pp-comment">   # or AZURE_OPENAI_ACCESS_TOKEN for Entra</span></code></pre><button type="button" class="pp-copy" data-copy-label="the azure credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Azure Bot&quot; --provider azure --model gpt-4o --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the azure agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-bedrock" aria-label="AWS Bedrock setup">
<h4>AWS Bedrock</h4>
<p class="pp-panel-sub">Claude and others under your AWS billing and IAM, via host-side SigV4.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export AWS_ACCESS_KEY_ID=AKIA...
export AWS_SECRET_ACCESS_KEY=...
export AWS_REGION=us-east-1<span class="pp-comment">   # selects the bedrock-runtime.{region} host</span></code></pre><button type="button" class="pp-copy" data-copy-label="the bedrock credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Bedrock Bot&quot; --provider bedrock --model anthropic.claude-3-5-sonnet-20240620-v1:0 --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the bedrock agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-vertex" aria-label="Google Vertex AI setup">
<h4>Google Vertex AI</h4>
<p class="pp-panel-sub">Gemini under your GCP project, billing, and IAM. A project is required; region defaults when unset.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export GOOGLE_VERTEX_PROJECT=my-gcp-project<span class="pp-comment">   # or GOOGLE_CLOUD_PROJECT</span>
export GOOGLE_VERTEX_USE_GCLOUD=1<span class="pp-comment">   # use gcloud Application Default Credentials</span></code></pre><button type="button" class="pp-copy" data-copy-label="the vertex credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Vertex Bot&quot; --provider vertex --model gemini-1.5-pro --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the vertex agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
<section class="pp-panel" id="pp-panel-codex" aria-label="ChatGPT / Codex (via credential gateway) setup">
<h4>ChatGPT / Codex (via credential gateway)</h4>
<p class="pp-panel-sub">Reuse a ChatGPT/Codex OAuth subscription fronted by a local gateway (e.g. OneCLI). The control-plane holds no model credential at all.</p>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">1</span><span class="pp-step-title">Set the credential (host-side)</span></div>
<div class="pp-code"><pre><code>export IRONCLAW_MODEL_GATEWAY_URL=http://127.0.0.1:10255
export IRONCLAW_MODEL_GATEWAY_HOSTS=chatgpt.com</code></pre><button type="button" class="pp-copy" data-copy-label="the codex credential config"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Export in the control-plane's environment and restart it. This value stays host-side and never enters the sandbox.</p></div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">2</span><span class="pp-step-title">Create the agent</span></div>
<div class="pp-code"><pre><code>./bin/ironctl agent create --name &quot;Codex Bot&quot; --provider codex --model gpt-4o --yes</code></pre><button type="button" class="pp-copy" data-copy-label="the codex agent-create command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
</div>
<div class="pp-step"><div class="pp-step-head"><span class="pp-step-num" aria-hidden="true">3</span><span class="pp-step-title">Verify the isolation</span></div>
<div class="pp-code"><pre><code>./bin/ironctl doctor</code></pre><button type="button" class="pp-copy" data-copy-label="the isolation-check command"><span class="pp-copy-idle">Copy</span><span class="pp-copy-done">Copied ✓</span></button></div>
<p class="pp-step-note">Read-only preflight: confirms the gVisor sandbox and <code>network=none</code> egress before the first run. Then message the group in the console for a live reply.</p></div>
</section>
</div>
</div>

!!! tip "Prefer the console?"
    You can also set a group's provider in the web console (**Agents** &rarr; edit a group &rarr; **Provider**), then approve the change through the human gateway so it lands on the [audit log](../observability.md). The commands above are the CLI equivalent.

## Capability matrix

| Provider | Kind | Auth method | Credential source (host-side) | Streaming | Best for | Setup |
|---|---|---|---|---|---|---|
| **Mock** | `mock` | none | none — offline, deterministic | n/a | Demos, e2e tests, first run | [Quickstart](../quickstart.md#a-working-chat-in-5-minutes-no-credentials) |
| **Local / self-hosted** | `local` | none (optional key) | `IRONCLAW_LOCAL_MODEL_KEY` only if your server requires one | server-dependent | 100% local model, no data egress | [Ollama tutorial](../tutorials/local-model-ollama.md) |
| **Anthropic** _(default)_ | `anthropic` | API key | `ANTHROPIC_API_KEY` | yes (SSE) | Strongest default, tool use | [Quickstart](../quickstart.md#a-working-chat-in-5-minutes-no-credentials) |
| **OpenAI** | `openai` | API key | `OPENAI_API_KEY` | yes (SSE) | GPT-class models | [Setup](#anthropic-openai-openrouter) |
| **OpenRouter** | `openrouter` | API key | `OPENROUTER_API_KEY` | yes (SSE) | One key, many models | [Setup](#anthropic-openai-openrouter) |
| **Google Gemini** | `gemini` | API key | `GOOGLE_API_KEY` (or `GEMINI_API_KEY`) | yes (SSE) | Hosted Google models, generous free tier | [Setup](#gemini-google-ai-studio) |
| **Google Vertex AI** | `vertex` | OAuth2 bearer | gcloud ADC (`GOOGLE_VERTEX_USE_GCLOUD=1`) or `GOOGLE_VERTEX_ACCESS_TOKEN`; `GOOGLE_VERTEX_PROJECT` required | yes (SSE) | Gemini under GCP billing / IAM | [Setup](#vertex-ai-google-cloud) |
| **AWS Bedrock** | `bedrock` | AWS SigV4 | `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` (+ `AWS_SESSION_TOKEN`), `AWS_REGION` | no (InvokeModel) | Claude/others under AWS billing / IAM | [Setup](#aws-bedrock) |
| **ChatGPT / Codex** | `codex` | OAuth via gateway | credential gateway (`IRONCLAW_MODEL_GATEWAY_URL`, e.g. OneCLI) | yes (SSE) | Reuse a ChatGPT/Codex subscription | [Setup](#codex-chatgpt-via-a-credential-gateway) |
| **Azure OpenAI** | `azure` | api-key or Entra token | `AZURE_OPENAI_API_KEY` (or `AZURE_OPENAI_ACCESS_TOKEN`) + `AZURE_OPENAI_ENDPOINT` | yes (SSE) | GPT-class models under Azure billing / IAM | [Azure OpenAI](azure.md) |

!!! note "Streaming, and what it means here"
    Where a provider streams, IronClaw consumes the upstream token stream and
    **accumulates the full reply before it returns** — streaming is a transport
    detail on the host side, not a partial-render feature in the console. Bedrock
    uses the non-stream `InvokeModel` call today. Either way you get one complete,
    audited reply.

## Decision guide

- **Just want to see it work?** Start with **`mock`** — no key, no network. The
  [Quickstart](../quickstart.md) zero-credential demo replies in seconds.
- **Privacy or air-gap is the constraint?** Run **`local`** against Ollama, LM
  Studio, vLLM, or llama.cpp on your own box. Nothing leaves the machine, and no
  cloud credential is required. See the [local model tutorial](../tutorials/local-model-ollama.md).
- **You have an API key and want the best answer now?** Use **`anthropic`** (the
  default), **`openai`**, **`openrouter`**, or **`gemini`**. One environment
  variable and a restart.
- **Procurement, billing, and IAM must stay in your cloud?** Use **`azure`** (Azure
  OpenAI), **`bedrock`** (AWS), or **`vertex`** (Google Cloud). Credentials come from
  your existing cloud identity — an Azure api-key or Entra token, AWS SigV4, or gcloud
  ADC — and stay host-side.
- **Already paying for ChatGPT?** Front a **`codex`** OAuth credential with a local
  [credential gateway](../mcp.md) and reuse it.

## Setup by provider

Set **one** credential in the control-plane's environment and restart it. Every
value below lives host-side only.

### Anthropic, OpenAI, OpenRouter

```bash
export ANTHROPIC_API_KEY=sk-ant-…       # default provider
# or
export OPENAI_API_KEY=sk-…
# or
export OPENROUTER_API_KEY=sk-or-…
```

### Gemini (Google AI Studio)

```bash
export GOOGLE_API_KEY=…                  # GEMINI_API_KEY is honored as a fallback
```

### Vertex AI (Google Cloud)

Vertex speaks the same wire format as Gemini but authenticates with a short-lived
OAuth bearer that IronClaw refreshes host-side. A project is required; the region
defaults when unset.

```bash
export GOOGLE_VERTEX_PROJECT=my-gcp-project   # or GOOGLE_CLOUD_PROJECT
export GOOGLE_VERTEX_LOCATION=us-central1     # optional; provider default otherwise
export GOOGLE_VERTEX_USE_GCLOUD=1             # use gcloud Application Default Credentials
# or supply a token you refresh out of band:
# export GOOGLE_VERTEX_ACCESS_TOKEN=ya29.…
```

### AWS Bedrock

For orgs that consume models only through Bedrock. Credentials come from the
standard AWS environment; the region selects the regional
`bedrock-runtime.{region}.amazonaws.com` host.

```bash
export AWS_ACCESS_KEY_ID=AKIA…
export AWS_SECRET_ACCESS_KEY=…
export AWS_SESSION_TOKEN=…                # optional, for temporary credentials
export AWS_REGION=us-east-1               # or AWS_DEFAULT_REGION
```

!!! note "How Bedrock auth works"
    Host-side SigV4 signing is validated against AWS's reference vectors; requests
    use the non-stream `InvokeModel` API. The signature is region-bound, so
    `AWS_REGION` is required and no static credential ever enters the sandbox.

### Codex (ChatGPT) via a credential gateway

The Codex path routes model egress through a local, operator-vetted credential
gateway (such as OneCLI) that injects a ChatGPT/Codex OAuth token. The
control-plane and sandbox then hold **no** model credential at all.

```bash
export IRONCLAW_MODEL_GATEWAY_URL=http://127.0.0.1:10255
export IRONCLAW_MODEL_GATEWAY_HOSTS=chatgpt.com
```

### Azure OpenAI

For orgs that consume models only through Azure. Azure routes by **deployment name**
in the URL and authenticates with an `api-key` header or a Microsoft Entra bearer
token. See the full [Azure OpenAI guide](azure.md).

```bash
export AZURE_OPENAI_ENDPOINT=https://my-resource.openai.azure.com
export AZURE_OPENAI_API_KEY=…                 # or AZURE_OPENAI_ACCESS_TOKEN for Entra
export AZURE_OPENAI_API_VERSION=2024-10-21    # optional; provider default otherwise
```

## Point an agent group at a provider

A provider credential being present does not force any group to use it — you opt a
group in explicitly. Two surfaces:

- **Web console** → **Agents** → edit a group → **Provider** field (leave blank for
  the default Anthropic backend; set `openai`, `gemini`, `vertex`, `bedrock`,
  `local`, etc. to route that group elsewhere). The group's provider and model show
  on its card and in the first-run **Setup** checklist.
- **CLI** — submit a model/persona change with `ironctl` and approve it through the
  human gateway, so the switch lands on the [audit log](../observability.md) like any
  other change. See the [Quickstart](../quickstart.md#4-submit-a-change-watch-it-get-held).

Leaving the field blank keeps the sealed, single-provider default posture: only
groups that opt in reach another backend.

## Task-focused guides

Prefer to start from what you want to run? These pages lead with the job and give
you the exact setup:

- [Run Claude in an isolated sandbox (AWS Bedrock)](run-claude-sandbox-bedrock.md)
- [Run GPT securely (Azure OpenAI)](run-gpt-securely-azure-openai.md)
- [Run Llama locally (Ollama)](run-llama-locally-ollama.md)
- [Run any model (OpenRouter)](run-any-model-openrouter.md)

## See also

- [Quickstart](../quickstart.md) — first working chat, then a real provider
- [Bring your own model (Ollama / Gemini / Vertex)](../bring-your-own-model.md) — 5-minute setup per backend
- [Run a 100% local model (Ollama)](../tutorials/local-model-ollama.md)
- [Security and isolation](../security-isolation.md) — why keys stay host-side
- [FAQ: which providers are supported?](../faq.md#which-model-providers-are-supported)
