package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	mcpagent "mcpagent/agent"
	"mcpagent/llm"
	"mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/internal/events"
)

// SummarizeConversationRequest represents a request to summarize conversation history
type SummarizeConversationRequest struct {
	KeepLastMessages int `json:"keep_last_messages,omitempty"` // Optional: number of recent messages to keep (default: 8)
}

// SummarizeConversationResponse represents the response for summarization
type SummarizeConversationResponse struct {
	SessionID     string `json:"session_id"`
	Status        string `json:"status"`
	Message       string `json:"message,omitempty"`
	OriginalCount int    `json:"original_count,omitempty"`
	NewCount      int    `json:"new_count,omitempty"`
	ReducedBy     int    `json:"reduced_by,omitempty"`
	Summary       string `json:"summary,omitempty"`
}

// handleSummarizeConversation handles manual conversation summarization
func (api *StreamingAPI) handleSummarizeConversation(w http.ResponseWriter, r *http.Request) {
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

	log.Printf("[SUMMARIZATION DEBUG] Requested session ID: %s", sessionID)
	log.Printf("[SUMMARIZATION DEBUG] Available session IDs in conversation history: %v", func() []string {
		api.conversationMux.RLock()
		defer api.conversationMux.RUnlock()
		keys := make([]string, 0, len(api.conversationHistory))
		for k := range api.conversationHistory {
			keys = append(keys, k)
		}
		return keys
	}())

	var req SummarizeConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If body is empty, use defaults
		req = SummarizeConversationRequest{}
	}

	// Get conversation history
	api.conversationMux.RLock()
	messages, exists := api.conversationHistory[sessionID]
	api.conversationMux.RUnlock()

	log.Printf("[SUMMARIZATION DEBUG] Session %s exists: %v, message count: %d", sessionID, exists, len(messages))

	if !exists || len(messages) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(SummarizeConversationResponse{
			SessionID: sessionID,
			Status:    "error",
			Message:   "No conversation history found for this session",
		})
		return
	}

	// Use server defaults for LLM config
	provider := api.provider
	modelID := api.model

	// Create LLM instance for summarization
	llmProvider, err := llm.ValidateProvider(provider)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid provider: %v", err), http.StatusInternalServerError)
		return
	}

	llmConfig := llm.Config{
		Provider:    llmProvider,
		ModelID:     modelID,
		Temperature: 0.0, // Use 0.0 for consistent summaries
		Logger:      createLLMLogger(),
	}

	summarizationLLM, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create LLM for summarization: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a minimal agent for summarization (no MCP servers needed)
	ctx := context.Background()
	keepLastMessages := req.KeepLastMessages
	if keepLastMessages <= 0 {
		keepLastMessages = 8 // Default: keep last 8 messages
	}

	// Get observer ID from active session or request header
	observerID := r.Header.Get("X-Observer-ID")
	if observerID == "" {
		// Try to get observer ID from active session
		if activeSession, exists := api.getActiveSession(sessionID); exists {
			observerID = activeSession.ObserverID
			log.Printf("[SUMMARIZATION] Using observer ID from active session: %s", observerID)
		}
	}

	// If still no observer ID, create a temporary one for event storage
	if observerID == "" {
		observerID = fmt.Sprintf("summarization_%s_%d", sessionID, time.Now().UnixNano())
		log.Printf("[SUMMARIZATION] Created temporary observer ID: %s", observerID)
	}

	// Create minimal agent with NO_SERVERS to avoid connecting to MCP servers
	tempAgent, err := mcpagent.NewAgent(
		ctx,
		summarizationLLM,
		api.mcpConfigPath,
		mcpagent.WithServerName(mcpclient.NoServers), // No MCP servers needed for summarization
		mcpagent.WithContextSummarization(true),
		mcpagent.WithSummaryKeepLastMessages(keepLastMessages),
		mcpagent.WithLogger(api.logger),
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create agent for summarization: %v", err), http.StatusInternalServerError)
		return
	}

	// Attach event observer to capture summarization events
	if api.eventStore != nil {
		eventObserver := events.NewEventObserverWithLogger(api.eventStore, observerID, sessionID, api.logger)
		tempAgent.AddEventListener(eventObserver)
		log.Printf("[SUMMARIZATION] Attached event observer %s to capture summarization events", observerID)
	} else {
		log.Printf("[SUMMARIZATION] Warning: eventStore is nil, events will not be captured")
	}

	// Call summarization
	summarizedMessages, err := mcpagent.SummarizeConversationHistory(tempAgent, ctx, messages, keepLastMessages)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to summarize conversation: %v", err), http.StatusInternalServerError)
		return
	}

	// Extract summary from the summarized messages (it's added as a user message)
	var summary string
	for i, msg := range summarizedMessages {
		if msg.Role == llmtypes.ChatMessageTypeHuman {
			for _, part := range msg.Parts {
				if textPart, ok := part.(llmtypes.TextContent); ok {
					if strings.Contains(textPart.Text, "=== CONVERSATION SUMMARY") {
						summary = textPart.Text
						break
					}
				}
			}
			if summary != "" {
				break
			}
		}
		// Only check first few messages for summary
		if i > 5 {
			break
		}
	}

	// Update conversation history
	api.conversationMux.Lock()
	api.conversationHistory[sessionID] = summarizedMessages
	api.conversationMux.Unlock()

	log.Printf("[SUMMARIZATION] Summarized conversation for session %s: %d -> %d messages", sessionID, len(messages), len(summarizedMessages))

	response := SummarizeConversationResponse{
		SessionID:     sessionID,
		Status:        "success",
		Message:       "Conversation summarized successfully",
		OriginalCount: len(messages),
		NewCount:      len(summarizedMessages),
		ReducedBy:     len(messages) - len(summarizedMessages),
		Summary:       summary,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
