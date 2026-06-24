# Credential vault (request-time injection)

Long-running agents often need credentials for the third-party APIs they call. IronClaw
lets an agent reach a credential by **logical name** — `vault://<cred>/<path>` — while
**never holding the key**. The secret is attached host-side by a *separate* principal,
so neither the sandbox nor the egress broker ever becomes a secret sink (threat model
[§11](threat-model.md)).

## How a vaulted request flows

```
sandbox ──vault://github/repos/acme──▶ egress broker ──▶ vault injector ──▶ api.github.com
  (no key)                              (per-session     (holds the key,      (sees the
                                         socket, policy    attaches it)         real token)
                                         enforcement)
```

1. The agent addresses `vault://<cred>/<path>`. The sandbox holds no key.
2. The **egress broker** receives the request on a **per-session socket** whose identity
   the host created at launch — *not* the spoofable `X-Ironclaw-Session` header. It
   resolves that trusted session to its agent group and enforces the gateway-approved
   **vault policy** (`registry.VaultPolicyStore.Allows(group, cred, host)`),
   **deny-by-default**: an un-granted credential, an unknown session, or a request on
   the shared (non per-session) socket is refused `403`, audited with the credential
   NAME (never the key) and a correlation id.
3. On approval the broker rewrites the request to the **injector** (stripping any
   sandbox-supplied `Authorization`, adding only the credential name + correlation id)
   and forwards it. The injector endpoint is itself deny-by-default on the broker
   allowlist.
4. The **injector** — a distinct OS principal — maps `<cred>` to its upstream + secret,
   attaches the secret host-side, and reverse-proxies to the real upstream. It is the
   ONE place a real key is added. The response is redacted on the broker→sandbox hop so
   an injected credential can never echo back.

## Why per-session sockets

The egress broker is a single shared instance bound into every sandbox. Attributing a
request to a session by a request header would be **spoofable** — a compromised sandbox
could set another group's session id and borrow its credentials. Mirroring the MCP
broker, vault enforcement instead runs over a **per-session unix socket** the host
created, so the session→group mapping the policy is keyed on is host-trusted and cannot
be escalated by a header.

## The broker ⇄ injector contract

The injector is **swappable**: IronClaw ships a minimal reference
(`cmd/ironclaw-vault-injector`, package `internal/host/vaultinjector`), or an operator
may point the broker at an external injector (e.g. OneCLI) that honours the same
contract:

| Hop | Header / field | Meaning |
| --- | --- | --- |
| broker → injector | `X-Ironclaw-Vault-Cred` | the logical credential NAME (never a key) |
| broker → injector | `X-Ironclaw-Vault-Request-Id` | host-authored correlation id for audit |
| broker → injector | request path | the upstream path from `vault://<cred>/<path>` |
| broker → injector | `Authorization` | **stripped** — the broker forwards no credential |
| injector → upstream | configured header (default `Authorization: Bearer …`) | the host-held secret, attached host-side |

The injector refuses an unknown credential `403` and never writes the secret into its
response.

## Running it

The control-plane and the injector share **one config file** (`cred → {upstream,
secretEnv}`). Secrets live in the **environment**, never in the file:

```json
{
  "creds": {
    "github": { "upstream": "https://api.github.com", "secretEnv": "VAULT_GITHUB_TOKEN" }
  }
}
```

Run the injector as its own OS user, then start the control-plane pointing at it:

```sh
# 1) the injector (separate principal; holds the secrets)
VAULT_GITHUB_TOKEN=ghp_… ironclaw-vault-injector \
  --config vault-injector.json --addr 127.0.0.1:8200

# 2) the control-plane (enforces policy; holds no secret)
controlplane \
  --egress-socket /run/ironclaw/egress.sock \
  --vault-endpoint http://127.0.0.1:8200 \
  --vault-injector-config vault-injector.json
```

`--vault-endpoint` requires `--vault-injector-config`: without the cred→upstream-host
map the broker cannot enforce vault policy, so the daemon refuses to enable vault rather
than run it unenforced.

A group is granted a credential through the gateway's human-approval path (a vault-policy
change is a capability change like any other); see
[`ironctl vault`](channels.md) for the management commands. With no grant, vault stays
deny-by-default.
