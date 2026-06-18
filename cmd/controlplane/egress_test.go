package main

import (
	"testing"

	"github.com/nivardsec/ironclaw/internal/obs"
)

func testLogger() *obs.Logger { return obs.New(obs.Options{}).Component("test") }

func TestBuildEgressBrokerDisabled(t *testing.T) {
	b, err := buildEgressBroker("", "a.com", "http://127.0.0.1:8200", testLogger())
	if err != nil || b != nil {
		t.Fatalf("no socket must disable egress: b=%v err=%v", b, err)
	}
}

func TestBuildEgressBrokerBasic(t *testing.T) {
	b, err := buildEgressBroker("/run/egress.sock", "api.github.com, api.pagerduty.com", "", testLogger())
	if err != nil {
		t.Fatalf("basic broker: %v", err)
	}
	if b == nil {
		t.Fatal("broker should be constructed")
	}
	if !b.Allowed("api.github.com") || !b.Allowed("api.pagerduty.com") {
		t.Error("configured hosts should be allowlisted")
	}
	if b.Allowed("evil.example.com") {
		t.Error("unconfigured host must be denied")
	}
}

func TestBuildEgressBrokerWithVault(t *testing.T) {
	b, err := buildEgressBroker("/run/egress.sock", "", "http://127.0.0.1:8200", testLogger())
	if err != nil {
		t.Fatalf("vault broker: %v", err)
	}
	// The injector endpoint is auto-allowlisted so vault routing can reach it.
	if !b.Allowed("127.0.0.1:8200") {
		t.Error("the vault injector endpoint should be allowlisted")
	}
}

func TestBuildEgressBrokerBadVault(t *testing.T) {
	if _, err := buildEgressBroker("/run/egress.sock", "", "not-a-url", testLogger()); err == nil {
		t.Error("an invalid vault endpoint must error")
	}
}
