package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/azure"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/utils"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

// getSupportedProviders returns the list of supported LLM providers based on environment configuration
func getSupportedProviders() []string {
	allProviders := []string{"openrouter", "bedrock", "openai", "vertex", "anthropic", "azure", "minimax", "minimax-coding-plan", "claude-code", "gemini-cli", "codex-cli"}
	envValue := os.Getenv("SUPPORTED_LLM_PROVIDERS")
	if envValue == "" {
		return allProviders
	}

	// Parse comma-separated list
	parts := strings.Split(envValue, ",")
	validProviders := make(map[string]bool)
	for _, p := range allProviders {
		validProviders[p] = true
	}

	var supported []string
	for _, part := range parts {
		provider := strings.ToLower(strings.TrimSpace(part))
		if provider == "" {
			continue
		}
		if validProviders[provider] {
			supported = append(supported, provider)
		} else {
			log.Printf("Warning: ignoring invalid provider '%s' in SUPPORTED_LLM_PROVIDERS", part)
		}
	}

	// If no valid providers found, return all
	if len(supported) == 0 {
		log.Printf("Warning: no valid providers found in SUPPORTED_LLM_PROVIDERS, enabling all providers")
		return allProviders
	}

	return supported
}

// isGlobalLLMConfigLocked returns true if all LLM configuration is locked
func isGlobalLLMConfigLocked() bool {
	val := os.Getenv("LLM_CONFIG_LOCKED")
	return val == "true" || val == "1"
}

// getLockedProviders returns a list of locked providers from the environment variable
func getLockedProviders() []string {
	val := os.Getenv("LLM_CONFIG_LOCKED")
	if val == "true" || val == "1" {
		return []string{"all"}
	}
	if val == "" || val == "false" || val == "0" {
		return []string{}
	}
	// Split by comma
	parts := strings.Split(val, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(strings.ToLower(parts[i]))
	}
	return parts
}

// isProviderLocked returns true if the specific provider is locked
func isProviderLocked(provider string) bool {
	if provider == "" {
		return false
	}
	locked := getLockedProviders()
	for _, p := range locked {
		if p == "all" || p == strings.ToLower(provider) {
			return true
		}
	}
	return false
}

// isAllowedDefaultLLM returns true when (provider, modelID) is in the default published LLMs list (for locked mode).
func isAllowedDefaultLLM(provider, modelID string) bool {
	if provider == "" || modelID == "" {
		return false
	}
	defaults := llm.GetLLMDefaults()
	// Only restrict to defaults if the *specific* provider is locked
	if !isProviderLocked(provider) {
		return true
	}

	// Allow any model listed in AvailableModels for this provider
	if models, ok := defaults.AvailableModels[provider]; ok {
		for _, m := range models {
			if m == modelID {
				return true
			}
		}
	}

	list := getDefaultPublishedLLMs(true, defaults.PrimaryConfig)
	for _, entry := range list {
		p, _ := entry["provider"].(string)
		m, _ := entry["model_id"].(string)
		if p == provider && m == modelID {
			return true
		}
	}
	return false
}

// getPrimaryProviderAndModelFromDefaults extracts provider and model_id from llm.GetLLMDefaults().PrimaryConfig.
func getPrimaryProviderAndModelFromDefaults() (provider, modelID string) {
	defaults := llm.GetLLMDefaults()
	bytes, err := json.Marshal(defaults.PrimaryConfig)
	if err != nil {
		return "openrouter", llm.GetDefaultModel(llm.Provider("openrouter"))
	}
	var m map[string]interface{}
	if err := json.Unmarshal(bytes, &m); err != nil {
		return "openrouter", llm.GetDefaultModel(llm.Provider("openrouter"))
	}
	if p, _ := m["provider"].(string); p != "" {
		provider = p
	} else {
		provider = "openrouter"
	}
	if mid, _ := m["model_id"].(string); mid != "" {
		modelID = mid
	} else {
		modelID = llm.GetDefaultModel(llm.Provider(provider))
	}
	return provider, modelID
}

