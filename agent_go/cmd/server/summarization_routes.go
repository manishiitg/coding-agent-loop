package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	mcpagent "mcpagent/agent"
	"mcpagent/llm"
	"mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/internal/events"
)

// SummarizeConversationRequest represents a request to summarize conversation history
type SummarizeConversationRequest struct {
	KeepLastMessages int `json:"keep_last_messages,omitempty"` // Optional: number of recent messages to keep (default: 4, matches orchestrator)
	// Context summarization configuration (optional - uses orchestrator defaults if not provided)
	EnableContextSummarization     *bool   `json:"enable_context_summarization,omitempty"`       // Enable context summarization feature (nil = inherit default, true/false = explicit override)
	SummarizeOnTokenThreshold      *bool   `json:"summarize_on_token_threshold,omitempty"`       // Enable token-based summarization trigger (nil = inherit default, true/false = explicit override)
	TokenThresholdPercent          float64 `json:"token_threshold_percent,omitempty"`            // Percentage of context window to trigger summarization (0.0-1.0, default: 0.8 = 80%)
	SummarizeOnFixedTokenThreshold *bool   `json:"summarize_on_fixed_token_threshold,omitempty"` // Enable fixed token-based summarization trigger (nil = inherit default, true/false = explicit override)
	FixedTokenThreshold            int     `json:"fixed_token_threshold,omitempty"`              // Fixed token threshold to trigger summarization (default: 200000 = 200k tokens, matches orchestrator)
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

	// Load context summarization settings with orchestrator defaults
	// Priority: Request > Environment Variable > Default (matches orchestrator defaults)
	enableContextSummarization := func() bool {
		// If explicitly set in request, use that value
		if req.EnableContextSummarization != nil {
			return *req.EnableContextSummarization
		}
		// Check environment variable - default to enabled (true), can be disabled via "false"
		if envVal := os.Getenv("ENABLE_CONTEXT_SUMMARIZATION"); envVal == "false" {
			return false
		}
		return true // Default to enabled (matches orchestrator)
	}()

	summarizeOnTokenThreshold := func() bool {
		// If explicitly set in request, use that value
		if req.SummarizeOnTokenThreshold != nil {
			return *req.SummarizeOnTokenThreshold
		}
		// Check environment variable - default to enabled (true), can be disabled via "false"
		if envVal := os.Getenv("SUMMARIZE_ON_TOKEN_THRESHOLD"); envVal == "false" {
			return false
		}
		return true // Default to enabled (matches orchestrator)
	}()

	tokenThresholdPercent := req.TokenThresholdPercent
	if tokenThresholdPercent <= 0 {
		// Check environment variable
		if envVal := os.Getenv("TOKEN_THRESHOLD_PERCENT"); envVal != "" {
			if threshold, err := strconv.ParseFloat(envVal, 64); err == nil && threshold > 0 && threshold <= 1.0 {
				tokenThresholdPercent = threshold
			}
		}
		if tokenThresholdPercent <= 0 {
			tokenThresholdPercent = 0.8 // Default to 80% (matches orchestrator)
		}
	}

	summarizeOnFixedTokenThreshold := func() bool {
		// If explicitly set in request, use that value
		if req.SummarizeOnFixedTokenThreshold != nil {
			return *req.SummarizeOnFixedTokenThreshold
		}
		// Check environment variable - default to enabled (true), can be disabled via "false"
		if envVal := os.Getenv("SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD"); envVal == "false" {
			return false
		}
		return true // Default to enabled (matches orchestrator)
	}()

	fixedTokenThreshold := req.FixedTokenThreshold
	if fixedTokenThreshold <= 0 {
		// Check environment variable
		if envVal := os.Getenv("FIXED_TOKEN_THRESHOLD"); envVal != "" {
			if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
				fixedTokenThreshold = threshold
			}
		}
		if fixedTokenThreshold <= 0 {
			fixedTokenThreshold = 200000 // Default to 200k tokens (matches orchestrator)
		}
	}

	keepLastMessages := req.KeepLastMessages
	if keepLastMessages <= 0 {
		// Check environment variable
		if envVal := os.Getenv("SUMMARY_KEEP_LAST_MESSAGES"); envVal != "" {
			if keepLast, err := strconv.Atoi(envVal); err == nil && keepLast > 0 {
				keepLastMessages = keepLast
			}
		}
		if keepLastMessages <= 0 {
			keepLastMessages = 4 // Default to 4 messages (matches orchestrator)
		}
	}

	// No observer ID needed - events are stored by sessionID

	// Build agent options
	agentOptions := []mcpagent.AgentOption{
		mcpagent.WithServerName(mcpclient.NoServers), // No MCP servers needed for summarization
		mcpagent.WithLogger(api.logger),
	}

	// Add context summarization configuration
	if enableContextSummarization {
		agentOptions = append(agentOptions, mcpagent.WithContextSummarization(true))
		if summarizeOnTokenThreshold {
			agentOptions = append(agentOptions, mcpagent.WithSummarizeOnTokenThreshold(true, tokenThresholdPercent))
		}
		if summarizeOnFixedTokenThreshold && fixedTokenThreshold > 0 {
			agentOptions = append(agentOptions, mcpagent.WithSummarizeOnFixedTokenThreshold(true, fixedTokenThreshold))
		}
		if keepLastMessages > 0 {
			agentOptions = append(agentOptions, mcpagent.WithSummaryKeepLastMessages(keepLastMessages))
		}
	}

	// Create minimal agent with NO_SERVERS to avoid connecting to MCP servers
	tempAgent, err := mcpagent.NewAgent(
		ctx,
		summarizationLLM,
		api.mcpConfigPath,
		agentOptions...,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create agent for summarization: %v", err), http.StatusInternalServerError)
		return
	}

	// Attach event observer to capture summarization events
	if api.eventStore != nil {
		eventObserver := events.NewEventObserverWithLogger(api.eventStore, sessionID, api.logger)
		tempAgent.AddEventListener(eventObserver)
		log.Printf("[SUMMARIZATION] Attached event observer to capture summarization events for session %s", sessionID)
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
