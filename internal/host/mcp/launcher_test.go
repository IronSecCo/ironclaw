package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestContainerLauncher_BuildsHardenedCommand(t *testing.T) {
	t.Setenv("MCP_SECRET", "s3cr3t")
	l := ContainerLauncher{Runtime: "docker", OCIRuntime: "runsc", DefaultImage: "ironclaw-mcp:latest"}
	cfg := ServerConfig{
		Name:      "files",
		Transport: TransportStdio,
		Command:   "mcp-files",
		Args:      []string{"--root", "/data"},
		Env:       map[string]string{"TOKEN": "${MCP_SECRET}"},
	}
	cmd, err := l.command(context.Background(), cfg, expandEnv(cfg.Env))
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	argv := strings.Join(cmd.Args, " ")

	// Hardening flags must be present.
	for _, want := range []string{
		"docker run --rm -i",
		"--network none",
		"--read-only",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--runtime runsc",
		"--user 65532:65532",
		"ironclaw-mcp:latest mcp-files --root /data",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv missing %q\nargv: %s", want, argv)
		}
	}

	// The secret is forwarded BY NAME (-e TOKEN), and its VALUE must NOT appear in the
	// argv (no `ps` leak) — it lives only in the command's own environment.
	if !strings.Contains(argv, "-e TOKEN") {
		t.Errorf("env not forwarded by name: %s", argv)
	}
	if strings.Contains(argv, "s3cr3t") {
		t.Errorf("secret value leaked into argv: %s", argv)
	}
	foundEnv := false
	for _, e := range cmd.Env {
		if e == "TOKEN=s3cr3t" {
			foundEnv = true
		}
	}
	if !foundEnv {
		t.Errorf("expanded secret missing from command env")
	}
}

func TestContainerLauncher_RequiresImage(t *testing.T) {
	l := ContainerLauncher{Runtime: "docker"} // no default image
	_, err := l.command(context.Background(), ServerConfig{Name: "x", Transport: TransportStdio, Command: "c"}, nil)
	if err == nil {
		t.Fatal("expected an error when no image is available")
	}
}

func TestDirectLauncher_Describe(t *testing.T) {
	if !strings.Contains(DirectLauncher{}.describe(), "UNISOLATED") {
		t.Error("DirectLauncher should advertise that it is unisolated")
	}
}
