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
		string(llm.ProviderMiniMaxCodingPlan),
		string(llm.ProviderCodexCLI),
		string(llm.ProviderZAI),
		string(llm.ProviderKimi),
		string(llm.ProviderClaudeCode),
		string(llm.ProviderGeminiCLI),
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

	return all
}

func (api *StreamingAPI) registerMultiAgentLLMTools(underlyingAgent *mcpagent.Agent) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	registerTool := func(name, description string, params map[string]interface{}, exec func(context.Context, map[string]interface{}) (string, error)) error {
		return underlyingAgent.RegisterCustomTool(name, description, params, exec, "llm_config_tools")
	}

	if err := registerTool(
		"list_llm_capabilities",
		"List supported and currently usable LLM providers/models by capability: chat, search_web, read_image, read_video, and generate_image. Includes config files, auth requirements, and CLI runtime availability.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"capability": map[string]interface{}{
					"type":        "string",
					"description": "Optional filter. Supported values: all, chat, search_web, read_image, read_video, generate_image.",
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
					"description": "Provider id: openrouter, openai, anthropic, z-ai, vertex, gemini-cli, minimax, minimax-coding-plan, bedrock, or azure.",
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
