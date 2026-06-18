# Quickstart — your first approved action in 5 minutes

This walks you from a clean clone to **submitting a change, approving it at the human-approval gateway,
and reading the audit log** — entirely on your machine, in `--dev` mode (loopback, no gVisor required).

> **What you're seeing:** every mutation in IronClaw — persona, tools, packages, wiring, permissions,
> mounts — is *held* at a deterministic gateway until a human approves it. There is no path that
> bypasses it. The quickstart makes that choke point concrete in a couple of commands.

---

## Prerequisites

- **Go 1.23+** and a **C toolchain** — IronClaw builds with `CGO_ENABLED=1` (the encrypted-queue
  binding, SQLCipher, is unconditional). macOS: `xcode-select --install`. Debian/Ubuntu: `sudo apt-get install build-essential`.
- **An Anthropic API key** (`ANTHROPIC_API_KEY`) — held host-side; it never enters a sandbox. For this
  `--dev` walkthrough you won't actually call the model, but the daemon expects the variable to be set.
- **`openssl`** (almost always preinstalled) — used to mint a local API token.

Check Go:

```sh
go version   # expect go1.23 or newer
```

---

## 1. Get IronClaw and build

```sh
git clone https://github.com/IronSecCo/ironclaw.git
cd ironclaw
CGO_ENABLED=1 go build -o bin/ ./cmd/controlplane ./cmd/ironctl
```

This produces `bin/controlplane` (the host daemon) and `bin/ironctl` (the admin CLI).
If the build fails with an SQLite/cgo error, your C toolchain isn't set up — see Prerequisites.

## 2. Start the control-plane (Terminal 1)

```sh
export ANTHROPIC_API_KEY=sk-ant-...                 # held host-side; never enters the sandbox
export IRONCLAW_API_TOKEN=$(openssl rand -hex 32)   # bearer token for the admin API
echo "API token: $IRONCLAW_API_TOKEN"               # copy this — you need it in Terminal 2

./bin/controlplane --dev --api-addr 127.0.0.1:8787
```

`--dev` binds to loopback and uses in-memory backends — **no gVisor, no containerd, no root**. Leave this
running. You should see the daemon log that it has started and is serving on `127.0.0.1:8787`.

## 3. Point `ironctl` at it (Terminal 2)

```sh
cd ironclaw
export IRONCLAW_API_TOKEN=<paste the token printed in Terminal 1>
# --addr defaults to http://127.0.0.1:8787, so no extra flag is needed in dev
```

Confirm connectivity by reading the (empty) audit log:

```sh
./bin/ironctl audit --limit 5
```

## 4. Submit a change — watch it get *held*

`--dev` seeds a `default` agent group. Submit a persona change for it:

```sh
./bin/ironctl change submit --kind persona --group default --by alice
```

The CLI prints a **change id**. The change is **not applied** — it is parked at the gateway awaiting a
human decision. That's the whole point.

## 5. See what's pending, then approve it

```sh
./bin/ironctl change pending                         # lists the change id from step 4
./bin/ironctl change approve <change-id> --by alice  # paste the id
```

Only now is the change applied. (Try `./bin/ironctl change reject <id> --by alice` next time to see the
other outcome.)

## 6. Read the append-only audit log

```sh
./bin/ironctl audit --limit 20
```

You'll see the submit → approve → apply trail. The audit log is append-only — it's the record of every
decision the gateway made.

---

## What just happened

```
ironctl change submit ─▶ gateway (HELD) ─▶ ironctl change approve ─▶ applied ─▶ audit log
                          ▲ no bypass: persona, tools, packages, wiring,
                            permissions, mounts ALL flow through here
```

You exercised IronClaw's core invariant — the **mandatory human-approval gateway** — without needing the
full sandbox stack. In production the same flow runs behind gVisor-isolated, `network=none` per-session
sandboxes.

## Next steps

- **Run it for real:** install the prebuilt binaries and a systemd/launchd service — see the
  [Deployment](../README.md#deployment) section of the README and [`deploy/install.sh`](../deploy/install.sh).
  Production sandboxing needs **containerd + gVisor (`runsc`)**.
- **Wire a channel:** connect an agent group to Slack / Discord / Telegram via the registry
  (`ironctl registry ...`).
- **Understand the design:** [architecture](architecture.md) · [threat model](threat-model.md) ·
  [contract](contract.md).
- **Where it's headed:** the [roadmap](roadmap.md) — a guided `ironctl onboard`
  wizard and a web console are on the way.
