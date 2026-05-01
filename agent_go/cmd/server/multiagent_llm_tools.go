package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/utils"
)

// listProviderModelsJSON returns a JSON string of all models for the given provider
// from the shared model metadata catalog. Used by both multi-agent and workshop LLM tools.
func listProviderModelsJSON(provider string) string {
	allModels := utils.GetAllModelMetadata()
	filtered := make([]interface{}, 0, len(allModels))
	for _, m := range allModels {
		if m == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(m.Provider)) != provider {
			continue
		}
		filtered = append(filtered, m)
	}
	return prettyJSON(map[string]interface{}{
		"provider": provider,
		"count":    len(filtered),
		"models":   filtered,
	})
}

func prettyJSON(v interface{}) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

func normalizeManagedProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func cloneOptionsMap(options map[string]interface{}) map[string]interface{} {
	if len(options) == 0 {
		return nil
	}

	cloned := make(map[string]interface{}, len(options))
	for k, v := range options {
		cloned[k] = v
	}
	return cloned
}

func getStoredProviderAPIKey(keys *StoredProviderKeys, provider string) string {
	if keys == nil {
		return ""
	}

	switch normalizeManagedProvider(provider) {
	case "openrouter":
		return strings.TrimSpace(keys.OpenRouter)
	case "openai":
		return strings.TrimSpace(keys.OpenAI)
	case "anthropic":
		return strings.TrimSpace(keys.Anthropic)
	case "z-ai":
		return strings.TrimSpace(keys.ZAI)
	case "vertex":
		return strings.TrimSpace(keys.Vertex)
	case "gemini-cli":
		return strings.TrimSpace(keys.GeminiCLI)
	case "minimax":
		return strings.TrimSpace(keys.MiniMax)
	case "minimax-coding-plan":
		return strings.TrimSpace(keys.MiniMaxCodingPlan)
	case "elevenlabs":
		return strings.TrimSpace(keys.ElevenLabs)
	case "deepgram":
		return strings.TrimSpace(keys.Deepgram)
	default:
		return ""
	}
}

func setStoredProviderAPIKey(keys *StoredProviderKeys, provider, apiKey string) bool {
	if keys == nil {
		return false
	}

	value := strings.TrimSpace(apiKey)
	switch normalizeManagedProvider(provider) {
	case "openrouter":
		keys.OpenRouter = value
	case "openai":
		keys.OpenAI = value
	case "anthropic":
		keys.Anthropic = value
	case "z-ai":
		keys.ZAI = value
	case "vertex":
		keys.Vertex = value
	case "gemini-cli":
		keys.GeminiCLI = value
	case "minimax":
		keys.MiniMax = value
	case "minimax-coding-plan":
		keys.MiniMaxCodingPlan = value
	case "elevenlabs":
		keys.ElevenLabs = value
	case "deepgram":
		keys.Deepgram = value
	default:
		return false
	}

	return true
}

type llmCapabilityProvider struct {
	Provider          string                 `json:"provider"`
	Models            []string               `json:"models,omitempty"`
	ModelCount        int                    `json:"model_count,omitempty"`
	DefaultModel      string                 `json:"default_model,omitempty"`
	ConfigFile        string                 `json:"config_file,omitempty"`
	AuthSource        string                 `json:"auth_source,omitempty"`
	AuthConfigured    bool                   `json:"auth_configured"`
	RuntimeDependency string                 `json:"runtime_dependency,omitempty"`
	RuntimeAvailable  *bool                  `json:"runtime_available,omitempty"`
	Usable            bool                   `json:"usable"`
	Notes             []string               `json:"notes,omitempty"`
	Extra             map[string]interface{} `json:"extra,omitempty"`
}

type llmPricingRule struct {
	Unit         string  `json:"unit"`
	USD          float64 `json:"usd"`
	Description  string  `json:"description,omitempty"`
	EstimateOnly bool    `json:"estimate_only,omitempty"`
	Source       string  `json:"source,omitempty"`
}

type llmPricingCatalogEntry struct {
	Capability string           `json:"capability"`
	Provider   string           `json:"provider"`
	Models     []string         `json:"models,omitempty"`
	Default    bool             `json:"default,omitempty"`
	Rules      []llmPricingRule `json:"rules"`
	Notes      []string         `json:"notes,omitempty"`
}

const (
	pricingSourceGoogleVertex = "Google Cloud Vertex AI Generative AI pricing, checked 2026-04-30"
	pricingSourceGeminiAPI    = "Google AI Gemini API pricing, checked 2026-04-30"
	pricingSourceElevenLabs   = "ElevenLabs API pricing, checked 2026-04-30"
	pricingSourceDeepgram     = "Deepgram pricing, checked 2026-04-30"
	pricingSourceMiniMax      = "MiniMax pay-as-you-go pricing, checked 2026-04-30"
)

