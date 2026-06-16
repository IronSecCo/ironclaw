// OWNER: AGENT1

// Command controlplane is the IronClaw host daemon entrypoint. It wires the
// control-plane API, gateway, isolation, router, delivery, sweep, keys, channels,
// and model proxy. The skeleton only references the constructors so the
// dependency graph compiles.
package main

import (
	"fmt"
	"os"

	"github.com/nivardsec/ironclaw/internal/host/api"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
)

func main() {
	_ = api.New()
	_ = gateway.New()
	fmt.Println("ironclaw control-plane (pre-alpha): not yet implemented")
	os.Exit(0)
}
