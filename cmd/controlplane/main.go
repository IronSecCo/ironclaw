// Command controlplane is the IronClaw host daemon entrypoint. It composes the
// control-plane subsystems and runs them until a signal arrives:
//
//   - structured logging (internal/obs) — text or JSON, secret-redacting;
//   - durable key custody (internal/host/keys) — a file-backed master key seals
//     per-session keys that survive a restart;
//   - the gateway (durable FileStore + append-only audit) with the deterministic
//     rejecters, the create_agent verifier/applier (RFC-0004), and the
//     AlwaysRequireHuman floor;
//   - the control-plane API, with Prometheus metrics exposed at /metrics;
//   - the model-proxy egress with rate caps, per-request audit (feeding metrics),
//     and response secret redaction;
//   - the live per-session lifecycle (SessionManager over the encrypted queue
//     factory + isolator), the maintenance sweep with respawn backoff, and the
//     outbound delivery loop;
//   - channel adapters (Slack/Discord/Telegram) registered when their bot token
//     is present in the environment.
//
// The API binds to a single address that SHOULD be the Tailscale (tailnet)
// interface so the control-plane has no public port.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/api"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/host/delivery"
	"github.com/IronSecCo/ironclaw/internal/host/egress"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/isolation"
	"github.com/IronSecCo/ironclaw/internal/host/keys"
	"github.com/IronSecCo/ironclaw/internal/host/mcp"
	"github.com/IronSecCo/ironclaw/internal/host/metrics"
	"github.com/IronSecCo/ironclaw/internal/host/modelproxy"
	"github.com/IronSecCo/ironclaw/internal/host/questions"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/router"
	"github.com/IronSecCo/ironclaw/internal/host/session"
	"github.com/IronSecCo/ironclaw/internal/host/skills"
	"github.com/IronSecCo/ironclaw/internal/host/sweep"
	"github.com/IronSecCo/ironclaw/internal/host/vaultinjector"
	"github.com/IronSecCo/ironclaw/internal/obs"
	"github.com/IronSecCo/ironclaw/internal/sandbox/tools"
	"github.com/IronSecCo/ironclaw/internal/version"
)

