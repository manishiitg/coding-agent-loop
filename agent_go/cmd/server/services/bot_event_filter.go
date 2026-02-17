package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/mcpagent/events"

	"mcp-agent-builder-go/agent_go/pkg/database"
)

// BotEventFilter applies 3-tier filtering on agent events and posts updates to a platform thread.
// Tier 1 (immediate): agent_start/end, errors, delegation events, human feedback
// Tier 2 (batched): tool calls, LLM completions — batched every 10-15s
// Tier 3 (skip): streaming chunks, system prompts, debug, performance events
type BotEventFilter struct {
	connector    BotConnector
	threadID     ThreadID
	botSessionID string
	db           database.Database

	mu           sync.Mutex
	batchBuffer  []string
	lastFlush    time.Time
	batchInterval time.Duration
}

// NewBotEventFilter creates a new event filter
func NewBotEventFilter(connector BotConnector, threadID ThreadID, botSessionID string, db database.Database) *BotEventFilter {
	return &BotEventFilter{
		connector:     connector,
		threadID:      threadID,
		botSessionID:  botSessionID,
		db:            db,
		batchBuffer:   make([]string, 0),
		lastFlush:     time.Now(),
		batchInterval: 12 * time.Second,
	}
}

// Start begins listening to events for a session and forwarding filtered updates to the thread
func (f *BotEventFilter) Start(ctx context.Context, subscriber BotEventSubscriber, sessionID string) {
	ch, unsubscribe := subscriber.SubscribeBot(sessionID)
	defer unsubscribe()

	// Batch flush ticker
	ticker := time.NewTicker(f.batchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			f.flushBatch(ctx)
			return

		case event, ok := <-ch:
			if !ok {
				f.flushBatch(ctx)
				return
			}
			f.processEvent(ctx, event)

		case <-ticker.C:
			f.flushBatch(ctx)
		}
	}
}

// processEvent applies tier classification and handles accordingly
func (f *BotEventFilter) processEvent(ctx context.Context, event BotEventData) {
	tier := classifyEvent(event.Type)

	switch tier {
	case 1:
		// Send immediately
		msg := formatTier1Event(event)
		if msg != "" {
			msgID, err := f.connector.SendThreadMessage(ctx, f.threadID, msg)
			if err != nil {
				log.Printf("[BOT_FILTER] Failed to send tier-1 event: %v", err)
				return
			}
			f.recordMessage(msg, msgID)
		}

	case 2:
		// Add to batch buffer
		msg := formatTier2Event(event)
		if msg != "" {
			f.mu.Lock()
			f.batchBuffer = append(f.batchBuffer, msg)
			f.mu.Unlock()
		}

	default:
		// Tier 3: skip
	}
}

// flushBatch sends accumulated tier-2 events as a single message
func (f *BotEventFilter) flushBatch(ctx context.Context) {
	f.mu.Lock()
	if len(f.batchBuffer) == 0 {
		f.mu.Unlock()
		return
	}
	items := make([]string, len(f.batchBuffer))
	copy(items, f.batchBuffer)
	f.batchBuffer = f.batchBuffer[:0]
	f.lastFlush = time.Now()
	f.mu.Unlock()

	// Deduplicate and summarize
	summary := summarizeBatchItems(items)
	if summary == "" {
		return
	}

	msgID, err := f.connector.SendThreadMessage(ctx, f.threadID, summary)
	if err != nil {
		log.Printf("[BOT_FILTER] Failed to send batch update: %v", err)
		return
	}
	f.recordMessage(summary, msgID)
}

func (f *BotEventFilter) recordMessage(content, platformMsgID string) {
	f.db.CreateBotMessage(context.Background(), &database.CreateBotMessageRequest{
		BotSessionID:      f.botSessionID,
		Direction:         "outgoing",
		MessageType:       "progress",
		Content:           content,
		PlatformMessageID: platformMsgID,
	})
}

// classifyEvent returns the tier (1=immediate, 2=batched, 3=skip) for an event type
func classifyEvent(eventType string) int {
	// Tier 1 — send immediately
	tier1 := map[string]bool{
		"agent_start":              true,
		"agent_end":                true,
		"agent_error":              true,
		"delegation_start":         true,
		"delegation_end":           true,
		"blocking_human_feedback":  true,
		"blocking_human_questions": true,
		"conversation_error":       true,
	}
	if tier1[eventType] {
		return 1
	}

	// Tier 2 — batch every 10-15s
	tier2 := map[string]bool{
		"tool_call_start":          true,
		"tool_call_end":            true,
		"unified_llm_completion":   true,
		"conversation_turn":        true,
	}
	if tier2[eventType] {
		return 2
	}

	// Tier 3 — skip everything else
	return 3
}

// formatTier1Event creates a message for immediate events
func formatTier1Event(event BotEventData) string {
	if event.Data == nil {
		return ""
	}

	switch event.Type {
	case "agent_start":
		return "Agent started processing..."
	case "agent_end":
		return "Agent completed."
	case "agent_error":
		return "Agent encountered an error."
	case "delegation_start":
		return "Sub-agent started..."
	case "delegation_end":
		return "Sub-agent completed."
	case "blocking_human_feedback":
		// Extract the question from the event data
		return "Waiting for your input (see above)."
	case "blocking_human_questions":
		return "Waiting for your answers (see above)."
	case "conversation_error":
		return "An error occurred during processing."
	}
	return ""
}

// formatTier2Event creates a summary line for batched events
func formatTier2Event(event BotEventData) string {
	if event.Data == nil || event.Data.Data == nil {
		return ""
	}

	// Extract metadata from the event data's base data if available
	toolName := extractMetadataString(event.Data, "tool_name")
	text := extractMetadataString(event.Data, "text")

	switch event.Type {
	case "tool_call_start":
		if toolName != "" {
			return fmt.Sprintf("Running: %s", toolName)
		}
		return "Running tool..."
	case "tool_call_end":
		if toolName != "" {
			return fmt.Sprintf("Completed: %s", toolName)
		}
		return ""
	case "unified_llm_completion":
		if text != "" {
			if len(text) > 200 {
				text = text[:200] + "..."
			}
			return text
		}
		return ""
	}
	return ""
}

// extractMetadataString tries to extract a string value from event metadata
func extractMetadataString(event *events.AgentEvent, key string) string {
	if event == nil || event.Data == nil {
		return ""
	}

	// Try to get metadata from BaseEventData via GetBaseEventData()
	type baseGetter interface {
		GetBaseEventData() *events.BaseEventData
	}
	if bg, ok := event.Data.(baseGetter); ok {
		base := bg.GetBaseEventData()
		if base != nil && base.Metadata != nil {
			if v, exists := base.Metadata[key]; exists {
				if s, ok := v.(string); ok {
					return s
				}
			}
		}
	}

	return ""
}

// summarizeBatchItems deduplicates and creates a compact summary
func summarizeBatchItems(items []string) string {
	if len(items) == 0 {
		return ""
	}

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, item := range items {
		if !seen[item] && item != "" {
			seen[item] = true
			unique = append(unique, item)
		}
	}

	if len(unique) == 0 {
		return ""
	}

	// Limit to last 5 items to keep messages compact
	if len(unique) > 5 {
		unique = unique[len(unique)-5:]
	}

	return strings.Join(unique, "\n")
}
