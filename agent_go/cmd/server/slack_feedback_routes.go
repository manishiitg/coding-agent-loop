package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"

	"github.com/gorilla/mux"
)

// ChannelRoute maps a Slack @slug to a specific workflow, including the workspace
// path needed so the bot can read the workflow manifest without scanning all
// workspaces during execution.
type ChannelRoute = services.ChannelRoute

// SlackConfigRequest represents a request to update Slack config (Socket Mode only)
type SlackConfigRequest struct {
	Enabled        bool                    `json:"enabled"`
	BotToken       string                  `json:"bot_token"` // Bot User OAuth Token (xoxb-...)
	AppToken       string                  `json:"app_token"` // App-level token (xapp-...) for Socket Mode
	ChannelID      string                  `json:"channel_id"`
	BotMode        bool                    `json:"bot_mode"`        // Enable @mention bot mode (starts agent sessions from Slack)
	ChannelRouting map[string]ChannelRoute `json:"channel_routing"` // Historical name; now maps Slack @slugs to ChannelRoute.
}

// SlackConfigResponse represents the Slack configuration response
type SlackConfigResponse struct {
	Enabled        bool                    `json:"enabled"`
	BotToken       string                  `json:"bot_token,omitempty"` // Masked in GET
	AppToken       string                  `json:"app_token,omitempty"` // Masked in GET
	ChannelID      string                  `json:"channel_id,omitempty"`
	BotMode        bool                    `json:"bot_mode"`
	ChannelRouting map[string]ChannelRoute `json:"channel_routing,omitempty"` // Historical name; now maps Slack @slugs to ChannelRoute.
}

// SlackTestResponse represents test connection response
type SlackTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	TestID  string `json:"test_id,omitempty"` // Unique ID for polling test replies
}

func joinSlackSlugLog(slugs []string) string {
	if len(slugs) == 0 {
		return ""
	}
	return strings.Join(slugs, ", @")
}

func slackAutoRoutedWorkflowSet(configJSON string) (map[string]bool, map[string]json.RawMessage) {
	raw := map[string]json.RawMessage{}
	if strings.TrimSpace(configJSON) != "" {
		_ = json.Unmarshal([]byte(configJSON), &raw)
	}
	var ids []string
	if data, ok := raw["auto_routed_workflows"]; ok {
		_ = json.Unmarshal(data, &ids)
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			set[id] = true
		}
	}
	return set, raw
}

