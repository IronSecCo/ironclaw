package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// statusReport is the read-only daemon snapshot `ironctl status` renders. Every
// field is gathered from an existing control-plane endpoint — status never
// mutates anything.
type statusReport struct {
	Addr             string         `json:"addr"`
	Healthy          bool           `json:"healthy"`
	HealthDetail     string         `json:"healthDetail,omitempty"`
	Ready            bool           `json:"ready"`
	ReadyReason      string         `json:"readyReason,omitempty"`
	Sessions         int            `json:"sessions"`
	SessionsByStatus map[string]int `json:"sessionsByStatus,omitempty"`
	PendingApprovals int            `json:"pendingApprovals"`
	LastActivity     string         `json:"lastActivity,omitempty"`
	LastActivityKind string         `json:"lastActivityKind,omitempty"`
}

// cmdStatus implements `ironctl status [--json]`.
func cmdStatus(addr string, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the status report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rep := gatherStatus(addr)
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(rep)
	}
	printStatus(os.Stdout, rep)
	if !rep.Healthy {
		return fmt.Errorf("control-plane not healthy at %s", addr)
	}
	return nil
}

// gatherStatus probes the daemon's read endpoints. It is resilient: a failed
// probe degrades that field rather than aborting the whole report, so `status`
// still tells the operator what it could reach.
func gatherStatus(addr string) statusReport {
	rep := statusReport{Addr: addr, SessionsByStatus: map[string]int{}}

	if resp, err := httpGet(addr + "/healthz"); err != nil {
		rep.HealthDetail = err.Error()
	} else {
		resp.Body.Close()
		rep.Healthy = resp.StatusCode == http.StatusOK
		if !rep.Healthy {
			rep.HealthDetail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
	}

	var rz struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := getJSON(addr+"/readyz", &rz); err == nil {
		rep.Ready = rz.Status == "ready"
		rep.ReadyReason = rz.Reason
	}

	// registry.Session is marshaled with its Go field names (no json tags).
	var sessions []struct {
		ContainerStatus string `json:"ContainerStatus"`
	}
	if err := getJSON(addr+"/v1/registry/sessions", &sessions); err == nil {
		rep.Sessions = len(sessions)
		for _, s := range sessions {
			st := s.ContainerStatus
			if st == "" {
				st = "unknown"
			}
			rep.SessionsByStatus[st]++
		}
	}

	var pending []json.RawMessage
	if err := getJSON(addr+"/v1/changes/pending", &pending); err == nil {
		rep.PendingApprovals = len(pending)
	}

	// The most recent audit entry is the best read-only signal for "last
	// activity" (the audit log records the gateway/delivery lifecycle).
	var audit []struct {
		Time time.Time `json:"time"`
		Kind string    `json:"kind"`
	}
	if err := getJSON(addr+"/v1/audit?limit=1", &audit); err == nil && len(audit) > 0 {
		rep.LastActivity = audit[0].Time.UTC().Format(time.RFC3339)
		rep.LastActivityKind = audit[0].Kind
	}

	return rep
}

func printStatus(w io.Writer, rep statusReport) {
	health := "DOWN"
	if rep.Healthy {
		health = "ok"
	}
	fmt.Fprintf(w, "control-plane @ %s\n", rep.Addr)
	fmt.Fprintf(w, "  health:           %s", health)
	if rep.HealthDetail != "" {
		fmt.Fprintf(w, " (%s)", rep.HealthDetail)
	}
	fmt.Fprintln(w)

	ready := "not ready"
	if rep.Ready {
		ready = "ready"
	}
	fmt.Fprintf(w, "  readiness:        %s", ready)
	if rep.ReadyReason != "" {
		fmt.Fprintf(w, " (%s)", rep.ReadyReason)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "  live sessions:    %d", rep.Sessions)
	if len(rep.SessionsByStatus) > 0 {
		keys := make([]string, 0, len(rep.SessionsByStatus))
		for k := range rep.SessionsByStatus {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%d", k, rep.SessionsByStatus[k]))
		}
		fmt.Fprintf(w, " (%s)", strings.Join(parts, ", "))
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "  pending approvals: %d\n", rep.PendingApprovals)

	if rep.LastActivity != "" {
		fmt.Fprintf(w, "  last activity:    %s", rep.LastActivity)
		if rep.LastActivityKind != "" {
			fmt.Fprintf(w, " (%s)", rep.LastActivityKind)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "  last activity:    none recorded")
	}
}

// --- usage -----------------------------------------------------------------

// modelUsage is the token/model-call usage derived from the model-proxy audit
// records, which are surfaced as Prometheus counters at /metrics.
type modelUsage struct {
	Calls     float64 `json:"calls"`
	Errors    float64 `json:"errors"`
	DurSum    float64 `json:"-"`
	DurCount  float64 `json:"-"`
	AvgMillis float64 `json:"avgMillis"`
}

// cmdUsage implements `ironctl usage [--json]` — a model-call usage report from
// the model-proxy audit counters exposed at /metrics.
func cmdUsage(addr string, args []string) error {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the usage report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := httpGet(addr + "/metrics")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("metrics not available at %s (control-plane started without metrics)", addr)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("GET /metrics: HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	u := parseModelUsage(string(body))

	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(u)
	}
	errPct := 0.0
	if u.Calls > 0 {
		errPct = u.Errors / u.Calls * 100
	}
	fmt.Println("model usage (model-proxy audit -> /metrics):")
	fmt.Printf("  model calls:   %.0f\n", u.Calls)
	fmt.Printf("  errors:        %.0f (%.1f%%)\n", u.Errors, errPct)
	fmt.Printf("  avg latency:   %.0f ms\n", u.AvgMillis)
	return nil
}

// parseModelUsage extracts the ironclaw model-call counters/histogram from the
// Prometheus text exposition. Unlabeled series (the totals and the histogram
// _sum/_count) are simple "name value" lines.
func parseModelUsage(metricsText string) modelUsage {
	var u modelUsage
	for _, line := range strings.Split(metricsText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		val, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			continue
		}
		switch name {
		case "ironclaw_model_calls_total":
			u.Calls = val
		case "ironclaw_model_call_errors_total":
			u.Errors = val
		case "ironclaw_model_call_duration_seconds_sum":
			u.DurSum = val
		case "ironclaw_model_call_duration_seconds_count":
			u.DurCount = val
		}
	}
	if u.DurCount > 0 {
		u.AvgMillis = u.DurSum / u.DurCount * 1000
	}
	return u
}

// getJSON issues an authenticated GET and decodes a JSON body into v.
func getJSON(url string, v any) error {
	resp, err := httpGet(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
