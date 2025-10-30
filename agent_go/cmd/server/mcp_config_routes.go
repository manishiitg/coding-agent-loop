package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"mcp-agent/agent_go/pkg/mcpcache"
	"mcp-agent/agent_go/pkg/mcpclient"
)

// MCPConfigRequest represents a request to save MCP config
type MCPConfigRequest struct {
	Config mcpclient.MCPConfig `json:"config"`
}

// MCPConfigResponse represents the response for config operations
type MCPConfigResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Servers int    `json:"servers,omitempty"`
}

// handleGetMCPConfig handles GET requests to retrieve current MCP config (base + user additions)
func (api *StreamingAPI) handleGetMCPConfig(w http.ResponseWriter, r *http.Request) {
	// Reload base config to get latest version
	if err := api.mcpConfig.ReloadConfig(api.mcpConfigPath); err != nil {
		api.logger.Errorf("Failed to reload base MCP config: %v", err)
		http.Error(w, fmt.Sprintf("Failed to reload base config: %v", err), http.StatusInternalServerError)
		return
	}

	// Load user additions (if any)
	userConfigPath := strings.Replace(api.mcpConfigPath, ".json", "_user.json", 1)
	userConfig, err := mcpclient.LoadConfig(userConfigPath)
	if err != nil {
		// User config doesn't exist yet, use empty config
		userConfig = &mcpclient.MCPConfig{MCPServers: make(map[string]mcpclient.MCPServerConfig)}
		api.logger.Debugf("No user config found at %s, using empty user config", userConfigPath)
	}

	// Create ordered response with base servers first, then user servers
	// Since Go maps don't preserve order in JSON, we'll create a custom structure
	type OrderedMCPConfig struct {
		MCPServers map[string]mcpclient.MCPServerConfig `json:"mcpServers"`
	}

	// Get all server names and sort them
	allServerNames := make([]string, 0)

	// Add base server names
	for name := range api.mcpConfig.MCPServers {
		allServerNames = append(allServerNames, name)
	}

	// Add user server names (only new ones)
	for name := range userConfig.MCPServers {
		found := false
		for _, existingName := range allServerNames {
			if existingName == name {
				found = true
				break
			}
		}
		if !found {
			allServerNames = append(allServerNames, name)
		}
	}

	// Sort all server names alphabetically
	sort.Strings(allServerNames)

	// Create the response with ordered servers
	orderedConfig := &OrderedMCPConfig{
		MCPServers: make(map[string]mcpclient.MCPServerConfig),
	}

	// Populate the config in sorted order
	for _, name := range allServerNames {
		// Check if it's a user server first (user servers override base servers)
		if userServer, exists := userConfig.MCPServers[name]; exists {
			orderedConfig.MCPServers[name] = userServer
		} else if baseServer, exists := api.mcpConfig.MCPServers[name]; exists {
			orderedConfig.MCPServers[name] = baseServer
		}
	}

	api.logger.Debugf("Merged config: %d base servers + %d user servers = %d total",
		len(api.mcpConfig.MCPServers), len(userConfig.MCPServers), len(orderedConfig.MCPServers))

	w.Header().Set("Content-Type", "application/json")

	// Write JSON manually to preserve order
	fmt.Fprintf(w, "{\n  \"mcpServers\": {\n")

	// Write servers in the correct order
	for i, name := range allServerNames {
		var server mcpclient.MCPServerConfig
		if userServer, exists := userConfig.MCPServers[name]; exists {
			server = userServer
		} else if baseServer, exists := api.mcpConfig.MCPServers[name]; exists {
			server = baseServer
		}

		// Write server name and config
		serverJson, _ := json.Marshal(server)
		fmt.Fprintf(w, "    \"%s\": %s", name, string(serverJson))

		// Add comma if not the last server
		if i < len(allServerNames)-1 {
			fmt.Fprintf(w, ",")
		}
		fmt.Fprintf(w, "\n")
	}

	fmt.Fprintf(w, "  }\n}")
}

