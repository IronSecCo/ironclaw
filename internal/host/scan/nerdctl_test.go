package scan

import "testing"

// nerdctl inspect is Docker-compatible. A hardened nerdctl container running under
// the containerd gVisor shim (io.containerd.runsc.v1) must grade identically to the
// Docker path and surface the hardened runtime informationally.
const nerdctlHardenedInspect = `[{
  "Id": "nerd0011223344",
  "Name": "/hardened-nerd",
  "Config": { "User": "65532" },
  "HostConfig": {
    "NetworkMode": "none",
    "Privileged": false,
    "ReadonlyRootfs": true,
    "CapAdd": null,
    "CapDrop": ["ALL"],
    "SecurityOpt": ["no-new-privileges"],
    "PidMode": "",
    "IpcMode": "private",
    "Runtime": "io.containerd.runsc.v1",
    "Binds": []
  },
  "Mounts": []
}]`

func TestSpecFromNerdctlInspect_Hardened(t *testing.T) {
	s, err := SpecFromNerdctlInspect([]byte(nerdctlHardenedInspect))
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != "nerdctl" {
		t.Errorf("source=%q want nerdctl", s.Source)
	}
	if s.Target != "hardened-nerd" {
		t.Errorf("target=%q", s.Target)
	}
	if s.RunAsNonRoot != Yes || s.CapDropAll != Yes {
		t.Error("hardened posture not parsed")
	}
	r := Score(s)
	if r.Score != 100 || r.Grade != "A" {
		t.Fatalf("hardened nerdctl scored %d/%s, want 100/A", r.Score, r.Grade)
	}
	// containerd gVisor shim recognized as a hardened runtime (informational).
	if r.HardenedRuntime != "gVisor (runsc)" {
		t.Errorf("hardenedRuntime=%q want gVisor (runsc)", r.HardenedRuntime)
	}
}

func TestStrongIsolationRuntime(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantOK   bool
	}{
		{"runsc", "gVisor (runsc)", true},
		{"io.containerd.runsc.v1", "gVisor (runsc)", true},
		{"gvisor", "gVisor (runsc)", true},
		{"kata-runtime", "Kata Containers", true},
		{"io.containerd.kata.v2", "Kata Containers", true},
		{"kata-qemu", "Kata Containers", true},
		{"firecracker", "Firecracker", true},
		{"runc-fc", "Firecracker", true},
		{"runc", "", false},
		{"crun", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		name, ok := StrongIsolationRuntime(c.in)
		if name != c.wantName || ok != c.wantOK {
			t.Errorf("StrongIsolationRuntime(%q)=(%q,%v) want (%q,%v)", c.in, name, ok, c.wantName, c.wantOK)
		}
	}
}

// A hardened runtime name never awards points on its own (IRO-429): a misconfigured
// container under runsc still scores low.
func TestHardenedRuntime_NoScoreBonus(t *testing.T) {
	const weakUnderRunsc = `[{
	  "Name": "/weak-runsc",
	  "Config": { "User": "0" },
	  "HostConfig": { "NetworkMode": "host", "Privileged": true, "Runtime": "io.containerd.runsc.v1" }
	}]`
	s, err := SpecFromNerdctlInspect([]byte(weakUnderRunsc))
	if err != nil {
		t.Fatal(err)
	}
	r := Score(s)
	if r.HardenedRuntime != "gVisor (runsc)" {
		t.Errorf("hardenedRuntime=%q want gVisor (runsc)", r.HardenedRuntime)
	}
	if r.Grade != "F" {
		t.Fatalf("privileged host-network container under runsc must still grade F, got %s (%d)", r.Grade, r.Score)
	}
}