func main() {
	apiAddr := flag.String("api-addr", "127.0.0.1:8787",
		"control-plane API listen address (also serves /metrics) — set to the Tailscale (tailnet) IP in production so there is no public port")
	socket := flag.String("model-proxy-socket", defaultModelProxySocket(),
		"unix socket the model proxy listens on (bound into each sandbox)")
	stateDir := flag.String("state-dir", defaultStateDir(),
		"directory for durable control-plane state (gateway change store, audit log, per-session queues, keys)")
	runtimeBin := flag.String("runtime", envOr("IRONCLAW_RUNTIME", isolation.DefaultRuntimeBinary),
		"OCI runtime binary used to launch sandboxes (gVisor's runsc by default; IRONCLAW_RUNTIME=docker selects the runc fallback for hosts without gVisor, e.g. macOS Docker Desktop)")
	bundleRoot := flag.String("bundle-root", filepath.Join(defaultStateDir(), "bundles"),
		"directory under which per-session OCI bundles (config.json + rootfs) are written")
	sandboxImage := flag.String("sandbox-image", "ironclaw-sandbox:latest",
		"container image reference recorded in each sandbox's OCI spec")
	sweepInterval := flag.Duration("sweep-interval", 60*time.Second,
		"how often the maintenance sweep runs (stale-sandbox detection + due-message wake)")
	deliveryInterval := flag.Duration("delivery-interval", 2*time.Second,
		"how often the outbound delivery loop polls per-session outbound queues")
	logFormat := flag.String("log-format", "text",
		"structured log format: \"text\" (human-readable) or \"json\" (log shippers)")
	proxyRPS := flag.Float64("model-proxy-rps", 50,
		"model-proxy egress rate cap in requests/second (per session when the sandbox sends an X-Ironclaw-Session header, otherwise global); 0 disables the cap")
	proxyBurst := flag.Int("model-proxy-burst", 100,
		"model-proxy rate-cap burst size")
	egressSocket := flag.String("egress-socket", "",
		"OPT-IN: host unix socket for the egress broker, bound into each sandbox so an agent can reach approved external hosts (empty keeps the sandbox sealed to the model proxy)")
	egressAllow := flag.String("egress-allow", "",
		"comma-separated hostnames the egress broker permits (deny-by-default; only used with --egress-socket)")
	vaultEndpoint := flag.String("vault-endpoint", "",
		"OPT-IN: host-local credential-injector URL; enables vault://<cred>/<path> routing through the egress broker. Requires --egress-socket")
	vaultInjectorConfig := flag.String("vault-injector-config", "",
		"path to the JSON injector config (cred -> {upstream, secretEnv}); supplies the cred->upstream-host map the broker enforces vault policy against. Required with --vault-endpoint")
	vaultControlEndpoint := flag.String("vault-control-endpoint", "",
		"OPT-IN: the injector's rotation CONTROL surface (http://host:port or unix:/path), SEPARATE from --vault-endpoint; enables gateway-approved `ironctl vault rotate` to signal the injector to re-resolve a credential's held secret. No secret travels this channel")
	searchBackend := flag.String("search-backend", "",
		"web_search provider given to each sandbox: duckduckgo (keyless) or brave[:cred] (keyed via the vault). Requires --egress-socket; its host is auto-added to the egress allowlist. Empty disables web_search (--dev defaults it to duckduckgo)")
	skillsDir := flag.String("skills-dir", "",
		"curated skills source directory; setting it enables the host-side /v1/skills endpoints (empty disables skills)")
	skillsTrustKey := flag.String("skills-trust-key", "",
		"path to the minisign public-key file that skill bundles must be signed by (required when --skills-dir is set)")
	mcpCatalog := flag.String("mcp-catalog", "",
		"path to the MCP server catalog file; setting it enables MCP — the /v1/mcp endpoints, the per-session MCP broker, and the mcp_access change kind (empty disables MCP). --dev defaults it under the state dir")
	mcpIsolation := flag.String("mcp-isolation", "container",
		"how LOCAL (stdio) MCP servers run: \"container\" (hardened, network=none — production) or \"none\" (bare host process — UNISOLATED, dev only). --dev defaults it to none")
	mcpRuntime := flag.String("mcp-runtime", "",
		"OCI runtime for container MCP isolation, passed as docker --runtime (e.g. \"runsc\" for gVisor); empty uses the container CLI default")
	mcpImage := flag.String("mcp-image", "",
		"default container image for isolated local MCP servers when a server config sets no image")
	localModelURL := flag.String("local-model-url", os.Getenv("IRONCLAW_LOCAL_MODEL_URL"),
		"OPT-IN: base URL of a LOCAL OpenAI-compatible model server — Ollama (http://localhost:11434/v1), LM Studio, vLLM, or llama.cpp. Allowlists its host, forwards to a loopback host over plain HTTP, and makes it the deployment-default model — no cloud credential. Empty disables local-model mode")
	localModel := flag.String("local-model", os.Getenv("IRONCLAW_LOCAL_MODEL"),
		"model id served by --local-model-url (e.g. llama3.2); required when --local-model-url is set")
	dev := flag.Bool("dev", false,
		"seed the registry with a tiny dev owner/agent-group for local testing")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("ironclaw-controlplane " + version.String())
		return
	}

	// Structured, secret-redacting logger for the whole daemon.
	logger := obs.New(obs.Options{Format: obs.Format(*logFormat)}).Component("controlplane")

	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		logger.Error("create state dir", "dir", *stateDir, "error", err)
		os.Exit(1)
	}

	// Durable host key custody: the master key is loaded from (or created
	// at) a 0600 file, and the sealed per-session keys are mirrored to a durable
	// store, so session keys survive a control-plane restart instead of being lost
	// with a per-process master.
	masterPath := filepath.Join(*stateDir, "host-master.key")
	keySource, err := keys.NewFileKeySource(masterPath)
	if err != nil {
		logger.Error("key source", "path", masterPath, "error", err)
		os.Exit(1)
	}
	sealedPath := filepath.Join(*stateDir, "sealed-keys.json")
	sealedStore, err := keys.NewFileStore(sealedPath)
	if err != nil {
		logger.Error("sealed key store", "path", sealedPath, "error", err)
		os.Exit(1)
	}
	custodian, err := keys.NewDurable(keySource, sealedStore)
	if err != nil {
		logger.Error("keys", "error", err)
		os.Exit(1)
	}

	// Durable per-group vault policy (threat-model §11): persisted in its own
	// encrypted SQLCipher DB under the state dir so an approved {credential -> hosts}
	// grant survives a control-plane restart (deny-by-default still holds for any
	// group with no row). The DB key is derived from the host master with a distinct
	// purpose label — never the raw master or a per-session key — so it is stable
	// across restarts and isolated from the session keystore.
	master, err := keySource.Master()
	if err != nil {
		logger.Error("master key", "error", err)
		os.Exit(1)
	}
	vaultPolicyKey := keys.DeriveSubKey(master, "ironclaw/vault-policy-db/v1")
	vaultPolicyPath := filepath.Join(*stateDir, "vault-policies.db")
	vaultPolicies, err := registry.OpenDurableVaultPolicyStore(vaultPolicyPath, vaultPolicyKey)
	if err != nil {
		logger.Error("vault policy store", "path", vaultPolicyPath, "error", err)
		os.Exit(1)
	}
	defer vaultPolicies.Close()

	// Registry: the control-plane data model (in-memory dev backend until the
	// durable, encrypted store is selected at startup).
	reg := registry.NewMemRegistry()
	if *dev {
		seedDev(reg, logger)
		// Batteries-included local testing: open a DuckDuckGo-only egress path so the
		// web_search tool works out of the box. Production stays sealed — egress is
		// enabled only by an explicit --egress-socket — and even here it is deny-by-
		// default (only the search backend's host is allowlisted, below) and fully
		// audited. The egress socket sits next to the model-proxy socket so it rides the
		// same mount the sandbox already gets.
		if *egressSocket == "" {
			*egressSocket = filepath.Join(filepath.Dir(*socket), "egress.sock")
			logger.Warn("dev mode: opening brokered egress for web_search (deny-by-default; only the search backend host is allowlisted)",
				"egress_socket", *egressSocket)
		}
		if *searchBackend == "" {
			*searchBackend = "duckduckgo"
		}
		// Batteries-included MCP for local testing: a catalog under the state dir and the
		// UNISOLATED launcher (a dev box may have no container runtime or MCP image). The
		// daemon warns loudly that local MCP servers run unisolated; production sets
		// --mcp-catalog + keeps --mcp-isolation=container.
		if *mcpCatalog == "" {
			*mcpCatalog = filepath.Join(*stateDir, "mcp-catalog.json")
		}
		if *mcpIsolation == "container" {
			*mcpIsolation = "none"
			logger.Warn("dev mode: local MCP servers run UNISOLATED (--mcp-isolation=none); production should keep container isolation")
		}
	}

	// Metrics: pre-wired domain metrics, exposed at /metrics on the API.
	m := metrics.New()

	// Model egress: the sandbox has network=none, so the proxy is its only path to
	// the model host. It is the sole authenticator (host-held key injected per
	// request, never in the sandbox) and is hardened with an egress rate
	// cap, per-request audit that also feeds metrics, and response secret redaction.
	//
	// Multi-provider: each provider is enabled only when its credential is
	// present in the control-plane environment. The proxy then allowlists exactly
	// the enabled providers' hosts and injects the matching credential per upstream;
	// a per-agent-group provider selection is reachable only if its host is enabled
	// here. Anthropic is the primary; OpenAI and OpenRouter are opt-in and only
	// widen egress when their key is set.
	var (
		proxyOpts  []modelproxy.Option
		injectors  []modelproxy.Injector
		allowHosts []string
		redactKeys []string
	)
	addProvider := func(host string, inj modelproxy.Injector, key string) {
		allowHosts = append(allowHosts, host)
		injectors = append(injectors, inj)
		redactKeys = append(redactKeys, key)
	}
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		addProvider("api.anthropic.com", modelproxy.AnthropicInjector(apiKey, "2023-06-01"), apiKey)
	}
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		addProvider("api.openai.com", modelproxy.OpenAIInjector(apiKey), apiKey)
	}
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		addProvider("openrouter.ai", modelproxy.OpenRouterInjector(apiKey), apiKey)
	}
	// Google Gemini (Google AI Studio). GOOGLE_API_KEY is preferred; GEMINI_API_KEY
	// is honored as the Gemini-CLI-conventional fallback. Either enables the
	// generativelanguage.googleapis.com host and injects the key host-side. The
	// Gemini CLI's OAuth credential is instead fronted by the credential gateway
	// (IRONCLAW_MODEL_GATEWAY_URL) — the same `gemini` provider serves both.
	if apiKey := os.Getenv("GOOGLE_API_KEY"); apiKey != "" {
		addProvider("generativelanguage.googleapis.com", modelproxy.GeminiInjector(apiKey), apiKey)
	} else if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		addProvider("generativelanguage.googleapis.com", modelproxy.GeminiInjector(apiKey), apiKey)
	}
	// Google Cloud Vertex AI. Vertex speaks the identical Gemini wire format but auth
	// is an OAuth2 bearer (not an API key) and the project/region ride in the URL path,
	// so the allowlisted host is the regional {location}-aiplatform.googleapis.com. The
	// token is sourced host-side and refreshed there; the sandbox never holds it. Two
	// token sources, in precedence: a static GOOGLE_VERTEX_ACCESS_TOKEN (operator
	// refreshes out of band), else gcloud Application Default Credentials when
	// GOOGLE_VERTEX_USE_GCLOUD=1 (auto-refreshing, no extra dependency). A project is
	// required; the region defaults to the provider default when unset.
	if proj := envOr("GOOGLE_VERTEX_PROJECT", os.Getenv("GOOGLE_CLOUD_PROJECT")); proj != "" {
		var ts modelproxy.TokenSource
		if tok := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN"); tok != "" {
			ts = modelproxy.StaticTokenSource(tok)
		} else if os.Getenv("GOOGLE_VERTEX_USE_GCLOUD") == "1" {
			ts = &modelproxy.GcloudTokenSource{}
		}
		if ts != nil {
			loc := envOr("GOOGLE_VERTEX_LOCATION", os.Getenv("GOOGLE_CLOUD_LOCATION"))
			vHost := vertexAllowHost(loc)
			// The bearer is dynamic (refreshed per call), so it is not added to the
			// static redactKeys set; nothing host-side echoes it into a response body.
			addProvider(vHost, modelproxy.VertexInjector(ts), "")
			logger.Info("vertex ai enabled", "host", vHost, "project", proj)
		}
	}
	// AWS Bedrock. For orgs that consume models only through Bedrock (no direct
	// Anthropic/OpenAI keys). The primary target is Claude on Bedrock; it speaks the
	// Anthropic Messages wire format, but the model id rides in the URL path and auth
	// is AWS SigV4 signed host-side (not a static header), so the allowlisted host is
	// the regional bedrock-runtime.{region}.amazonaws.com. Credentials come from the
	// standard AWS environment (AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, optional
	// AWS_SESSION_TOKEN for temporary creds) and the region from AWS_REGION (else
	// AWS_DEFAULT_REGION, default us-east-1). The credential lives only on the host —
	// the injector signs each forwarded request; the sandbox never holds it.
	var bedrockDefaultHost string
	if ak := os.Getenv("AWS_ACCESS_KEY_ID"); ak != "" {
		if sk := os.Getenv("AWS_SECRET_ACCESS_KEY"); sk != "" {
			region := envOr("AWS_REGION", os.Getenv("AWS_DEFAULT_REGION"))
			if region == "" {
				region = "us-east-1"
			}
			bedrockDefaultHost = "bedrock-runtime." + region + ".amazonaws.com"
			creds := modelproxy.StaticCredentials{
				AccessKeyID:     ak,
				SecretAccessKey: sk,
				SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
			}
			// Redact the secret access key from any response body as a defense in
			// depth; it is never echoed by Bedrock but the guard is cheap.
			addProvider(bedrockDefaultHost, modelproxy.BedrockInjector(creds), sk)
			logger.Info("aws bedrock enabled", "host", bedrockDefaultHost, "region", region)
		}
	}
	// Azure OpenAI (Azure AI Foundry). For orgs that can consume models only through
	// Azure — a common enterprise constraint. Azure speaks the identical OpenAI Chat
	// Completions wire format, but the model is selected by a DEPLOYMENT name in the URL
	// path plus an api-version query param, and auth is the `api-key` header or a
	// Microsoft Entra ID bearer token, so the allowlisted host is the per-resource
	// {resource}.openai.azure.com derived from AZURE_OPENAI_ENDPOINT. Two auth modes, in
	// precedence: a static AZURE_OPENAI_API_KEY, else a static Entra bearer token in
	// AZURE_OPENAI_ACCESS_TOKEN (operator refreshes it out of band). The api-version
	// defaults to the provider default unless AZURE_OPENAI_API_VERSION is set. The
	// credential lives only on the host — the injector stamps each forwarded request;
	// the sandbox never holds it.
	var (
		azureDefaultHost       string
		azureDefaultAPIVersion = os.Getenv("AZURE_OPENAI_API_VERSION")
	)
	if ep := strings.TrimSpace(os.Getenv("AZURE_OPENAI_ENDPOINT")); ep != "" {
		azHost := azureEndpointHost(ep)
		if azHost == "" {
			logger.Error("invalid AZURE_OPENAI_ENDPOINT (want e.g. https://my-resource.openai.azure.com)", "value", ep)
		} else if key := os.Getenv("AZURE_OPENAI_API_KEY"); key != "" {
			azureDefaultHost = azHost
			addProvider(azHost, modelproxy.AzureKeyInjector(key), key)
			logger.Info("azure openai enabled", "host", azHost)
		} else if tok := os.Getenv("AZURE_OPENAI_ACCESS_TOKEN"); tok != "" {
			azureDefaultHost = azHost
			// The Entra bearer is dynamic (refreshed out of band by the operator), so it
			// is not added to the static redactKeys set; nothing host-side echoes it.
			addProvider(azHost, modelproxy.AzureTokenInjector(modelproxy.StaticTokenSource(tok)), "")
			logger.Info("azure openai enabled (entra token)", "host", azHost)
		} else {
			logger.Warn("AZURE_OPENAI_ENDPOINT set but no AZURE_OPENAI_API_KEY or AZURE_OPENAI_ACCESS_TOKEN; azure provider not enabled")
		}
	}
	// Local / self-hosted model (Ollama, LM Studio, vLLM, llama.cpp). When
	// --local-model-url is set, allowlist the server's host, reach a loopback host
	// over plain HTTP (these servers serve no TLS), and make it the deployment-default
	// model so a provider-less agent group runs fully local. No cloud credential is
	// required — a key is injected ONLY if the operator set IRONCLAW_LOCAL_MODEL_KEY
	// (e.g. a guarded vLLM). This is the "100% local, zero cloud credential" path.
	var (
		localModelHost  string
		localInsecure   []string
		localDefaultSel session.ModelSelection
	)
	if raw := strings.TrimSpace(*localModelURL); raw != "" {
		parsed, perr := url.Parse(raw)
		if perr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			logger.Error("invalid --local-model-url (want e.g. http://localhost:11434/v1)", "value", raw, "error", perr)
			os.Exit(1)
		}
		if strings.TrimSpace(*localModel) == "" {
			logger.Error("--local-model is required when --local-model-url is set (e.g. llama3.2)")
			os.Exit(1)
		}
		localModelHost = parsed.Host
		allowHosts = append(allowHosts, localModelHost)
		if parsed.Scheme == "http" {
			localInsecure = append(localInsecure, localModelHost)
		}
		localKeyed := false
		if key := os.Getenv("IRONCLAW_LOCAL_MODEL_KEY"); key != "" {
			injectors = append(injectors, modelproxy.LocalInjector(localModelHost, key))
			redactKeys = append(redactKeys, key)
			localKeyed = true
		}
		localDefaultSel = session.ModelSelection{Provider: "local", Model: *localModel, Host: localModelHost}
		logger.Info("local model enabled", "host", localModelHost, "model", *localModel, "scheme", parsed.Scheme, "credentialed", localKeyed)
	}

	credInjected := len(injectors) > 0
	if credInjected {
		proxyOpts = append(proxyOpts,
			modelproxy.WithInjector(modelproxy.MultiInjector(injectors...)),
			modelproxy.WithRedactedSecrets(redactKeys...),
		)
	}
	// Credential gateway: when IRONCLAW_MODEL_GATEWAY_URL is set,
	// route every upstream model call through that loopback CONNECT proxy — an
	// operator-vetted credential gateway such as OneCLI — which injects the real
	// provider credential. The sandbox AND this control-plane then hold no model
	// credential at all (none is injected here for the gateway's hosts). The hosts
	// the gateway serves (e.g. chatgpt.com for a ChatGPT/Codex token) are
	// allowlisted so the model proxy forwards them; the gateway URL may carry the
	// per-agent gateway token as Basic userinfo. Empty keeps the direct posture.
	if gwURL := os.Getenv("IRONCLAW_MODEL_GATEWAY_URL"); gwURL != "" {
		proxyOpts = append(proxyOpts, modelproxy.WithUpstreamGateway(gwURL, true))
		gwHosts := os.Getenv("IRONCLAW_MODEL_GATEWAY_HOSTS")
		for _, h := range strings.Split(gwHosts, ",") {
			if h = strings.TrimSpace(h); h != "" {
				allowHosts = append(allowHosts, h)
			}
		}
		logger.Info("model egress routed through credential gateway", "gateway_hosts", gwHosts)
	}

	if len(allowHosts) == 0 {
		// No provider credential configured (e.g. dev): keep Anthropic allowlisted so
		// the proxy still routes — the unauthenticated upstream rejects, not the proxy.
		allowHosts = []string{"api.anthropic.com"}
	}
	if *proxyRPS > 0 {
		proxyOpts = append(proxyOpts,
			modelproxy.WithRateCap(*proxyRPS, *proxyBurst),
			modelproxy.WithIdentity(func(r *http.Request) string { return r.Header.Get("X-Ironclaw-Session") }),
		)
	}
	proxyOpts = append(proxyOpts, modelproxy.WithAudit(func(rec modelproxy.AuditRecord) {
		m.ObserveModelCall(rec.Duration.Seconds(), rec.Status >= 400 || !rec.Allowed)
		logger.Info("model-proxy request",
			"host", rec.Host, "method", rec.Method, "status", rec.Status,
			"allowed", rec.Allowed, "rate_limited", rec.RateLimited, "duration_ms", rec.Duration.Milliseconds())
	}))
	if len(localInsecure) > 0 {
		// Plain-HTTP forwarding for the local loopback model server(s) only.
		proxyOpts = append(proxyOpts, modelproxy.WithInsecureUpstreams(localInsecure...))
	}
	proxy := modelproxy.New(allowHosts, proxyOpts...)

	// Egress broker: OPT-IN. With --egress-socket set, each sandbox gets
	// a second host unix socket to reach operator-approved external hosts
	// (deny-by-default), and with --vault-endpoint set, vault://<cred> credentials via
	// a host-local injector (a SEPARATE principal — the broker injects no secret).
	// Empty keeps the sealed default: the sandbox reaches only the model proxy.
	broker, err := buildEgressBroker(*egressSocket, *egressAllow, *vaultEndpoint, logger)
	if err != nil {
		logger.Error("egress broker", "error", err)
		os.Exit(1)
	}
	// Vault enforcement: with --vault-endpoint set, load the injector config so the
	// broker can enforce per-group policy against the credential's UPSTREAM host (the
	// host dimension of VaultPolicyStore.Allows). The injector config is the one fact
	// the broker side shares with the injector: cred -> upstream host. Fail fast on a
	// missing/invalid config so vault is never enabled without enforceable policy.
	var vaultCredHosts map[string]string
	if *vaultEndpoint != "" {
		if *vaultInjectorConfig == "" {
			logger.Error("vault", "error", "--vault-endpoint requires --vault-injector-config (cred->upstream host map for policy enforcement)")
			os.Exit(1)
		}
		cfg, cerr := vaultinjector.LoadConfig(*vaultInjectorConfig)
		if cerr != nil {
			logger.Error("vault injector config", "error", cerr)
			os.Exit(1)
		}
		vaultCredHosts = cfg.CredHosts()
		logger.Info("vault enforcement enabled", "credentials", len(vaultCredHosts))
	}
	// OPT-IN credential-secret rotation (IRO-144): with --vault-control-endpoint set,
	// an approved `vault rotate` change signals the injector's control surface to
	// re-resolve a credential's held secret from the host environment. The signaller is
	// the gateway's RotateCredentialFunc seam; nil leaves rotation unconfigured, so an
	// approved rotation fails loudly ("no rotation signaller wired") rather than silently
	// no-opping. No secret ever crosses this channel.
	var vaultRotateSignaller func(contract.AgentGroupID, string) error
	if *vaultControlEndpoint != "" {
		vaultRotateSignaller, err = newVaultRotateSignaller(*vaultControlEndpoint)
		if err != nil {
			logger.Error("vault control endpoint", "error", err)
			os.Exit(1)
		}
		logger.Info("vault credential rotation enabled")
	}
	// When a search backend is configured, approve its egress host so web_search is not
	// present-but-403 (selecting a backend IS the operator opting into reaching it). A
	// vault-routed backend returns "" here — its injector endpoint is allowlisted via
	// --vault-endpoint instead. Validated even when the broker is off, so a typo'd
	// backend fails loudly at startup rather than at first search.
	if *searchBackend != "" {
		host, err := tools.SearchBackendEgressHost(*searchBackend)
		if err != nil {
			logger.Error("search backend", "spec", *searchBackend, "error", err)
			os.Exit(1)
		}
		if broker == nil {
			// web_search rides the egress broker; without --egress-socket it can never
			// register in the sandbox. Warn loudly rather than fail so the rest of the
			// daemon still comes up.
			logger.Warn("--search-backend is set but egress is off (--egress-socket); web_search will NOT be available", "backend", *searchBackend)
		} else if host != "" {
			broker.Allow(host)
		}
	}

	// MCP: OPT-IN via --mcp-catalog. When enabled, operator-configured MCP
	// servers are reachable by gateway-approved agents through a PER-SESSION broker
	// socket — the sandbox never speaks MCP and never reaches a server directly. Local
	// (stdio) servers run in a hardened, network=none container unless
	// --mcp-isolation=none (dev). Every MCP call is enforced deny-by-default against the
	// approved grant and audited. Empty --mcp-catalog disables MCP entirely (no broker,
	// the mcp_access kind has no configured server so any grant is rejected).
	var (
		mcpCatalogStore *mcp.Catalog
		mcpBroker       *mcp.Broker
		mcpCancel       context.CancelFunc
	)
	if *mcpCatalog != "" {
		mcpCatalogStore, err = mcp.NewCatalog(*mcpCatalog)
		if err != nil {
			logger.Error("mcp catalog", "path", *mcpCatalog, "error", err)
			os.Exit(1)
		}
		launcher, isoDesc, lerr := buildMCPLauncher(*mcpIsolation, *mcpRuntime, *mcpImage)
		if lerr != nil {
			logger.Error("mcp isolation", "error", lerr)
			os.Exit(1)
		}
		var mcpCtx context.Context
		mcpCtx, mcpCancel = context.WithCancel(context.Background())
		mcpBroker = mcp.New(mcpCtx, mcpCatalogStore, mcpGrantsFor(reg),
			mcp.WithLauncher(launcher),
			mcp.WithAudit(func(rec mcp.AuditRecord) {
				logger.Info("mcp request",
					"session", rec.Session, "server", rec.Server, "tool", rec.Tool, "op", rec.Op,
					"allowed", rec.Allowed, "status", rec.Status, "bytes", rec.Bytes,
					"duration_ms", rec.Duration.Milliseconds(), "error", rec.Error)
			}),
		)
		logger.Info("mcp enabled", "catalog", *mcpCatalog, "isolation", isoDesc, "servers", len(mcpCatalogStore.List()))
	}
	if mcpCancel != nil {
		defer mcpCancel()
	}

	// Gateway: durable FileStore + append-only audit log. The chain is the
	// deterministic rejecters, then the create_agent verifier (RFC-0004 — always
	// holds a new agent for a human, never auto-approved), then the
	// AlwaysRequireHuman floor. The applier materializes an approved create_agent
	// into the registry and delegates every other kind to the log applier.
	storePath := filepath.Join(*stateDir, "changes")
	store, err := gateway.NewFileStore(storePath)
	if err != nil {
		logger.Error("gateway store", "path", storePath, "error", err)
		os.Exit(1)
	}
	auditPath := filepath.Join(*stateDir, "audit.jsonl")
	audit, err := gateway.NewAuditLog(auditPath)
	if err != nil {
		logger.Error("gateway audit log", "path", auditPath, "error", err)
		os.Exit(1)
	}
	defer audit.Close()

	agentExists := func(id contract.AgentGroupID) bool { _, ok := reg.GetAgentGroup(id); return ok }
	// mcpServerKnown gates ChangeMCPAccess: a grant may only name a server the operator
	// has configured in the catalog. With MCP disabled (no catalog) every server is
	// unknown, so any mcp_access change is rejected before the human floor.
	mcpServerKnown := func(server string) bool {
		if mcpCatalogStore == nil {
			return false
		}
		_, ok := mcpCatalogStore.Get(server)
		return ok
	}
	createAgent := func(id contract.AgentGroupID, name, folder string) error {
		return reg.PutAgentGroup(registry.AgentGroup{ID: id, Name: name, Folder: folder})
	}
	// Applier chain (apply-side): create_agent is materialized into the
	// registry; when egress is enabled, an approved change's egress grants (e.g. a
	// skill install's bundle) are materialized into the broker's allowlist so the
	// grant takes effect; every other kind is logged.
	// Per-group vault policy (threat-model §11): "which agent group may use which
	// credential against which host", deny-by-default. Mutated only here, through the
	// gateway apply path after a human approves a vault-policy change; read by the
	// broker/injector before a credential is used (broker enforcement is wired
	// separately). Backed by the durable, encrypted store opened above, so an
	// approved grant survives a restart (IRO-139).
	//
	// Wire the broker's vault guard: it enforces per-group policy on a HOST-TRUSTED
	// session->group mapping (the per-session socket identity, never the spoofable
	// header). The guard resolves the trusted session's group, looks up the
	// credential's approved upstream host, and consults the gateway-approved
	// VaultPolicyStore — deny-by-default on any miss. A revoked grant stops working
	// immediately (the store is read live).
	if broker != nil && *vaultEndpoint != "" {
		broker.SetVaultGuard(func(session, cred string) (string, bool) {
			host, known := vaultCredHosts[strings.ToLower(strings.TrimSpace(cred))]
			if !known {
				return "", false
			}
			sess, ok := reg.GetSession(contract.SessionID(session))
			if !ok {
				return host, false
			}
			return host, vaultPolicies.Allows(sess.AgentGroupID, cred, host)
		})
	}
	var capApplier contract.Applier = gateway.NewLogApplier()
	capApplier = gateway.NewPersonaApplier(func(id contract.AgentGroupID, persona string) error {
		return registry.SetPersona(reg, id, persona)
	}, capApplier)
	capApplier = gateway.NewEnabledToolsApplier(func(id contract.AgentGroupID, tools []string) error {
		g, ok := reg.GetAgentGroup(id)
		if !ok {
			return fmt.Errorf("agent group %q not found", id)
		}
		g.EnabledTools = tools
		return reg.PutAgentGroup(g)
	}, capApplier).WithCurrentTools(func(id contract.AgentGroupID) []string {
		// Enables the additive ({"add":[...]}) form so an agent asking for one more tool
		// unions it into the group's set instead of replacing the whole thing.
		if g, ok := reg.GetAgentGroup(id); ok {
			return g.EnabledTools
		}
		return nil
	})
	capApplier = gateway.NewSkillInstallApplier(func(id contract.AgentGroupID, name, version string) error {
		return registry.AddInstalledSkill(reg, id, name, version)
	}, capApplier)
	// An approved ChangeMCPAccess records the {server, tools} grant on the group so the
	// next launch exposes that server's approved tools through the per-session broker.
	capApplier = gateway.NewMCPAccessApplier(func(id contract.AgentGroupID, server string, tools []string) error {
		return registry.SetGrantedMCP(reg, id, server, tools)
	}, capApplier)
	// An approved vault-policy change records the per-group {credential -> hosts} rules
	// so subsequent vaulted calls are authorized deny-by-default. The target group is
	// the change's trusted AgentGroupID, never the payload.
	capApplier = gateway.NewVaultPolicyApplier(func(id contract.AgentGroupID, rules []gateway.VaultRule) error {
		rr := make([]registry.VaultRule, 0, len(rules))
		for _, r := range rules {
			rr = append(rr, registry.VaultRule{Credential: r.Credential, Hosts: r.Hosts})
		}
		return vaultPolicies.Set(registry.VaultPolicy{AgentGroupID: id, Rules: rr})
	}, capApplier)
	// An approved vault-rotate change signals the injector to re-resolve the named
	// credential's held secret. It carries no secret and mutates no control-plane state;
	// the side effect is entirely host-side in the injector.
	capApplier = gateway.NewVaultRotateApplier(vaultRotateSignaller, capApplier)
	// An approved ChangeMCPRegister lands the proposed server in the host catalog and
	// drops any cached broker connection so the next use reconnects with it. It grants
	// the proposing agent NOTHING — access stays the separate ChangeMCPAccess approval.
	capApplier = gateway.NewMCPRegisterApplier(func(cfg mcp.ServerConfig) error {
		if mcpCatalogStore == nil {
			return fmt.Errorf("mcp register: MCP is not enabled")
		}
		if err := mcpCatalogStore.Put(cfg); err != nil {
			return err
		}
		if mcpBroker != nil {
			mcpBroker.Invalidate(cfg.Name)
		}
		return nil
	}, capApplier)
	if broker != nil {
		capApplier = gateway.NewEgressApplier(broker, capApplier)
	}
	// Outermost applier: after a change is materialized (registry/broker mutations),
	// relaunch the target group's live sessions so an approved capability takes effect
	// on a running agent — not just on its next cold start. The session manager (the
	// respawner) is built below, so the hook is set post-construction.
	respawnApplier := gateway.NewRespawnApplier(nil, gateway.NewCreateAgentApplier(createAgent, capApplier))
	gw := gateway.New(
		gateway.VerifierChain{
			gateway.MountAllowlistVerifier{AllowedPrefixes: []string{filepath.Join(*stateDir, "mounts")}},
			gateway.PackageNameVerifier{},
			gateway.NewCreateAgentVerifier(agentExists),
			gateway.NewMCPServerVerifier(mcpServerKnown),
			gateway.VaultPolicyVerifier{},
			gateway.VaultRotateVerifier{},
			gateway.NewMCPRegisterVerifier(func() bool { return mcpCatalogStore != nil }),
			gateway.AlwaysRequireHuman{},
		},
		gateway.NewManualApprover(),
		respawnApplier,
		store,
	).SetAudit(audit).SetMetrics(m)

	// WithRegistry attaches the control-plane registry so the /v1/registry admin
	// endpoints are live and the approvals read-model (/v1/ui/approvals) can
	// resolve agent-group/requester names instead of showing raw ids.
	server := api.New(gw).WithHistory(store).WithAuditPath(auditPath).WithMetrics(m.Handler()).WithRegistry(reg).WithVault(vaultPolicies)
	// Optional bearer-token auth (defense-in-depth behind the tailnet). Read from
	// the host environment; never logged.
	apiToken := os.Getenv("IRONCLAW_API_TOKEN")
	if apiToken != "" {
		server = server.WithToken(apiToken)
	}

	// Isolation: per session, build a hardened OCI bundle and exec the runtime
	// (runsc/gVisor by default). With --runtime docker, launch a plain Docker
	// container instead (runc, NOT gVisor) for hosts without runsc (e.g. macOS dev):
	// the per-session queues/key + model-proxy socket reach the sandbox via the
	// shared volume binds in IRONCLAW_DOCKER_BINDS, at the same paths the control
	// plane uses. This relaxes the isolation boundary (kernel-shared) but keeps the
	// model credential host-side — use only for development.
	var isolator isolation.Isolator
	if *runtimeBin == "docker" {
		var binds []string
		for _, b := range strings.Split(os.Getenv("IRONCLAW_DOCKER_BINDS"), ",") {
			if b = strings.TrimSpace(b); b != "" {
				binds = append(binds, b)
			}
		}
		network := os.Getenv("IRONCLAW_DOCKER_NETWORK")
		isolator = isolation.NewDocker(
			envOr("IRONCLAW_DOCKER_SOCKET", "/var/run/docker.sock"),
			network,
			binds,
			envOr("IRONCLAW_DOCKER_USER", "0:0"),
		)
		logger.Warn("isolation runtime is Docker (runc, NOT gVisor) — development only",
			"network", network, "binds", strings.Join(binds, " "))
	} else {
		isolator = isolation.NewRunsc(
			isolation.WithRuntimeBinary(*runtimeBin),
			isolation.WithBundleRoot(*bundleRoot),
		)
	}

	// Per-session lifecycle: the SessionManager composes the encrypted queue
	// factory, the durable key custodian, and the isolator. It provides the
	// inbound-writer / outbound-reader factories the delivery loop uses and serves
	// as the sweep's Prober/Killer/Waker.
	factory := queue.NewFactory(filepath.Join(*stateDir, "sessions"))
	// Only hand the manager a non-nil MCP provisioner when MCP is enabled — a typed nil
	// *mcp.Broker in the interface would pass the manager's nil-check and then panic.
	var mcpProvisioner session.MCPProvisioner
	if mcpBroker != nil {
		mcpProvisioner = mcpBroker
	}
	// Vault enforcement requires a host-trusted session identity, so when vault is
	// enabled the manager provisions a PER-SESSION egress socket per sandbox (the
	// socket identity, not the spoofable header). Otherwise it binds the single shared
	// EgressSocket as before.
	var egressProvisioner session.EgressProvisioner
	if broker != nil && *vaultEndpoint != "" {
		egressProvisioner = broker
	}
	manager, err := session.New(session.Config{
		Factory:          factory,
		Keys:             custodian,
		Isolator:         isolator,
		Registry:         reg,
		SelectModel:      selectModelFromRegistry(reg, localDefaultSel, localModelHost, bedrockDefaultHost, azureDefaultHost, azureDefaultAPIVersion),
		ModelProxySocket: *socket,
		EgressSocket:     *egressSocket,
		EgressBroker:     egressProvisioner,
		SearchBackend:    *searchBackend,
		SkillsDir:        *skillsDir,
		MCPBroker:        mcpProvisioner,
		MCPSocketDir:     filepath.Join(filepath.Dir(*socket), "mcp"),
		EgressSocketDir:  filepath.Join(filepath.Dir(*socket), "egress"),
		Image:            *sandboxImage,
		KeyDir:           filepath.Join(*stateDir, "keys"),
		WorkspaceRoot:    filepath.Join(*stateDir, "workspaces"),
		OnLaunch:         m.SandboxLaunches.Inc,
	})
	if err != nil {
		logger.Error("session manager", "error", err)
		os.Exit(1)
	}

	// Wire the web console's terminate action to the SessionManager now
	// that it exists. Stop is idempotent; this is the only host-control surface the
	// read-only console exposes.
	server = server.WithSessionTerminator(manager.Stop)

	// Now that the live lifecycle exists, let an approved capability change relaunch the
	// affected group's running sessions so the grant takes effect immediately.
	respawnApplier.SetRespawner(manager)

	// Delivery: poll per-session outbound queues and deliver via channel adapters,
	// re-authorizing privileged system actions through the gateway. Concrete
	// platform adapters register when their bot token is configured.
	channelReg := channels.NewRegistry()
	registerChannelAdapters(channelReg, logger)

	// Chat playground: register an in-process webchat adapter so the
	// delivery loop routes an agent's "webchat" replies back to it for the browser
	// to poll, and instantiate the inbound Router (registry + the SessionManager's
	// inbound-writer factory + waker) so the API can feed a browser message into
	// the normal engage/route/deliver path. Additive — no existing adapter calls
	// the router, so the inbound posture is unchanged for them.
	webchat := channels.NewWebchatAdapter("webchat")
	if err := channelReg.Register(webchat); err != nil {
		logger.Error("register webchat adapter", "error", err)
	}
	chatRouter := router.New(reg, manager.InboundWriter, manager)
	server = server.WithChat(chatRouter, webchat)

	// Skills: when a curated source + trust root are configured, expose
	// the host-side /v1/skills endpoints so `ironctl skill add/list/remove` can install
	// a signed, gateway-approved capability bundle. Off by default (no --skills-dir),
	// in which case the daemon exposes no skills surface and a sandbox can never trigger
	// an install — only a host admin can, and only a human approves it.
	skillsResolver, err := buildSkillsResolver(*skillsDir, *skillsTrustKey)
	if err != nil {
		logger.Error("skills", "error", err)
		os.Exit(1)
	}
	if skillsResolver != nil {
		server = server.WithSkills(skillsResolver)
		logger.Info("skills enabled", "source", *skillsDir)
	}

	// MCP endpoints: live when MCP is enabled (--mcp-catalog). With it disabled
	// (nil catalog) the endpoints return 503, so the default daemon exposes no MCP
	// surface and a sandbox can never reach one.
	if mcpCatalogStore != nil {
		server = server.WithMCP(mcpCatalogStore, mcpBroker)
	}

	pendingQuestions := questions.NewStore()
	deliverer := delivery.New(channelReg, gw, reg, manager.OutboundReader).
		WithInboundWriter(manager.InboundWriter).
		WithQuestions(pendingQuestions).
		WithMetrics(m.Deliveries).
		WithLogger(logger.Logger)
	// In-session skill install (RFC-0006): when skills are enabled, let an agent PROPOSE
	// a curated, signed skill from chat. Delivery resolves+signature-verifies the named
	// skill through the SAME resolver the operator `ironctl skill add` path uses, then
	// routes the resolved ChangePermissions bundle to the gateway's mandatory human floor.
	// With skills disabled (no resolver) a sandbox skill_install proposal is refused.
	if skillsResolver != nil {
		resolver := skillsResolver
		deliverer = deliverer.WithSkillResolver(
			func(skill, version string, group contract.AgentGroupID, by contract.UserID) (contract.ChangeRequest, error) {
				return skills.InstallChange(resolver, skill, version, group, by)
			})
	}

	// Respawn backoff: wrap the SessionManager (which is the sweep's Prober
	// and Killer) so a crash-looping sandbox is tracked — each stuck-kill records a
	// failure, a healthy probe resets it, and after the failure ceiling the session
	// is parked (needs-human) via the escalation callback.
	respawn := sweep.DefaultRespawnBackoff().OnEscalate(func(id contract.SessionID, failures int) {
		logger.Warn("sandbox parked after repeated failures (needs human)", "session", id, "failures", failures)
	})
	prober := backoffProber{inner: manager, backoff: respawn}
	killer := backoffKiller{inner: manager, backoff: respawn, metrics: m, logger: logger}

	// Sweep: live hooks — Prober/Killer are the backoff-wrapped SessionManager; the
	// Waker is the SessionManager; the DueSource and recurrence Enqueue read/write
	// the per-session inbound queues via the factory.
	sweeper := sweep.New(reg, prober, killer).
		WithScheduling(
			sweep.NewQueueDueSource(reg, factory, custodian),
			manager,
			sweep.NewInboundEnqueue(factory, custodian).Enqueue,
		)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("API listening (bind to the tailnet IP in production)", "addr", *apiAddr)
		if err := server.Run(ctx, *apiAddr); err != nil && err != context.Canceled {
			logger.Error("API stopped", "error", err)
			stop()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("model proxy listening", "socket", *socket, "allowlist", strings.Join(allowHosts, ","))
		if err := proxy.Serve(ctx, *socket); err != nil && err != context.Canceled {
			logger.Error("model proxy stopped", "error", err)
			stop()
		}
	}()

	// Egress broker serve loop (only when --egress-socket configured).
	if broker != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("egress broker listening", "socket", *egressSocket, "vault", *vaultEndpoint != "")
			if err := broker.Serve(ctx, *egressSocket); err != nil && err != context.Canceled {
				logger.Error("egress broker stopped", "error", err)
				stop()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(*sweepInterval)
		defer ticker.Stop()
		logger.Info("sweep running", "interval", sweepInterval.String())
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := sweeper.Run(ctx); err != nil && err != context.Canceled {
					logger.Error("sweep pass error", "error", err)
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(*deliveryInterval)
		defer ticker.Stop()
		logger.Info("delivery polling", "interval", deliveryInterval.String())
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := deliverer.Poll(ctx); err != nil && err != context.Canceled {
					logger.Error("delivery poll error", "error", err)
				}
			}
		}
	}()

	pending, _ := store.Pending()
	logger.Info("started",
		"state_dir", *stateDir,
		"change_store", storePath, "pending_changes", len(pending),
		"audit_log", auditPath,
		"gateway_chain", "mount-allowlist -> package-name -> create-agent -> always-require-human",
		"metrics", "/metrics on the API",
		"registry", "in-memory", "dev", *dev,
		"custody", "durable (file master + sealed store)",
		"api_gated", apiToken != "",
		"model_proxy_socket", *socket, "cred_injection", credInjected,
		"model_proxy_rate_cap_rps", *proxyRPS, "model_proxy_burst", *proxyBurst,
		"channel_adapters", channelReg.List(),
		"runtime", *runtimeBin, "bundle_root", *bundleRoot,
		"sweep_interval", sweepInterval.String(), "delivery_interval", deliveryInterval.String(),
	)
	logger.Info("send SIGINT/SIGTERM to stop")

	<-ctx.Done()
	logger.Info("shutting down")
	// Stop any sandboxes the SessionManager launched before exiting.
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := manager.StopAll(stopCtx); err != nil {
		logger.Error("stop sandboxes", "error", err)
	}
	// Tear down the MCP broker: close session sockets and kill any spawned local servers.
	if mcpBroker != nil {
		mcpBroker.Close()
	}
	cancel()
	wg.Wait()
	os.Exit(0)
}

