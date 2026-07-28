---
title: "Environment variable reference"
description: Every named environment variable IronClaw reads, including provider credentials, sandbox settings, channel tokens, and ironctl tool overrides.
---

# Environment variable reference

IronClaw reads the following named environment variables in its production Go
commands and packages. Keep credentials in the host process environment. The
control plane uses them to authenticate upstream requests, but does not pass them
into agent sandboxes.

An empty value has the same effect as an unset value unless a row says otherwise.
A command-line flag overrides the corresponding environment-backed default.
Boolean variables accept `1`, `true`, `yes`, or `on`, case-insensitively.

## Model providers and gateway

| Variable | What it controls | Default | Applies to |
|---|---|---|---|
| `ANTHROPIC_API_KEY` | Enables Anthropic and supplies its host-held API key. | Unset; Anthropic requests have no injected credential. | Provider |
| `AWS_ACCESS_KEY_ID` | Enables AWS Bedrock when `AWS_SECRET_ACCESS_KEY` is also set. | Unset; Bedrock is disabled. | Provider |
| `AWS_DEFAULT_REGION` | Supplies the Bedrock region when `AWS_REGION` is unset. | `us-east-1` after both region variables are checked. | Provider |
| `AWS_REGION` | Selects the regional Bedrock runtime endpoint. | `AWS_DEFAULT_REGION`, then `us-east-1`. | Provider |
| `AWS_SECRET_ACCESS_KEY` | Supplies the host-held Bedrock signing secret. | Unset; Bedrock is disabled. | Provider |
| `AWS_SESSION_TOKEN` | Supplies an optional session token for temporary AWS credentials. | Unset. | Provider |
| `AZURE_OPENAI_ACCESS_TOKEN` | Supplies an Entra bearer token when no Azure API key is set. | Unset. | Provider |
| `AZURE_OPENAI_API_KEY` | Supplies the host-held Azure OpenAI API key. | Unset. | Provider |
| `AZURE_OPENAI_API_VERSION` | Overrides the Azure OpenAI data-plane API version. | `2024-10-21`. | Provider |
| `AZURE_OPENAI_ENDPOINT` | Enables Azure OpenAI and selects its `*.openai.azure.com` resource host. | Unset; Azure OpenAI is disabled. | Provider |
| `GEMINI_API_KEY` | Enables Google Gemini when `GOOGLE_API_KEY` is unset. | Unset. | Provider |
| `GOOGLE_API_KEY` | Enables Google Gemini and takes precedence over `GEMINI_API_KEY`. | Unset; Gemini is disabled unless the fallback is set. | Provider |
| `GOOGLE_CLOUD_LOCATION` | Supplies the Vertex AI location when `GOOGLE_VERTEX_LOCATION` is unset. | `us-central1` after both location variables are checked. | Provider |
| `GOOGLE_CLOUD_PROJECT` | Supplies the Vertex AI project when `GOOGLE_VERTEX_PROJECT` is unset. | Unset; a project is required for Vertex AI. | Provider |
| `GOOGLE_VERTEX_ACCESS_TOKEN` | Supplies a static host-held Vertex AI bearer token. | Unset. | Provider |
| `GOOGLE_VERTEX_LOCATION` | Selects the Vertex AI regional endpoint and takes precedence over `GOOGLE_CLOUD_LOCATION`. | `GOOGLE_CLOUD_LOCATION`, then `us-central1`. | Provider |
| `GOOGLE_VERTEX_PROJECT` | Selects the Vertex AI project and takes precedence over `GOOGLE_CLOUD_PROJECT`. | `GOOGLE_CLOUD_PROJECT`. | Provider |
| `GOOGLE_VERTEX_USE_GCLOUD` | Uses `gcloud` Application Default Credentials when set to exactly `1` and no static Vertex token is set. | Disabled. | Provider |
| `IRONCLAW_DEV_MODEL` | Sets the deployment-default model and the seeded development agent model. | Empty; the selected provider chooses its model default. | Control plane |
| `IRONCLAW_DEV_PROVIDER` | Sets the deployment-default provider and the seeded development agent provider. | Empty; the built-in default is Anthropic. | Control plane |
| `IRONCLAW_DEV_VERTEX_LOCATION` | Overrides the Vertex location for the deployment-default or seeded development agent. | `GOOGLE_VERTEX_LOCATION`. | Control plane |
| `IRONCLAW_DEV_VERTEX_PROJECT` | Overrides the Vertex project for the deployment-default or seeded development agent. | `GOOGLE_VERTEX_PROJECT`. | Control plane |
| `IRONCLAW_LOCAL_MODEL` | Sets the model ID for `IRONCLAW_LOCAL_MODEL_URL`. | Empty; required when the local model URL is set. | Provider |
| `IRONCLAW_LOCAL_MODEL_KEY` | Supplies an optional bearer key for a protected local OpenAI-compatible server. | Unset; no credential is injected. | Provider |
| `IRONCLAW_LOCAL_MODEL_URL` | Enables a local OpenAI-compatible server and sets its base URL. | Unset; local-model mode is disabled. | Provider |
| `IRONCLAW_MODEL_GATEWAY_HOSTS` | Lists comma-separated upstream hosts served by the credential gateway. | Empty. | Model gateway |
| `IRONCLAW_MODEL_GATEWAY_URL` | Routes model traffic through a host credential gateway. | Unset; providers connect directly through the model proxy. | Model gateway |
| `IRONCLAW_OLLAMA` | Enables the zero-credential Ollama provider. | `false`. | Provider |
| `IRONCLAW_OLLAMA_MODEL` | Sets the Ollama model ID. | `llama3.2`. | Provider |
| `OLLAMA_API_KEY` | Supplies an optional key for Ollama behind an authenticating proxy. | Unset; no credential is injected. | Provider |
| `OLLAMA_HOST` | Sets the Ollama host or URL. A bare host gets port `11434`. | `localhost:11434` over HTTP. | Provider |
| `OPENAI_API_KEY` | Enables OpenAI and supplies its host-held API key. | Unset; OpenAI is disabled. | Provider |
| `OPENROUTER_API_KEY` | Enables OpenRouter and supplies its host-held API key. | Unset; OpenRouter is disabled. | Provider |

