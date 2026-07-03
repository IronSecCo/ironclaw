package queue

import (
	"fmt"
	"sync"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// TestHostInboundConcurrentWritersNoCollision is the deterministic concurrency
// regression for IRO-278/IRO-283. It reproduces the exact production contention
// that broke recurring schedule_task and 500'd /chat/send: several independent
// host inbound writer handles for the SAME session — the router (a fresh
// /chat/send), the sweep re-enqueuer (a recurring schedule firing), and the
// delivery re-enqueuer — all writing with Seq==0 at the same time.
//
// Each writer opens its own handle (as the real code paths do), so nothing but
// the authoritative in-INSERT allocator coordinates them. Pre-fix, the router
// minted from an in-memory counter while sweep/delivery did SELECT MAX(seq)+2,
// so two writers picked the same seq and one INSERT failed on UNIQUE(seq). Post
// fix every writer mints atomically inside the INSERT, so the run must produce N
// distinct EVEN seqs with zero errors under -race.
func TestHostInboundConcurrentWritersNoCollision(t *testing.T) {
	f := NewFactory(t.TempDir())
	const sid = "sess-concurrent"
	k := testKey(0x77)
	if err := f.Provision(sid, k); err != nil {
		t.Fatalf("provision: %v", err)
	}

	const writers = 24
	var wg sync.WaitGroup
	errs := make([]error, writers)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w, err := f.OpenHostInbound(sid, k)
			if err != nil {
				errs[i] = fmt.Errorf("open %d: %w", i, err)
				return
			}
			defer w.Close()
			<-start // release all writers together to maximize contention
			errs[i] = w.WriteMessageIn(contract.MessageIn{
				ID:      contract.MessageID(fmt.Sprintf("in-%d", i)),
				Seq:     0,
				Content: fmt.Sprintf("msg-%d", i),
			})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			// A UNIQUE(seq) collision surfaces here as the IRO-278 failure.
			t.Fatalf("writer %d failed (seq collision regressed?): %v", i, err)
		}
	}

	// Read every persisted seq and assert: exactly N rows, all distinct, all even.
	sin, err := f.OpenSandboxInbound(sid, k)
	if err != nil {
		t.Fatalf("OpenSandboxInbound: %v", err)
	}
	defer sin.Close()
	rows, err := sin.Query("SELECT seq FROM messages_in ORDER BY seq")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	seen := make(map[int64]struct{}, writers)
	var count int
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if s%2 != 0 {
			t.Fatalf("seq %d is odd; host must write even seq", s)
		}
		if _, dup := seen[s]; dup {
			t.Fatalf("duplicate seq %d — UNIQUE(seq) allocator is not authoritative", s)
		}
		seen[s] = struct{}{}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if count != writers {
		t.Fatalf("got %d rows, want %d (a lost write means a swallowed collision)", count, writers)
	}
}
