package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/internal/events"
)

// CompactContextRequest represents a request to compact stale tool responses
type CompactContextRequest struct {
	TokenThreshold int `json:"token_threshold,omitempty"` // Optional: token threshold (default: 1000)
	TurnThreshold  int `json:"turn_threshold,omitempty"`  // Optional: turn age threshold (default: 10)
}

// CompactContextResponse represents the response for context editing
type CompactContextResponse struct {
	SessionID        string `json:"session_id"`
	Status           string `json:"status"`
	Message          string `json:"message,omitempty"`
	TotalMessages    int    `json:"total_messages,omitempty"`
	CompactedCount   int    `json:"compacted_count,omitempty"`
	TotalTokensSaved int    `json:"total_tokens_saved,omitempty"`
}

// handleCompactContext handles manual context editing (compacting stale tool responses)
func (api *StreamingAPI) handleCompactContext(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Also check header as fallback (for consistency with other endpoints)
	if sessionID == "" {
		sessionID = r.Header.Get("X-Session-ID")
	}

	log.Printf("[CONTEXT_EDITING DEBUG] Requested session ID: %s", sessionID)

	var req CompactContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If body is empty, use defaults
		req = CompactContextRequest{}
	}

	// Get conversation history
	api.conversationMux.RLock()
	messages, exists := api.conversationHistory[sessionID]
	api.conversationMux.RUnlock()

	log.Printf("[CONTEXT_EDITING DEBUG] Session %s exists: %v, message count: %d", sessionID, exists, len(messages))

	if !exists || len(messages) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(CompactContextResponse{
			SessionID: sessionID,
			Status:    "error",
			Message:   "No conversation history found for this session",
		})
		return
	}

	// Use server defaults for LLM config
	provider := api.provider
	modelID := api.model

	// Create LLM instance
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid provider: %v", err), http.StatusInternalServerError)
		return
	}

	llmConfig := llm.Config{
		Provider:    llmProvider,
		ModelID:     modelID,
		Temperature: 0.0,
		Logger:      api.logger,
	}

	compactLLM, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create LLM for context editing: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a minimal agent for context editing (no MCP servers needed)
	ctx := context.Background()
	tokenThreshold := req.TokenThreshold
	if tokenThreshold <= 0 {
		tokenThreshold = 1000 // Default: 1000 tokens
	}
	turnThreshold := req.TurnThreshold
	if turnThreshold <= 0 {
		turnThreshold = 10 // Default: 10 turns
	}

	// No observer ID needed - events are stored by sessionID

	// Create minimal agent with NO_SERVERS to avoid connecting to MCP servers
	tempAgent, err := mcpagent.NewAgent(
		ctx,
		compactLLM,
		api.mcpConfigPath,
		mcpagent.WithServerName(mcpclient.NoServers), // No MCP servers needed for context editing
		mcpagent.WithContextEditing(true),
		mcpagent.WithContextEditingThreshold(tokenThreshold),
		mcpagent.WithContextEditingTurnThreshold(turnThreshold),
		mcpagent.WithLogger(api.logger),
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create agent for context editing: %v", err), http.StatusInternalServerError)
		return
	}

	// Attach event observer to capture context editing events
	if api.eventStore != nil {
		eventObserver := events.NewEventObserverWithLogger(api.eventStore, sessionID, api.logger)
		tempAgent.AddEventListener(eventObserver)
		log.Printf("[CONTEXT_EDITING] Attached event observer to capture context editing events for session %s", sessionID)
	} else {
		log.Printf("[CONTEXT_EDITING] Warning: eventStore is nil, events will not be captured")
	}

	// Get current turn count (estimate from message count)
	currentTurn := len(messages) / 3 // Rough estimate: each turn = user + assistant + tool

	// Manually trigger compaction
	compactedMessages, err := mcpagent.CompactStaleToolResponses(tempAgent, ctx, messages, currentTurn)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to compact context: %v", err), http.StatusInternalServerError)
		return
	}

	// Count compacted tool responses by comparing before/after
	compactedCount := 0
	totalTokensSaved := 0

	// Create a map of original tool response contents for comparison
	originalContents := make(map[int]string)
	for i, msg := range messages {
		if msg.Role == llmtypes.ChatMessageTypeTool {
			for _, part := range msg.Parts {
				if toolResp, ok := part.(llmtypes.ToolCallResponse); ok {
					originalContents[i] = toolResp.Content
					break
				}
			}
		}
	}

	// Compare compacted messages with originals
	for i, msg := range compactedMessages {
		if msg.Role == llmtypes.ChatMessageTypeTool {
			for _, part := range msg.Parts {
				if toolResp, ok := part.(llmtypes.ToolCallResponse); ok {
					if originalContent, exists := originalContents[i]; exists {
						if originalContent != toolResp.Content {
							// This was compacted (content changed)
							compactedCount++
							// Estimate tokens saved (rough calculation: original - compacted)
							originalTokens := len(originalContent) / 4
							compactedTokens := len(toolResp.Content) / 4
							if originalTokens > compactedTokens {
								totalTokensSaved += originalTokens - compactedTokens
							}
						}
					}
				}
			}
		}
	}

	// Update conversation history
	api.conversationMux.Lock()
	api.conversationHistory[sessionID] = compactedMessages
	api.conversationMux.Unlock()

	log.Printf("[CONTEXT_EDITING] Compacted context for session %s: %d tool responses compacted, ~%d tokens saved", sessionID, compactedCount, totalTokensSaved)

	response := CompactContextResponse{
		SessionID:        sessionID,
		Status:           "success",
		Message:          "Context compacted successfully",
		TotalMessages:    len(compactedMessages),
		CompactedCount:   compactedCount,
		TotalTokensSaved: totalTokensSaved,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
