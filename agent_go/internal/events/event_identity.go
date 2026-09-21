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
	if event != nil && event.Data != nil {
		if carrier, ok := event.Data.(interface {
			GetBaseEventData() *agentevents.BaseEventData
		}); ok {
			if base := carrier.GetBaseEventData(); base != nil && strings.TrimSpace(base.EventID) != "" {
				return namespace + ":" + strings.TrimSpace(base.EventID)
			}
		}
	}
	if encoded, err := json.Marshal(event); err == nil && len(encoded) > 0 {
		digest := sha256.Sum256(encoded)
		return namespace + ":sha256:" + hex.EncodeToString(digest[:16])
	}
	return fmt.Sprintf("%s:fallback:%d", namespace, time.Now().UnixNano())
}
