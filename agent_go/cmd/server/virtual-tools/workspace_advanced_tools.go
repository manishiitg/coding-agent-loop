package virtualtools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
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
	wrapReadImageExecutor(executors, workspaceURL)
	wrapReadVideoExecutor(executors, workspaceURL)
	executors["generate_text_llm"] = createGenerateTextLLMExecutor(workspaceURL)
	executors["search_web_llm"] = createSearchWebLLMExecutor(workspaceURL)
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

// GenerateTextOneShot is an exported helper that mirrors generate_text_llm
// but is callable directly from Go code (not via the LLM tool surface). Used
// by the auto-improvement framework's evaluator-agent path to narrate
// experiment verdicts without having to spin up a full agent.
//
// Pass tier "low", "medium", or "high"; system + user are the two messages.
// Returns the model's text response, trimmed.
func GenerateTextOneShot(ctx context.Context, tier, systemMessage, userMessage string) (string, error) {
	if strings.TrimSpace(userMessage) == "" {
		return "", fmt.Errorf("user_message is required")
	}
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier != "high" && tier != "medium" && tier != "low" {
		return "", fmt.Errorf("tier must be one of: high, medium, low")
	}

	workspaceURL := getWorkspaceAPIURL()

	tierModel, err := loadWorkspaceTierModel(ctx, workspaceURL, tier)
	if err != nil {
		return "", err
	}

	llmModel, err := createLLMFromTierModel(ctx, tierModel, loadWorkspaceProviderAPIKeys(ctx, workspaceURL))
	if err != nil {
		return "", fmt.Errorf("failed to initialize LLM for tier %q: %w", tier, err)
	}

	messages := make([]llmtypes.MessageContent, 0, 2)
	if strings.TrimSpace(systemMessage) != "" {
		messages = append(messages, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeSystem,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: systemMessage}},
		})
	}
	messages = append(messages, llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage}},
	})

	resp, err := llmModel.GenerateContent(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("GenerateTextOneShot failed for tier %q: %w", tier, err)
	}

	if len(resp.Choices) > 0 {
		return strings.TrimSpace(resp.Choices[0].Content), nil
	}
	return "", nil
}

func createSearchWebLLMExecutor(workspaceURL string) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		query := strings.TrimSpace(fmt.Sprintf("%v", args["query"]))
		if query == "" {
			return "", fmt.Errorf("query is required")
		}

		provider := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", args["provider"])))
		if provider == "" || provider == "<nil>" {
			return "", fmt.Errorf("provider is required")
		}

		modelID := strings.TrimSpace(fmt.Sprintf("%v", args["model_id"]))
		if modelID == "<nil>" {
			modelID = ""
		}

		llmModel, err := createPublishedSearchLLM(ctx, workspaceURL, provider, modelID)
		if err != nil {
			return "", err
		}

		result, err := llm.SearchWeb(ctx, llmModel, query)
		if err != nil {
			return "", fmt.Errorf("search_web_llm failed: %w", err)
		}
		return result, nil
	}
}

func isSearchCapableProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case string(llm.ProviderClaudeCode), string(llm.ProviderCodexCLI), string(llm.ProviderGeminiCLI), string(llm.ProviderMiniMaxCodingPlan), string(llm.ProviderVertex):
		return true
	default:
		return false
	}
}

func isSearchCapablePublishedLLM(entry services.PublishedLLM) bool {
	provider := strings.ToLower(strings.TrimSpace(entry.Provider))
	if !isSearchCapableProvider(provider) {
		return false
	}
	if provider != string(llm.ProviderVertex) {
		return true
	}

	modelID := strings.ToLower(strings.TrimSpace(entry.ModelID))
	return strings.HasPrefix(modelID, "gemini")
}