// handleSaveMCPConfig handles POST requests to save user additions to MCP config
func (api *StreamingAPI) handleSaveMCPConfig(w http.ResponseWriter, r *http.Request) {
	var req MCPConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate config
	if err := api.validateMCPConfig(&req.Config); err != nil {
		http.Error(w, fmt.Sprintf("Config validation failed: %v", err), http.StatusBadRequest)
		return
	}

	// Extract user additions (servers not in base config)
	userAdditions := &mcpclient.MCPConfig{MCPServers: make(map[string]mcpclient.MCPServerConfig)}

	// Reload base config to get current base servers
	if err := api.mcpConfig.ReloadConfig(api.mcpConfigPath); err != nil {
		api.logger.Errorf("Failed to reload base config: %v", err)
		http.Error(w, fmt.Sprintf("Failed to reload base config: %v", err), http.StatusInternalServerError)
		return
	}

	// Find servers that are not in base config (user additions)
	for name, server := range req.Config.MCPServers {
		if _, exists := api.mcpConfig.MCPServers[name]; !exists {
			userAdditions.MCPServers[name] = server
		}
	}

	// Save only user additions to user config file
	userConfigPath := strings.Replace(api.mcpConfigPath, ".json", "_user.json", 1)
	if err := mcpclient.SaveConfig(userConfigPath, userAdditions); err != nil {
		api.logger.Errorf("Failed to save user MCP config: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save user config: %v", err), http.StatusInternalServerError)
		return
	}

	// Clear cache to force fresh discovery
	cacheManager := mcpcache.GetCacheManager(api.logger)
	if err := cacheManager.Clear(); err != nil {
		api.logger.Warnf("Failed to clear cache: %v", err)
	}

	// Trigger background discovery
	go api.triggerMCPDiscovery()

	// Clear in-memory tool status to force refresh
	api.toolStatusMux.Lock()
	api.toolStatus = make(map[string]ToolStatus)
	api.toolStatusMux.Unlock()

	api.logger.Infof("✅ User MCP config saved successfully with %d user additions", len(userAdditions.MCPServers))

	response := MCPConfigResponse{
		Status:  "saved",
		Message: "User config saved and discovery triggered",
		Servers: len(req.Config.MCPServers), // Total servers (base + user)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDiscoverServers handles POST requests to trigger MCP server discovery
func (api *StreamingAPI) handleDiscoverServers(w http.ResponseWriter, r *http.Request) {
	// Trigger background discovery
	go api.triggerMCPDiscovery()

	api.logger.Infof("🔄 MCP server discovery triggered manually")

	response := MCPConfigResponse{
		Status:  "discovery_started",
		Message: "Server discovery started in background",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// validateMCPConfig validates the MCP config before saving
func (api *StreamingAPI) validateMCPConfig(config *mcpclient.MCPConfig) error {
	if config.MCPServers == nil {
		return fmt.Errorf("mcpServers field is required")
	}

	if len(config.MCPServers) == 0 {
		return fmt.Errorf("at least one server must be configured")
	}

	// Check for duplicate server names
	serverNames := make(map[string]bool)
	for name, server := range config.MCPServers {
		if name == "" {
			return fmt.Errorf("server name cannot be empty")
		}

		if serverNames[name] {
			return fmt.Errorf("duplicate server name: %s", name)
		}
		serverNames[name] = true

		// Validate protocol-specific fields
		if server.URL != "" {
			// SSE/HTTP server
			if server.Command != "" || len(server.Args) > 0 {
				return fmt.Errorf("server %s: cannot have both URL and command/args", name)
			}
		} else {
			// stdio server
			if server.Command == "" {
				return fmt.Errorf("server %s: command is required for stdio servers", name)
			}
		}

		// Validate protocol field if present
		if server.Protocol != "" {
			validProtocols := []mcpclient.ProtocolType{
				mcpclient.ProtocolStdio,
				mcpclient.ProtocolSSE,
				mcpclient.ProtocolHTTP,
			}
			isValid := false
			for _, valid := range validProtocols {
				if server.Protocol == valid {
					isValid = true
					break
				}
			}
			if !isValid {
				return fmt.Errorf("server %s: invalid protocol '%s', must be one of: %v", name, server.Protocol, validProtocols)
			}
		}
	}

	return nil
}

// loadMergedConfig loads the merged configuration (base + user additions)
func (api *StreamingAPI) loadMergedConfig() (*mcpclient.MCPConfig, error) {
	// Reload base config to get latest version
	if err := api.mcpConfig.ReloadConfig(api.mcpConfigPath); err != nil {
		return nil, fmt.Errorf("failed to reload base config: %w", err)
	}

	// Load user additions (if any)
	userConfigPath := strings.Replace(api.mcpConfigPath, ".json", "_user.json", 1)
	api.logger.Debugf("🔍 Attempting to load user config from: %s", userConfigPath)

	userConfig, err := mcpclient.LoadConfig(userConfigPath)
	if err != nil {
		// User config doesn't exist yet, use empty config
		userConfig = &mcpclient.MCPConfig{MCPServers: make(map[string]mcpclient.MCPServerConfig)}
		api.logger.Debugf("❌ No user config found at %s, using empty user config. Error: %v", userConfigPath, err)
	} else {
		api.logger.Debugf("✅ Successfully loaded user config from %s with %d servers", userConfigPath, len(userConfig.MCPServers))
		for serverName := range userConfig.MCPServers {
			api.logger.Debugf("  📋 User config server: %s", serverName)
		}
	}

	// Merge base config with user additions
	mergedConfig := &mcpclient.MCPConfig{
		MCPServers: make(map[string]mcpclient.MCPServerConfig),
	}

	// Add base servers first
	for name, server := range api.mcpConfig.MCPServers {
		mergedConfig.MCPServers[name] = server
	}

	// Add user servers (these will override base servers with same name)
	for name, server := range userConfig.MCPServers {
		mergedConfig.MCPServers[name] = server
	}

	api.logger.Debugf("Merged config: %d base servers + %d user servers = %d total",
		len(api.mcpConfig.MCPServers), len(userConfig.MCPServers), len(mergedConfig.MCPServers))

	// Debug: List all servers in merged config
	api.logger.Debugf("🔍 Final merged config servers:")
	for serverName := range mergedConfig.MCPServers {
		api.logger.Debugf("  📋 Merged server: %s", serverName)
	}

	return mergedConfig, nil
}

// createTempMergedConfig creates a temporary merged config file and returns its path
func (api *StreamingAPI) createTempMergedConfig() (string, error) {
	// Load merged config
	mergedConfig, err := api.loadMergedConfig()
	if err != nil {
		return "", err
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "mcp_merged_config_*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	// Write merged config to temp file
	if err := mcpclient.SaveConfig(tmpFile.Name(), mergedConfig); err != nil {
		os.Remove(tmpFile.Name()) // Clean up on error
		return "", fmt.Errorf("failed to write temp config: %w", err)
	}

	return tmpFile.Name(), nil
}

// triggerMCPDiscovery triggers MCP server discovery in the background
func (api *StreamingAPI) triggerMCPDiscovery() {
	api.logger.Infof("🔄 Triggering MCP server discovery after config change")

	// Use existing tool cache initialization
	api.initializeToolCache()

	// Start background discovery if not already running
	api.discoveryMux.RLock()
	isRunning := api.discoveryRunning
	api.discoveryMux.RUnlock()

	if !isRunning {
		api.startBackgroundDiscovery()
	}

	api.logger.Infof("✅ MCP discovery process initiated")
}

// handleGetMCPConfigStatus handles GET requests to get config status
func (api *StreamingAPI) handleGetMCPConfigStatus(w http.ResponseWriter, r *http.Request) {
	// Reload base config to get latest version
	if err := api.mcpConfig.ReloadConfig(api.mcpConfigPath); err != nil {
		api.logger.Errorf("Failed to reload base MCP config: %v", err)
		http.Error(w, fmt.Sprintf("Failed to reload base config: %v", err), http.StatusInternalServerError)
		return
	}

	// Load user additions
	userConfigPath := strings.Replace(api.mcpConfigPath, ".json", "_user.json", 1)
	userConfig, err := mcpclient.LoadConfig(userConfigPath)
	if err != nil {
		// User config doesn't exist yet
		userConfig = &mcpclient.MCPConfig{MCPServers: make(map[string]mcpclient.MCPServerConfig)}
	}

	// Get cache manager
	cacheManager := mcpcache.GetCacheManager(api.logger)
	cacheStats := cacheManager.GetStats()

	// Count discovered servers
	api.toolStatusMux.RLock()
	discoveredCount := len(api.toolStatus)
	api.toolStatusMux.RUnlock()

	// Check discovery status
	api.discoveryMux.RLock()
	isDiscoveryRunning := api.discoveryRunning
	lastDiscovery := api.lastDiscovery
	api.discoveryMux.RUnlock()

	status := map[string]interface{}{
		"config_path":        api.mcpConfigPath,
		"user_config_path":   userConfigPath,
		"base_servers":       len(api.mcpConfig.MCPServers),
		"user_servers":       len(userConfig.MCPServers),
		"total_servers":      len(api.mcpConfig.MCPServers) + len(userConfig.MCPServers),
		"discovered_servers": discoveredCount,
		"discovery_running":  isDiscoveryRunning,
		"last_discovery":     lastDiscovery.Format(time.RFC3339),
		"cache_stats":        cacheStats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
