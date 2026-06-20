# API reference

The control-plane exposes a small HTTP API consumed by `ironctl` and the web
console. It is the machine-readable contract for `ironclaw-controlplane`, kept in
lockstep with `internal/host/api` and the frozen wire types in
`internal/contract`.

!!! info "No public port"
    The API binds **only** to the private mesh (Tailscale) interface — it has no
    public port. Network reachability is the primary access control; the optional
    bearer token is defense-in-depth on top of it. See
    [Security & trust](../security.md).

The route family is `/v1`. The three surfaces are:

- **Gateway** (`/v1/changes`, `/v1/audit`) — submit capability changes, list what
  is pending, record human approve/reject decisions, and read the append-only
  audit log.
- **Registry** — administer agent groups, personas, tools, and wiring.
- **Health / version** — liveness and the running build version.

The full specification is below, rendered from the canonical
[`api/openapi.yaml`](https://github.com/IronSecCo/ironclaw/blob/main/api/openapi.yaml)
(OpenAPI 3.1). The spec is the source of truth; this page renders it.

<!-- The OpenAPI spec is copied into the build by docs/hooks.py (single source of
     truth: api/openapi.yaml) and rendered below. -->
!!swagger openapi.yaml!!
