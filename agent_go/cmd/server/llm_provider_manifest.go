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
	"sync"
	"time"

	"github.com/gorilla/mux"
	llm "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/utils"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

// providerManifestEntry is the per-provider metadata returned by the manifest endpoint.
type providerManifestEntry struct {
	ID                    string                            `json:"id"`
	DisplayName           string                            `json:"display_name"`
	Description           string                            `json:"description"`
	Kind                  string                            `json:"kind"`
	IntegrationKind       string                            `json:"integration_kind"`
	ModelSelectionMode    string                            `json:"model_selection_mode"`
	AuthDescription       string                            `json:"auth_description"`
	RuntimeCommand        string                            `json:"runtime_command,omitempty"`
	RuntimeAvailable      *bool                             `json:"runtime_available,omitempty"`
	AuthConfigured        bool                              `json:"auth_configured"`
	AuthSource            string                            `json:"auth_source,omitempty"`
	Usable                bool                              `json:"usable"`
	SetupHint             string                            `json:"setup_hint,omitempty"`
	RequiresAPIKey        bool                              `json:"requires_api_key"`
	SupportsDynamicModels bool                              `json:"supports_dynamic_models"`
	DefaultModelID        string                            `json:"default_model_id"`
	DefaultTierModels     *llm.CodingAgentDefaultTierModels `json:"default_tier_models,omitempty"`
	Models                []*llmtypes.ModelMetadata         `json:"models"`
	Capabilities          []string                          `json:"capabilities"`
	CodingAgent           *providerCodingAgentInfo          `json:"coding_agent,omitempty"`
	APIKeyEnv             string                            `json:"api_key_env,omitempty"`
	APIKeyURL             string                            `json:"api_key_url,omitempty"`
}

type providerCodingAgentInfo struct {
	Transport         string `json:"transport"`
	SupportsLiveInput bool   `json:"supports_live_input"`
	SupportsInterrupt bool   `json:"supports_interrupt"`
}

type integrationKindInfo struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type providerManifestResponse struct {
	Providers        []providerManifestEntry        `json:"providers"`
	IntegrationKinds map[string]integrationKindInfo `json:"integration_kinds"`
	ProviderOrder    []string                       `json:"provider_order"`
}

type dynamicModelEntry struct {
	ModelID       string  `json:"model_id"`
	ModelName     string  `json:"model_name"`
	Group         string  `json:"group,omitempty"`
	IsDefault     bool    `json:"is_default,omitempty"`
	ContextWindow int     `json:"context_window,omitempty"`
	CostInput     float64 `json:"cost_input,omitempty"`
	CostOutput    float64 `json:"cost_output,omitempty"`
}

type dynamicModelsResponse struct {
	Provider           string              `json:"provider"`
	ModelSelectionMode string              `json:"model_selection_mode"`
	Models             []dynamicModelEntry `json:"models"`
	Groups             []string            `json:"groups,omitempty"`
	SupportsCustom     bool                `json:"supports_custom_model,omitempty"`
	CustomModelHint    string              `json:"custom_model_hint,omitempty"`
	Source             string              `json:"source"`
	CachedAt           string              `json:"cached_at,omitempty"`
	CacheTTLSeconds    int                 `json:"cache_ttl_seconds,omitempty"`
}

// --- Provider static metadata ---

type providerStaticInfo struct {
	displayName     string
	description     string
	integrationKind string
	authDescription string
	requiresAPIKey  bool
	apiKeyEnv       string
	apiKeyURL       string
}

