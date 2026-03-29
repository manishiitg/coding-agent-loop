package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/pkg/database"

	pkgevents "github.com/manishiitg/mcpagent/events"

	"github.com/gin-gonic/gin"
)

// ChatHistoryRoutes sets up chat history API routes
func ChatHistoryRoutes(router *gin.Engine, db database.Database) {
	api := router.Group("/api/chat-history")
	{
		// Chat session management
		api.POST("/sessions", createChatSession(db))
		api.GET("/sessions", listChatSessions(db))
		api.GET("/sessions/:session_id", getChatSession(db))
		api.PUT("/sessions/:session_id", updateChatSession(db))
		api.DELETE("/sessions/:session_id", deleteChatSession(db))

		// Events
		api.GET("/sessions/:session_id/events", getSessionEvents(db))
		api.GET("/events", searchEvents(db))

		// Costs
		api.GET("/costs", getAllSessionCosts(db))
		api.GET("/sessions/:session_id/costs", getSessionCosts(db))

		// Delegation logs (multi-agent mode)
		api.GET("/sessions/:session_id/delegation-logs", getDelegationLogs(db))
		api.GET("/sessions/:session_id/delegation-logs/:delegation_id/events", getDelegationEvents(db))

		// Preset queries management
		api.POST("/presets", createPresetQuery(db))
		api.GET("/presets", listPresetQueries(db))
		api.GET("/presets/:id", getPresetQuery(db))
		api.PUT("/presets/:id", updatePresetQuery(db))
		api.DELETE("/presets/:id", deletePresetQuery(db))

		// Health check
		api.GET("/health", healthCheck(db))
	}
}

// createChatSession creates a new chat session (associated with current user)
func createChatSession(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req database.CreateChatSessionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Get user ID from context (set by auth middleware)
		userID := GetUserIDFromContext(c.Request.Context())

		// Create session associated with the current user
		session, err := db.CreateChatSessionWithUser(c.Request.Context(), &req, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, session)
	}
}

// listChatSessions lists all chat sessions with pagination (filtered by user)
func listChatSessions(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		limitStr := c.DefaultQuery("limit", "20")
		offsetStr := c.DefaultQuery("offset", "0")
		presetQueryID := c.Query("preset_query_id")
		agentMode := c.Query("agent_mode")

		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit parameter"})
			return
		}

		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset parameter"})
			return
		}

		// Convert preset_query_id to pointer for optional filtering
		var presetQueryIDPtr *string
		if presetQueryID != "" {
			presetQueryIDPtr = &presetQueryID
		}

		// Convert agent_mode to pointer for optional filtering
		var agentModePtr *string
		if agentMode != "" {
			agentModePtr = &agentMode
		}

		// Get user ID from context (set by auth middleware)
		userID := GetUserIDFromContext(c.Request.Context())

		// Use user-scoped query to only return sessions for this user
		sessions, total, err := db.ListChatSessionsWithUser(c.Request.Context(), limit, offset, presetQueryIDPtr, agentModePtr, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Ensure sessions is never null - convert to empty array if nil
		if sessions == nil {
			sessions = []database.ChatHistorySummary{}
		}

		c.JSON(http.StatusOK, gin.H{
			"sessions": sessions,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	}
}

// getChatSession gets a specific chat session (user-scoped)
func getChatSession(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
			return
		}

		// Get user ID from context (set by auth middleware)
		userID := GetUserIDFromContext(c.Request.Context())

		// Get session with user scope to ensure user owns the session
		session, err := db.GetChatSessionWithUser(c.Request.Context(), sessionID, userID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, session)
	}
}

// updateChatSession updates a chat session
func updateChatSession(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
			return
		}

		var req database.UpdateChatSessionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Auto-populate preset_query_id from config.workflow_metadata.preset_id
		// This ensures the top-level column stays in sync for efficient DB queries
		if req.PresetQueryID == "" && len(req.Config) > 0 {
			var config struct {
				WorkflowMetadata *struct {
					PresetID string `json:"preset_id"`
				} `json:"workflow_metadata"`
			}
			if json.Unmarshal(req.Config, &config) == nil && config.WorkflowMetadata != nil && config.WorkflowMetadata.PresetID != "" {
				req.PresetQueryID = config.WorkflowMetadata.PresetID
			}
		}

		session, err := db.UpdateChatSession(c.Request.Context(), sessionID, &req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, session)
	}
}

// deleteChatSession deletes a chat session
func deleteChatSession(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
			return
		}

		err := db.DeleteChatSession(c.Request.Context(), sessionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Chat session deleted successfully"})
	}
}

