package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// fixedVerifier returns a preset verdict.
type fixedVerifier struct {
	name    string
	verdict contract.Verdict
	reason  string
	err     error
	called  *int
}

func (v fixedVerifier) Name() string { return v.name }
func (v fixedVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if v.called != nil {
		*v.called++
	}
	return v.verdict, v.reason, v.err
}

func TestVerifierChainRun(t *testing.T) {
	tests := []struct {
		name    string
		chain   VerifierChain
		want    contract.Verdict
		wantErr bool
	}{
		{
			name:  "empty chain passes",
			chain: VerifierChain{},
			want:  contract.VerdictPass,
		},
		{
			name:  "all pass",
			chain: VerifierChain{fixedVerifier{name: "a", verdict: contract.VerdictPass}, fixedVerifier{name: "b", verdict: contract.VerdictPass}},
			want:  contract.VerdictPass,
		},
		{
			name:  "one require-human elevates",
			chain: VerifierChain{fixedVerifier{name: "a", verdict: contract.VerdictPass}, fixedVerifier{name: "b", verdict: contract.VerdictRequireHuman}},
			want:  contract.VerdictRequireHuman,
		},
		{
			name:  "reject short-circuits over require-human",
			chain: VerifierChain{fixedVerifier{name: "a", verdict: contract.VerdictReject}, fixedVerifier{name: "b", verdict: contract.VerdictRequireHuman}},
			want:  contract.VerdictReject,
		},
		{
			name:    "verifier error rejects",
			chain:   VerifierChain{fixedVerifier{name: "a", verdict: contract.VerdictPass, err: errors.New("boom")}},
			want:    contract.VerdictReject,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := tt.chain.Run(context.Background(), contract.ChangeRequest{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("verdict = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerifierChainRejectShortCircuits(t *testing.T) {
	var laterCalled int
	chain := VerifierChain{
		fixedVerifier{name: "reject", verdict: contract.VerdictReject},
		fixedVerifier{name: "later", verdict: contract.VerdictPass, called: &laterCalled},
	}
	if _, _, err := chain.Run(context.Background(), contract.ChangeRequest{}); err != nil {
		t.Fatal(err)
	}
	if laterCalled != 0 {
		t.Fatalf("later verifier ran %d times, want 0 (short-circuit)", laterCalled)
	}
}

// countingApprover records calls and returns a preset decision.
type countingApprover struct {
	mu       sync.Mutex
	calls    int
	decision contract.Decision
}

func (a *countingApprover) RequestDecision(ctx context.Context, req contract.ChangeRequest, reason string) (contract.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return a.decision, nil
}

func TestSubmitRejectSkipsApprover(t *testing.T) {
	approver := &countingApprover{}
	applier := NewLogApplier()
	store := NewMemoryStore()
	chain := VerifierChain{fixedVerifier{name: "deny", verdict: contract.VerdictReject, reason: "nope"}}
	gw := New(chain, approver, applier, store)

	id, err := gw.Submit(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona})
	if err != nil {
		t.Fatal(err)
	}
	if approver.calls != 0 {
		t.Fatalf("approver called %d times on reject, want 0", approver.calls)
	}
	if len(applier.Applied()) != 0 {
		t.Fatalf("applier ran on reject, want 0")
	}
	if st, _ := store.Status(id); st != string(statusRejected) {
		t.Fatalf("status = %q, want rejected", st)
	}
}

func TestSubmitRequireHumanApprove(t *testing.T) {
	approver := NewManualApprover()
	applier := NewLogApplier()
	store := NewMemoryStore()
	gw := New(VerifierChain{AlwaysRequireHuman{}}, approver, applier, store)

	idCh := make(chan contract.ChangeID, 1)
	errCh := make(chan error, 1)
	go func() {
		id, err := gw.Submit(context.Background(), contract.ChangeRequest{ID: "c1", Kind: contract.ChangeWiring})
		idCh <- id
		errCh <- err
	}()

	// Wait for it to land in pending, then approve.
	waitPending(t, store, "c1")
	if err := gw.Decide("c1", contract.Decision{Outcome: OutcomeApprove, DecidedBy: "admin", DecidedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	<-idCh
	if st, _ := store.Status("c1"); st != string(statusApplied) {
		t.Fatalf("status = %q, want applied", st)
	}
	if len(applier.Applied()) != 1 {
		t.Fatalf("applied %d, want 1", len(applier.Applied()))
	}
}

func TestSubmitRequireHumanReject(t *testing.T) {
	approver := NewManualApprover()
	applier := NewLogApplier()
	store := NewMemoryStore()
	gw := New(VerifierChain{AlwaysRequireHuman{}}, approver, applier, store)

	done := make(chan error, 1)
	go func() {
		_, err := gw.Submit(context.Background(), contract.ChangeRequest{ID: "c2"})
		done <- err
	}()
	waitPending(t, store, "c2")
	if err := gw.Decide("c2", contract.Decision{Outcome: OutcomeReject, DecidedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(applier.Applied()) != 0 {
		t.Fatalf("applier ran on human reject, want 0")
	}
	if st, _ := store.Status("c2"); st != string(statusRejected) {
		t.Fatalf("status = %q, want rejected", st)
	}
}

func TestSubmitContextCancel(t *testing.T) {
	gw := New(VerifierChain{AlwaysRequireHuman{}}, NewManualApprover(), NewLogApplier(), NewMemoryStore())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := gw.Submit(ctx, contract.ChangeRequest{ID: "c3"})
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestMemoryStoreConcurrent(t *testing.T) {
	store := NewMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := contract.ChangeID("c" + string(rune('A'+n%26)) + string(rune('0'+n/26)))
			_ = store.Put(contract.ChangeRequest{ID: id})
			_, _ = store.Pending()
			_ = store.SetDecision(id, contract.Decision{Outcome: OutcomeApprove})
			_ = store.MarkApplied(id)
		}(i)
	}
	wg.Wait()
}

func waitPending(t *testing.T, store *MemoryStore, id contract.ChangeID) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := store.Status(id); ok && st == string(statusPending) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("change %q never became pending", id)
}