var llmPricingCatalog = []llmPricingCatalogEntry{
	{
		Capability: "generate_video",
		Provider:   string(llm.ProviderVertex),
		Models:     []string{"veo-3.1-generate-preview", "veo-3.1-generate-001"},
		Rules: []llmPricingRule{
			{Unit: "video_second", USD: 0.40, Description: "Veo 3.1 standard video with audio", Source: pricingSourceGoogleVertex},
			{Unit: "video_second_no_audio", USD: 0.20, Description: "Veo 3.1 standard video without audio", Source: pricingSourceGoogleVertex},
		},
	},
	{
		Capability: "generate_video",
		Provider:   string(llm.ProviderVertex),
		Models:     []string{"veo-3.1-fast-generate-preview", "veo-3.1-fast-generate-001"},
		Rules: []llmPricingRule{
			{Unit: "video_second", USD: 0.15, Description: "Veo 3.1 Fast video with audio", Source: pricingSourceGoogleVertex},
			{Unit: "video_second_no_audio", USD: 0.10, Description: "Veo 3.1 Fast video without audio", Source: pricingSourceGoogleVertex},
		},
	},
	{
		Capability: "generate_video",
		Provider:   string(llm.ProviderVertex),
		Models:     []string{"veo-3.1-lite-generate-001"},
		Rules: []llmPricingRule{
			{Unit: "video_second", USD: 0.15, Description: "Veo 3.1 Fast/Lite-class video with audio. Verify SKU before high-volume use.", EstimateOnly: true, Source: pricingSourceGoogleVertex},
			{Unit: "video_second_no_audio", USD: 0.10, Description: "Veo 3.1 Fast/Lite-class video without audio. Verify SKU before high-volume use.", EstimateOnly: true, Source: pricingSourceGoogleVertex},
		},
	},
	{
		Capability: "text_to_speech",
		Provider:   string(llm.ProviderVertex),
		Models:     []string{"gemini-3.1-flash-tts-preview", "gemini-2.5-flash-preview-tts"},
		Rules: []llmPricingRule{
			{Unit: "input_text_1m_tokens", USD: 0.50, Description: "Gemini Flash Preview TTS text input. Exact Gemini 3.1 TTS SKU was not published when checked.", EstimateOnly: true, Source: pricingSourceGeminiAPI},
			{Unit: "output_audio_1m_tokens", USD: 10.00, Description: "Gemini Flash Preview TTS audio output. Exact Gemini 3.1 TTS SKU was not published when checked.", EstimateOnly: true, Source: pricingSourceGeminiAPI},
		},
		Notes: []string{"For Gemini TTS, pass input_tokens and output_audio_tokens for the closest estimate. characters are converted to input tokens at roughly 4 chars/token."},
	},
	{
		Capability: "text_to_speech",
		Provider:   string(llm.ProviderElevenLabs),
		Models:     []string{"eleven_multilingual_v2", "eleven_v3"},
		Rules:      []llmPricingRule{{Unit: "1k_characters", USD: 0.10, Description: "ElevenLabs Multilingual v2/v3 TTS", Source: pricingSourceElevenLabs}},
	},
	{
		Capability: "text_to_speech",
		Provider:   string(llm.ProviderElevenLabs),
		Models:     []string{"eleven_turbo_v2_5", "eleven_flash_v2_5"},
		Rules:      []llmPricingRule{{Unit: "1k_characters", USD: 0.05, Description: "ElevenLabs Flash/Turbo TTS", Source: pricingSourceElevenLabs}},
	},
	{
		Capability: "text_to_speech",
		Provider:   string(llm.ProviderDeepgram),
		Models:     []string{"aura-2-thalia-en", "aura-2-luna-en", "aura-2-asteria-en", "aura-2-apollo-en"},
		Rules:      []llmPricingRule{{Unit: "1k_characters", USD: 0.030, Description: "Deepgram Aura-2 TTS", Source: pricingSourceDeepgram}},
	},
	{
		Capability: "text_to_speech",
		Provider:   string(llm.ProviderMiniMax),
		Models:     []string{"speech-2.8-turbo", "speech-2.6-turbo", "speech-02-turbo"},
		Rules:      []llmPricingRule{{Unit: "1k_characters", USD: 0.060, Description: "MiniMax speech turbo TTS", Source: pricingSourceMiniMax}},
	},
	{
		Capability: "text_to_speech",
		Provider:   string(llm.ProviderMiniMax),
		Models:     []string{"speech-2.8-hd", "speech-2.6-hd", "speech-02-hd"},
		Rules:      []llmPricingRule{{Unit: "1k_characters", USD: 0.10, Description: "MiniMax speech HD TTS", Source: pricingSourceMiniMax}},
	},
	{
		Capability: "speech_to_text",
		Provider:   string(llm.ProviderDeepgram),
		Models:     []string{"nova-3"},
		Default:    true,
		Rules:      []llmPricingRule{{Unit: "audio_minute", USD: 0.0048, Description: "Deepgram Nova-3 monolingual STT", Source: pricingSourceDeepgram}},
		Notes:      []string{"Nova-3 multilingual is $0.0058/min; use model_id nova-3-multilingual to estimate that rate."},
	},
	{
		Capability: "speech_to_text",
		Provider:   string(llm.ProviderDeepgram),
		Models:     []string{"nova-3-multilingual"},
		Rules:      []llmPricingRule{{Unit: "audio_minute", USD: 0.0058, Description: "Deepgram Nova-3 multilingual STT", Source: pricingSourceDeepgram}},
	},
	{
		Capability: "generate_music",
		Provider:   string(llm.ProviderElevenLabs),
		Models:     []string{"music_v1"},
		Default:    true,
		Rules:      []llmPricingRule{{Unit: "audio_minute", USD: 0.30, Description: "ElevenLabs Music API", Source: pricingSourceElevenLabs}},
		Notes:      []string{"Paid users only; API supports 3 seconds to 10 minutes per request."},
	},
	{
		Capability: "generate_music",
		Provider:   string(llm.ProviderMiniMax),
		Models:     []string{"music-2.6", "music-2.6-free", "music-cover", "music-cover-free"},
		Rules:      []llmPricingRule{{Unit: "song_up_to_5_min", USD: 0.15, Description: "MiniMax Music 2.5/2.6-style song generation up to 5 minutes. MiniMax 2.6 pay-as-you-go SKU should be verified before high-volume use.", EstimateOnly: true, Source: pricingSourceMiniMax}},
		Notes:      []string{"MiniMax official pay-as-you-go page listed music generation by song/up-to-5-min pricing when checked; 2.6-specific pricing may be plan/quota based."},
	},
}

func runtimeAvailable(command string) *bool {
	if command == "" {
		return nil
	}
	_, err := exec.LookPath(command)
	ok := err == nil
	return &ok
}

func providerRuntime(provider string) string {
	switch normalizeManagedProvider(provider) {
	case string(llm.ProviderClaudeCode):
		return "claude"
	case string(llm.ProviderCodexCLI):
		return "codex"
	case string(llm.ProviderGeminiCLI):
		return "gemini"
	case string(llm.ProviderMiniMaxCodingPlan):
		return "mmx"
	default:
		return ""
	}
}

func providerAuthConfigured(provider string, keys *llm.ProviderAPIKeys) (bool, string) {
	if keys == nil {
		keys = &llm.ProviderAPIKeys{}
	}
	switch normalizeManagedProvider(provider) {
	case string(llm.ProviderOpenRouter):
		return keys.OpenRouter != nil && strings.TrimSpace(*keys.OpenRouter) != "", "OPENROUTER_API_KEY or workspace provider auth"
	case string(llm.ProviderOpenAI):
		return keys.OpenAI != nil && strings.TrimSpace(*keys.OpenAI) != "", "OPENAI_API_KEY or workspace provider auth"
	case string(llm.ProviderAnthropic):
		return keys.Anthropic != nil && strings.TrimSpace(*keys.Anthropic) != "", "ANTHROPIC_API_KEY or workspace provider auth"
	case string(llm.ProviderClaudeCode):
		return keys.Anthropic != nil && strings.TrimSpace(*keys.Anthropic) != "", "ANTHROPIC_API_KEY or workspace provider auth"
	case string(llm.ProviderZAI):
		return keys.ZAI != nil && strings.TrimSpace(*keys.ZAI) != "", "Z_AI_API_KEY/ZAI_API_KEY or workspace provider auth"
	case string(llm.ProviderKimi):
		return keys.Kimi != nil && strings.TrimSpace(*keys.Kimi) != "", "KIMI_API_KEY or workspace provider auth"
	case string(llm.ProviderVertex):
		return keys.Vertex != nil && strings.TrimSpace(*keys.Vertex) != "", "VERTEX_API_KEY/GOOGLE_API_KEY/GEMINI_API_KEY/ADC or workspace provider auth"
	case string(llm.ProviderGeminiCLI):
		return keys.GeminiCLI != nil && strings.TrimSpace(*keys.GeminiCLI) != "", "GEMINI_API_KEY or workspace provider auth"
	case string(llm.ProviderCodexCLI):
		return true, "Codex CLI login or CODEX_API_KEY/workspace provider auth"
	case string(llm.ProviderMiniMax):
		return keys.MiniMax != nil && strings.TrimSpace(*keys.MiniMax) != "", "MINIMAX_API_KEY or workspace provider auth"
	case string(llm.ProviderMiniMaxCodingPlan):
		return keys.MiniMaxCodingPlan != nil && strings.TrimSpace(*keys.MiniMaxCodingPlan) != "", "MINIMAX_CODING_PLAN_API_KEY or workspace provider auth"
	case string(llm.ProviderElevenLabs):
		return keys.ElevenLabs != nil && strings.TrimSpace(*keys.ElevenLabs) != "", "ELEVENLABS_API_KEY or workspace provider auth"
	case string(llm.ProviderDeepgram):
		return keys.Deepgram != nil && strings.TrimSpace(*keys.Deepgram) != "", "DEEPGRAM_API_KEY or workspace provider auth"
	case string(llm.ProviderBedrock):
		return keys.Bedrock != nil && strings.TrimSpace(keys.Bedrock.Region) != "", "BEDROCK_REGION or workspace provider auth"
	case string(llm.ProviderAzure):
		return keys.Azure != nil && strings.TrimSpace(keys.Azure.APIKey) != "" && strings.TrimSpace(keys.Azure.Endpoint) != "", "AZURE_AI_ENDPOINT/AZURE_AI_API_KEY or workspace provider auth"
	default:
		return false, "unknown provider"
	}
}