// getSessionEvents gets events for a specific session
func getSessionEvents(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
			return
		}

		limitStr := c.DefaultQuery("limit", "100")
		offsetStr := c.DefaultQuery("offset", "0")

		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit parameter"})
			return
		}

		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset parameter"})
			return
		}

		// Cap limit to prevent slow queries (max 500 per request)
		const maxLimit = 500
		if limit <= 0 || limit > maxLimit {
			limit = maxLimit
		}

		dbEvents, err := db.GetEventsBySession(c.Request.Context(), sessionID, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[CHAT_HISTORY] Loading events for session %s: found %d events", sessionID, len(dbEvents))

		// Convert database events to polling events format (same structure as polling API)
		convertedEvents := make([]events.Event, 0, len(dbEvents))
		parseErrors := 0
		filteredOut := 0
		for i, dbEvent := range dbEvents {
			// Unmarshal the full AgentEvent - use helper struct to handle EventData interface
			// Since EventData is an interface, we need to unmarshal Data as json.RawMessage first
			type agentEventWithRawData struct {
				Type           pkgevents.EventType `json:"type"`
				Timestamp      time.Time           `json:"timestamp"`
				EventIndex     int                 `json:"event_index"`
				TraceID        string              `json:"trace_id,omitempty"`
				SpanID         string              `json:"span_id,omitempty"`
				ParentID       string              `json:"parent_id,omitempty"`
				CorrelationID  string              `json:"correlation_id,omitempty"`
				HierarchyLevel int                 `json:"hierarchy_level"`
				SessionID      string              `json:"session_id,omitempty"`
				Component      string              `json:"component,omitempty"`
				Data           json.RawMessage     `json:"data"`
			}

			var helper agentEventWithRawData
			if err := json.Unmarshal(dbEvent.EventData, &helper); err != nil {
				parseErrors++
				if i < 3 {
					log.Printf("[CHAT_HISTORY ERROR] Failed to parse event %d for session %s: %v, event_type=%s", i, sessionID, err, dbEvent.EventType)
				}
				continue
			}

			// Apply event mode filtering (same as polling API)
			if !events.ShouldShowEvent(string(helper.Type)) {
				filteredOut++
				continue
			}

			// Unmarshal Data field into a map to preserve structure
			var dataMap map[string]interface{}
			if err := json.Unmarshal(helper.Data, &dataMap); err != nil {
				parseErrors++
				if i < 3 {
					log.Printf("[CHAT_HISTORY ERROR] Failed to parse event data %d for session %s: %v, event_type=%s", i, sessionID, err, dbEvent.EventType)
				}
				continue
			}

			// Create AgentEvent with GenericEventData wrapper
			agentEvent := pkgevents.AgentEvent{
				Type:           helper.Type,
				Timestamp:      helper.Timestamp,
				EventIndex:     helper.EventIndex,
				TraceID:        helper.TraceID,
				SpanID:         helper.SpanID,
				ParentID:       helper.ParentID,
				CorrelationID:  helper.CorrelationID,
				HierarchyLevel: helper.HierarchyLevel,
				SessionID:      helper.SessionID,
				Component:      helper.Component,
				Data: &pkgevents.GenericEventData{
					BaseEventData: pkgevents.BaseEventData{},
					Data:          dataMap,
				},
			}

			convertedEvents = append(convertedEvents, events.Event{
				ID:        dbEvent.ID,
				Type:      dbEvent.EventType,
				Timestamp: dbEvent.Timestamp,
				SessionID: sessionID,
				Data:      &agentEvent,
			})
		}

		// Match polling API DB-fallback behavior so restored history from the DB path
		// has the same ordering metadata as in-memory restores.
		for i := range convertedEvents {
			if convertedEvents[i].Data != nil && convertedEvents[i].Data.EventIndex == 0 {
				convertedEvents[i].Data.EventIndex = i
			}
		}

		log.Printf("[CHAT_HISTORY] Converted %d events: converted=%d, filtered_out=%d, parse_errors=%d", len(dbEvents), len(convertedEvents), filteredOut, parseErrors)

		// Get total count using COUNT(*) - O(1) with index
		total := offset + len(dbEvents)
		if count, err := db.CountEventsBySession(c.Request.Context(), sessionID); err == nil {
			total = count
		}

		c.JSON(http.StatusOK, gin.H{
			"events": convertedEvents,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// searchEvents searches events with filters
func searchEvents(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var filter database.EventFilter

		// Parse query parameters
		if sessionID := c.Query("session_id"); sessionID != "" {
			filter.SessionID = sessionID
		}

		if eventType := c.Query("event_type"); eventType != "" {
			filter.EventType = pkgevents.EventType(eventType)
		}

		if fromDateStr := c.Query("from_date"); fromDateStr != "" {
			if fromDate, err := time.Parse(time.RFC3339, fromDateStr); err == nil {
				filter.FromDate = fromDate
			}
		}

		if toDateStr := c.Query("to_date"); toDateStr != "" {
			if toDate, err := time.Parse(time.RFC3339, toDateStr); err == nil {
				filter.ToDate = toDate
			}
		}

		limitStr := c.DefaultQuery("limit", "100")
		offsetStr := c.DefaultQuery("offset", "0")

		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit parameter"})
			return
		}

		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset parameter"})
			return
		}

		filter.Limit = limit
		filter.Offset = offset

		req := &database.GetChatHistoryRequest{
			SessionID: filter.SessionID,
			EventType: string(filter.EventType),
			FromDate:  filter.FromDate,
			ToDate:    filter.ToDate,
			Limit:     filter.Limit,
			Offset:    filter.Offset,
		}

		response, err := db.GetEvents(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, response)
	}
}

// createPresetQuery creates a new preset query
func createPresetQuery(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req database.CreatePresetQueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Validate that folder is required for orchestrator and workflow modes
		if (req.AgentMode == "orchestrator" || req.AgentMode == "workflow") && req.SelectedFolder == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "folder selection is required for orchestrator and workflow presets"})
			return
		}

		preset, err := db.CreatePresetQuery(c.Request.Context(), &req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, preset)
	}
}

