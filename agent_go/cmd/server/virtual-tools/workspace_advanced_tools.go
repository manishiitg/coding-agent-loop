package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/cmd/server/services"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

// GetWorkspaceAdvancedToolCategory returns the category name for workspace advanced tools
func GetWorkspaceAdvancedToolCategory() string {
	return "workspace_advanced"
}

// CreateWorkspaceAdvancedTools returns the shared advanced workspace tools from the workspace package
func CreateWorkspaceAdvancedTools() []llmtypes.Tool {
	return workspace.GetAdvancedToolDefinitions()
}

// CreateWorkspaceAdvancedToolExecutors creates the execution functions for workspace advanced tools
// Uses the shared executors from pkg/workspace
// Includes FolderGuard to restrict LLM writes
// The read_image executor is wrapped with LLM analysis (config read from context at execution time)
func CreateWorkspaceAdvancedToolExecutors() map[string]func(ctx context.Context, args map[string]any) (string, error) {
	wsURL := getWorkspaceAPIURL()
	env := getMCPExtraEnv()
	client := workspace.NewClient(
		wsURL,
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithExtraEnv(env),
	)
	log.Printf("[GLOBAL_CLIENT_DEBUG] Created global workspace client=%p (no session) MCP_API_URL=%s", client, env["MCP_API_URL"])
	executors := workspace.NewAdvancedExecutor(client)
	attachWorkspaceAdvancedLLMExecutors(executors, wsURL)
	return executors
}

// CreateWorkspaceAdvancedToolExecutorsWithUserID creates workspace advanced tool executors
// with an explicit user ID set on the client
// even if the context doesn't carry the user ID.
// The read_image executor is wrapped with LLM analysis (config read from context at execution time)
func CreateWorkspaceAdvancedToolExecutorsWithUserID(userID string) map[string]func(ctx context.Context, args map[string]any) (string, error) {
	wsURL := getWorkspaceAPIURL()
	client := workspace.NewClient(
		wsURL,
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithUserID(userID),
		workspace.WithExtraEnv(getMCPExtraEnv()),
	)
	executors := workspace.NewAdvancedExecutor(client)
	attachWorkspaceAdvancedLLMExecutors(executors, wsURL)
	return executors
}

// CreateWorkspaceAdvancedToolExecutorsWithSession creates workspace advanced tool executors
// with an explicit user ID and session ID. The session ID is injected as MCP_SESSION_ID
// env var so that code execution mode HTTP tool calls can include it for connection reuse
// (e.g., sharing the same Playwright browser across calls within a session).
// Returns (executors, envMap) — the envMap is the same map reference used by the workspace
// client, so callers can update MCP_API_URL/MCP_SESSION_ID dynamically when the session changes.
func CreateWorkspaceAdvancedToolExecutorsWithSession(userID, sessionID string) (map[string]func(ctx context.Context, args map[string]any) (string, error), map[string]string) {
	return CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv(userID, sessionID, nil)
}

// CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv creates workspace advanced tool executors
// with session support and additional environment variables (e.g., secrets).
// The extraEnvVars are injected into the shell environment alongside MCP_API_URL, MCP_API_TOKEN, etc.
// Returns (executors, envMap) — the envMap is the same map reference stored as Client.ExtraEnv,
// so callers can update MCP_API_URL/MCP_SESSION_ID in-place and the changes propagate to all
// subsequent executor calls (Go maps are reference types).
func CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv(userID, sessionID string, extraEnvVars map[string]string) (map[string]func(ctx context.Context, args map[string]any) (string, error), map[string]string) {
	wsURL := getWorkspaceAPIURL()
	env := getMCPExtraEnv(sessionID)
	// Merge additional env vars (secrets, etc.) — these don't override MCP vars
	for k, v := range extraEnvVars {
		if _, exists := env[k]; !exists {
			env[k] = v
		}
	}
	client := workspace.NewClient(
		wsURL,
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithUserID(userID),
		workspace.WithExtraEnv(env),
	)
	log.Printf("[SESSION_CLIENT_DEBUG] Created session-aware workspace client=%p sessionID=%s MCP_API_URL=%s", client, sessionID, env["MCP_API_URL"])
	executors := workspace.NewAdvancedExecutor(client)
	attachWorkspaceAdvancedLLMExecutors(executors, wsURL)
	return executors, env
}

