package main

import (
	"reflect"
	"testing"
)

func TestApplyVaultGrant_NewCredential(t *testing.T) {
	got := applyVaultGrant(nil, "GitHub", []string{"API.github.com"})
	want := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestApplyVaultGrant_UnionsHostsDedup(t *testing.T) {
	start := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}
	got := applyVaultGrant(start, "github", []string{"api.github.com", "uploads.github.com"})
	want := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com", "uploads.github.com"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestApplyVaultGrant_DoesNotMutateInput(t *testing.T) {
	start := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}
	_ = applyVaultGrant(start, "github", []string{"uploads.github.com"})
	if len(start[0].Hosts) != 1 {
		t.Fatalf("input mutated: %+v", start)
	}
}

func TestApplyVaultGrant_AddsSecondCredential(t *testing.T) {
	start := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}
	got := applyVaultGrant(start, "stripe", []string{"api.stripe.com"})
	if len(got) != 2 {
		t.Fatalf("want 2 rules, got %+v", got)
	}
}

func TestApplyVaultRevoke_WholeCredential(t *testing.T) {
	start := []vaultRule{
		{Credential: "github", Hosts: []string{"api.github.com"}},
		{Credential: "stripe", Hosts: []string{"api.stripe.com"}},
	}
	got := applyVaultRevoke(start, "github", nil)
	want := []vaultRule{{Credential: "stripe", Hosts: []string{"api.stripe.com"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestApplyVaultRevoke_SingleHostKeepsCredential(t *testing.T) {
	start := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com", "uploads.github.com"}}}
	got := applyVaultRevoke(start, "github", []string{"uploads.github.com"})
	want := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestApplyVaultRevoke_LastHostDropsCredential(t *testing.T) {
	start := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}
	got := applyVaultRevoke(start, "github", []string{"api.github.com"})
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}

func TestApplyVaultRevoke_UnknownCredentialIsNoop(t *testing.T) {
	start := []vaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}
	got := applyVaultRevoke(start, "stripe", nil)
	if !reflect.DeepEqual(got, start) {
		t.Fatalf("got %+v, want unchanged %+v", got, start)
	}
}

func TestParseVaultRules_OK(t *testing.T) {
	got, err := parseVaultRules([]string{"github=api.github.com,uploads.github.com", "stripe=api.stripe.com"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []vaultRule{
		{Credential: "github", Hosts: []string{"api.github.com", "uploads.github.com"}},
		{Credential: "stripe", Hosts: []string{"api.stripe.com"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseVaultRules_RejectsMalformed(t *testing.T) {
	for _, bad := range []string{"github", "=api.github.com", "github="} {
		if _, err := parseVaultRules([]string{bad}); err == nil {
			t.Errorf("parseVaultRules(%q) = nil err, want error", bad)
		}
	}
}
