package inspector

import (
	"sync"
	"testing"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func eventFor(sessionID string) llmtypes.InspectorEvent {
	return llmtypes.InspectorEvent{
		Phase:       llmtypes.InspectorPhaseEvent,
		Timestamp:   time.Now().UTC(),
		Seq:         1,
		Provider:    "anthropic",
		Model:       "claude-haiku-4-5",
		EventName:   "delta",
		Metadata:    map[string]interface{}{"x": 1},
		StepContext: llmtypes.StepContext{SessionID: sessionID, StepID: "s"},
	}
}

func TestStoreAppendAndEventsPerSession(t *testing.T) {
	s := NewStore()
	s.Append(eventFor("alpha"))
	s.Append(eventFor("alpha"))
	s.Append(eventFor("beta"))

	if got := len(s.Events("alpha", 0)); got != 2 {
		t.Fatalf("alpha events = %d, want 2", got)
	}
	if got := len(s.Events("beta", 0)); got != 1 {
		t.Fatalf("beta events = %d, want 1", got)
	}
	if got := len(s.Events("gamma", 0)); got != 0 {
		t.Fatalf("gamma events = %d, want 0 (no such session)", got)
	}
}

func TestStoreAssignsMonotonicGlobalSeq(t *testing.T) {
	s := NewStore()
	for i := 0; i < 5; i++ {
		s.Append(eventFor("sess"))
	}
	events := s.Events("sess", 0)
	for i, ev := range events {
		if ev.GlobalSeq != i+1 {
			t.Fatalf("events[%d].GlobalSeq = %d, want %d", i, ev.GlobalSeq, i+1)
		}
	}
	if latest := s.LatestSeq("sess"); latest != 5 {
		t.Fatalf("LatestSeq = %d, want 5", latest)
	}
}

func TestStoreSinceCursorFiltersEarlierEvents(t *testing.T) {
	s := NewStore()
	for i := 0; i < 5; i++ {
		s.Append(eventFor("sess"))
	}
	// Poll cursor at 2: want only events 3, 4, 5.
	got := s.Events("sess", 2)
	if len(got) != 3 {
		t.Fatalf("Events(since=2) returned %d events, want 3", len(got))
	}
	for i, ev := range got {
		if ev.GlobalSeq != i+3 {
			t.Fatalf("Events(since=2)[%d].GlobalSeq = %d, want %d", i, ev.GlobalSeq, i+3)
		}
	}
}

func TestStoreRingEvictsOldestWhenFull(t *testing.T) {
	s := NewStoreWithCapacity(3)
	for i := 0; i < 5; i++ {
		s.Append(eventFor("sess"))
	}
	events := s.Events("sess", 0)
	if len(events) != 3 {
		t.Fatalf("buffer size = %d, want 3 after eviction", len(events))
	}
	// Oldest two were evicted; remaining are GlobalSeq 3, 4, 5.
	for i, ev := range events {
		if ev.GlobalSeq != i+3 {
			t.Fatalf("events[%d].GlobalSeq = %d, want %d", i, ev.GlobalSeq, i+3)
		}
	}
}

func TestStoreDropsEventsWithEmptySessionID(t *testing.T) {
	s := NewStore()
	ev := eventFor("")
	s.Append(ev)
	if got := len(s.Sessions()); got != 0 {
		t.Fatalf("Sessions count = %d, want 0 (empty session dropped)", got)
	}
}

func TestStoreClear(t *testing.T) {
	s := NewStore()
	s.Append(eventFor("sess"))
	s.Append(eventFor("sess"))
	s.Clear("sess")
	if got := s.Events("sess", 0); got != nil {
		t.Fatalf("after Clear, Events = %v, want nil", got)
	}
}

func TestStoreSinkRoutesEventsToAppend(t *testing.T) {
	s := NewStore()
	sink := s.Sink()
	sink.Emit(eventFor("sess"))
	sink.Emit(eventFor("sess"))
	if got := len(s.Events("sess", 0)); got != 2 {
		t.Fatalf("sink-routed events = %d, want 2", got)
	}
}

func TestStoreConcurrentAppendsSafe(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.Append(eventFor("sess"))
			}
		}()
	}
	wg.Wait()
	if got := len(s.Events("sess", 0)); got != 400 {
		t.Fatalf("concurrent appends = %d, want 400", got)
	}
}

func TestStoreSessionsReturnsAllTracked(t *testing.T) {
	s := NewStore()
	s.Append(eventFor("alpha"))
	s.Append(eventFor("beta"))
	s.Append(eventFor("alpha"))
	sessions := s.Sessions()
	if len(sessions) != 2 {
		t.Fatalf("Sessions len = %d, want 2", len(sessions))
	}
	// Sorted, so deterministic.
	if sessions[0] != "alpha" || sessions[1] != "beta" {
		t.Fatalf("Sessions = %v, want [alpha beta]", sessions)
	}
}
