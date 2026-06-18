package e2e

import (
	"io"
	"log"
	"testing"
	"time"
)

// discardLogger returns a logger that drops everything (keeps test output clean).
func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// eventually polls cond until it returns true or timeout elapses, failing the
// test otherwise. Used to await the asynchronously-running sandbox loop.
func eventually(t *testing.T, timeout, interval time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(interval)
	}
}
