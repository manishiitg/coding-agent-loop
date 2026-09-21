package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentevents "github.com/manishiitg/mcpagent/events"
)

// StableAgentEventID preserves an upstream event identity when one exists and
// otherwise derives one from the complete structured envelope. The content
// hash is intentionally a fallback: it makes replay idempotent for older
// producers without pretending that they already expose a first-class event
// ID.
func StableAgentEventID(namespace string, event *agentevents.AgentEvent) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "structured"
	}
	baseCorrelationID := ""
	if event != nil && event.Data != nil {
		if carrier, ok := event.Data.(interface {
			GetBaseEventData() *agentevents.BaseEventData
		}); ok {
			if base := carrier.GetBaseEventData(); base != nil {
				if eventID := strings.TrimSpace(base.EventID); eventID != "" {
					return namespace + ":" + eventID
				}
				baseCorrelationID = strings.TrimSpace(base.CorrelationID)
			}
		}
	}
	if event != nil {
		if turnID := strings.TrimSpace(event.TurnID); turnID != "" && isSingletonTurnBoundary(event.Type) {
			return namespace + ":turn:" + turnID + ":" + string(event.Type)
		}
		if baseCorrelationID != "" {
			return namespace + ":correlation:" + baseCorrelationID + ":" + string(event.Type)
		}
		if correlationID := strings.TrimSpace(event.CorrelationID); correlationID != "" {
			return namespace + ":correlation:" + correlationID + ":" + string(event.Type)
		}
		if spanID := strings.TrimSpace(event.SpanID); spanID != "" {
			return namespace + ":span:" + spanID + ":" + string(event.Type)
		}
	}
	if encoded, err := json.Marshal(event); err == nil && len(encoded) > 0 {
		digest := sha256.Sum256(encoded)
		return namespace + ":sha256:" + hex.EncodeToString(digest[:16])
	}
	return fmt.Sprintf("%s:fallback:%d", namespace, time.Now().UnixNano())
}

func isSingletonTurnBoundary(eventType agentevents.EventType) bool {
	switch eventType {
	case agentevents.EventTypeUnifiedCompletion,
		agentevents.UserMessage,
		agentevents.AgentEnd,
		agentevents.AgentError,
		agentevents.ConversationEnd,
		agentevents.ConversationError:
		return true
	default:
		return false
	}
}
