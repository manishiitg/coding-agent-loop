// Package inspector buffers structured debug events emitted by the
// LLM adapters (via llmtypes.InspectorSink) so the inspector panel
// in the frontend can render a live "what is the model doing right
// now" timeline for any session that has the panel open.
//
// Architecturally this is the JSON/API-path counterpart to
// internal/terminals/store.go (which handles the tmux pane preview):
// same per-session aggregation shape, same step-attribution rules,
// but the inspector keeps structured InspectorEvent objects rather
// than raw text chunks.
//
// The store is intentionally in-memory only — inspector events are
// debug-only and don't need to survive a process restart. A bounded
// ring buffer per session keeps memory use predictable when the
// panel is left open for a long-running session.
package inspector

import (
	"sort"
	"sync"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// DefaultMaxEventsPerSession bounds the per-session ring buffer.
// Inspector callers that need more history should poll more often
// rather than ask the store to grow unbounded.
const DefaultMaxEventsPerSession = 2000

// Store is the in-memory aggregator of InspectorEvents, grouped by
// session ID. Concurrency-safe; multiple adapter goroutines (per
// streaming chunk) and a single HTTP reader (per polled GET) can
// hammer it in parallel.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*sessionBuffer
	maxPer   int
}

// NewStore builds a fresh Store with the default capacity. Pass 0
// (or use WithMax) to override.
func NewStore() *Store {
	return &Store{
		sessions: make(map[string]*sessionBuffer),
		maxPer:   DefaultMaxEventsPerSession,
	}
}

// NewStoreWithCapacity builds a store with a custom per-session
// event cap.
func NewStoreWithCapacity(maxPer int) *Store {
	if maxPer <= 0 {
		maxPer = DefaultMaxEventsPerSession
	}
	return &Store{
		sessions: make(map[string]*sessionBuffer),
		maxPer:   maxPer,
	}
}

// Append records one inspector event under the session identified by
// its StepContext.SessionID. Events with an empty SessionID are
// silently dropped — they can't be routed to any panel.
//
// Within a session the storage is a bounded ring: when the buffer
// fills, the oldest event is evicted. Each event also gets a
// monotonic GlobalSeq assigned at insertion time so polling clients
// can fetch only events newer than a previously-seen cursor.
func (s *Store) Append(event llmtypes.InspectorEvent) {
	sessionID := event.StepContext.SessionID
	if sessionID == "" {
		// Inspector events should always carry a session through the
		// scoped sink. If one slipped through without, drop it
		// rather than throwing it onto a phantom "" bucket.
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	buf, ok := s.sessions[sessionID]
	if !ok {
		buf = newSessionBuffer(s.maxPer)
		s.sessions[sessionID] = buf
	}
	buf.append(event)
}

// Events returns a snapshot of all stored events for a session,
// ordered by GlobalSeq ascending. Pass since>0 to filter to events
// with GlobalSeq strictly greater than since (the polling cursor).
//
// The returned slice is a copy — safe to iterate without holding any
// lock.
func (s *Store) Events(sessionID string, since int) []StoredEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	buf, ok := s.sessions[sessionID]
	if !ok {
		return nil
	}
	return buf.snapshot(since)
}

// LatestSeq returns the highest GlobalSeq the store has assigned for
// a session, or 0 if no events. Callers that want to start polling
// from "now" can use this as the initial cursor.
func (s *Store) LatestSeq(sessionID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	buf, ok := s.sessions[sessionID]
	if !ok {
		return 0
	}
	return buf.latestSeq()
}

// Clear drops all stored events for a session. The session bucket
// itself is removed; the next Append for that session rebuilds it.
func (s *Store) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// Sessions returns the set of session IDs currently tracked. Useful
// for housekeeping endpoints.
func (s *Store) Sessions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.sessions))
	for k := range s.sessions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// StoredEvent wraps an InspectorEvent with the store-assigned
// GlobalSeq. The original Seq on the event is the per-call counter
// the adapter emits; GlobalSeq is the per-session insertion order
// used for polling cursors.
type StoredEvent struct {
	GlobalSeq int                      `json:"global_seq"`
	Event     llmtypes.InspectorEvent `json:"event"`
}

// sessionBuffer is the per-session ring storage. Not concurrency-safe
// on its own — the outer Store holds the lock.
type sessionBuffer struct {
	events   []StoredEvent
	capacity int
	nextSeq  int
}

func newSessionBuffer(capacity int) *sessionBuffer {
	return &sessionBuffer{
		events:   make([]StoredEvent, 0, capacity),
		capacity: capacity,
		nextSeq:  1,
	}
}

func (b *sessionBuffer) append(ev llmtypes.InspectorEvent) {
	seq := b.nextSeq
	b.nextSeq++
	stored := StoredEvent{GlobalSeq: seq, Event: ev}
	if len(b.events) < b.capacity {
		b.events = append(b.events, stored)
		return
	}
	// Ring: drop the oldest, append the newest. Cheap because the
	// capacity is fixed and modest.
	copy(b.events, b.events[1:])
	b.events[len(b.events)-1] = stored
}

func (b *sessionBuffer) snapshot(since int) []StoredEvent {
	out := make([]StoredEvent, 0, len(b.events))
	for _, ev := range b.events {
		if ev.GlobalSeq > since {
			out = append(out, ev)
		}
	}
	return out
}

func (b *sessionBuffer) latestSeq() int {
	if len(b.events) == 0 {
		return 0
	}
	return b.events[len(b.events)-1].GlobalSeq
}

// Sink returns an llmtypes.InspectorSink that forwards every Emit
// to s.Append. Used by the chat handler / orchestrator to wire the
// store up as the consumer for a session's inspector events:
//
//	sink := store.Sink()
//	scoped := llmtypes.NewScopedInspectorSink(sink, stepCtx)
//	opts = append(opts, llmtypes.WithInspectorSink(scoped))
//
// The returned value is a pointer-to-struct wrapper so future
// changes (rate-limiting, fanout) only touch this one place.
func (s *Store) Sink() llmtypes.InspectorSink {
	return &storeSink{store: s}
}

type storeSink struct{ store *Store }

func (s *storeSink) Emit(event llmtypes.InspectorEvent) {
	if s == nil || s.store == nil {
		return
	}
	s.store.Append(event)
}
