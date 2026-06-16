// OWNER: AGENT1

// Command ironctl is the IronClaw admin CLI. It is a thin client of the
// control-plane HTTP API (submit change requests, list pending approvals, record
// decisions, query the registry).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("ironctl (pre-alpha): not yet implemented")
	fmt.Println("usage: ironctl <command> [flags]")
	fmt.Println("  (commands: submit, approvals, decide, sessions — all TODO AGENT1)")
	os.Exit(0)
}
