package events

import (
	"strings"
	"sync"
	"time"
)

// A message steered into a busy CLI is journalled when the CLI confirms it
// took the message, which can be a fraction of a second after the CLI started
// answering it. Rows of that answer must not land above the question, so while
// a deferred steer awaits its row, answer rows that follow the in-flight
// answer's completion are held and released right after the user row.

var deferredSteerHoldTimeout = 20 * time.Second

type deferredSteerHold struct {
	pending        int
	boundaryPassed bool
	releasing      bool
	held           []Event
	timer          *time.Timer
}

type steerOrdering struct {
	mu    sync.Mutex
	holds map[string]*deferredSteerHold
}

func (es *EventStore) steerHoldState() *steerOrdering {
	es.steerOrderingOnce.Do(func() { es.steerOrdering = &steerOrdering{holds: make(map[string]*deferredSteerHold)} })
	return es.steerOrdering
}

// BeginDeferredSteer marks a steered message whose user row will be written
// later (at the CLI's durable ack). Pair every call with EndDeferredSteer.
func (es *EventStore) BeginDeferredSteer(sessionID string) {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	state := es.steerHoldState()
	state.mu.Lock()
	defer state.mu.Unlock()
	hold := state.holds[sessionID]
	if hold == nil {
		hold = &deferredSteerHold{}
		state.holds[sessionID] = hold
	}
	hold.pending++
	if hold.timer != nil {
		hold.timer.Stop()
	}
	hold.timer = time.AfterFunc(deferredSteerHoldTimeout, func() { es.releaseSteerHold(sessionID, true) })
}

// EndDeferredSteer is called right after the deferred user row was added. The
// last pending steer releases the held answer rows after it, in order.
func (es *EventStore) EndDeferredSteer(sessionID string) {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	es.releaseSteerHold(sessionID, false)
}

func (es *EventStore) releaseSteerHold(sessionID string, timedOut bool) {
	state := es.steerHoldState()
	state.mu.Lock()
	hold := state.holds[sessionID]
	if hold == nil {
		state.mu.Unlock()
		return
	}
	if !timedOut {
		hold.pending--
		if hold.pending > 0 {
			state.mu.Unlock()
			return
		}
	}
	if hold.timer != nil {
		hold.timer.Stop()
	}
	hold.releasing = true
	state.mu.Unlock()
	for {
		state.mu.Lock()
		batch := hold.held
		hold.held = nil
		if len(batch) == 0 {
			if state.holds[sessionID] == hold {
				delete(state.holds, sessionID)
			}
			state.mu.Unlock()
			return
		}
		state.mu.Unlock()
		for _, event := range batch {
			_ = es.addEventUnheld(sessionID, event)
		}
	}
}

// holdForDeferredSteer reports whether the event was taken into a hold.
func (es *EventStore) holdForDeferredSteer(sessionID string, event Event) bool {
	if !isSteerOrderedAnswerRow(event) {
		return false
	}
	state := es.steerHoldState()
	state.mu.Lock()
	defer state.mu.Unlock()
	hold := state.holds[sessionID]
	if hold == nil {
		return false
	}
	if hold.releasing {
		// Keep order behind rows still being released.
		hold.held = append(hold.held, event)
		return true
	}
	if !hold.boundaryPassed {
		// Rows up to the in-flight answer's completion belong to that answer.
		if event.Type == "unified_completion" {
			hold.boundaryPassed = true
		}
		return false
	}
	hold.held = append(hold.held, event)
	return true
}

func isSteerOrderedAnswerRow(event Event) bool {
	kind := strings.ToLower(strings.TrimSpace(event.ExecutionKind))
	if kind != "" && kind != "main" && kind != "main_agent" && kind != "chat" {
		return false
	}
	switch event.Type {
	case "unified_completion", "tool_call_start", "tool_call_end", "tool_call_error":
		return true
	}
	return IsTranscriptMessage(event)
}
