# Repo Map — IronClaw

- **Base SHA:** `02748dd` (`origin/main` after Wave 1 completed; Wave 4/5 planned)
- **Stack:** Go 1.23+, **CGO_ENABLED=1** (SQLCipher via `github.com/mutecomm/go-sqlcipher/v4`)
- **Build/test:** `make build vet test` (== `CGO_ENABLED=1 go {build,vet,test} ./...`); format `gofmt -l .`
- **CI:** `.github/workflows/ci.yml` — `CGO_ENABLED=1`, Go 1.23, ubuntu, `build`+`vet`+`test` on push/PR.
- **Tests:** ~30 files / ~200 tests across `internal/**` + `test/parity/**`. Dev mode: `--dev` (in-memory backends, no gVisor).

## Directory map

```
cmd/controlplane   Host daemon entrypoint (AGENT1)   [SHARED FILE: main.go]
cmd/sandbox        In-sandbox agent entrypoint (AGENT2)
cmd/ironctl        Admin CLI (gateway-only today)
internal/contract  FROZEN seam — both sides import (RFC-gated)   [lock:contract]
internal/host/...   control-plane: api gateway router delivery sweep keys
                    modelproxy isolation queue registry channels scheduling
internal/sandbox/...  loop provider queue tools
test/parity        black-box cross-tree specs (shared, additive)
docs               architecture, threat-model, contract (RFC log), building, protocol
deploy             host deploy notes + install.sh
```

## High-risk / shared files (need locks or single ownership)

- `internal/contract/**` — **frozen**; joint RFC + both CODEOWNERS (`lock:contract`, human-gated).
- `cmd/controlplane/main.go` — single shared entrypoint; one owning task (T-016).
- `go.mod` / `go.sum` — `lock:dependency`. `.github/**`, `Makefile` — `lock:ci`. `deploy/**` — `lock:release`.
- `AGENTS.md`, `.agents/task-registry.json` — single-owner coordination files.
- `internal/sandbox/loop` — tool-registration seam (soft-lock; T-082/083/084 coordinate here).

## RFC status

- **RFC-0001** encrypted-SQLite binding — **applied** (`OpenInboundRW` etc. live, `CGO_ENABLED=1`).
- **RFC-0002** cross-seam wire formats (`internal/contract/actions.go`) — **applied**.
- Cross-tree encrypted-queue parity specs — **landed in `33bb237`** (crossmount, sandbox-outbound, capability-change).
