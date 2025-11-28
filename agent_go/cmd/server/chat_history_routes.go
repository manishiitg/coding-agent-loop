package server

import (
	"net/http"
	"strconv"
	"time"

	"mcp-agent/agent_go/pkg/database"
	"mcpagent/events"

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

		events, err := db.GetEventsBySession(c.Request.Context(), sessionID, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"events": events,
			"total":  len(events),
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
			filter.EventType = events.EventType(eventType)
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