var providerStaticInfoMap = map[string]providerStaticInfo{
	"codex-cli": {
		displayName:     "OpenAI Codex CLI",
		description:     "Uses the locally installed codex CLI. Authentication via codex login or CODEX_API_KEY.",
		integrationKind: "coding_agent",
		authDescription: "Local CLI (API key optional)",
		requiresAPIKey:  false,
	},
	"cursor-cli": {
		displayName:     "Cursor CLI",
		description:     "Uses cursor-agent through tmux. Supports 100+ models via Cursor subscription.",
		integrationKind: "coding_agent",
		authDescription: "Local CLI (API key optional)",
		requiresAPIKey:  false,
	},
	"agy-cli": {
		displayName:     "Antigravity CLI (Alpha)",
		description:     "Alpha local coding agent through agy and tmux. Requires local Antigravity sign-in; structured JSON transport is not supported.",
		integrationKind: "coding_agent",
		authDescription: "Local CLI (Agy sign-in)",
		requiresAPIKey:  false,
	},
	"opencode-cli": {
		displayName:     "OpenCode CLI",
		description:     "Uses the locally installed opencode CLI through tmux. Supports multiple model providers.",
		integrationKind: "coding_agent",
		authDescription: "Local CLI (API key optional)",
		requiresAPIKey:  false,
	},
	"claude-code": {
		displayName:     "Claude Code",
		description:     "Uses the locally installed claude CLI. Handles its own authentication, model selection, and tool execution.",
		integrationKind: "coding_agent",
		authDescription: "Local CLI (no API key)",
		requiresAPIKey:  false,
	},
	"gemini-cli": {
		displayName:     "Gemini CLI",
		description:     "Uses the locally installed gemini CLI. Provide a Gemini API key or use gcloud auth.",
		integrationKind: "coding_agent",
		authDescription: "Local CLI (API key optional)",
		requiresAPIKey:  false,
		apiKeyEnv:       "GEMINI_API_KEY",
		apiKeyURL:       "https://aistudio.google.com/apikey",
	},
	"openai": {
		displayName:     "OpenAI API",
		description:     "Direct OpenAI API access for GPT models.",
		integrationKind: "api_model",
		authDescription: "API key required",
		requiresAPIKey:  true,
		apiKeyEnv:       "OPENAI_API_KEY",
		apiKeyURL:       "https://platform.openai.com/api-keys",
	},
	"anthropic": {
		displayName:     "Anthropic API",
		description:     "Direct Anthropic API access for Claude models.",
		integrationKind: "api_model",
		authDescription: "API key required",
		requiresAPIKey:  true,
		apiKeyEnv:       "ANTHROPIC_API_KEY",
		apiKeyURL:       "https://console.anthropic.com/settings/keys",
	},
	"vertex": {
		displayName:     "Gemini / Vertex",
		description:     "Google Vertex AI or Gemini API for Google models.",
		integrationKind: "api_model",
		authDescription: "API key or ADC required",
		requiresAPIKey:  true,
		apiKeyEnv:       "VERTEX_API_KEY",
		apiKeyURL:       "https://aistudio.google.com/apikey",
	},
	"bedrock": {
		displayName:     "Amazon Bedrock",
		description:     "AWS Bedrock for Claude, Titan, and other models.",
		integrationKind: "api_model",
		authDescription: "AWS region + credentials",
		requiresAPIKey:  true,
		apiKeyEnv:       "BEDROCK_REGION",
	},
	"azure": {
		displayName:     "Azure AI",
		description:     "Azure OpenAI Service for GPT and other deployed models.",
		integrationKind: "api_model",
		authDescription: "Endpoint + API key required",
		requiresAPIKey:  true,
		apiKeyEnv:       "AZURE_AI_API_KEY",
	},
	"minimax": {
		displayName:     "MiniMax",
		description:     "MiniMax API for speech, music, and audio models.",
		integrationKind: "audio_provider",
		authDescription: "API key required",
		requiresAPIKey:  true,
		apiKeyEnv:       "MINIMAX_API_KEY",
	},
	"elevenlabs": {
		displayName:     "ElevenLabs",
		description:     "Speech synthesis, voice cloning, and audio generation.",
		integrationKind: "audio_provider",
		authDescription: "API key required",
		requiresAPIKey:  true,
		apiKeyEnv:       "ELEVENLABS_API_KEY",
		apiKeyURL:       "https://elevenlabs.io/app/settings/api-keys",
	},
	"deepgram": {
		displayName:     "Deepgram",
		description:     "Speech-to-text transcription and audio intelligence.",
		integrationKind: "audio_provider",
		authDescription: "API key required",
		requiresAPIKey:  true,
		apiKeyEnv:       "DEEPGRAM_API_KEY",
		apiKeyURL:       "https://console.deepgram.com/",
	},
	"ollama": {
		displayName:     "Ollama",
		description:     "Local or cloud Ollama models. Set the server URL (defaults to http://localhost:11434). API key is optional for local deployments.",
		integrationKind: "api_model",
		authDescription: "Server URL + optional API key",
		requiresAPIKey:  false,
		apiKeyEnv:       "OLLAMA_BASE_URL",
		apiKeyURL:       "https://ollama.com",
	},
}

