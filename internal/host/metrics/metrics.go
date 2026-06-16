package metrics

import "net/http"

// Metrics is the control plane's pre-wired metric set. Construct one with New
// and hand the typed fields to the subsystems that record events; expose it over
// HTTP with Handler (mounted at /metrics by the API layer / daemon wiring).
//
// All metric names are namespaced ironclaw_*. Counters are monotonic; the model
// call latency histogram captures egress timing.
type Metrics struct {
	reg *Registry

	// ModelCalls counts requests forwarded to the model host; ModelCallErrors
	// counts those that failed. ModelCallDuration captures call latency.
	ModelCalls        *Counter
	ModelCallErrors   *Counter
	ModelCallDuration *Histogram

	// gatewayApproved/gatewayRejected back GatewayDecision; both are series of
	// ironclaw_gateway_decisions_total distinguished by the decision label.
	gatewayApproved *Counter
	gatewayRejected *Counter

	// Deliveries counts outbound messages delivered to a channel.
	Deliveries *Counter

	// SandboxLaunches / SandboxKills count sandbox lifecycle transitions.
	SandboxLaunches *Counter
	SandboxKills    *Counter
}

// New builds the registry and registers every control-plane metric.
func New() *Metrics {
	r := NewRegistry()
	return &Metrics{
		reg:               r,
		ModelCalls:        r.NewCounter("ironclaw_model_calls_total", "Total model-host requests forwarded by the proxy."),
		ModelCallErrors:   r.NewCounter("ironclaw_model_call_errors_total", "Model-host requests that returned an error."),
		ModelCallDuration: r.NewHistogram("ironclaw_model_call_duration_seconds", "Model-host request latency in seconds.", DefaultLatencyBuckets),
		gatewayApproved:   r.NewCounter("ironclaw_gateway_decisions_total", "Gateway change decisions by outcome.", Label{"decision", "approved"}),
		gatewayRejected:   r.NewCounter("ironclaw_gateway_decisions_total", "Gateway change decisions by outcome.", Label{"decision", "rejected"}),
		Deliveries:        r.NewCounter("ironclaw_deliveries_total", "Outbound messages delivered to a channel."),
		SandboxLaunches:   r.NewCounter("ironclaw_sandbox_launches_total", "Sandboxes launched."),
		SandboxKills:      r.NewCounter("ironclaw_sandbox_kills_total", "Sandboxes killed/stopped."),
	}
}

// Registry returns the underlying registry (for registering extra metrics).
func (m *Metrics) Registry() *Registry { return m.reg }

// Handler returns the /metrics http.Handler.
func (m *Metrics) Handler() http.Handler { return m.reg.Handler() }

// GatewayDecision records one gateway decision. approved selects the series.
func (m *Metrics) GatewayDecision(approved bool) {
	if approved {
		m.gatewayApproved.Inc()
		return
	}
	m.gatewayRejected.Inc()
}

// ObserveModelCall records a model call: its latency in seconds and whether it
// errored. It is the convenience path that keeps the three model-call metrics in
// sync at each call site.
func (m *Metrics) ObserveModelCall(seconds float64, err bool) {
	m.ModelCalls.Inc()
	m.ModelCallDuration.Observe(seconds)
	if err {
		m.ModelCallErrors.Inc()
	}
}
