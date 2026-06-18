package channels

import (
	"context"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

var _ Adapter = (*WebchatAdapter)(nil)

func TestWebchatBuffersAndDrains(t *testing.T) {
	a := NewWebchatAdapter("webchat")
	if a.Name() != "webchat" {
		t.Fatalf("name = %q", a.Name())
	}
	for _, m := range []contract.MessageOut{
		{ID: "m1", PlatformID: strptr("conv1"), Content: "hi"},
		{ID: "m2", PlatformID: strptr("conv1"), Content: "there"},
	} {
		if _, err := a.Deliver(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	got := a.Drain("conv1")
	if len(got) != 2 || got[0].Content != "hi" || got[1].Content != "there" {
		t.Fatalf("drained = %+v, want hi/there", got)
	}
	// Drain is clear-on-read: a second drain is empty (but never nil).
	if again := a.Drain("conv1"); again == nil || len(again) != 0 {
		t.Errorf("second drain = %+v, want empty non-nil", again)
	}
}

func TestWebchatNoConversationDropped(t *testing.T) {
	a := NewWebchatAdapter("")
	if a.Name() != "webchat" {
		t.Errorf("default name = %q, want webchat", a.Name())
	}
	id, err := a.Deliver(context.Background(), contract.MessageOut{ID: "m1", Content: "x"}) // no PlatformID
	if err != nil {
		t.Fatal(err)
	}
	if id != "m1" {
		t.Errorf("id = %q, want m1", id)
	}
	if len(a.Drain("")) != 0 {
		t.Error("a message with no conversation must not be buffered")
	}
}

func TestWebchatCapBounds(t *testing.T) {
	a := NewWebchatAdapter("webchat")
	a.cap = 3
	for _, id := range []contract.MessageID{"m1", "m2", "m3", "m4", "m5"} {
		if _, err := a.Deliver(context.Background(), contract.MessageOut{ID: id, PlatformID: strptr("c"), Content: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	got := a.Drain("c")
	if len(got) != 3 || got[0].ID != "m3" || got[2].ID != "m5" {
		t.Fatalf("cap not enforced: got %d msgs %+v", len(got), got)
	}
}