// buildProviderAPIKeysFromEnv builds llm.ProviderAPIKeys from environment variables (for locked mode).
func buildProviderAPIKeysFromEnv() *llm.ProviderAPIKeys {
	keys := &llm.ProviderAPIKeys{}
	if s := os.Getenv("OPENROUTER_API_KEY"); s != "" {
		keys.OpenRouter = &s
	}
	if s := os.Getenv("OPENAI_API_KEY"); s != "" {
		keys.OpenAI = &s
	}
	if s := os.Getenv("ANTHROPIC_API_KEY"); s != "" {
		keys.Anthropic = &s
	}
	if s := os.Getenv("VERTEX_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GOOGLE_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GEMINI_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); s != "" {
		keys.Vertex = &s
	}
	if region := os.Getenv("BEDROCK_REGION"); region != "" {
		keys.Bedrock = &llm.BedrockConfig{Region: region}
	}
	if s := os.Getenv("GEMINI_API_KEY"); s != "" {
		keys.GeminiCLI = &s
	}
	// Codex CLI: only use explicit CODEX_API_KEY (not OPENAI_API_KEY).
	// Codex CLI has its own stored auth via `codex login`.
	if s := os.Getenv("CODEX_API_KEY"); s != "" {
		keys.CodexCLI = &s
	}
	if s := os.Getenv("MINIMAX_API_KEY"); s != "" {
		keys.MiniMax = &s
	}
	if s := os.Getenv("MINIMAX_CODING_PLAN_API_KEY"); s != "" {
		keys.MiniMaxCodingPlan = &s
	}
	if endpoint := os.Getenv("AZURE_AI_ENDPOINT"); endpoint != "" {
		apiKey := os.Getenv("AZURE_AI_API_KEY")
		apiVer := os.Getenv("AZURE_AI_API_VERSION")
		region := os.Getenv("AZURE_AI_REGION")
		keys.Azure = &llm.AzureAPIConfig{
			Endpoint:   endpoint,
			APIKey:     apiKey,
			APIVersion: apiVer,
			Region:     region,
		}
	}
	return keys
}

// stripSecretsFromMap recursively removes api_key, endpoint, and other sensitive fields from m (for locked mode).
// endpoint is stripped so Azure tenant URLs (e.g. https://tenant-name.openai.azure.com/) are not sent to the client.
func stripSecretsFromMap(m map[string]interface{}) {
	delete(m, "api_key")
	delete(m, "endpoint")
	for _, v := range m {
		if nested, ok := v.(map[string]interface{}); ok {
			stripSecretsFromMap(nested)
		}
	}
}

