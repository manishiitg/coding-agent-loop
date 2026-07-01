package server

import (
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
	"bedrock",
	"openai",
	"vertex",
	"anthropic",
	"azure",
	"minimax",
	"elevenlabs",
	"deepgram",
	"claude-code",
	"gemini-cli",
	"codex-cli",
	"cursor-cli",
	"agy-cli",
	"pi-cli",
}

const claudeCodeDisableAutoMemoryEnv = "CLAUDE_CODE_DISABLE_AUTO_MEMORY"

func isDeprecatedLLMProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "gemini-cli" || provider == "agy-cli"
}

func providerDeprecationReason(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch {
	case provider == "gemini-cli":
		return "Gemini CLI is retained for existing sessions only. Use Pi CLI for new Gemini coding-agent setup."
	case provider == "agy-cli":
		return "Antigravity CLI is retained for existing sessions only. Use Pi CLI for new multi-provider coding-agent setup."
	default:
		return ""
	}
}

func providerReplacementProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch {
	case provider == "gemini-cli", provider == "agy-cli":
		return "pi-cli"
	default:
		return ""
	}
}

func isPublishedLLMProviderAllowed(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "bedrock", "openai", "vertex", "anthropic", "azure",
		"claude-code", "gemini-cli", "codex-cli", "cursor-cli", "agy-cli", "pi-cli":
		return true
	default:
		return false
	}
}