func providerModelSelectionMode(provider string) string {
	switch provider {
	case "cursor-cli", "opencode-cli", "ollama":
		return "dynamic"
	default:
		return "fixed_tier"
	}
}

func providerKind(provider string) string {
	if common.IsCLIProvider(provider) {
		return "local_cli"
	}
	return "api"
}

func providerCodingAgentManifestInfo(provider, modelID string) *providerCodingAgentInfo {
	contractProvider := provider
	if strings.HasPrefix(provider, "opencode-cli-") {
		contractProvider = "opencode-cli"
	}
	contract, ok := llm.GetCodingAgentProviderContract(llm.Provider(contractProvider), modelID)
	if !ok {
		return nil
	}
	return &providerCodingAgentInfo{
		Transport:         string(contract.Transport),
		SupportsLiveInput: contract.SupportsLiveInput,
		SupportsInterrupt: contract.SupportsInterrupt,
	}
}

func providerWorkflowTierDefaults(provider string) *llm.CodingAgentDefaultTierModels {
	if providerStaticInfoMap[provider].integrationKind != "coding_agent" {
		return nil
	}
	defaults, ok := llm.GetCodingAgentDefaultTierModels(llm.Provider(provider))
	if !ok {
		return nil
	}
	return defaults
}

// handleGetProviderManifest returns the full provider manifest for the frontend.
func (api *StreamingAPI) handleGetProviderManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	keys := MergedProviderAPIKeys(ctx)
	allMetadata := utils.GetAllModelMetadata()

	metadataByProvider := map[string][]*llmtypes.ModelMetadata{}
	for _, m := range allMetadata {
		if m == nil {
			continue
		}
		metadataByProvider[m.Provider] = append(metadataByProvider[m.Provider], m)
	}

	capabilitiesByProvider := buildProviderCapabilities(ctx)

	providerOrder := []string{
		"codex-cli", "cursor-cli", "agy-cli", "opencode-cli",
		"opencode-cli-kimi", "opencode-cli-deepseek", "opencode-cli-qwen",
		"opencode-cli-minimax", "opencode-cli-glm", "opencode-cli-free",
		"claude-code", "gemini-cli",
		"openai", "anthropic", "vertex", "bedrock", "azure",
		"elevenlabs", "deepgram", "minimax",
	}

	supported := getSupportedProviders()
	supportedSet := make(map[string]bool, len(supported))
	for _, p := range supported {
		supportedSet[p] = true
	}

	// Resolve OpenCode CLI runtime once; sub-provider tiles share it.
	_, _, openCodeRuntimeOK := providerUsable("opencode-cli", true)
	openCodeRuntimeAvailable := openCodeRuntimeOK != nil && *openCodeRuntimeOK
	openCodeSubKeys := MergedOpenCodeSubProviderKeys(ctx)
	openCodeSubEntries := openCodeSubProviderManifestEntries(openCodeRuntimeAvailable, openCodeSubKeys)
	openCodeSubByID := make(map[string]providerManifestEntry, len(openCodeSubEntries))
	for _, e := range openCodeSubEntries {
		openCodeSubByID[e.ID] = e
	}

	entries := make([]providerManifestEntry, 0, len(providerOrder))
	for _, provider := range providerOrder {
		// OpenCode sub-provider tiles short-circuit the generic
		// static-info pipeline — their entries are fully constructed
		// from the opencodecli sub-provider catalog.
		if subEntry, ok := openCodeSubByID[provider]; ok {
			entries = append(entries, subEntry)
			continue
		}

		if !supportedSet[provider] {
			continue
		}

		info, ok := providerStaticInfoMap[provider]
		if !ok {
			continue
		}

		authConfigured, authSource := providerAuthConfigured(provider, keys)
		usable, runtimeCommand, runtimeOK := providerUsable(provider, authConfigured)

		setupHint := ""
		if runtimeOK != nil && !*runtimeOK {
			setupHint = discoverySetupHint(provider, true)
		} else if !usable {
			setupHint = discoverySetupHint(provider, false)
		}

		defaultModel := llm.GetDefaultModel(llm.Provider(provider))
		if defaultModel == "" {
			opts := discoveryModelOptions(provider)
			if len(opts) > 0 {
				defaultModel = opts[0]
			}
		}

		models := metadataByProvider[provider]
		if models == nil {
			models = []*llmtypes.ModelMetadata{}
		}

		selectionMode := providerModelSelectionMode(provider)

		caps := capabilitiesByProvider[provider]
		if caps == nil {
			caps = []string{}
		}

		entry := providerManifestEntry{
			ID:                    provider,
			DisplayName:           info.displayName,
			Description:           info.description,
			Kind:                  providerKind(provider),
			IntegrationKind:       info.integrationKind,
			ModelSelectionMode:    selectionMode,
			AuthDescription:       info.authDescription,
			RuntimeCommand:        runtimeCommand,
			RuntimeAvailable:      runtimeOK,
			AuthConfigured:        authConfigured,
			AuthSource:            authSource,
			Usable:                usable,
			SetupHint:             setupHint,
			RequiresAPIKey:        info.requiresAPIKey,
			SupportsDynamicModels: selectionMode == "dynamic",
			DefaultModelID:        defaultModel,
			DefaultTierModels:     providerWorkflowTierDefaults(provider),
			Models:                models,
			Capabilities:          caps,
			CodingAgent:           providerCodingAgentManifestInfo(provider, defaultModel),
			APIKeyEnv:             info.apiKeyEnv,
			APIKeyURL:             info.apiKeyURL,
		}
		entries = append(entries, entry)
	}

	resp := providerManifestResponse{
		Providers: entries,
		IntegrationKinds: map[string]integrationKindInfo{
			"coding_agent":   {Label: "Coding Agents", Description: "Local agent CLI runtimes"},
			"api_model":      {Label: "API Providers", Description: "Cloud-hosted LLM APIs"},
			"audio_provider": {Label: "Audio & Media", Description: "Speech, voice, and media models"},
		},
		ProviderOrder: providerOrder,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- Dynamic model listing ---

var (
	dynamicModelCache   = map[string]*cachedDynamicModels{}
	dynamicModelCacheMu sync.RWMutex
)

type cachedDynamicModels struct {
	response  *dynamicModelsResponse
	fetchedAt time.Time
}

const dynamicModelCacheTTL = 5 * time.Minute

func (api *StreamingAPI) handleGetProviderModels(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	provider := vars["provider"]
	if provider == "" {
		http.Error(w, "provider is required", http.StatusBadRequest)
		return
	}

	mode := providerModelSelectionMode(provider)

	if mode == "dynamic" {
		resp := getDynamicModels(r.Context(), provider)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Fixed tier / API — return from metadata registry
	allMetadata := utils.GetAllModelMetadata()
	models := make([]dynamicModelEntry, 0)
	for _, m := range allMetadata {
		if m == nil || m.Provider != provider {
			continue
		}
		defaultModel := llm.GetDefaultModel(llm.Provider(provider))
		models = append(models, dynamicModelEntry{
			ModelID:       m.ModelID,
			ModelName:     m.ModelName,
			IsDefault:     m.ModelID == defaultModel,
			ContextWindow: m.ContextWindow,
			CostInput:     m.InputCostPer1MTokens,
			CostOutput:    m.OutputCostPer1MTokens,
		})
	}

	resp := &dynamicModelsResponse{
		Provider:           provider,
		ModelSelectionMode: mode,
		Models:             models,
		Source:             "metadata_registry",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func getDynamicModels(ctx context.Context, provider string) *dynamicModelsResponse {
	dynamicModelCacheMu.RLock()
	cached, ok := dynamicModelCache[provider]
	dynamicModelCacheMu.RUnlock()

	if ok && time.Since(cached.fetchedAt) < dynamicModelCacheTTL {
		return cached.response
	}

	var resp *dynamicModelsResponse
	switch provider {
	case "cursor-cli":
		resp = fetchCursorCLIModels()
	case "opencode-cli":
		resp = fetchOpenCodeCLIModels()
	case "ollama":
		resp = fetchOllamaModels(ctx)
	default:
		resp = &dynamicModelsResponse{
			Provider:           provider,
			ModelSelectionMode: "dynamic",
			Models:             []dynamicModelEntry{},
			Source:             "unknown",
		}
	}

	dynamicModelCacheMu.Lock()
	dynamicModelCache[provider] = &cachedDynamicModels{
		response:  resp,
		fetchedAt: time.Now(),
	}
	dynamicModelCacheMu.Unlock()

	return resp
}

func fetchOllamaModels(ctx context.Context) *dynamicModelsResponse {
	keys := MergedProviderAPIKeys(ctx)

	baseURL := "http://localhost:11434"
	if keys != nil && keys.OllamaBaseURL != nil && strings.TrimSpace(*keys.OllamaBaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(*keys.OllamaBaseURL), "/")
	} else if v := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL")); v != "" {
		baseURL = strings.TrimRight(v, "/")
	}

	apiKey := ""
	if keys != nil && keys.OllamaAPIKey != nil {
		apiKey = strings.TrimSpace(*keys.OllamaAPIKey)
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OLLAMA_API_KEY"))
	}

	type ollamaModel struct {
		Name string `json:"name"`
	}
	type ollamaTagsResp struct {
		Models []ollamaModel `json:"models"`
	}

	tagsURL := baseURL + "/api/tags"
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, tagsURL, nil)
	if err == nil && apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	fallback := func(reason string) *dynamicModelsResponse {
		log.Printf("[OLLAMA] fetchOllamaModels: %s (base_url=%s)", reason, baseURL)
		return &dynamicModelsResponse{
			Provider:           "ollama",
			ModelSelectionMode: "dynamic",
			Models:             []dynamicModelEntry{},
			Source:             "ollama_tags_error",
		}
	}

	if err != nil {
		return fallback("failed to build request: " + err.Error())
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fallback("request failed: " + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fallback(fmt.Sprintf("HTTP %d from %s", resp.StatusCode, tagsURL))
	}

	var tagsResp ollamaTagsResp
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return fallback("failed to decode /api/tags response: " + err.Error())
	}

	models := make([]dynamicModelEntry, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		if m.Name == "" {
			continue
		}
		models = append(models, dynamicModelEntry{
			ModelID:   m.Name,
			ModelName: m.Name,
		})
	}

	log.Printf("[OLLAMA] fetchOllamaModels: found %d models at %s", len(models), baseURL)
	return &dynamicModelsResponse{
		Provider:           "ollama",
		ModelSelectionMode: "dynamic",
		Models:             models,
		Source:             "ollama_tags",
	}
}

func fetchCursorCLIModels() *dynamicModelsResponse {
	resp := &dynamicModelsResponse{
		Provider:           "cursor-cli",
		ModelSelectionMode: "dynamic",
		SupportsCustom:     true,
		CustomModelHint:    "Enter a model ID (e.g., gpt-5.5-high)",
		Source:             "cli_dynamic",
		CacheTTLSeconds:    300,
		CachedAt:           time.Now().UTC().Format(time.RFC3339),
	}

	binPath, err := runtimeAvailableForProvider("cursor-cli")
	if err != nil {
		resp.Source = "fallback_metadata"
		resp.Models = cursorFallbackModels()
		return resp
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binPath, "--list-models").Output()
	if err != nil {
		resp.Source = "fallback_metadata"
		resp.Models = cursorFallbackModels()
		return resp
	}

	models, groups := parseCursorModelList(string(out))
	if len(models) == 0 {
		resp.Source = "fallback_metadata"
		resp.Models = cursorFallbackModels()
		return resp
	}

	resp.Models = models
	resp.Groups = groups
	return resp
}

func parseCursorModelList(output string) ([]dynamicModelEntry, []string) {
	lines := strings.Split(output, "\n")
	models := make([]dynamicModelEntry, 0, len(lines))
	groupSet := map[string]bool{}
	groupOrder := []string{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Available") || strings.HasPrefix(line, "Tip:") {
			continue
		}

		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}

		modelID := strings.TrimSpace(parts[0])
		modelName := strings.TrimSpace(parts[1])
		group := inferCursorModelGroup(modelID, modelName)

		if !groupSet[group] {
			groupSet[group] = true
			groupOrder = append(groupOrder, group)
		}

		models = append(models, dynamicModelEntry{
			ModelID:   modelID,
			ModelName: modelName,
			Group:     group,
			IsDefault: modelID == "composer-2-fast" || modelID == "auto",
		})
	}

	return models, groupOrder
}