// listPresetQueries lists all preset queries
func listPresetQueries(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		limitStr := c.DefaultQuery("limit", "50")
		offsetStr := c.DefaultQuery("offset", "0")

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 50
		}

		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			offset = 0
		}

		presets, total, err := db.ListPresetQueries(c.Request.Context(), limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		response := database.ListPresetQueriesResponse{
			Presets: presets,
			Total:   total,
			Limit:   limit,
			Offset:  offset,
		}

		c.JSON(http.StatusOK, response)
	}
}

// getPresetQuery retrieves a specific preset query
func getPresetQuery(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "preset query ID is required"})
			return
		}

		preset, err := db.GetPresetQuery(c.Request.Context(), id)
		if err != nil {
			if err.Error() == "preset query not found" {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, preset)
	}
}

// updatePresetQuery updates a preset query
func updatePresetQuery(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "preset query ID is required"})
			return
		}

		var req database.UpdatePresetQueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Validate that folder is required for orchestrator and workflow modes
		if (req.AgentMode == "orchestrator" || req.AgentMode == "workflow") && req.SelectedFolder == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "folder selection is required for orchestrator and workflow presets"})
			return
		}

		preset, err := db.UpdatePresetQuery(c.Request.Context(), id, &req)
		if err != nil {
			if err.Error() == "preset query not found" {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, preset)
	}
}

// deletePresetQuery deletes a preset query
func deletePresetQuery(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "preset query ID is required"})
			return
		}

		err := db.DeletePresetQuery(c.Request.Context(), id)
		if err != nil {
			if err.Error() == "preset query not found" {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusNoContent, nil)
	}
}

// healthCheck provides a health check endpoint
func healthCheck(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "chat-history",
		})
	}
}

// ============================================================================
// Cost Analysis Types
// ============================================================================

// ChatModelUsage represents token usage and cost for a specific model
type ChatModelUsage struct {
	Provider            string  `json:"provider"`
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	ReasoningTokens     int     `json:"reasoning_tokens"`
	CacheTokens         int     `json:"cache_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheWriteTokens    int     `json:"cache_write_tokens"`
	LLMCallCount        int     `json:"llm_call_count"`
	InputCost           float64 `json:"input_cost_usd"`
	OutputCost          float64 `json:"output_cost_usd"`
	ReasoningCost       float64 `json:"reasoning_cost_usd"`
	CacheCost           float64 `json:"cache_cost_usd"`
	TotalCost           float64 `json:"total_cost_usd"`
	ContextWindowUsage  int     `json:"context_window_usage"`
	ModelContextWindow  int     `json:"model_context_window"`
	ContextUsagePercent float64 `json:"context_usage_percent"`
}

// SessionCostSummary represents cost summary for a single chat session
type SessionCostSummary struct {
	SessionID   string                     `json:"session_id"`
	Title       string                     `json:"title"`
	AgentMode   string                     `json:"agent_mode"`
	CreatedAt   time.Time                  `json:"created_at"`
	Status      string                     `json:"status"`
	TotalCost   float64                    `json:"total_cost_usd"`
	TotalInput  int                        `json:"total_input_tokens"`
	TotalOutput int                        `json:"total_output_tokens"`
	TotalCalls  int                        `json:"total_llm_calls"`
	ByModel     map[string]*ChatModelUsage `json:"by_model"`
	ByAgent     map[string]*ChatModelUsage `json:"by_agent,omitempty"`
}

// AggregateCosts represents aggregate costs across all sessions
type AggregateCosts struct {
	TotalCost     float64                    `json:"total_cost_usd"`
	TotalInput    int                        `json:"total_input_tokens"`
	TotalOutput   int                        `json:"total_output_tokens"`
	TotalCalls    int                        `json:"total_llm_calls"`
	TotalSessions int                        `json:"total_sessions"`
	ByModel       map[string]*ChatModelUsage `json:"by_model"`
	ByAgent       map[string]*ChatModelUsage `json:"by_agent,omitempty"`
}

// UserCostsResponse is the response for GET /api/chat-history/costs
type UserCostsResponse struct {
	Sessions  []SessionCostSummary `json:"sessions"`
	Aggregate AggregateCosts       `json:"aggregate"`
}

// SessionCostDetail is the response for GET /api/chat-history/sessions/:session_id/costs
type SessionCostDetail struct {
	SessionID       string                                `json:"session_id"`
	Title           string                                `json:"title"`
	CreatedAt       time.Time                             `json:"created_at"`
	ByModel         map[string]*ChatModelUsage            `json:"by_model"`
	ByTurnAndModel  map[string]map[string]*ChatModelUsage `json:"by_turn_and_model,omitempty"`
	ByAgentAndModel map[string]map[string]*ChatModelUsage `json:"by_agent_and_model,omitempty"`
	TotalCost       float64                               `json:"total_cost_usd"`
	TotalInput      int                                   `json:"total_input_tokens"`
	TotalOutput     int                                   `json:"total_output_tokens"`
	TotalCalls      int                                   `json:"total_llm_calls"`
}

// tokenUsageData represents the parsed token_usage event data from the DB
type tokenUsageData struct {
	ModelID          string                 `json:"model_id"`
	Provider         string                 `json:"provider"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ReasoningTokens  int                    `json:"reasoning_tokens"`
	InputCost        float64                `json:"input_cost_usd"`
	OutputCost       float64                `json:"output_cost_usd"`
	ReasoningCost    float64                `json:"reasoning_cost_usd"`
	CacheCost        float64                `json:"cache_cost_usd"`
	TotalCost        float64                `json:"total_cost_usd"`
	Turn             int                    `json:"turn"`
	Component        string                 `json:"component"`
	ContextWindowUsage  int                 `json:"context_window_usage"`
	ModelContextWindow  int                 `json:"model_context_window"`
	ContextUsagePercent float64             `json:"context_usage_percent"`
	GenerationInfo   map[string]interface{} `json:"generation_info"`
	// Extracted from GenerationInfo (not from JSON directly)
	cacheReadTokens  int
	cacheWriteTokens int
	// Extracted from event wrapper (not from inner data JSON)
	correlationID string
}

