// OWNER: AGENT1

// Command controlplane is the IronClaw host daemon entrypoint. It wires the
// control-plane API, gateway (durable FileStore + append-only audit log), keys,
// registry, and model proxy and runs them until a signal arrives. The API binds
// to a single address that SHOULD be the Tailscale (tailnet) interface so the
// control-plane has no public port.
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

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/api"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/keys"
	"github.com/nivardsec/ironclaw/internal/host/modelproxy"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

func main() {
	apiAddr := flag.String("api-addr", "127.0.0.1:8787",
		"control-plane API listen address — set to the Tailscale (tailnet) IP in production so there is no public port")
	socket := flag.String("model-proxy-socket", "/run/ironclaw/modelproxy.sock",
		"unix socket the model proxy listens on (bound into each sandbox)")
	stateDir := flag.String("state-dir", defaultStateDir(),
		"directory for durable control-plane state (gateway change store + audit log)")
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
	_ = custodian // wired into router/isolation once those bindings land

	// Registry: the control-plane data model (in-memory dev backend until the
	// durable, encrypted store lands).
	reg := registry.NewMemRegistry()
	if *dev {
		seedDev(reg)
	}

	// Model egress: the sandbox has network=none, so the proxy is its only path.
	proxy := modelproxy.New([]string{"api.anthropic.com"})

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

	pending, _ := store.Pending()
	log.Printf("controlplane: started")
	log.Printf("controlplane:   state dir      %s", *stateDir)
	log.Printf("controlplane:   change store   %s (%d pending)", storePath, len(pending))
	log.Printf("controlplane:   audit log      %s", auditPath)
	log.Printf("controlplane:   gateway chain  mount-allowlist -> package-name -> always-require-human")
	log.Printf("controlplane:   registry       in-memory (dev=%v)", *dev)
	log.Printf("controlplane: send SIGINT/SIGTERM to stop.")

	<-ctx.Done()
	log.Printf("controlplane: shutting down")
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
