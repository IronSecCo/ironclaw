// OWNER: T-016

// Command controlplane is the IronClaw host daemon entrypoint. It wires the
// control-plane API, gateway (durable FileStore + append-only audit log), keys,
// registry, model proxy, and the live per-session lifecycle (SessionManager over
// the encrypted queue factory + isolator) plus the maintenance sweep and outbound
// delivery loop, and runs them until a signal arrives. The API binds to a single
// address that SHOULD be the Tailscale (tailnet) interface so the control-plane
// has no public port.
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/api"
	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/delivery"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/isolation"
	"github.com/nivardsec/ironclaw/internal/host/keys"
	"github.com/nivardsec/ironclaw/internal/host/modelproxy"
	"github.com/nivardsec/ironclaw/internal/host/queue"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/session"
	"github.com/nivardsec/ironclaw/internal/host/sweep"
)

func main() {
	apiAddr := flag.String("api-addr", "127.0.0.1:8787",
		"control-plane API listen address — set to the Tailscale (tailnet) IP in production so there is no public port")
	socket := flag.String("model-proxy-socket", "/run/ironclaw/modelproxy.sock",
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
	dev := flag.Bool("dev", false,
		"seed the registry with a tiny dev owner/agent-group for local testing")
	flag.Parse()

	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		log.Fatalf("controlplane: create state dir %s: %v", *stateDir, err)
	}

	// Host master key for the session-key custodian. In production this is loaded
	// from a host secret store / KMS; here it is generated per process.
	var master [32]byte
	if _, err := rand.Read(master[:]); err != nil {
		log.Fatalf("controlplane: generate master key: %v", err)
	}
	custodian, err := keys.New(master)
	if err != nil {
		log.Fatalf("controlplane: keys: %v", err)
	}

	// Registry: the control-plane data model (in-memory dev backend until the
	// durable, encrypted store is selected at startup).
	reg := registry.NewMemRegistry()
	if *dev {
		seedDev(reg)
	}

	// Model egress: the sandbox has network=none, so the proxy is its only path.
	// The proxy is also the sole authenticator — the host-held API key is read
	// here from the environment and injected per request; it never enters the
	// sandbox image or environment. With no key set, the proxy still runs but
	// forwards unauthenticated (useful for local/dev upstreams).
	var proxyOpts []modelproxy.Option
	credInjected := false
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		proxyOpts = append(proxyOpts, modelproxy.WithInjector(
			modelproxy.AnthropicInjector(key, "2023-06-01")))
		credInjected = true
	}
	proxy := modelproxy.New([]string{"api.anthropic.com"}, proxyOpts...)

	// Gateway: durable FileStore + append-only audit log. The v1 floor is
	// AlwaysRequireHuman, preceded by the deterministic rejecters (these only ADD
	// rejections; they never bypass the human floor).
	storePath := filepath.Join(*stateDir, "changes")
	store, err := gateway.NewFileStore(storePath)
	if err != nil {
		log.Fatalf("controlplane: gateway store: %v", err)
	}
	auditPath := filepath.Join(*stateDir, "audit.jsonl")
	audit, err := gateway.NewAuditLog(auditPath)
	if err != nil {
		log.Fatalf("controlplane: audit log: %v", err)
	}
	defer audit.Close()

	gw := gateway.New(
		gateway.VerifierChain{
			gateway.MountAllowlistVerifier{AllowedPrefixes: []string{filepath.Join(*stateDir, "mounts")}},
			gateway.PackageNameVerifier{},
			gateway.AlwaysRequireHuman{},
		},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	).SetAudit(audit)

	server := api.New(gw).WithHistory(store).WithAuditPath(auditPath)
	// Optional bearer-token auth (defense-in-depth behind the tailnet). Read from
	// the host environment; never logged.
	apiToken := os.Getenv("IRONCLAW_API_TOKEN")
	if apiToken != "" {
		server = server.WithToken(apiToken)
	}

	// Isolation: build a hardened OCI bundle per session and exec the runtime. The
	// runtime can't actually launch until rootfs provisioning (the one external
	// image-unpacker integration point) lands, but the spec building, bundle
	// writing, and exec wiring are real.
	isolator := isolation.NewRunsc(
		isolation.WithRuntimeBinary(*runtimeBin),
		isolation.WithBundleRoot(*bundleRoot),
	)

	// Per-session lifecycle: the SessionManager composes the encrypted queue
	// factory, the key custodian, and the isolator. It provides the inbound-writer
	// / outbound-reader factories the delivery loop uses and serves as the sweep's
	// Prober/Killer/Waker. Launch is best-effort (a triggering message is durably
	// queued first), so an un-provisioned rootfs in dev never breaks the pipeline.
	factory := queue.NewFactory(filepath.Join(*stateDir, "sessions"))
	manager, err := session.New(session.Config{
		Factory:          factory,
		Keys:             custodian,
		Isolator:         isolator,
		Registry:         reg,
		ModelProxySocket: *socket,
		Image:            *sandboxImage,
		KeyDir:           filepath.Join(*stateDir, "keys"),
		WorkspaceRoot:    filepath.Join(*stateDir, "workspaces"),
	})
	if err != nil {
		log.Fatalf("controlplane: session manager: %v", err)
	}

	// Delivery: poll per-session outbound queues and deliver via channel adapters,
	// re-authorizing privileged system actions through the gateway. The channel
	// registry starts empty; concrete platform adapters register here once
	// configured. The schedule_task system action enqueues a future inbound prompt
	// via the SessionManager's inbound-writer factory.
	channelReg := channels.NewRegistry()
	deliverer := delivery.New(channelReg, gw, reg, manager.OutboundReader).
		WithInboundWriter(manager.InboundWriter)

	// Sweep: live hooks — Prober/Killer/Waker are the SessionManager; the DueSource
	// and recurrence Enqueue read/write the per-session inbound queues via the
	// factory. Scheduling carries only a prompt (no script field → no RCE).
	sweeper := sweep.New(reg, manager, manager).
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
		log.Printf("controlplane: API listening on %s (bind to the tailnet IP in production)", *apiAddr)
		if err := server.Run(ctx, *apiAddr); err != nil && err != context.Canceled {
			log.Printf("controlplane: API stopped: %v", err)
			stop()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("controlplane: model proxy listening on unix:%s (allowlist: api.anthropic.com)", *socket)
		if err := proxy.Serve(ctx, *socket); err != nil && err != context.Canceled {
			log.Printf("controlplane: model proxy stopped: %v", err)
			stop()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(*sweepInterval)
		defer ticker.Stop()
		log.Printf("controlplane: sweep running every %s (stale-sandbox + due-message wake)", *sweepInterval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := sweeper.Run(ctx); err != nil && err != context.Canceled {
					log.Printf("controlplane: sweep pass error: %v", err)
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(*deliveryInterval)
		defer ticker.Stop()
		log.Printf("controlplane: delivery polling every %s (outbound → channel adapters)", *deliveryInterval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := deliverer.Poll(ctx); err != nil && err != context.Canceled {
					log.Printf("controlplane: delivery poll error: %v", err)
				}
			}
		}
	}()

	pending, _ := store.Pending()
	log.Printf("controlplane: started")
	log.Printf("controlplane:   state dir      %s", *stateDir)
	log.Printf("controlplane:   change store   %s (%d pending)", storePath, len(pending))
	log.Printf("controlplane:   audit log      %s", auditPath)
	log.Printf("controlplane:   gateway chain  mount-allowlist -> package-name -> always-require-human")
	log.Printf("controlplane:   registry       in-memory (dev=%v)", *dev)
	log.Printf("controlplane:   api auth       bearer-token=%v (set IRONCLAW_API_TOKEN; tailnet is the primary boundary)", apiToken != "")
	log.Printf("controlplane:   model proxy    socket=%s allowlist=[api.anthropic.com] credential-injection=%v", *socket, credInjected)
	log.Printf("controlplane:   sessions       queue-root=%s key-hand-off=tmpfs image=%q", filepath.Join(*stateDir, "sessions"), *sandboxImage)
	log.Printf("controlplane:   isolation      runtime=%q bundle-root=%s (live launch needs a provisioned rootfs)", *runtimeBin, *bundleRoot)
	log.Printf("controlplane:   sweep          interval=%s (scheduling carries a prompt only — no script, no RCE)", *sweepInterval)
	log.Printf("controlplane:   delivery       interval=%s (channel adapters registered when configured)", *deliveryInterval)
	log.Printf("controlplane: send SIGINT/SIGTERM to stop.")

	<-ctx.Done()
	log.Printf("controlplane: shutting down")
	// Stop any sandboxes the SessionManager launched before exiting.
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := manager.StopAll(stopCtx); err != nil {
		log.Printf("controlplane: stop sandboxes: %v", err)
	}
	cancel()
	wg.Wait()
	os.Exit(0)
}

// defaultStateDir returns a per-user state directory under the OS state/cache
// location, falling back to a temp dir.
func defaultStateDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "ironclaw", "state")
	}
	return filepath.Join(os.TempDir(), "ironclaw-state")
}

// seedDev inserts a minimal owner, agent group, and DM messaging-group wiring so a
// local operator can exercise the pipeline without a real platform.
func seedDev(reg *registry.MemRegistry) {
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
	log.Printf("controlplane: dev seed — owner=%s agent-group=%s messaging-group=%s", owner, groupID, mg.ID)
}