// ============================================================================
// Cost Analysis Handlers
// ============================================================================

// getAllSessionCosts returns aggregate costs across all user's chat sessions
func getAllSessionCosts(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := GetUserIDFromContext(c.Request.Context())
		agentMode := c.Query("agent_mode")
		limitStr := c.DefaultQuery("limit", "100")

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 100
		}

		// Convert agent_mode to pointer for optional filtering
		var agentModePtr *string
		if agentMode != "" {
			agentModePtr = &agentMode
		}

		// Get user's chat sessions
		sessions, _, err := db.ListChatSessionsWithUser(c.Request.Context(), limit, 0, nil, agentModePtr, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list sessions: %v", err)})
			return
		}

		aggregate := AggregateCosts{
			ByModel: make(map[string]*ChatModelUsage),
			ByAgent: make(map[string]*ChatModelUsage),
		}

		var sessionCosts []SessionCostSummary

		for _, session := range sessions {
			// Query token_usage events for this session
			tokenEvents, err := getTokenUsageEventsForSession(c, db, session.SessionID)
			if err != nil {
				log.Printf("[COSTS] Error getting token events for session %s: %v", session.SessionID, err)
				continue
			}

			summary := SessionCostSummary{
				SessionID: session.SessionID,
				Title:     session.Title,
				AgentMode: session.AgentMode,
				CreatedAt: session.CreatedAt,
				Status:    session.Status,
				ByModel:   make(map[string]*ChatModelUsage),
				ByAgent:   make(map[string]*ChatModelUsage),
			}

			for _, tud := range tokenEvents {
				// Accumulate by model
				modelUsage := getOrCreateModelUsage(summary.ByModel, tud.ModelID)
				accumulateUsage(modelUsage, &tud)

				// Accumulate by agent (component)
				if tud.Component != "" {
					agentUsage := getOrCreateModelUsage(summary.ByAgent, tud.Component)
					accumulateUsage(agentUsage, &tud)
				}

				// Session totals
				summary.TotalCost += tud.TotalCost
				summary.TotalInput += tud.PromptTokens
				summary.TotalOutput += tud.CompletionTokens
				summary.TotalCalls++
			}

			sessionCosts = append(sessionCosts, summary)

			// Aggregate totals
			aggregate.TotalCost += summary.TotalCost
			aggregate.TotalInput += summary.TotalInput
			aggregate.TotalOutput += summary.TotalOutput
			aggregate.TotalCalls += summary.TotalCalls

			// Merge per-model into aggregate
			for modelID, usage := range summary.ByModel {
				aggModel := getOrCreateModelUsage(aggregate.ByModel, modelID)
				mergeUsage(aggModel, usage)
			}
			for agentName, usage := range summary.ByAgent {
				aggAgent := getOrCreateModelUsage(aggregate.ByAgent, agentName)
				mergeUsage(aggAgent, usage)
			}
		}

		aggregate.TotalSessions = len(sessionCosts)

		// Ensure sessions is never null
		if sessionCosts == nil {
			sessionCosts = []SessionCostSummary{}
		}

		c.JSON(http.StatusOK, UserCostsResponse{
			Sessions:  sessionCosts,
			Aggregate: aggregate,
		})
	}
}

