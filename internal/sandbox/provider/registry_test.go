package provider

import (
	"context"
	"strings"
	"testing"
)

// stubProvider is a minimal Provider used to prove the registry wires a brand-new
// backend without any edit to provider.go.
type stubProvider struct{ cfg Config }

func (s stubProvider) Query(context.Context, string) (string, error) { return "stub", nil }

// TestRegistryExtensibility demonstrates the acceptance criterion: a hypothetical
// new provider is added purely by calling Register (as a backend file's init would),
// touching no shared region of provider.go, and New then resolves it — including the
// case-insensitive, whitespace-trimmed lookup and the factory-applied default.
func TestRegistryExtensibility(t *testing.T) {
	const kind = "iro314-demo"
	Register(kind, func(cfg Config) (Provider, error) {
		if cfg.Model == "" {
			cfg.Model = "demo-default" // per-kind default colocated in the factory
		}
		return stubProvider{cfg: cfg}, nil
	})

	pv, err := New(Config{Kind: "  IRO314-Demo  "}) // mixed case + surrounding space
	if err != nil {
		t.Fatalf("New(demo): %v", err)
	}
	sp, ok := pv.(stubProvider)
	if !ok {
		t.Fatalf("New(demo) = %T, want stubProvider", pv)
	}
	if sp.cfg.Model != "demo-default" {
		t.Fatalf("factory default not applied: model = %q", sp.cfg.Model)
	}
}

// TestRegisterRejectsDuplicate proves a duplicate registration panics rather than
// silently overwriting a backend (which could mask changed defaults / a wire fork).
func TestRegisterRejectsDuplicate(t *testing.T) {
	const kind = "iro314-dup"
	Register(kind, func(cfg Config) (Provider, error) { return stubProvider{}, nil })

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("duplicate Register: want panic, got none")
		}
		if !strings.Contains(strings.ToLower(r.(string)), "duplicate") {
			t.Fatalf("panic = %v, want it to mention a duplicate registration", r)
		}
	}()
	Register(kind, func(cfg Config) (Provider, error) { return stubProvider{}, nil })
}

// TestRegisterRejectsEmptyAndNil guards the other two init-time programming errors.
func TestRegisterRejectsEmptyAndNil(t *testing.T) {
	assertPanics(t, "empty kind", func() { Register("   ", func(Config) (Provider, error) { return nil, nil }) })
	assertPanics(t, "nil factory", func() { Register("iro314-nil", nil) })
}

func assertPanics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s: want panic, got none", name)
		}
	}()
	f()
}