// getDefaultPublishedLLMs returns the list of default published LLMs from env, file, or primary config.
// When locked is true, entries must not include api_key or endpoint (Azure tenant URL).
func getDefaultPublishedLLMs(locked bool, primaryConfig interface{}) []map[string]interface{} {
	stripEntrySecrets := func(entry map[string]interface{}) {
		delete(entry, "api_key")
		delete(entry, "endpoint")
	}
	// 1) Try DEFAULT_PUBLISHED_LLMS (JSON string)
	if s := os.Getenv("DEFAULT_PUBLISHED_LLMS"); s != "" {
		var list []map[string]interface{}
		if err := json.Unmarshal([]byte(s), &list); err == nil && len(list) > 0 {
			for i := range list {
				provider, _ := list[i]["provider"].(string)
				if locked || isProviderLocked(provider) {
					stripEntrySecrets(list[i])
				}
			}
			return list
		}
	}
	// 2) Try DEFAULT_PUBLISHED_LLMS_PATH (path to JSON file)
	if path := os.Getenv("DEFAULT_PUBLISHED_LLMS_PATH"); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			var list []map[string]interface{}
			if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
				for i := range list {
					provider, _ := list[i]["provider"].(string)
					if locked || isProviderLocked(provider) {
						stripEntrySecrets(list[i])
					}
				}
				return list
			}
		}
	}

	// 3) Auto-generate defaults from AvailableModels for locked providers
	var entries []map[string]interface{}
	defaults := llm.GetLLMDefaults()
	providers := []string{"azure", "bedrock", "openrouter", "openai", "anthropic", "vertex", "minimax", "minimax-coding-plan"}

	for _, p := range providers {
		// If provider is locked (or global lock is on), include its available models
		if isProviderLocked(p) || locked {
			if models, ok := defaults.AvailableModels[p]; ok {
				for _, m := range models {
					entry := map[string]interface{}{
						"id":       "default-" + p + "-" + strings.ReplaceAll(m, "/", "-"),
						"name":     m, // Simple name
						"provider": p,
						"model_id": m,
					}
					// Secrets are stripped by default since we don't add them here
					entries = append(entries, entry)
				}
			}
		}
	}

	if len(entries) > 0 {
		return entries
	}

	// 4) Fallback: Build one entry from primary config if nothing else found
	var provider, modelID string
	if pc, ok := primaryConfig.(map[string]interface{}); ok {
		if p, _ := pc["provider"].(string); p != "" {
			provider = p
		}
		if m, _ := pc["model_id"].(string); m != "" {
			modelID = m
		}
	}
	if provider == "" {
		provider = "openrouter"
	}
	if modelID == "" {
		modelID = llm.GetDefaultModel(llm.Provider(provider))
	}
	entry := map[string]interface{}{
		"id":       "default-" + provider + "-" + strings.ReplaceAll(modelID, "/", "-"),
		"name":     "Default (" + provider + ")",
		"provider": provider,
		"model_id": modelID,
	}

	isLocked := locked || isProviderLocked(provider)

	if !isLocked {
		if key := os.Getenv("OPENROUTER_API_KEY"); provider == "openrouter" && key != "" {
			entry["api_key"] = key
		} else if key := os.Getenv("OPENAI_API_KEY"); provider == "openai" && key != "" {
			entry["api_key"] = key
		} else if key := os.Getenv("ANTHROPIC_API_KEY"); provider == "anthropic" && key != "" {
			entry["api_key"] = key
		}
	}
	return []map[string]interface{}{entry}
}