// CreateWorkspaceAdvancedToolExecutorsWithURL creates workspace advanced tool executors
// pointing to a custom workspace API URL.
func CreateWorkspaceAdvancedToolExecutorsWithURL(wsURL, userID, sessionID string) (map[string]func(ctx context.Context, args map[string]any) (string, error), map[string]string) {
	env := getMCPExtraEnv(sessionID)
	client := workspace.NewClient(
		wsURL,
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithUserID(userID),
		workspace.WithExtraEnv(env),
	)
	executors := workspace.NewAdvancedExecutor(client)
	attachWorkspaceAdvancedLLMExecutors(executors, wsURL)
	return executors, env
}

func attachWorkspaceAdvancedLLMExecutors(executors map[string]func(ctx context.Context, args map[string]any) (string, error), workspaceURL string) {
	wrapReadImageExecutor(executors)
	executors["generate_text_llm"] = createGenerateTextLLMExecutor(workspaceURL)
}

// getMCPExtraEnv returns MCP-related env vars to inject into shell commands.
// These are set by server.go at startup for code execution mode.
// An optional sessionID can be passed to inject MCP_SESSION_ID for connection reuse.
func getMCPExtraEnv(sessionID ...string) map[string]string {
	env := make(map[string]string)
	baseURL := os.Getenv("MCP_API_URL")
	sid := ""
	if len(sessionID) > 0 {
		sid = sessionID[0]
	}
	if baseURL != "" {
		if sid != "" {
			// Embed session_id in the URL path: MCP_API_URL becomes {base}/s/{session_id}
			// The server registers session-scoped routes at /s/{session_id}/tools/...
			// so agent code calling {MCP_API_URL}/tools/mcp/{server}/{tool} automatically
			// includes the session_id without the agent needing to add it to the body.
			env["MCP_API_URL"] = baseURL + "/s/" + sid
		} else {
			env["MCP_API_URL"] = baseURL
		}
	}
	if token := os.Getenv("MCP_API_TOKEN"); token != "" {
		env["MCP_API_TOKEN"] = token
	}
	if sid != "" {
		env["MCP_SESSION_ID"] = sid
	}
	log.Printf("[MCP_ENV_DEBUG] getMCPExtraEnv: baseURL=%s sessionID=%s final_MCP_API_URL=%s", baseURL, sid, env["MCP_API_URL"])
	return env
}

type generateTextLLMResult struct {
	Tier     string `json:"tier"`
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
	Response string `json:"response"`
}

func createGenerateTextLLMExecutor(workspaceURL string) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		userMessage := strings.TrimSpace(fmt.Sprintf("%v", args["user_message"]))
		if userMessage == "" {
			return "", fmt.Errorf("user_message is required")
		}

		tier := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", args["tier"])))
		if tier != "high" && tier != "medium" && tier != "low" {
			return "", fmt.Errorf("tier must be one of: high, medium, low")
		}

		tierModel, err := loadWorkspaceTierModel(ctx, workspaceURL, tier)
		if err != nil {
			return "", err
		}

		llmModel, err := createLLMFromTierModel(ctx, tierModel, loadWorkspaceProviderAPIKeys(ctx, workspaceURL))
		if err != nil {
			return "", fmt.Errorf("failed to initialize LLM for tier %q: %w", tier, err)
		}

		resp, err := llmModel.GenerateContent(ctx, []llmtypes.MessageContent{
			{
				Role: llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{
					llmtypes.TextContent{Text: userMessage},
				},
			},
		})
		if err != nil {
			return "", fmt.Errorf("generate_text_llm failed for tier %q: %w", tier, err)
		}

		responseText := ""
		if len(resp.Choices) > 0 {
			responseText = strings.TrimSpace(resp.Choices[0].Content)
		}
		if responseText == "" {
			responseText = "(No response generated)"
		}

		payload, err := json.Marshal(generateTextLLMResult{
			Tier:     tier,
			Provider: tierModel.Provider,
			ModelID:  tierModel.ModelID,
			Response: responseText,
		})
		if err != nil {
			return "", fmt.Errorf("failed to marshal generate_text_llm result: %w", err)
		}

		return string(payload), nil
	}
}

func loadWorkspaceTierModel(ctx context.Context, workspaceURL, tier string) (*TierModel, error) {
	cfg := loadWorkspaceTierConfig(ctx, workspaceURL)

	var model *TierModel
	switch tier {
	case "high":
		model = cfg.High
	case "medium":
		model = cfg.Medium
	case "low":
		model = cfg.Low
	}

	if model == nil || strings.TrimSpace(model.Provider) == "" || strings.TrimSpace(model.ModelID) == "" {
		return nil, fmt.Errorf("tier %q is not configured in workspace tier config", tier)
	}

	return model, nil
}