// backoffProber wraps a sweep.Prober so a healthy probe (a fresh heartbeat) clears
// the session's crash-loop state in the RespawnBackoff. It never changes the probe
// result — it only observes liveness to reset the backoff.
type backoffProber struct {
	inner   sweep.Prober
	backoff *sweep.RespawnBackoff
}

func (p backoffProber) Probe(id contract.SessionID) (int64, int64, error) {
	hb, claim, err := p.inner.Probe(id)
	// A present, fresh heartbeat means the sandbox is healthy → reset its backoff.
	if err == nil && hb >= 0 && hb <= sweep.HeartbeatStaleMs {
		p.backoff.Succeed(id)
	}
	return hb, claim, err
}

// backoffKiller wraps a sweep.Killer so every stuck-kill records a failure in the
// RespawnBackoff (escalating to parked/needs-human after the ceiling via the
// OnEscalate callback) and increments the sandbox-kill metric.
type backoffKiller struct {
	inner   sweep.Killer
	backoff *sweep.RespawnBackoff
	metrics *metrics.Metrics
	logger  *obs.Logger
}

func (k backoffKiller) Kill(id contract.SessionID, action sweep.StuckAction) error {
	st := k.backoff.Fail(id)
	if err := k.inner.Kill(id, action); err != nil {
		return err
	}
	k.metrics.SandboxKills.Inc()
	k.logger.Info("sandbox killed (stuck)",
		"session", id, "action", int(action), "consecutive_failures", st.Failures, "parked", st.Parked)
	return nil
}