func fallbackPublishedLLMProviderAndModel() (string, string) {
	for _, provider := range []string{
		"codex-cli",
		"cursor-cli",
		"pi-cli",
		"claude-code",
		"bedrock",
		"openai",
		"anthropic",
		"vertex",
		"azure",
	} {
		if !isPublishedLLMProviderAllowed(provider) {
			continue
		}
		modelID := strings.TrimSpace(llm.GetDefaultModel(llm.Provider(provider)))
		if modelID != "" {
			return provider, modelID
		}
	}
	return "codex-cli", "codex-cli"
}

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

	// Allow any model listed in AvailableModels for this provider. Dynamic CLI
	// providers can come from the curated discovery options when the provider
	// library does not expose them through static defaults.
	models := defaults.AvailableModels[provider]
	if len(models) == 0 {
		models = discoveryModelOptions(provider)
	}
	for _, m := range models {
		if m == modelID {
			return true
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
	fallbackProvider, fallbackModelID := fallbackPublishedLLMProviderAndModel()
	bytes, err := json.Marshal(defaults.PrimaryConfig)
	if err != nil {
		return fallbackProvider, fallbackModelID
	}
	var m map[string]interface{}
	if err := json.Unmarshal(bytes, &m); err != nil {
		return fallbackProvider, fallbackModelID
	}
	if p, _ := m["provider"].(string); p != "" {
		provider = p
	} else {
		provider = fallbackProvider
	}
	if !isPublishedLLMProviderAllowed(provider) {
		return fallbackProvider, fallbackModelID
	}
	if mid, _ := m["model_id"].(string); mid != "" {
		modelID = mid
	} else {
		modelID = llm.GetDefaultModel(llm.Provider(provider))
	}
	if strings.TrimSpace(modelID) == "" {
		return fallbackProvider, fallbackModelID
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

	setProviderKeyFromEnv(llm.ProviderOpenAI, "OPENAI_API_KEY")
	setProviderKeyFromEnv(llm.ProviderOpenRouter, "OPENROUTER_API_KEY", "OPEN_ROUTER_API_KEY")
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
	if s := os.Getenv("AGY_API_KEY"); s != "" {
		keys.AgyCLI = &s
	}
	if s := os.Getenv("PI_API_KEY"); s != "" {
		keys.PiCLI = &s
	} else if s := os.Getenv("GEMINI_API_KEY"); s != "" {
		keys.PiCLI = &s
	} else if s := os.Getenv("GOOGLE_API_KEY"); s != "" {
		keys.PiCLI = &s
	}
	if s := os.Getenv("MINIMAX_API_KEY"); s != "" {
		keys.MiniMax = &s
	}
	keys.PiProviderKeys = buildPiProviderKeysFromEnv()
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

func buildPiProviderKeysFromEnv() map[string]string {
	envByProvider := map[string][]string{
		"google":            {"PI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"},
		"google-vertex":     {"PI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"},
		"openai":            {"OPENAI_API_KEY"},
		"anthropic":         {"ANTHROPIC_API_KEY"},
		"openrouter":        {"OPENROUTER_API_KEY"},
		"deepseek":          {"DEEPSEEK_API_KEY"},
		"nvidia":            {"NVIDIA_API_KEY"},
		"mistral":           {"MISTRAL_API_KEY"},
		"groq":              {"GROQ_API_KEY"},
		"cerebras":          {"CEREBRAS_API_KEY"},
		"xai":               {"XAI_API_KEY"},
		"zai":               {"ZAI_API_KEY"},
		"zai-coding-cn":     {"ZAI_CODING_CN_API_KEY"},
		"opencode":          {"OPENCODE_API_KEY"},
		"opencode-go":       {"OPENCODE_API_KEY"},
		"fireworks":         {"FIREWORKS_API_KEY"},
		"together":          {"TOGETHER_API_KEY"},
		"kimi-coding":       {"KIMI_API_KEY"},
		"moonshotai":        {"KIMI_API_KEY"},
		"moonshotai-cn":     {"KIMI_API_KEY"},
		"minimax":           {"MINIMAX_API_KEY"},
		"minimax-cn":        {"MINIMAX_CN_API_KEY"},
		"vercel-ai-gateway": {"AI_GATEWAY_API_KEY"},
	}
	result := map[string]string{}
	for provider, envNames := range envByProvider {
		for _, envName := range envNames {
			if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
				result[provider] = value
				break
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

type llmDiscoveryCandidate struct {
	ID                  string   `json:"id"`
	Provider            string   `json:"provider"`
	ModelID             string   `json:"model_id"`
	ModelName           string   `json:"model_name,omitempty"`
	Label               string   `json:"label"`
	Kind                string   `json:"kind"`
	DetectionSource     string   `json:"detection_source"`
	AuthSource          string   `json:"auth_source,omitempty"`
	AuthConfigured      bool     `json:"auth_configured"`
	RuntimeCommand      string   `json:"runtime_command,omitempty"`
	RuntimeAvailable    *bool    `json:"runtime_available,omitempty"`
	Usable              bool     `json:"usable"`
	Recommended         bool     `json:"recommended"`
	Reason              string   `json:"reason"`
	SetupHint           string   `json:"setup_hint,omitempty"`
	Deprecated          bool     `json:"deprecated,omitempty"`
	DeprecationReason   string   `json:"deprecation_reason,omitempty"`
	ReplacementProvider string   `json:"replacement_provider,omitempty"`
	Options             []string `json:"options,omitempty"`
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
	case "agy-cli":
		return "Antigravity CLI (Deprecated)"
	case "pi-cli":
		return "Pi CLI"
	case "claude-code":
		return "Claude Code"
	case "gemini-cli":
		return "Gemini CLI (Deprecated)"
	case "openai":
		return "OpenAI API"
	case "anthropic":
		return "Anthropic API"
	case "vertex":
		return "Gemini / Vertex"
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
	return providerKind(provider)
}

func discoveryModelOptions(provider string) []string {
	switch provider {
	case "codex-cli":
		return []string{"codex-cli", "high", "medium", "low"}
	case "cursor-cli":
		return []string{"cursor-cli", "composer-2.5", "gpt-5", "sonnet-4-thinking", "sonnet-4"}
	case "agy-cli":
		return []string{"agy-cli"}
	case "pi-cli":
		return piCuratedModelIDs()
	case "claude-code":
		return []string{"claude-code", "high", "medium", "low"}
	case "gemini-cli":
		return []string{"auto", "high", "medium", "low"}
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
		case "agy-cli":
			return "Install Antigravity CLI so the agy command is available on the backend PATH."
		case "pi-cli":
			return "Install Pi CLI with npm install -g @earendil-works/pi-coding-agent, or ensure npx is available on the backend PATH."
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
	case "agy-cli":
		return "Run agy locally and complete Antigravity sign-in, then test again."
	case "pi-cli":
		return "Set PI_API_KEY, GEMINI_API_KEY, or GOOGLE_API_KEY, then test again."
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
		"claude-code",
		"codex-cli",
		"cursor-cli",
		"pi-cli",
		"gemini-cli",
		"agy-cli",
		"openai",
		"anthropic",
		"vertex",
		"bedrock",
		"azure",
	}

	candidates := make([]llmDiscoveryCandidate, 0, len(providerOrder))
	for _, provider := range providerOrder {
		if !supportedSet[provider] {
			continue
		}

		authConfigured, authSource := providerAuthConfigured(provider, keys)
		usable, runtimeCommand, runtimeOK := providerUsable(provider, authConfigured)
		modelID := llm.GetDefaultModel(llm.Provider(provider))
		if strings.TrimSpace(modelID) == "" {
			if options := discoveryModelOptions(provider); len(options) > 0 {
				modelID = options[0]
			}
		}
		if modelID == "" {
			continue
		}
		deprecated := isDeprecatedLLMProvider(provider)

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
			ID:                  provider + ":" + modelID,
			Provider:            provider,
			ModelID:             modelID,
			ModelName:           modelNameForProviderModel(provider, modelID),
			Label:               providerDisplayLabel(provider),
			Kind:                kind,
			DetectionSource:     source,
			AuthSource:          authSource,
			AuthConfigured:      authConfigured,
			RuntimeCommand:      runtimeCommand,
			RuntimeAvailable:    runtimeOK,
			Usable:              usable,
			Recommended:         usable && !deprecated,
			Reason:              reason,
			SetupHint:           setupHint,
			Deprecated:          deprecated,
			DeprecationReason:   providerDeprecationReason(provider),
			ReplacementProvider: providerReplacementProvider(provider),
			Options:             discoveryModelOptions(provider),
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
			list = filterDefaultPublishedLLMs(list)
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
				list = filterDefaultPublishedLLMs(list)
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
	providers := []string{"codex-cli", "cursor-cli", "pi-cli", "claude-code", "azure", "bedrock", "openai", "anthropic", "vertex"}

	for _, p := range providers {
		// If provider is locked (or global lock is on), include its available models
		if isProviderLocked(p) || locked {
			models := defaults.AvailableModels[p]
			if len(models) == 0 {
				models = discoveryModelOptions(p)
			}
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
	if !isPublishedLLMProviderAllowed(provider) {
		provider, modelID = fallbackPublishedLLMProviderAndModel()
	}
	if provider == "" {
		provider, modelID = fallbackPublishedLLMProviderAndModel()
	}
	if modelID == "" {
		modelID = llm.GetDefaultModel(llm.Provider(provider))
	}
	if strings.TrimSpace(modelID) == "" {
		provider, modelID = fallbackPublishedLLMProviderAndModel()
	}
	entry := map[string]interface{}{
		"id":       "default-" + provider + "-" + strings.ReplaceAll(modelID, "/", "-"),
		"name":     "Default (" + provider + ")",
		"provider": provider,
		"model_id": modelID,
	}

	isLocked := locked || isProviderLocked(provider)

	if !isLocked {
		if key := os.Getenv("OPENAI_API_KEY"); provider == "openai" && key != "" {
			entry["api_key"] = key
		} else if key := os.Getenv("ANTHROPIC_API_KEY"); provider == "anthropic" && key != "" {
			entry["api_key"] = key
		}
	}
	return []map[string]interface{}{entry}
}

func filterDefaultPublishedLLMs(list []map[string]interface{}) []map[string]interface{} {
	filtered := make([]map[string]interface{}, 0, len(list))
	for _, entry := range list {
		provider, _ := entry["provider"].(string)
		if !isPublishedLLMProviderAllowed(provider) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// handleGetLLMDefaults returns default LLM configurations from environment variables.
// When LLM_CONFIG_LOCKED=true (or specific provider is locked), api_key and endpoint are stripped.
func (api *StreamingAPI) handleGetLLMDefaults(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request for LLM defaults")
	defaults := llm.GetLLMDefaults()
	availableModels := make(map[string][]string, len(defaults.AvailableModels))
	for provider, models := range defaults.AvailableModels {
		if !isPublishedLLMProviderAllowed(provider) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(provider)) {
		case "openrouter", "z-ai", "kimi", "minimax-coding-plan":
			continue
		default:
			availableModels[provider] = models
		}
	}
	for _, provider := range getSupportedProviders() {
		if _, exists := availableModels[provider]; exists || !isPublishedLLMProviderAllowed(provider) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(provider)) {
		case "openrouter", "z-ai", "kimi", "minimax-coding-plan":
			continue
		}
		if models := discoveryModelOptions(provider); len(models) > 0 {
			availableModels[provider] = models
		}
	}

	globalLocked := isGlobalLLMConfigLocked()
	lockedProviders := getLockedProviders()
	primaryProvider, primaryModelID := getPrimaryProviderAndModelFromDefaults()
	primaryConfig := map[string]interface{}{
		"provider":        primaryProvider,
		"model_id":        primaryModelID,
		"fallback_models": []string{},
	}

	// Build response (same shape as before)
	response := map[string]interface{}{
		"primary_config":        primaryConfig,
		"bedrock_config":        defaults.BedrockConfig,
		"openai_config":         defaults.OpenaiConfig,
		"anthropic_config":      defaults.AnthropicConfig,
		"azure_config":          defaults.AzureConfig,
		"zai_config":            defaults.ZAIConfig,
		"kimi_config":           defaults.KimiConfig,
		"minimax_config":        defaults.MinimaxConfig,
		"elevenlabs_config":     defaults.ElevenLabsConfig,
		"deepgram_config":       defaults.DeepgramConfig,
		"available_models":      availableModels,
		"provider_capabilities": buildProviderCapabilities(r.Context()),
		"supported_providers":   getSupportedProviders(),
		"locked_providers":      lockedProviders,
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

// handleValidateAPIKey validates API keys for supported LLM providers and coding CLIs.
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

	provider := strings.ToLower(strings.TrimSpace(req.Provider))

	keys := MergedProviderAPIKeys(ctx)
	if keys == nil {
		return
	}
	setAPIKey := func(key *string) {
		if req.APIKey == "" && key != nil {
			req.APIKey = strings.TrimSpace(*key)
		}
	}

	switch provider {
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
	case "agy-cli":
		setAPIKey(keys.AgyCLI)
	case "pi-cli":
		if req.APIKey == "" {
			req.APIKey = selectPiAPIKeyForModel(keys, req.ModelID)
		}
	case "minimax":
		setAPIKey(keys.MiniMax)
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
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	switch provider {
	case "openrouter", "minimax-coding-plan":
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Provider %s is no longer available as a direct LLM provider.", req.Provider),
		}
	}

	switch provider {
	case "claude-code":
		return validateClaudeCodeCLI()
	case "gemini-cli":
		return validateGeminiCLI(req.APIKey)
	case "codex-cli":
		return validateCodexCLI(req.APIKey)
	case "cursor-cli":
		return validateCursorCLI(req.APIKey, req.ModelID)
	case "agy-cli":
		return validateAgyCLI(req.APIKey, req.ModelID)
	case "pi-cli":
		return validatePiCLI(req.APIKey, req.ModelID, req.Options)
	case "kimi":
		return llm.ValidateAPIKey(req)
	default:
		return llm.ValidateAPIKey(req)
	}
}

// validateClaudeCodeCLI validates the Claude Code CLI by checking it exists and
// then running a real adapter call through llm.InitializeLLM so the test
// exercises the same code path as a live workflow run (env vars, transport,
// model resolution all match production).
func validateClaudeCodeCLI() llm.APIKeyValidationResponse {
	log.Printf("[CLAUDE-CODE VALIDATION] Starting CLI validation")

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		log.Printf("[CLAUDE-CODE VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Claude Code CLI not found. Install it with: npm install -g @anthropic-ai/claude-code",
		}
	}
	log.Printf("[CLAUDE-CODE VALIDATION] CLI found at: %s", claudePath)

	workspaceDir, err := os.MkdirTemp("", "claude-code-validation-*")
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Could not create a temporary workspace for Claude Code validation.",
		}
	}
	defer os.RemoveAll(workspaceDir)

	model, err := llm.InitializeLLM(llm.Config{
		Provider: llm.ProviderClaudeCode,
		ModelID:  "claude-code",
		Context:  context.Background(),
	})
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Failed to initialize Claude Code: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := model.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly: Claude Code is working."},
			},
		},
	}, llm.WithClaudeCodeWorkingDir(workspaceDir))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Claude Code timed out after 90s. Check that you are authenticated (run 'claude' to log in).",
			}
		}
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Claude Code error: %s", strings.TrimSpace(err.Error())),
		}
	}

	responseText := ""
	if resp != nil && len(resp.Choices) > 0 {
		responseText = strings.TrimSpace(resp.Choices[0].Content)
	}
	if responseText == "" {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Claude Code returned an empty response. Check authentication with 'claude'.",
		}
	}

	log.Printf("[CLAUDE-CODE VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Claude Code is working. Response: %s", responseText),
	}
}