func providerUsable(provider string, authConfigured bool) (bool, string, *bool) {
	runtime := providerRuntime(provider)
	runtimeOK := runtimeAvailable(runtime)
	usable := authConfigured
	if runtimeOK != nil {
		usable = usable && *runtimeOK
	}
	return usable, runtime, runtimeOK
}

func buildChatLLMCapabilities(keys *llm.ProviderAPIKeys, includeModels bool) []llmCapabilityProvider {
	metadata := utils.GetAllModelMetadata()
	modelsByProvider := map[string][]string{}
	for _, model := range metadata {
		if model == nil {
			continue
		}
		provider := normalizeManagedProvider(model.Provider)
		modelID := strings.TrimSpace(model.ModelID)
		if provider == "" || modelID == "" {
			continue
		}
		modelsByProvider[provider] = append(modelsByProvider[provider], modelID)
	}

	providers := getSupportedProviders()
	result := make([]llmCapabilityProvider, 0, len(providers))
	for _, provider := range providers {
		authConfigured, authSource := providerAuthConfigured(provider, keys)
		usable, runtime, runtimeOK := providerUsable(provider, authConfigured)
		entry := llmCapabilityProvider{
			Provider:          provider,
			ModelCount:        len(modelsByProvider[provider]),
			DefaultModel:      llm.GetDefaultModel(llm.Provider(provider)),
			AuthSource:        authSource,
			AuthConfigured:    authConfigured,
			RuntimeDependency: runtime,
			RuntimeAvailable:  runtimeOK,
			Usable:            usable,
			Notes:             []string{"Use list_provider_models for full chat/text model metadata."},
		}
		if includeModels {
			entry.Models = modelsByProvider[provider]
		}
		result = append(result, entry)
	}
	return result
}

func buildFixedCapabilityProviders(keys *llm.ProviderAPIKeys, configFile string, providerModels map[string][]string, defaults map[string]string, notes map[string][]string) []llmCapabilityProvider {
	result := make([]llmCapabilityProvider, 0, len(providerModels))
	for _, provider := range []string{
		string(llm.ProviderVertex),
		string(llm.ProviderMiniMax),
		string(llm.ProviderMiniMaxCodingPlan),
		string(llm.ProviderCodexCLI),
		string(llm.ProviderZAI),
		string(llm.ProviderKimi),
		string(llm.ProviderClaudeCode),
		string(llm.ProviderGeminiCLI),
		string(llm.ProviderElevenLabs),
		string(llm.ProviderDeepgram),
	} {
		models, ok := providerModels[provider]
		if !ok {
			continue
		}
		authConfigured, authSource := providerAuthConfigured(provider, keys)
		usable, runtime, runtimeOK := providerUsable(provider, authConfigured)
		result = append(result, llmCapabilityProvider{
			Provider:          provider,
			Models:            models,
			ModelCount:        len(models),
			DefaultModel:      defaults[provider],
			ConfigFile:        configFile,
			AuthSource:        authSource,
			AuthConfigured:    authConfigured,
			RuntimeDependency: runtime,
			RuntimeAvailable:  runtimeOK,
			Usable:            usable,
			Notes:             notes[provider],
		})
	}
	return result
}

func pricingForProvider(capability, provider string) []llmPricingCatalogEntry {
	capability = normalizeManagedProvider(capability)
	provider = normalizeManagedProvider(provider)
	var matches []llmPricingCatalogEntry
	for _, entry := range llmPricingCatalog {
		if normalizeManagedProvider(entry.Capability) == capability && normalizeManagedProvider(entry.Provider) == provider {
			matches = append(matches, entry)
		}
	}
	return matches
}

func attachPricing(capability string, providers []llmCapabilityProvider) []llmCapabilityProvider {
	for i := range providers {
		pricing := pricingForProvider(capability, providers[i].Provider)
		if len(pricing) == 0 {
			continue
		}
		if providers[i].Extra == nil {
			providers[i].Extra = map[string]interface{}{}
		}
		providers[i].Extra["pricing"] = pricing
	}
	return providers
}