// registerChannelAdapters registers the Slack/Discord/Telegram adapters whose bot
// token is present in the environment, so the daemon still boots with none set
// (e.g. in --dev). Tokens are read from the environment and never logged.
func registerChannelAdapters(reg *channels.Registry, logger *obs.Logger) {
	type adapterSpec struct {
		name string
		env  string
		make func(name, token string) channels.Adapter
	}
	specs := []adapterSpec{
		{"slack", "SLACK_BOT_TOKEN", func(n, t string) channels.Adapter { return channels.NewSlackAdapter(n, t) }},
		{"discord", "DISCORD_BOT_TOKEN", func(n, t string) channels.Adapter { return channels.NewDiscordAdapter(n, t) }},
		{"telegram", "TELEGRAM_BOT_TOKEN", func(n, t string) channels.Adapter { return channels.NewTelegramAdapter(n, t) }},
	}
	for _, s := range specs {
		token := os.Getenv(s.env)
		if token == "" {
			continue
		}
		if err := reg.Register(s.make(s.name, token)); err != nil {
			logger.Error("register channel adapter", "adapter", s.name, "error", err)
			continue
		}
		logger.Info("channel adapter registered", "adapter", s.name)
	}

	// Teams (Incoming Webhook), Signal (signal-cli REST bridge), and iMessage
	// (macOS Messages bridge) take richer config than a single bot token, so they
	// register explicitly. Each is env-gated and skipped when unconfigured;
	// none affects the daemon's boot when unset.
	reqExtra := func(name string, ok bool, make func() channels.Adapter) {
		if !ok {
			return
		}
		if err := reg.Register(make()); err != nil {
			logger.Error("register channel adapter", "adapter", name, "error", err)
			return
		}
		logger.Info("channel adapter registered", "adapter", name)
	}
	teamsURL := os.Getenv("IRONCLAW_TEAMS_WEBHOOK_URL")
	reqExtra("teams", teamsURL != "", func() channels.Adapter { return channels.NewTeamsAdapter("teams", teamsURL) })
	mattermostURL := os.Getenv("IRONCLAW_MATTERMOST_WEBHOOK_URL")
	reqExtra("mattermost", mattermostURL != "", func() channels.Adapter {
		return channels.NewMattermostAdapter("mattermost", mattermostURL)
	})

	signalURL := os.Getenv("IRONCLAW_SIGNAL_CLI_URL")
	reqExtra("signal", signalURL != "", func() channels.Adapter {
		return channels.NewSignalAdapter("signal", signalURL, os.Getenv("IRONCLAW_SIGNAL_NUMBER"))
	})
	reqExtra("imessage", runtime.GOOS == "darwin" && os.Getenv("IRONCLAW_IMESSAGE_ENABLE") == "1",
		func() channels.Adapter { return channels.NewIMessageAdapter("imessage") })
}