// getSessionCosts returns detailed cost breakdown for a single session
func getSessionCosts(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
			return
		}

		userID := GetUserIDFromContext(c.Request.Context())

		// Validate session ownership
		session, err := db.GetChatSessionWithUser(c.Request.Context(), sessionID, userID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}

		// Query token_usage events
		tokenEvents, err := getTokenUsageEventsForSession(c, db, sessionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get token events: %v", err)})
			return
		}

		detail := SessionCostDetail{
			SessionID:       session.SessionID,
			Title:           session.Title,
			CreatedAt:       session.CreatedAt,
			ByModel:         make(map[string]*ChatModelUsage),
			ByTurnAndModel:  make(map[string]map[string]*ChatModelUsage),
			ByAgentAndModel: make(map[string]map[string]*ChatModelUsage),
		}

		for _, tud := range tokenEvents {
			// By model
			modelUsage := getOrCreateModelUsage(detail.ByModel, tud.ModelID)
			accumulateUsage(modelUsage, &tud)

			// By turn and model
			turnKey := fmt.Sprintf("turn-%d", tud.Turn)
			if detail.ByTurnAndModel[turnKey] == nil {
				detail.ByTurnAndModel[turnKey] = make(map[string]*ChatModelUsage)
			}
			turnModelUsage := getOrCreateModelUsage(detail.ByTurnAndModel[turnKey], tud.ModelID)
			accumulateUsage(turnModelUsage, &tud)

			// By agent and model
			agentKey := tud.Component
			if agentKey == "" {
				agentKey = "main"
			}
			if detail.ByAgentAndModel[agentKey] == nil {
				detail.ByAgentAndModel[agentKey] = make(map[string]*ChatModelUsage)
			}
			agentModelUsage := getOrCreateModelUsage(detail.ByAgentAndModel[agentKey], tud.ModelID)
			accumulateUsage(agentModelUsage, &tud)

			// Totals
			detail.TotalCost += tud.TotalCost
			detail.TotalInput += tud.PromptTokens
			detail.TotalOutput += tud.CompletionTokens
			detail.TotalCalls++
		}

		// Also fetch sub-agent session costs (IDs like {parent}-sub-{n}-{ts})
		subSessionPrefix := sessionID + "-sub-"
		subTokenEvents := getSubSessionTokenEvents(c, db, subSessionPrefix)
		for _, tud := range subTokenEvents {
			modelUsage := getOrCreateModelUsage(detail.ByModel, tud.ModelID)
			accumulateUsage(modelUsage, &tud)

			agentKey := tud.Component
			if agentKey == "" {
				agentKey = "sub-agent"
			}
			if detail.ByAgentAndModel[agentKey] == nil {
				detail.ByAgentAndModel[agentKey] = make(map[string]*ChatModelUsage)
			}
			agentModelUsage := getOrCreateModelUsage(detail.ByAgentAndModel[agentKey], tud.ModelID)
			accumulateUsage(agentModelUsage, &tud)

			detail.TotalCost += tud.TotalCost
			detail.TotalInput += tud.PromptTokens
			detail.TotalOutput += tud.CompletionTokens
			detail.TotalCalls++
		}

		c.JSON(http.StatusOK, detail)
	}
}

// ============================================================================
// Cost Analysis Helpers
// ============================================================================

// getTokenUsageEventsForSession queries and parses token_usage events for a session
func getTokenUsageEventsForSession(c *gin.Context, db database.Database, sessionID string) ([]tokenUsageData, error) {
	// Get all events for this session, then filter for token_usage
	// We query with a high limit since token_usage events are small and sparse
	dbEvents, err := db.GetEventsBySession(c.Request.Context(), sessionID, 10000, 0)
	if err != nil {
		return nil, err
	}

	var result []tokenUsageData
	for _, dbEvent := range dbEvents {
		if dbEvent.EventType != "token_usage" {
			continue
		}

		tud, err := parseTokenUsageEvent(dbEvent.EventData)
		if err != nil {
			log.Printf("[COSTS] Error parsing token_usage event: %v", err)
			continue
		}
		result = append(result, tud)
	}

	return result, nil
}

