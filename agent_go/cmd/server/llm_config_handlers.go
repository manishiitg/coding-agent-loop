package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/azure"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/utils"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

var supportedLLMProviders = []string{
	"openrouter",
	"bedrock",
	"openai",
	"vertex",
	"anthropic",
	"azure",
	"z-ai",
	"kimi",
	"minimax",
	"minimax-coding-plan",
	"elevenlabs",
	"deepgram",
	"claude-code",
	"gemini-cli",
	"codex-cli",
	"cursor-cli",
}

const claudeCodeDisableAutoMemoryEnv = "CLAUDE_CODE_DISABLE_AUTO_MEMORY"

// getSupportedProviders returns the list of supported LLM providers based on environment configuration
func getSupportedProviders() []string {
	envValue := os.Getenv("SUPPORTED_LLM_PROVIDERS")
	if envValue == "" {
		return supportedLLMProviders
	}

	// Parse comma-separated list
	parts := strings.Split(envValue, ",")
	validProviders := make(map[string]bool)
	for _, p := range supportedLLMProviders {
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
		return supportedLLMProviders
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

func buildProviderCapabilities(ctx context.Context) map[string][]string {
	raw := buildLLMCapabilities(ctx, "all", false)
	grouped := make(map[string]map[string]bool)

	for capabilityName, value := range raw {
		section, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		providers, ok := section["providers"].([]llmCapabilityProvider)
		if !ok {
			continue
		}
		for _, provider := range providers {
			if strings.TrimSpace(provider.Provider) == "" {
				continue
			}
			if grouped[provider.Provider] == nil {
				grouped[provider.Provider] = make(map[string]bool)
			}
			grouped[provider.Provider][capabilityName] = true
		}
	}

	result := make(map[string][]string, len(grouped))
	for provider, capabilitySet := range grouped {
		capabilities := make([]string, 0, len(capabilitySet))
		for capabilityName := range capabilitySet {
			capabilities = append(capabilities, capabilityName)
		}
		sort.Strings(capabilities)
		result[provider] = capabilities
	}
	return result
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
	setProviderKeyFromEnv := func(provider llm.Provider, envNames ...string) {
		for _, envName := range envNames {
			if s := strings.TrimSpace(os.Getenv(envName)); s != "" {
				keys.SetKeyForProvider(provider, &s)
				return
			}
		}
	}

	setProviderKeyFromEnv(llm.ProviderOpenRouter, "OPENROUTER_API_KEY")
	setProviderKeyFromEnv(llm.ProviderOpenAI, "OPENAI_API_KEY")
	setProviderKeyFromEnv(llm.ProviderAnthropic, "ANTHROPIC_API_KEY")
	setProviderKeyFromEnv(llm.ProviderZAI, "ZAI_API_KEY")
	setProviderKeyFromEnv(llm.ProviderKimi, "KIMI_API_KEY")
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
	if s := os.Getenv("CURSOR_API_KEY"); s != "" {
		keys.CursorCLI = &s
	}
	if s := os.Getenv("MINIMAX_API_KEY"); s != "" {
		keys.MiniMax = &s
	}
	if s := os.Getenv("MINIMAX_CODING_PLAN_API_KEY"); s != "" {
		keys.MiniMaxCodingPlan = &s
	}
	if s := os.Getenv("ELEVENLABS_API_KEY"); s != "" {
		keys.ElevenLabs = &s
	}
	if s := os.Getenv("DEEPGRAM_API_KEY"); s != "" {
		keys.Deepgram = &s
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

type llmDiscoveryCandidate struct {
	ID               string   `json:"id"`
	Provider         string   `json:"provider"`
	ModelID          string   `json:"model_id"`
	ModelName        string   `json:"model_name,omitempty"`
	Label            string   `json:"label"`
	Kind             string   `json:"kind"`
	DetectionSource  string   `json:"detection_source"`
	AuthSource       string   `json:"auth_source,omitempty"`
	AuthConfigured   bool     `json:"auth_configured"`
	RuntimeCommand   string   `json:"runtime_command,omitempty"`
	RuntimeAvailable *bool    `json:"runtime_available,omitempty"`
	Usable           bool     `json:"usable"`
	Recommended      bool     `json:"recommended"`
	Reason           string   `json:"reason"`
	SetupHint        string   `json:"setup_hint,omitempty"`
	Options          []string `json:"options,omitempty"`
}

type llmDiscoveryResponse struct {
	Candidates []llmDiscoveryCandidate `json:"candidates"`
	Notes      []string                `json:"notes"`
}

func providerDisplayLabel(provider string) string {
	switch provider {
	case "codex-cli":
		return "OpenAI Codex CLI"
	case "cursor-cli":
		return "Cursor CLI"
	case "claude-code":
		return "Claude Code"
	case "gemini-cli":
		return "Gemini CLI"
	case "openai":
		return "OpenAI API"
	case "anthropic":
		return "Anthropic API"
	case "vertex":
		return "Gemini / Vertex"
	case "openrouter":
		return "OpenRouter"
	case "bedrock":
		return "Amazon Bedrock"
	case "azure":
		return "Azure AI"
	case "z-ai":
		return "Z.AI"
	case "kimi":
		return "Kimi"
	case "minimax":
		return "MiniMax"
	case "minimax-coding-plan":
		return "MiniMax Coding Plan"
	default:
		return provider
	}
}

func modelNameForProviderModel(provider, modelID string) string {
	for _, metadata := range utils.GetAllModelMetadata() {
		if metadata == nil {
			continue
		}
		if strings.EqualFold(metadata.Provider, provider) && metadata.ModelID == modelID {
			if metadata.ModelName != "" {
				return metadata.ModelName
			}
			return metadata.ModelID
		}
	}
	return modelID
}

func discoveryCandidateKind(provider string) string {
	switch provider {
	case "codex-cli", "cursor-cli", "claude-code", "gemini-cli":
		return "local_cli"
	default:
		return "api"
	}
}

func discoveryModelOptions(provider string) []string {
	switch provider {
	case "codex-cli":
		return []string{"codex-cli", "high", "medium", "low"}
	case "cursor-cli":
		return []string{"cursor-cli", "gpt-5", "sonnet-4-thinking", "sonnet-4"}
	case "claude-code":
		return []string{"claude-code", "high", "medium", "low"}
	case "gemini-cli":
		return []string{"auto", "high", "medium", "low"}
	case "kimi":
		return []string{"kimi-k2.6"}
	default:
		return nil
	}
}

func discoverySetupHint(provider string, runtimeMissing bool) string {
	if runtimeMissing {
		switch provider {
		case "codex-cli":
			return "Install Codex CLI so the codex command is available on the backend PATH."
		case "cursor-cli":
			return "Install Cursor CLI so the cursor-agent command is available on the backend PATH."
		case "claude-code":
			return "Install Claude Code so the claude command is available on the backend PATH."
		case "gemini-cli":
			return "Install Gemini CLI so the gemini command is available on the backend PATH."
		default:
			return "Install the provider CLI so its command is available on the backend PATH."
		}
	}

	switch provider {
	case "codex-cli":
		return "Run codex login or set CODEX_API_KEY, then test again."
	case "cursor-cli":
		return "Run cursor-agent login or set CURSOR_API_KEY, then test again."
	case "claude-code":
		return "Run claude to finish Claude Code authentication, then test again."
	case "gemini-cli":
		return "Set GEMINI_API_KEY or finish Gemini CLI authentication, then test again."
	default:
		return "Provider auth was not detected in the server environment or workspace provider keys."
	}
}

func buildLLMDiscovery(ctx context.Context) llmDiscoveryResponse {
	keys := MergedProviderAPIKeys(ctx)
	supported := getSupportedProviders()
	supportedSet := make(map[string]bool, len(supported))
	for _, provider := range supported {
		supportedSet[provider] = true
	}

	providerOrder := []string{
		"codex-cli",
		"cursor-cli",
		"claude-code",
		"gemini-cli",
		"kimi",
		"openai",
		"anthropic",
		"vertex",
		"openrouter",
		"bedrock",
		"azure",
		"z-ai",
		"minimax",
		"minimax-coding-plan",
	}

	candidates := make([]llmDiscoveryCandidate, 0, len(providerOrder))
	for _, provider := range providerOrder {
		if !supportedSet[provider] {
			continue
		}

		authConfigured, authSource := providerAuthConfigured(provider, keys)
		usable, runtimeCommand, runtimeOK := providerUsable(provider, authConfigured)
		modelID := llm.GetDefaultModel(llm.Provider(provider))
		if modelID == "" {
			continue
		}

		kind := discoveryCandidateKind(provider)
		discovered := authConfigured || usable
		if runtimeOK != nil && *runtimeOK {
			discovered = true
		}
		if !discovered && kind != "local_cli" {
			continue
		}

		source := "Environment or workspace auth"
		if runtimeOK != nil && *runtimeOK {
			source = "Local CLI"
		} else if runtimeOK != nil && !*runtimeOK {
			source = "CLI not found"
		}

		reason := "Ready to enable."
		setupHint := ""
		if runtimeOK != nil && !*runtimeOK {
			reason = "CLI runtime was not detected."
			setupHint = discoverySetupHint(provider, true)
		} else if !usable {
			reason = "Detected, but setup may be incomplete."
			setupHint = discoverySetupHint(provider, false)
		}

		candidates = append(candidates, llmDiscoveryCandidate{
			ID:               provider + ":" + modelID,
			Provider:         provider,
			ModelID:          modelID,
			ModelName:        modelNameForProviderModel(provider, modelID),
			Label:            providerDisplayLabel(provider),
			Kind:             kind,
			DetectionSource:  source,
			AuthSource:       authSource,
			AuthConfigured:   authConfigured,
			RuntimeCommand:   runtimeCommand,
			RuntimeAvailable: runtimeOK,
			Usable:           usable,
			Recommended:      usable,
			Reason:           reason,
			SetupHint:        setupHint,
			Options:          discoveryModelOptions(provider),
		})
	}

	return llmDiscoveryResponse{
		Candidates: candidates,
		Notes: []string{
			"Discovery only checks installed runtimes and configured auth. It does not send prompts or spend provider credits.",
			"Environment variables are read from the backend server process, not unrelated terminal tabs opened after startup.",
		},
	}
}

// handleDiscoverLLMSetup reports local/provider LLM setup candidates without running model calls.
func (api *StreamingAPI) handleDiscoverLLMSetup(w http.ResponseWriter, r *http.Request) {
	response := buildLLMDiscovery(r.Context())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode LLM discovery response", http.StatusInternalServerError)
		return
	}
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
	providers := []string{"azure", "bedrock", "openrouter", "openai", "anthropic", "vertex", "z-ai", "kimi", "minimax", "minimax-coding-plan"}

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
		} else if key := os.Getenv("ZAI_API_KEY"); provider == "z-ai" && key != "" {
			entry["api_key"] = key
		} else if key := os.Getenv("KIMI_API_KEY"); provider == "kimi" && key != "" {
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
		"zai_config":                 defaults.ZAIConfig,
		"kimi_config":                defaults.KimiConfig,
		"minimax_config":             defaults.MinimaxConfig,
		"minimax_coding_plan_config": defaults.MinimaxCodingPlanConfig,
		"elevenlabs_config":          defaults.ElevenLabsConfig,
		"deepgram_config":            defaults.DeepgramConfig,
		"available_models":           defaults.AvailableModels,
		"provider_capabilities":      buildProviderCapabilities(r.Context()),
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
			case "z-ai":
				stripSecrets("zai_config")
			case "kimi":
				stripSecrets("kimi_config")
			case "vertex":
				stripSecrets("vertex_config")
			case "minimax":
				stripSecrets("minimax_config")
			case "minimax-coding-plan":
				stripSecrets("minimax_coding_plan_config")
			case "elevenlabs":
				stripSecrets("elevenlabs_config")
			case "deepgram":
				stripSecrets("deepgram_config")
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

	api.populateValidationCredentialsFromMergedKeys(r.Context(), &req)
	log.Printf("Received API key validation request for provider: %s", req.Provider)
	response := validateProviderConfig(req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (api *StreamingAPI) populateValidationCredentialsFromMergedKeys(ctx context.Context, req *llm.APIKeyValidationRequest) {
	if req == nil {
		return
	}
	keys := MergedProviderAPIKeys(ctx)
	if keys == nil {
		return
	}
	setAPIKey := func(key *string) {
		if req.APIKey == "" && key != nil {
			req.APIKey = strings.TrimSpace(*key)
		}
	}

	switch strings.ToLower(strings.TrimSpace(req.Provider)) {
	case "openrouter":
		setAPIKey(keys.OpenRouter)
	case "openai":
		setAPIKey(keys.OpenAI)
	case "anthropic":
		setAPIKey(keys.Anthropic)
	case "vertex":
		setAPIKey(keys.Vertex)
	case "gemini-cli":
		setAPIKey(keys.GeminiCLI)
	case "codex-cli":
		setAPIKey(keys.CodexCLI)
	case "cursor-cli":
		setAPIKey(keys.CursorCLI)
	case "minimax":
		setAPIKey(keys.MiniMax)
	case "minimax-coding-plan":
		setAPIKey(keys.MiniMaxCodingPlan)
	case "elevenlabs":
		setAPIKey(keys.ElevenLabs)
	case "deepgram":
		setAPIKey(keys.Deepgram)
	case "z-ai":
		setAPIKey(keys.ZAI)
	case "kimi":
		setAPIKey(keys.Kimi)
	case "azure":
		if keys.Azure != nil {
			if req.APIKey == "" {
				req.APIKey = strings.TrimSpace(keys.Azure.APIKey)
			}
			if strings.TrimSpace(keys.Azure.Endpoint) != "" {
				if req.Options == nil {
					req.Options = map[string]interface{}{}
				}
				if _, ok := req.Options["endpoint"]; !ok {
					req.Options["endpoint"] = strings.TrimSpace(keys.Azure.Endpoint)
				}
			}
		}
	}
}

func validateProviderConfig(req llm.APIKeyValidationRequest) llm.APIKeyValidationResponse {
	switch req.Provider {
	case "claude-code":
		return validateClaudeCodeCLI()
	case "gemini-cli":
		return validateGeminiCLI(req.APIKey)
	case "codex-cli":
		return validateCodexCLI(req.APIKey)
	case "cursor-cli":
		return validateCursorCLI(req.APIKey, req.ModelID)
	case "kimi":
		return llm.ValidateAPIKey(req)
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

	cmd := exec.CommandContext(ctx, "claude", "--print", "Say hello in one short sentence.")
	// Remove ANTHROPIC_API_KEY from env so Claude Code uses its own OAuth credentials
	// instead of picking up the server's API key (which may have different billing).
	env := os.Environ()
	filteredEnv := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") && !strings.HasPrefix(e, claudeCodeDisableAutoMemoryEnv+"=") {
			filteredEnv = append(filteredEnv, e)
		}
	}
	filteredEnv = append(filteredEnv, claudeCodeDisableAutoMemoryEnv+"=1")
	cmd.Env = filteredEnv
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
			Message: "Gemini CLI not found. Install it with: npm install -g @google/gemini-cli (see https://github.com/google-gemini/gemini-cli)",
		}
	}
	log.Printf("[GEMINI-CLI VALIDATION] CLI found at: %s", geminiPath)

	// Step 1.5: Ensure Gemini CLI settings are configured for API key auth
	// Without this, the CLI defaults to oauth-personal and fails in non-interactive mode.
	ensureGeminiAPIKeyAuth()

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

// ensureGeminiAPIKeyAuth ensures Gemini CLI settings.json uses "gemini-api-key" auth type
// instead of the default "oauth-personal" which requires an interactive terminal.
func ensureGeminiAPIKeyAuth() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[GEMINI-CLI] Could not determine home dir: %v", err)
		return
	}
	geminiDir := filepath.Join(homeDir, ".gemini")
	settingsPath := filepath.Join(geminiDir, "settings.json")

	// Read existing settings
	var settings map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	// Check if already set to gemini-api-key
	if security, ok := settings["security"].(map[string]interface{}); ok {
		if auth, ok := security["auth"].(map[string]interface{}); ok {
			if auth["selectedType"] == "gemini-api-key" {
				return // already configured
			}
		}
	}

	// Set auth type to gemini-api-key
	settings["security"] = map[string]interface{}{
		"auth": map[string]interface{}{
			"selectedType": "gemini-api-key",
		},
	}

	if err := os.MkdirAll(geminiDir, 0700); err != nil {
		log.Printf("[GEMINI-CLI] Could not create .gemini dir: %v", err)
		return
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		log.Printf("[GEMINI-CLI] Could not marshal settings: %v", err)
		return
	}
	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		log.Printf("[GEMINI-CLI] Could not write settings: %v", err)
		return
	}
	log.Printf("[GEMINI-CLI] Configured settings.json for API key auth")
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

	outputFile, err := os.CreateTemp("", "codex-cli-validation-*.txt")
	if err != nil {
		log.Printf("[CODEX-CLI VALIDATION] Failed to create output temp file: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Could not create a temporary file for Codex CLI validation.",
		}
	}
	outputPath := outputFile.Name()
	_ = outputFile.Close()
	defer os.Remove(outputPath)

	cmd := exec.CommandContext(ctx,
		"codex",
		"exec",
		"--sandbox", "workspace-write",
		"--skip-git-repo-check",
		"--output-last-message", outputPath,
		"-c", `model_reasoning_effort="medium"`,
		"Say hello in one short sentence.",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Only pass API key if explicitly provided from frontend.
	// Do NOT fall back to OPENAI_API_KEY — Codex CLI has its own auth
	// (via `codex login` stored in ~/.codex/auth.json) which should be preferred.
	if apiKey == "" {
		apiKey = os.Getenv("CODEX_API_KEY")
	}
	if apiKey != "" {
		cmd.Env = append(os.Environ(), "CODEX_API_KEY="+apiKey)
	}
	output, err := cmd.Output()
	if err != nil {
		errMsg := stderr.String()
		if strings.TrimSpace(errMsg) == "" {
			errMsg = string(output)
		}
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

	responseBytes, readErr := os.ReadFile(outputPath)
	responseText := strings.TrimSpace(string(responseBytes))
	if readErr != nil || responseText == "" {
		responseText = strings.TrimSpace(string(output))
	}
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

// validateCursorCLI validates Cursor Agent CLI through the real tmux adapter path.
func validateCursorCLI(apiKey, modelID string) llm.APIKeyValidationResponse {
	log.Printf("[CURSOR-CLI VALIDATION] Starting CLI validation")

	cursorPath, err := exec.LookPath("cursor-agent")
	if err != nil {
		log.Printf("[CURSOR-CLI VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Cursor Agent CLI not found. Install it with: curl https://cursor.com/install -fsS | bash",
		}
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		log.Printf("[CURSOR-CLI VALIDATION] tmux not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "tmux not found. Cursor CLI integration requires tmux for interactive mode.",
		}
	}
	log.Printf("[CURSOR-CLI VALIDATION] CLI found at: %s", cursorPath)

	if modelID == "" {
		modelID = "cursor-cli"
	}

	workspaceDir, err := os.MkdirTemp("", "cursor-cli-validation-*")
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Could not create a temporary workspace for Cursor CLI validation.",
		}
	}
	defer os.RemoveAll(workspaceDir)

	keys := &llm.ProviderAPIKeys{}
	if apiKey == "" {
		apiKey = os.Getenv("CURSOR_API_KEY")
	}
	if strings.TrimSpace(apiKey) != "" {
		keys.CursorCLI = &apiKey
	}

	model, err := llm.InitializeLLM(llm.Config{
		Provider: llm.ProviderCursorCLI,
		ModelID:  modelID,
		APIKeys:  keys,
		Context:  context.Background(),
	})
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Failed to initialize Cursor CLI: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := model.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly: Cursor CLI is working."},
			},
		},
	}, llm.WithCursorWorkingDir(workspaceDir))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Cursor CLI timed out after 90s. Check that you are authenticated with Cursor Agent CLI.",
			}
		}
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Cursor CLI error: %s", strings.TrimSpace(err.Error())),
		}
	}

	responseText := ""
	if resp != nil && len(resp.Choices) > 0 {
		responseText = strings.TrimSpace(resp.Choices[0].Content)
	}
	if responseText == "" {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Cursor CLI returned an empty response. Check authentication with 'cursor-agent login'.",
		}
	}

	log.Printf("[CURSOR-CLI VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Cursor CLI is working. Response: %s", responseText),
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
