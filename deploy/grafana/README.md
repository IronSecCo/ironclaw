# Grafana dashboards

`ironclaw-overview.json` — control-plane overview built on the host `/metrics`
endpoint (model-call volume, error rate, latency percentiles, sandbox kills).

## Import

1. Point Prometheus at the control-plane `/metrics` endpoint — see
   [docs/observability.md](../../docs/observability.md) for the scrape config.
2. In Grafana: **Dashboards → New → Import**, upload this JSON, and select your
   Prometheus data source when prompted.

The dashboard uses a `$job` template variable (defaults to `ironclaw`); set it to
match the `job_name` in your scrape config.

> Some panels (sandbox launches, gateway decisions, deliveries) chart series that
> are registered but not yet emitted by the control plane — they read `0` until
> that instrumentation lands. See `docs/observability.md` for the current status.
