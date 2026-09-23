package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
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

	history := make([]internalevents.Event, 0, len(document.ConversationHistory))
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
			history = append(history, internalevents.Event{
				ID: id, Type: "user_message", Timestamp: eventTime, SessionID: sessionID,
				Data: &pkgevents.AgentEvent{Type: pkgevents.EventType("user_message"), Timestamp: eventTime, SessionID: sessionID,
					Data: internalevents.NewGenericEventData("user_message", map[string]interface{}{"content": content, "role": "user", "migrated": true})},
			})
		case "ai", "assistant":
			history = append(history, internalevents.Event{
				ID: id, Type: "streaming_chunk", Timestamp: eventTime, SessionID: sessionID,
				Data: &pkgevents.AgentEvent{Type: pkgevents.EventType("streaming_chunk"), Timestamp: eventTime, SessionID: sessionID,
					Data: &pkgevents.StreamingChunkEvent{Source: "transcript", Content: content}},
			})
		}
	}
	return mergeLegacyChatTrace(history, document.UIEvents, sessionID), nil
}

// The provider history has complete text but no timestamps; the bounded UI
// trace has tool-call ordering and some user/final carriers. Match those
// carriers to anchor the history, then insert trace-only events between them.
// Exact order is unknowable when the trace has no matching carrier, so keep
// that fallback deterministic and leave the original JSON for diagnostics.
func mergeLegacyChatTrace(history, trace []internalevents.Event, sessionID string) []internalevents.Event {
	if len(trace) == 0 {
		return history
	}
	// Carriers are decoded once per event: decoding inside the nested
	// history x trace scan made large imports quadratic in JSON round trips.
	historyCarriers := make([]legacyCarrier, len(history))
	historyHasCarrier := make(map[legacyCarrier]bool, len(history))
	for index, event := range history {
		historyCarriers[index] = newLegacyCarrier(event)
		if historyCarriers[index].valid() {
			historyHasCarrier[historyCarriers[index]] = true
		}
	}
	traceCarriers := make([]legacyCarrier, len(trace))
	traceIndexesByCarrier := make(map[legacyCarrier][]int)
	for index, event := range trace {
		traceCarriers[index] = newLegacyCarrier(event)
		if traceCarriers[index].valid() {
			traceIndexesByCarrier[traceCarriers[index]] = append(traceIndexesByCarrier[traceCarriers[index]], index)
		}
	}
	// Walk history backwards, anchoring each carrier to the latest matching
	// trace entry strictly before the previous anchor.
	anchors := make(map[int]int)
	nextTrace := len(trace) - 1
	for historyIndex := len(history) - 1; historyIndex >= 0 && nextTrace >= 0; historyIndex-- {
		carrier := historyCarriers[historyIndex]
		if !carrier.valid() {
			continue
		}
		indexes := traceIndexesByCarrier[carrier]
		position := sort.SearchInts(indexes, nextTrace+1) - 1
		if position < 0 {
			continue
		}
		anchors[indexes[position]] = historyIndex
		nextTrace = indexes[position] - 1
	}
	imported := make([]internalevents.Event, 0, len(history)+len(trace))
	historyCursor := 0
	if len(anchors) == 0 {
		// The trace is usually a bounded tail. With no reliable carrier anchor,
		// keep history intact and put its diagnostics before the last answer.
		if len(history) > 0 && history[len(history)-1].Type == "streaming_chunk" {
			imported = append(imported, history[:len(history)-1]...)
			historyCursor = len(history) - 1
		} else {
			imported = append(imported, history...)
			historyCursor = len(history)
		}
	} else {
		firstAnchor := len(history)
		for _, historyIndex := range anchors {
			if historyIndex < firstAnchor {
				firstAnchor = historyIndex
			}
		}
		imported = append(imported, history[:firstAnchor]...)
		historyCursor = firstAnchor
	}
	for traceIndex, event := range trace {
		if anchor, ok := anchors[traceIndex]; ok {
			for historyCursor <= anchor {
				imported = append(imported, history[historyCursor])
				historyCursor++
			}
			continue // The matched user/final carrier is already in history.
		}
		carrier := traceCarriers[traceIndex]
		if carrier.valid() && historyHasCarrier[carrier] {
			continue // Other trace copies of the same carrier are not extra turns.
		}
		if event.ID == "" {
			event.ID = legacyChatEventID(sessionID, traceIndex, "trace:"+event.Type, carrier.text)
		}
		imported = append(imported, event)
	}
	imported = append(imported, history[historyCursor:]...)
	return imported
}

type legacyCarrier struct {
	role, text string
}

func newLegacyCarrier(event internalevents.Event) legacyCarrier {
	role, text := legacyEventCarrier(event)
	return legacyCarrier{role: role, text: text}
}

func (carrier legacyCarrier) valid() bool {
	return carrier.role != "" && carrier.text != ""
}

func legacyEventCarrier(event internalevents.Event) (string, string) {
	role := ""
	switch event.Type {
	case "user_message":
		role = "user"
	case "streaming_chunk", "llm_generation_end", "unified_completion":
		role = "assistant"
	default:
		return "", ""
	}
	if event.Data == nil || event.Data.Data == nil {
		return "", ""
	}
	raw, err := json.Marshal(event.Data.Data)
	if err != nil {
		return "", ""
	}
	var payload map[string]interface{}
	if json.Unmarshal(raw, &payload) != nil {
		return "", ""
	}
	if nested, ok := payload["data"].(map[string]interface{}); ok {
		for key, value := range nested {
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
	}
	for _, field := range []string{"content", "final_result", "result"} {
		if value, ok := payload[field].(string); ok && strings.TrimSpace(value) != "" {
			return role, strings.ToLower(strings.Join(strings.Fields(value), " "))
		}
	}
	return "", ""
}

func legacyChatEventID(sessionID string, index int, role, content string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", sessionID, index, role, content)))
	return "legacy-chat-" + hex.EncodeToString(digest[:12])
}