## Sandbox and containment

| Variable | What it controls | Default | Applies to |
|---|---|---|---|
| `IRONCLAW_DOCKER_BINDS` | Lists comma-separated host-to-container base path mappings for the Docker development fallback. | Empty. | Docker sandbox fallback |
| `IRONCLAW_DOCKER_NETWORK` | Selects the Docker network for fallback sandbox containers. | Empty; Docker uses its default network behavior. | Docker sandbox fallback |
| `IRONCLAW_DOCKER_SOCKET` | Sets the Docker Engine Unix socket used by the fallback isolator. | `/var/run/docker.sock`. | Docker sandbox fallback |
| `IRONCLAW_DOCKER_USER` | Sets the `uid:gid` used by fallback sandbox containers. | `0:0`. | Docker sandbox fallback |
| `IRONCLAW_ENABLE_SANDBOX_EXEC` | Enables the control-plane `POST /v1/sandbox/exec` endpoint. | `false`. | Control plane |
| `IRONCLAW_RUNTIME` | Selects the OCI runtime used for sandboxes and by `ironctl doctor`. | `runsc`. | Control plane and CLI |
| `IRONCLAW_SANDBOX_EXEC_DOCKER` | Selects the container runtime CLI for one-shot `sandbox_exec` boxes. | `docker`. | Control plane |
| `IRONCLAW_SANDBOX_EXEC_IMAGE` | Sets the default image for one-shot `sandbox_exec` boxes. | `docker.io/library/alpine:3.20`. | Control plane |
| `IRONCLAW_SANDBOX_EXEC_RUNTIME` | Selects the OCI runtime for one-shot `sandbox_exec` boxes. | `runsc`. | Control plane |
| `IRONCLAW_SANDBOX_IMAGE` | Overrides the sandbox image reported and checked by onboarding. | `ironclaw-sandbox:latest`. | `ironctl onboard` |

## Channel adapters

