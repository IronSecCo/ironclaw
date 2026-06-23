package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/obs"
)

// registerChannelAdapters reads bot tokens / bridge config from the environment
// and registers exactly the adapters whose config is present, so the daemon still
// boots with none set (e.g. in --dev). These tests pin that auth-env wiring: which
// env var maps to which adapter, that an unset var registers nothing, and that a
// secret token never lands in a log line. They use t.Setenv (no real network,
// no secrets), so they are hermetic and run in the existing CI go-test job.

// allChannelEnv is every environment variable registerChannelAdapters consults.
// Tests clear all of them to "" first so the host environment cannot leak a real
// token into a case that expects a clean slate.
var allChannelEnv = []string{
	"SLACK_BOT_TOKEN",
	"DISCORD_BOT_TOKEN",
	"TELEGRAM_BOT_TOKEN",
	"IRONCLAW_TEAMS_WEBHOOK_URL",
	"IRONCLAW_SIGNAL_CLI_URL",
	"IRONCLAW_SIGNAL_NUMBER",
	"IRONCLAW_IMESSAGE_ENABLE",
}

// clearChannelEnv blanks every channel env var for the duration of the test so
// each case starts from a known-empty environment.
func clearChannelEnv(t *testing.T) {
	t.Helper()
	for _, k := range allChannelEnv {
		t.Setenv(k, "")
	}
}

func TestRegisterChannelAdapters_NoEnvRegistersNothing(t *testing.T) {
	clearChannelEnv(t)

	reg := channels.NewRegistry()
	registerChannelAdapters(reg, obs.Discard())

	if got := reg.List(); len(got) != 0 {
		t.Fatalf("with no channel env set, registered %v, want none", got)
	}
}

func TestRegisterChannelAdapters_TokenEnvGating(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string // adapter name expected to register
	}{
		{"slack", "SLACK_BOT_TOKEN", "slack"},
		{"discord", "DISCORD_BOT_TOKEN", "discord"},
		{"telegram", "TELEGRAM_BOT_TOKEN", "telegram"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearChannelEnv(t)
			t.Setenv(tc.env, "secret-"+tc.name+"-token")

			reg := channels.NewRegistry()
			registerChannelAdapters(reg, obs.Discard())

			got := reg.List()
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("env %s set: registered %v, want exactly [%q]", tc.env, got, tc.want)
			}
			if _, ok := reg.Get(tc.want); !ok {
				t.Fatalf("adapter %q not retrievable after registration", tc.want)
			}
		})
	}
}

func TestRegisterChannelAdapters_TeamsWebhookGating(t *testing.T) {
	clearChannelEnv(t)
	t.Setenv("IRONCLAW_TEAMS_WEBHOOK_URL", "https://example.invalid/webhook")

	reg := channels.NewRegistry()
	registerChannelAdapters(reg, obs.Discard())

	if _, ok := reg.Get("teams"); !ok {
		t.Fatalf("teams adapter not registered when IRONCLAW_TEAMS_WEBHOOK_URL is set; got %v", reg.List())
	}
}

func TestRegisterChannelAdapters_SignalBridgeGating(t *testing.T) {
	clearChannelEnv(t)
	t.Setenv("IRONCLAW_SIGNAL_CLI_URL", "http://127.0.0.1:8080")
	t.Setenv("IRONCLAW_SIGNAL_NUMBER", "+15555550100")

	reg := channels.NewRegistry()
	registerChannelAdapters(reg, obs.Discard())

	if _, ok := reg.Get("signal"); !ok {
		t.Fatalf("signal adapter not registered when IRONCLAW_SIGNAL_CLI_URL is set; got %v", reg.List())
	}
}

// TestRegisterChannelAdapters_TokenNeverLogged is a security assertion: the
// registration path logs an adapter-registered line, but the secret token must
// never appear in any log record.
func TestRegisterChannelAdapters_TokenNeverLogged(t *testing.T) {
	clearChannelEnv(t)
	const secret = "xoxb-super-secret-slack-token-value"
	t.Setenv("SLACK_BOT_TOKEN", secret)

	var buf bytes.Buffer
	logger := obs.New(obs.Options{Output: &buf})

	reg := channels.NewRegistry()
	registerChannelAdapters(reg, logger)

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("secret token leaked into logs:\n%s", buf.String())
	}
	// Sanity: the registration line was emitted (so the absence above is meaningful).
	if !strings.Contains(buf.String(), "slack") {
		t.Fatalf("expected a registration log mentioning the slack adapter, got:\n%s", buf.String())
	}
}
