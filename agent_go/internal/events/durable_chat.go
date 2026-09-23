package events

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	pkgevents "github.com/manishiitg/mcpagent/events"
)

const maxDurableChatEventBytes = 64 * 1024

// durableChatEventTypes is intentionally an allowlist. Adding a producer does
// not make it part of conversation history until its bounded, user-visible
// meaning is reviewed here.
var durableChatEventTypes = map[string]bool{
	"agent_end":                   true,
	"agent_error":                 true,
	"background_agent_completed":  true,
	"background_agent_started":    true,
	"background_agent_terminated": true,
	"batch_execution_canceled":    true,
	"blocking_human_feedback":     true,
	"context_cancelled":           true, //nolint:misspell // Wire contract.
	"conversation_end":            true,
	"conversation_error":          true,
	"conversation_resumed":        true,
	"human_feedback_resolved":     true,
	"live_input_confirmed":        true,
	"orchestrator_end":            true,
	"plan_approval":               true,
	"pre_validation_completed":    true,
	"product_interaction":         true,
	"request_human_feedback":      true,
	"tool_call_end":               true,
	"tool_call_error":             true,
	"tool_call_start":             true,
	"unified_completion":          true,
	"user_message":                true,
}

var durableChildSummaryTypes = map[string]bool{
	"background_agent_completed":  true,
	"background_agent_started":    true,
	"background_agent_terminated": true,
}

// IsDurableChatEvent mirrors the journal projector for transport filtering.
// AddEventChecked publishes the projected row only after SQLite accepts it.
func IsDurableChatEvent(event Event) bool {
	if IsTranscriptMessage(event) {
		return true
	}
	if !durableChatEventTypes[event.Type] {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(event.ExecutionKind))
	return kind == "" || kind == "main" || kind == "main_agent" || kind == "chat" || durableChildSummaryTypes[event.Type]
}

func projectDurableChatEvent(event Event) (Event, bool) {
	if !IsDurableChatEvent(event) {
		return Event{}, false
	}
	return compactDurableChatEvent(event), true
}

func compactDurableChatEvent(event Event) Event {
	encoded, err := json.Marshal(event)
	if err != nil || len(encoded) <= maxDurableChatEventBytes {
		return event
	}
	payload := eventPayloadMap(&event)
	fields := make(map[string]interface{}, 16)
	for _, key := range []string{"content", "final_result", "result", "error", "message", "question", "tool_name", "tool_call_id", "name", "status", "role", "source"} {
		value, exists := payload[key]
		if !exists {
			continue
		}
		if text, ok := value.(string); ok {
			fields[key] = truncateDurableText(text, 48*1024)
		} else if value == nil || isDurableScalar(value) {
			fields[key] = value
		}
	}
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
		boundedMetadata := make(map[string]interface{})
		for _, key := range []string{"kind", "message_id", "turn_id", "provider", "confirmation", "delivery_status"} {
			if value, exists := metadata[key]; exists && isDurableScalar(value) {
				boundedMetadata[key] = value
			}
		}
		if len(boundedMetadata) > 0 {
			fields["metadata"] = boundedMetadata
		}
	}
	fields["payload_truncated"] = true
	fields["original_size_bytes"] = len(encoded)
	fields["artifact_source"] = "conversation_json"
	fields["artifact_event_id"] = event.ID
	event.Data = &pkgevents.AgentEvent{
		Type:      pkgevents.EventType(event.Type),
		Timestamp: event.Timestamp,
		SessionID: event.SessionID,
		Data:      NewGenericEventData(event.Type, fields),
	}
	if compact, marshalErr := json.Marshal(event); marshalErr == nil && len(compact) > maxDurableChatEventBytes {
		for key, value := range fields {
			if text, ok := value.(string); ok && len(text) > 4*1024 {
				fields[key] = truncateDurableText(text, 4*1024)
			}
		}
	}
	if compact, marshalErr := json.Marshal(event); marshalErr == nil && len(compact) > maxDurableChatEventBytes {
		minimal := map[string]interface{}{
			"payload_truncated":   true,
			"original_size_bytes": len(encoded),
			"artifact_source":     "conversation_json",
			"artifact_event_id":   event.ID,
		}
		for _, key := range []string{"content", "final_result", "result", "error", "message", "question"} {
			if text, ok := fields[key].(string); ok && text != "" {
				minimal[key] = truncateDurableText(text, 16*1024)
				break
			}
		}
		for _, key := range []string{"tool_name", "tool_call_id", "name", "status", "role", "source"} {
			if value, ok := fields[key]; ok {
				minimal[key] = value
			}
		}
		event.Data.Data = NewGenericEventData(event.Type, minimal)
	}
	return event
}