func slackConfigJSONWithAutoRouted(raw map[string]json.RawMessage, set map[string]bool) string {
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	sort.Strings(ids)
	data, _ := json.Marshal(ids)
	raw["auto_routed_workflows"] = data
	out, err := json.Marshal(raw)
	if err != nil {
		return "{}"
	}
	return string(out)
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
		log.Printf("[SLACK_FLOW] api config GET: begin")
		slackService, err := ensureSlackService()
		if err != nil {
			log.Printf("[SLACK_FLOW] api config GET: ensure service failed: %v", err)
			http.Error(w, fmt.Sprintf("failed to initialize Slack service: %v", err), http.StatusInternalServerError)
			return
		}

		config := slackService.GetConfig()

		// Check bot_mode and Slack slug routing from the filesystem-backed bot connector config.
		botMode := false
		var channelRouting map[string]ChannelRoute
		autoRouted := map[string]bool{}
		configJSONRaw := map[string]json.RawMessage{}
		botCfg, _ := api.chatStore.GetBotConnectorConfig(r.Context(), "slack")
		if botCfg != nil {
			botMode = botCfg.BotMode
			if botCfg.AllowedChannels != "" && botCfg.AllowedChannels != "[]" && botCfg.AllowedChannels != "{}" {
				_ = json.Unmarshal([]byte(botCfg.AllowedChannels), &channelRouting)
			}
			autoRouted, configJSONRaw = slackAutoRoutedWorkflowSet(botCfg.ConfigJSON)
		}
		ensuredRouting, added, marked, ensureErr := services.EnsureSlackDefaultWorkflowRoutes(r.Context(), channelRouting, autoRouted)
		if ensureErr != nil {
			log.Printf("[SLACK_FLOW] api config GET: default Slack slug ensure failed: %v", ensureErr)
		} else {
			channelRouting = map[string]ChannelRoute(ensuredRouting)
			if len(added) > 0 || len(marked) > 0 {
				if data, err := json.Marshal(channelRouting); err == nil {
					_, saveErr := api.chatStore.UpsertBotConnectorConfig(r.Context(), &chathistory.CreateBotConnectorConfigRequest{
						ID:              "slack",
						Enabled:         config.Enabled,
						BotMode:         botMode,
						ConfigJSON:      slackConfigJSONWithAutoRouted(configJSONRaw, autoRouted),
						AllowedChannels: string(data),
					})
					if saveErr != nil {
						log.Printf("[SLACK_FLOW] api config GET: failed to persist auto Slack slugs added=%d err=%v", len(added), saveErr)
					} else {
						log.Printf("[SLACK_FLOW] api config GET: auto-created Slack slugs @%s marked=%d", joinSlackSlugLog(added), len(marked))
					}
				}
			}
		}
		log.Printf("[SLACK_FLOW] api config GET: returning enabled=%v botMode=%v channel=%s slugRoutes=%d hasBotToken=%v hasAppToken=%v",
			config.Enabled, botMode, config.ChannelID, len(channelRouting), config.BotToken != "", config.AppToken != "")

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
		log.Printf("[SLACK_FLOW] api config POST: begin")

		var req SlackConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[SLACK] Failed to decode request: %v", err)
			log.Printf("[SLACK_FLOW] api config POST: decode failed: %v", err)
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}
		log.Printf("[SLACK_FLOW] api config POST: decoded enabled=%v botMode=%v hasBotToken=%v hasAppToken=%v channel=%s slugRoutes=%d",
			req.Enabled, req.BotMode, req.BotToken != "", req.AppToken != "", req.ChannelID, len(req.ChannelRouting))

		slackService, err := ensureSlackService()
		if err != nil {
			log.Printf("[SLACK] Failed to initialize service: %v", err)
			log.Printf("[SLACK_FLOW] api config POST: ensure service failed: %v", err)
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
			log.Printf("[SLACK_FLOW] api config POST: SaveConfig failed: %v", err)
			http.Error(w, fmt.Sprintf("failed to save config: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("[SLACK_FLOW] api config POST: Slack config saved")

		existingBotCfg, _ := api.chatStore.GetBotConnectorConfig(r.Context(), "slack")
		autoRouted := map[string]bool{}
		configJSONRaw := map[string]json.RawMessage{}
		if existingBotCfg != nil {
			autoRouted, configJSONRaw = slackAutoRoutedWorkflowSet(existingBotCfg.ConfigJSON)
		}
		ensuredRouting, added, marked, ensureErr := services.EnsureSlackDefaultWorkflowRoutes(r.Context(), req.ChannelRouting, autoRouted)
		if ensureErr != nil {
			log.Printf("[SLACK_FLOW] api config POST: default Slack slug ensure failed: %v", ensureErr)
		} else {
			req.ChannelRouting = map[string]ChannelRoute(ensuredRouting)
			if len(added) > 0 || len(marked) > 0 {
				log.Printf("[SLACK_FLOW] api config POST: auto-created Slack slugs @%s marked=%d", joinSlackSlugLog(added), len(marked))
			}
		}

		// Marshal Slack slug routing into the historical AllowedChannels JSON field.
		allowedChannelsJSON := ""
		if len(req.ChannelRouting) > 0 {
			if data, err := json.Marshal(req.ChannelRouting); err == nil {
				allowedChannelsJSON = string(data)
			}
		}

		// Save bot_mode and Slack slug routing to the filesystem-backed bot connector config.
		if _, err := api.chatStore.UpsertBotConnectorConfig(r.Context(), &chathistory.CreateBotConnectorConfigRequest{
			ID:              "slack",
			Enabled:         req.Enabled,
			BotMode:         req.BotMode,
			ConfigJSON:      slackConfigJSONWithAutoRouted(configJSONRaw, autoRouted),
			AllowedChannels: allowedChannelsJSON,
		}); err != nil {
			log.Printf("[SLACK] Failed to save bot config: %v", err)
			log.Printf("[SLACK_FLOW] api config POST: bot connector config save failed: %v", err)
			// Non-fatal — Slack config itself was saved
		} else {
			log.Printf("[SLACK_FLOW] api config POST: bot connector config saved botMode=%v slugRoutes=%d", req.BotMode, len(req.ChannelRouting))
		}

		// Dynamically register/unregister Slack bot connector
		if api.botManager != nil {
			if req.BotMode && req.Enabled {
				// Register if not already registered
				if api.botManager.GetConnector("slack") == nil {
					api.botManager.RegisterConnector(slackService)
					slackService.StartListening(r.Context())
					log.Printf("[SLACK] Bot mode enabled — registered with bot manager")
					log.Printf("[SLACK_FLOW] api config POST: bot manager registered Slack connector")
				} else {
					log.Printf("[SLACK_FLOW] api config POST: Slack connector already registered")
				}
			} else {
				log.Printf("[SLACK_FLOW] api config POST: bot manager registration skipped enabled=%v botMode=%v", req.Enabled, req.BotMode)
			}
			// Note: unregistering at runtime is complex (active sessions) — disable takes effect on restart
		} else {
			log.Printf("[SLACK_FLOW] api config POST: bot manager unavailable")
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
		log.Printf("[SLACK_FLOW] api test POST: begin contentLength=%d", r.ContentLength)

		slackService, err := ensureSlackService()
		if err != nil {
			log.Printf("[SLACK_FLOW] api test POST: ensure service failed: %v", err)
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
				log.Printf("[SLACK_FLOW] api test POST: using request config enabled=%v hasBotToken=%v hasAppToken=%v channel=%s",
					req.Enabled, req.BotToken != "", req.AppToken != "", req.ChannelID)
			} else {
				log.Printf("[SLACK_FLOW] api test POST: request config decode ignored: %v", err)
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
			log.Printf("[SLACK_FLOW] api test POST: failed: %v", err)
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
		log.Printf("[SLACK_FLOW] api test POST: success testID=%s", testUniqueID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getTestConnectionReplyHandler checks if a reply was received for a test connection
func getTestConnectionReplyHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		testUniqueID := r.URL.Query().Get("test_id")
		log.Printf("[SLACK_FLOW] api test reply GET: begin testID=%s", testUniqueID)
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
			log.Printf("[SLACK_FLOW] api test reply GET: no reply yet testID=%s", testUniqueID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Printf("[SLACK_FLOW] api test reply GET: reply found testID=%s", testUniqueID)

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
