package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestMain doubles as the stdio MCP server for TestClient_Stdio: when the env var is
// set it runs the sample server over stdin/stdout and exits, so the stdio transport
// can spawn this very test binary as a real subprocess (the os/exec self-exec idiom).
func TestMain(m *testing.M) {
	if os.Getenv("IRONCLAW_MCP_SAMPLE_STDIO") == "1" {
		_ = SampleServer().ServeStdio(context.Background(), os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// assertSampleTools checks a connected client exposes echo+add and that both work.
func assertSampleTools(t *testing.T, c *Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := c.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if info.Name != "ironclaw-mcp-sample" {
		t.Fatalf("serverInfo.Name = %q, want ironclaw-mcp-sample", info.Name)
	}
	// Initialize is idempotent.
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
		if len(tl.InputSchema) == 0 {
			t.Errorf("tool %q has no input schema", tl.Name)
		}
	}
	if !names["echo"] || !names["add"] {
		t.Fatalf("ListTools = %v, want echo+add", names)
	}

	res, err := c.CallTool(ctx, "echo", json.RawMessage(`{"text":"hi there"}`))
	if err != nil {
		t.Fatalf("CallTool echo: %v", err)
	}
	if res.IsError || res.Text() != "hi there" {
		t.Fatalf("echo result = %q (isError=%v), want %q", res.Text(), res.IsError, "hi there")
	}

	res, err = c.CallTool(ctx, "add", json.RawMessage(`{"a":2,"b":3}`))
	if err != nil {
		t.Fatalf("CallTool add: %v", err)
	}
	if res.IsError || res.Text() != "5" {
		t.Fatalf("add result = %q (isError=%v), want 5", res.Text(), res.IsError)
	}
}

func TestClient_Stdio(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := DialStdio(ctx, DirectLauncher{}, ServerConfig{
		Name:      "sample",
		Transport: TransportStdio,
		Command:   os.Args[0],
		Env:       map[string]string{"IRONCLAW_MCP_SAMPLE_STDIO": "1"},
	})
	if err != nil {
		t.Fatalf("DialStdio: %v", err)
	}
	defer c.Close()
	assertSampleTools(t, c)
}

func TestClient_HTTP(t *testing.T) {
	srv := httptest.NewServer(SampleServer().Handler())
	defer srv.Close()

	c, err := DialHTTP(httpConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("DialHTTP: %v", err)
	}
	defer c.Close()
	assertSampleTools(t, c)
}

func TestClient_ToolError(t *testing.T) {
	srv := httptest.NewServer(SampleServer().Handler())
	defer srv.Close()
	c, _ := DialHTTP(httpConfig{URL: srv.URL})
	defer c.Close()
	ctx := context.Background()
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// A bad-arguments call comes back as an IsError result (a tool error, not an RPC
	// failure), carrying the error text for the agent.
	res, err := c.CallTool(ctx, "add", json.RawMessage(`{"a":"not-a-number"}`))
	if err != nil {
		t.Fatalf("CallTool returned transport error, want tool error result: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError result for bad arguments, got %q", res.Text())
	}
}

func TestClient_CallBeforeInitialize(t *testing.T) {
	srv := httptest.NewServer(SampleServer().Handler())
	defer srv.Close()
	c, _ := DialHTTP(httpConfig{URL: srv.URL})
	defer c.Close()
	if _, err := c.CallTool(context.Background(), "echo", nil); err == nil {
		t.Fatal("CallTool before Initialize should error")
	}
}
