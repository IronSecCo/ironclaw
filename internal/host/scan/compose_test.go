package scan

import "testing"

const composeWeak = `
services:
  web:
    image: nginx
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    pid: host
`

const composeHardened = `
services:
  agent:
    image: ironclaw
    user: "65532:65532"
    read_only: true
    network_mode: none
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    runtime: runsc
`

const composeTwo = `
services:
  a: { image: x }
  b: { image: y }
`

func TestSpecFromCompose_Weak(t *testing.T) {
	s, err := SpecFromCompose([]byte(composeWeak), "web")
	if err != nil {
		t.Fatal(err)
	}
	if s.DockerSock != Yes || s.HostPID != Yes {
		t.Errorf("docker.sock/hostPID not detected: %+v", s)
	}
	if s.RunAsNonRoot != No {
		t.Error("no user: pinned -> fail-closed to root")
	}
	if Score(s).Score >= 50 {
		t.Errorf("weak compose scored %d, want low", Score(s).Score)
	}
}

func TestSpecFromCompose_Hardened(t *testing.T) {
	s, err := SpecFromCompose([]byte(composeHardened), "agent")
	if err != nil {
		t.Fatal(err)
	}
	if s.RunAsNonRoot != Yes || s.CapDropAll != Yes || s.ReadonlyRoot != Yes || s.NetworkMode != "none" {
		t.Errorf("hardened postures not parsed: %+v", s)
	}
	r := Score(s)
	if r.Grade != "A" {
		t.Errorf("hardened compose graded %s (%d), want A", r.Grade, r.Score)
	}
}

func TestSpecFromCompose_SingleServiceAutoSelect(t *testing.T) {
	if _, err := SpecFromCompose([]byte(composeHardened), ""); err != nil {
		t.Errorf("single service should auto-select: %v", err)
	}
}

func TestSpecFromCompose_AmbiguousService(t *testing.T) {
	if _, err := SpecFromCompose([]byte(composeTwo), ""); err == nil {
		t.Error("expected error requiring --service for multi-service file")
	}
	if _, err := SpecFromCompose([]byte(composeTwo), "missing"); err == nil {
		t.Error("expected error for unknown service")
	}
}