// handleGetLLMDefaults returns default LLM configurations from environment variables.
// When LLM_CONFIG_LOCKED=true (or specific provider is locked), api_key and endpoint are stripped.
func (api *StreamingAPI) handleGetLLMDefaults(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request for LLM defaults")
	defaults := llm.GetLLMDefaults()

	globalLocked := isGlobalLLMConfigLocked()
	lockedProviders := getLockedProviders()

	// Build response (same shape as before)
	response := map[string]interface{}{
		"primary_config":             defaults.PrimaryConfig,
		"openrouter_config":          defaults.OpenrouterConfig,
		"bedrock_config":             defaults.BedrockConfig,
		"openai_config":              defaults.OpenaiConfig,
		"anthropic_config":           defaults.AnthropicConfig,
		"azure_config":               defaults.AzureConfig,
		"minimax_config":             defaults.MinimaxConfig,
		"minimax_coding_plan_config": defaults.MinimaxCodingPlanConfig,
		"available_models":           defaults.AvailableModels,
		"supported_providers":        getSupportedProviders(),
		"locked_providers":           lockedProviders,
	}

	// Helper to safely strip secrets from a specific config map
	stripSecrets := func(configKey string) {
		if cfg, ok := response[configKey].(map[string]interface{}); ok {
			delete(cfg, "api_key")
			delete(cfg, "endpoint")
			response[configKey] = cfg
		}
	}

	// Strip secrets based on locking status
	if globalLocked {
		// Strip from all
		stripSecretsFromMap(response)
	} else {
		// Strip from specifically locked providers
		for _, p := range lockedProviders {
			switch p {
			case "openrouter":
				stripSecrets("openrouter_config")
			case "bedrock":
				stripSecrets("bedrock_config")
			case "openai":
				stripSecrets("openai_config")
			case "anthropic":
				stripSecrets("anthropic_config")
			case "azure":
				stripSecrets("azure_config")
			case "vertex":
				stripSecrets("vertex_config")
			case "minimax":
				stripSecrets("minimax_config")
			case "minimax-coding-plan":
				stripSecrets("minimax_coding_plan_config")
			}
		}
	}

	response["llm_config_locked"] = globalLocked
	response["default_published_llms"] = getDefaultPublishedLLMs(globalLocked, defaults.PrimaryConfig)
	response["default_published_llms_locked"] = globalLocked

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTestImageGen tests image generation config by attempting to generate a single test image
func (api *StreamingAPI) handleTestImageGen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		ModelID  string `json:"model_id"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	cfg := virtualtools.ImageGenExecutorConfig{
		Provider: req.Provider,
		ModelID:  req.ModelID,
		APIKey:   req.APIKey,
	}
	if cfg.Provider == "" {
		cfg.Provider = "vertex"
	}
	if cfg.ModelID == "" {
		cfg.ModelID = "imagen-4.0-generate-001"
	}

	executor := virtualtools.CreateImageGenExecutor(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	_, err := executor(ctx, map[string]any{"prompt": "a simple red circle on white background"})

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"valid": false, "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"valid": true, "message": "Image generation is working"})
}

// handleValidateAPIKey validates API keys for OpenRouter, OpenAI, Bedrock, Vertex, Anthropic, and Claude Code
func (api *StreamingAPI) handleValidateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req llm.APIKeyValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode API key validation request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Received API key validation request for provider: %s", req.Provider)
	response := validateProviderConfig(req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func validateProviderConfig(req llm.APIKeyValidationRequest) llm.APIKeyValidationResponse {
	switch req.Provider {
	case "claude-code":
		return validateClaudeCodeCLI()
	case "gemini-cli":
		return validateGeminiCLI(req.APIKey)
	case "codex-cli":
		return validateCodexCLI(req.APIKey)
	default:
		return llm.ValidateAPIKey(req)
	}
}

// validateClaudeCodeCLI validates the Claude Code CLI by checking it exists and sending a test prompt
func validateClaudeCodeCLI() llm.APIKeyValidationResponse {
	log.Printf("[CLAUDE-CODE VALIDATION] Starting CLI validation")

	// Step 1: Check if claude CLI is on PATH
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		log.Printf("[CLAUDE-CODE VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Claude Code CLI not found. Install it with: npm install -g @anthropic-ai/claude-code",
		}
	}
	log.Printf("[CLAUDE-CODE VALIDATION] CLI found at: %s", claudePath)

	// Step 2: Send a test prompt via the CLI and check for a response
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "--print", "--dangerously-skip-permissions", "Say hello in one short sentence.")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(output)
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[CLAUDE-CODE VALIDATION] CLI test timed out")
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Claude Code CLI timed out after 60s. Check that you are authenticated (run 'claude' to log in).",
			}
		}
		log.Printf("[CLAUDE-CODE VALIDATION] CLI test failed: %v — output: %s", err, errMsg)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Claude Code CLI error: %s", strings.TrimSpace(errMsg)),
		}
	}

	responseText := strings.TrimSpace(string(output))
	if responseText == "" {
		log.Printf("[CLAUDE-CODE VALIDATION] CLI returned empty response")
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Claude Code CLI returned an empty response. Check authentication with 'claude'.",
		}
	}

	log.Printf("[CLAUDE-CODE VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Claude Code CLI is working. Response: %s", responseText),
	}
}

// validateGeminiCLI validates the Gemini CLI by checking it exists and sending a test prompt
func validateGeminiCLI(apiKey string) llm.APIKeyValidationResponse {
	log.Printf("[GEMINI-CLI VALIDATION] Starting CLI validation")

	// Step 1: Check if gemini CLI is on PATH
	geminiPath, err := exec.LookPath("gemini")
	if err != nil {
		log.Printf("[GEMINI-CLI VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Gemini CLI not found. Install it with: npm install -g @anthropic-ai/gemini-cli (see https://github.com/google-gemini/gemini-cli)",
		}
	}
	log.Printf("[GEMINI-CLI VALIDATION] CLI found at: %s", geminiPath)

	// Step 2: Send a test prompt via the CLI and check for a response
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gemini", "--approval-mode", "yolo", "--prompt", "Say hello in one short sentence.")
	// Pass API key as env var if provided (from frontend or server .env)
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey != "" {
		cmd.Env = append(os.Environ(), "GEMINI_API_KEY="+apiKey)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(output)
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[GEMINI-CLI VALIDATION] CLI test timed out")
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Gemini CLI timed out after 60s. Check that you are authenticated (run 'gemini' to log in).",
			}
		}
		log.Printf("[GEMINI-CLI VALIDATION] CLI test failed: %v — output: %s", err, errMsg)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Gemini CLI error: %s", strings.TrimSpace(errMsg)),
		}
	}

	responseText := strings.TrimSpace(string(output))
	if responseText == "" {
		log.Printf("[GEMINI-CLI VALIDATION] CLI returned empty response")
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Gemini CLI returned an empty response. Check authentication with 'gemini'.",
		}
	}

	log.Printf("[GEMINI-CLI VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Gemini CLI is working. Response: %s", responseText),
	}
}

// validateCodexCLI validates the OpenAI Codex CLI by checking it exists and sending a test prompt
func validateCodexCLI(apiKey string) llm.APIKeyValidationResponse {
	log.Printf("[CODEX-CLI VALIDATION] Starting CLI validation")

	// Step 1: Check if codex CLI is on PATH
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		log.Printf("[CODEX-CLI VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Codex CLI not found. Install it with: npm install -g @openai/codex",
		}
	}
	log.Printf("[CODEX-CLI VALIDATION] CLI found at: %s", codexPath)

	// Step 2: Send a test prompt via the CLI and check for a response
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "codex", "exec", "--full-auto", "--skip-git-repo-check", "-c", `model_reasoning_effort="medium"`, "Say hello in one short sentence.")
	// Only pass API key if explicitly provided from frontend.
	// Do NOT fall back to OPENAI_API_KEY — Codex CLI has its own auth
	// (via `codex login` stored in ~/.codex/auth.json) which should be preferred.
	if apiKey == "" {
		apiKey = os.Getenv("CODEX_API_KEY")
	}
	if apiKey != "" {
		cmd.Env = append(os.Environ(), "CODEX_API_KEY="+apiKey)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(output)
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[CODEX-CLI VALIDATION] CLI test timed out")
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Codex CLI timed out after 60s. Check that you are authenticated (run 'codex login').",
			}
		}
		log.Printf("[CODEX-CLI VALIDATION] CLI test failed: %v — output: %s", err, errMsg)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Codex CLI error: %s", strings.TrimSpace(errMsg)),
		}
	}

	responseText := strings.TrimSpace(string(output))
	if responseText == "" {
		log.Printf("[CODEX-CLI VALIDATION] CLI returned empty response")
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Codex CLI returned an empty response. Check authentication with 'codex login'.",
		}
	}

	log.Printf("[CODEX-CLI VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Codex CLI is working. Response: %s", responseText),
	}
}

// handleGetModelMetadata returns metadata for all available models across providers
func (api *StreamingAPI) handleGetModelMetadata(w http.ResponseWriter, r *http.Request) {
	// Get all model metadata from the utility function
	models := utils.GetAllModelMetadata()

	response := map[string]interface{}{
		"models": models,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// AzureDeployedModelsRequest represents the request body for fetching Azure deployed models
type AzureDeployedModelsRequest struct {
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key"`
}

// handleGetAzureDeployedModels returns only the models deployed in the user's Azure resource
func (api *StreamingAPI) handleGetAzureDeployedModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AzureDeployedModelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Endpoint == "" || req.APIKey == "" {
		http.Error(w, "endpoint and api_key are required", http.StatusBadRequest)
		return
	}

	// Fetch deployed models from Azure
	models, err := azure.GetAzureDeployedModels(req.Endpoint, req.APIKey)
	if err != nil {
		// Return error response - allows frontend to fall back to manual entry
		response := map[string]interface{}{
			"models": []interface{}{},
			"error":  err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := map[string]interface{}{
		"models": models,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
