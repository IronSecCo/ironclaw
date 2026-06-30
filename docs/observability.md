# Observability — Prometheus `/metrics`

IronClaw's control plane ships a built-in Prometheus endpoint at **`GET /metrics`**.
It is always wired (no flag needed) and is served on the same address as the API
(`--api-addr`, default `127.0.0.1:8787`). The exposition is hand-rolled in
`internal/host/metrics` — no Prometheus client library is pulled into the host, keeping
the dependency surface minimal — and emits the standard text format
(`text/plain; version=0.0.4`).

The same endpoint backs the CLI: `ironctl metrics` and `ironctl status` scrape `/metrics`
to print a model-call usage summary, so you can spot-check it without a Prometheus server.

## Reaching the endpoint

`/metrics` is **bearer-gated** whenever an API token is set (`IRONCLAW_API_TOKEN`). Scrape
it with the admin token, and keep it on the private network — do not expose it at the
public edge (the [deployment guide](deployment.md#reverse-proxy-tls-termination) shows how
to hide it behind the reverse proxy).

```bash
# With an API token (recommended): pass it as a bearer credential.
curl -s -H "Authorization: Bearer $IRONCLAW_API_TOKEN" http://127.0.0.1:8787/metrics

# Token-less dev runs (no IRONCLAW_API_TOKEN): the endpoint is open on the mesh boundary.
curl -s http://127.0.0.1:8787/metrics
```

A quick human-readable summary of the model-call series, without Prometheus:

```bash
ironctl metrics            # model calls, error %, avg latency
ironctl metrics --json     # same, machine-readable
ironctl status             # broader control-plane status, includes model usage
```

If the endpoint returns `404 metrics not configured`, the control plane was started with
metrics disabled — the standard `controlplane` binary always enables them.

## Exposed series

All series are namespaced `ironclaw_*`. Counters are monotonic; the latency histogram uses
buckets `0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10` seconds (plus `+Inf`).

### Live today

These are recorded by the running control plane:

| Series | Type | Meaning |
| --- | --- | --- |
| `ironclaw_model_calls_total` | counter | Model-host requests forwarded by the egress proxy. |
| `ironclaw_model_call_errors_total` | counter | Forwarded requests that errored (HTTP ≥ 400 or denied by the egress policy). |
| `ironclaw_model_call_duration_seconds` | histogram | Model-host request latency. Emits `_bucket{le=...}`, `_sum`, `_count`. |
| `ironclaw_sandbox_launches_total` | counter | Sandboxes launched (incremented per sandbox that actually starts). |
| `ironclaw_sandbox_kills_total` | counter | Sandboxes killed/stopped by the stuck-session sweeper. |
| `ironclaw_gateway_decisions_total{decision="approved"\|"rejected"}` | counter | Gateway change decisions by outcome — verifier rejects, human approve/reject, and auto-approve. |
| `ironclaw_deliveries_total` | counter | Outbound messages successfully delivered to a channel adapter. |

The model-call series come from the **model-proxy audit** — every egress request the proxy
forwards (the sandbox's only network path) is counted and timed at the host, so the numbers
reflect real, host-observed traffic rather than anything the sandbox self-reports. The
gateway-decision, delivery, and sandbox-launch counters are likewise recorded host-side, at
the gateway decision path, the outbound delivery loop, and the session launcher respectively.

## Prometheus scrape config

Drop this into `prometheus.yml`. Replace the target with your control-plane API address and
supply the admin token (omit the `authorization` block only for token-less dev runs):

```yaml
scrape_configs:
  - job_name: ironclaw
    metrics_path: /metrics
    scheme: http            # use https if a TLS-terminating proxy fronts the API
    authorization:
      credentials: <IRONCLAW_API_TOKEN>
    static_configs:
      - targets: ["127.0.0.1:8787"]   # control-plane --api-addr
```

For Kubernetes, a `PodMonitor`/`ServiceMonitor` works the same way — point it at the API
port and reference a secret holding `IRONCLAW_API_TOKEN`.

## Grafana dashboard

A ready-to-import dashboard lives at
[`deploy/grafana/ironclaw-overview.json`](https://github.com/IronSecCo/ironclaw/blob/main/deploy/grafana/ironclaw-overview.json).
It charts model-call volume, error rate, and latency percentiles (p50/p90/p99) derived from
the histogram, plus sandbox kills.

1. In Grafana, **Dashboards → New → Import**.
2. Upload the JSON (or paste its contents).
3. Pick your Prometheus data source when prompted.

The dashboard's PromQL building blocks, if you want to roll your own panels:

```promql
# Model-call rate (req/s)
rate(ironclaw_model_calls_total[5m])

# Error ratio (%)
100 * rate(ironclaw_model_call_errors_total[5m]) / rate(ironclaw_model_calls_total[5m])

# Latency percentiles
histogram_quantile(0.50, sum(rate(ironclaw_model_call_duration_seconds_bucket[5m])) by (le))
histogram_quantile(0.90, sum(rate(ironclaw_model_call_duration_seconds_bucket[5m])) by (le))
histogram_quantile(0.99, sum(rate(ironclaw_model_call_duration_seconds_bucket[5m])) by (le))

# Average latency (s)
rate(ironclaw_model_call_duration_seconds_sum[5m]) / rate(ironclaw_model_call_duration_seconds_count[5m])

# Sandbox launches / kills (per minute)
60 * rate(ironclaw_sandbox_launches_total[5m])
60 * rate(ironclaw_sandbox_kills_total[5m])

# Gateway decisions (per minute), split by outcome
60 * rate(ironclaw_gateway_decisions_total[5m])

# Outbound deliveries (per minute)
60 * rate(ironclaw_deliveries_total[5m])
```

## Security notes

- **Bearer-gated.** `/metrics` requires the admin token whenever one is set; it is not a
  public endpoint. Keep it on the private network and off the public edge.
- **No secrets in metrics.** Series carry only counts and timings — never tokens, keys, or
  message content. (Logs are likewise secret-redacted; see the deployment guide.)
- **Host-observed, not sandbox-reported.** The model-call series come from the egress proxy
  on the host side of the trust boundary, so a compromised sandbox cannot forge them.

## See also

- [Production deployment → Observability](deployment.md#observability) — where `/metrics`
  sits in a hardened deployment, plus liveness/readiness probes, logs, and the audit log.
- `ironctl metrics` / `ironctl status` — CLI consumers of this same endpoint.
