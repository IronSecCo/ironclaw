// OWNER: AGENT2

// Command sandbox is the in-sandbox agent entrypoint. It receives the session key
// and queue paths, constructs the queue, and runs the reasoning poll loop.
//
// The session key is read from a file (delivered via tmpfs at launch), never from
// an environment variable — the sandbox image never contains a key. Until the
// encrypted SQLite binding is wired in, the queue open returns
// contract.ErrCryptoBindingPending and the process exits cleanly without starting
// the loop (see docs/building.md).
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/sandbox/loop"
	"github.com/nivardsec/ironclaw/internal/sandbox/provider"
	"github.com/nivardsec/ironclaw/internal/sandbox/queue"
	"github.com/nivardsec/ironclaw/internal/sandbox/tools"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("ironclaw sandbox: %v", err)
	}
}

func run() error {
	var (
		inboundPath  = flag.String("inbound", "/run/ironclaw/inbound.db", "path to the inbound queue database")
		outboundPath = flag.String("outbound", "/run/ironclaw/outbound.db", "path to the outbound queue database")
		keyPath      = flag.String("key", "/run/ironclaw/session.key", "path to the per-session key (tmpfs; raw 32 bytes or 64 hex chars)")
		workspace    = flag.String("workspace", "/workspace", "writable workspace directory exposed to file tools")
		heartbeat    = flag.String("heartbeat", "/workspace/.heartbeat", "heartbeat file touched every poll")
		modelSocket  = flag.String("model-socket", provider.DefaultSocketPath, "host model-proxy unix socket")
		model        = flag.String("model", "", "model id override (defaults to the provider's default)")
	)
	flag.Parse()

	key, err := loadSessionKey(*keyPath)
	if err != nil {
		return err
	}

	// Open outbound first: it surfaces the pending-binding condition directly
	// (the inbound reader swallows it so it can retry per poll).
	outbound, err := queue.OpenOutbound(*outboundPath, key)
	if err != nil {
		if errors.Is(err, contract.ErrCryptoBindingPending) {
			fmt.Println("ironclaw sandbox: encrypted queue binding pending (SQLite3 Multiple Ciphers, CGo); not starting poll loop. See docs/building.md.")
			return nil
		}
		return fmt.Errorf("open outbound queue: %w", err)
	}
	defer outbound.Close()

	inbound, err := queue.OpenInbound(*inboundPath, key)
	if err != nil {
		return fmt.Errorf("open inbound queue: %w", err)
	}
	defer inbound.Close()

	registry, err := buildTools(*workspace)
	if err != nil {
		return err
	}

	prov := provider.NewAnthropic(provider.Config{SocketPath: *modelSocket, Model: *model})

	l, err := loop.New(loop.Config{
		Inbound:       inbound,
		Outbound:      outbound,
		Provider:      prov,
		Tools:         registry,
		HeartbeatPath: *heartbeat,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("ironclaw sandbox: starting poll loop (%d tools enabled)", len(registry.Names()))
	if err := l.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// buildTools assembles the in-sandbox tool registry: workspace file operations
// plus the gateway-bound capability-change request tool. There are deliberately
// no package-install, MCP, or self-edit tools.
func buildTools(workspaceDir string) (*tools.Registry, error) {
	registry := tools.NewRegistry()

	ws, err := tools.NewWorkspace(workspaceDir)
	if err != nil {
		return nil, err
	}
	for _, t := range ws.Tools() {
		if err := registry.Register(t); err != nil {
			return nil, fmt.Errorf("register %s: %w", t.Name(), err)
		}
	}
	if err := registry.Register(tools.NewRequestCapabilityChangeTool()); err != nil {
		return nil, fmt.Errorf("register request_capability_change: %w", err)
	}
	return registry, nil
}

// loadSessionKey reads the per-session key from path. It accepts either 64 hex
// characters (with optional surrounding whitespace) or a raw 32-byte file.
func loadSessionKey(path string) (contract.SessionKey, error) {
	var key contract.SessionKey
	raw, err := os.ReadFile(path)
	if err != nil {
		return key, fmt.Errorf("read session key %q: %w", path, err)
	}

	trimmed := bytes.TrimSpace(raw)
	switch {
	case len(trimmed) == hex.EncodedLen(len(key)): // 64 hex chars
		if _, err := hex.Decode(key[:], trimmed); err != nil {
			return key, fmt.Errorf("decode hex session key: %w", err)
		}
	case len(raw) == len(key): // raw 32 bytes
		copy(key[:], raw)
	default:
		return key, fmt.Errorf("session key %q: expected 32 raw bytes or 64 hex chars, got %d bytes", path, len(raw))
	}
	return key, nil
}
