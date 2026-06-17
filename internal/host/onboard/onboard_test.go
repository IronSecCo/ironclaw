package onboard

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
)

// fakeDeps builds a Deps with in-memory fakes. env and files are mutable maps.
func fakeDeps(env map[string]string, files map[string]string) *Deps {
	if env == nil {
		env = map[string]string{}
	}
	if files == nil {
		files = map[string]string{}
	}
	d := Deps{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Getenv:   func(k string) string { return env[k] },
		GenToken: func() (string, error) { return "MINTED_TOKEN", nil },
		ReadFile: func(p string) ([]byte, error) {
			v, ok := files[p]
			if !ok {
				return nil, fs.ErrNotExist
			}
			return []byte(v), nil
		},
		WriteFile:  func(p string, b []byte, _ fs.FileMode) error { files[p] = string(b); return nil },
		MkdirAll:   func(string, fs.FileMode) error { return nil },
		Stat:       func(string) (fs.FileInfo, error) { return nil, fs.ErrNotExist },
		Ping:       func(context.Context, string) error { return nil },
		Stdout:     io.Discard,
		ConfigPath: "/tmp/ironclaw/onboard.env",
	}
	return &d
}

func stepByName(res Result, name string) Step {
	for _, s := range res.Steps {
		if s.Name == name {
			return s
		}
	}
	return Step{Name: name, Status: "<absent>"}
}

func TestEnvTokenWins(t *testing.T) {
	files := map[string]string{}
	d := fakeDeps(map[string]string{"IRONCLAW_API_TOKEN": "ENVTOK"}, files)
	res, err := d.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "ENVTOK" {
		t.Fatalf("token = %q, want ENVTOK", res.Token)
	}
	if got := stepByName(res, "api-token").Status; got != StatusSkipped {
		t.Fatalf("api-token status = %q, want skipped", got)
	}
	if len(files) != 0 {
		t.Fatalf("env token should not write config; wrote %v", files)
	}
}

func TestMintAndPersist(t *testing.T) {
	files := map[string]string{}
	d := fakeDeps(nil, files)
	res, err := d.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "MINTED_TOKEN" {
		t.Fatalf("token = %q, want MINTED_TOKEN", res.Token)
	}
	if got := stepByName(res, "api-token").Status; got != StatusOK {
		t.Fatalf("api-token status = %q, want ok", got)
	}
	persisted, ok := files[d.ConfigPath]
	if !ok || !strings.Contains(persisted, "IRONCLAW_API_TOKEN=MINTED_TOKEN") {
		t.Fatalf("token not persisted to config: %q", persisted)
	}
}

func TestIdempotentReuseConfigToken(t *testing.T) {
	files := map[string]string{"/tmp/ironclaw/onboard.env": "IRONCLAW_API_TOKEN=PRIOR\n"}
	d := fakeDeps(nil, files)
	d.ConfigPath = "/tmp/ironclaw/onboard.env"
	d.GenToken = func() (string, error) {
		t.Fatal("should not mint when a config token exists without --force")
		return "", nil
	}
	res, err := d.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "PRIOR" {
		t.Fatalf("token = %q, want PRIOR (reused)", res.Token)
	}
	if got := stepByName(res, "api-token").Status; got != StatusSkipped {
		t.Fatalf("api-token status = %q, want skipped", got)
	}
}

func TestForceReplacesConfigToken(t *testing.T) {
	files := map[string]string{"/tmp/ironclaw/onboard.env": "IRONCLAW_API_TOKEN=PRIOR\n"}
	d := fakeDeps(nil, files)
	d.ConfigPath = "/tmp/ironclaw/onboard.env"
	res, err := d.Run(context.Background(), Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "MINTED_TOKEN" {
		t.Fatalf("token = %q, want MINTED_TOKEN (forced)", res.Token)
	}
	if !strings.Contains(files[d.ConfigPath], "MINTED_TOKEN") {
		t.Fatalf("config not overwritten: %q", files[d.ConfigPath])
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	files := map[string]string{}
	d := fakeDeps(nil, files)
	res, err := d.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := stepByName(res, "api-token").Status; got != StatusPlanned {
		t.Fatalf("api-token status = %q, want planned", got)
	}
	if len(files) != 0 {
		t.Fatalf("dry-run must not write; wrote %v", files)
	}
}

func TestRuntimeAndChannelDetection(t *testing.T) {
	files := map[string]string{}
	d := fakeDeps(map[string]string{
		"IRONCLAW_API_TOKEN":          "T",
		"ANTHROPIC_API_KEY":           "sk-ant-x",
		"IRONCLAW_TELEGRAM_BOT_TOKEN": "tg",
	}, files)
	d.LookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", errors.New("nope")
	}
	res, _ := d.Run(context.Background(), Options{})
	if s := stepByName(res, "runtime"); s.Status != StatusOK || !strings.Contains(s.Detail, "docker") {
		t.Fatalf("runtime step = %+v, want ok+docker", s)
	}
	if s := stepByName(res, "model-credential"); s.Status != StatusOK {
		t.Fatalf("model-credential = %q, want ok", s.Status)
	}
	if s := stepByName(res, "channel"); s.Status != StatusOK || !strings.Contains(s.Detail, "telegram") {
		t.Fatalf("channel step = %+v, want ok+telegram", s)
	}
}

func TestVerifyReachableVsNot(t *testing.T) {
	d := fakeDeps(map[string]string{"IRONCLAW_API_TOKEN": "T"}, nil)
	res, _ := d.Run(context.Background(), Options{})
	if s := stepByName(res, "verify"); s.Status != StatusOK {
		t.Fatalf("verify = %q, want ok", s.Status)
	}

	d2 := fakeDeps(map[string]string{"IRONCLAW_API_TOKEN": "T"}, nil)
	d2.Ping = func(context.Context, string) error { return errors.New("conn refused") }
	res2, _ := d2.Run(context.Background(), Options{})
	if s := stepByName(res2, "verify"); s.Status != StatusAction {
		t.Fatalf("verify(down) = %q, want action", s.Status)
	}
	if !res2.Ok() {
		t.Fatal("an unreachable API is a guided action, not a hard failure; Ok() should stay true")
	}
}

func TestDefaultAddrAndFirstMessage(t *testing.T) {
	d := fakeDeps(map[string]string{"IRONCLAW_API_TOKEN": "T"}, nil)
	res, _ := d.Run(context.Background(), Options{})
	if res.APIURL != "http://127.0.0.1:8787" {
		t.Fatalf("APIURL = %q, want default loopback", res.APIURL)
	}
	if !strings.Contains(res.FirstMessage, "change submit") {
		t.Fatalf("FirstMessage = %q, want a change-submit hint", res.FirstMessage)
	}
}
