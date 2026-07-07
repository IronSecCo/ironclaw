package scan

import (
	"strings"
	"testing"
)

// A weak, unhardened container: root, default caps, writable rootfs, bridge
// network, docker.sock bind-mounted, host PID. The common misconfiguration.
const weakInspect = `[{
  "Id": "abc123def456789",
  "Name": "/weak-app",
  "Config": { "User": "" },
  "HostConfig": {
    "NetworkMode": "bridge",
    "Privileged": false,
    "ReadonlyRootfs": false,
    "CapAdd": ["SYS_ADMIN"],
    "CapDrop": null,
    "SecurityOpt": ["seccomp=unconfined"],
    "PidMode": "host",
    "IpcMode": "",
    "Runtime": "runc",
    "Binds": ["/var/run/docker.sock:/var/run/docker.sock"]
  },
  "Mounts": [ { "Source": "/var/run/docker.sock", "Destination": "/var/run/docker.sock" } ]
}]`

// A hardened IronClaw ic-sbx container: non-root, all caps dropped, read-only,
// network=none, no docker.sock, gVisor runtime, no host namespaces.
const hardenedInspect = `[{
  "Id": "sbx0011223344",
  "Name": "/ic-sbx-mg-abc123",
  "Config": { "User": "65532:65532" },
  "HostConfig": {
    "NetworkMode": "none",
    "Privileged": false,
    "ReadonlyRootfs": true,
    "CapAdd": null,
    "CapDrop": ["ALL"],
    "SecurityOpt": ["no-new-privileges", "seccomp=/etc/ironclaw/seccomp.json"],
    "PidMode": "",
    "IpcMode": "private",
    "Runtime": "runsc",
    "Binds": []
  },
  "Mounts": []
}]`

func TestSpecFromDockerInspect_Weak(t *testing.T) {
	s, err := SpecFromDockerInspect([]byte(weakInspect))
	if err != nil {
		t.Fatal(err)
	}
	if s.Target != "weak-app" {
		t.Errorf("target=%q", s.Target)
	}
	if s.RunAsNonRoot != No {
		t.Error("empty user should grade as root")
	}
	if s.DockerSock != Yes {
		t.Error("docker.sock bind not detected")
	}
	if s.HostPID != Yes {
		t.Error("host PID not detected")
	}
	if s.Seccomp != "unconfined" {
		t.Errorf("seccomp=%q want unconfined", s.Seccomp)
	}
	r := Score(s)
	if r.Score >= 50 {
		t.Fatalf("weak container scored %d (grade %s); expected a low/failing score", r.Score, r.Grade)
	}
	if dimByKey(r, "docker.sock").Verdict != VerdictFail {
		t.Error("docker.sock should FAIL")
	}
}

func TestSpecFromDockerInspect_Hardened(t *testing.T) {
	s, err := SpecFromDockerInspect([]byte(hardenedInspect))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Target, "ic-sbx-") {
		t.Errorf("target=%q", s.Target)
	}
	if s.RunAsNonRoot != Yes {
		t.Error("65532 should grade as non-root")
	}
	if s.CapDropAll != Yes {
		t.Error("cap drop ALL not detected")
	}
	if s.Runtime != "runsc" {
		t.Errorf("runtime=%q want runsc", s.Runtime)
	}
	if s.Seccomp != "confined" {
		t.Errorf("custom seccomp profile should grade confined, got %q", s.Seccomp)
	}
	r := Score(s)
	if r.Score != 100 || r.Grade != "A" {
		t.Fatalf("hardened ic-sbx scored %d/%s, want 100/A", r.Score, r.Grade)
	}
}

// A --privileged container is the escape-hatch posture; it must score near the
// floor even though nothing else is obviously wrong.
func TestSpecFromDockerInspect_Privileged(t *testing.T) {
	const priv = `[{"Name":"/p","Config":{"User":"0"},"HostConfig":{"Privileged":true,"NetworkMode":"bridge","Runtime":"runc"}}]`
	s, err := SpecFromDockerInspect([]byte(priv))
	if err != nil {
		t.Fatal(err)
	}
	if s.Privileged != Yes {
		t.Fatal("privileged not detected")
	}
	if s.Seccomp != "unconfined" {
		t.Errorf("privileged disables seccomp, got %q", s.Seccomp)
	}
	if g := Score(s).Grade; g != "F" && g != "D" {
		t.Fatalf("privileged graded %s; expected D or F", g)
	}
}

func TestSpecFromDockerInspect_Errors(t *testing.T) {
	if _, err := SpecFromDockerInspect([]byte("not json")); err == nil {
		t.Error("expected parse error")
	}
	if _, err := SpecFromDockerInspect([]byte("[]")); err == nil {
		t.Error("expected empty-array error")
	}
	// A single object (not wrapped in an array) is tolerated.
	if _, err := SpecFromDockerInspect([]byte(`{"Name":"/x","HostConfig":{"NetworkMode":"none"}}`)); err != nil {
		t.Errorf("single object should parse: %v", err)
	}
}
