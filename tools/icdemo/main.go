// Command icdemo is a THROWAWAY local demo harness (not part of the product):
// it seeds an encrypted inbound queue with one task and reads back the sandbox's
// outbound replies, so cmd/sandbox can be run directly against real encrypted
// queues without the full control-plane/isolator. Safe to delete.
package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	hq "github.com/nivardsec/ironclaw/internal/host/queue"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: icdemo seed <dir> <task> | icdemo read <dir>")
		os.Exit(2)
	}
	cmd, dir := os.Args[1], os.Args[2]
	switch cmd {
	case "seed":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "seed needs a task string")
			os.Exit(2)
		}
		seed(dir, os.Args[3])
	case "read":
		read(dir)
	default:
		fmt.Fprintln(os.Stderr, "unknown cmd:", cmd)
		os.Exit(2)
	}
}

func seed(dir, task string) {
	must(os.MkdirAll(dir, 0o700))
	var key contract.SessionKey
	keyPath := dir + "/session.key"
	// Reuse the session key if it already exists so re-seeding an existing session
	// (to re-test a running sandbox) keeps the same encryption key — regenerating it
	// would orphan the existing encrypted queues ("file is not a database").
	if raw, err := os.ReadFile(keyPath); err == nil && len(raw) >= len(key) {
		copy(key[:], raw[:len(key)])
	} else {
		if _, err := rand.Read(key[:]); err != nil {
			panic(err)
		}
		must(os.WriteFile(keyPath, key[:], 0o600))
	}

	in, err := hq.OpenInbound(dir+"/inbound.db", key)
	must(err)
	defer in.Close()
	// Unique id + even seq per call so re-seeding the same session (to re-test a
	// running sandbox) never collides on the messages_in primary key / seq parity.
	n := time.Now().UnixNano()
	msg := contract.MessageIn{
		ID:        contract.MessageID(fmt.Sprintf("msg-%d", n)),
		Seq:       n &^ 1, // even: host parity
		Kind:      contract.KindChat,
		Timestamp: time.Now().UTC(),
		Status:    contract.StatusQueued, // a status the sandbox treats as pending
		Trigger:   1,                     // engage immediately (don't just accumulate)
		Content:   task,
	}
	must(in.WriteMessageIn(msg))
	fmt.Printf("seeded %s/inbound.db (id=%s status=queued trigger=1)\n", dir, msg.ID)
}

func read(dir string) {
	raw, err := os.ReadFile(dir + "/session.key")
	must(err)
	var key contract.SessionKey
	copy(key[:], raw)
	out, err := hq.OpenOutbound(dir+"/outbound.db", key)
	must(err)
	defer out.Close()
	msgs, err := out.DueMessages()
	must(err)
	if len(msgs) == 0 {
		fmt.Println("(no outbound messages yet)")
		return
	}
	for _, m := range msgs {
		fmt.Printf("[%s] %s\n", m.Kind, m.Content)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