func buildLLMCapabilities(ctx context.Context, capability string, includeModels bool) map[string]interface{} {
	keys := MergedProviderAPIKeys(ctx)
	capability = normalizeManagedProvider(capability)
	if capability == "" {
		capability = "all"
	}

	all := map[string]interface{}{
		"schema_version": 1,
		"notes": []string{
			"usable means required workspace/env auth is configured and any required CLI runtime is installed.",
			"Provider auth is managed in config/provider-api-keys.json; do not hand-edit that encrypted file.",
			"Pricing is a static snapshot and should be treated as an estimate; verify provider pricing pages before high-volume runs.",
		},
	}

	if capability == "all" || capability == "chat" || capability == "text" {
		all["chat"] = map[string]interface{}{
			"description": "Providers usable for normal chat/text LLM calls.",
			"providers":   buildChatLLMCapabilities(keys, includeModels),
		}
	}

	if capability == "all" || capability == "search" || capability == "search_web" {
		all["search_web"] = map[string]interface{}{
			"description": "Providers usable by search_web_llm. Routing is configured in config/published-llms.json with search_role/search_priority.",
			"config_file": "config/published-llms.json",
			"providers": buildFixedCapabilityProviders(
				keys,
				"config/published-llms.json",
				map[string][]string{
					string(llm.ProviderClaudeCode):        {"claude-code"},
					string(llm.ProviderCodexCLI):          {"codex-cli", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.3-codex-spark"},
					string(llm.ProviderGeminiCLI):         {"gemini CLI models"},
					string(llm.ProviderMiniMaxCodingPlan): {"MiniMax coding-plan search via mmx"},
					string(llm.ProviderVertex):            {"gemini* models only"},
				},
				map[string]string{},
				map[string][]string{
					string(llm.ProviderVertex): {"Only published Vertex models whose model_id starts with gemini are search-capable."},
				},
			),
			"routing_fields": map[string]interface{}{
				"search_role":     []string{"primary", "fallback"},
				"search_priority": "lower number wins within the same role",
			},
		}
	}

	if capability == "all" || capability == "read_image" || capability == "image_analysis" {
		all["read_image"] = map[string]interface{}{
			"description": "Providers usable by read_image for image understanding.",
			"config_file": "config/image-analysis-config.json",
			"providers": buildFixedCapabilityProviders(
				keys,
				"config/image-analysis-config.json",
				map[string][]string{
					string(llm.ProviderVertex):            {"gemini-3-pro-preview", "gemini-3-flash-preview", "gemini-3.1-flash-lite-preview"},
					string(llm.ProviderMiniMaxCodingPlan): {"claude-sonnet-4-5", "claude-opus-4-6", "claude-haiku-4-5-20251001"},
					string(llm.ProviderZAI):               {"glm-4.6v", "glm-5v-turbo"},
					string(llm.ProviderKimi):              {"kimi-k2.6"},
				},
				map[string]string{
					string(llm.ProviderVertex):            "gemini-3-pro-preview",
					string(llm.ProviderMiniMaxCodingPlan): "claude-sonnet-4-5",
					string(llm.ProviderZAI):               "glm-4.6v",
					string(llm.ProviderKimi):              "kimi-k2.6",
				},
				map[string][]string{
					string(llm.ProviderMiniMaxCodingPlan): {"Use minimax-coding-plan, not plain minimax, for image understanding."},
				},
			),
		}
	}

	if capability == "all" || capability == "read_video" || capability == "video_analysis" {
		kimiAuth, kimiAuthSource := providerAuthConfigured(string(llm.ProviderKimi), keys)
		kimiUsable, kimiRuntime, kimiRuntimeOK := providerUsable(string(llm.ProviderKimi), kimiAuth)
		zaiAuth, zaiAuthSource := providerAuthConfigured(string(llm.ProviderZAI), keys)
		zaiRuntimeOK := runtimeAvailable("npx")
		zaiUsable := zaiAuth && zaiRuntimeOK != nil && *zaiRuntimeOK
		all["read_video"] = map[string]interface{}{
			"description": "Providers usable by read_video for video understanding.",
			"providers": []llmCapabilityProvider{
				{
					Provider:          string(llm.ProviderKimi),
					Models:            []string{"kimi-k2.6"},
					ModelCount:        1,
					DefaultModel:      "kimi-k2.6",
					AuthSource:        kimiAuthSource,
					AuthConfigured:    kimiAuth,
					RuntimeDependency: kimiRuntime,
					RuntimeAvailable:  kimiRuntimeOK,
					Usable:            kimiUsable,
					Notes:             []string{"Uploads videos to Moonshot/Kimi file storage and references them as ms://<file-id>.", "Supported formats: mp4, mpeg, mov, avi, flv, mpg, webm, wmv, 3gp, 3gpp."},
				},
				{
					Provider:          string(llm.ProviderZAI),
					Models:            []string{"glm-4.6v"},
					ModelCount:        1,
					DefaultModel:      "glm-4.6v",
					AuthSource:        zaiAuthSource,
					AuthConfigured:    zaiAuth,
					RuntimeDependency: "npx",
					RuntimeAvailable:  zaiRuntimeOK,
					Usable:            zaiUsable,
					Notes:             []string{"Uses Z.AI Vision MCP server tool video_analysis via npx -y @z_ai/mcp-server@latest.", "Supported formats: mp4, mov, m4v. Max file size: 8 MB."},
				},
			},
		}
	}

	if capability == "all" || capability == "generate_image" || capability == "image_generation" {
		all["generate_image"] = map[string]interface{}{
			"description": "Providers usable by image_gen/image_edit.",
			"config_file": "config/image-generation-config.json",
			"providers": buildFixedCapabilityProviders(
				keys,
				"config/image-generation-config.json",
				map[string][]string{
					string(llm.ProviderVertex):            {"gemini-3.1-flash-image-preview", "gemini-3-pro-image-preview"},
					string(llm.ProviderMiniMaxCodingPlan): {"image-01"},
					string(llm.ProviderCodexCLI):          {"codex-cli", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.3-codex-spark"},
				},
				map[string]string{
					string(llm.ProviderVertex):            "gemini-3.1-flash-image-preview",
					string(llm.ProviderMiniMaxCodingPlan): "image-01",
					string(llm.ProviderCodexCLI):          "codex-cli",
				},
				map[string][]string{},
			),
		}
	}

	if capability == "all" || capability == "generate_video" || capability == "video_generation" {
		all["generate_video"] = map[string]interface{}{
			"description": "Providers usable by generate_video.",
			"providers": attachPricing("generate_video", buildFixedCapabilityProviders(
				keys,
				"",
				map[string][]string{
					string(llm.ProviderVertex): {"veo-3.1-generate-preview", "veo-3.1-fast-generate-preview", "veo-3.1-generate-001", "veo-3.1-fast-generate-001", "veo-3.1-lite-generate-001"},
				},
				map[string]string{
					string(llm.ProviderVertex): "veo-3.1-generate-preview",
				},
				map[string][]string{
					string(llm.ProviderVertex): {"Uses Gemini API-key auth for preview models or Vertex AI project auth/ADC for GA models."},
				},
			)),
		}
	}

	if capability == "all" || capability == "text_to_speech" || capability == "audio_generation" {
		all["text_to_speech"] = map[string]interface{}{
			"description": "Providers usable by text_to_speech.",
			"providers": attachPricing("text_to_speech", buildFixedCapabilityProviders(
				keys,
				"",
				map[string][]string{
					string(llm.ProviderVertex):     {"gemini-3.1-flash-tts-preview"},
					string(llm.ProviderMiniMax):    {"speech-2.8-turbo", "speech-2.8-hd", "speech-2.6-turbo", "speech-2.6-hd", "speech-02-turbo", "speech-02-hd"},
					string(llm.ProviderElevenLabs): {"eleven_multilingual_v2", "eleven_turbo_v2_5", "eleven_flash_v2_5", "eleven_v3"},
					string(llm.ProviderDeepgram):   {"aura-2-thalia-en", "aura-2-luna-en", "aura-2-asteria-en", "aura-2-apollo-en"},
				},
				map[string]string{
					string(llm.ProviderVertex):     "gemini-3.1-flash-tts-preview",
					string(llm.ProviderMiniMax):    "speech-2.8-turbo",
					string(llm.ProviderElevenLabs): "eleven_multilingual_v2",
					string(llm.ProviderDeepgram):   "aura-2-thalia-en",
				},
				map[string][]string{
					string(llm.ProviderVertex):     {"Uses Gemini API-key auth via provider-api-keys vertex/GEMINI_API_KEY and returns WAV audio."},
					string(llm.ProviderMiniMax):    {"Uses MiniMax API-key auth via provider-api-keys minimax/MINIMAX_API_KEY and returns MP3 audio by default."},
					string(llm.ProviderElevenLabs): {"Uses ElevenLabs API-key auth via provider-api-keys elevenlabs/ELEVENLABS_API_KEY and returns MP3 audio by default."},
					string(llm.ProviderDeepgram):   {"Uses Deepgram API-key auth via provider-api-keys deepgram/DEEPGRAM_API_KEY and returns MP3 audio by default."},
				},
			)),
		}
	}

	if capability == "all" || capability == "speech_to_text" || capability == "audio_transcription" {
		all["speech_to_text"] = map[string]interface{}{
			"description": "Providers usable by speech_to_text.",
			"providers": attachPricing("speech_to_text", buildFixedCapabilityProviders(
				keys,
				"",
				map[string][]string{
					string(llm.ProviderDeepgram): {"nova-3", "nova-3-multilingual", "nova-2", "base"},
				},
				map[string]string{
					string(llm.ProviderDeepgram): "nova-3",
				},
				map[string][]string{
					string(llm.ProviderDeepgram): {"Uses Deepgram API-key auth via provider-api-keys deepgram/DEEPGRAM_API_KEY."},
				},
			)),
		}
	}

	if capability == "all" || capability == "generate_music" || capability == "music_generation" {
		all["generate_music"] = map[string]interface{}{
			"description": "Providers usable by generate_music.",
			"providers": attachPricing("generate_music", buildFixedCapabilityProviders(
				keys,
				"",
				map[string][]string{
					string(llm.ProviderElevenLabs): {"music_v1"},
					string(llm.ProviderMiniMax):    {"music-2.6", "music-2.6-free", "music-cover", "music-cover-free"},
				},
				map[string]string{
					string(llm.ProviderElevenLabs): "music_v1",
					string(llm.ProviderMiniMax):    "music-2.6",
				},
				map[string][]string{
					string(llm.ProviderElevenLabs): {"Uses ElevenLabs API-key auth via provider-api-keys elevenlabs/ELEVENLABS_API_KEY and returns audio directly."},
					string(llm.ProviderMiniMax):    {"Uses MiniMax API-key auth via provider-api-keys minimax/MINIMAX_API_KEY and returns MP3 audio decoded from hex output."},
				},
			)),
		}
	}

	return all
}

func buildLLMCapabilityPromptSection(ctx context.Context) string {
	capabilities := buildLLMCapabilities(ctx, "all", false)
	orderedCapabilities := []string{
		"chat",
		"search_web",
		"read_image",
		"read_video",
		"generate_image",
		"generate_video",
		"text_to_speech",
		"speech_to_text",
		"generate_music",
	}

	var lines []string
	for _, capability := range orderedCapabilities {
		entry, _ := capabilities[capability].(map[string]interface{})
		if entry == nil {
			continue
		}
		providers, _ := entry["providers"].([]llmCapabilityProvider)
		if len(providers) == 0 {
			continue
		}

		var providerSummaries []string
		for _, provider := range providers {
			if strings.TrimSpace(provider.Provider) == "" {
				continue
			}
			summary := provider.Provider
			if provider.DefaultModel != "" {
				summary += " (" + provider.DefaultModel + ")"
			}
			if provider.Usable {
				summary += " usable"
			} else if provider.AuthConfigured {
				summary += " auth configured"
			} else {
				summary += " auth missing"
			}
			providerSummaries = append(providerSummaries, summary)
		}
		if len(providerSummaries) == 0 {
			continue
		}
		lines = append(lines, "- `"+capability+"`: "+strings.Join(providerSummaries, ", "))
	}

	if len(lines) == 0 {
		return ""
	}

	return `## Workspace LLM And Media Capability Snapshot

Published LLM entries are chat/text routing entries. Provider-backed media tools can be available even when they are not listed as published LLMs. Use ` + "`list_llm_capabilities`" + ` for authoritative provider, model, auth, runtime, and pricing status before answering availability questions or selecting a provider.

For audio/music requests, call the dedicated ` + "`text_to_speech`" + ` or ` + "`generate_music`" + ` tool directly. If provider auth is missing and the user supplied a key, call ` + "`set_provider_auth`" + `; never paste provider keys into shell commands, scripts, curl calls, or logs.

` + strings.Join(lines, "\n")
}

func normalizePricingCapability(capability string) string {
	switch normalizeManagedProvider(capability) {
	case "video", "video_generation":
		return "generate_video"
	case "tts", "audio_generation":
		return "text_to_speech"
	case "stt", "audio_transcription":
		return "speech_to_text"
	case "music", "music_generation":
		return "generate_music"
	default:
		return normalizeManagedProvider(capability)
	}
}

func pricingEntryFor(capability, provider, modelID string) *llmPricingCatalogEntry {
	capability = normalizePricingCapability(capability)
	provider = normalizeManagedProvider(provider)
	modelID = strings.TrimSpace(modelID)

	var providerFallback *llmPricingCatalogEntry
	var defaultFallback *llmPricingCatalogEntry
	for i := range llmPricingCatalog {
		entry := &llmPricingCatalog[i]
		if normalizePricingCapability(entry.Capability) != capability || normalizeManagedProvider(entry.Provider) != provider {
			continue
		}
		if providerFallback == nil {
			providerFallback = entry
		}
		if entry.Default {
			defaultFallback = entry
		}
		for _, model := range entry.Models {
			if strings.EqualFold(strings.TrimSpace(model), modelID) {
				return entry
			}
		}
	}
	if modelID == "" && defaultFallback != nil {
		return defaultFallback
	}
	return providerFallback
}

func firstPricingRule(entry *llmPricingCatalogEntry, unit string) *llmPricingRule {
	if entry == nil {
		return nil
	}
	unit = strings.TrimSpace(unit)
	for i := range entry.Rules {
		if entry.Rules[i].Unit == unit {
			return &entry.Rules[i]
		}
	}
	if len(entry.Rules) == 0 {
		return nil
	}
	return &entry.Rules[0]
}

func numberArg(args map[string]interface{}, name string) float64 {
	switch v := args[name].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

func estimateLLMCost(args map[string]interface{}) (map[string]interface{}, error) {
	capabilityRaw, _ := args["capability"].(string)
	providerRaw, _ := args["provider"].(string)
	capability := normalizePricingCapability(capabilityRaw)
	provider := normalizeManagedProvider(providerRaw)
	modelID, _ := args["model_id"].(string)
	if capability == "" || provider == "" {
		return nil, fmt.Errorf("capability and provider are required")
	}

	entry := pricingEntryFor(capability, provider, modelID)
	if entry == nil {
		return nil, fmt.Errorf("no pricing metadata found for capability %q provider %q model %q", capability, provider, modelID)
	}

	count := numberArg(args, "count")
	if count <= 0 {
		count = 1
	}
	characters := numberArg(args, "characters")
	seconds := numberArg(args, "seconds")
	minutes := numberArg(args, "minutes")
	inputTokens := numberArg(args, "input_tokens")
	outputAudioTokens := numberArg(args, "output_audio_tokens")
	withAudio, hasWithAudio := args["with_audio"].(bool)
	if !hasWithAudio {
		withAudio = true
	}

	var usage float64
	var unit string
	var cost float64
	var rulesUsed []llmPricingRule

	switch capability {
	case "generate_video":
		if seconds <= 0 {
			return nil, fmt.Errorf("seconds is required for generate_video cost estimates")
		}
		unit = "video_second"
		if !withAudio {
			unit = "video_second_no_audio"
		}
		rule := firstPricingRule(entry, unit)
		if rule == nil {
			return nil, fmt.Errorf("no %s pricing rule found", unit)
		}
		usage = seconds * count
		cost = usage * rule.USD
		rulesUsed = append(rulesUsed, *rule)
	case "text_to_speech":
		if provider == string(llm.ProviderVertex) {
			if inputTokens <= 0 && characters > 0 {
				inputTokens = characters / 4
			}
			inputRule := firstPricingRule(entry, "input_text_1m_tokens")
			outputRule := firstPricingRule(entry, "output_audio_1m_tokens")
			if inputRule != nil && inputTokens > 0 {
				cost += (inputTokens / 1_000_000) * inputRule.USD * count
				rulesUsed = append(rulesUsed, *inputRule)
			}
			if outputRule != nil && outputAudioTokens > 0 {
				cost += (outputAudioTokens / 1_000_000) * outputRule.USD * count
				rulesUsed = append(rulesUsed, *outputRule)
			}
			if cost == 0 {
				return nil, fmt.Errorf("Gemini TTS estimates require characters/input_tokens and ideally output_audio_tokens")
			}
			unit = "tokens"
			usage = inputTokens + outputAudioTokens
		} else {
			if characters <= 0 {
				return nil, fmt.Errorf("characters is required for text_to_speech cost estimates for provider %q", provider)
			}
			rule := firstPricingRule(entry, "1k_characters")
			if rule == nil {
				return nil, fmt.Errorf("no character pricing rule found")
			}
			usage = (characters / 1000) * count
			unit = "1k_characters"
			cost = usage * rule.USD
			rulesUsed = append(rulesUsed, *rule)
		}
	case "speech_to_text":
		if minutes <= 0 && seconds > 0 {
			minutes = seconds / 60
		}
		if minutes <= 0 {
			return nil, fmt.Errorf("minutes or seconds is required for speech_to_text cost estimates")
		}
		rule := firstPricingRule(entry, "audio_minute")
		if rule == nil {
			return nil, fmt.Errorf("no audio minute pricing rule found")
		}
		usage = minutes * count
		unit = "audio_minute"
		cost = usage * rule.USD
		rulesUsed = append(rulesUsed, *rule)
	case "generate_music":
		if provider == string(llm.ProviderElevenLabs) {
			if minutes <= 0 && seconds > 0 {
				minutes = seconds / 60
			}
			if minutes <= 0 {
				return nil, fmt.Errorf("minutes or seconds is required for ElevenLabs generate_music cost estimates")
			}
			rule := firstPricingRule(entry, "audio_minute")
			if rule == nil {
				return nil, fmt.Errorf("no audio minute pricing rule found")
			}
			usage = minutes * count
			unit = "audio_minute"
			cost = usage * rule.USD
			rulesUsed = append(rulesUsed, *rule)
		} else {
			rule := firstPricingRule(entry, "song_up_to_5_min")
			if rule == nil {
				return nil, fmt.Errorf("no song pricing rule found")
			}
			usage = count
			unit = "song_up_to_5_min"
			cost = usage * rule.USD
			rulesUsed = append(rulesUsed, *rule)
		}
	default:
		return nil, fmt.Errorf("unsupported priced capability %q", capability)
	}

	return map[string]interface{}{
		"capability":       capability,
		"provider":         provider,
		"model_id":         strings.TrimSpace(modelID),
		"estimated_cost":   cost,
		"currency":         "USD",
		"usage":            usage,
		"usage_unit":       unit,
		"count":            count,
		"pricing":          entry,
		"rules_used":       rulesUsed,
		"estimate_warning": "Static pricing snapshot; verify provider pricing before high-volume runs.",
	}, nil
}

func (api *StreamingAPI) registerMultiAgentLLMTools(underlyingAgent *mcpagent.Agent) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	registerTool := func(name, description string, params map[string]interface{}, exec func(context.Context, map[string]interface{}) (string, error)) error {
		return underlyingAgent.RegisterCustomTool(name, description, params, exec, "llm_config_tools")
	}

	if err := registerLLMCapabilityTools(registerTool); err != nil {
		return err
	}

	return nil
}

func registerLLMCapabilityDiscoveryTools(registerTool func(string, string, map[string]interface{}, func(context.Context, map[string]interface{}) (string, error)) error) error {
	if err := registerTool(
		"list_llm_capabilities",
		"List supported and currently usable LLM providers/models by capability: chat, search_web, read_image, read_video, generate_image, generate_video, text_to_speech, speech_to_text, and generate_music. Includes config files, auth requirements, CLI runtime availability, and static pricing metadata where available.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"capability": map[string]interface{}{
					"type":        "string",
					"description": "Optional filter. Supported values: all, chat, search_web, read_image, read_video, generate_image, generate_video, text_to_speech, speech_to_text, generate_music.",
				},
				"include_models": map[string]interface{}{
					"type":        "boolean",
					"description": "When true, include full chat/text model id lists. Defaults to false because chat catalogs can be large.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			capability, _ := args["capability"].(string)
			includeModels, _ := args["include_models"].(bool)
			return prettyJSON(buildLLMCapabilities(ctx, capability, includeModels)), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"estimate_llm_cost",
		"Estimate cost for priced generation/transcription capabilities using static pricing metadata. Supports generate_video, text_to_speech, speech_to_text, and generate_music.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"capability": map[string]interface{}{
					"type":        "string",
					"description": "Required. Supported values: generate_video, text_to_speech, speech_to_text, generate_music. Aliases: video, tts, stt, music.",
				},
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Required provider id, e.g. vertex, minimax, elevenlabs, deepgram.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional model id. If omitted, provider default pricing is used where available.",
				},
				"characters": map[string]interface{}{
					"type":        "number",
					"description": "Text characters for character-metered TTS providers. For Gemini TTS this is converted to input tokens at roughly 4 chars/token.",
				},
				"seconds": map[string]interface{}{
					"type":        "number",
					"description": "Video seconds for generate_video, audio seconds for speech_to_text, or music seconds for ElevenLabs generate_music.",
				},
				"minutes": map[string]interface{}{
					"type":        "number",
					"description": "Audio minutes for speech_to_text or ElevenLabs generate_music.",
				},
				"count": map[string]interface{}{
					"type":        "number",
					"description": "Number of generated items/files. Defaults to 1.",
				},
				"with_audio": map[string]interface{}{
					"type":        "boolean",
					"description": "For video estimates. Defaults to true.",
				},
				"input_tokens": map[string]interface{}{
					"type":        "number",
					"description": "Input text token count for token-metered providers such as Gemini TTS.",
				},
				"output_audio_tokens": map[string]interface{}{
					"type":        "number",
					"description": "Output audio token count for token-metered providers such as Gemini TTS.",
				},
			},
			"required": []string{"capability", "provider"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			estimate, err := estimateLLMCost(args)
			if err != nil {
				return "", err
			}
			return prettyJSON(estimate), nil
		},
	); err != nil {
		return err
	}

	return nil
}

