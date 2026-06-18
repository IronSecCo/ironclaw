package mcp

import (
	"path/filepath"
	"testing"
)

func TestServerConfig_Validate(t *testing.T) {
	cases := []struct {
		name string
		cfg  ServerConfig
		ok   bool
	}{
		{"good stdio", ServerConfig{Name: "files", Transport: TransportStdio, Command: "mcp-server"}, true},
		{"good https", ServerConfig{Name: "gh", Transport: TransportHTTP, URL: "https://mcp.example.com/rpc"}, true},
		{"loopback http ok", ServerConfig{Name: "local", Transport: TransportHTTP, URL: "http://127.0.0.1:9000"}, true},
		{"plain http rejected", ServerConfig{Name: "gh", Transport: TransportHTTP, URL: "http://mcp.example.com"}, false},
		{"bad name", ServerConfig{Name: "Files!", Transport: TransportStdio, Command: "x"}, false},
		{"stdio without command", ServerConfig{Name: "files", Transport: TransportStdio}, false},
		{"http without url", ServerConfig{Name: "gh", Transport: TransportHTTP}, false},
		{"stdio with url", ServerConfig{Name: "x", Transport: TransportStdio, Command: "c", URL: "https://x"}, false},
		{"unknown transport", ServerConfig{Name: "x", Transport: "carrier-pigeon"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if (err == nil) != c.ok {
				t.Fatalf("Validate() err=%v, want ok=%v", err, c.ok)
			}
		})
	}
}

func TestServerConfig_PublicMasksSecrets(t *testing.T) {
	cfg := ServerConfig{
		Name:      "gh",
		Transport: TransportHTTP,
		URL:       "https://mcp.example.com",
		Headers:   map[string]string{"Authorization": "Bearer sk-secret", "X-Ref": "${GH_TOKEN}"},
	}
	pub := cfg.Public()
	if pub.Headers["Authorization"] != "••••" {
		t.Errorf("raw secret not masked: %q", pub.Headers["Authorization"])
	}
	if pub.Headers["X-Ref"] != "${GH_TOKEN}" {
		t.Errorf("env reference should be preserved (non-secret), got %q", pub.Headers["X-Ref"])
	}
	// The original must be untouched.
	if cfg.Headers["Authorization"] != "Bearer sk-secret" {
		t.Errorf("Public mutated the original config")
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("MCP_TEST_TOKEN", "s3cr3t")
	out := expandEnv(map[string]string{"Authorization": "Bearer ${MCP_TEST_TOKEN}", "Plain": "literal"})
	if out["Authorization"] != "Bearer s3cr3t" {
		t.Errorf("env not expanded: %q", out["Authorization"])
	}
	if out["Plain"] != "literal" {
		t.Errorf("literal value changed: %q", out["Plain"])
	}
}

func TestCatalog_Persistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp", "catalog.json")
	cat, err := NewCatalog(path)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	if err := cat.Put(ServerConfig{Name: "files", Transport: TransportStdio, Command: "mcp-files"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := cat.Put(ServerConfig{Name: "gh", Transport: TransportHTTP, URL: "https://mcp.example.com"}); err != nil {
		t.Fatalf("Put gh: %v", err)
	}
	// Reload from disk: both servers survive.
	reloaded, err := NewCatalog(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.List(); len(got) != 2 || got[0].Name != "files" || got[1].Name != "gh" {
		t.Fatalf("reloaded list = %+v, want files+gh sorted", got)
	}
	if _, ok := reloaded.Get("files"); !ok {
		t.Fatal("files missing after reload")
	}
	if err := reloaded.Delete("files"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := reloaded.Get("files"); ok {
		t.Fatal("files present after delete")
	}
	// Invalid config is refused.
	if err := reloaded.Put(ServerConfig{Name: "bad", Transport: "nope"}); err == nil {
		t.Fatal("expected invalid config to be refused")
	}
}
