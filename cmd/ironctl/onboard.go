package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/IronSecCo/ironclaw/internal/host/onboard"
)

// cmdOnboard runs the guided first-run wizard: `ironctl [--addr URL] onboard [flags]`.
// It is non-interactive by design (reads env + flags), idempotent, and safe to re-run.
func cmdOnboard(addr string, args []string) error {
	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "non-interactive: accept defaults, no prompts")
	dry := fs.Bool("dry-run", false, "show the plan without writing anything")
	force := fs.Bool("force", false, "overwrite an existing onboarding token/config")
	cfg := fs.String("config", defaultOnboardConfig(), "path to the onboarding env file (0600)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	d := onboard.New(*cfg)
	res, err := d.Run(context.Background(), onboard.Options{
		Addr:   addr,
		Yes:    *yes,
		DryRun: *dry,
		Force:  *force,
	})
	if err != nil {
		return err
	}
	d.Report(res)
	if !res.Ok() {
		return fmt.Errorf("onboarding finished with errors — see the steps above")
	}
	return nil
}

// defaultOnboardConfig is $IRONCLAW_CONFIG, else <user-config-dir>/ironclaw/onboard.env.
func defaultOnboardConfig() string {
	if v := os.Getenv("IRONCLAW_CONFIG"); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(os.TempDir(), "ironclaw", "onboard.env")
	}
	return filepath.Join(dir, "ironclaw", "onboard.env")
}
