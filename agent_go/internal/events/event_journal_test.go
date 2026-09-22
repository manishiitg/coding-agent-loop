package events

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	agentevents "github.com/manishiitg/mcpagent/events"
)

type failingEventJournal struct{}

func (failingEventJournal) Append(string, Event) (Event, bool, error) {
	return Event{}, false, errors.New("disk unavailable")
}
func (failingEventJournal) LoadTail(string, int) ([]Event, error) { return []Event{}, nil }
func (failingEventJournal) Close() error                          { return nil }

func journalTestEvent(id, content string) Event {
	message := agentevents.NewUserMessageEvent(1, content, "user")
	return Event{
		ID: id, Type: string(agentevents.UserMessage), Timestamp: time.Now(),
		Data: agentevents.NewAgentEvent(message),
	}
}

func streamingTestEvent(id, content string) Event {
	chunk := &agentevents.StreamingChunkEvent{Content: content, Source: "llm"}
	return Event{
		ID: id, Type: string(agentevents.StreamingChunk), Timestamp: time.Now(),
		Data: agentevents.NewAgentEvent(chunk),
	}
}

func classifyInteractiveTestSession(t *testing.T, store *EventStore, sessionID string) {
	t.Helper()
	if err := store.SetSessionPersistenceClass(sessionID, SessionPersistenceInteractiveChat); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteEventJournalAssignsSequenceAndDeduplicates(t *testing.T) {
	journal, err := OpenSQLiteEventJournal(filepath.Join(t.TempDir(), "events.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	first, inserted, err := journal.Append("session-1", journalTestEvent("event-1", "one"))
	if err != nil || !inserted || first.Sequence != 1 {
		t.Fatalf("first append = seq %d inserted %v err %v", first.Sequence, inserted, err)
	}
	replayed, inserted, err := journal.Append("session-1", journalTestEvent("event-1", "changed"))
	if err != nil || inserted || replayed.Sequence != 1 {
		t.Fatalf("duplicate append = seq %d inserted %v err %v", replayed.Sequence, inserted, err)
	}
	second, inserted, err := journal.Append("session-1", journalTestEvent("event-2", "two"))
	if err != nil || !inserted || second.Sequence != 2 {
		t.Fatalf("second append = seq %d inserted %v err %v", second.Sequence, inserted, err)
	}
	loaded, err := journal.LoadTail("session-1", 10)
	if err != nil || len(loaded) != 2 || loaded[0].ID != "event-1" || loaded[1].ID != "event-2" {
		t.Fatalf("loaded events = %+v err=%v", loaded, err)
	}
}

func TestOnlyInteractiveChatSessionsUseDurableJournal(t *testing.T) {
	journal, err := OpenSQLiteEventJournal(filepath.Join(t.TempDir(), "events.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewEventStore(100)
	defer store.Stop()
	store.SetDurableJournal(journal)

	if err := store.SetSessionPersistenceClass("execution-1", SessionPersistenceExecution); err != nil {
		t.Fatal(err)
	}
	if err := store.AddEventChecked("execution-1", journalTestEvent("execution-event", "run")); err != nil {
		t.Fatal(err)
	}
	if err := store.AddEventChecked("unknown-1", journalTestEvent("unknown-event", "unknown")); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{"execution-1", "unknown-1"} {
		persisted, err := journal.LoadTail(sessionID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(persisted) != 0 {
			t.Fatalf("%s persisted %d live-only events", sessionID, len(persisted))
		}
		if got := store.GetAllEventsRaw(sessionID); len(got) != 1 {
			t.Fatalf("%s live events = %d, want 1", sessionID, len(got))
		}
	}
}

func TestSessionPersistenceClassificationIsImmutableAndPrecedesEvents(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	if err := store.SetSessionPersistenceClass("chat-1", SessionPersistenceInteractiveChat); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSessionPersistenceClass("chat-1", SessionPersistenceExecution); err == nil {
		t.Fatal("persistence class change unexpectedly succeeded")
	}
	store.AddEvent("unknown-1", journalTestEvent("event-1", "live"))
	if err := store.SetSessionPersistenceClass("unknown-1", SessionPersistenceInteractiveChat); err == nil {
		t.Fatal("late persistence classification unexpectedly succeeded")
	}
	store.AddEvent("execution-1", journalTestEvent("event-2", "live execution"))
	if err := store.SetSessionPersistenceClass("execution-1", SessionPersistenceExecution); err != nil {
		t.Fatalf("late live-only classification failed: %v", err)
	}
}

func TestStreamingChunksStayLiveOnlyAndBoundaryPreservesSequenceGap(t *testing.T) {
	journal, err := OpenSQLiteEventJournal(filepath.Join(t.TempDir(), "events.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewEventStore(100)
	defer store.Stop()
	store.SetDurableJournal(journal)
	classifyInteractiveTestSession(t, store, "session-1")
	for _, id := range []string{"chunk-1", "chunk-2", "chunk-3"} {
		if err := store.AddEventChecked("session-1", streamingTestEvent(id, id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.AddEventChecked("session-1", journalTestEvent("message-1", "boundary")); err != nil {
		t.Fatal(err)
	}
	if got := store.GetAllEventsRaw("session-1"); len(got) != 4 || got[3].Sequence != 4 {
		t.Fatalf("live sequence = %+v", got)
	}
	persisted, err := journal.LoadTail("session-1", 10)
	if err != nil || len(persisted) != 1 || persisted[0].ID != "message-1" || persisted[0].Sequence != 4 {
		t.Fatalf("durable boundaries = %+v err=%v", persisted, err)
	}
}

func TestSQLiteEventJournalReportsSizeAndCount(t *testing.T) {
	journal, err := OpenSQLiteEventJournal(filepath.Join(t.TempDir(), "events.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, _, err := journal.Append("session-1", journalTestEvent("event-1", "one")); err != nil {
		t.Fatal(err)
	}
	stats, err := journal.Stats()
	if err != nil || stats.Events != 1 || stats.SizeBytes <= 0 || stats.Path == "" {
		t.Fatalf("stats = %+v err=%v", stats, err)
	}
}

func TestEventStoreSuppliesStableIdentityForLegacyEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.sqlite")
	journal, err := OpenSQLiteEventJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	store := NewEventStore(100)
	defer store.Stop()
	store.SetDurableJournal(journal)
	classifyInteractiveTestSession(t, store, "session-1")
	event := journalTestEvent("", "legacy")
	if err := store.AddEventChecked("session-1", event); err != nil {
		t.Fatalf("legacy event without an ID was rejected: %v", err)
	}
	if err := store.AddEventChecked("session-1", event); err != nil {
		t.Fatalf("legacy replay failed: %v", err)
	}
	stored := store.GetAllEventsRaw("session-1")
	if len(stored) != 1 || stored[0].ID == "" {
		t.Fatalf("legacy replay was not identified exactly once: %+v", stored)
	}
}

func TestEventStoreRestoresDurableEventsAndContinuesSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.sqlite")
	journal, err := OpenSQLiteEventJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	firstStore := NewEventStore(100)
	firstStore.SetDurableJournal(journal)
	classifyInteractiveTestSession(t, firstStore, "session-1")
	firstStore.AddEvent("session-1", journalTestEvent("event-1", "one"))
	firstStore.AddEvent("session-1", journalTestEvent("event-2", "two"))
	firstStore.Stop()

	reopened, err := OpenSQLiteEventJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	secondStore := NewEventStore(100)
	defer secondStore.Stop()
	secondStore.SetDurableJournal(reopened)
	classifyInteractiveTestSession(t, secondStore, "session-1")
	restored := secondStore.GetEvents("session-1", GetEventsOptions{SinceIndex: -1, IncludeStreaming: true}).Events
	if len(restored) != 2 || restored[0].Sequence != 1 || restored[1].Sequence != 2 {
		t.Fatalf("restored = %+v", restored)
	}
	secondStore.AddEvent("session-1", journalTestEvent("event-3", "three"))
	all := secondStore.GetAllEventsRaw("session-1")
	if len(all) != 3 || all[2].Sequence != 3 {
		t.Fatalf("continued sequence = %+v", all)
	}
	secondStore.AddEvent("session-1", journalTestEvent("event-2", "duplicate"))
	if got := len(secondStore.GetAllEventsRaw("session-1")); got != 3 {
		t.Fatalf("duplicate durable event was published again: %d", got)
	}
}

func TestSQLiteEventJournalTailIsChronologicalAndBounded(t *testing.T) {
	journal, err := OpenSQLiteEventJournal(filepath.Join(t.TempDir(), "events.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for _, id := range []string{"one", "two", "three"} {
		if _, _, err := journal.Append("session-1", journalTestEvent(id, id)); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := journal.LoadTail("session-1", 2)
	if err != nil || len(loaded) != 2 || loaded[0].ID != "two" || loaded[1].ID != "three" {
		t.Fatalf("bounded tail = %+v err=%v", loaded, err)
	}
}

func TestEventStoreDoesNotPublishBeforeDurableAppend(t *testing.T) {
	store := NewEventStore(100)
	defer store.Stop()
	store.SetDurableJournal(failingEventJournal{})
	classifyInteractiveTestSession(t, store, "session-1")
	subscriber := store.Subscribe("session-1")
	defer store.Unsubscribe("session-1", subscriber)
	if err := store.AddEventChecked("session-1", journalTestEvent("event-1", "one")); err == nil {
		t.Fatal("durable append failure was hidden")
	}
	if got := len(store.GetAllEventsRaw("session-1")); got != 0 {
		t.Fatalf("non-durable event entered memory: %d", got)
	}
	select {
	case event := <-subscriber.Ch:
		t.Fatalf("non-durable event was published over SSE: %+v", event)
	default:
	}
}

func TestSessionStatusDoesNotHydrateAndRemovalDoesNotResurrect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.sqlite")
	journal, err := OpenSQLiteEventJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	first := NewEventStore(100)
	first.SetDurableJournal(journal)
	classifyInteractiveTestSession(t, first, "session-1")
	first.AddEvent("session-1", journalTestEvent("event-1", "one"))
	first.Stop()

	reopened, err := OpenSQLiteEventJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	store := NewEventStore(100)
	defer store.Stop()
	store.SetDurableJournal(reopened)
	classifyInteractiveTestSession(t, store, "session-1")
	if count, exists := store.GetSessionStatus("session-1"); exists || count != 0 {
		t.Fatalf("status unexpectedly hydrated durable history: count=%d exists=%v", count, exists)
	}
	if got := store.GetEvents("session-1", GetEventsOptions{SinceIndex: -1}).Events; len(got) != 1 {
		t.Fatalf("explicit event restore = %+v", got)
	}
	store.RemoveSession("session-1")
	result := store.GetEvents("session-1", GetEventsOptions{SinceIndex: -1})
	if result.Exists || len(result.Events) != 0 {
		t.Fatalf("removed session resurrected: %+v", result)
	}
	store.AddEvent("session-1", journalTestEvent("event-2", "two"))
	if got := store.GetAllEventsRaw("session-1"); len(got) != 1 || got[0].ID != "event-2" {
		t.Fatalf("new activity resurrected old tail: %+v", got)
	}
}