func loadWorkspaceTierConfig(ctx context.Context, workspaceURL string) *DelegationTierConfig {
	cfg := &DelegationTierConfig{
		High:   envTierModel("DELEGATION_TIER_HIGH_PROVIDER", "DELEGATION_TIER_HIGH_MODEL"),
		Medium: envTierModel("DELEGATION_TIER_MEDIUM_PROVIDER", "DELEGATION_TIER_MEDIUM_MODEL"),
		Low:    envTierModel("DELEGATION_TIER_LOW_PROVIDER", "DELEGATION_TIER_LOW_MODEL"),
	}

	if workspaceURL == "" {
		return cfg
	}

	rawCfg, exists, err := services.LoadDelegationTierConfig(ctx, workspaceURL)
	if err != nil {
		log.Printf("[GENERATE_TEXT_LLM] Failed to load workspace tier config: %v", err)
		return cfg
	}
	if !exists || len(rawCfg) == 0 {
		return cfg
	}

	data, err := json.Marshal(rawCfg)
	if err != nil {
		log.Printf("[GENERATE_TEXT_LLM] Failed to marshal workspace tier config: %v", err)
		return cfg
	}

	var workspaceCfg DelegationTierConfig
	if err := json.Unmarshal(data, &workspaceCfg); err != nil {
		log.Printf("[GENERATE_TEXT_LLM] Failed to parse workspace tier config: %v", err)
		return cfg
	}

	if sanitized := sanitizeTierModelLocal(workspaceCfg.High); sanitized != nil {
		cfg.High = sanitized
	}
	if sanitized := sanitizeTierModelLocal(workspaceCfg.Medium); sanitized != nil {
		cfg.Medium = sanitized
	}
	if sanitized := sanitizeTierModelLocal(workspaceCfg.Low); sanitized != nil {
		cfg.Low = sanitized
	}

	return cfg
}

func envTierModel(providerEnv, modelEnv string) *TierModel {
	provider := strings.TrimSpace(os.Getenv(providerEnv))
	modelID := strings.TrimSpace(os.Getenv(modelEnv))
	if provider == "" || modelID == "" {
		return nil
	}
	return &TierModel{
		Provider: provider,
		ModelID:  modelID,
	}
}

func sanitizeTierModelLocal(model *TierModel) *TierModel {
	if model == nil {
		return nil
	}

	provider := strings.TrimSpace(model.Provider)
	modelID := strings.TrimSpace(model.ModelID)
	if provider == "" || modelID == "" {
		return nil
	}

	sanitized := &TierModel{
		Provider:  provider,
		ModelID:   modelID,
		Fallbacks: nil,
	}

	for _, fb := range model.Fallbacks {
		fallbackModelID := strings.TrimSpace(fb.ModelID)
		if fallbackModelID == "" {
			continue
		}
		sanitized.Fallbacks = append(sanitized.Fallbacks, TierModelFallback{
			Provider: strings.TrimSpace(fb.Provider),
			ModelID:  fallbackModelID,
		})
	}

	if len(sanitized.Fallbacks) == 0 {
		sanitized.Fallbacks = nil
	}

	return sanitized
}

func loadWorkspaceProviderAPIKeys(ctx context.Context, workspaceURL string) *llm.ProviderAPIKeys {
	if workspaceURL == "" {
		return nil
	}

	rawKeys, exists, err := services.LoadProviderKeys(ctx, workspaceURL)
	if err != nil {
		log.Printf("[GENERATE_TEXT_LLM] Failed to load provider keys from workspace: %v", err)
		return nil
	}
	if !exists || len(rawKeys) == 0 {
		return nil
	}

	keys := &llm.ProviderAPIKeys{}
	if value, ok := rawKeys["openrouter"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.OpenRouter = &v
	}
	if value, ok := rawKeys["openai"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.OpenAI = &v
	}
	if value, ok := rawKeys["anthropic"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.Anthropic = &v
	}
	if value, ok := rawKeys["vertex"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.Vertex = &v
	}
	if value, ok := rawKeys["gemini_cli"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.GeminiCLI = &v
	}
	if value, ok := rawKeys["minimax"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.MiniMax = &v
	}
	if value, ok := rawKeys["minimax-coding-plan"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.MiniMaxCodingPlan = &v
	}
	if value, ok := rawKeys["bedrock"].(map[string]interface{}); ok {
		if region, ok := value["region"].(string); ok && strings.TrimSpace(region) != "" {
			keys.Bedrock = &llm.BedrockConfig{Region: region}
		}
	}
	if value, ok := rawKeys["azure"].(map[string]interface{}); ok {
		cfg := &llm.AzureAPIConfig{}
		if endpoint, ok := value["endpoint"].(string); ok {
			cfg.Endpoint = endpoint
		}
		if apiKey, ok := value["api_key"].(string); ok {
			cfg.APIKey = apiKey
		}
		if apiVersion, ok := value["api_version"].(string); ok {
			cfg.APIVersion = apiVersion
		}
		if region, ok := value["region"].(string); ok {
			cfg.Region = region
		}
		if cfg.Endpoint != "" || cfg.APIKey != "" || cfg.APIVersion != "" || cfg.Region != "" {
			keys.Azure = cfg
		}
	}

	return keys
}

