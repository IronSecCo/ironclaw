// OWNER: T-103

package api

import (
	"net/http"
	"sync"
	"time"
)

// This file adds the production-hardening layer on top of the base API server:
// optional TLS, a token-bucket rate limiter, request body/header size caps, a
// /readyz readiness probe, and an injected /metrics handler. Every feature is
// opt-in via a With* option and a no-op when unset, so the mesh-only default
// behaviour is unchanged.

// probeExempt reports whether a path is a liveness/readiness probe that must
// never be throttled or gated behind auth.
func probeExempt(path string) bool {
	return path == "/healthz" || path == "/readyz"
}

// rateLimiter is a single global token bucket guarding total request throughput.
// Behind the mesh boundary a global ceiling is a sufficient flood guard and is
// deterministic to reason about; it is intentionally not per-client.
type rateLimiter struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	perSec   float64
	lastFill time.Time
	now      func() time.Time // injectable for tests
}

// newRateLimiter builds a bucket that refills at rps tokens/second and holds at
// most burst tokens. It starts full.
func newRateLimiter(rps float64, burst int) *rateLimiter {
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{
		tokens:   float64(burst),
		max:      float64(burst),
		perSec:   rps,
		lastFill: time.Now(),
		now:      time.Now,
	}
}

// allow consumes a token if one is available, refilling based on elapsed time.
func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	if elapsed := now.Sub(rl.lastFill).Seconds(); elapsed > 0 {
		rl.tokens += elapsed * rl.perSec
		if rl.tokens > rl.max {
			rl.tokens = rl.max
		}
		rl.lastFill = now
	}
	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// rateLimit wraps h with the configured limiter. Probe endpoints bypass it so a
// flood never starves health checks. A no-op when no limiter is configured.
func (s *Server) rateLimit(h http.Handler) http.Handler {
	if s.limiter == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if probeExempt(r.URL.Path) || s.limiter.allow() {
			h.ServeHTTP(w, r)
			return
		}
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
	})
}

// limitBody wraps h so request bodies over maxBodyBytes are rejected (the reader
// returns an error that handlers surface as 400). A no-op when unset.
func (s *Server) limitBody(h http.Handler) http.Handler {
	if s.maxBodyBytes <= 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
		}
		h.ServeHTTP(w, r)
	})
}

// handleReadyz is an unauthenticated readiness probe. With a readiness check
// attached it returns 503 until the check passes; otherwise it always reports
// ready. Distinct from /healthz, which only reflects liveness.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.ready != nil {
		if err := s.ready(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not ready",
				"reason": err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleMetrics serves the injected metrics handler (e.g. from internal/host/
// metrics). When none is configured the endpoint 404s so scrapers fail loudly
// rather than silently receiving an empty body.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metrics == nil {
		http.Error(w, "metrics not configured", http.StatusNotFound)
		return
	}
	s.metrics.ServeHTTP(w, r)
}