| Variable | What it controls | Default | Applies to |
|---|---|---|---|
| `DISCORD_BOT_TOKEN` | Registers the Discord adapter with this bot token. | Unset; the adapter is not registered. | Control plane |
| `IRONCLAW_IMESSAGE_ENABLE` | Registers the iMessage adapter on macOS when set to exactly `1`. | Disabled. | Control plane |
| `IRONCLAW_MATTERMOST_WEBHOOK_URL` | Registers the Mattermost incoming-webhook adapter. | Unset; the adapter is not registered. | Control plane |
| `IRONCLAW_SIGNAL_CLI_URL` | Registers the Signal adapter and sets the signal-cli REST bridge URL. | Unset; the adapter is not registered. | Control plane |
| `IRONCLAW_SIGNAL_NUMBER` | Sets the sending phone number for the Signal adapter. | Empty. | Control plane |
| `IRONCLAW_TEAMS_WEBHOOK_URL` | Registers the Microsoft Teams incoming-webhook adapter. | Unset; the adapter is not registered. | Control plane |
| `SLACK_BOT_TOKEN` | Registers the Slack adapter with this bot token. | Unset; the adapter is not registered. | Control plane |
| `TELEGRAM_BOT_TOKEN` | Registers the Telegram adapter with this bot token. | Unset; the adapter is not registered. | Control plane |

## Control plane, MCP, and CLI

| Variable | What it controls | Default | Applies to |
|---|---|---|---|
| `APPDATA` | Locates Docker Desktop settings on Windows for the file-sharing check. | The process environment value; no Windows settings check when unset. | `ironctl doctor` |
| `HOME` | Adds the home directory to the minimal environment inherited by local MCP server processes. | The process environment value; omitted when unset. | MCP host process |
| `IRONCLAW_API_TOKEN` | Enables control-plane bearer authentication and supplies the default token used by `ironctl`. | Unset; control-plane API auth is disabled and the CLI sends no token. | Control plane and CLI |
| `IRONCLAW_CONFIG` | Overrides the onboarding token-file path used by the CLI and web console. | The platform user config directory under `ironclaw/onboard.env`. | Onboarding |
| `IRONCLAW_CONTROLPLANE_URL` | Selects thin-client mode and sets the delegated control-plane URL for `ironctl mcp serve`. | Unset; MCP serve runs its standalone sandbox backend. | `ironctl mcp serve` |
| `IRONCLAW_MCP_AUTH_TOKEN` | Sets bearer authentication for the `ironctl mcp serve` HTTP listener. | Unset; non-loopback HTTP binds are rejected. | `ironctl mcp serve` |
| `PATH` | Adds the executable search path to the minimal environment inherited by local MCP server processes. | The process environment value, or a fixed system path when unset. | MCP host process |

## Scan tool overrides

These variables choose helper binaries used by `ironctl scan`. The matching
command-line option, such as `--helm-bin`, takes precedence.

| Variable | What it controls | Default | Applies to |
|---|---|---|---|
| `AZ` | Selects the Azure CLI binary used as the Bicep fallback. | `az`. | `ironctl scan` |
| `BICEP` | Selects the standalone Bicep compiler. | `bicep`. | `ironctl scan` |
| `CDK` | Selects the AWS CDK binary. | `cdk`. | `ironctl scan` |
| `DOCKER` | Selects the Docker CLI used for container inspection. | `docker`. | `ironctl scan` |
| `HELM` | Selects the Helm binary used to render charts. | `helm`. | `ironctl scan` |
| `IRONCTL_SCAN_RUNTIME` | Selects container inspection via `auto`, `docker`, `podman`, or `nerdctl`. | `auto`. | `ironctl scan` |
| `KUBECTL` | Selects the kubectl binary used as the Kustomize fallback. | `kubectl`. | `ironctl scan` |
| `KUSTOMIZE` | Selects the standalone Kustomize binary. | `kustomize`. | `ironctl scan` |
| `NERDCTL` | Selects the nerdctl CLI used for container inspection. | `nerdctl`. | `ironctl scan` |
| `NOMAD` | Selects the Nomad CLI used to render HCL jobs. | `nomad`. | `ironctl scan` |
| `PODMAN` | Selects the Podman CLI used for container inspection. | `podman`. | `ironctl scan` |
| `TERRAFORM` | Selects the Terraform CLI used to render plans and state. | `terraform`. | `ironctl scan` |

## Dynamically named variables

Three configuration paths can read a variable whose name is not fixed in the
source:

- `ironctl mcp serve --token-env NAME` reads `NAME` for the delegated control-plane
  token. The default name is `IRONCLAW_API_TOKEN`.
- MCP catalog values may contain `${NAME}` references. IronClaw expands them from
  the host environment when the MCP server connects.
- Each vault-injector credential names its secret variable in the config file's
  `secretEnv` field. The injector reads that variable at startup and again after an
  approved rotation.

These values remain host-side. Do not put the secret itself in an MCP catalog or a
vault-injector config file.
