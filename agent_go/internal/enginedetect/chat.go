package enginedetect

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ChatMessage is one turn in a conversation.
type ChatMessage struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

// Chat runs a single agent turn for the given engine over the supplied
// conversation, in workingDir. This is the first "dynamic" slice: a plain
// completion (no bridge tools yet). Bridge-only tools + FolderGuard + streaming
// are layered on in later slices.
func Chat(ctx context.Context, provider, modelID, workingDir, systemPrompt string, history []ChatMessage) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))

	var llmProvider llm.Provider
	var defaultModel string
	var callOpts []llmtypes.CallOption
	keys := &llm.ProviderAPIKeys{}

	switch provider {
	case "claude-code":
		llmProvider = llm.ProviderClaudeCode
		defaultModel = "claude-code"
		if workingDir != "" {
			callOpts = append(callOpts, llm.WithClaudeCodeWorkingDir(workingDir))
		}
	case "codex-cli":
		llmProvider = llm.ProviderCodexCLI
		defaultModel = "medium"
		if k := strings.TrimSpace(os.Getenv("CODEX_API_KEY")); k != "" {
			keys.CodexCLI = &k
		}
	case "cursor-cli":
		llmProvider = llm.ProviderCursorCLI
		defaultModel = "cursor-cli"
		if workingDir != "" {
			callOpts = append(callOpts, llm.WithCursorWorkingDir(workingDir))
		}
		if k := strings.TrimSpace(os.Getenv("CURSOR_API_KEY")); k != "" {
			keys.CursorCLI = &k
		}
	case "pi-cli":
		llmProvider = llm.ProviderPiCLI
		defaultModel = "google/gemini-3.5-flash"
		if workingDir != "" {
			callOpts = append(callOpts, llm.WithPiWorkingDir(workingDir))
		}
		if mk := selectPiAPIKeyForModel(buildProviderAPIKeysFromEnv(), defaultModel); mk != "" {
			piProv := piProviderFromModelID(defaultModel)
			keys.PiProviderKeys = map[string]string{piProv: mk}
			if piProv == "google" || piProv == "google-vertex" {
				keys.PiCLI = &mk
			}
		}
	default:
		return "", fmt.Errorf("engine %q is not supported", provider)
	}

	if strings.TrimSpace(modelID) == "" {
		modelID = defaultModel
	}

	model, err := llm.InitializeLLM(llm.Config{
		Provider: llmProvider,
		ModelID:  modelID,
		APIKeys:  keys,
		Context:  context.Background(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to start %s: %w", provider, err)
	}

	msgs := make([]llmtypes.MessageContent, 0, len(history)+1)
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeSystem,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: systemPrompt}},
		})
	}
	for _, m := range history {
		role := llmtypes.ChatMessageTypeHuman
		if strings.EqualFold(m.Role, "assistant") || strings.EqualFold(m.Role, "ai") {
			role = llmtypes.ChatMessageTypeAI
		}
		msgs = append(msgs, llmtypes.MessageContent{
			Role:  role,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: m.Text}},
		})
	}

	callCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	resp, err := model.GenerateContent(callCtx, msgs, callOpts...)
	// Best-effort cleanup of any interactive tmux session the adapter spun up.
	if handle, ok := llmtypes.ExtractCodingProviderSessionHandleFromResponse(resp); ok && provider == "pi-cli" {
		llm.ClosePiCLIInteractiveSessionByTmux(handle.TmuxSession, "chat cleanup")
	}
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("the engine timed out")
		}
		return "", fmt.Errorf("%s", strings.TrimSpace(err.Error()))
	}
	if resp != nil && len(resp.Choices) > 0 {
		return strings.TrimSpace(resp.Choices[0].Content), nil
	}
	return "", fmt.Errorf("the engine returned an empty response")
}