func (api *StreamingAPI) registerWorkflowLLMDiscoveryTools(underlyingAgent *mcpagent.Agent) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}
	registerTool := func(name, description string, params map[string]interface{}, exec func(context.Context, map[string]interface{}) (string, error)) error {
		return underlyingAgent.RegisterCustomTool(name, description, params, exec, "llm_config_tools")
	}
	return registerLLMCapabilityDiscoveryTools(registerTool)
}

func registerLLMCapabilityTools(registerTool func(string, string, map[string]interface{}, func(context.Context, map[string]interface{}) (string, error)) error) error {
	if err := registerTool(
		"list_llm_capabilities",
		"List supported and currently usable LLM providers/models by capability: chat, search_web, read_image, read_video, generate_image, generate_video, text_to_speech, speech_to_text, and generate_music. Includes config files, auth requirements, CLI runtime availability, and static pricing metadata where available.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"capability": map[string]interface{}{
					"type":        "string",
					"description": "Optional filter. Supported values: all, chat, search_web, read_image, read_video, generate_image, generate_video, text_to_speech, speech_to_text, generate_music.",
				},
				"include_models": map[string]interface{}{
					"type":        "boolean",
					"description": "When true, include full chat/text model id lists. Defaults to false because chat catalogs can be large.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			capability, _ := args["capability"].(string)
			includeModels, _ := args["include_models"].(bool)
			return prettyJSON(buildLLMCapabilities(ctx, capability, includeModels)), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"estimate_llm_cost",
		"Estimate cost for priced generation/transcription capabilities using static pricing metadata. Supports generate_video, text_to_speech, speech_to_text, and generate_music.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"capability": map[string]interface{}{
					"type":        "string",
					"description": "Required. Supported values: generate_video, text_to_speech, speech_to_text, generate_music. Aliases: video, tts, stt, music.",
				},
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Required provider id, e.g. vertex, minimax, elevenlabs, deepgram.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional model id. If omitted, provider default pricing is used where available.",
				},
				"characters": map[string]interface{}{
					"type":        "number",
					"description": "Text characters for character-metered TTS providers. For Gemini TTS this is converted to input tokens at roughly 4 chars/token.",
				},
				"seconds": map[string]interface{}{
					"type":        "number",
					"description": "Video seconds for generate_video, audio seconds for speech_to_text, or music seconds for ElevenLabs generate_music.",
				},
				"minutes": map[string]interface{}{
					"type":        "number",
					"description": "Audio minutes for speech_to_text or ElevenLabs generate_music.",
				},
				"count": map[string]interface{}{
					"type":        "number",
					"description": "Number of generated items/files. Defaults to 1.",
				},
				"with_audio": map[string]interface{}{
					"type":        "boolean",
					"description": "For video estimates. Defaults to true.",
				},
				"input_tokens": map[string]interface{}{
					"type":        "number",
					"description": "Input text token count for token-metered providers such as Gemini TTS.",
				},
				"output_audio_tokens": map[string]interface{}{
					"type":        "number",
					"description": "Output audio token count for token-metered providers such as Gemini TTS.",
				},
			},
			"required": []string{"capability", "provider"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			estimate, err := estimateLLMCost(args)
			if err != nil {
				return "", err
			}
			return prettyJSON(estimate), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"list_provider_models",
		"List the available models for a provider using the same shared metadata catalog the frontend uses from /api/llm-config/models/metadata.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Provider id such as openai, openrouter, anthropic, z-ai, vertex, azure, minimax, minimax-coding-plan, or bedrock.",
				},
			},
			"required": []string{"provider"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			provider := normalizeManagedProvider(fmt.Sprintf("%v", args["provider"]))
			if provider == "" {
				return "provider is required.", nil
			}

			allModels := utils.GetAllModelMetadata()
			filteredModels := make([]interface{}, 0, len(allModels))
			for _, model := range allModels {
				if model == nil {
					continue
				}
				if normalizeManagedProvider(model.Provider) != provider {
					continue
				}
				filteredModels = append(filteredModels, model)
			}

			return prettyJSON(map[string]interface{}{
				"provider": provider,
				"count":    len(filteredModels),
				"models":   filteredModels,
				"source":   "/api/llm-config/models/metadata",
			}), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"test_llm",
		"Validate an LLM provider/model configuration before publishing. Uses workspace-backed provider auth from config/provider-api-keys.json by default, but temporary overrides can be provided in the call.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Provider id such as openai, openrouter, anthropic, z-ai, vertex, azure, minimax, minimax-coding-plan, gemini-cli, claude-code, codex-cli, or bedrock.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional model id to validate.",
				},
				"api_key": map[string]interface{}{
					"type":        "string",
					"description": "Optional temporary API key override. If omitted, the tool uses workspace-backed provider auth when available.",
				},
				"temperature": map[string]interface{}{
					"type":        "number",
					"description": "Optional temperature to merge into validation options.",
				},
				"options": map[string]interface{}{
					"type":                 "object",
					"description":          "Optional model-specific options object, such as reasoning_effort, thinking_level, or Azure endpoint settings.",
					"additionalProperties": true,
				},
				"endpoint": map[string]interface{}{
					"type":        "string",
					"description": "Optional Azure endpoint override. If omitted, the tool uses the workspace-backed Azure endpoint when available.",
				},
				"region": map[string]interface{}{
					"type":        "string",
					"description": "Optional region override for Azure or Bedrock.",
				},
				"api_version": map[string]interface{}{
					"type":        "string",
					"description": "Optional Azure API version override.",
				},
			},
			"required": []string{"provider"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			provider := normalizeManagedProvider(fmt.Sprintf("%v", args["provider"]))
			modelID, _ := args["model_id"].(string)
			apiKey, _ := args["api_key"].(string)
			endpoint, _ := args["endpoint"].(string)
			region, _ := args["region"].(string)
			apiVersion, _ := args["api_version"].(string)
			options, _ := args["options"].(map[string]interface{})

			if provider == "" {
				return "provider is required.", nil
			}

			explicitAPIKeyProvided := strings.TrimSpace(apiKey) != ""
			validationOptions := cloneOptionsMap(options)
			if raw, ok := args["temperature"].(float64); ok {
				if validationOptions == nil {
					validationOptions = map[string]interface{}{}
				}
				validationOptions["temperature"] = raw
			}

			keys, err := LoadProviderKeys(ctx)
			if err != nil {
				return "", err
			}

			usedWorkspaceAuth := false
			if !explicitAPIKeyProvided && keys != nil {
				if value := getStoredProviderAPIKey(keys, provider); value != "" {
					apiKey = value
					usedWorkspaceAuth = true
				}
				switch provider {
				case "bedrock":
					if keys.Bedrock != nil && strings.TrimSpace(region) == "" && keys.Bedrock.Region != "" {
						region = keys.Bedrock.Region
						usedWorkspaceAuth = true
					}
				case "azure":
					if keys.Azure != nil {
						if apiKey == "" && keys.Azure.APIKey != "" {
							apiKey = keys.Azure.APIKey
							usedWorkspaceAuth = true
						}
						if strings.TrimSpace(endpoint) == "" && keys.Azure.Endpoint != "" {
							endpoint = keys.Azure.Endpoint
							usedWorkspaceAuth = true
						}
						if strings.TrimSpace(region) == "" && keys.Azure.Region != "" {
							region = keys.Azure.Region
						}
						if strings.TrimSpace(apiVersion) == "" && keys.Azure.APIVersion != "" {
							apiVersion = keys.Azure.APIVersion
						}
					}
				}
			}

			if endpoint = strings.TrimSpace(endpoint); endpoint != "" {
				if validationOptions == nil {
					validationOptions = map[string]interface{}{}
				}
				validationOptions["endpoint"] = endpoint
			}
			if region = strings.TrimSpace(region); region != "" {
				if validationOptions == nil {
					validationOptions = map[string]interface{}{}
				}
				validationOptions["region"] = region
			}
			if apiVersion = strings.TrimSpace(apiVersion); apiVersion != "" {
				if validationOptions == nil {
					validationOptions = map[string]interface{}{}
				}
				validationOptions["api_version"] = apiVersion
			}

			response := validateProviderConfig(llm.APIKeyValidationRequest{
				Provider: provider,
				APIKey:   strings.TrimSpace(apiKey),
				ModelID:  strings.TrimSpace(modelID),
				Options:  validationOptions,
			})

			return prettyJSON(map[string]interface{}{
				"provider":            provider,
				"model_id":            strings.TrimSpace(modelID),
				"valid":               response.Valid,
				"message":             response.Message,
				"error":               response.Error,
				"corrected_options":   response.CorrectedOptions,
				"used_workspace_auth": usedWorkspaceAuth,
			}), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"save_published_llm",
		"Create or update a workspace-backed published LLM entry in config/published-llms.json. Authentication is managed separately in config/provider-api-keys.json.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Optional existing published LLM id to update. If omitted, the tool upserts by provider/model_id/name.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Display name for the published LLM.",
				},
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Provider id such as openai, openrouter, anthropic, z-ai, vertex, azure, minimax, gemini-cli, claude-code, or codex-cli.",
				},
				"model_id": map[string]interface{}{
					"type":        "string",
					"description": "Model id for the published LLM.",
				},
				"model_name": map[string]interface{}{
					"type":        "string",
					"description": "Optional human-readable model name.",
				},
				"auth_method": map[string]interface{}{
					"type":        "string",
					"description": "Optional auth method label such as api_key, oauth, or none.",
				},
				"context_window": map[string]interface{}{
					"type":        "integer",
					"description": "Optional context window snapshot to persist with the published LLM.",
				},
				"input_cost_per_1m": map[string]interface{}{
					"type":        "number",
					"description": "Optional input token price snapshot in USD per 1M tokens.",
				},
				"output_cost_per_1m": map[string]interface{}{
					"type":        "number",
					"description": "Optional output token price snapshot in USD per 1M tokens.",
				},
				"reasoning_cost_per_1m": map[string]interface{}{
					"type":        "number",
					"description": "Optional reasoning token price snapshot in USD per 1M tokens.",
				},
				"cached_input_cost_per_1m": map[string]interface{}{
					"type":        "number",
					"description": "Optional cached input token price snapshot in USD per 1M tokens.",
				},
				"cached_input_cost_write_per_1m": map[string]interface{}{
					"type":        "number",
					"description": "Optional cache write token price snapshot in USD per 1M tokens.",
				},
				"temperature": map[string]interface{}{
					"type":        "number",
					"description": "Optional temperature override for the published LLM.",
				},
				"options": map[string]interface{}{
					"type":                 "object",
					"description":          "Optional model-specific options object, for example reasoning_effort or thinking_level.",
					"additionalProperties": true,
				},
			},
			"required": []string{"name", "provider", "model_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			name, _ := args["name"].(string)
			provider, _ := args["provider"].(string)
			modelID, _ := args["model_id"].(string)
			modelName, _ := args["model_name"].(string)
			authMethod, _ := args["auth_method"].(string)
			id, _ := args["id"].(string)
			options, _ := args["options"].(map[string]interface{})

			name = strings.TrimSpace(name)
			provider = strings.TrimSpace(provider)
			modelID = strings.TrimSpace(modelID)
			if name == "" || provider == "" || modelID == "" {
				return "name, provider, and model_id are required.", nil
			}

			var temperature *float64
			if raw, ok := args["temperature"].(float64); ok {
				temperature = &raw
			}
			var contextWindow *int
			if raw, ok := args["context_window"].(float64); ok {
				value := int(raw)
				contextWindow = &value
			}
			var inputCostPer1M *float64
			if raw, ok := args["input_cost_per_1m"].(float64); ok {
				inputCostPer1M = &raw
			}
			var outputCostPer1M *float64
			if raw, ok := args["output_cost_per_1m"].(float64); ok {
				outputCostPer1M = &raw
			}
			var reasoningCostPer1M *float64
			if raw, ok := args["reasoning_cost_per_1m"].(float64); ok {
				reasoningCostPer1M = &raw
			}
			var cachedInputCostPer1M *float64
			if raw, ok := args["cached_input_cost_per_1m"].(float64); ok {
				cachedInputCostPer1M = &raw
			}
			var cachedInputCostWritePer1M *float64
			if raw, ok := args["cached_input_cost_write_per_1m"].(float64); ok {
				cachedInputCostWritePer1M = &raw
			}

			llms, err := LoadPublishedLLMs(ctx)
			if err != nil {
				return "", err
			}
			if llms == nil {
				llms = []StoredPublishedLLM{}
			}

			entry := StoredPublishedLLM{
				ID:                        strings.TrimSpace(id),
				Name:                      name,
				Provider:                  provider,
				ModelID:                   modelID,
				ModelName:                 strings.TrimSpace(modelName),
				AuthMethod:                strings.TrimSpace(authMethod),
				ContextWindow:             contextWindow,
				InputCostPer1M:            inputCostPer1M,
				OutputCostPer1M:           outputCostPer1M,
				ReasoningCostPer1M:        reasoningCostPer1M,
				CachedInputCostPer1M:      cachedInputCostPer1M,
				CachedInputCostWritePer1M: cachedInputCostWritePer1M,
				Options:                   options,
				Temperature:               temperature,
				CreatedAt:                 time.Now().UTC().Format(time.RFC3339),
			}

			matchIndex := -1
			for i, existing := range llms {
				if entry.ID != "" && existing.ID == entry.ID {
					matchIndex = i
					entry.CreatedAt = existing.CreatedAt
					break
				}
				if entry.ID == "" && existing.Provider == entry.Provider && existing.ModelID == entry.ModelID && existing.Name == entry.Name {
					matchIndex = i
					entry.ID = existing.ID
					entry.CreatedAt = existing.CreatedAt
					break
				}
			}

			if matchIndex >= 0 {
				llms[matchIndex] = entry
			} else {
				llms = append(llms, entry)
			}

			if err := SavePublishedLLMs(ctx, llms); err != nil {
				return "", err
			}

			return fmt.Sprintf("Saved published LLM.\n%s", prettyJSON(entry)), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"set_provider_auth",
		"Create or update workspace-backed provider authentication in config/provider-api-keys.json. Use this for providers that need API keys, Azure endpoint config, or Bedrock region config.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Provider id: openrouter, openai, anthropic, z-ai, vertex, gemini-cli, minimax, minimax-coding-plan, elevenlabs, deepgram, bedrock, or azure.",
				},
				"api_key": map[string]interface{}{
					"type":        "string",
					"description": "API key for providers that require one. Not used for bedrock.",
				},
				"region": map[string]interface{}{
					"type":        "string",
					"description": "Region for bedrock or azure when needed.",
				},
				"endpoint": map[string]interface{}{
					"type":        "string",
					"description": "Azure endpoint url.",
				},
				"api_version": map[string]interface{}{
					"type":        "string",
					"description": "Optional Azure API version.",
				},
			},
			"required": []string{"provider"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			provider := normalizeManagedProvider(fmt.Sprintf("%v", args["provider"]))
			apiKey, _ := args["api_key"].(string)
			region, _ := args["region"].(string)
			endpoint, _ := args["endpoint"].(string)
			apiVersion, _ := args["api_version"].(string)

			keys, err := LoadProviderKeys(ctx)
			if err != nil {
				return "", err
			}
			if keys == nil {
				keys = &StoredProviderKeys{}
			}

			switch provider {
			case "bedrock":
				if strings.TrimSpace(region) == "" {
					return "region is required for bedrock.", nil
				}
				keys.Bedrock = &StoredBedrockConfig{Region: strings.TrimSpace(region)}
			case "azure":
				if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(apiKey) == "" {
					return "endpoint and api_key are required for azure.", nil
				}
				keys.Azure = &StoredAzureConfig{
					Endpoint:   strings.TrimSpace(endpoint),
					APIKey:     apiKey,
					APIVersion: strings.TrimSpace(apiVersion),
					Region:     strings.TrimSpace(region),
				}
			default:
				if strings.TrimSpace(apiKey) == "" {
					return fmt.Sprintf("api_key is required for %s.", provider), nil
				}
				if !setStoredProviderAPIKey(keys, provider, apiKey) {
					return fmt.Sprintf("Unsupported managed provider %q.", provider), nil
				}
			}

			if err := SaveProviderKeys(ctx, keys); err != nil {
				return "", err
			}

			return fmt.Sprintf("Updated provider auth for %s.", provider), nil
		},
	); err != nil {
		return err
	}

	return nil
}