func inferCursorModelGroup(modelID, _ string) string {
	id := strings.ToLower(modelID)

	switch {
	case strings.HasPrefix(id, "composer"):
		return "Composer"
	case strings.Contains(id, "codex"):
		return "Codex"
	case strings.HasPrefix(id, "gpt-5.5"):
		return "GPT-5.5"
	case strings.HasPrefix(id, "gpt-5.4-nano"):
		return "GPT-5.4 Nano"
	case strings.HasPrefix(id, "gpt-5.4-mini"):
		return "GPT-5.4 Mini"
	case strings.HasPrefix(id, "gpt-5.4"):
		return "GPT-5.4"
	case strings.HasPrefix(id, "gpt-5.3"):
		return "GPT-5.3"
	case strings.HasPrefix(id, "gpt-5.2"):
		return "GPT-5.2"
	case strings.HasPrefix(id, "gpt-5.1"):
		return "GPT-5.1"
	case strings.HasPrefix(id, "gpt-5"):
		return "GPT-5"
	case strings.Contains(id, "claude-opus-4-8") || strings.Contains(id, "opus-4-8"):
		return "Claude Opus 4.8"
	case strings.Contains(id, "claude-opus-4-7") || strings.Contains(id, "opus-4-7"):
		return "Claude Opus 4.7"
	case strings.Contains(id, "claude-4.6-opus") || strings.Contains(id, "opus-4-6"):
		return "Claude Opus 4.6"
	case strings.Contains(id, "claude-4.5-opus"):
		return "Claude Opus 4.5"
	case strings.Contains(id, "claude-4.6-sonnet"):
		return "Claude Sonnet 4.6"
	case strings.Contains(id, "claude-4.5-sonnet"):
		return "Claude Sonnet 4.5"
	case strings.Contains(id, "claude-4-sonnet") || strings.Contains(id, "claude-sonnet-4"):
		return "Claude Sonnet 4"
	case strings.Contains(id, "claude"):
		return "Claude"
	case strings.Contains(id, "gemini"):
		return "Gemini"
	case strings.Contains(id, "grok"):
		return "Grok"
	case strings.Contains(id, "kimi"):
		return "Kimi"
	default:
		return "Other"
	}
}