func hasSearchProviderAuth(provider string, apiKeys *llm.ProviderAPIKeys) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case string(llm.ProviderClaudeCode):
		return apiKeys != nil && apiKeys.Anthropic != nil && strings.TrimSpace(*apiKeys.Anthropic) != ""
	case string(llm.ProviderCodexCLI):
		return true
	case string(llm.ProviderGeminiCLI):
		return apiKeys != nil && apiKeys.GeminiCLI != nil && strings.TrimSpace(*apiKeys.GeminiCLI) != ""
	case string(llm.ProviderMiniMaxCodingPlan):
		return apiKeys != nil && apiKeys.MiniMaxCodingPlan != nil && strings.TrimSpace(*apiKeys.MiniMaxCodingPlan) != ""
	case string(llm.ProviderVertex):
		return apiKeys != nil && apiKeys.Vertex != nil && strings.TrimSpace(*apiKeys.Vertex) != ""
	default:
		return false
	}
}

func isSearchProviderAvailable(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case string(llm.ProviderGeminiCLI):
		_, err := exec.LookPath("gemini")
		return err == nil
	case string(llm.ProviderClaudeCode):
		_, err := exec.LookPath("claude")
		return err == nil
	case string(llm.ProviderCodexCLI):
		_, err := exec.LookPath("codex")
		return err == nil
	case string(llm.ProviderMiniMaxCodingPlan):
		_, err := exec.LookPath("mmx")
		return err == nil
	case string(llm.ProviderVertex):
		return true
	default:
		return false
	}
}

func publishedSearchProviderSummary(entries []services.PublishedLLM) string {
	var available []string
	seen := map[string]bool{}
	for _, entry := range entries {
		if !isSearchCapablePublishedLLM(entry) {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(entry.Provider))
		modelID := strings.TrimSpace(entry.ModelID)
		key := provider + "|" + modelID
		if provider == "" || modelID == "" || seen[key] {
			continue
		}
		seen[key] = true
		available = append(available, fmt.Sprintf("%s (%s)", provider, modelID))
	}
	if len(available) == 0 {
		return "No published search-capable providers are configured."
	}
	sort.Strings(available)
	return "Published search-capable providers: " + strings.Join(available, ", ")
}

func searchRoleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "primary":
		return 0
	case "", "default":
		return 1
	case "fallback":
		return 2
	default:
		return 3
	}
}

func searchPriorityValue(priority *int) int {
	if priority == nil {
		return 1000
	}
	return *priority
}

func preferredSearchModelRank(provider, modelID string) int {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	switch provider {
	case string(llm.ProviderCodexCLI):
		switch modelID {
		case "gpt-5.4-mini":
			return 0
		case "gpt-5.4":
			return 1
		case "gpt-5.3-codex-spark":
			return 2
		case "gpt-5.3-codex":
			return 3
		}
	case string(llm.ProviderGeminiCLI):
		if modelID == "auto" {
			return 0
		}
	case string(llm.ProviderMiniMaxCodingPlan):
		if modelID == "claude-sonnet-4-5" || modelID == "minimax" {
			return 0
		}
	}
	return 100
}

func searchModelAlias(provider, modelID string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	normalizedModelID := strings.ToLower(strings.TrimSpace(modelID))
	if normalizedModelID == "" || normalizedModelID == provider {
		switch provider {
		case string(llm.ProviderCodexCLI):
			return "gpt-5.4-mini"
		case string(llm.ProviderGeminiCLI):
			return "auto"
		case string(llm.ProviderMiniMaxCodingPlan):
			return "claude-sonnet-4-5"
		}
	}
	return modelID
}

func sortPublishedSearchCandidates(candidates []services.PublishedLLM) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		leftRole := searchRoleRank(left.SearchRole)
		rightRole := searchRoleRank(right.SearchRole)
		if leftRole != rightRole {
			return leftRole < rightRole
		}
		leftPriority := searchPriorityValue(left.SearchPriority)
		rightPriority := searchPriorityValue(right.SearchPriority)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftModelRank := preferredSearchModelRank(left.Provider, left.ModelID)
		rightModelRank := preferredSearchModelRank(right.Provider, right.ModelID)
		if leftModelRank != rightModelRank {
			return leftModelRank < rightModelRank
		}
		leftProvider := strings.ToLower(strings.TrimSpace(left.Provider))
		rightProvider := strings.ToLower(strings.TrimSpace(right.Provider))
		if leftProvider != rightProvider {
			return leftProvider < rightProvider
		}
		return strings.ToLower(strings.TrimSpace(left.ModelID)) < strings.ToLower(strings.TrimSpace(right.ModelID))
	})
}

