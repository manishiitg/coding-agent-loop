package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/utils"
)

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

func (api *StreamingAPI) registerMultiAgentLLMTools(underlyingAgent *mcpagent.Agent) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	registerTool := func(name, description string, params map[string]interface{}, exec func(context.Context, map[string]interface{}) (string, error)) error {
		return underlyingAgent.RegisterCustomTool(name, description, params, exec, "llm_config_tools")
	}

	if err := registerTool(
		"list_provider_models",
		"List the available models for a provider using the same shared metadata catalog the frontend uses from /api/llm-config/models/metadata.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"provider": map[string]interface{}{
					"type":        "string",
					"description": "Provider id such as openai, openrouter, anthropic, vertex, azure, minimax, minimax-coding-plan, or bedrock.",
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
					"description": "Provider id such as openai, openrouter, anthropic, vertex, azure, minimax, minimax-coding-plan, gemini-cli, claude-code, codex-cli, or bedrock.",
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
				switch provider {
				case "openrouter":
					if keys.OpenRouter != "" {
						apiKey = keys.OpenRouter
						usedWorkspaceAuth = true
					}
				case "openai":
					if keys.OpenAI != "" {
						apiKey = keys.OpenAI
						usedWorkspaceAuth = true
					}
				case "anthropic":
					if keys.Anthropic != "" {
						apiKey = keys.Anthropic
						usedWorkspaceAuth = true
					}
				case "vertex":
					if keys.Vertex != "" {
						apiKey = keys.Vertex
						usedWorkspaceAuth = true
					}
				case "gemini-cli":
					if keys.GeminiCLI != "" {
						apiKey = keys.GeminiCLI
						usedWorkspaceAuth = true
					}
				case "minimax":
					if keys.MiniMax != "" {
						apiKey = keys.MiniMax
						usedWorkspaceAuth = true
					}
				case "minimax-coding-plan":
					if keys.MiniMaxCodingPlan != "" {
						apiKey = keys.MiniMaxCodingPlan
						usedWorkspaceAuth = true
					}
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
					"description": "Provider id such as openai, openrouter, anthropic, vertex, azure, minimax, gemini-cli, claude-code, or codex-cli.",
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
					"description": "Provider id: openrouter, openai, anthropic, vertex, gemini-cli, minimax, minimax-coding-plan, bedrock, or azure.",
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
			case "openrouter":
				if strings.TrimSpace(apiKey) == "" {
					return "api_key is required for openrouter.", nil
				}
				keys.OpenRouter = apiKey
			case "openai":
				if strings.TrimSpace(apiKey) == "" {
					return "api_key is required for openai.", nil
				}
				keys.OpenAI = apiKey
			case "anthropic":
				if strings.TrimSpace(apiKey) == "" {
					return "api_key is required for anthropic.", nil
				}
				keys.Anthropic = apiKey
			case "vertex":
				if strings.TrimSpace(apiKey) == "" {
					return "api_key is required for vertex.", nil
				}
				keys.Vertex = apiKey
			case "gemini-cli":
				if strings.TrimSpace(apiKey) == "" {
					return "api_key is required for gemini-cli.", nil
				}
				keys.GeminiCLI = apiKey
			case "minimax":
				if strings.TrimSpace(apiKey) == "" {
					return "api_key is required for minimax.", nil
				}
				keys.MiniMax = apiKey
			case "minimax-coding-plan":
				if strings.TrimSpace(apiKey) == "" {
					return "api_key is required for minimax-coding-plan.", nil
				}
				keys.MiniMaxCodingPlan = apiKey
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
				return fmt.Sprintf("Unsupported managed provider %q.", provider), nil
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