func cursorFallbackModels() []dynamicModelEntry {
	allMetadata := utils.GetAllModelMetadata()
	models := make([]dynamicModelEntry, 0)
	for _, m := range allMetadata {
		if m == nil || m.Provider != "cursor-cli" {
			continue
		}
		models = append(models, dynamicModelEntry{
			ModelID:   m.ModelID,
			ModelName: m.ModelName,
			Group:     "Default",
			IsDefault: m.ModelID == "cursor-cli",
		})
	}
	return models
}

func fetchOpenCodeCLIModels() *dynamicModelsResponse {
	resp := &dynamicModelsResponse{
		Provider:           "opencode-cli",
		ModelSelectionMode: "dynamic",
		SupportsCustom:     true,
		CustomModelHint:    "Enter as provider/model (e.g., openai/gpt-5.1)",
		Source:             "cli_dynamic",
		CacheTTLSeconds:    300,
		CachedAt:           time.Now().UTC().Format(time.RFC3339),
	}

	binPath, err := runtimeAvailableForProvider("opencode-cli")
	if err != nil {
		resp.Source = "fallback_metadata"
		resp.Models = opencodeFallbackModels()
		return resp
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binPath, "models", "--verbose").Output()
	if err != nil {
		resp.Source = "fallback_metadata"
		resp.Models = opencodeFallbackModels()
		return resp
	}

	models, groups := parseOpenCodeModelList(string(out))
	if len(models) == 0 {
		resp.Source = "fallback_metadata"
		resp.Models = opencodeFallbackModels()
		return resp
	}

	resp.Models = models
	resp.Groups = groups
	return resp
}

