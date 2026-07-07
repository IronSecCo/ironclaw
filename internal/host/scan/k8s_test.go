package scan

import "testing"

const podWeak = `
apiVersion: v1
kind: Pod
metadata:
  name: weak-pod
spec:
  hostPID: true
  hostNetwork: true
  containers:
    - name: app
      image: nginx
      securityContext:
        privileged: true
      volumeMounts:
        - name: sock
          mountPath: /var/run/docker.sock
  volumes:
    - name: sock
      hostPath:
        path: /var/run/docker.sock
`

const podHardened = `
apiVersion: v1
kind: Pod
metadata:
  name: hardened-pod
spec:
  containers:
    - name: app
      image: ironclaw
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        readOnlyRootFilesystem: true
        allowPrivilegeEscalation: false
        capabilities:
          drop: [ALL]
        seccompProfile:
          type: RuntimeDefault
`

const deploymentHardened = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dep
spec:
  template:
    spec:
      containers:
        - name: app
          securityContext:
            runAsUser: 1000
            readOnlyRootFilesystem: true
            capabilities:
              drop: [ALL]
            seccompProfile:
              type: RuntimeDefault
`

func TestSpecFromK8s_Weak(t *testing.T) {
	s, err := SpecFromK8s([]byte(podWeak))
	if err != nil {
		t.Fatal(err)
	}
	if s.Target != "weak-pod" {
		t.Errorf("target=%q", s.Target)
	}
	if s.Privileged != Yes || s.HostPID != Yes || s.HostNetwork != Yes || s.DockerSock != Yes {
		t.Errorf("weak postures not parsed: %+v", s)
	}
	if Score(s).Grade != "F" {
		t.Errorf("weak pod graded %s, want F", Score(s).Grade)
	}
}

func TestSpecFromK8s_Hardened(t *testing.T) {
	s, err := SpecFromK8s([]byte(podHardened))
	if err != nil {
		t.Fatal(err)
	}
	if s.RunAsNonRoot != Yes || s.CapDropAll != Yes || s.ReadonlyRoot != Yes || s.Seccomp != "confined" {
		t.Errorf("hardened postures not parsed: %+v", s)
	}
	// Kubernetes network egress is not visible in a pod manifest, so a hardened
	// pod cannot reach a perfect 100; it should still be a strong B or better.
	r := Score(s)
	if r.Grade != "A" && r.Grade != "B" {
		t.Errorf("hardened pod graded %s (%d), want A or B", r.Grade, r.Score)
	}
	if dimByKey(r, "network.isolated").Verdict != VerdictWarn {
		t.Error("pod network should grade WARN (NetworkPolicy not visible)")
	}
}

func TestSpecFromK8s_Deployment(t *testing.T) {
	s, err := SpecFromK8s([]byte(deploymentHardened))
	if err != nil {
		t.Fatal(err)
	}
	// A workload template's pod spec must be located under spec.template.spec.
	if s.CapDropAll != Yes || s.ReadonlyRoot != Yes {
		t.Errorf("deployment template not parsed: %+v", s)
	}
}

func TestSpecFromK8s_Errors(t *testing.T) {
	if _, err := SpecFromK8s([]byte("kind: Pod\nspec: {}\n")); err == nil {
		t.Error("expected error for a pod with no containers")
	}
	if _, err := SpecFromK8s([]byte(":\n  not yaml")); err == nil {
		t.Error("expected parse error")
	}
}
