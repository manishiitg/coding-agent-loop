package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	slackservice "mcp-agent-builder-go/agent_go/cmd/server/services"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/database"

	"github.com/gorilla/mux"
)

// SlackConfigRequest represents a request to update Slack config (Socket Mode only)
type SlackConfigRequest struct {
	Enabled   bool   `json:"enabled"`
	BotToken  string `json:"bot_token"` // Bot User OAuth Token (xoxb-...)
	AppToken  string `json:"app_token"` // App-level token (xapp-...) for Socket Mode
	ChannelID string `json:"channel_id"`
}

// SlackConfigResponse represents the Slack configuration response
type SlackConfigResponse struct {
	Enabled   bool   `json:"enabled"`
	BotToken  string `json:"bot_token,omitempty"` // Masked in GET
	AppToken  string `json:"app_token,omitempty"` // Masked in GET
	ChannelID string `json:"channel_id,omitempty"`
}

// SlackTestResponse represents test connection response
type SlackTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	TestID  string `json:"test_id,omitempty"` // Unique ID for polling test replies
}

// Webhook types removed - using Socket Mode for real-time events

// SlackFeedbackRoutes sets up Slack feedback API routes
func SlackFeedbackRoutes(router *mux.Router, api *StreamingAPI, db database.Database) {
	apiRouter := router.PathPrefix("/api/human-feedback/slack").Subrouter()

	// Configuration routes
	apiRouter.HandleFunc("/config", getSlackConfigHandler(api, db)).Methods("GET")
	apiRouter.HandleFunc("/config", updateSlackConfigHandler(api, db)).Methods("POST", "OPTIONS")

	// Test connection
	apiRouter.HandleFunc("/test", testSlackConnectionHandler(api, db)).Methods("POST", "OPTIONS")

	// Get test connection reply (for polling)
	apiRouter.HandleFunc("/test/reply", getTestConnectionReplyHandler(api, db)).Methods("GET", "OPTIONS")
	// Note: Using Socket Mode for real-time events - no webhook endpoint needed
}

// getSlackConfigHandler retrieves current Slack configuration
func getSlackConfigHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slackService := slackservice.GetSlackService()
		if slackService == nil {
			// Initialize if not already initialized
			sqliteDB, ok := db.(*database.SQLiteDB)
			if !ok {
				http.Error(w, "database type not supported", http.StatusInternalServerError)
				return
			}
			var err error
			slackService, err = slackservice.InitSlackService(sqliteDB.GetDB())
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to initialize Slack service: %v", err), http.StatusInternalServerError)
				return
			}
		}

		config := slackService.GetConfig()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
	}
}

// updateSlackConfigHandler creates/updates Slack configuration
func updateSlackConfigHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req SlackConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[SLACK] Failed to decode request: %v", err)
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		slackService := slackservice.GetSlackService()
		if slackService == nil {
			// Initialize if not already initialized
			sqliteDB, ok := db.(*database.SQLiteDB)
			if !ok {
				http.Error(w, "database type not supported", http.StatusInternalServerError)
				return
			}
			var err error
			slackService, err = slackservice.InitSlackService(sqliteDB.GetDB())
			if err != nil {
				log.Printf("[SLACK] Failed to initialize service: %v", err)
				http.Error(w, fmt.Sprintf("failed to initialize Slack service: %v", err), http.StatusInternalServerError)
				return
			}
		}

		config := &slackservice.SlackConfig{
			Enabled:   req.Enabled,
			BotToken:  req.BotToken,
			AppToken:  req.AppToken,
			ChannelID: req.ChannelID,
		}

		if err := slackService.SaveConfig(r.Context(), config); err != nil {
			log.Printf("[SLACK] SaveConfig failed: %v", err)
			http.Error(w, fmt.Sprintf("failed to save config: %v", err), http.StatusInternalServerError)
			return
		}

		response := SlackConfigResponse{
			Enabled:   config.Enabled,
			ChannelID: config.ChannelID,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// testSlackConnectionHandler tests Slack connection
// Accepts optional config in request body to test without saving
func testSlackConnectionHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		slackService := slackservice.GetSlackService()
		if slackService == nil {
			// Initialize if not already initialized
			sqliteDB, ok := db.(*database.SQLiteDB)
			if !ok {
				http.Error(w, "database type not supported", http.StatusInternalServerError)
				return
			}
			var err error
			slackService, err = slackservice.InitSlackService(sqliteDB.GetDB())
			if err != nil {
				response := SlackTestResponse{
					Success: false,
					Message: fmt.Sprintf("Failed to initialize Slack service: %v", err),
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
				return
			}
		}

		// Check if config is provided in request body (for testing without saving)
		var testConfig *SlackConfigRequest
		if r.ContentLength > 0 {
			var req SlackConfigRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				// Config provided - use it for testing without saving
				testConfig = &req
			}
		}

		// If config provided, test with it directly; otherwise use saved config
		var testUniqueID string
		var err error
		if testConfig != nil {
			testUniqueID, err = slackService.TestConnectionWithConfig(r.Context(), &slackservice.SlackConfig{
				Enabled:   testConfig.Enabled,
				BotToken:  testConfig.BotToken,
				AppToken:  testConfig.AppToken,
				ChannelID: testConfig.ChannelID,
			})
		} else {
			// TestConnection will reload config internally
			err = slackService.TestConnection(r.Context())
			// For saved config tests, we can't get the test ID easily, so leave it empty
			testUniqueID = ""
		}

		if err != nil {
			response := SlackTestResponse{
				Success: false,
				Message: fmt.Sprintf("Connection test failed: %v", err),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		response := SlackTestResponse{
			Success: true,
			Message: "Slack connection test successful! A test message has been sent to your Slack channel. Reply to it in a thread to test Socket Mode.",
			TestID:  testUniqueID,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getTestConnectionReplyHandler checks if a reply was received for a test connection
func getTestConnectionReplyHandler(api *StreamingAPI, db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		testUniqueID := r.URL.Query().Get("test_id")
		if testUniqueID == "" {
			http.Error(w, "test_id parameter is required", http.StatusBadRequest)
			return
		}

		feedbackStore := virtualtools.GetHumanFeedbackStore()
		if feedbackStore == nil {
			log.Printf("[SLACK_TEST] ❌ Human feedback store not initialized")
			http.Error(w, "human feedback store not initialized", http.StatusInternalServerError)
			return
		}

		// Check if there's a response for this test connection
		response, exists := feedbackStore.GetResponse(testUniqueID)
		if !exists {
			// Return 204 No Content if no reply yet
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Return the reply
		responseData := map[string]interface{}{
			"test_id":  testUniqueID,
			"reply":    response,
			"received": true,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responseData)
	}
}

// Webhook handler removed - using Socket Mode for real-time events