type openCodeModelJSON struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Cost       struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"cost"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Capabilities struct {
		Reasoning  bool `json:"reasoning"`
		ToolCall   bool `json:"toolcall"`
		Attachment bool `json:"attachment"`
	} `json:"capabilities"`
}

func parseOpenCodeModelList(output string) ([]dynamicModelEntry, []string) {
	lines := strings.Split(output, "\n")
	models := make([]dynamicModelEntry, 0)
	groupSet := map[string]bool{}
	groupOrder := []string{}

	var currentModelID string
	var jsonBuf strings.Builder
	inJSON := false
	braceDepth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inJSON && trimmed != "" && !strings.HasPrefix(trimmed, "{") {
			currentModelID = trimmed
			continue
		}

		if !inJSON && strings.HasPrefix(trimmed, "{") {
			inJSON = true
			braceDepth = 0
			jsonBuf.Reset()
		}

		if inJSON {
			jsonBuf.WriteString(line)
			jsonBuf.WriteString("\n")

			braceDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")

			if braceDepth <= 0 {
				inJSON = false
				braceDepth = 0
				var model openCodeModelJSON
				if err := json.Unmarshal([]byte(jsonBuf.String()), &model); err == nil {
					group := model.ProviderID
					if group == "" {
						group = "OpenCode"
					}
					if !groupSet[group] {
						groupSet[group] = true
						groupOrder = append(groupOrder, group)
					}

					fullID := currentModelID
					if fullID == "" {
						fullID = fmt.Sprintf("%s/%s", model.ProviderID, model.ID)
					}

					models = append(models, dynamicModelEntry{
						ModelID:       fullID,
						ModelName:     model.Name,
						Group:         group,
						ContextWindow: model.Limit.Context,
						CostInput:     model.Cost.Input,
						CostOutput:    model.Cost.Output,
					})
				}
				currentModelID = ""
			}
		}
	}

	return models, groupOrder
}

func opencodeFallbackModels() []dynamicModelEntry {
	allMetadata := utils.GetAllModelMetadata()
	models := make([]dynamicModelEntry, 0)
	for _, m := range allMetadata {
		if m == nil || m.Provider != "opencode-cli" {
			continue
		}
		models = append(models, dynamicModelEntry{
			ModelID:   m.ModelID,
			ModelName: m.ModelName,
			Group:     "Default",
			IsDefault: m.ModelID == "opencode-cli",
		})
	}
	return models
}
