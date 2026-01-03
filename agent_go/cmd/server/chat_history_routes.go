package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/pkg/database"
	pkgevents "mcpagent/events"

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

// createChatSession creates a new chat session
func createChatSession(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req database.CreateChatSessionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		session, err := db.CreateChatSession(c.Request.Context(), &req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, session)
	}
}

// listChatSessions lists all chat sessions with pagination
func listChatSessions(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		limitStr := c.DefaultQuery("limit", "20")
		offsetStr := c.DefaultQuery("offset", "0")
		presetQueryID := c.Query("preset_query_id")

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

		sessions, total, err := db.ListChatSessions(c.Request.Context(), limit, offset, presetQueryIDPtr)
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

// getChatSession gets a specific chat session
func getChatSession(db database.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required"})
			return
		}

		session, err := db.GetChatSession(c.Request.Context(), sessionID)
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

		dbEvents, err := db.GetEventsBySession(c.Request.Context(), sessionID, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[CHAT_HISTORY] Loading events for session %s: found %d events", sessionID, len(dbEvents))

		// Convert database events to polling events format (same structure as polling API)
		convertedEvents := make([]events.Event, 0, len(dbEvents))
		parseErrors := 0
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

			// Unmarshal Data field into a map to preserve structure
			var dataMap map[string]interface{}
			if err := json.Unmarshal(helper.Data, &dataMap); err != nil {
				parseErrors++
				if i < 3 {
					log.Printf("[CHAT_HISTORY ERROR] Failed to parse event data %d for session %s: %v, event_type=%s", i, sessionID, err, dbEvent.EventType)
				}
				continue
			}

			// Log first event structure for debugging
			if i == 0 {
				log.Printf("[CHAT_HISTORY DEBUG] First event structure: type=%s, hasData=%v, dataKeys=%v",
					helper.Type, len(dataMap) > 0, func() []string {
						keys := make([]string, 0, len(dataMap))
						for k := range dataMap {
							keys = append(keys, k)
						}
						return keys
					}())
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

		log.Printf("[CHAT_HISTORY] Converted %d events: total=%d, converted=%d, parse_errors=%d", len(dbEvents), len(dbEvents), len(convertedEvents), parseErrors)

		// Get total count of events for this session
		totalCount, err := db.GetEventsBySession(c.Request.Context(), sessionID, 0, 0)
		total := len(dbEvents)
		if err == nil && len(totalCount) > 0 {
			// If we got events, count them (this is a workaround since GetEventsBySession doesn't return total)
			// For now, if limit is 0, we can use it to get all events and count them
			// But this is inefficient - we should add a CountEventsBySession method
			if limit == 0 || len(dbEvents) == limit {
				// Try to get a count by fetching with a very high limit
				allEvents, err := db.GetEventsBySession(c.Request.Context(), sessionID, 1000000, 0)
				if err == nil {
					total = len(allEvents)
				}
			} else {
				// Estimate: if we got fewer events than limit, that's the total
				// Otherwise, we don't know the exact total
				if len(dbEvents) < limit {
					total = offset + len(dbEvents)
				}
			}
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
