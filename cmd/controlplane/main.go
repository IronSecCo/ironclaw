// OWNER: AGENT1

// Command controlplane is the IronClaw host daemon entrypoint. It wires the
// control-plane API, gateway, keys, and model proxy and runs them until a signal
// arrives. The API binds to a single address that SHOULD be the Tailscale
// (tailnet) interface so the control-plane has no public port.
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/nivardsec/ironclaw/internal/host/api"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/keys"
	"github.com/nivardsec/ironclaw/internal/host/modelproxy"
)

func main() {
	apiAddr := flag.String("api-addr", "127.0.0.1:8787",
		"control-plane API listen address — set to the Tailscale (tailnet) IP in production so there is no public port")
	socket := flag.String("model-proxy-socket", "/run/ironclaw/modelproxy.sock",
		"unix socket the model proxy listens on (bound into each sandbox)")
	flag.Parse()

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

	// Model egress: the sandbox has network=none, so the proxy is its only path.
	proxy := modelproxy.New([]string{"api.anthropic.com"})

	// Gateway: v1 floor is AlwaysRequireHuman over every mutation.
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	server := api.New(gw)

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

	log.Printf("controlplane: started (gateway v1 floor: always-require-human). Send SIGINT/SIGTERM to stop.")
	<-ctx.Done()
	log.Printf("controlplane: shutting down")
	wg.Wait()
	os.Exit(0)
}