// defaultStateDir returns a per-user state directory under the OS state/cache
// location, falling back to a temp dir.
func defaultStateDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "ironclaw", "state")
	}
	return filepath.Join(os.TempDir(), "ironclaw-state")
}

// defaultModelProxySocket picks the host model-proxy unix-socket path. On Linux the
// daemon runs under systemd with RuntimeDirectory=ironclaw, so /run/ironclaw is the
// natural home (and matches the in-container mount target). Off-Linux (macOS
// development — there is no creatable /run at the SIP-protected volume root) it
// falls back to a user-writable cache dir so `--dev` runs without root. Production
// passes --model-proxy-socket explicitly (deploy/install.sh does). Mirrors the
// sandbox's defaultDirs (cmd/sandbox).
func defaultModelProxySocket() string {
	if runtime.GOOS == "linux" {
		return "/run/ironclaw/modelproxy.sock"
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "ironclaw", "run", "modelproxy.sock")
	}
	return filepath.Join(os.TempDir(), "ironclaw", "modelproxy.sock")
}

// buildEgressBroker constructs the opt-in egress broker. It returns (nil, nil) when
// socket is empty — egress is then disabled and sandboxes stay sealed to the model
// proxy. When a vaultEndpoint is given, it enables vault:// routing (the injector
// endpoint is itself allowlisted) plus audit correlation; the broker always strips
// credential-bearing headers from responses on the way back to the sandbox.
func buildEgressBroker(socket, allow, vaultEndpoint string, logger *obs.Logger) (*egress.Broker, error) {
	if socket == "" {
		return nil, nil
	}
	var hosts []string
	for _, h := range strings.Split(allow, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	opts := []egress.Option{
		egress.WithResponseRedactor(egress.NewRedactor()),
		egress.WithSessionIdentifier(func(r *http.Request) string { return r.Header.Get("X-Ironclaw-Session") }),
		egress.WithAudit(func(rec egress.AuditRecord) {
			logger.Info("egress request",
				"host", rec.Host, "path", rec.Path, "status", rec.Status, "allowed", rec.Allowed,
				"vault_cred", rec.VaultCredential, "correlation_id", rec.CorrelationID,
				"duration_ms", rec.Duration.Milliseconds())
		}),
	}
	if vaultEndpoint != "" {
		v, err := egress.NewVault(vaultEndpoint)
		if err != nil {
			return nil, fmt.Errorf("vault endpoint: %w", err)
		}
		opts = append(opts, egress.WithVault(v), egress.WithCorrelator(egress.NewCorrelator()))
		hosts = append(hosts, v.Endpoint()) // the host-local injector is allowlisted
	}
	return egress.New(hosts, opts...), nil
}

// buildMCPLauncher selects how LOCAL (stdio) MCP servers run. "container" wraps each
// server in a hardened, network=none container (the production posture); "none" runs it
// as a bare host process (dev only — UNISOLATED). It returns the launcher plus a short
// description for the startup log.
func buildMCPLauncher(isolation, runtime, image string) (mcp.Launcher, string, error) {
	switch isolation {
	case "none":
		return mcp.DirectLauncher{}, "none (UNISOLATED — dev only)", nil
	case "container", "":
		desc := "container (docker)"
		if runtime != "" {
			desc = "container (docker --runtime " + runtime + ")"
		}
		return mcp.ContainerLauncher{Runtime: "docker", OCIRuntime: runtime, DefaultImage: image}, desc, nil
	default:
		return nil, "", fmt.Errorf("--mcp-isolation must be \"container\" or \"none\", got %q", isolation)
	}
}

// mcpGrantsFor resolves a session to its agent group's gateway-approved MCP grants, so
// the broker exposes exactly the currently-approved surface — a revoked grant stops
// working immediately, with no relaunch needed.
func mcpGrantsFor(reg *registry.MemRegistry) mcp.GrantResolver {
	return func(session string) []mcp.Grant {
		sess, ok := reg.GetSession(contract.SessionID(session))
		if !ok {
			return nil
		}
		g, ok := reg.GetAgentGroup(sess.AgentGroupID)
		if !ok {
			return nil
		}
		out := make([]mcp.Grant, 0, len(g.GrantedMCP))
		for _, gr := range g.GrantedMCP {
			out = append(out, mcp.Grant{Server: gr.Server, Tools: gr.Tools})
		}
		return out
	}
}

// buildSkillsResolver constructs the curated, signature-verifying skills resolver
// from a source directory + a minisign trust-key file, using the COMPILED sandbox
// tool set for manifest validation (a skill can only enable tools the binary already
// implements). It returns (nil, nil) when sourceDir is empty — skills are then
// disabled and the daemon exposes no skills surface. It fails closed: a configured
// source with no trust key, an unreadable key, or an invalid key is an error.
func buildSkillsResolver(sourceDir, trustKeyPath string) (*skills.Resolver, error) {
	if sourceDir == "" {
		return nil, nil
	}
	if trustKeyPath == "" {
		return nil, fmt.Errorf("--skills-trust-key is required when --skills-dir is set")
	}
	key, err := os.ReadFile(trustKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read skills trust key: %w", err)
	}
	trust, err := skills.LoadTrustRoot(string(key))
	if err != nil {
		return nil, err
	}
	return &skills.Resolver{
		Source:     skills.DirSource{Root: sourceDir},
		Trust:      trust,
		KnownTools: tools.CompiledToolSet(),
	}, nil
}

// envOr returns the environment value for key, or def when it is unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// vertexAllowHost returns the Vertex AI host to allowlist for a region: the global
// (region-less) endpoint for "global", otherwise the regional
// {location}-aiplatform.googleapis.com. An unset location resolves to the same
// default region the sandbox provider applies (provider.defaultVertexLocation,
// "us-central1") so the allowlisted host matches the one the sandbox addresses.
func vertexAllowHost(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		location = "us-central1"
	}
	if location == "global" {
		return "aiplatform.googleapis.com"
	}
	return location + "-aiplatform.googleapis.com"
}

