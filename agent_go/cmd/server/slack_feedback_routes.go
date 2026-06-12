package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"mcp-agent-builder-go/agent_go/cmd/server/services"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/chathistory"

	"github.com/gorilla/mux"
)

// ChannelRoute maps a Slack channel to a specific workflow, including the workspace path
// needed so the bot can read the workflow manifest (e.g. workshop_mode) without scanning all workspaces.
type ChannelRoute = services.ChannelRoute

// SlackConfigRequest represents a request to update Slack config (Socket Mode only)
type SlackConfigRequest struct {
	Enabled        bool                    `json:"enabled"`
	BotToken       string                  `json:"bot_token"` // Bot User OAuth Token (xoxb-...)
	AppToken       string                  `json:"app_token"` // App-level token (xapp-...) for Socket Mode
	ChannelID      string                  `json:"channel_id"`
	BotMode        bool                    `json:"bot_mode"`        // Enable @mention bot mode (starts agent sessions from Slack)
	ChannelRouting map[string]ChannelRoute `json:"channel_routing"` // Maps Slack channel IDs to ChannelRoute{workflow_id, workspace_path}
}

// SlackConfigResponse represents the Slack configuration response
type SlackConfigResponse struct {
	Enabled        bool                    `json:"enabled"`
	BotToken       string                  `json:"bot_token,omitempty"`  // Masked in GET
	AppToken       string                  `json:"app_token,omitempty"`  // Masked in GET
	ChannelID      string                  `json:"channel_id,omitempty"`
	BotMode        bool                    `json:"bot_mode"`
	ChannelRouting map[string]ChannelRoute `json:"channel_routing,omitempty"` // Maps Slack channel IDs to ChannelRoute{workflow_id, workspace_path}
}

// SlackTestResponse represents test connection response
type SlackTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	TestID  string `json:"test_id,omitempty"` // Unique ID for polling test replies
}

// Webhook types removed - using Socket Mode for real-time events

// SlackFeedbackRoutes sets up Slack feedback API routes
func SlackFeedbackRoutes(router *mux.Router, api *StreamingAPI) {
	apiRouter := router.PathPrefix("/api/human-feedback/slack").Subrouter()

	// Configuration routes
	apiRouter.HandleFunc("/config", getSlackConfigHandler(api)).Methods("GET")
	apiRouter.HandleFunc("/config", updateSlackConfigHandler(api)).Methods("POST", "OPTIONS")

	// Test connection
	apiRouter.HandleFunc("/test", testSlackConnectionHandler(api)).Methods("POST", "OPTIONS")

	// Get test connection reply (for polling)
	apiRouter.HandleFunc("/test/reply", getTestConnectionReplyHandler(api)).Methods("GET", "OPTIONS")
	// Note: Using Socket Mode for real-time events - no webhook endpoint needed
}

// ensureSlackService returns the global Slack service, initializing it lazily
// on first use. The service reads its config from the filesystem now, so no
// database handle is required.
func ensureSlackService() (*services.SlackService, error) {
	slackService := services.GetSlackService()
	if slackService != nil {
		return slackService, nil
	}
	svc, err := services.InitSlackService()
	if err != nil {
		return nil, err
	}
	return svc, nil
}

// getSlackConfigHandler retrieves current Slack configuration
func getSlackConfigHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slackService, err := ensureSlackService()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize Slack service: %v", err), http.StatusInternalServerError)
			return
		}

		config := slackService.GetConfig()

		// Check bot_mode and channel routing from the filesystem-backed bot connector config.
		botMode := false
		var channelRouting map[string]ChannelRoute
		botCfg, _ := api.chatStore.GetBotConnectorConfig(r.Context(), "slack")
		if botCfg != nil {
			botMode = botCfg.BotMode
			if botCfg.AllowedChannels != "" && botCfg.AllowedChannels != "[]" && botCfg.AllowedChannels != "{}" {
				_ = json.Unmarshal([]byte(botCfg.AllowedChannels), &channelRouting)
			}
		}

		resp := SlackConfigResponse{
			Enabled:        config.Enabled,
			BotToken:       config.BotToken,
			AppToken:       config.AppToken,
			ChannelID:      config.ChannelID,
			BotMode:        botMode,
			ChannelRouting: channelRouting,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// updateSlackConfigHandler creates/updates Slack configuration
func updateSlackConfigHandler(api *StreamingAPI) http.HandlerFunc {
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

		slackService, err := ensureSlackService()
		if err != nil {
			log.Printf("[SLACK] Failed to initialize service: %v", err)
			http.Error(w, fmt.Sprintf("failed to initialize Slack service: %v", err), http.StatusInternalServerError)
			return
		}

		config := &services.SlackConfig{
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

		// Marshal channel routing into AllowedChannels JSON.
		allowedChannelsJSON := ""
		if len(req.ChannelRouting) > 0 {
			if data, err := json.Marshal(req.ChannelRouting); err == nil {
				allowedChannelsJSON = string(data)
			}
		}

		// Save bot_mode and channel routing to the filesystem-backed bot connector config.
		if _, err := api.chatStore.UpsertBotConnectorConfig(r.Context(), &chathistory.CreateBotConnectorConfigRequest{
			ID:              "slack",
			Enabled:         req.Enabled,
			BotMode:         req.BotMode,
			AllowedChannels: allowedChannelsJSON,
		}); err != nil {
			log.Printf("[SLACK] Failed to save bot config: %v", err)
			// Non-fatal — Slack config itself was saved
		}

		// Dynamically register/unregister Slack bot connector
		if api.botManager != nil {
			if req.BotMode && req.Enabled {
				// Register if not already registered
				if api.botManager.GetConnector("slack") == nil {
					api.botManager.RegisterConnector(slackService)
					slackService.StartListening(r.Context())
					log.Printf("[SLACK] Bot mode enabled — registered with bot manager")
				}
			}
			// Note: unregistering at runtime is complex (active sessions) — disable takes effect on restart
		}

		response := SlackConfigResponse{
			Enabled:        config.Enabled,
			ChannelID:      config.ChannelID,
			BotMode:        req.BotMode,
			ChannelRouting: req.ChannelRouting,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// testSlackConnectionHandler tests Slack connection
// Accepts optional config in request body to test without saving
func testSlackConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		slackService, err := ensureSlackService()
		if err != nil {
			response := SlackTestResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to initialize Slack service: %v", err),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
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
		if testConfig != nil {
			testUniqueID, err = slackService.TestConnectionWithConfig(r.Context(), &services.SlackConfig{
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
func getTestConnectionReplyHandler(api *StreamingAPI) http.HandlerFunc {
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
