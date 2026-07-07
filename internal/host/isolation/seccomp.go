package isolation

// This file defines the seccomp portion of the OCI runtime spec and IronClaw's
// restrictive default profile. Like oci.go it models only the runtime-spec
// fields we emit (stdlib-only; no runtime-spec import); the JSON tags match so a
// real OCI runtime accepts the profile verbatim.
//
// The profile is allowlist-shaped: the default action is to fail an unknown
// syscall with EPERM (SCMP_ACT_ERRNO), and only a curated set of syscalls a
// sandboxed Go process needs is allowed. This is defense in depth alongside
// network=none, all-caps-dropped, no_new_privs, and the read-only rootfs — a
// kernel-attack-surface reduction, not the primary boundary.

// OCISeccomp is the linux.seccomp section of the runtime spec.
type OCISeccomp struct {
	DefaultAction string          `json:"defaultAction"`
	Architectures []string        `json:"architectures,omitempty"`
	Syscalls      []OCISyscallSet `json:"syscalls,omitempty"`
}

// OCISyscallSet maps a group of syscall names to a single action.
type OCISyscallSet struct {
	Names  []string `json:"names"`
	Action string   `json:"action"`
}

// Seccomp actions (subset of the runtime-spec enum).
const (
	seccompActErrno = "SCMP_ACT_ERRNO"
	seccompActAllow = "SCMP_ACT_ALLOW"
)

// seccompArchitectures are the CPU architectures the profile applies to. Both
// the x86_64 and aarch64 native + compat sets are listed so the same profile is
// valid on common hosts.
var seccompArchitectures = []string{
	"SCMP_ARCH_X86_64",
	"SCMP_ARCH_X86",
	"SCMP_ARCH_X32",
	"SCMP_ARCH_AARCH64",
	"SCMP_ARCH_ARM",
}

// allowedSyscalls is the curated allowlist for a sandboxed static Go binary: the
// runtime (scheduler, GC, netpoll-on-pipes, signal handling), file/socket I/O
// against the bound queue files and the model-proxy unix socket, and process
// lifecycle. Anything not listed is denied by the EPERM default. It is a
// deliberately conservative baseline meant to be tightened, not widened.
var allowedSyscalls = []string{
	// Basic file & descriptor I/O.
	"read", "write", "readv", "writev", "pread64", "pwrite64", "preadv", "pwritev",
	"open", "openat", "openat2", "close", "close_range", "lseek", "fcntl", "fcntl64",
	"dup", "dup2", "dup3", "pipe", "pipe2", "fsync", "fdatasync", "ftruncate",
	"truncate", "fallocate", "flock",
	// Stat / metadata.
	"stat", "fstat", "lstat", "newfstatat", "statx", "fstatfs", "statfs",
	"getdents", "getdents64", "readlink", "readlinkat", "access", "faccessat",
	"faccessat2", "getcwd", "umask",
	// Directory / file management within the writable workspace.
	"mkdir", "mkdirat", "unlink", "unlinkat", "rename", "renameat", "renameat2",
	"chmod", "fchmod", "fchmodat", "chown", "fchown", "fchownat", "lchown",
	"symlink", "symlinkat", "link", "linkat", "utimensat", "futimesat",
	// Memory management.
	"mmap", "mmap2", "munmap", "mremap", "mprotect", "madvise", "brk", "mlock",
	"munlock", "msync",
	// Scheduling, futex, time — core Go runtime.
	"futex", "futex_waitv", "sched_yield", "sched_getaffinity", "sched_setaffinity",
	"nanosleep", "clock_nanosleep", "clock_gettime", "clock_getres", "gettimeofday",
	"times", "getrandom",
	// Signals.
	"rt_sigaction", "rt_sigprocmask", "rt_sigreturn", "rt_sigpending",
	"rt_sigtimedwait", "rt_sigqueueinfo", "rt_sigsuspend", "sigaltstack",
	"tgkill", "tkill", "kill", "restart_syscall",
	// Process / thread lifecycle. fork/vfork are needed by musl (busybox) and
	// glibc (dash) shells to spawn subprocesses for a multi-command `sh -c`;
	// omitting them made runc's non-gVisor fallback unable to run a normal shell
	// script ("sh: can't fork"). Allowing them does not widen the boundary —
	// network=none, all caps dropped, non-root, read-only rootfs, and no host
	// socket all remain. Docker's own default profile allows both.
	"clone", "clone3", "fork", "vfork", "execve", "execveat", "exit", "exit_group", "wait4",
	"waitid", "set_tid_address", "set_robust_list", "get_robust_list", "gettid",
	"getpid", "getppid", "prctl", "arch_prctl", "setpgid", "getpgid", "setsid",
	// Identity (read-only; caps are already dropped).
	"getuid", "geteuid", "getgid", "getegid", "getgroups", "getresuid",
	"getresgid", "setuid", "setgid",
	// Sockets — needed to connect() the bound model-proxy unix socket.
	"socket", "socketpair", "connect", "shutdown", "sendto", "recvfrom",
	"sendmsg", "recvmsg", "getsockname", "getpeername", "getsockopt", "setsockopt",
	// Polling / event loop.
	"poll", "ppoll", "select", "pselect6", "epoll_create", "epoll_create1",
	"epoll_ctl", "epoll_wait", "epoll_pwait", "eventfd", "eventfd2",
	// Misc safe queries.
	"uname", "sysinfo", "getrlimit", "prlimit64", "getrusage", "ioctl", "memfd_create",
}

// DefaultSeccompProfile returns IronClaw's restrictive default seccomp profile:
// deny-by-default (EPERM) with only the curated allowlist permitted.
func DefaultSeccompProfile() *OCISeccomp {
	return &OCISeccomp{
		DefaultAction: seccompActErrno,
		Architectures: seccompArchitectures,
		Syscalls: []OCISyscallSet{
			{Names: allowedSyscalls, Action: seccompActAllow},
		},
	}
}
