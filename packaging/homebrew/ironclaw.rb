# Homebrew formula source for IronClaw — the source of truth for the tap
# formula (the tap itself lives at nivardsec/homebrew-tap). Release automation
# regenerates `url` + `sha256` for each tagged release from this template; until
# the first release, install from source with `brew install --HEAD`.
#
#   brew install nivardsec/tap/ironclaw          # stable (once a release exists)
#   brew install --HEAD nivardsec/tap/ironclaw   # latest main
#
# Installs both the `ironctl` CLI and the `ironclaw-controlplane` daemon. CGO is
# required (the SQLCipher encrypted-queue binding).
class Ironclaw < Formula
  desc "Security-isolated multi-agent control plane with gVisor-sandboxed agents"
  homepage "https://github.com/nivardsec/ironclaw"
  url "https://github.com/nivardsec/ironclaw/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000" # release automation fills this in
  license "MIT"
  head "https://github.com/nivardsec/ironclaw.git", branch: "main"

  depends_on "go" => :build

  def install
    ENV["CGO_ENABLED"] = "1"
    ldflags = "-s -w"
    system "go", "build", *std_go_args(output: bin/"ironctl", ldflags: ldflags), "./cmd/ironctl"
    system "go", "build", *std_go_args(output: bin/"ironclaw-controlplane", ldflags: ldflags), "./cmd/controlplane"
  end

  test do
    # ironctl prints usage and exits non-zero on --help; assert on the output.
    assert_match "ironctl", shell_output("#{bin}/ironctl --help 2>&1", 1)
    # The daemon binary should at least exist and be runnable.
    assert_predicate bin/"ironclaw-controlplane", :exist?
  end
end
