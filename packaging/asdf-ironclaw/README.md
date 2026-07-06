# asdf / mise plugin for IronClaw

Install pinned, checksum-verified IronClaw release binaries (`ironctl` and
`ironclaw-controlplane`) with [asdf](https://asdf-vm.com) or
[mise](https://mise.jdx.dev). No account, no build toolchain, no `sudo`: the plugin
pulls the release tarball from GitHub, verifies it against the published
`SHA256SUMS`, and drops the binaries on your managed PATH.

## Layout

```
bin/list-all        # every installable version, oldest first
bin/latest-stable   # newest released version
bin/download        # fetch + SHA256-verify + extract a pinned release
bin/install         # place ironctl + ironclaw-controlplane on PATH
```

These scripts implement the asdf plugin contract, which mise consumes natively.

## Use it (once the plugin is registered)

The plugin lives in its own repository so `asdf plugin add` / `mise use` can clone
it (asdf plugins are resolved as standalone repos). Point either tool at it:

```sh
# asdf
asdf plugin add ironclaw https://github.com/IronSecCo/asdf-ironclaw.git
asdf install ironclaw latest
asdf global ironclaw latest

# mise (reads the same asdf plugin)
mise use -g asdf:IronSecCo/asdf-ironclaw@latest
ironctl version
```

Pin a project with a `.tool-versions` file:

```
ironclaw 0.1.217
```

## No-plugin one-liner (works today, CLI only)

mise's `ubi` backend installs the `ironctl` CLI straight from the GitHub release
with no plugin repo and no account:

```sh
mise use -g "ubi:IronSecCo/ironclaw[exe=ironctl]@latest"
ironctl version
```

`ubi` installs a single binary, so it gives you `ironctl`; use the asdf plugin above
when you also want `ironclaw-controlplane`.

## Local test (no asdf/mise required)

The scripts are plain shell honoring the asdf env contract, so you can exercise them
directly:

```sh
export ASDF_INSTALL_VERSION=0.1.217
export ASDF_DOWNLOAD_PATH=$(mktemp -d)
export ASDF_INSTALL_PATH=$(mktemp -d)
bin/download
bin/install
"$ASDF_INSTALL_PATH/bin/ironctl" version
```