// parseTokenUsageEvent parses a token_usage event from the DB event_data JSON
// The event_data is an AgentEvent wrapper with a "data" field containing the TokenUsageEvent fields
func parseTokenUsageEvent(eventData json.RawMessage) (tokenUsageData, error) {
	// AgentEvent wrapper structure
	var wrapper struct {
		Component     string          `json:"component"`
		CorrelationID string          `json:"correlation_id"`
		Data          json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(eventData, &wrapper); err != nil {
		return tokenUsageData{}, fmt.Errorf("failed to parse event wrapper: %w", err)
	}

	var tud tokenUsageData
	if err := json.Unmarshal(wrapper.Data, &tud); err != nil {
		return tokenUsageData{}, fmt.Errorf("failed to parse token usage data: %w", err)
	}

	// Use wrapper-level component if data-level is empty
	if tud.Component == "" && wrapper.Component != "" {
		tud.Component = wrapper.Component
	}

	// Store correlation_id for delegation name resolution
	tud.correlationID = wrapper.CorrelationID

	// Extract cache tokens from generation_info if available
	if tud.GenerationInfo != nil {
		if cacheRead, ok := extractFloat(tud.GenerationInfo, "cache_read_input_tokens"); ok {
			tud.cacheReadTokens = int(cacheRead)
		}
		if cacheWrite, ok := extractFloat(tud.GenerationInfo, "cache_creation_input_tokens"); ok {
			tud.cacheWriteTokens = int(cacheWrite)
		}
	}

	return tud, nil
}

// extractFloat extracts a float64 value from a map[string]interface{}
func extractFloat(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch f := v.(type) {
	case float64:
		return f, true
	case int:
		return float64(f), true
	case json.Number:
		val, err := f.Float64()
		if err != nil {
			return 0, false
		}
		return val, true
	default:
		return 0, false
	}
}

// getSubSessionTokenEvents finds token_usage events from sub-agent sessions
func getSubSessionTokenEvents(c *gin.Context, db database.Database, subSessionPrefix string) []tokenUsageData {
	// Query events that have session_id starting with the sub-session prefix
	// Use the search events API with a session_id filter
	sqlDB := db.GetDB()
	if sqlDB == nil {
		return nil
	}

	// Find chat_sessions whose session_id starts with the prefix
	rows, err := sqlDB.QueryContext(c.Request.Context(),
		`SELECT session_id FROM chat_sessions WHERE session_id LIKE ?`,
		subSessionPrefix+"%",
	)
	if err != nil {
		log.Printf("[COSTS] Error finding sub-sessions: %v", err)
		return nil
	}
	defer rows.Close()

	var result []tokenUsageData
	for rows.Next() {
		var subSessionID string
		if err := rows.Scan(&subSessionID); err != nil {
			continue
		}

		events, err := getTokenUsageEventsForSession(c, db, subSessionID)
		if err != nil {
			log.Printf("[COSTS] Error getting sub-session events for %s: %v", subSessionID, err)
			continue
		}
		result = append(result, events...)
	}

	return result
}

// getOrCreateModelUsage gets or creates a ChatModelUsage entry in a map
func getOrCreateModelUsage(m map[string]*ChatModelUsage, key string) *ChatModelUsage {
	if m[key] == nil {
		m[key] = &ChatModelUsage{}
	}
	return m[key]
}

// accumulateUsage adds token usage data to a ChatModelUsage
func accumulateUsage(usage *ChatModelUsage, tud *tokenUsageData) {
	if usage.Provider == "" {
		usage.Provider = tud.Provider
	}
	usage.InputTokens += tud.PromptTokens
	usage.OutputTokens += tud.CompletionTokens
	usage.ReasoningTokens += tud.ReasoningTokens
	usage.CacheReadTokens += tud.cacheReadTokens
	usage.CacheWriteTokens += tud.cacheWriteTokens
	usage.LLMCallCount++
	usage.InputCost += tud.InputCost
	usage.OutputCost += tud.OutputCost
	usage.ReasoningCost += tud.ReasoningCost
	usage.CacheCost += tud.CacheCost
	usage.TotalCost += tud.TotalCost

	// Keep the latest context window values
	if tud.ContextWindowUsage > usage.ContextWindowUsage {
		usage.ContextWindowUsage = tud.ContextWindowUsage
	}
	if tud.ModelContextWindow > usage.ModelContextWindow {
		usage.ModelContextWindow = tud.ModelContextWindow
	}
	if tud.ContextUsagePercent > usage.ContextUsagePercent {
		usage.ContextUsagePercent = tud.ContextUsagePercent
	}
}

// replaceUsage overwrites model usage with the latest cumulative values from a token_usage event.
// Used for the main agent, which emits CUMULATIVE token_usage events (one per user turn).
// Each event contains the running total, so we REPLACE to avoid double-counting.
func replaceUsage(usage *ChatModelUsage, tud *tokenUsageData) {
	if usage.Provider == "" {
		usage.Provider = tud.Provider
	}
	usage.InputTokens = tud.PromptTokens
	usage.OutputTokens = tud.CompletionTokens
	usage.ReasoningTokens = tud.ReasoningTokens
	usage.CacheReadTokens = tud.cacheReadTokens
	usage.CacheWriteTokens = tud.cacheWriteTokens
	usage.LLMCallCount++ // Intentionally increment: counts how many turns/events were processed
	usage.InputCost = tud.InputCost
	usage.OutputCost = tud.OutputCost
	usage.ReasoningCost = tud.ReasoningCost
	usage.CacheCost = tud.CacheCost
	usage.TotalCost = tud.TotalCost

	// Keep the latest context window values
	if tud.ContextWindowUsage > usage.ContextWindowUsage {
		usage.ContextWindowUsage = tud.ContextWindowUsage
	}
	if tud.ModelContextWindow > usage.ModelContextWindow {
		usage.ModelContextWindow = tud.ModelContextWindow
	}
	if tud.ContextUsagePercent > usage.ContextUsagePercent {
		usage.ContextUsagePercent = tud.ContextUsagePercent
	}
}

// mergeUsage merges one ChatModelUsage into another
func mergeUsage(dst, src *ChatModelUsage) {
	if dst.Provider == "" {
		dst.Provider = src.Provider
	}
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.ReasoningTokens += src.ReasoningTokens
	dst.CacheTokens += src.CacheTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
	dst.LLMCallCount += src.LLMCallCount
	dst.InputCost += src.InputCost
	dst.OutputCost += src.OutputCost
	dst.ReasoningCost += src.ReasoningCost
	dst.CacheCost += src.CacheCost
	dst.TotalCost += src.TotalCost
}

// ============================================================================
// Delegation Logs Types & Handlers (Multi-Agent Mode)
// ============================================================================

// DelegationLogEntry represents a single delegation with its costs
type DelegationLogEntry struct {
	DelegationID      string                     `json:"delegation_id"`
	SessionID         string                     `json:"session_id,omitempty"`
	Instruction       string                     `json:"instruction"`
	ReasoningLevel    string                     `json:"reasoning_level,omitempty"`
	ModelID           string                     `json:"model_id,omitempty"`
	ToolMode          string                     `json:"tool_mode,omitempty"`
	Servers           []string                   `json:"servers,omitempty"`
	BackgroundAgentID string                     `json:"background_agent_id,omitempty"`
	Depth             int                        `json:"depth"`
	Status            string                     `json:"status"` // running, completed, failed
	StartTime         string                     `json:"start_time"`
	EndTime           string                     `json:"end_time,omitempty"`
	Duration          string                     `json:"duration,omitempty"`
	Result            string                     `json:"result,omitempty"`
	Error             string                     `json:"error,omitempty"`
	InputTokens       int64                      `json:"input_tokens"`
	OutputTokens      int64                      `json:"output_tokens"`
	ToolCalls         int64                      `json:"tool_calls"`
	TokenUsage        map[string]*ChatModelUsage `json:"token_usage,omitempty"`
	TotalCostUSD      float64                    `json:"total_cost_usd"`
}

// DelegationLogsResponse is the response for GET /sessions/:id/delegation-logs
type DelegationLogsResponse struct {
	Delegations []DelegationLogEntry       `json:"delegations"`
	TotalCost   float64                    `json:"total_cost_usd"`
	TotalInput  int64                      `json:"total_input_tokens"`
	TotalOutput int64                      `json:"total_output_tokens"`
	TotalCalls  int64                      `json:"total_llm_calls"`
	ByModel     map[string]*ChatModelUsage `json:"by_model"`
}

// AgentCostSummary holds cost/token info for one agent (main or sub-agent)
type AgentCostSummary struct {
	Name         string                     `json:"name"`
	InputTokens  int64                      `json:"input_tokens"`
	OutputTokens int64                      `json:"output_tokens"`
	TotalCostUSD float64                    `json:"total_cost_usd"`
	LLMCalls     int64                      `json:"llm_calls"`
	ByModel      map[string]*ChatModelUsage `json:"by_model"`
}

// SessionDelegationLogs groups delegation logs + costs per session
type SessionDelegationLogs struct {
	SessionID    string                     `json:"session_id"`
	Title        string                     `json:"title"`
	CreatedAt    time.Time                  `json:"created_at"`
	Status       string                     `json:"status"`
	TotalCost    float64                    `json:"total_cost_usd"`
	TotalInput   int64                      `json:"total_input_tokens"`
	TotalOutput  int64                      `json:"total_output_tokens"`
	TotalCalls   int64                      `json:"total_llm_calls"`
	MainAgent    AgentCostSummary           `json:"main_agent"`
	Delegations  []DelegationLogEntry       `json:"delegations"`
	ByModel      map[string]*ChatModelUsage `json:"by_model"`
}

// AllDelegationLogsResponse is the response for GET /delegation-logs (all sessions)
type AllDelegationLogsResponse struct {
	Sessions    []SessionDelegationLogs    `json:"sessions"`
	TotalCost   float64                    `json:"total_cost_usd"`
	TotalInput  int64                      `json:"total_input_tokens"`
	TotalOutput int64                      `json:"total_output_tokens"`
	TotalCalls  int64                      `json:"total_llm_calls"`
	ByModel     map[string]*ChatModelUsage `json:"by_model"`
}

// getDelegationLogs returns delegation log entries with costs for a session
func getDelegationLogs(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
			return
		}

		// Collect all session IDs (main + sub-agent sessions) then fetch
		// only delegation-related events in a single query.
		sqlDB := db.GetDB()
		if sqlDB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database connection unavailable"})
			return
		}

		// Resolve main session's internal ID
		var mainInternalID string
		err := sqlDB.QueryRowContext(c.Request.Context(),
			`SELECT id FROM chat_sessions WHERE session_id = ?`, sessionID,
		).Scan(&mainInternalID)
		if err != nil {
			mainInternalID = sessionID
		}

		// Collect all session IDs: main + sub-agent sessions
		sessionIDs := []interface{}{mainInternalID}
		subSessionPrefix := sessionID + "-sub-"
		subRows, err := sqlDB.QueryContext(c.Request.Context(),
			`SELECT id, session_id FROM chat_sessions WHERE session_id LIKE ?`,
			subSessionPrefix+"%",
		)
		if err == nil {
			defer subRows.Close()
			for subRows.Next() {
				var subID, subSessionID string
				if err := subRows.Scan(&subID, &subSessionID); err != nil {
					continue
				}
				sessionIDs = append(sessionIDs, subID)
			}
		}

		// Build a single query with IN clause + event_type filter
		// This replaces N+1 queries with 1 query, and filters at DB level
		placeholders := make([]string, len(sessionIDs))
		for i := range sessionIDs {
			placeholders[i] = "?"
		}
		query := fmt.Sprintf(`
			SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
			FROM events
			WHERE chat_session_id IN (%s)
			  AND event_type IN ('delegation_start', 'delegation_end', 'token_usage')
			ORDER BY timestamp ASC
		`, strings.Join(placeholders, ","))

		rows, err := sqlDB.QueryContext(c.Request.Context(), query, sessionIDs...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get delegation events: %v", err)})
			return
		}
		defer rows.Close()

		var allEvents []database.Event
		for rows.Next() {
			var event database.Event
			var eventDataJSON string
			if err := rows.Scan(&event.ID, &event.SessionID, &event.ChatSessionID, &event.EventType, &event.Timestamp, &eventDataJSON); err != nil {
				continue
			}
			if err := json.Unmarshal([]byte(eventDataJSON), &event.EventData); err != nil {
				continue
			}
			allEvents = append(allEvents, event)
		}

		// Build delegation entries from start/end events and aggregate token_usage
		delegationMap := make(map[string]*DelegationLogEntry)
		var delegationOrder []string

		for _, dbEvent := range allEvents {
			// Parse event_data wrapper to get correlation_id and data
			var wrapper struct {
				CorrelationID string          `json:"correlation_id"`
				Timestamp     time.Time       `json:"timestamp"`
				Data          json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(dbEvent.EventData, &wrapper); err != nil {
				continue
			}

			switch dbEvent.EventType {
			case "delegation_start":
				var startData struct {
					DelegationID      string   `json:"delegation_id"`
					Depth             int      `json:"depth"`
					Instruction       string   `json:"instruction"`
					ReasoningLevel    string   `json:"reasoning_level"`
					ModelID           string   `json:"model_id"`
					ToolMode          string   `json:"tool_mode"`
					Servers           []string `json:"servers"`
					BackgroundAgentID string   `json:"background_agent_id"`
					Timestamp         string   `json:"timestamp"`
				}
				if err := json.Unmarshal(wrapper.Data, &startData); err != nil {
					continue
				}

				entry := &DelegationLogEntry{
					DelegationID:      startData.DelegationID,
					Instruction:       startData.Instruction,
					ReasoningLevel:    startData.ReasoningLevel,
					ModelID:           startData.ModelID,
					ToolMode:          startData.ToolMode,
					Servers:           startData.Servers,
					BackgroundAgentID: startData.BackgroundAgentID,
					Depth:             startData.Depth,
					Status:            "running",
					StartTime:         startData.Timestamp,
					TokenUsage:        make(map[string]*ChatModelUsage),
				}
				delegationMap[startData.DelegationID] = entry
				delegationOrder = append(delegationOrder, startData.DelegationID)

			case "delegation_end":
				var endData struct {
					DelegationID string `json:"delegation_id"`
					Result       string `json:"result"`
					Error        string `json:"error"`
					Success      bool   `json:"success"`
					Timestamp    string `json:"timestamp"`
					InputTokens  int64  `json:"input_tokens"`
					OutputTokens int64  `json:"output_tokens"`
					ToolCalls    int64  `json:"tool_calls"`
					Duration     string  `json:"duration"`
				TotalCostUSD float64 `json:"total_cost_usd"`
				}
				if err := json.Unmarshal(wrapper.Data, &endData); err != nil {
					continue
				}

				entry, ok := delegationMap[endData.DelegationID]
				if !ok {
					continue
				}

				if endData.Success {
					entry.Status = "completed"
				} else {
					entry.Status = "failed"
				}
				entry.EndTime = endData.Timestamp
				entry.Duration = endData.Duration
				entry.Result = endData.Result
				entry.Error = endData.Error
				entry.InputTokens = endData.InputTokens
				entry.OutputTokens = endData.OutputTokens
				entry.ToolCalls = endData.ToolCalls
				// Use cost from delegation_end if token_usage events didn't provide it
				if endData.TotalCostUSD > 0 {
					entry.TotalCostUSD += endData.TotalCostUSD
				}

			case "token_usage":
				// Aggregate token_usage events per delegation using correlation_id
				correlationID := wrapper.CorrelationID
				if correlationID == "" {
					continue
				}

				entry, ok := delegationMap[correlationID]
				if !ok {
					continue
				}

				tud, err := parseTokenUsageEvent(dbEvent.EventData)
				if err != nil {
					continue
				}

				modelUsage := getOrCreateModelUsage(entry.TokenUsage, tud.ModelID)
				accumulateUsage(modelUsage, &tud)
				entry.TotalCostUSD += tud.TotalCost
			}
		}

		// Build ordered response
		response := DelegationLogsResponse{
			Delegations: make([]DelegationLogEntry, 0, len(delegationOrder)),
			ByModel:     make(map[string]*ChatModelUsage),
		}

		for _, id := range delegationOrder {
			entry := delegationMap[id]
			response.Delegations = append(response.Delegations, *entry)
			response.TotalCost += entry.TotalCostUSD
			response.TotalInput += entry.InputTokens
			response.TotalOutput += entry.OutputTokens
			response.TotalCalls++

			// Merge per-model usage into aggregate
			for modelID, usage := range entry.TokenUsage {
				aggModel := getOrCreateModelUsage(response.ByModel, modelID)
				mergeUsage(aggModel, usage)
			}
		}

		c.JSON(http.StatusOK, response)
	}
}