// validateGeminiCLI validates the Gemini CLI by checking it exists and sending a test prompt
// validateGeminiCLI validates the Gemini CLI by running a real adapter call
// through llm.InitializeLLM so the test exercises the same path as production
// (TRUST_WORKSPACE env, model alias resolution, transport, all match).
func validateGeminiCLI(apiKey string) llm.APIKeyValidationResponse {
	log.Printf("[GEMINI-CLI VALIDATION] Starting CLI validation")

	geminiPath, err := exec.LookPath("gemini")
	if err != nil {
		log.Printf("[GEMINI-CLI VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Gemini CLI not found. Install it with: npm install -g @google/gemini-cli (see https://github.com/google-gemini/gemini-cli)",
		}
	}
	log.Printf("[GEMINI-CLI VALIDATION] CLI found at: %s", geminiPath)

	// Gemini CLI defaults to oauth-personal auth which fails in non-interactive
	// mode. Flip ~/.gemini/settings.json to "gemini-api-key" so the adapter's
	// API-key auth path actually works.
	ensureGeminiAPIKeyAuth()

	workspaceDir, err := os.MkdirTemp("", "gemini-cli-validation-*")
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Could not create a temporary workspace for Gemini CLI validation.",
		}
	}
	defer os.RemoveAll(workspaceDir)

	keys := &llm.ProviderAPIKeys{}
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if strings.TrimSpace(apiKey) != "" {
		keys.GeminiCLI = &apiKey
	}

	model, err := llm.InitializeLLM(llm.Config{
		Provider: llm.ProviderGeminiCLI,
		ModelID:  "medium",
		APIKeys:  keys,
		Context:  context.Background(),
	})
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Failed to initialize Gemini CLI: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := model.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly: Gemini CLI is working."},
			},
		},
	}, llm.WithGeminiWorkingDir(workspaceDir))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Gemini CLI timed out after 90s. Check that you are authenticated (run 'gemini' to log in).",
			}
		}
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Gemini CLI error: %s", strings.TrimSpace(err.Error())),
		}
	}

	responseText := ""
	if resp != nil && len(resp.Choices) > 0 {
		responseText = strings.TrimSpace(resp.Choices[0].Content)
	}
	if responseText == "" {
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

// validateCodexCLI validates the OpenAI Codex CLI by running a real adapter
// call through llm.InitializeLLM so the test exercises the same code path as
// a live workflow run.
func validateCodexCLI(apiKey string) llm.APIKeyValidationResponse {
	log.Printf("[CODEX-CLI VALIDATION] Starting CLI validation")

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		log.Printf("[CODEX-CLI VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Codex CLI not found. Install it with: npm install -g @openai/codex",
		}
	}
	log.Printf("[CODEX-CLI VALIDATION] CLI found at: %s", codexPath)

	keys := &llm.ProviderAPIKeys{}
	if apiKey == "" {
		apiKey = os.Getenv("CODEX_API_KEY")
	}
	if strings.TrimSpace(apiKey) != "" {
		keys.CodexCLI = &apiKey
	}

	model, err := llm.InitializeLLM(llm.Config{
		Provider: llm.ProviderCodexCLI,
		ModelID:  "medium",
		APIKeys:  keys,
		Context:  context.Background(),
	})
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Failed to initialize Codex CLI: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := model.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly: Codex CLI is working."},
			},
		},
	})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Codex CLI timed out after 90s. Check that you are authenticated (run 'codex login').",
			}
		}
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Codex CLI error: %s", strings.TrimSpace(err.Error())),
		}
	}

	responseText := ""
	if resp != nil && len(resp.Choices) > 0 {
		responseText = strings.TrimSpace(resp.Choices[0].Content)
	}
	if responseText == "" {
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
	if strings.TrimSpace(apiKey) == "" && strings.TrimSpace(os.Getenv("CURSOR_API_KEY")) == "" && !cursorCLILocalAuthConfigured() {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: cursorCLILoginRequiredMessage(),
		}
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

// validateAgyCLI validates Antigravity CLI through the real tmux adapter path.
func validateAgyCLI(apiKey, modelID string) llm.APIKeyValidationResponse {
	log.Printf("[AGY-CLI VALIDATION] Starting CLI validation")

	agyPath, err := exec.LookPath("agy")
	if err != nil {
		log.Printf("[AGY-CLI VALIDATION] CLI not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Antigravity CLI not found. Install it so the agy command is available on the backend PATH.",
		}
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		log.Printf("[AGY-CLI VALIDATION] tmux not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "tmux not found. Antigravity CLI integration requires tmux for interactive mode.",
		}
	}
	log.Printf("[AGY-CLI VALIDATION] CLI found at: %s", agyPath)

	if modelID == "" {
		modelID = "agy-cli"
	}

	workspaceDir, err := os.MkdirTemp("", "agy-cli-validation-*")
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Could not create a temporary workspace for Antigravity CLI validation.",
		}
	}
	defer os.RemoveAll(workspaceDir)

	keys := &llm.ProviderAPIKeys{}
	if apiKey == "" {
		apiKey = os.Getenv("AGY_API_KEY")
	}
	if strings.TrimSpace(apiKey) != "" {
		keys.AgyCLI = &apiKey
	}

	model, err := llm.InitializeLLM(llm.Config{
		Provider: llm.ProviderAgyCLI,
		ModelID:  modelID,
		APIKeys:  keys,
		Context:  context.Background(),
	})
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Failed to initialize Antigravity CLI: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	resp, err := model.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly: Antigravity CLI is working."},
			},
		},
	}, llm.WithAgyWorkingDir(workspaceDir))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Antigravity CLI timed out after 90s. Check that you are authenticated with agy.",
			}
		}
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Antigravity CLI error: %s", strings.TrimSpace(err.Error())),
		}
	}

	responseText := ""
	if resp != nil && len(resp.Choices) > 0 {
		responseText = strings.TrimSpace(resp.Choices[0].Content)
	}
	if responseText == "" {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Antigravity CLI returned an empty response. Check authentication with agy.",
		}
	}

	log.Printf("[AGY-CLI VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Antigravity CLI is working. Response: %s", responseText),
	}
}

