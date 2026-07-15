package scan

import (
	"strings"
	"testing"
)

// nomadHardenedJob mimics `nomad job run -output` for a job whose single docker
// task is fully hardened: non-root user, drop ALL capabilities, default seccomp
// profile, network=none, read-only rootfs, no docker.sock. The `-output` form
// wraps the job as {"Job": {...}} and the driver `config` block uses lowercase
// HCL keys.
const nomadHardenedJob = `{
  "Job": {
    "ID": "web",
    "Name": "web",
    "TaskGroups": [
      {
        "Name": "app",
        "Networks": [{"Mode": "none"}],
        "Tasks": [
          {
            "Name": "server",
            "Driver": "docker",
            "User": "1000",
            "Config": {
              "image": "web:1",
              "privileged": false,
              "cap_drop": ["ALL"],
              "network_mode": "none",
              "readonly_rootfs": true,
              "security_opt": ["no-new-privileges"]
            }
          }
        ]
      }
    ]
  }
}`

// nomadPrivilegedJob mimics a raw api.Job document (no {"Job": ...} wrapper) with
// a privileged docker task that host-mounts the docker socket and shares the host
// network/PID namespaces — the worst posture.
const nomadPrivilegedJob = `{
  "ID": "legacy",
  "Name": "legacy",
  "TaskGroups": [
    {
      "Name": "ci",
      "Tasks": [
        {
          "Name": "runner",
          "Driver": "docker",
          "Config": {
            "image": "docker:dind",
            "privileged": true,
            "network_mode": "host",
            "pid_mode": "host",
            "volumes": ["/var/run/docker.sock:/var/run/docker.sock"]
          }
        }
      ]
    }
  ]
}`

// nomadMixedJob pairs a hardened task with a porous one across two groups; the
// aggregate must reflect the WEAKEST task. It also carries a non-docker (exec)
// task that must be skipped.
const nomadMixedJob = `{
  "Job": {
    "Name": "mixed",
    "TaskGroups": [
      {
        "Name": "safe",
        "Networks": [{"Mode": "none"}],
        "Tasks": [
          {
            "Name": "api",
            "Driver": "docker",
            "User": "65532",
            "Config": {
              "image": "api:1",
              "cap_drop": ["ALL"],
              "network_mode": "none",
              "readonly_rootfs": true
            }
          },
          {
            "Name": "sidecar",
            "Driver": "exec",
            "Config": {"command": "/bin/true"}
          }
        ]
      },
      {
        "Name": "porous",
        "Tasks": [
          {
            "Name": "worker",
            "Driver": "docker",
            "Config": {
              "image": "worker:1",
              "privileged": true,
              "network_mode": "host",
              "mount": [{"type": "bind", "source": "/var/run/docker.sock", "target": "/var/run/docker.sock"}]
            }
          }
        ]
      }
    ]
  }
}`

// nomadBaselineJob is a docker task with NO security config at all: the docker
// driver defaults apply (default seccomp profile, default capability set, bridge
// network, image-default user, writable rootfs).
const nomadBaselineJob = `{
  "Job": {
    "Name": "baseline",
    "TaskGroups": [
      {
        "Name": "cache",
        "Tasks": [
          {"Name": "redis", "Driver": "docker", "Config": {"image": "redis:7"}}
        ]
      }
    ]
  }
}`

func gradeNomad(t *testing.T, raw string) (Report, Spec) {
	t.Helper()
	specs, err := SpecsFromNomad([]byte(raw))
	if err != nil {
		t.Fatalf("SpecsFromNomad: %v", err)
	}
	report, worst, err := AggregateNomad(specs, "job.nomad.json")
	if err != nil {
		t.Fatalf("AggregateNomad: %v", err)
	}
	return report, worst
}

func TestNomadHardenedJobGradesA(t *testing.T) {
	report, _ := gradeNomad(t, nomadHardenedJob)
	if report.Grade != "A" {
		t.Fatalf("hardened nomad job: got grade %s (%d/100), want A; notes: %v", report.Grade, report.Score, report.Notes)
	}
	if report.Score != 100 {
		t.Errorf("hardened nomad job: got %d/100, want 100", report.Score)
	}
	if report.Source != "nomad" {
		t.Errorf("source = %q, want nomad", report.Source)
	}
}

func TestNomadPrivilegedDockerSockGradesF(t *testing.T) {
	report, worst := gradeNomad(t, nomadPrivilegedJob)
	if report.Grade != "F" {
		t.Fatalf("privileged nomad job: got grade %s (%d/100), want F", report.Grade, report.Score)
	}
	if worst.DockerSock != Yes {
		t.Errorf("docker.sock not detected on privileged task: %+v", worst)
	}
	if worst.Privileged != Yes {
		t.Errorf("privileged not detected: %+v", worst)
	}
	if worst.HostNetwork != Yes || worst.HostPID != Yes {
		t.Errorf("host network/PID not detected: hostNet=%v hostPID=%v", worst.HostNetwork, worst.HostPID)
	}
}