func isDurableScalar(value interface{}) bool {
	switch value.(type) {
	case bool, float32, float64, int, int32, int64, json.Number:
		return true
	default:
		return false
	}
}

func truncateDurableText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "\n\n[Full payload retained in conversation JSON diagnostics]"
}

// ReadDurableChatPage reads the canonical SQLite conversation log directly.
// It never falls back to the in-memory transport window.
func (es *EventStore) ReadDurableChatPage(sessionID string, opts DurableEventPageOptions) (DurableEventPage, error) {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return DurableEventPage{Events: []Event{}}, nil
	}
	es.adoptJournaledSession(sessionID)
	es.mu.RLock()
	journal, ok := es.durableJournal.(DurableEventJournalPageReader)
	class := es.persistenceClasses[sessionID]
	es.mu.RUnlock()
	if !ok || journal == nil || class != SessionPersistenceInteractiveChat {
		return DurableEventPage{Events: []Event{}}, nil
	}
	return journal.ReadPage(sessionID, opts)
}

func (es *EventStore) DeleteDurableChatSession(sessionID string) error {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	es.mu.RLock()
	journal, ok := es.durableJournal.(DurableEventJournalPageReader)
	es.mu.RUnlock()
	if !ok || journal == nil {
		return nil
	}
	// Hold the session's append lock so an in-flight turn cannot write a row
	// after the delete, then drop the in-memory class and buffer: a still
	// running turn continues live-only instead of re-creating ownerless rows.
	sessionLock := es.sessionAppendLock(sessionID)
	sessionLock.Lock()
	defer sessionLock.Unlock()
	if err := journal.DeleteSession(sessionID); err != nil {
		return err
	}
	es.RemoveSession(sessionID)
	es.mu.Lock()
	es.journalProbed[sessionID] = true
	es.mu.Unlock()
	return nil
}

// ImportDurableChatEvents seeds the canonical journal without publishing old
// events to live subscribers. It is used only by the one-time JSON migration.
func (es *EventStore) ImportDurableChatEvents(sessionID string, imported []Event) error {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	es.mu.RLock()
	journal := es.durableJournal
	es.mu.RUnlock()
	if journal == nil {
		return nil
	}
	for _, event := range imported {
		projected, keep := projectDurableChatEvent(event)
		if !keep {
			continue
		}
		if _, _, err := journal.Append(sessionID, projected); err != nil {
			return err
		}
	}
	if migrator, ok := journal.(DurableEventJournalMigrator); ok {
		return migrator.MarkMigrationComplete(sessionID)
	}
	return nil
}

func (es *EventStore) DurableChatMigrationComplete(sessionID string) (bool, error) {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return false, nil
	}
	es.mu.RLock()
	journal := es.durableJournal
	es.mu.RUnlock()
	migrator, ok := journal.(DurableEventJournalMigrator)
	if !ok {
		return true, nil
	}
	return migrator.MigrationComplete(sessionID)
}

func (es *EventStore) MarkDurableChatMigrationComplete(sessionID string) error {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	es.mu.RLock()
	journal := es.durableJournal
	es.mu.RUnlock()
	if migrator, ok := journal.(DurableEventJournalMigrator); ok {
		return migrator.MarkMigrationComplete(sessionID)
	}
	return nil
}

func (es *EventStore) DurableChatOwner(sessionID string) (string, error) {
	if es == nil || strings.TrimSpace(sessionID) == "" {
		return "", nil
	}
	es.mu.RLock()
	journal := es.durableJournal
	es.mu.RUnlock()
	if ownership, ok := journal.(DurableEventJournalOwnership); ok {
		return ownership.Owner(sessionID)
	}
	return "", nil
}
