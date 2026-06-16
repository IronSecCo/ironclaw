// OWNER: AGENT2

// Command sandbox is the in-sandbox agent entrypoint. It receives the session key
// and queue paths, constructs the queue, and runs the reasoning poll loop. The
// skeleton only references the constructors so the dependency graph compiles.
package main

import (
	"fmt"
	"os"

	"github.com/nivardsec/ironclaw/internal/sandbox/loop"
)

func main() {
	_ = loop.New()
	fmt.Println("ironclaw sandbox (pre-alpha): not yet implemented")
	os.Exit(0)
}