func loadPublishedSearchProvider(ctx context.Context, workspaceURL string, apiKeys *llm.ProviderAPIKeys, requestedProvider, requestedModelID string) (*services.PublishedLLM, error) {
	publishedLLMs, exists, err := services.LoadPublishedLLMs(ctx, workspaceURL)
	if err != nil {
		return nil, fmt.Errorf("failed to load published LLMs: %w", err)
	}
	if !exists || len(publishedLLMs) == 0 {
		return nil, fmt.Errorf("search_web_llm requires a published search-capable provider in config/published-llms.json")
	}

	requestedProvider = strings.ToLower(strings.TrimSpace(requestedProvider))
	if requestedProvider == "" {
		return nil, fmt.Errorf("provider is required. %s", publishedSearchProviderSummary(publishedLLMs))
	}
	requestedModelID = strings.TrimSpace(searchModelAlias(requestedProvider, requestedModelID))

	var candidates []services.PublishedLLM
	for _, entry := range publishedLLMs {
		provider := strings.ToLower(strings.TrimSpace(entry.Provider))
		modelID := strings.TrimSpace(entry.ModelID)
		if requestedProvider != "" && provider != requestedProvider {
			continue
		}
		if requestedModelID != "" && !strings.EqualFold(modelID, requestedModelID) {
			continue
		}
		if !isSearchCapablePublishedLLM(entry) {
			continue
		}
		if !hasSearchProviderAuth(provider, apiKeys) {
			continue
		}
		if !isSearchProviderAvailable(provider) {
			continue
		}
		candidates = append(candidates, entry)
	}
	if len(candidates) > 0 {
		sortPublishedSearchCandidates(candidates)
		candidate := candidates[0]
		return &candidate, nil
	}

	foundProvider := false
	foundModel := false
	for _, entry := range publishedLLMs {
		provider := strings.ToLower(strings.TrimSpace(entry.Provider))
		modelID := strings.TrimSpace(entry.ModelID)
		if provider != requestedProvider {
			continue
		}
		foundProvider = true
		if requestedModelID != "" && !strings.EqualFold(modelID, requestedModelID) {
			continue
		}
		foundModel = true
		if !isSearchCapablePublishedLLM(entry) {
			continue
		}
		if !hasSearchProviderAuth(provider, apiKeys) {
			return nil, fmt.Errorf("search_web_llm requires auth for requested provider %q in config/provider-api-keys.json. %s", entry.Provider, publishedSearchProviderSummary(publishedLLMs))
		}
		if !isSearchProviderAvailable(provider) {
			return nil, fmt.Errorf("search_web_llm cannot use requested provider %q because its runtime dependency is unavailable. %s", entry.Provider, publishedSearchProviderSummary(publishedLLMs))
		}
		candidate := entry
		return &candidate, nil
	}
	if !foundProvider {
		return nil, fmt.Errorf("search_web_llm could not find requested provider %q in config/published-llms.json. %s", requestedProvider, publishedSearchProviderSummary(publishedLLMs))
	}
	if !foundModel {
		if requestedModelID == "" {
			return nil, fmt.Errorf("search_web_llm could not find a usable search-capable model under provider %q in config/published-llms.json. %s", requestedProvider, publishedSearchProviderSummary(publishedLLMs))
		}
		return nil, fmt.Errorf("search_web_llm could not find model %q under provider %q in config/published-llms.json. %s", requestedModelID, requestedProvider, publishedSearchProviderSummary(publishedLLMs))
	}
	if requestedModelID == "" {
		return nil, fmt.Errorf("search_web_llm does not support published provider %q for search yet. %s", requestedProvider, publishedSearchProviderSummary(publishedLLMs))
	}
	return nil, fmt.Errorf("search_web_llm does not support published provider %q with model %q for search yet. %s", requestedProvider, requestedModelID, publishedSearchProviderSummary(publishedLLMs))
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
	if value, ok := rawKeys["z-ai"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.ZAI = &v
	}
	if value, ok := rawKeys["kimi"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.Kimi = &v
	}
	if value, ok := rawKeys["vertex"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.Vertex = &v
	}
	if value, ok := rawKeys["gemini_cli"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.GeminiCLI = &v
	}
	if value, ok := rawKeys["codex_cli"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.CodexCLI = &v
	}
	if value, ok := rawKeys["minimax"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.MiniMax = &v
	}
	if value, ok := rawKeys["minimax-coding-plan"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.MiniMaxCodingPlan = &v
	}
	if value, ok := rawKeys["elevenlabs"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.ElevenLabs = &v
	}
	if value, ok := rawKeys["deepgram"].(string); ok && strings.TrimSpace(value) != "" {
		v := value
		keys.Deepgram = &v
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

func createPublishedSearchLLM(ctx context.Context, workspaceURL string, requestedProvider, requestedModelID string) (llmtypes.Model, error) {
	apiKeys := loadWorkspaceProviderAPIKeys(ctx, workspaceURL)
	publishedLLM, err := loadPublishedSearchProvider(ctx, workspaceURL, apiKeys, requestedProvider, requestedModelID)
	if err != nil {
		return nil, err
	}

	provider := llm.Provider(strings.ToLower(strings.TrimSpace(publishedLLM.Provider)))
	switch provider {
	case llm.ProviderGeminiCLI:
		if apiKeys == nil || apiKeys.GeminiCLI == nil || strings.TrimSpace(*apiKeys.GeminiCLI) == "" {
			return nil, fmt.Errorf("search_web_llm requires Gemini CLI auth in config/provider-api-keys.json for the published provider")
		}
	case llm.ProviderCodexCLI:
		// Codex CLI can use workspace auth, CODEX_API_KEY, or its own stored login state.
	case llm.ProviderClaudeCode:
		if apiKeys == nil || apiKeys.Anthropic == nil || strings.TrimSpace(*apiKeys.Anthropic) == "" {
			return nil, fmt.Errorf("search_web_llm requires Anthropic auth in config/provider-api-keys.json for the published Claude Code provider")
		}
	case llm.ProviderMiniMaxCodingPlan:
		if apiKeys == nil || apiKeys.MiniMaxCodingPlan == nil || strings.TrimSpace(*apiKeys.MiniMaxCodingPlan) == "" {
			return nil, fmt.Errorf("search_web_llm requires MiniMax auth in config/provider-api-keys.json for the published MiniMax coding plan provider")
		}
	case llm.ProviderVertex:
		if apiKeys == nil || apiKeys.Vertex == nil || strings.TrimSpace(*apiKeys.Vertex) == "" {
			return nil, fmt.Errorf("search_web_llm requires Vertex auth in config/provider-api-keys.json for the published Vertex provider")
		}
	default:
		return nil, fmt.Errorf("search_web_llm does not support published provider %q yet", publishedLLM.Provider)
	}

	llmCfg := llm.Config{
		Provider:   provider,
		ModelID:    resolveRuntimeModelIDForVirtualTool(provider, publishedLLM.ModelID),
		Context:    ctx,
		APIKeys:    apiKeys,
		MaxRetries: 3,
	}
	return llm.InitializeLLM(llmCfg)
}

func createLLMFromTierModel(ctx context.Context, model *TierModel, apiKeys *llm.ProviderAPIKeys) (llmtypes.Model, error) {
	provider := llm.Provider(model.Provider)
	llmCfg := llm.Config{
		Provider:       provider,
		ModelID:        resolveRuntimeModelIDForVirtualTool(provider, model.ModelID),
		Context:        ctx,
		APIKeys:        apiKeys,
		FallbackModels: formatTierFallbackModels(model),
		MaxRetries:     3,
	}

	return llm.InitializeLLM(llmCfg)
}

func resolveRuntimeModelIDForVirtualTool(provider llm.Provider, modelID string) string {
	normalizedProvider := strings.ToLower(strings.TrimSpace(string(provider)))
	normalizedModelID := strings.ToLower(strings.TrimSpace(modelID))
	if normalizedProvider == string(llm.ProviderMiniMaxCodingPlan) && normalizedModelID == "minimax" {
		return "claude-sonnet-4-5"
	}
	return modelID
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
func wrapReadImageExecutor(executors map[string]func(ctx context.Context, args map[string]any) (string, error), workspaceURL string) {
	if baseExecutor, exists := executors["read_image"]; exists {
		executors["read_image"] = wrapReadImageWithLLM(baseExecutor, workspaceURL)
		log.Printf("[READ_IMAGE_DEBUG] read_image executor wrapped with workspace-configurable LLM analysis")
	}
}

func wrapReadVideoExecutor(executors map[string]func(ctx context.Context, args map[string]any) (string, error), workspaceURL string) {
	if baseExecutor, exists := executors["read_video"]; exists {
		executors["read_video"] = wrapReadVideoWithProvider(baseExecutor, workspaceURL)
		log.Printf("[READ_VIDEO] read_video executor wrapped with provider-backed video analysis")
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

func wrapReadVideoWithProvider(
	baseExecutor func(ctx context.Context, args map[string]any) (string, error),
	workspaceURL string,
) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		rawResult, err := baseExecutor(ctx, args)
		if err != nil {
			return "", err
		}

		var videoData workspace.ReadVideoResult
		if err := json.Unmarshal([]byte(rawResult), &videoData); err != nil {
			return "", fmt.Errorf("failed to parse video data: %w", err)
		}

		provider := normalizeReadVideoProvider(videoData.Provider)
		if provider == "" {
			provider = normalizeReadVideoProvider(stringFromMap(args, "provider"))
		}
		if provider == "" {
			provider = "kimi"
		}

		switch provider {
		case "kimi":
			return wrapReadVideoWithKimi(ctx, workspaceURL, videoData)
		case "z-ai":
			return wrapReadVideoWithZAI(ctx, workspaceURL, videoData)
		default:
			return "", fmt.Errorf("unsupported read_video provider %q (supported: kimi, z-ai)", provider)
		}
	}
}

func wrapReadVideoWithKimi(ctx context.Context, workspaceURL string, videoData workspace.ReadVideoResult) (string, error) {
	apiKeys := loadWorkspaceProviderAPIKeys(ctx, workspaceURL)
	apiKey := ""
	if apiKeys != nil && apiKeys.Kimi != nil {
		apiKey = strings.TrimSpace(*apiKeys.Kimi)
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("KIMI_API_KEY"))
	}
	if apiKey == "" {
		return "", fmt.Errorf("read_video requires Kimi provider auth in config/provider-api-keys.json or KIMI_API_KEY")
	}

	videoBytes, err := base64.StdEncoding.DecodeString(videoData.Data)
	if err != nil {
		return "", fmt.Errorf("failed to decode video data: %w", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("KIMI_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = "https://api.moonshot.ai/v1"
	}

	fileID, err := uploadKimiVideo(ctx, baseURL, apiKey, videoData.Filepath, videoData.MimeType, videoBytes)
	if err != nil {
		return "", fmt.Errorf("failed to upload video to Kimi/Moonshot: %w", err)
	}

	responseText, err := analyzeKimiVideo(ctx, baseURL, apiKey, fileID, videoData.Query)
	if err != nil {
		return "", fmt.Errorf("Kimi video analysis failed: %w", err)
	}
	if responseText == "" {
		responseText = "(No response from video analysis)"
	}

	const maxResponseSize = 100 * 1024
	if len(responseText) > maxResponseSize {
		responseText = responseText[:maxResponseSize] + "\n... (response truncated)"
	}

	response := map[string]any{
		"filepath": videoData.Filepath,
		"query":    videoData.Query,
		"provider": "kimi",
		"model":    "kimi-k2.6",
		"response": responseText,
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}
	return string(responseJSON), nil
}

func wrapReadVideoWithZAI(ctx context.Context, workspaceURL string, videoData workspace.ReadVideoResult) (string, error) {
	apiKeys := loadWorkspaceProviderAPIKeys(ctx, workspaceURL)
	apiKey := ""
	if apiKeys != nil && apiKeys.ZAI != nil {
		apiKey = strings.TrimSpace(*apiKeys.ZAI)
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("Z_AI_API_KEY"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("ZAI_API_KEY"))
	}
	if apiKey == "" {
		return "", fmt.Errorf("read_video provider z-ai requires Z.AI auth in config/provider-api-keys.json or Z_AI_API_KEY")
	}

	videoBytes, err := base64.StdEncoding.DecodeString(videoData.Data)
	if err != nil {
		return "", fmt.Errorf("failed to decode video data: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(videoData.Filepath))
	if !zaiVideoAnalysisExtensions[ext] {
		return "", fmt.Errorf("Z.AI video_analysis supports only MP4/MOV/M4V files up to 8 MB (got extension: %s)", ext)
	}
	const maxZAIVideoSize = 8 * 1024 * 1024
	if len(videoBytes) > maxZAIVideoSize {
		return "", fmt.Errorf("Z.AI video_analysis supports videos up to 8 MB (got %d bytes)", len(videoBytes))
	}

	responseText, err := analyzeZAIVideoWithMCP(ctx, apiKey, videoData.Filepath, videoBytes, videoData.Query)
	if err != nil {
		return "", err
	}
	if responseText == "" {
		responseText = "(No response from video analysis)"
	}

	const maxResponseSize = 100 * 1024
	if len(responseText) > maxResponseSize {
		responseText = responseText[:maxResponseSize] + "\n... (response truncated)"
	}

	response := map[string]any{
		"filepath": videoData.Filepath,
		"query":    videoData.Query,
		"provider": "z-ai",
		"tool":     "video_analysis",
		"model":    "glm-4.6v",
		"response": responseText,
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}
	return string(responseJSON), nil
}

var zaiVideoAnalysisExtensions = map[string]bool{
	".mp4": true,
	".mov": true,
	".m4v": true,
}

func analyzeZAIVideoWithMCP(ctx context.Context, apiKey, sourcePath string, data []byte, query string) (string, error) {
	ext := strings.ToLower(filepath.Ext(sourcePath))
	tmp, err := os.CreateTemp("", "zai-video-*"+ext)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary video file for Z.AI MCP: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("failed to write temporary video file for Z.AI MCP: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("failed to close temporary video file for Z.AI MCP: %w", err)
	}

	mcpCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	env := append(os.Environ(),
		"Z_AI_API_KEY="+apiKey,
		"ZAI_API_KEY="+apiKey,
		"Z_AI_MODE=ZAI",
	)
	client, err := mcpclient.NewStdioMCPClient("npx", env, "-y", "@z_ai/mcp-server@latest")
	if err != nil {
		return "", fmt.Errorf("failed to start Z.AI Vision MCP server with npx: %w", err)
	}
	defer client.Close()

	if _, err := client.Initialize(mcpCtx, mcpgo.InitializeRequest{
		Params: mcpgo.InitializeParams{
			ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcpgo.Implementation{
				Name:    "mcp-agent-builder-go",
				Version: "dev",
			},
		},
	}); err != nil {
		return "", fmt.Errorf("failed to initialize Z.AI Vision MCP server: %w", err)
	}

	tools, err := client.ListTools(mcpCtx, mcpgo.ListToolsRequest{})
	if err != nil {
		return "", fmt.Errorf("failed to list Z.AI Vision MCP tools: %w", err)
	}
	var videoTool *mcpgo.Tool
	for i := range tools.Tools {
		if tools.Tools[i].Name == "video_analysis" {
			videoTool = &tools.Tools[i]
			break
		}
	}
	if videoTool == nil {
		return "", fmt.Errorf("Z.AI Vision MCP server did not expose video_analysis")
	}

	result, err := client.CallTool(mcpCtx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      "video_analysis",
			Arguments: zaiVideoAnalysisArguments(*videoTool, tmpPath, query),
		},
	})
	if err != nil {
		return "", fmt.Errorf("Z.AI video_analysis MCP call failed: %w", err)
	}

	text := mcpToolResultText(result)
	if result.IsError {
		if text == "" {
			text = "unknown MCP tool error"
		}
		return "", fmt.Errorf("Z.AI video_analysis returned an error: %s", text)
	}
	return text, nil
}

func zaiVideoAnalysisArguments(tool mcpgo.Tool, videoPath, query string) map[string]any {
	properties := mcpToolPropertyNames(tool)
	pathKey := firstPresent(properties,
		"video_path", "videoPath", "local_video_path", "localVideoPath",
		"file_path", "filePath", "filepath", "path", "video_file", "videoFile",
		"video", "video_url", "videoUrl", "url", "file",
	)
	if pathKey == "" {
		pathKey = "video_path"
	}
	queryKey := firstPresent(properties, "prompt", "query", "question", "text", "instruction")
	if queryKey == "" {
		queryKey = "prompt"
	}
	return map[string]any{
		pathKey:  videoPath,
		queryKey: query,
	}
}

func mcpToolPropertyNames(tool mcpgo.Tool) map[string]bool {
	properties := make(map[string]bool)
	for key := range tool.InputSchema.Properties {
		properties[key] = true
	}
	if len(properties) > 0 || len(tool.RawInputSchema) == 0 {
		return properties
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(tool.RawInputSchema, &schema); err != nil {
		return properties
	}
	for key := range schema.Properties {
		properties[key] = true
	}
	return properties
}

func firstPresent(properties map[string]bool, keys ...string) string {
	for _, key := range keys {
		if properties[key] {
			return key
		}
	}
	return ""
}

func mcpToolResultText(result *mcpgo.CallToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content)+1)
	for _, content := range result.Content {
		if text, ok := mcpgo.AsTextContent(content); ok && strings.TrimSpace(text.Text) != "" {
			parts = append(parts, text.Text)
			continue
		}
		if encoded, err := json.Marshal(content); err == nil && string(encoded) != "null" {
			parts = append(parts, string(encoded))
		}
	}
	if result.StructuredContent != nil {
		if encoded, err := json.Marshal(result.StructuredContent); err == nil && string(encoded) != "null" {
			parts = append(parts, string(encoded))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func normalizeReadVideoProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "":
		return ""
	case "kimi", "moonshot":
		return "kimi"
	case "zai", "z.ai", "z_ai", "z-ai":
		return "z-ai"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func stringFromMap(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func uploadKimiVideo(ctx context.Context, baseURL, apiKey, videoPath, mimeType string, data []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("purpose", "video"); err != nil {
		return "", err
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeMultipartFilename(filepath.Base(videoPath))))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/files", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}
	if strings.TrimSpace(parsed.ID) == "" {
		return "", fmt.Errorf("upload response did not include file id")
	}
	return strings.TrimSpace(parsed.ID), nil
}

func analyzeKimiVideo(ctx context.Context, baseURL, apiKey, fileID, query string) (string, error) {
	payload := map[string]any{
		"model": "kimi-k2.6",
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "You are Kimi, an AI assistant provided by Moonshot AI. Analyze the uploaded video accurately and answer the user's question.",
			},
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "video_url",
						"video_url": map[string]any{
							"url": "ms://" + fileID,
						},
					},
					{
						"type": "text",
						"text": query,
					},
				},
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("chat completion failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse chat response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", nil
	}
	return parsed.Choices[0].Message.Content, nil
}

func escapeMultipartFilename(name string) string {
	name = strings.ReplaceAll(name, `\`, `\\`)
	name = strings.ReplaceAll(name, `"`, `\"`)
	return name
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
	workspaceURL string,
) func(ctx context.Context, args map[string]any) (string, error) {
	return func(ctx context.Context, args map[string]any) (string, error) {
		log.Printf("[READ_IMAGE_DEBUG] Wrapped read_image executor called")

		// Step 1: Call the base executor to get base64 image data from workspace
		rawResult, err := baseExecutor(ctx, args)
		if err != nil {
			log.Printf("[READ_IMAGE_DEBUG] Base executor failed: %v", err)
			return "", err
		}

		// Step 2: Parse the structured result from workspace
		var imageData workspace.ReadImageResult
		if err := json.Unmarshal([]byte(rawResult), &imageData); err != nil {
			log.Printf("[READ_IMAGE_DEBUG] Failed to parse base result as ReadImageResult: %v", err)
			return "", fmt.Errorf("failed to parse image data: %w", err)
		}

		log.Printf("[READ_IMAGE_DEBUG] Image data received: filepath=%s, mimeType=%s, base64Length=%d",
			imageData.Filepath, imageData.MimeType, len(imageData.Data))

		// Step 3: Resolve the analysis LLM from workspace config, falling back to the current agent model.
		llmModel, provider, modelID, err := createImageAnalysisLLM(ctx, workspaceURL)
		if err != nil {
			log.Printf("[READ_IMAGE_DEBUG] Failed to create LLM client: %v", err)
			return "", fmt.Errorf("failed to initialize LLM for image analysis: %w", err)
		}

		log.Printf("[READ_IMAGE_DEBUG] LLM client created (provider=%s, model=%s), making GenerateContent call",
			provider, modelID)

		// Step 4: Make the LLM call with the image + query
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

		// Step 5: Extract and return the text response
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

func createImageAnalysisLLM(ctx context.Context, workspaceURL string) (llmtypes.Model, string, string, error) {
	apiKeys := loadWorkspaceProviderAPIKeys(ctx, workspaceURL)

	if workspaceURL != "" {
		imageCfg, exists, err := services.LoadImageAnalysisConfig(ctx, workspaceURL)
		if err != nil {
			log.Printf("[READ_IMAGE_DEBUG] Failed to load image analysis config: %v", err)
		} else if exists && imageCfg != nil {
			var candidates []services.ImageGenerationModelConfig
			if imageCfg.Primary != nil {
				candidates = append(candidates, *imageCfg.Primary)
			}
			candidates = append(candidates, imageCfg.Fallbacks...)

			for _, candidate := range candidates {
				provider, modelID, err := normalizeImageAnalysisProviderAndModel(candidate.Provider, candidate.ModelID)
				if err != nil {
					continue
				}
				if !hasImageAnalysisProviderAuth(provider, apiKeys) {
					continue
				}
				model, err := llm.InitializeLLM(llm.Config{
					Provider: llm.Provider(provider),
					ModelID:  modelID,
					Context:  ctx,
					APIKeys:  apiKeys,
				})
				if err == nil {
					return model, provider, modelID, nil
				}
				log.Printf("[READ_IMAGE_DEBUG] Failed to initialize configured image analysis model %s/%s: %v", provider, modelID, err)
			}
			return nil, "", "", fmt.Errorf("image analysis config requires a valid configured provider/model with matching auth")
		}
	}

	llmConfigRaw := ctx.Value(mcpagent.ToolExecutionLLMConfigKey)
	if llmConfigRaw == nil {
		log.Printf("[READ_IMAGE_DEBUG] No LLM config in context — cannot perform image analysis fallback")
		return nil, "", "", fmt.Errorf("LLM configuration not available in context for image analysis")
	}
	llmConfig, ok := llmConfigRaw.(mcpagent.LLMModel)
	if !ok {
		log.Printf("[READ_IMAGE_DEBUG] LLM config in context has unexpected type: %T", llmConfigRaw)
		return nil, "", "", fmt.Errorf("LLM configuration in context has unexpected type")
	}

	model, err := createLLMFromConfig(ctx, llmConfig)
	if err != nil {
		return nil, "", "", err
	}
	return model, llmConfig.Provider, llmConfig.ModelID, nil
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
		case llm.ProviderZAI:
			apiKeys.ZAI = config.APIKey
		case llm.ProviderVertex:
			apiKeys.Vertex = config.APIKey
		case llm.ProviderGeminiCLI:
			apiKeys.GeminiCLI = config.APIKey
		case llm.ProviderCodexCLI:
			apiKeys.CodexCLI = config.APIKey
		case llm.ProviderMiniMax:
			apiKeys.MiniMax = config.APIKey
		case llm.ProviderMiniMaxCodingPlan:
			apiKeys.MiniMaxCodingPlan = config.APIKey
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
