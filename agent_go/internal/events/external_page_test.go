package events

import (
	"fmt"
	"testing"
)

func TestForwardEventPageBoundsInitialPageAndMaintainsFilteredCursor(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	store.InitializeSession("s", 0)
	store.events["s"] = []Event{
		{ID: "a", Type: "user_message"},
		{ID: "hidden", Type: "streaming_chunk"},
		{ID: "b", Type: "conversation_end"},
		{ID: "hidden2", Type: "streaming_chunk"},
		{ID: "c", Type: "user_message"},
	}
	first := store.GetForwardEventPage("s", -1, 2)
	if len(first.Events) != 2 || first.Events[0].ID != "a" || first.Events[1].ID != "b" || !first.HasMore || first.LastProcessedIndex != 3 {
		t.Fatalf("wrong first page: %+v", first)
	}
	next := store.GetForwardEventPage("s", first.LastProcessedIndex, 2)
	if len(next.Events) != 1 || next.Events[0].ID != "c" || next.HasMore || next.LastProcessedIndex != 4 {
		t.Fatalf("wrong continuation: %+v", next)
	}
	end := store.GetForwardEventPage("s", next.LastProcessedIndex, 2)
	if len(end.Events) != 0 || end.CursorReset || end.LastProcessedIndex != 4 {
		t.Fatalf("wrong terminal page: %+v", end)
	}
}

func TestForwardEventPageExplicitCursorReset(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	store.InitializeSession("s", 20)
	store.events["s"] = []Event{{ID: "a", Type: "user_message"}, {ID: "b", Type: "user_message"}}
	old := store.GetForwardEventPage("s", 4, 1)
	if !old.CursorReset || old.FirstAvailableIndex != 20 || old.LastProcessedIndex != 20 || len(old.Events) != 1 || !old.HasMore {
		t.Fatalf("old cursor page: %+v", old)
	}
	future := store.GetForwardEventPage("s", 99, 1)
	if !future.CursorReset || len(future.Events) != 0 || future.LastProcessedIndex != 21 {
		t.Fatalf("future cursor page: %+v", future)
	}
	missing := store.GetForwardEventPage("missing", 99, 1)
	if missing.Exists || !missing.CursorReset || len(missing.Events) != 0 || missing.LastProcessedIndex != -1 {
		t.Fatalf("missing session page: %+v", missing)
	}
}

func TestForwardEventPageCapsStructuralEvents(t *testing.T) {
	store := NewEventStore(500)
	defer store.Stop()
	store.InitializeSession("s", 0)
	for i := 0; i < 300; i++ {
		store.events["s"] = append(store.events["s"], Event{ID: fmt.Sprint(i), Type: "user_message"})
	}
	page := store.GetForwardEventPage("s", 0, 5000)
	if len(page.Events) != 200 || page.Events[0].ID != "1" || !page.HasMore || page.LastProcessedIndex != 200 {
		t.Fatalf("page not bounded: %+v", page)
	}
}
