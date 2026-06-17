// OWNER: T-016

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
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/api"
	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/delivery"
	"github.com/nivardsec/ironclaw/internal/host/egress"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/isolation"
	"github.com/nivardsec/ironclaw/internal/host/keys"
	"github.com/nivardsec/ironclaw/internal/host/metrics"
	"github.com/nivardsec/ironclaw/internal/host/modelproxy"
	"github.com/nivardsec/ironclaw/internal/host/questions"
	"github.com/nivardsec/ironclaw/internal/host/queue"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/router"
	"github.com/nivardsec/ironclaw/internal/host/session"
	"github.com/nivardsec/ironclaw/internal/host/skills"
	"github.com/nivardsec/ironclaw/internal/host/sweep"
	"github.com/nivardsec/ironclaw/internal/obs"
	"github.com/nivardsec/ironclaw/internal/sandbox/tools"
	"github.com/nivardsec/ironclaw/internal/version"
)

func main() {
	apiAddr := flag.String("api-addr", "127.0.0.1:8787",
		"control-plane API listen address (also serves /metrics) — set to the Tailscale (tailnet) IP in production so there is no public port")
	socket := flag.String("model-proxy-socket", defaultModelProxySocket(),
		"unix socket the model proxy listens on (bound into each sandbox)")
	stateDir := flag.String("state-dir", defaultStateDir(),
		"directory for durable control-plane state (gateway change store, audit log, per-session queues, keys)")
	runtimeBin := flag.String("runtime", isolation.DefaultRuntimeBinary,
		"OCI runtime binary used to launch sandboxes (gVisor's runsc by default)")
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
		"OPT-IN: host-local credential-injector URL; enables vault://<cred>/<path> routing through the egress broker (T-260). Requires --egress-socket")
	skillsDir := flag.String("skills-dir", "",
		"curated skills source directory; setting it enables the host-side /v1/skills endpoints (empty disables skills)")
	skillsTrustKey := flag.String("skills-trust-key", "",
		"path to the minisign public-key file that skill bundles must be signed by (required when --skills-dir is set)")
	dev := flag.Bool("dev", false,
		"seed the registry with a tiny dev owner/agent-group for local testing")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("ironclaw-controlplane " + version.String())
		return
	}

	// Structured, secret-redacting logger for the whole daemon (T-101).
	logger := obs.New(obs.Options{Format: obs.Format(*logFormat)}).Component("controlplane")

	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		logger.Error("create state dir", "dir", *stateDir, "error", err)
		os.Exit(1)
	}

	// Durable host key custody (T-100): the master key is loaded from (or created
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

	// Registry: the control-plane data model (in-memory dev backend until the
	// durable, encrypted store is selected at startup).
	reg := registry.NewMemRegistry()
	if *dev {
		seedDev(reg, logger)
	}

	// Metrics (T-102): pre-wired domain metrics, exposed at /metrics on the API.
	m := metrics.New()

	// Model egress: the sandbox has network=none, so the proxy is its only path to
	// the model host. It is the sole authenticator (host-held key injected per
	// request, never in the sandbox) and is hardened (T-107) with an egress rate
	// cap, per-request audit that also feeds metrics, and response secret redaction.
	//
	// Multi-provider (T-233): each provider is enabled only when its credential is
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
	credInjected := len(injectors) > 0
	if credInjected {
		proxyOpts = append(proxyOpts,
			modelproxy.WithInjector(modelproxy.MultiInjector(injectors...)),
			modelproxy.WithRedactedSecrets(redactKeys...),
		)
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
	proxy := modelproxy.New(allowHosts, proxyOpts...)

	// Egress broker (T-111/T-260): OPT-IN. With --egress-socket set, each sandbox gets
	// a second host unix socket to reach operator-approved external hosts
	// (deny-by-default), and with --vault-endpoint set, vault://<cred> credentials via
	// a host-local injector (a SEPARATE principal — the broker injects no secret).
	// Empty keeps the sealed default: the sandbox reaches only the model proxy.
	broker, err := buildEgressBroker(*egressSocket, *egressAllow, *vaultEndpoint, logger)
	if err != nil {
		logger.Error("egress broker", "error", err)
		os.Exit(1)
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
	createAgent := func(id contract.AgentGroupID, name, folder string) error {
		return reg.PutAgentGroup(registry.AgentGroup{ID: id, Name: name, Folder: folder})
	}
	// Applier chain (apply-side, T-096): create_agent is materialized into the
	// registry; when egress is enabled, an approved change's egress grants (e.g. a
	// skill install's bundle) are materialized into the broker's allowlist so the
	// grant takes effect; every other kind is logged.
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
	}, capApplier)
	capApplier = gateway.NewSkillInstallApplier(func(id contract.AgentGroupID, name, version string) error {
		return registry.AddInstalledSkill(reg, id, name, version)
	}, capApplier)
	if broker != nil {
		capApplier = gateway.NewEgressApplier(broker, capApplier)
	}
	gw := gateway.New(
		gateway.VerifierChain{
			gateway.MountAllowlistVerifier{AllowedPrefixes: []string{filepath.Join(*stateDir, "mounts")}},
			gateway.PackageNameVerifier{},
			gateway.NewCreateAgentVerifier(agentExists),
			gateway.AlwaysRequireHuman{},
		},
		gateway.NewManualApprover(),
		gateway.NewCreateAgentApplier(createAgent, capApplier),
		store,
	).SetAudit(audit)

	// WithRegistry attaches the control-plane registry so the /v1/registry admin
	// endpoints are live and the approvals read-model (/v1/ui/approvals, T-221) can
	// resolve agent-group/requester names instead of showing raw ids.
	server := api.New(gw).WithHistory(store).WithAuditPath(auditPath).WithMetrics(m.Handler()).WithRegistry(reg)
	// Optional bearer-token auth (defense-in-depth behind the tailnet). Read from
	// the host environment; never logged.
	apiToken := os.Getenv("IRONCLAW_API_TOKEN")
	if apiToken != "" {
		server = server.WithToken(apiToken)
	}

	// Isolation: build a hardened OCI bundle per session and exec the runtime.
	isolator := isolation.NewRunsc(
		isolation.WithRuntimeBinary(*runtimeBin),
		isolation.WithBundleRoot(*bundleRoot),
	)

	// Per-session lifecycle: the SessionManager composes the encrypted queue
	// factory, the durable key custodian, and the isolator. It provides the
	// inbound-writer / outbound-reader factories the delivery loop uses and serves
	// as the sweep's Prober/Killer/Waker.
	factory := queue.NewFactory(filepath.Join(*stateDir, "sessions"))
	manager, err := session.New(session.Config{
		Factory:          factory,
		Keys:             custodian,
		Isolator:         isolator,
		Registry:         reg,
		ModelProxySocket: *socket,
		EgressSocket:     *egressSocket,
		SkillsDir:        *skillsDir,
		Image:            *sandboxImage,
		KeyDir:           filepath.Join(*stateDir, "keys"),
		WorkspaceRoot:    filepath.Join(*stateDir, "workspaces"),
	})
	if err != nil {
		logger.Error("session manager", "error", err)
		os.Exit(1)
	}

	// Wire the web console's terminate action (T-222) to the SessionManager now
	// that it exists. Stop is idempotent; this is the only host-control surface the
	// read-only console exposes.
	server = server.WithSessionTerminator(manager.Stop)

	// Delivery: poll per-session outbound queues and deliver via channel adapters,
	// re-authorizing privileged system actions through the gateway. Concrete
	// platform adapters register when their bot token is configured.
	channelReg := channels.NewRegistry()
	registerChannelAdapters(channelReg, logger)

	// Chat playground (T-226): register an in-process webchat adapter so the
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

	// Skills (T-096/T-227): when a curated source + trust root are configured, expose
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

	pendingQuestions := questions.NewStore()
	deliverer := delivery.New(channelReg, gw, reg, manager.OutboundReader).
		WithInboundWriter(manager.InboundWriter).
		WithQuestions(pendingQuestions)

	// Respawn backoff (T-105): wrap the SessionManager (which is the sweep's Prober
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
	// register explicitly (T-232). Each is env-gated and skipped when unconfigured;
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

// seedDev inserts a minimal owner, agent group, and DM messaging-group wiring so a
// local operator can exercise the pipeline without a real platform.
func seedDev(reg *registry.MemRegistry, logger *obs.Logger) {
	const (
		owner   = "cli:dev"
		groupID = "dev-agent"
	)
	_ = reg.PutAgentGroup(registry.AgentGroup{ID: groupID, Name: "Dev Agent", Folder: "dev-agent"})
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
