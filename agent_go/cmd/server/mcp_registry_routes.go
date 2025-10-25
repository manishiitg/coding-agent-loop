package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mcp-agent/agent_go/pkg/mcpcache"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/gorilla/mux"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPRegistryServer represents a server from the MCP Registry
type MCPRegistryServer struct {
	Schema      string                 `json:"$schema,omitempty"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Version     string                 `json:"version"`
	Status      string                 `json:"status,omitempty"`
	Repository  *Repository            `json:"repository,omitempty"`
	Packages    []Package              `json:"packages,omitempty"`
	Remotes     []Remote               `json:"remotes,omitempty"`
	Meta        map[string]interface{} `json:"_meta,omitempty"`
}

type Repository struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

type Package struct {
	RegistryType         string                `json:"registryType"`
	RegistryBaseURL      string                `json:"registryBaseUrl,omitempty"`
	Identifier           string                `json:"identifier"`
	Version              string                `json:"version"`
	Transport            Transport             `json:"transport"`
	EnvironmentVariables []EnvironmentVariable `json:"environmentVariables,omitempty"`
}

type Transport struct {
	Type string `json:"type"`
}

type EnvironmentVariable struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsRequired  bool   `json:"isRequired,omitempty"`
	Format      string `json:"format,omitempty"`
	IsSecret    bool   `json:"isSecret,omitempty"`
}

type Remote struct {
	Type    string   `json:"type"`
	URL     string   `json:"url"`
	Headers []Header `json:"headers,omitempty"`
}

type Header struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsSecret    bool   `json:"isSecret,omitempty"`
}

// MCPRegistryResponse represents the response from MCP Registry API
type MCPRegistryResponse struct {
	Servers  []MCPRegistryServer `json:"servers"`
	Metadata Metadata            `json:"metadata"`
}

// EnhancedMCPRegistryResponse includes cache information
type EnhancedMCPRegistryResponse struct {
	Servers  []EnhancedMCPRegistryServer `json:"servers"`
	Metadata Metadata                    `json:"metadata"`
}

// EnhancedMCPRegistryServer includes cache status
type EnhancedMCPRegistryServer struct {
	MCPRegistryServer
	CacheStatus *CacheStatus `json:"cacheStatus,omitempty"`
}

// CacheStatus represents cached server information
type CacheStatus struct {
	IsCached       bool   `json:"isCached"`
	ToolsCount     int    `json:"toolsCount,omitempty"`
	PromptsCount   int    `json:"promptsCount,omitempty"`
	ResourcesCount int    `json:"resourcesCount,omitempty"`
	LastUpdated    string `json:"lastUpdated,omitempty"`
}

type Metadata struct {
	NextCursor string `json:"next_cursor"`
	Count      int    `json:"count"`
}

// MCPRegistrySearchParams represents search parameters
type MCPRegistrySearchParams struct {
	Query    string   `json:"query,omitempty"`
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Limit    int      `json:"limit,omitempty"`
	Cursor   string   `json:"cursor,omitempty"`
}

const (
	MCPRegistryBaseURL = "https://registry.modelcontextprotocol.io/v0"
	RequestTimeout     = 30 * time.Second
)

// PromptDetail represents a prompt
type PromptDetail struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ResourceDetail represents a resource
type ResourceDetail struct {
	Name        string `json:"name"`
	URI         string `json:"uri"`
	Description string `json:"description,omitempty"`
}

// EnhancedToolStatus extends ToolStatus with prompts and resources
type EnhancedToolStatus struct {
	ToolStatus
	Prompts   []PromptDetail   `json:"prompts,omitempty"`
	Resources []ResourceDetail `json:"resources,omitempty"`
}

// handleGetMCPRegistryServers handles GET /api/mcp-registry/servers
func (api *StreamingAPI) handleGetMCPRegistryServers(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	limitStr := r.URL.Query().Get("limit")
	cursor := r.URL.Query().Get("cursor")

	// Set defaults
	limit := 50

	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	// Build the registry API URL
	registryURL := fmt.Sprintf("%s/servers", MCPRegistryBaseURL)
	params := url.Values{}

	if query != "" {
		params.Add("search", query)
	}
	if category != "" {
		params.Add("category", category)
	}
	if limit > 0 {
		params.Add("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		params.Add("cursor", cursor)
	}

	if len(params) > 0 {
		registryURL += "?" + params.Encode()
	}

	// Make request to MCP Registry
	client := &http.Client{Timeout: RequestTimeout}
	req, err := http.NewRequest("GET", registryURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	req.Header.Set("User-Agent", "MCP-Agent/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch from registry: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Registry API error: %d %s", resp.StatusCode, string(body)), http.StatusBadGateway)
		return
	}

	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	var registryResponse MCPRegistryResponse
	if err := json.Unmarshal(body, &registryResponse); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse response: %v", err), http.StatusInternalServerError)
		return
	}

	// Enhance response with cached server information
	enhancedResponse, err := api.enhanceRegistryResponseWithCache(registryResponse)
	if err != nil {
		api.logger.Warnf("Failed to enhance registry response with cache: %v", err)
		// Continue with original response if enhancement fails
		enhancedResponse = &EnhancedMCPRegistryResponse{
			Servers:  make([]EnhancedMCPRegistryServer, len(registryResponse.Servers)),
			Metadata: registryResponse.Metadata,
		}
		for i, server := range registryResponse.Servers {
			enhancedResponse.Servers[i] = EnhancedMCPRegistryServer{
				MCPRegistryServer: server,
			}
		}
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300") // Cache for 5 minutes

	// Return the enhanced response
	if err := json.NewEncoder(w).Encode(enhancedResponse); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleGetMCPRegistryServerDetails handles GET /api/mcp-registry/servers/{id}
func (api *StreamingAPI) handleGetMCPRegistryServerDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]

	if serverID == "" {
		http.Error(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	// Build the registry API URL
	registryURL := fmt.Sprintf("%s/servers/%s", MCPRegistryBaseURL, serverID)

	// Make request to MCP Registry
	client := &http.Client{Timeout: RequestTimeout}
	req, err := http.NewRequest("GET", registryURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	req.Header.Set("User-Agent", "MCP-Agent/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch from registry: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Registry API error: %d %s", resp.StatusCode, string(body)), http.StatusBadGateway)
		return
	}

	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read response: %v", err), http.StatusInternalServerError)
		return
	}

	var server MCPRegistryServer
	if err := json.Unmarshal(body, &server); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse response: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300") // Cache for 5 minutes

	// Return the response
	if err := json.NewEncoder(w).Encode(server); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleGetMCPRegistryServerTools handles POST /api/mcp-registry/servers/{id}/tools
func (api *StreamingAPI) handleGetMCPRegistryServerTools(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]

	if serverID == "" {
		http.Error(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	// Parse request body for custom headers and environment variables
	var requestBody struct {
		Headers map[string]string `json:"headers"`
		EnvVars map[string]string `json:"envVars"`
	}

	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			api.logger.Warnf("Failed to read request body: %v", err)
		} else if len(bodyBytes) > 0 {
			if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
				api.logger.Warnf("Failed to parse request body: %v", err)
			}
		}
	}

	// Note: Registry routes don't have access to full server config for cache keys
	// For now, we'll skip cache lookup in this context
	api.logger.Debugf("Skipping cache lookup for server %s - configuration required for cache keys", serverID)

	// Cache lookup skipped - configuration required for cache keys
	api.logger.Debugf("Proceeding with live discovery for server %s", serverID)

	api.logger.Debugf("Cache miss for server %s, discovering tools live with headers: %v, envVars: %v", serverID, requestBody.Headers, requestBody.EnvVars)

	// Cache miss - discover tools live with custom headers and environment variables
	response, err := api.discoverServerToolsLiveWithAuth(serverID, requestBody.Headers, requestBody.EnvVars)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to discover server tools: %v", err), http.StatusBadGateway)
		return
	}

	// Store in cache for future requests (only if no custom headers or env vars)
	if len(requestBody.Headers) == 0 && len(requestBody.EnvVars) == 0 {
		if err := api.storeServerToolsInCache(serverID, response); err != nil {
			api.logger.Warnf("Failed to store server tools in cache: %v", err)
			// Continue without caching
		}
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300") // Cache for 5 minutes
	if len(requestBody.Headers) > 0 || len(requestBody.EnvVars) > 0 {
		w.Header().Set("X-Cache-Status", "BYPASS")
	} else {
		w.Header().Set("X-Cache-Status", "MISS")
	}

	// Return the response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// getRegistryServerDetails fetches server details from the MCP Registry
func (api *StreamingAPI) getRegistryServerDetails(serverID string) (*MCPRegistryServer, error) {
	// Build the registry API URL
	registryURL := fmt.Sprintf("%s/servers/%s", MCPRegistryBaseURL, serverID)

	// Make request to MCP Registry
	client := &http.Client{Timeout: RequestTimeout}
	req, err := http.NewRequest("GET", registryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("User-Agent", "MCP-Agent/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from registry: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("server not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry API error: %d %s", resp.StatusCode, string(body))
	}

	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	var server MCPRegistryServer
	if err := json.Unmarshal(body, &server); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return &server, nil
}

// convertRegistryServerToConfig converts a MCPRegistryServer to MCPServerConfig
func (api *StreamingAPI) convertRegistryServerToConfig(server *MCPRegistryServer) (mcpclient.MCPServerConfig, error) {
	// Check if server has packages or remotes
	if len(server.Packages) == 0 && len(server.Remotes) == 0 {
		return mcpclient.MCPServerConfig{}, fmt.Errorf("server has no installation instructions (packages or remotes) defined. This server may not be ready for installation yet.")
	}

	var protocol mcpclient.ProtocolType
	var command string
	var args []string
	var url string
	var env map[string]string

	// Handle packages first (npm/other package managers)
	if len(server.Packages) > 0 {
		// Use the first package for now (could be enhanced to let user choose)
		pkg := server.Packages[0]

		// Determine protocol based on package type
		switch pkg.RegistryType {
		case "npm":
			protocol = mcpclient.ProtocolStdio
			command = "npx"
			args = []string{"-y", pkg.Identifier}
			if pkg.Version != "" {
				args = append(args, "@"+pkg.Version)
			}
		case "pypi":
			protocol = mcpclient.ProtocolStdio
			command = "pip"
			args = []string{"install", pkg.Identifier}
		case "nuget":
			protocol = mcpclient.ProtocolStdio
			command = "dotnet"
			args = []string{"add", "package", pkg.Identifier}
		case "remote":
			// Check if there are remotes defined
			if len(server.Remotes) > 0 {
				remote := server.Remotes[0]
				protocol = mcpclient.ProtocolHTTP
				url = remote.URL
			} else {
				return mcpclient.MCPServerConfig{}, fmt.Errorf("server has no remotes defined")
			}
		default:
			return mcpclient.MCPServerConfig{}, fmt.Errorf("unsupported registry type: %s", pkg.RegistryType)
		}

		// Extract environment variables
		env = make(map[string]string)
		for _, envVar := range pkg.EnvironmentVariables {
			if envVar.IsRequired {
				// For required variables, set a placeholder value
				// In a real implementation, you might want to prompt the user
				env[envVar.Name] = fmt.Sprintf("REQUIRED_%s", envVar.Name)
			}
		}
	} else if len(server.Remotes) > 0 {
		// Handle remotes only (no packages)
		// Prefer SSE over HTTP if both are available
		var selectedRemote Remote
		var selectedProtocol mcpclient.ProtocolType

		// Look for SSE first, then HTTP
		for _, remote := range server.Remotes {
			if remote.Type == "sse" {
				selectedRemote = remote
				selectedProtocol = mcpclient.ProtocolSSE
				break
			}
		}

		// If no SSE found, use the first HTTP remote
		if selectedRemote.URL == "" {
			for _, remote := range server.Remotes {
				if remote.Type == "streamable-http" || remote.Type == "http" {
					selectedRemote = remote
					selectedProtocol = mcpclient.ProtocolHTTP
					break
				}
			}
		}

		// If still no remote found, use the first one
		if selectedRemote.URL == "" {
			selectedRemote = server.Remotes[0]
			switch selectedRemote.Type {
			case "sse":
				selectedProtocol = mcpclient.ProtocolSSE
			case "streamable-http", "http":
				selectedProtocol = mcpclient.ProtocolHTTP
			default:
				return mcpclient.MCPServerConfig{}, fmt.Errorf("unsupported remote type: %s", selectedRemote.Type)
			}
		}

		protocol = selectedProtocol
		url = selectedRemote.URL

		// Extract headers as environment variables
		env = make(map[string]string)
		for _, header := range selectedRemote.Headers {
			// For secret headers, set a placeholder value
			if header.IsSecret {
				env[header.Name] = fmt.Sprintf("REQUIRED_%s", header.Name)
			}
		}
	}

	config := mcpclient.MCPServerConfig{
		Description: server.Description,
		Protocol:    protocol,
		Command:     command,
		Args:        args,
		URL:         url,
		Env:         env,
	}

	return config, nil
}

// discoverServerToolsLive discovers tools by connecting to the MCP server
func (api *StreamingAPI) discoverServerToolsLive(serverID string) (*EnhancedToolStatus, error) {
	// First, get the server details from the registry
	server, err := api.getRegistryServerDetails(serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to get server details: %v", err)
	}

	// Convert registry server to MCP config
	config, err := api.convertRegistryServerToConfig(server)
	if err != nil {
		return nil, fmt.Errorf("failed to convert server config: %v", err)
	}

	// Create temporary MCP client and discover tools
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client := mcpclient.New(config, api.logger)

	// Connect to the server
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}
	defer client.Close()

	// List tools
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %v", err)
	}

	// List prompts
	prompts, err := client.ListPrompts(ctx)
	if err != nil {
		// Prompts are optional, log but don't fail
		api.logger.Warnf("Failed to list prompts for server %s: %v", serverID, err)
		prompts = []mcp.Prompt{}
	}

	// List resources
	resources, err := client.ListResources(ctx)
	if err != nil {
		// Resources are optional, log but don't fail
		api.logger.Warnf("Failed to list resources for server %s: %v", serverID, err)
		resources = []mcp.Resource{}
	}

	// Convert tools to the expected format
	toolDetails := make([]mcpclient.ToolDetail, 0, len(tools))
	functionNames := make([]string, 0, len(tools))

	for _, tool := range tools {
		// Convert mcp.Tool to ToolDetail format
		// Convert InputSchema to map[string]interface{} with proper JSON Schema format
		parameters := make(map[string]interface{})

		// Set type
		if tool.InputSchema.Type != "" {
			parameters["type"] = tool.InputSchema.Type
		} else {
			parameters["type"] = "object"
		}

		// Only add properties if they exist and are not empty
		if tool.InputSchema.Properties != nil && len(tool.InputSchema.Properties) > 0 {
			parameters["properties"] = tool.InputSchema.Properties
		} else {
			parameters["properties"] = map[string]interface{}{}
		}

		// Only add required if they exist and are not empty
		if tool.InputSchema.Required != nil && len(tool.InputSchema.Required) > 0 {
			parameters["required"] = tool.InputSchema.Required
		} else {
			parameters["required"] = []string{}
		}

		// Add additional properties restriction for better validation
		parameters["additionalProperties"] = false

		toolDetail := mcpclient.ToolDetail{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  parameters,
		}
		toolDetails = append(toolDetails, toolDetail)
		functionNames = append(functionNames, tool.Name)
	}

	// Convert prompts to the expected format
	promptDetails := make([]PromptDetail, 0, len(prompts))
	for _, prompt := range prompts {
		promptDetail := PromptDetail{
			Name:        prompt.Name,
			Description: prompt.Description,
		}
		promptDetails = append(promptDetails, promptDetail)
	}

	// Convert resources to the expected format
	resourceDetails := make([]ResourceDetail, 0, len(resources))
	for _, resource := range resources {
		resourceDetail := ResourceDetail{
			Name:        resource.Name,
			URI:         resource.URI,
			Description: resource.Description,
		}
		resourceDetails = append(resourceDetails, resourceDetail)
	}

	// Create response in the same format as /api/tools/detail
	response := &EnhancedToolStatus{
		ToolStatus: ToolStatus{
			Name:          server.Name,
			Server:        server.Name,
			Status:        "ok",
			Description:   server.Description,
			ToolsEnabled:  len(tools),
			FunctionNames: functionNames,
			Tools:         toolDetails,
		},
		Prompts:   promptDetails,
		Resources: resourceDetails,
	}

	return response, nil
}

// discoverServerToolsLiveWithHeaders discovers tools by connecting to the MCP server with custom headers
func (api *StreamingAPI) discoverServerToolsLiveWithHeaders(serverID string, customHeaders map[string]string) (*EnhancedToolStatus, error) {
	// First, get the server details from the registry
	server, err := api.getRegistryServerDetails(serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to get server details: %v", err)
	}

	// Convert registry server to MCP config
	config, err := api.convertRegistryServerToConfig(server)
	if err != nil {
		return nil, fmt.Errorf("failed to convert server config: %v", err)
	}

	// Apply custom headers to the config
	if len(customHeaders) > 0 {
		// For HTTP/SSE servers, we need to add headers to the connection
		// This is a simplified approach - in a real implementation, you'd need to
		// modify the MCP client to support custom headers
		api.logger.Debugf("Applying custom headers to server config: %v", customHeaders)

		// Add headers as environment variables for now
		// In a full implementation, you'd modify the HTTP client to include these headers
		if config.Env == nil {
			config.Env = make(map[string]string)
		}
		for key, value := range customHeaders {
			config.Env[key] = value
		}
	}

	// Create temporary MCP client and discover tools
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client := mcpclient.New(config, api.logger)

	// Connect to the server
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}
	defer client.Close()

	// List tools
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %v", err)
	}

	// List prompts
	prompts, err := client.ListPrompts(ctx)
	if err != nil {
		// Prompts are optional, log but don't fail
		api.logger.Warnf("Failed to list prompts for server %s: %v", serverID, err)
		prompts = []mcp.Prompt{}
	}

	// List resources
	resources, err := client.ListResources(ctx)
	if err != nil {
		// Resources are optional, log but don't fail
		api.logger.Warnf("Failed to list resources for server %s: %v", serverID, err)
		resources = []mcp.Resource{}
	}

	// Convert tools to the expected format
	toolDetails := make([]mcpclient.ToolDetail, 0, len(tools))
	functionNames := make([]string, 0, len(tools))

	for _, tool := range tools {
		// Convert mcp.Tool to ToolDetail format
		// Convert InputSchema to map[string]interface{} with proper JSON Schema format
		parameters := make(map[string]interface{})

		// Set type
		if tool.InputSchema.Type != "" {
			parameters["type"] = tool.InputSchema.Type
		} else {
			parameters["type"] = "object"
		}

		// Only add properties if they exist and are not empty
		if tool.InputSchema.Properties != nil && len(tool.InputSchema.Properties) > 0 {
			parameters["properties"] = tool.InputSchema.Properties
		} else {
			parameters["properties"] = map[string]interface{}{}
		}

		// Only add required if they exist and are not empty
		if tool.InputSchema.Required != nil && len(tool.InputSchema.Required) > 0 {
			parameters["required"] = tool.InputSchema.Required
		} else {
			parameters["required"] = []string{}
		}

		// Add additional properties restriction for better validation
		parameters["additionalProperties"] = false

		toolDetail := mcpclient.ToolDetail{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  parameters,
		}
		toolDetails = append(toolDetails, toolDetail)
		functionNames = append(functionNames, tool.Name)
	}

	// Convert prompts to the expected format
	promptDetails := make([]PromptDetail, 0, len(prompts))
	for _, prompt := range prompts {
		promptDetail := PromptDetail{
			Name:        prompt.Name,
			Description: prompt.Description,
		}
		promptDetails = append(promptDetails, promptDetail)
	}

	// Convert resources to the expected format
	resourceDetails := make([]ResourceDetail, 0, len(resources))
	for _, resource := range resources {
		resourceDetail := ResourceDetail{
			Name:        resource.Name,
			URI:         resource.URI,
			Description: resource.Description,
		}
		resourceDetails = append(resourceDetails, resourceDetail)
	}

	// Create response in the same format as /api/tools/detail
	response := &EnhancedToolStatus{
		ToolStatus: ToolStatus{
			Name:          server.Name,
			Server:        server.Name,
			Status:        "ok",
			Description:   server.Description,
			ToolsEnabled:  len(tools),
			FunctionNames: functionNames,
			Tools:         toolDetails,
		},
		Prompts:   promptDetails,
		Resources: resourceDetails,
	}

	return response, nil
}

// discoverServerToolsLiveWithAuth discovers tools by connecting to the MCP server with custom headers and environment variables
func (api *StreamingAPI) discoverServerToolsLiveWithAuth(serverID string, customHeaders map[string]string, customEnvVars map[string]string) (*EnhancedToolStatus, error) {
	// First, get the server details from the registry
	server, err := api.getRegistryServerDetails(serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to get server details: %v", err)
	}

	// Convert registry server to MCP config
	config, err := api.convertRegistryServerToConfig(server)
	if err != nil {
		return nil, fmt.Errorf("failed to convert server config: %v", err)
	}

	// Apply custom environment variables
	if len(customEnvVars) > 0 {
		if config.Env == nil {
			config.Env = make(map[string]string)
		}
		for key, value := range customEnvVars {
			config.Env[key] = value
		}
	}

	// Apply custom headers for remote servers
	if len(customHeaders) > 0 {
		if config.Headers == nil {
			config.Headers = make(map[string]string)
		}
		for key, value := range customHeaders {
			config.Headers[key] = value
		}
	}

	// Create temporary MCP client and discover tools
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client := mcpclient.New(config, api.logger)

	// Connect to the server
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}
	defer client.Close()

	// List tools
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %v", err)
	}

	// List prompts
	prompts, err := client.ListPrompts(ctx)
	if err != nil {
		// Prompts are optional, log but don't fail
		api.logger.Warnf("Failed to list prompts for server %s: %v", serverID, err)
		prompts = []mcp.Prompt{}
	}

	// List resources
	resources, err := client.ListResources(ctx)
	if err != nil {
		// Resources are optional, log but don't fail
		api.logger.Warnf("Failed to list resources for server %s: %v", serverID, err)
		resources = []mcp.Resource{}
	}

	// Convert tools to the expected format
	toolDetails := make([]mcpclient.ToolDetail, 0, len(tools))
	functionNames := make([]string, 0, len(tools))

	for _, tool := range tools {
		// Convert mcp.Tool to ToolDetail format
		// Convert InputSchema to map[string]interface{} with proper JSON Schema format
		parameters := make(map[string]interface{})

		// Set type
		if tool.InputSchema.Type != "" {
			parameters["type"] = tool.InputSchema.Type
		} else {
			parameters["type"] = "object"
		}

		// Only add properties if they exist and are not empty
		if tool.InputSchema.Properties != nil && len(tool.InputSchema.Properties) > 0 {
			parameters["properties"] = tool.InputSchema.Properties
		} else {
			parameters["properties"] = map[string]interface{}{}
		}

		// Only add required if they exist and are not empty
		if tool.InputSchema.Required != nil && len(tool.InputSchema.Required) > 0 {
			parameters["required"] = tool.InputSchema.Required
		} else {
			parameters["required"] = []string{}
		}

		// Add additional properties restriction for better validation
		parameters["additionalProperties"] = false

		toolDetail := mcpclient.ToolDetail{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  parameters,
		}
		toolDetails = append(toolDetails, toolDetail)
		functionNames = append(functionNames, tool.Name)
	}

	// Convert prompts to the expected format
	promptDetails := make([]PromptDetail, 0, len(prompts))
	for _, prompt := range prompts {
		promptDetail := PromptDetail{
			Name:        prompt.Name,
			Description: prompt.Description,
		}
		promptDetails = append(promptDetails, promptDetail)
	}

	// Convert resources to the expected format
	resourceDetails := make([]ResourceDetail, 0, len(resources))
	for _, resource := range resources {
		resourceDetail := ResourceDetail{
			Name:        resource.Name,
			URI:         resource.URI,
			Description: resource.Description,
		}
		resourceDetails = append(resourceDetails, resourceDetail)
	}

	// Create response in the same format as /api/tools/detail
	response := &EnhancedToolStatus{
		ToolStatus: ToolStatus{
			Name:          server.Name,
			Server:        server.Name,
			Status:        "ok",
			Description:   server.Description,
			ToolsEnabled:  len(tools),
			FunctionNames: functionNames,
			Tools:         toolDetails,
		},
		Prompts:   promptDetails,
		Resources: resourceDetails,
	}

	return response, nil
}

// convertCacheEntryToResponse converts a cached CacheEntry to EnhancedToolStatus
func (api *StreamingAPI) convertCacheEntryToResponse(entry *mcpcache.CacheEntry) (*EnhancedToolStatus, error) {
	// Convert tools to ToolDetail format
	toolDetails := make([]mcpclient.ToolDetail, 0, len(entry.Tools))
	functionNames := make([]string, 0, len(entry.Tools))

	for _, tool := range entry.Tools {
		// Convert llms.Tool to ToolDetail format
		parameters := make(map[string]interface{})
		if tool.Function.Parameters != nil {
			if params, ok := tool.Function.Parameters.(map[string]interface{}); ok {
				parameters = params
			}
		}

		toolDetail := mcpclient.ToolDetail{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  parameters,
		}
		toolDetails = append(toolDetails, toolDetail)
		functionNames = append(functionNames, tool.Function.Name)
	}

	// Convert prompts to PromptDetail format
	promptDetails := make([]PromptDetail, 0, len(entry.Prompts))
	for _, prompt := range entry.Prompts {
		promptDetail := PromptDetail{
			Name:        prompt.Name,
			Description: prompt.Description,
		}
		promptDetails = append(promptDetails, promptDetail)
	}

	// Convert resources to ResourceDetail format
	resourceDetails := make([]ResourceDetail, 0, len(entry.Resources))
	for _, resource := range entry.Resources {
		resourceDetail := ResourceDetail{
			Name:        resource.Name,
			URI:         resource.URI,
			Description: resource.Description,
		}
		resourceDetails = append(resourceDetails, resourceDetail)
	}

	// Create response
	response := &EnhancedToolStatus{
		ToolStatus: ToolStatus{
			Name:          entry.ServerName,
			Server:        entry.ServerName,
			Status:        "ok",
			Description:   entry.SystemPrompt, // Use system prompt as description
			ToolsEnabled:  len(entry.Tools),
			FunctionNames: functionNames,
			Tools:         toolDetails,
		},
		Prompts:   promptDetails,
		Resources: resourceDetails,
	}

	return response, nil
}

// storeServerToolsInCache stores the discovered tools in the cache
func (api *StreamingAPI) storeServerToolsInCache(serverID string, response *EnhancedToolStatus) error {
	// Cache storage commented out since we need server config for cache keys
	// TODO: Implement proper cache storage for registry servers
	api.logger.Debugf("Cache storage skipped for server %s - configuration required for cache keys", serverID)
	return nil
}

// enhanceRegistryResponseWithCache adds cache information to registry servers
func (api *StreamingAPI) enhanceRegistryResponseWithCache(response MCPRegistryResponse) (*EnhancedMCPRegistryResponse, error) {
	cacheManager := mcpcache.GetCacheManager(api.logger)

	enhancedServers := make([]EnhancedMCPRegistryServer, 0, len(response.Servers))

	for _, server := range response.Servers {
		enhancedServer := EnhancedMCPRegistryServer{
			MCPRegistryServer: server,
		}

		// Try to find cached server by matching registry server name with MCP server names
		// This is a heuristic approach since registry names and MCP config names don't match exactly
		cachedEntry := api.findCachedServerByRegistryName(server, cacheManager)

		if cachedEntry != nil {
			enhancedServer.CacheStatus = &CacheStatus{
				IsCached:       true,
				ToolsCount:     len(cachedEntry.Tools),
				PromptsCount:   len(cachedEntry.Prompts),
				ResourcesCount: len(cachedEntry.Resources),
				LastUpdated:    cachedEntry.LastAccessed.Format(time.RFC3339),
			}
		}

		enhancedServers = append(enhancedServers, enhancedServer)
	}

	return &EnhancedMCPRegistryResponse{
		Servers:  enhancedServers,
		Metadata: response.Metadata,
	}, nil
}

// findCachedServerByRegistryName attempts to find a cached server by matching registry server name
func (api *StreamingAPI) findCachedServerByRegistryName(registryServer MCPRegistryServer, cacheManager *mcpcache.CacheManager) *mcpcache.CacheEntry {
	// Get all cached entries by scanning the cache directory
	cachedEntries := api.getAllCachedEntries(cacheManager)

	registryName := registryServer.Name

	// Strategy 1: Direct name match
	if entry, found := cachedEntries[registryName]; found {
		api.logger.Debugf("Found direct cache match: registry='%s' -> cache='%s'", registryName, entry.ServerName)
		return entry
	}

	// Debug: Log what we're looking for and what we have
	api.logger.Debugf("Looking for registry server: '%s'", registryName)
	api.logger.Debugf("Available cached entries: %v", func() []string {
		var keys []string
		for k := range cachedEntries {
			keys = append(keys, k)
		}
		return keys
	}())

	// Strategy 2: Extract package name from registry name and match
	// e.g., "io.github.containers/kubernetes-mcp-server" -> "kubernetes-mcp-server"
	if lastSlash := strings.LastIndex(registryName, "/"); lastSlash != -1 {
		packageName := registryName[lastSlash+1:]
		if entry, found := cachedEntries[packageName]; found {
			api.logger.Debugf("Found package cache match: registry='%s' -> cache='%s'", registryName, entry.ServerName)
			return entry
		}
	}

	// Strategy 3: Exact matching only - no fuzzy matching
	// This prevents false positives and ensures accurate cache status

	return nil
}

// getAllCachedEntries returns all cached entries from the cache manager
func (api *StreamingAPI) getAllCachedEntries(cacheManager *mcpcache.CacheManager) map[string]*mcpcache.CacheEntry {
	// Get all entries from the cache manager - this is completely dynamic
	allEntries := cacheManager.GetAllEntries()

	// Create a map with exact keys only
	entries := make(map[string]*mcpcache.CacheEntry)

	for cacheKey, entry := range allEntries {
		// Add the original cache key
		entries[cacheKey] = entry

		// Add the exact server name as a key
		entries[entry.ServerName] = entry

		// Add lowercase version for case-insensitive matching
		serverNameLower := strings.ToLower(entry.ServerName)
		entries[serverNameLower] = entry

		// Add package name if it's a registry server (contains slashes)
		if strings.Contains(entry.ServerName, "/") {
			lastSlash := strings.LastIndex(entry.ServerName, "/")
			packageName := entry.ServerName[lastSlash+1:]
			entries[packageName] = entry
			entries[strings.ToLower(packageName)] = entry
		}
	}

	api.logger.Debugf("Loaded %d cached entries for registry matching", len(allEntries))
	return entries
}

// extractKeywords extracts meaningful keywords from a server name
func (api *StreamingAPI) extractKeywords(name string) []string {
	// Remove common prefixes and suffixes
	cleaned := strings.ToLower(name)
	cleaned = strings.ReplaceAll(cleaned, "io.github.", "")
	cleaned = strings.ReplaceAll(cleaned, "io.", "")
	cleaned = strings.ReplaceAll(cleaned, "com.", "")
	cleaned = strings.ReplaceAll(cleaned, "ai.", "")
	cleaned = strings.ReplaceAll(cleaned, "-mcp-server", "")
	cleaned = strings.ReplaceAll(cleaned, "-mcp", "")
	cleaned = strings.ReplaceAll(cleaned, "mcp-server", "")
	cleaned = strings.ReplaceAll(cleaned, "mcp", "")

	// Split by common delimiters
	keywords := []string{}

	// Split by slash
	for _, part := range strings.Split(cleaned, "/") {
		if part != "" {
			keywords = append(keywords, part)
		}
	}

	// Split by dash and underscore
	for _, part := range strings.Split(cleaned, "-") {
		if part != "" && len(part) > 2 {
			keywords = append(keywords, part)
		}
	}

	for _, part := range strings.Split(cleaned, "_") {
		if part != "" && len(part) > 2 {
			keywords = append(keywords, part)
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	result := []string{}
	for _, keyword := range keywords {
		if !seen[keyword] && len(keyword) > 2 {
			seen[keyword] = true
			result = append(result, keyword)
		}
	}

	return result
}
