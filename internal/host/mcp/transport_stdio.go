package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// stdioTransport speaks newline-delimited JSON-RPC over a subprocess's stdin/stdout
// (the MCP stdio transport). A background reader dispatches responses to waiters by
// id, so a per-call context deadline is honored without desynchronizing the stream
// and server-initiated notifications are simply ignored.
type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *strings.Builder

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[int64]chan rpcResponse
	closed  bool
	exitErr error
}

// DialStdio spawns a local MCP server via the given Launcher and returns a connected
// (but not yet initialized) Client. The Launcher decides HOW the server runs — a bare
// host process (DirectLauncher) or a hardened container (ContainerLauncher) — so the
// isolation policy is a host decision the agent cannot influence. cfg.Env's ${VAR}
// references are expanded host-side here and never reach the sandbox. The subprocess
// lives until Client.Close; ctx governs its lifetime.
func DialStdio(ctx context.Context, launcher Launcher, cfg ServerConfig) (*Client, error) {
	if launcher == nil {
		launcher = DirectLauncher{}
	}
	cmd, err := launcher.command(ctx, cfg, expandEnv(cfg.Env))
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %q: %w", cfg.Command, err)
	}
	t := &stdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  &stderr,
		pending: make(map[int64]chan rpcResponse),
	}
	go t.readLoop(stdout)
	return newClient(t), nil
}

// stdioEnv builds the subprocess environment: a minimal, non-secret base (PATH +
// HOME, inherited from the host so npx/python and friends find their tools and
// caches) plus the operator-declared env overlaid on top. It deliberately does NOT
// inherit the host's full environment, so a model/API credential in the daemon's env
// never leaks into a third-party MCP server.
func stdioEnv(extra map[string]string) []string {
	base := map[string]string{
		"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	if p := os.Getenv("PATH"); p != "" {
		base["PATH"] = p
	}
	if h := os.Getenv("HOME"); h != "" {
		base["HOME"] = h
	}
	for k, v := range extra {
		base[k] = v
	}
	env := make([]string, 0, len(base))
	for k, v := range base {
		env = append(env, k+"="+v)
	}
	return env
}

// readLoop reads newline-delimited JSON messages from the server and delivers each
// response to its waiter. It runs until the stream closes (server exit / Close).
func (t *stdioTransport) readLoop(stdout io.Reader) {
	r := bufio.NewReader(stdout)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var resp rpcResponse
			if json.Unmarshal(line, &resp) == nil && resp.ID != nil {
				t.deliver(*resp.ID, resp)
			}
			// A message without an id (a server notification/request) is ignored: this
			// client drives the conversation and needs only responses.
		}
		if err != nil {
			t.fail(err)
			return
		}
	}
}

// deliver hands a response to its waiter, if one is still registered.
func (t *stdioTransport) deliver(id int64, resp rpcResponse) {
	t.mu.Lock()
	ch, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
	}
	t.mu.Unlock()
	if ok {
		ch <- resp
	}
}

// fail marks the transport dead and wakes every waiter so callers do not block on a
// server that has exited.
func (t *stdioTransport) fail(err error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	if err != nil && !errors.Is(err, io.EOF) {
		t.exitErr = err
	}
	waiters := t.pending
	t.pending = map[int64]chan rpcResponse{}
	t.mu.Unlock()
	for _, ch := range waiters {
		close(ch)
	}
}

func (t *stdioTransport) roundTrip(ctx context.Context, id int64, method string, params any) (json.RawMessage, error) {
	ch := make(chan rpcResponse, 1)
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, t.deadErr()
	}
	t.pending[id] = ch
	t.mu.Unlock()

	if err := t.write(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, t.deadErr()
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("server error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (t *stdioTransport) notify(_ context.Context, method string, params any) error {
	return t.write(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

// write serializes a message and writes it as one newline-delimited line.
func (t *stdioTransport) write(msg rpcRequest) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mcp: marshal request: %w", err)
	}
	b = append(b, '\n')
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.stdin.Write(b); err != nil {
		return fmt.Errorf("mcp: write to server (%w)%s", err, t.stderrTail())
	}
	return nil
}

// deadErr reports why the transport is no longer usable, including a tail of the
// server's stderr to make a crashed local server diagnosable.
func (t *stdioTransport) deadErr() error {
	t.mu.Lock()
	exit := t.exitErr
	t.mu.Unlock()
	if exit != nil {
		return fmt.Errorf("mcp: server exited: %v%s", exit, t.stderrTail())
	}
	return fmt.Errorf("mcp: server connection closed%s", t.stderrTail())
}

// stderrTail returns a short, parenthesized tail of the server's stderr for error
// messages, or "" when there is none.
func (t *stdioTransport) stderrTail() string {
	s := strings.TrimSpace(t.stderr.String())
	if s == "" {
		return ""
	}
	const max = 400
	if len(s) > max {
		s = "…" + s[len(s)-max:]
	}
	return " (stderr: " + s + ")"
}

// Close kills the subprocess and releases its pipes. Safe to call more than once.
func (t *stdioTransport) Close() error {
	t.fail(nil)
	_ = t.stdin.Close()
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	_ = t.cmd.Wait()
	return nil
}