// azureEndpointHost extracts the host to allowlist from an AZURE_OPENAI_ENDPOINT. It
// accepts a full URL (https://my-resource.openai.azure.com[/...]) or a bare host
// (my-resource.openai.azure.com[:port]) and returns the lowercased host (without
// scheme/path/port), or "" if it does not look like an Azure OpenAI endpoint. Only
// *.openai.azure.com is accepted so a misconfigured endpoint cannot silently widen
// egress to an arbitrary host.
func azureEndpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	host := endpoint
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err != nil || u.Host == "" {
			return ""
		}
		host = u.Host
	} else if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !strings.HasSuffix(host, ".openai.azure.com") {
		return ""
	}
	return host
}

// selectModelFromRegistry resolves a session's model backend from its agent group
// : session -> agent group -> {Provider, Model}. A group pinned to an
// explicit Provider uses it; any group without one inherits the deployment default.
//
// The deployment default is IRONCLAW_DEV_PROVIDER/IRONCLAW_DEV_MODEL (empty = the
// built-in Anthropic backend). This makes the dev provider deployment-wide rather
// than only pinned on the seeded dev-agent. It matters in a gateway-only posture:
// there the model-proxy allowlist is just the gateway's hosts (e.g. chatgpt.com for
// Codex) and no Anthropic credential is enabled, so an agent created after the seed
// — with no Provider — would otherwise fall back to api.anthropic.com, which is not
// allowlisted, and every model call 403s ("destination not on allowlist").
// localDefault, when its Provider is non-empty, is the local-model deployment
// default (set by --local-model-url); it overrides the env-based default so a
// provider-less agent group runs 100% local. localHost backfills the loopback host
// for any group pinned to the "local" provider that did not carry one itself.
func selectModelFromRegistry(reg *registry.MemRegistry, localDefault session.ModelSelection, localHost, bedrockHost, azureHost, azureAPIVersion string) func(contract.SessionID) session.ModelSelection {
	def := session.ModelSelection{
		Provider:   os.Getenv("IRONCLAW_DEV_PROVIDER"),
		Model:      os.Getenv("IRONCLAW_DEV_MODEL"),
		Project:    envOr("IRONCLAW_DEV_VERTEX_PROJECT", os.Getenv("GOOGLE_VERTEX_PROJECT")),
		Location:   envOr("IRONCLAW_DEV_VERTEX_LOCATION", os.Getenv("GOOGLE_VERTEX_LOCATION")),
		APIVersion: os.Getenv("AZURE_OPENAI_API_VERSION"),
	}
	if localDefault.Provider != "" {
		def = localDefault
	}
	// A deployment defaulted to bedrock (IRONCLAW_DEV_PROVIDER=bedrock) needs the
	// regional host: the provider has no safe default host, so backfill the one the
	// deployment allowlisted and signs for.
	if strings.EqualFold(def.Provider, "bedrock") && def.Host == "" {
		def.Host = bedrockHost
	}
	// A deployment defaulted to azure (IRONCLAW_DEV_PROVIDER=azure) needs the
	// per-resource host and api-version: the provider has no safe default host, so
	// backfill the ones the deployment allowlisted/configured.
	if strings.EqualFold(def.Provider, "azure") {
		if def.Host == "" {
			def.Host = azureHost
		}
		if def.APIVersion == "" {
			def.APIVersion = azureAPIVersion
		}
	}
	return func(id contract.SessionID) session.ModelSelection {
		sess, ok := reg.GetSession(id)
		if !ok {
			return def
		}
		g, ok := reg.GetAgentGroup(sess.AgentGroupID)
		if !ok {
			return def
		}
		if g.Provider == "" {
			return def
		}
		sel := session.ModelSelection{Provider: g.Provider, Model: g.Model, Project: g.Project, Location: g.Location, APIVersion: g.APIVersion}
		// A group pinned to the local provider but without its own host inherits the
		// deployment's configured loopback host so it reaches the same local server.
		if strings.EqualFold(sel.Provider, "local") && sel.Host == "" {
			sel.Host = localHost
		}
		// Likewise a bedrock-pinned group without its own host inherits the
		// deployment's regional Bedrock host (which is what the proxy signs for).
		if strings.EqualFold(sel.Provider, "bedrock") && sel.Host == "" {
			sel.Host = bedrockHost
		}
		// A group pinned to azure but without its own host / api-version inherits the
		// deployment's per-resource Azure host and configured api-version (which is what
		// the proxy allowlisted and the sandbox builds the URL from).
		if strings.EqualFold(sel.Provider, "azure") {
			if sel.Host == "" {
				sel.Host = azureHost
			}
			if sel.APIVersion == "" {
				sel.APIVersion = azureAPIVersion
			}
		}
		return sel
	}
}

