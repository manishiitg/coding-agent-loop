package enginedetect

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Validate runs a live validation for one of the four supported coding-agent
// engines. It exercises the same adapter path as a real workflow run.
// modelID may be empty; a sensible default is chosen per provider.
func Validate(ctx context.Context, provider, modelID string) (ok bool, message string) {
	resp := validateProviderConfig(llm.APIKeyValidationRequest{
		Provider: provider,
		ModelID:  modelID,
	})
	return resp.Valid, resp.Message
}

// validateProviderConfig dispatches to the per-engine validator.
// (Copied from cmd/server/llm_config_handlers.go, restricted to the four
// supported coding agents.)
func validateProviderConfig(req llm.APIKeyValidationRequest) llm.APIKeyValidationResponse {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	switch provider {
	case "claude-code":
		return validateClaudeCodeCLI()
	case "codex-cli":
		return validateCodexCLI(req.APIKey)
	case "cursor-cli":
		return validateCursorCLI(req.APIKey, req.ModelID)
	case "pi-cli":
		return validatePiCLI(req.APIKey, req.ModelID, req.Options)
	default:
		return llm.APIKeyValidationResponse{
			Valid:   false,
			Message: fmt.Sprintf("Provider %q is not a supported coding-agent engine.", req.Provider),
		}
	}
}

// validateClaudeCodeCLI validates the Claude Code CLI by checking it exists and
// then running a real adapter call through llm.InitializeLLM.
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

// validateCodexCLI validates the OpenAI Codex CLI through the real adapter path.
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
	if strings.TrimSpace(apiKey) == "" && strings.TrimSpace(os.Getenv("CURSOR_API_KEY")) == "" {
		authenticated, conclusive := cursorCLILocalAuthState()
		if conclusive && !authenticated {
			return llm.APIKeyValidationResponse{
				Valid:   false,
				Message: cursorCLILoginRequiredMessage(),
			}
		}
		if !conclusive {
			log.Printf("[CURSOR-CLI VALIDATION] Auth status probe was inconclusive; continuing with real adapter validation")
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

// validatePiCLI validates Pi CLI through the real tmux adapter path.
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

// --- Pi key-selection helpers (copied from cmd/server/provider_keys_store.go) ---

func selectPiAPIKeyForModel(keys *llm.ProviderAPIKeys, modelID string) string {
	if keys == nil {
		return ""
	}
	provider := piProviderFromModelID(modelID)
	if key := piProviderKeyFromMap(keys.PiProviderKeys, provider); key != "" {
		return key
	}
	switch provider {
	case "google", "google-vertex":
		for _, value := range []*string{keys.PiCLI, keys.Vertex} {
			if key := strings.TrimSpace(stringPtrValue(value)); key != "" {
				return key
			}
		}
	case "openai":
		return stringPtrValue(keys.OpenAI)
	case "anthropic":
		return stringPtrValue(keys.Anthropic)
	case "openrouter":
		return stringPtrValue(keys.OpenRouter)
	case "zai":
		return stringPtrValue(keys.ZAI)
	case "kimi-coding", "moonshotai", "moonshotai-cn":
		return stringPtrValue(keys.Kimi)
	case "minimax":
		return stringPtrValue(keys.MiniMax)
	}
	return ""
}

func piProviderFromModelID(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || strings.EqualFold(modelID, "pi-cli") || strings.EqualFold(modelID, "auto") {
		return "google"
	}
	if slash := strings.Index(modelID, "/"); slash > 0 {
		return strings.ToLower(strings.TrimSpace(modelID[:slash]))
	}
	return "google"
}

func piProviderKeyFromMap(keys map[string]string, provider string) string {
	if keys == nil {
		return ""
	}
	for _, candidate := range piProviderKeyAliases(provider) {
		if key := strings.TrimSpace(keys[candidate]); key != "" {
			return key
		}
	}
	return ""
}

func piProviderKeyAliases(provider string) []string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "google-vertex":
		return []string{"google-vertex", "google"}
	case "moonshotai", "moonshotai-cn":
		return []string{provider, "kimi-coding"}
	default:
		return []string{provider}
	}
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
