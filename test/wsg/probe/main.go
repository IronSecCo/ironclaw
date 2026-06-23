// Command probe is the in-sandbox isolation verifier for the WS-G live-launch row
// (IRO-93). It runs as PID 1 (/sandbox) inside a real gVisor sandbox and writes a
// verdict to /workspace/result.txt that the host test (TestG2_LiveSandbox_Runsc)
// reads back. It is stdlib-only and built static (CGO_ENABLED=0) by the wsg-verify
// workflow so it needs nothing in the minimal rootfs.
//
// It asserts the sandbox's trust boundary from the inside:
//   - iface_only_lo   — no network interface other than loopback (network=none).
//   - internet_blocked — an outbound TCP dial to the public internet fails.
//   - modelproxy_ok   — the bound model-proxy unix socket is reachable.
package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const (
	resultPath     = "/workspace/result.txt"
	modelProxySock = "/run/ironclaw/modelproxy.sock"
)

func main() {
	var b strings.Builder
	b.WriteString(line("iface_only_lo", onlyLoopback()))
	b.WriteString(line("internet_blocked", internetBlocked()))
	b.WriteString(line("modelproxy_ok", modelProxyReachable()))
	// Best-effort write; the host polls for this file.
	_ = os.WriteFile(resultPath, []byte(b.String()), 0o644)
	// Stay alive briefly so the host can stop us cleanly after reading the verdict.
	time.Sleep(5 * time.Second)
}

func line(key string, pass bool) string {
	v := "FAIL"
	if pass {
		v = "PASS"
	}
	return fmt.Sprintf("%s=%s\n", key, v)
}

// onlyLoopback reports true when no non-loopback network interface exists — the
// observable effect of network=none (no NIC).
func onlyLoopback() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		// No network stack at all is the strictest form of network=none.
		return true
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if ifc.Name == "lo" {
			continue
		}
		return false // a real NIC is present — isolation breach
	}
	return true
}

// internetBlocked reports true when an outbound TCP dial to the public internet
// fails (expected: no route exists under network=none).
func internetBlocked() bool {
	for _, addr := range []string{"1.1.1.1:443", "8.8.8.8:443"} {
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err == nil {
			conn.Close()
			return false // reachable — isolation breach
		}
	}
	return true
}

// modelProxyReachable reports true when the bound model-proxy unix socket accepts a
// connection — the sandbox's only egress path under network=none.
func modelProxyReachable() bool {
	conn, err := net.DialTimeout("unix", modelProxySock, 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
