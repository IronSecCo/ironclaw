package isolation

import "testing"

func TestDefaultSeccompProfile(t *testing.T) {
	profile := DefaultSeccompProfile()
	if profile.DefaultAction != seccompActErrno {
		t.Fatalf("DefaultAction = %q, want %q", profile.DefaultAction, seccompActErrno)
	}
	if len(profile.Architectures) == 0 {
		t.Fatal("Architectures is empty")
	}
	for _, arch := range seccompArchitectures {
		if !contains(profile.Architectures, arch) {
			t.Fatalf("Architectures missing %q", arch)
		}
	}
	if len(profile.Syscalls) == 0 {
		t.Fatal("Syscalls is empty")
	}
	allowed := map[string]struct{}{}
	for _, rule := range profile.Syscalls {
		if rule.Action != seccompActAllow {
			t.Fatalf("syscall rule action = %q, want %q", rule.Action, seccompActAllow)
		}
		if len(rule.Names) == 0 {
			t.Fatal("syscall rule has no names")
		}
		for _, name := range rule.Names {
			allowed[name] = struct{}{}
		}
	}
	// Mounting filesystems would widen the sandbox boundary; deny-by-default should keep it blocked.
	if _, ok := allowed["mount"]; ok {
		t.Fatal("mount syscall is unexpectedly allowed")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