func createLLMFromTierModel(ctx context.Context, model *TierModel, apiKeys *llm.ProviderAPIKeys) (llmtypes.Model, error) {
	llmCfg := llm.Config{
		Provider:       llm.Provider(model.Provider),
		ModelID:        model.ModelID,
		Context:        ctx,
		APIKeys:        apiKeys,
		FallbackModels: formatTierFallbackModels(model),
		MaxRetries:     3,
	}

	return llm.InitializeLLM(llmCfg)
}

func formatTierFallbackModels(model *TierModel) []string {
	if model == nil || len(model.Fallbacks) == 0 {
		return nil
	}

	fallbacks := make([]string, 0, len(model.Fallbacks))
	defaultProvider := strings.TrimSpace(model.Provider)
	for _, fb := range model.Fallbacks {
		modelID := strings.TrimSpace(fb.ModelID)
		if modelID == "" {
			continue
		}
		provider := strings.TrimSpace(fb.Provider)
		if provider == "" || provider == defaultProvider {
			fallbacks = append(fallbacks, modelID)
			continue
		}
		fallbacks = append(fallbacks, provider+"/"+modelID)
	}

	if len(fallbacks) == 0 {
		return nil
	}
	return fallbacks
}

// wrapReadImageExecutor wraps the read_image executor in the map with LLM analysis.
// The LLM config is read from context at execution time (injected by conversation.go).
func wrapReadImageExecutor(executors map[string]func(ctx context.Context, args map[string]any) (string, error)) {
	if baseExecutor, exists := executors["read_image"]; exists {
		executors["read_image"] = wrapReadImageWithLLM(baseExecutor)
		log.Printf("[READ_IMAGE_DEBUG] read_image executor wrapped with context-based LLM analysis")
	}
}

// SetReadImageFallbackLLMConfig re-wraps the read_image executor so that when the
// context doesn't carry ToolExecutionLLMConfigKey (e.g. HTTP calls from claude CLI),
// the provided fallbackConfig is injected before the inner executor runs.
// Call this after both CreateWorkspaceAdvancedToolExecutors* AND the agent have been
// created, so the real LLM config is known.
func SetReadImageFallbackLLMConfig(
	executors map[string]func(ctx context.Context, args map[string]any) (string, error),
	fallback mcpagent.LLMModel,
) {
	if existing, ok := executors["read_image"]; ok {
		executors["read_image"] = injectLLMConfigFallback(existing, fallback)
		log.Printf("[READ_IMAGE_DEBUG] read_image executor wrapped with LLM fallback (provider=%s, model=%s)",
			fallback.Provider, fallback.ModelID)
	}
}

// injectLLMConfigFallback wraps an executor: if the context has no ToolExecutionLLMConfigKey,
// the fallback config is injected before calling the inner executor.
func injectLLMConfigFallback(
	inner func(ctx context.Context, args map[string]any) (string, error),
	fallback mcpagent.LLMModel,
) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if ctx.Value(mcpagent.ToolExecutionLLMConfigKey) == nil {
			log.Printf("[READ_IMAGE_DEBUG] No LLM config in context, injecting fallback (provider=%s, model=%s)",
				fallback.Provider, fallback.ModelID)
			ctx = context.WithValue(ctx, mcpagent.ToolExecutionLLMConfigKey, fallback)
		}
		return inner(ctx, args)
	}
}

