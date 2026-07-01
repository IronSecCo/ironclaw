package main

import (
	"bytes"
	"testing"
)

func TestWantsHelp(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"change", "pending", "--help"}, true},
		{[]string{"change", "pending", "-h"}, true},
		{[]string{"change", "submit", "--help"}, true},
		{[]string{"audit", "-h"}, true},
		{[]string{"registry", "--help"}, true},
		{[]string{"change", "pending"}, false},
		{[]string{"audit", "--limit", "10"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := wantsHelp(c.args); got != c.want {
			t.Errorf("wantsHelp(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestRunSubcommandHelp guards the IRO-244 regression: a `-h`/`--help` flag after
// a subcommand must print usage and exit 0 (return nil) WITHOUT dispatching the
// action — so probing for options never reaches the network or executes a command.
// run() with these args must not require a live control-plane.
func TestRunSubcommandHelp(t *testing.T) {
	for _, args := range [][]string{
		{"change", "pending", "--help"},
		{"change", "pending", "-h"},
		{"change", "history", "--help"},
		{"change", "submit", "--help"},
		{"audit", "--help"},
		{"registry", "--help"},
		{"agent", "--help"},
		// Global flags before the subcommand must not defeat the guard.
		{"--addr", "http://127.0.0.1:1", "change", "pending", "--help"},
	} {
		if err := run(args); err != nil {
			t.Errorf("run(%v) = %v, want nil (help should exit 0 without dispatching)", args, err)
		}
	}
}

// TestHelpForWritesToWriter confirms help output is routed to the provided writer
// (callers pass os.Stdout for exit-0 help), and that registry/agent get their own
// block while other commands fall back to the full reference.
func TestHelpForWritesToWriter(t *testing.T) {
	cases := []struct {
		args    []string
		wantSub string
	}{
		{[]string{"change", "pending", "--help"}, "usage:"},
		{[]string{"registry", "--help"}, "registry subcommands"},
		{[]string{"agent", "--help"}, "agent subcommands"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		helpFor(c.args, &buf)
		if !bytes.Contains(buf.Bytes(), []byte(c.wantSub)) {
			t.Errorf("helpFor(%v) output %q does not contain %q", c.args, buf.String(), c.wantSub)
		}
	}
}
