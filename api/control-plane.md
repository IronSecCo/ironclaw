<!-- OWNER: AGENT1 -->
# Control-plane HTTP API

The control-plane exposes a small HTTP API that drives the gateway — the single
choke point for every control-plane mutation. `ironctl` is a thin client of this
API. The server **must** bind only to the mesh (Tailscale) interface so there is
no public port (see [../deploy/README.md](../deploy/README.md)).

All request and response bodies are JSON. Times are RFC 3339.

## `POST /v1/changes`

Submit a control-plane mutation as a `ChangeRequest`. The gateway runs its
deterministic verifier chain and (per the v1 floor) holds the change pending a
human decision. Because the submit blocks on approval, the server processes it in
the background and returns immediately.

Request body (`contract.ChangeRequest`; fields omitted are defaulted):

```json
{
  "Kind": "persona",
  "AgentGroupID": "g1",
  "RequestedBy": "slack:alice",
  "Before": null,
  "After": null
}
```

`Kind` is one of: `persona`, `enabled_tools`, `packages`, `wiring`,
`permissions`, `mounts`.

Response — `202 Accepted`:

```json
{ "id": "chg_..." }
```

The returned `id` is the assigned `ChangeID`; use it to record a decision.

## `GET /v1/changes/pending`

List the change requests currently awaiting a decision.

Response — `200 OK`: a JSON array of `ChangeRequest` objects (empty array if none).

## `POST /v1/changes/{id}/decision`

Record a human decision for a pending change. On `approve`, the gateway applies
the change and marks it applied; on `reject`, it records the rejection and does
not apply.

Request body:

```json
{ "outcome": "approve", "decidedBy": "slack:admin" }
```

`outcome` must be `approve` or `reject`.

Responses:

- `200 OK` — `{ "status": "recorded" }`
- `400 Bad Request` — missing id or invalid outcome
- `409 Conflict` — no change is awaiting that id (or a decision is already
  pending delivery)