// wrapReadImageWithLLM wraps the base read_image executor (which returns base64 data)
// with a dedicated LLM call that analyzes the image and returns a text response.
// The LLM config (provider, model, API key) is read from context at execution time.
func wrapReadImageWithLLM(
	baseExecutor func(ctx context.Context, args map[string]any) (string, error),
) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		log.Printf("[READ_IMAGE_DEBUG] Wrapped read_image executor called")

		// Step 1: Extract LLM config from context (injected by conversation.go / parallel_tool_execution.go)
		llmConfigRaw := ctx.Value(mcpagent.ToolExecutionLLMConfigKey)
		if llmConfigRaw == nil {
			log.Printf("[READ_IMAGE_DEBUG] No LLM config in context — cannot perform image analysis")
			return "", fmt.Errorf("LLM configuration not available in context for image analysis")
		}
		llmConfig, ok := llmConfigRaw.(mcpagent.LLMModel)
		if !ok {
			log.Printf("[READ_IMAGE_DEBUG] LLM config in context has unexpected type: %T", llmConfigRaw)
			return "", fmt.Errorf("LLM configuration in context has unexpected type")
		}
		log.Printf("[READ_IMAGE_DEBUG] LLM config from context: provider=%s, model=%s", llmConfig.Provider, llmConfig.ModelID)

		// Step 2: Call the base executor to get base64 image data from workspace
		rawResult, err := baseExecutor(ctx, args)
		if err != nil {
			log.Printf("[READ_IMAGE_DEBUG] Base executor failed: %v", err)
			return "", err
		}

		// Step 3: Parse the structured result from workspace
		var imageData workspace.ReadImageResult
		if err := json.Unmarshal([]byte(rawResult), &imageData); err != nil {
			log.Printf("[READ_IMAGE_DEBUG] Failed to parse base result as ReadImageResult: %v", err)
			return "", fmt.Errorf("failed to parse image data: %w", err)
		}

		log.Printf("[READ_IMAGE_DEBUG] Image data received: filepath=%s, mimeType=%s, base64Length=%d",
			imageData.Filepath, imageData.MimeType, len(imageData.Data))

		// Step 4: Create a dedicated LLM client using multi-llm-provider-go
		llmModel, err := createLLMFromConfig(ctx, llmConfig)
		if err != nil {
			log.Printf("[READ_IMAGE_DEBUG] Failed to create LLM client: %v", err)
			return "", fmt.Errorf("failed to initialize LLM for image analysis: %w", err)
		}

		log.Printf("[READ_IMAGE_DEBUG] LLM client created (provider=%s, model=%s), making GenerateContent call",
			llmConfig.Provider, llmConfig.ModelID)

		// Step 5: Make the LLM call with the image + query
		messages := []llmtypes.MessageContent{
			{
				Role: llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{
					llmtypes.TextContent{Text: imageData.Query},
					llmtypes.ImageContent{
						SourceType: "base64",
						MediaType:  imageData.MimeType,
						Data:       imageData.Data,
					},
				},
			},
		}

		resp, err := llmModel.GenerateContent(ctx, messages)
		if err != nil {
			log.Printf("[READ_IMAGE_DEBUG] LLM GenerateContent failed: %v", err)
			return "", fmt.Errorf("LLM image analysis failed: %w", err)
		}

		// Step 6: Extract and return the text response
		var responseText string
		if len(resp.Choices) > 0 {
			responseText = resp.Choices[0].Content
		}
		if responseText == "" {
			responseText = "(No response from image analysis)"
		}

		log.Printf("[READ_IMAGE_DEBUG] LLM response received: %d chars", len(responseText))

		// Cap response size
		const maxResponseSize = 100 * 1024
		if len(responseText) > maxResponseSize {
			responseText = responseText[:maxResponseSize] + "\n... (response truncated)"
			log.Printf("[READ_IMAGE_DEBUG] Response truncated to %d chars", maxResponseSize)
		}

		// Return final JSON result (same pattern as read_pdf)
		response := map[string]any{
			"filepath": imageData.Filepath,
			"query":    imageData.Query,
			"response": responseText,
		}

		responseJSON, err := json.Marshal(response)
		if err != nil {
			return "", fmt.Errorf("failed to marshal response: %w", err)
		}

		log.Printf("[READ_IMAGE_DEBUG] read_image complete, returning LLM analysis result")
		return string(responseJSON), nil
	}
}

// createLLMFromConfig creates an LLM model instance using multi-llm-provider-go
// from the agent's LLMModel config (extracted from context).
func createLLMFromConfig(ctx context.Context, config mcpagent.LLMModel) (llmtypes.Model, error) {
	var apiKeys *llm.ProviderAPIKeys
	if config.APIKey != nil {
		apiKeys = &llm.ProviderAPIKeys{}
		switch llm.Provider(config.Provider) {
		case llm.ProviderAnthropic:
			apiKeys.Anthropic = config.APIKey
		case llm.ProviderOpenAI:
			apiKeys.OpenAI = config.APIKey
		case llm.ProviderOpenRouter:
			apiKeys.OpenRouter = config.APIKey
		case llm.ProviderVertex:
			apiKeys.Vertex = config.APIKey
		}
	}

	llmCfg := llm.Config{
		Provider: llm.Provider(config.Provider),
		ModelID:  config.ModelID,
		Context:  ctx,
		APIKeys:  apiKeys,
	}

	return llm.InitializeLLM(llmCfg)
}
