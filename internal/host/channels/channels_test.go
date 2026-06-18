package channels

import (
	"context"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	fake := NewFakeAdapter("fake")
	if err := r.Register(fake); err != nil {
		t.Fatal(err)
	}
	got, ok := r.Get("fake")
	if !ok {
		t.Fatal("Get returned not found")
	}
	if got.Name() != "fake" {
		t.Fatalf("name = %q", got.Name())
	}
}

func TestRegisterErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Fatal("expected error on nil adapter")
	}
	if err := r.Register(NewFakeAdapter("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(NewFakeAdapter("a")); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestList(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(NewFakeAdapter("a"))
	_ = r.Register(NewFakeAdapter("b"))
	if len(r.List()) != 2 {
		t.Fatalf("list = %v, want 2", r.List())
	}
}

func TestFakeDelivery(t *testing.T) {
	fake := NewFakeAdapter("fake")
	id, err := fake.Deliver(context.Background(), contract.MessageOut{ID: "m1", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty platform message id")
	}
	d := fake.Delivered()
	if len(d) != 1 || d[0].ID != "m1" {
		t.Fatalf("delivered = %+v", d)
	}
}
