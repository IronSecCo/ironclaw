// OWNER: T-104

package isolation

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildOCISpecDefaultResourceLimits(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	res := spec.Linux.Resources
	if res == nil {
		t.Fatal("Linux.Resources must always be set (defense in depth)")
	}
	if res.Memory == nil || res.Memory.Limit != defaultMemoryLimitBytes {
		t.Fatalf("memory limit = %+v, want %d", res.Memory, defaultMemoryLimitBytes)
	}
	if res.CPU == nil || res.CPU.Quota != defaultCPUQuota || res.CPU.Period != defaultCPUPeriod {
		t.Fatalf("cpu limit = %+v, want quota=%d period=%d", res.CPU, defaultCPUQuota, defaultCPUPeriod)
	}
	if res.Pids == nil || res.Pids.Limit != defaultPidsLimit {
		t.Fatalf("pids limit = %+v, want %d", res.Pids, defaultPidsLimit)
	}
}

func TestBuildOCISpecCustomResourceLimits(t *testing.T) {
	s := hardenedTestSpec()
	s.MemoryLimitBytes = 256 * 1024 * 1024
	s.CPUQuota = 50_000
	s.CPUPeriod = 100_000
	s.PidsLimit = 64
	spec, err := BuildOCISpec(s)
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	res := spec.Linux.Resources
	if res.Memory.Limit != s.MemoryLimitBytes {
		t.Fatalf("memory limit = %d, want %d", res.Memory.Limit, s.MemoryLimitBytes)
	}
	if res.CPU.Quota != s.CPUQuota || res.CPU.Period != s.CPUPeriod {
		t.Fatalf("cpu = %+v, want quota=%d period=%d", res.CPU, s.CPUQuota, s.CPUPeriod)
	}
	if res.Pids.Limit != s.PidsLimit {
		t.Fatalf("pids limit = %d, want %d", res.Pids.Limit, s.PidsLimit)
	}
}

func TestBuildOCISpecAttachesSeccomp(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	sc := spec.Linux.Seccomp
	if sc == nil {
		t.Fatal("Linux.Seccomp must always be set")
	}
	if sc.DefaultAction != seccompActErrno {
		t.Fatalf("default action = %q, want %q (deny-by-default)", sc.DefaultAction, seccompActErrno)
	}
	if len(sc.Architectures) == 0 {
		t.Fatal("seccomp profile must list architectures")
	}
	if len(sc.Syscalls) != 1 || sc.Syscalls[0].Action != seccompActAllow {
		t.Fatalf("expected one allow syscall set, got %+v", sc.Syscalls)
	}
}

func TestDefaultSeccompProfileAllowsRuntimeDeniesDangerous(t *testing.T) {
	allow := map[string]bool{}
	for _, n := range DefaultSeccompProfile().Syscalls[0].Names {
		allow[n] = true
	}
	// Core runtime + the model-proxy socket connect path must be permitted.
	for _, must := range []string{"read", "write", "futex", "mmap", "connect", "epoll_wait", "clone"} {
		if !allow[must] {
			t.Errorf("allowlist missing required syscall %q", must)
		}
	}
	// High-risk syscalls must NOT be on the allowlist (denied by the ERRNO default).
	for _, banned := range []string{"ptrace", "mount", "reboot", "kexec_load", "bpf", "init_module", "setns", "pivot_root"} {
		if allow[banned] {
			t.Errorf("allowlist must not permit dangerous syscall %q", banned)
		}
	}
}

func TestOCISpecResourcesAndSeccompMarshalJSON(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// runtime-spec JSON paths the runtime expects.
	for _, want := range []string{`"resources"`, `"memory"`, `"pids"`, `"seccomp"`, `"defaultAction":"SCMP_ACT_ERRNO"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("emitted config.json missing %s", want)
		}
	}
}
