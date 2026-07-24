package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/catalog"
)

func TestCmdToolsJSON(t *testing.T) {
	out, errOut := captureStdouterr(t, func() {
		if err := cmdTools("", []string{"--json"}); err != nil {
			t.Fatalf("cmdTools --json: %v", err)
		}
	})
	if errOut != "" {
		t.Fatalf("unexpected stderr: %q", errOut)
	}

	var tools []catalog.ToolInfo
	if err := json.Unmarshal([]byte(out), &tools); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%q", err, out)
	}
	if len(tools) != len(catalog.Tools()) {
		t.Fatalf("got %d tools in JSON, want %d", len(tools), len(catalog.Tools()))
	}
	var webSearch *catalog.ToolInfo
	for i := range tools {
		if tools[i].Name == "web_search" {
			webSearch = &tools[i]
			break
		}
	}
	if webSearch == nil || !webSearch.Egress {
		t.Fatal("expected web_search tool with egress=true in JSON output")
	}
}

func TestCmdToolsDefaultTextOutput(t *testing.T) {
	out, errOut := captureStdouterr(t, func() {
		if err := cmdTools("", nil); err != nil {
			t.Fatalf("cmdTools: %v", err)
		}
	})
	if errOut != "" {
		t.Fatalf("unexpected stderr: %q", errOut)
	}
	if !strings.Contains(out, "Built-in tools") {
		t.Fatalf("expected human-readable header, got %q", out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("default output should be text, not JSON: %q", out)
	}
}