// seedDev inserts a minimal owner, agent group, and DM messaging-group wiring so a
// local operator can exercise the pipeline without a real platform.
func seedDev(reg *registry.MemRegistry, logger *obs.Logger) {
	const (
		owner   = "cli:dev"
		groupID = "dev-agent"
	)
	grp := registry.AgentGroup{ID: groupID, Name: "Dev Agent", Folder: "dev-agent"}
	// Optionally pin the dev group's model backend so a local chat works without a
	// gateway-approved persona/provider change (e.g. IRONCLAW_DEV_PROVIDER=codex,
	// IRONCLAW_DEV_MODEL=gpt-5.5). Empty keeps the default Anthropic backend.
	if p := os.Getenv("IRONCLAW_DEV_PROVIDER"); p != "" {
		grp.Provider = p
		grp.Model = os.Getenv("IRONCLAW_DEV_MODEL")
		// Vertex needs a project + region in the URL path. Honor the dev-specific
		// overrides first, then the standard GOOGLE_VERTEX_* the proxy reads.
		grp.Project = envOr("IRONCLAW_DEV_VERTEX_PROJECT", os.Getenv("GOOGLE_VERTEX_PROJECT"))
		grp.Location = envOr("IRONCLAW_DEV_VERTEX_LOCATION", os.Getenv("GOOGLE_VERTEX_LOCATION"))
	}
	_ = reg.PutAgentGroup(grp)
	// Always seed a deterministic, offline agent group. Its mock provider makes
	// no network call and needs no credential, so chat (and the e2e chat test)
	// work even when no real model backend is configured or its token is dead.
	_ = reg.PutAgentGroup(registry.AgentGroup{
		ID: "mock-agent", Name: "Mock Agent (offline)", Folder: "mock-agent",
		Provider: "mock", // provider.KindMock, kept as a string to avoid importing sandbox internals
	})
	_ = reg.PutUser(registry.User{ID: owner, Kind: "human", DisplayName: "Dev Owner"})
	_ = reg.GrantRole(registry.Role{UserID: owner, Role: registry.RoleOwner})
	mg, _ := reg.GetOrCreateMessagingGroup("cli", "dev-dm", "", false, contract.UnknownStrict)
	_ = reg.PutWiring(registry.Wiring{
		MessagingGroupID: mg.ID,
		AgentGroupID:     groupID,
		EngageMode:       contract.EngagePattern,
		EngagePattern:    ".",
		SessionMode:      contract.SessionShared,
		Priority:         1,
	})
	logger.Info("dev seed", "owner", owner, "agent_group", groupID, "messaging_group", mg.ID)
}
