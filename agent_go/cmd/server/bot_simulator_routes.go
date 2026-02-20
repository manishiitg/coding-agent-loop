package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	slackservice "mcp-agent-builder-go/agent_go/cmd/server/services"
	"mcp-agent-builder-go/agent_go/pkg/database"

	"github.com/gorilla/mux"
)

// BotSimulatorRoutes sets up the web simulator API routes
func BotSimulatorRoutes(router *mux.Router, api *StreamingAPI, db database.Database) {
	simRouter := router.PathPrefix("/api/bot/simulate").Subrouter()

	simRouter.HandleFunc("/send", simulatorSendHandler(api)).Methods("POST", "OPTIONS")
	simRouter.HandleFunc("/config", simulatorGetConfigHandler(db)).Methods("GET")
	simRouter.HandleFunc("/config", simulatorSaveConfigHandler(db)).Methods("POST", "OPTIONS")
	simRouter.HandleFunc("/available-capabilities", simulatorAvailableCapabilitiesHandler(api)).Methods("GET")
	simRouter.HandleFunc("/threads", simulatorListThreadsHandler(api)).Methods("GET")
	simRouter.HandleFunc("/mode", simulatorGetModeHandler(api)).Methods("GET")
	simRouter.HandleFunc("/mode", simulatorSetModeHandler(api)).Methods("POST", "OPTIONS")
	simRouter.HandleFunc("/{threadId}/messages", simulatorMessagesHandler(api)).Methods("GET")
	simRouter.HandleFunc("/{threadId}/interact", simulatorInteractHandler(api)).Methods("POST", "OPTIONS")
	simRouter.HandleFunc("/{threadId}", simulatorCleanupHandler(api)).Methods("DELETE", "OPTIONS")
}

func simulatorSendHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		log.Printf("[SIM_SEND] Received simulate/send request")

		if api.webSimulator == nil {
			log.Printf("[SIM_SEND] ERROR: webSimulator is nil")
			http.Error(w, "Web simulator not initialized", http.StatusServiceUnavailable)
			return
		}

		if api.botManager == nil {
			log.Printf("[SIM_SEND] ERROR: botManager is nil")
			http.Error(w, "Bot manager not initialized", http.StatusServiceUnavailable)
			return
		}

		var req struct {
			Message  string `json:"message"`
			ThreadID string `json:"thread_id"` // optional: reuse existing thread for follow-ups
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[SIM_SEND] ERROR: invalid request body: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Message == "" {
			http.Error(w, "Message is required", http.StatusBadRequest)
			return
		}

		log.Printf("[SIM_SEND] Message: %q threadID: %q", req.Message, req.ThreadID)

		// Determine thread ID — reuse provided thread_id if given (for follow-ups)
		var threadTS string
		if req.ThreadID != "" {
			threadTS = req.ThreadID
		} else if api.webSimulator.IsThreaded() {
			threadTS = fmt.Sprintf("sim_%d", time.Now().UnixNano())
		} else {
			threadTS = "simulator"
		}

		// Store user message in the thread (for history)
		api.webSimulator.AddUserMessage(threadTS, req.Message)

		threadID := slackservice.ThreadID{
			Platform:  "web_simulator",
			ChannelID: "simulator",
			ThreadTS:  threadTS,
		}

		// Extract authenticated user from HTTP context for per-user secrets
		userID := GetUserIDFromContext(r.Context())

		log.Printf("[SIM_SEND] Calling HandleMessageSync for thread %s (userID=%s)", threadID.Key(), userID)
		startTime := time.Now()

		result, err := api.botManager.HandleMessageSync(context.Background(), slackservice.BotIncomingMessage{
			Platform:        "web_simulator",
			UserID:          userID,
			WorkspaceUserID: userID,
			UserName:        "Simulator User",
			ChannelID:       "simulator",
			ThreadTS:        threadTS,
			Text:            req.Message,
			Timestamp:       time.Now(),
		}, threadID)
		if err != nil {
			log.Printf("[SIM_SEND] ERROR: HandleMessageSync failed after %v: %v", time.Since(startTime), err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		log.Printf("[SIM_SEND] HandleMessageSync completed in %v, type=%s", time.Since(startTime), result.Type)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func simulatorMessagesHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if api.webSimulator == nil {
			http.Error(w, "Web simulator not initialized", http.StatusServiceUnavailable)
			return
		}

		threadID := mux.Vars(r)["threadId"]
		since := 0
		if s := r.URL.Query().Get("since"); s != "" {
			if parsed, err := strconv.Atoi(s); err == nil {
				since = parsed
			}
		}

		messages := api.webSimulator.GetThreadMessages(threadID, since)
		total := api.webSimulator.GetThreadMessageCount(threadID)

		// Ensure non-null JSON array
		if messages == nil {
			messages = []slackservice.SimulatorMessage{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"messages": messages,
			"total":    total,
		})
	}
}

func simulatorInteractHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if api.webSimulator == nil {
			http.Error(w, "Web simulator not initialized", http.StatusServiceUnavailable)
			return
		}

		threadID := mux.Vars(r)["threadId"]

		var req struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		api.webSimulator.HandleSimulatedInteraction(threadID, req.ActionID, req.Value)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
	}
}

func simulatorCleanupHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if api.webSimulator == nil {
			http.Error(w, "Web simulator not initialized", http.StatusServiceUnavailable)
			return
		}

		threadID := mux.Vars(r)["threadId"]
		api.webSimulator.CleanupThread(threadID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
	}
}

func simulatorListThreadsHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if api.webSimulator == nil {
			http.Error(w, "Web simulator not initialized", http.StatusServiceUnavailable)
			return
		}

		threads := api.webSimulator.ListThreads()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"threads": threads,
		})
	}
}

func simulatorGetModeHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if api.webSimulator == nil {
			http.Error(w, "Web simulator not initialized", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"threaded": api.webSimulator.IsThreaded(),
		})
	}
}

func simulatorSetModeHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if api.webSimulator == nil {
			http.Error(w, "Web simulator not initialized", http.StatusServiceUnavailable)
			return
		}

		var req struct {
			Threaded bool `json:"threaded"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		api.webSimulator.SetThreaded(req.Threaded)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"threaded": req.Threaded,
		})
	}
}

func simulatorGetConfigHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, err := db.GetBotConnectorConfig(r.Context(), "_global")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{})
			return
		}

		result := map[string]interface{}{}

		if cfg.ConfigJSON != "" {
			var cfgData map[string]json.RawMessage
			if err := json.Unmarshal([]byte(cfg.ConfigJSON), &cfgData); err == nil {
				if tierJSON, ok := cfgData["delegation_tier_config"]; ok {
					var tierConfig interface{}
					if err := json.Unmarshal(tierJSON, &tierConfig); err == nil {
						result["delegation_tier_config"] = tierConfig
					}
				}
				if raw, ok := cfgData["default_servers"]; ok {
					var servers []string
					if err := json.Unmarshal(raw, &servers); err == nil {
						result["default_servers"] = servers
					}
				}
				if raw, ok := cfgData["default_skills"]; ok {
					var skills []string
					if err := json.Unmarshal(raw, &skills); err == nil {
						result["default_skills"] = skills
					}
				}
				if raw, ok := cfgData["delegation_mode"]; ok {
					var mode string
					if err := json.Unmarshal(raw, &mode); err == nil {
						result["delegation_mode"] = mode
					}
				}
				if raw, ok := cfgData["allowed_emails"]; ok {
					var emails []string
					if err := json.Unmarshal(raw, &emails); err == nil {
						result["allowed_emails"] = emails
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func simulatorSaveConfigHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req struct {
			DelegationTierConfig json.RawMessage  `json:"delegation_tier_config,omitempty"`
			ProviderAPIKeys      map[string]string `json:"provider_api_keys,omitempty"`
			DefaultServers       []string          `json:"default_servers,omitempty"`
			DefaultSkills        []string          `json:"default_skills,omitempty"`
			DelegationMode       string            `json:"delegation_mode,omitempty"`
			AllowedEmails        []string          `json:"allowed_emails,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Build ConfigJSON: merge all config into one JSON blob
		cfgMap := map[string]interface{}{}

		// Preserve existing config values first
		existingCfg, _ := db.GetBotConnectorConfig(r.Context(), "_global")
		if existingCfg != nil && existingCfg.ConfigJSON != "" {
			json.Unmarshal([]byte(existingCfg.ConfigJSON), &cfgMap)
		}

		if len(req.DelegationTierConfig) > 0 {
			var tierData interface{}
			if err := json.Unmarshal(req.DelegationTierConfig, &tierData); err == nil {
				cfgMap["delegation_tier_config"] = tierData
			}
		}
		// Store per-provider API keys (from frontend) for bot session use
		if len(req.ProviderAPIKeys) > 0 {
			existing, _ := cfgMap["provider_api_keys"].(map[string]interface{})
			if existing == nil {
				existing = map[string]interface{}{}
			}
			for k, v := range req.ProviderAPIKeys {
				existing[k] = v
			}
			cfgMap["provider_api_keys"] = existing
		}
		// Store default servers/skills selections
		if req.DefaultServers != nil {
			cfgMap["default_servers"] = req.DefaultServers
		}
		if req.DefaultSkills != nil {
			cfgMap["default_skills"] = req.DefaultSkills
		}
		if req.DelegationMode == "plan" || req.DelegationMode == "spawn" {
			cfgMap["delegation_mode"] = req.DelegationMode
		}
		if req.AllowedEmails != nil {
			cfgMap["allowed_emails"] = req.AllowedEmails
		}

		configJSON := ""
		if len(cfgMap) > 0 {
			cfgBytes, _ := json.Marshal(cfgMap)
			configJSON = string(cfgBytes)
		}

		_, err := db.UpsertBotConnectorConfig(r.Context(), &database.CreateBotConnectorConfigRequest{
			ID:         "_global",
			Enabled:    true,
			BotMode:    true,
			ConfigJSON: configJSON,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
	}
}

// simulatorAvailableCapabilitiesHandler returns all available MCP servers and skills
func simulatorAvailableCapabilitiesHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if api.botManager == nil {
			http.Error(w, "Bot manager not initialized", http.StatusServiceUnavailable)
			return
		}

		servers, discoveredSkills := api.botManager.LoadAvailableCapabilities()

		type skillInfo struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		}

		skillList := make([]skillInfo, 0, len(discoveredSkills))
		for _, s := range discoveredSkills {
			skillList = append(skillList, skillInfo{
				Name:        s.FolderName,
				Description: s.Frontmatter.Description,
			})
		}

		if servers == nil {
			servers = []string{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"servers": servers,
			"skills":  skillList,
		})
	}
}
