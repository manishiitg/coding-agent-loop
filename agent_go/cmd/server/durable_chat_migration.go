package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	pkgevents "github.com/manishiitg/mcpagent/events"
)

func decodeLegacyChatEvents(raw []byte, sessionID string) ([]internalevents.Event, error) {
	var document struct {
		UpdatedAt           string                 `json:"updated_at"`
		ConversationHistory []json.RawMessage      `json:"conversation_history"`
		UIEvents            []internalevents.Event `json:"ui_events"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode legacy chat: %w", err)
	}
	baseTime, _ := time.Parse(time.RFC3339Nano, document.UpdatedAt)
	if baseTime.IsZero() {
		baseTime = time.Now().UTC()
	}

	imported := make([]internalevents.Event, 0, len(document.ConversationHistory)+len(document.UIEvents))
	for index, message := range document.ConversationHistory {
		role, content := chatHistoryMessageRoleAndText(message)
		content = strings.TrimSpace(content)
		if content == "" || role == "system" || role == "tool" || isPersistedToolCallMarker(content) {
			continue
		}
		eventTime := baseTime.Add(time.Duration(index-len(document.ConversationHistory)) * time.Nanosecond)
		id := legacyChatEventID(sessionID, index, role, content)
		switch role {
		case "human", "user":
			imported = append(imported, internalevents.Event{
				ID: id, Type: "user_message", Timestamp: eventTime, SessionID: sessionID,
				Data: &pkgevents.AgentEvent{Type: pkgevents.EventType("user_message"), Timestamp: eventTime, SessionID: sessionID,
					Data: internalevents.NewGenericEventData("user_message", map[string]interface{}{"content": content, "role": "user", "migrated": true})},
			})
		case "ai", "assistant":
			imported = append(imported, internalevents.Event{
				ID: id, Type: "streaming_chunk", Timestamp: eventTime, SessionID: sessionID,
				Data: &pkgevents.AgentEvent{Type: pkgevents.EventType("streaming_chunk"), Timestamp: eventTime, SessionID: sessionID,
					Data: &pkgevents.StreamingChunkEvent{Source: "transcript", Content: content}},
			})
		}
	}
	// Preserve bounded semantic diagnostics (tools, approvals, child summaries)
	// from the old archive. Transcript carriers above remain the readable spine.
	imported = append(imported, document.UIEvents...)
	return imported, nil
}

func legacyChatEventID(sessionID string, index int, role, content string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", sessionID, index, role, content)))
	return "legacy-chat-" + hex.EncodeToString(digest[:12])
}
