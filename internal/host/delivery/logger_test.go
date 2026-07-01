package delivery

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// chatMsg builds a sandbox outbound chat reply addressed at the given channel /
// platform coordinates (as the sandbox echoes back from session_routing).
func chatMsg(id string, seq int64, channel, platform, content string) contract.MessageOut {
	m := contract.MessageOut{ID: contract.MessageID(id), Seq: seq, Kind: contract.KindChat, Content: content}
	if channel != "" {
		c := channel
		m.ChannelType = &c
	}
	if platform != "" {
		p := platform
		m.PlatformID = &p
	}
	return m
}

func newGateway() *gateway.Gateway {
	return gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())
}

// TestPollLogsDueCountAndCoords verifies the delivery logger records the per-poll
// due-row count and the handled message's channel/platform coordinates (never its
// content) — the decisive diagnostic for a "reply written but never delivered"
// report.
func TestPollLogsDueCountAndCoords(t *testing.T) {
	reg := registry.NewMemRegistry()
	mg, _ := reg.GetOrCreateMessagingGroup("fake", "C1", "", true, contract.UnknownPublic)
	if _, err := reg.ResolveSession("g1", mg.ID, nil, contract.SessionShared); err != nil {
		t.Fatal(err)
	}

	store := queue.NewMemStore()
	hostOut := queue.NewMemOutbound(store)
	sandboxOut := queue.NewMemOutbound(store)

	channelReg := channels.NewRegistry()
	if err := channelReg.Register(channels.NewFakeAdapter("fake")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	d := New(channelReg, newGateway(), reg, func(id contract.SessionID) (contract.OutboundReader, error) {
		return hostOut, nil
	}).WithLogger(slog.New(slog.NewTextHandler(&buf, nil)))

	// Origin-chat reply: channel/platform match the session's messaging group.
	if err := sandboxOut.WriteMessageOut(chatMsg("m1", 1, "fake", "C1", "secret reply body")); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if d.DeliveredCount() != 1 {
		t.Fatalf("delivered = %d, want 1", d.DeliveredCount())
	}
	out := buf.String()
	if !strings.Contains(out, "due=1") {
		t.Errorf("expected a due=1 count log, got:\n%s", out)
	}
	if !strings.Contains(out, "channel=fake") || !strings.Contains(out, "platform=C1") {
		t.Errorf("expected handled coords log (channel=fake platform=C1), got:\n%s", out)
	}
	if strings.Contains(out, "secret reply body") {
		t.Errorf("delivery log leaked message content:\n%s", out)
	}
}

// TestPollSkipsBrokenSessionAndDeliversOthers verifies that a single session whose
// outbound reader cannot be opened is logged and skipped without aborting the
// whole poll — a broken/stuck session must not starve delivery for the rest.
func TestPollSkipsBrokenSessionAndDeliversOthers(t *testing.T) {
	reg := registry.NewMemRegistry()
	mgA, _ := reg.GetOrCreateMessagingGroup("fake", "C1", "", true, contract.UnknownPublic)
	mgB, _ := reg.GetOrCreateMessagingGroup("fake", "C2", "", true, contract.UnknownPublic)
	// Broken session first so, under the old first-error-aborts behavior, it would
	// have blocked the good session behind it.
	broken, _ := reg.ResolveSession("gbroken", mgA.ID, nil, contract.SessionShared)
	good, _ := reg.ResolveSession("ggood", mgB.ID, nil, contract.SessionShared)

	store := queue.NewMemStore()
	hostOut := queue.NewMemOutbound(store)
	sandboxOut := queue.NewMemOutbound(store)

	channelReg := channels.NewRegistry()
	if err := channelReg.Register(channels.NewFakeAdapter("fake")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	d := New(channelReg, newGateway(), reg, func(id contract.SessionID) (contract.OutboundReader, error) {
		if id == broken.ID {
			return nil, context.DeadlineExceeded
		}
		return hostOut, nil
	}).WithLogger(slog.New(slog.NewTextHandler(&buf, nil)))

	if err := sandboxOut.WriteMessageOut(chatMsg("m1", 1, "fake", "C2", "hi")); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("a broken session must not fail the poll: %v", err)
	}
	if d.DeliveredCount() != 1 {
		t.Fatalf("delivered = %d, want 1 (good session delivered despite broken sibling)", d.DeliveredCount())
	}
	if out := buf.String(); !strings.Contains(out, "open outbound reader failed") {
		t.Errorf("expected a warn log for the broken session, got:\n%s", out)
	}
	_ = good
}
