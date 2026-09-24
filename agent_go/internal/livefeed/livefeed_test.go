package livefeed

import (
	"reflect"
	"testing"
)

func TestPublishCoalescesDuplicatesInOrder(t *testing.T) {
	b := NewBus()
	s := b.Subscribe()
	for i := 0; i < 20; i++ {
		b.Publish(Report, "Workflow/a")
	}
	b.Publish(HumanInputs, "Workflow/a")
	b.Publish(Report, "Workflow/b")

	select {
	case <-s.Wake:
	default:
		t.Fatal("subscriber was not woken")
	}
	got, resync := s.Drain()
	want := []Notice{{Report, "Workflow/a"}, {HumanInputs, "Workflow/a"}, {Report, "Workflow/b"}}
	if resync || !reflect.DeepEqual(got, want) {
		t.Fatalf("Drain = %v resync=%v, want %v", got, resync, want)
	}
	if again, _ := s.Drain(); len(again) != 0 {
		t.Fatalf("second Drain = %v, want empty", again)
	}
}

func TestOverflowTurnsIntoResync(t *testing.T) {
	b := NewBus()
	s := b.Subscribe()
	for i := 0; i <= maxPending; i++ {
		b.Publish(Report, "Workflow/"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	got, resync := s.Drain()
	if !resync || len(got) != 0 {
		t.Fatalf("Drain after overflow = %d notices resync=%v, want 0 and true", len(got), resync)
	}
}

func TestUnsubscribedStreamGetsNothing(t *testing.T) {
	b := NewBus()
	s := b.Subscribe()
	b.Unsubscribe(s)
	b.Publish(Sessions, "")
	if got, _ := s.Drain(); len(got) != 0 {
		t.Fatalf("unsubscribed Drain = %v, want empty", got)
	}
}
