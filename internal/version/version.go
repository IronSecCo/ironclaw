// Package version exposes the build version stamped into IronClaw binaries.
package version

import "runtime/debug"

// Version is the release version. The release pipeline stamps it at build time:
//
//	go build -ldflags "-X github.com/nivardsec/ironclaw/internal/version.Version=v0.1.66" ./cmd/ironctl
//
// For a plain `go build` / `go run` it stays "dev"; String then falls back to the
// VCS revision embedded by the Go toolchain so even un-stamped builds are
// identifiable.
var Version = "dev"

// String returns the build version. When the binary was stamped by the release
// pipeline it returns that exact tag (e.g. "v0.1.66"); otherwise it derives a
// best-effort identifier from the module's VCS build info ("dev+<rev>[-dirty]").
func String() string {
	if Version != "dev" && Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev != "" {
		if len(rev) > 12 {
			rev = rev[:12]
		}
		return "dev+" + rev + dirty
	}
	return Version
}
