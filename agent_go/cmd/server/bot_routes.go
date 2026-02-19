package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	slackservice "mcp-agent-builder-go/agent_go/cmd/server/services"
	"mcp-agent-builder-go/agent_go/pkg/database"

	"github.com/gorilla/mux"
)

// BotRoutes sets up the bot connector API routes
func BotRoutes(router *mux.Router, api *StreamingAPI, db database.Database) {
	botRouter := router.PathPrefix("/api/bot").Subrouter()

	// Connector config routes
	botRouter.HandleFunc("/connectors", listBotConnectorsHandler(api, db)).Methods("GET")
	botRouter.HandleFunc("/connectors/{platform}", getBotConnectorHandler(api, db)).Methods("GET")
	botRouter.HandleFunc("/connectors/{platform}", saveBotConnectorHandler(api, db)).Methods("POST", "OPTIONS")
	botRouter.HandleFunc("/connectors/{platform}/test", testBotConnectorHandler(api, db)).Methods("POST", "OPTIONS")

	// Bot session routes
	botRouter.HandleFunc("/sessions", listBotSessionsHandler(api, db)).Methods("GET")
	botRouter.HandleFunc("/sessions/{id}", getBotSessionHandler(api, db)).Methods("GET")
	botRouter.HandleFunc("/sessions/{id}/stop", stopBotSessionHandler(api, db)).Methods("POST", "OPTIONS")
	botRouter.HandleFunc("/sessions/{id}/messages", listBotMessagesHandler(api, db)).Methods("GET")
}

// --- Connector config handlers ---

func listBotConnectorsHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configs, err := db.ListBotConnectorConfigs(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Add live status from registered connectors
		type connectorStatus struct {
			database.BotConnectorConfig
			Connected bool `json:"connected"`
		}

		var result []connectorStatus
		for _, cfg := range configs {
			cs := connectorStatus{BotConnectorConfig: cfg}
			if api.botManager != nil {
				connector := api.botManager.GetConnector(cfg.ID)
				if connector != nil {
					cs.Connected = connector.IsEnabled()
				}
			}
			result = append(result, cs)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func getBotConnectorHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		platform := mux.Vars(r)["platform"]

		cfg, err := db.GetBotConnectorConfig(r.Context(), platform)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)
	}
}

func saveBotConnectorHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		platform := mux.Vars(r)["platform"]

		var req database.CreateBotConnectorConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		req.ID = platform

		cfg, err := db.UpsertBotConnectorConfig(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[BOT_ROUTES] Saved connector config for %s: enabled=%v bot_mode=%v", platform, cfg.Enabled, cfg.BotMode)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)
	}
}

func testBotConnectorHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		platform := mux.Vars(r)["platform"]

		if api.botManager == nil {
			http.Error(w, "Bot manager not initialized", http.StatusServiceUnavailable)
			return
		}

		connector := api.botManager.GetConnector(platform)
		if connector == nil {
			http.Error(w, "Connector not found: "+platform, http.StatusNotFound)
			return
		}

		if !connector.IsEnabled() {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Connector is not enabled or not connected",
			})
			return
		}

		// Test by sending a message to the configured channel
		slackSvc := slackservice.GetSlackService()
		if slackSvc != nil && platform == "slack" {
			err := slackSvc.TestConnection(r.Context())
			if err != nil {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": err.Error(),
				})
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Connection test successful",
		})
	}
}

// --- Bot session handlers ---

func listBotSessionsHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 20
		offset := 0
		status := r.URL.Query().Get("status")

		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil {
				limit = parsed
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if parsed, err := strconv.Atoi(o); err == nil {
				offset = parsed
			}
		}

		sessions, total, err := db.ListBotSessions(r.Context(), limit, offset, status)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessions": sessions,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	}
}

func getBotSessionHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]

		session, err := db.GetBotSession(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	}
}

func stopBotSessionHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		session, err := db.GetBotSession(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		if session.Status != database.BotSessionStatusRunning && session.Status != database.BotSessionStatusAwaitingPlanApproval {
			http.Error(w, "Session is not active", http.StatusBadRequest)
			return
		}

		err = db.CompleteBotSession(r.Context(), id, database.BotSessionStatusFailed)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Session stopped",
		})
	}
}

func listBotMessagesHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		limit := 100
		offset := 0

		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil {
				limit = parsed
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if parsed, err := strconv.Atoi(o); err == nil {
				offset = parsed
			}
		}

		messages, total, err := db.ListBotMessages(r.Context(), id, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": messages,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	}
}
