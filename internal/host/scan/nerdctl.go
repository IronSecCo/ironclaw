package scan

// SpecFromNerdctlInspect parses `nerdctl inspect <container>` output into a Spec.
// nerdctl targets Docker CLI compatibility and emits a Docker-compatible inspect
// schema, so the shared Docker-family grading body handles it verbatim; only the
// source label differs.
//
// containerd runtime handlers surface in HostConfig.Runtime as reverse-DNS names
// (e.g. "io.containerd.runc.v2", "io.containerd.runsc.v1" for gVisor,
// "io.containerd.kata.v2" for Kata); StrongIsolationRuntime recognizes those, so
// a hardened runtime under nerdctl is surfaced informationally like any other.
//
// Fail-closed: nerdctl's inspect has historically been less complete than
// Docker's; any field it omits stays at its zero value and is graded as insecure
// by the scorers rather than silently passed.
func SpecFromNerdctlInspect(raw []byte) (Spec, error) {
	f, err := firstDockerInspect(raw)
	if err != nil {
		return Spec{}, err
	}
	return specFromDockerLike(f, "nerdctl"), nil
}
