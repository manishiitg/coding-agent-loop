package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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
	env := getMCPExtraEnv()
	client := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithExtraEnv(env),
	)
	log.Printf("[GLOBAL_CLIENT_DEBUG] Created global workspace client=%p (no session) MCP_API_URL=%s", client, env["MCP_API_URL"])
	executors := workspace.NewAdvancedExecutor(client)
	wrapReadImageExecutor(executors)
	return executors
}

// CreateWorkspaceAdvancedToolExecutorsWithUserID creates workspace advanced tool executors
// with an explicit user ID set on the client
// even if the context doesn't carry the user ID.
// The read_image executor is wrapped with LLM analysis (config read from context at execution time)
func CreateWorkspaceAdvancedToolExecutorsWithUserID(userID string) map[string]func(ctx context.Context, args map[string]any) (string, error) {
	client := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithUserID(userID),
		workspace.WithExtraEnv(getMCPExtraEnv()),
	)
	executors := workspace.NewAdvancedExecutor(client)
	wrapReadImageExecutor(executors)
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
	env := getMCPExtraEnv(sessionID)
	// Merge additional env vars (secrets, etc.) — these don't override MCP vars
	for k, v := range extraEnvVars {
		if _, exists := env[k]; !exists {
			env[k] = v
		}
	}
	client := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithUserID(userID),
		workspace.WithExtraEnv(env),
	)
	log.Printf("[SESSION_CLIENT_DEBUG] Created session-aware workspace client=%p sessionID=%s MCP_API_URL=%s", client, sessionID, env["MCP_API_URL"])
	executors := workspace.NewAdvancedExecutor(client)
	wrapReadImageExecutor(executors)
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
	wrapReadImageExecutor(executors)
	return executors, env
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