// getDelegationEvents returns events for a specific delegation (drill-down)
func getDelegationEvents(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		delegationID := c.Param("delegation_id")

		if sessionID == "" || delegationID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id and delegation_id are required"})
			return
		}

		limitStr := c.DefaultQuery("limit", "500")
		offsetStr := c.DefaultQuery("offset", "0")

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 500
		}
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			offset = 0
		}

		// Query events with matching correlation_id
		dbEvents, err := db.GetEventsByCorrelationID(c.Request.Context(), sessionID, delegationID, limit, offset)
		if err != nil {
			// Also try sub-agent sessions
			subSessionID := sessionID + "-sub-" + delegationID
			dbEvents, err = db.GetEventsBySession(c.Request.Context(), subSessionID, limit, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get delegation events: %v", err)})
				return
			}
		}

		// Return raw DB events (event_data is already JSON)
		type rawEvent struct {
			ID        string          `json:"id"`
			Type      string          `json:"type"`
			Timestamp time.Time       `json:"timestamp"`
			SessionID string          `json:"session_id"`
			Data      json.RawMessage `json:"data"`
		}

		convertedEvents := make([]rawEvent, 0, len(dbEvents))
		for _, dbEvent := range dbEvents {
			convertedEvents = append(convertedEvents, rawEvent{
				ID:        dbEvent.ID,
				Type:      dbEvent.EventType,
				Timestamp: dbEvent.Timestamp,
				SessionID: sessionID,
				Data:      dbEvent.EventData,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"events": convertedEvents,
			"total":  len(convertedEvents),
		})
	}
}
