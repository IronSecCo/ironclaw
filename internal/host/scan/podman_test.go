package scan

import (
	"strings"
	"testing"
)

// A rootless podman container: runs as root INSIDE the container, but the uid map
// remaps container-uid 0 to host uid 100000 (userns), all caps dropped, seccomp
// default, no docker.sock. OCIRuntime is crun. The rootless remap must earn credit
// on the non-root dimension even though Config.User is root.
const podmanRootlessInspect = `[{
  "Id": "pod0011223344",
  "Name": "rootless-app",
  "OCIRuntime": "crun",
  "Config": { "User": "0", "Image": "docker.io/library/alpine" },
  "HostConfig": {
    "NetworkMode": "slirp4netns",
    "Privileged": false,
    "ReadonlyRootfs": true,
    "CapAdd": null,
    "CapDrop": ["ALL"],
    "SecurityOpt": [],
    "PidMode": "private",
    "IpcMode": "private",
    "Runtime": "oci",
    "Binds": [],
    "IDMappings": { "UidMap": ["0:100000:65536"], "GidMap": ["0:100000:65536"] }
  },
  "Mounts": []
}]`

func TestSpecFromPodmanInspect_RootlessCredit(t *testing.T) {
	s, err := SpecFromPodmanInspect([]byte(podmanRootlessInspect), Unknown)
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != "podman" {
		t.Errorf("source=%q want podman", s.Source)
	}
	if s.Rootless != Yes {
		t.Fatal("rootless not inferred from uid map")
	}
	if s.UserNSHostUID != "100000" {
		t.Errorf("userns host uid=%q want 100000", s.UserNSHostUID)
	}
	// Runtime must come from OCIRuntime, not the generic HostConfig.Runtime "oci".
	if s.Runtime != "crun" {
		t.Errorf("runtime=%q want crun (OCIRuntime)", s.Runtime)
	}
	r := Score(s)
	nr := dimByKey(r, "user.nonroot")
	if nr.Verdict != VerdictWarn || nr.Score == 0 {
		t.Fatalf("rootless root container should earn partial non-root credit, got %s %d/%d", nr.Verdict, nr.Score, nr.Max)
	}
	// Rootless note surfaced.
	found := false
	for _, n := range r.Notes {
		if strings.Contains(n, "rootless") {
			found = true
		}
	}
	if !found {
		t.Error("expected a rootless note in the report")
	}
}

// Rootful podman (identity uid map, root user) must NOT get rootless credit.
func TestSpecFromPodmanInspect_RootfulNoCredit(t *testing.T) {
	const rootful = `[{
	  "Name": "rootful",
	  "OCIRuntime": "runc",
	  "Config": { "User": "0" },
	  "HostConfig": { "NetworkMode": "bridge", "Runtime": "oci", "IDMappings": { "UidMap": ["0:0:4294967295"] } }
	}]`
	s, err := SpecFromPodmanInspect([]byte(rootful), No)
	if err != nil {
		t.Fatal(err)
	}
	if s.Rootless == Yes {
		t.Error("identity uid map + rootless=No must not grade as rootless")
	}
	if dimByKey(Score(s), "user.nonroot").Verdict != VerdictFail {
		t.Error("rootful root container should FAIL the non-root dimension")
	}
}

// The daemon-level hint alone (no informative uid map) is enough to credit rootless.
func TestSpecFromPodmanInspect_HintCredits(t *testing.T) {
	const noMap = `[{"Name":"x","OCIRuntime":"crun","Config":{"User":"0"},"HostConfig":{"NetworkMode":"slirp4netns","CapDrop":["ALL"]}}]`
	s, err := SpecFromPodmanInspect([]byte(noMap), Yes)
	if err != nil {
		t.Fatal(err)
	}
	if s.Rootless != Yes {
		t.Fatal("rootless hint should mark the spec rootless")
	}
}

func TestSpecFromPodmanInspect_Errors(t *testing.T) {
	if _, err := SpecFromPodmanInspect([]byte("nope"), Unknown); err == nil {
		t.Error("expected parse error")
	}
	if _, err := SpecFromPodmanInspect([]byte("[]"), Unknown); err == nil {
		t.Error("expected empty-array error")
	}
}

func TestUserNSHostUID(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		wantUID string
		wantOK  bool
	}{
		{"rootless remap", []string{"0:100000:65536"}, "100000", true},
		{"identity map", []string{"0:0:4294967295"}, "", false},
		{"empty", nil, "", false},
		{"malformed", []string{"garbage"}, "", false},
		{"non-zero container id skipped", []string{"1000:200000:1"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uid, ok := userNSHostUID(c.in)
			if uid != c.wantUID || ok != c.wantOK {
				t.Errorf("userNSHostUID(%v)=(%q,%v) want (%q,%v)", c.in, uid, ok, c.wantUID, c.wantOK)
			}
		})
	}
}