// validatePiCLI validates Pi CLI through the real tmux marker adapter path.
func validatePiCLI(apiKey, modelID string, options map[string]interface{}) llm.APIKeyValidationResponse {
	log.Printf("[PI-CLI VALIDATION] Starting CLI validation")

	runtimePath, err := runtimeAvailableForProvider("pi-cli")
	if err != nil {
		log.Printf("[PI-CLI VALIDATION] runtime not found: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Pi CLI not found. Install with: npm install -g @earendil-works/pi-coding-agent, or ensure npx is available.",
		}
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		log.Printf("[PI-CLI VALIDATION] tmux not found on PATH: %v", err)
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "tmux not found. Pi CLI integration requires tmux for interactive mode.",
		}
	}
	log.Printf("[PI-CLI VALIDATION] runtime available: %s", runtimePath)

	if strings.TrimSpace(modelID) == "" || strings.EqualFold(strings.TrimSpace(modelID), "pi-cli") {
		modelID = "google/gemini-3.5-flash"
	}

	workspaceDir, err := os.MkdirTemp("", "pi-cli-validation-*")
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Could not create a temporary workspace for Pi CLI validation.",
		}
	}
	defer os.RemoveAll(workspaceDir)

	keys := &llm.ProviderAPIKeys{}
	if strings.TrimSpace(apiKey) == "" {
		apiKey = selectPiAPIKeyForModel(buildProviderAPIKeysFromEnv(), modelID)
	}
	if strings.TrimSpace(apiKey) != "" {
		piProvider := piProviderFromModelID(modelID)
		keys.PiProviderKeys = map[string]string{piProvider: strings.TrimSpace(apiKey)}
		if piProvider == "google" || piProvider == "google-vertex" {
			keys.PiCLI = &apiKey
		}
	}

	model, err := llm.InitializeLLM(llm.Config{
		Provider: llm.ProviderPiCLI,
		ModelID:  modelID,
		APIKeys:  keys,
		Context:  context.Background(),
	})
	if err != nil {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Failed to initialize Pi CLI: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	callOpts := []llmtypes.CallOption{llm.WithPiWorkingDir(workspaceDir)}
	if options != nil {
		if provider, ok := options["pi_provider"].(string); ok && strings.TrimSpace(provider) != "" {
			callOpts = append(callOpts, llm.WithPiProvider(provider))
		}
	}

	resp, err := model.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Reply with exactly: Pi CLI is working."},
			},
		},
	}, callOpts...)
	if handle, ok := llmtypes.ExtractCodingProviderSessionHandleFromResponse(resp); ok {
		llm.ClosePiCLIInteractiveSessionByTmux(handle.TmuxSession, "validation cleanup")
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: "Pi CLI timed out after 120s. Check that the selected model and API key are valid.",
			}
		}
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Pi CLI error: %s", strings.TrimSpace(err.Error())),
		}
	}

	responseText := ""
	if resp != nil && len(resp.Choices) > 0 {
		responseText = strings.TrimSpace(resp.Choices[0].Content)
	}
	if responseText == "" {
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: "Pi CLI returned an empty response. Check Pi provider/model/API-key configuration.",
		}
	}

	log.Printf("[PI-CLI VALIDATION SUCCESS] Got response: %s", responseText)
	return llm.APIKeyValidationResponse{
		Valid:   true,
		Message: fmt.Sprintf("Pi CLI is working. Response: %s", responseText),
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