func TestNomadWeakestTaskWins(t *testing.T) {
	report, worst := gradeNomad(t, nomadMixedJob)
	if report.Grade != "F" {
		t.Fatalf("mixed nomad job: got grade %s (%d/100), want F (weakest task governs)", report.Grade, report.Score)
	}
	if !strings.Contains(worst.Target, "worker") {
		t.Errorf("weakest task = %q, want the porous worker", worst.Target)
	}
	// The exec sidecar and the hardened api task must both be present in the
	// per-task roll-up; the exec task is skipped (only 2 docker tasks graded).
	joined := strings.Join(report.Notes, " ")
	if !strings.Contains(joined, "graded 2 nomad docker task") {
		t.Errorf("expected 2 docker tasks graded (exec skipped); notes: %v", report.Notes)
	}
}

func TestNomadHostNetworkPenalty(t *testing.T) {
	// A docker task hardened in every dimension EXCEPT host networking must lose
	// the network dimension relative to the fully hardened job.
	const hostNet = `{"Job":{"Name":"n","TaskGroups":[{"Name":"g","Tasks":[
      {"Name":"t","Driver":"docker","User":"1000","Config":{
        "cap_drop":["ALL"],"network_mode":"host","readonly_rootfs":true}}]}]}}`
	report, worst := gradeNomad(t, hostNet)
	if worst.HostNetwork != Yes {
		t.Fatalf("host network not detected: %+v", worst)
	}
	if report.Score >= 100 {
		t.Errorf("host-network job should not reach 100; got %d", report.Score)
	}
	// host network zeroes both the network dimension and host-namespace dimension.
	full, _ := gradeNomad(t, nomadHardenedJob)
	if report.Score >= full.Score {
		t.Errorf("host-network job (%d) should score below the network=none job (%d)", report.Score, full.Score)
	}
}

func TestNomadBaselineDefaults(t *testing.T) {
	// A bare docker task with no security config grades the docker DEFAULTS: the
	// default seccomp profile PASSES (confined), the default capability set and
	// bridge network are weak, user/rootfs unknown -> a mid/low D score.
	report, worst := gradeNomad(t, nomadBaselineJob)
	if worst.Seccomp != "confined" {
		t.Errorf("baseline seccomp = %q, want confined (docker default profile)", worst.Seccomp)
	}
	if worst.NetworkMode != "bridge" {
		t.Errorf("baseline network = %q, want bridge (docker default)", worst.NetworkMode)
	}
	if worst.RunAsNonRoot != Unknown {
		t.Errorf("baseline user = %v, want Unknown (image default not pinned)", worst.RunAsNonRoot)
	}
	if report.Grade != "D" {
		t.Errorf("baseline nomad task: got grade %s (%d/100), want D", report.Grade, report.Score)
	}
}

func TestNomadBoolAsString(t *testing.T) {
	// HCL2 renders bools as JSON bools, but a hand-authored API job may quote
	// them. nomadBool must accept "true"/"false" strings.
	const quoted = `{"Job":{"Name":"n","TaskGroups":[{"Name":"g","Tasks":[
      {"Name":"t","Driver":"docker","Config":{
        "privileged":"true","readonly_rootfs":"false"}}]}]}}`
	specs, err := SpecsFromNomad([]byte(quoted))
	if err != nil {
		t.Fatalf("SpecsFromNomad: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	if specs[0].Privileged != Yes {
		t.Errorf("privileged=\"true\" not decoded: %v", specs[0].Privileged)
	}
	if specs[0].ReadonlyRoot != No {
		t.Errorf("readonly_rootfs=\"false\" not decoded: %v", specs[0].ReadonlyRoot)
	}
}

func TestNomadNoDockerTasksIsError(t *testing.T) {
	// A job with only non-docker drivers has nothing to grade: fail-closed (an
	// error), not a silent pass.
	const execOnly = `{"Job":{"Name":"n","TaskGroups":[{"Name":"g","Tasks":[
      {"Name":"t","Driver":"exec","Config":{"command":"/bin/true"}}]}]}}`
	specs, err := SpecsFromNomad([]byte(execOnly))
	if err != nil {
		t.Fatalf("SpecsFromNomad: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("exec task should be skipped; got %d specs", len(specs))
	}
	if _, _, err := AggregateNomad(specs, "job"); err == nil {
		t.Error("AggregateNomad on an empty task set must error (fail-closed)")
	}
}

func TestNomadMalformedJSONErrors(t *testing.T) {
	if _, err := SpecsFromNomad([]byte("{not json")); err == nil {
		t.Error("malformed JSON must return a parse error")
	}
}
