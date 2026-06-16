<!-- OWNER: AGENT1 -->
# Control-plane HTTP API

The control-plane exposes a small HTTP API that drives the gateway — the single
choke point for every control-plane mutation. `ironctl` is a thin client of this
API. The server **must** bind only to the mesh (Tailscale) interface so there is
no public port (see [../deploy/README.md](../deploy/README.md)).

All request and response bodies are JSON. Times are RFC 3339.

## Authentication

The mesh (Tailscale) network boundary is the primary control. As defense-in-depth,
the API can also require a bearer token: start the control-plane with
`IRONCLAW_API_TOKEN` set, and every request (except `GET /healthz`) must carry:

```
Authorization: Bearer <token>
```

Requests without a valid token get `401 Unauthorized` (the comparison is
constant-time). With no token configured, the check is disabled and the API relies
on the tailnet boundary alone. `ironctl` sends the token from `--token` or
`$IRONCLAW_API_TOKEN`.

## `GET /healthz`

Unauthenticated liveness probe. Response — `200 OK`: `{ "status": "ok" }`.

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

## `GET /v1/changes/history`

List the applied + rejected change history (everything no longer pending). When
the gateway uses the durable `FileStore`, this reflects state across restarts;
with the in-memory store it returns an empty array.

Response — `200 OK`: a JSON array of history entries, each carrying the
`ChangeRequest`, its terminal `status` (`applied` | `rejected` | `approved`), and
the recorded `decision`.

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

## `GET /v1/audit`

Return recent gateway audit entries (append-only JSONL: submit / verdict /
decision / apply, with timestamps). The optional `?limit=N` query caps the count
(default 100). With no audit log attached it returns an empty array.

Response — `200 OK`: a JSON array of audit entries
(`{ "time", "stage", "changeId", "kind", "detail" }`).
