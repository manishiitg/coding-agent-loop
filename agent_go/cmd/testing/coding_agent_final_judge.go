package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"
)

type codingAgentFinalJudgeCase struct {
	Provider     string
	RawContext   string
	Extracted    string
	UserGoal     string
	MustContain  []string
	Forbidden    []string
	ExpectedNote string
}

func (c *codingAgentChatE2EClient) assertVertexFinalJudge(ctx context.Context, sessionID, provider, query, final, rawEvents string, required, forbidden []string) error {
	if !codingAgentChatE2EFlags.vertexFinalJudge {
		return nil
	}
	rawContext := rawEvents
	if terminalContext, err := c.collectTerminalJudgeContext(ctx, sessionID); err == nil && strings.TrimSpace(terminalContext) != "" {
		rawContext = terminalContext + "\n\nAPP_EVENTS:\n" + rawEvents
	}
	if strings.TrimSpace(rawContext) == "" {
		rawContext = final
	}
	return runCodingAgentVertexFinalJudge(ctx, codingAgentFinalJudgeCase{
		Provider:     provider,
		RawContext:   rawContext,
		Extracted:    final,
		UserGoal:     query,
		MustContain:  required,
		Forbidden:    mergeCodingAgentFinalJudgeForbidden(forbidden),
		ExpectedNote: "This is an app-level coding-agent E2E. EXTRACTED_FINAL is the server unified_completion.final_result; RAW_PROVIDER_OUTPUT is terminal snapshots plus app event JSON when available.",
	})
}

func (c *codingAgentChatE2EClient) collectTerminalJudgeContext(ctx context.Context, sessionID string) (string, error) {
	resp, _, err := c.getTerminals(ctx, sessionID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, terminal := range resp.Terminals {
		if strings.TrimSpace(terminal.Content) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "TERMINAL terminal_id=%s tmux_session=%s active=%v state=%s\n", terminal.TerminalID, terminal.TmuxSession, terminal.Active, terminal.State)
		b.WriteString(terminal.Content)
	}
	return b.String(), nil
}

func runCodingAgentVertexFinalJudge(parent context.Context, c codingAgentFinalJudgeCase) error {
	if strings.TrimSpace(c.Extracted) == "" {
		return fmt.Errorf("%s final extraction is empty", c.Provider)
	}
	for _, want := range c.MustContain {
		if !strings.Contains(c.Extracted, want) {
			return fmt.Errorf("%s final extraction missing %q: %s", c.Provider, want, c.Extracted)
		}
	}
	for _, bad := range c.Forbidden {
		if strings.Contains(c.Extracted, bad) {
			return fmt.Errorf("%s final extraction leaked %q: %s", c.Provider, bad, c.Extracted)
		}
	}
	key := codingAgentVertexJudgeAPIKey()
	if key == "" {
		return fmt.Errorf("Vertex final-extraction judge requires GEMINI_API_KEY, VERTEX_API_KEY, or GOOGLE_API_KEY")
	}

	ctx, cancel := context.WithTimeout(parent, 75*time.Second)
	defer cancel()
	ctx = vertex.WithResponseSchemaFromJSON(ctx, codingAgentFinalJudgeSchema())

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  key,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return fmt.Errorf("create Vertex judge client: %w", err)
	}
	adapter := vertex.NewGoogleGenAIAdapter(client, codingAgentVertexJudgeModel(), codingAgentNoopJudgeLogger{})
	resp, err := adapter.GenerateContent(ctx, []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeSystem,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: `You are a strict test oracle for coding-agent final-response extraction.
Return only valid JSON. Do not explain outside JSON.`}},
		},
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: buildCodingAgentFinalJudgePrompt(c)}},
		},
	},
		llmtypes.WithJSONMode(),
		llmtypes.WithTemperature(0),
		llmtypes.WithMaxTokens(8192),
	)
	if err != nil {
		return fmt.Errorf("Vertex final-extraction judge call failed: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0] == nil {
		return fmt.Errorf("Vertex final-extraction judge returned no choices")
	}

	var verdict struct {
		Pass            bool   `json:"pass"`
		MatchesUserGoal bool   `json:"matches_user_goal"`
		FormattingOK    bool   `json:"formatting_ok"`
		NoiseFree       bool   `json:"noise_free"`
		Reason          string `json:"reason"`
	}
	raw := strings.TrimSpace(resp.Choices[0].Content)
	if err := json.Unmarshal([]byte(extractCodingAgentJudgeJSONObject(raw)), &verdict); err != nil {
		return fmt.Errorf("Vertex final-extraction judge returned invalid JSON: %w; content=%q", err, raw)
	}
	if !verdict.Pass || !verdict.MatchesUserGoal || !verdict.FormattingOK || !verdict.NoiseFree {
		return fmt.Errorf("Vertex final-extraction judge rejected %s extraction: %+v; extracted=%q", c.Provider, verdict, c.Extracted)
	}
	return nil
}

func codingAgentVertexJudgeAPIKey() string {
	for _, name := range []string{"GEMINI_API_KEY", "VERTEX_API_KEY", "GOOGLE_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func codingAgentVertexJudgeModel() string {
	if model := strings.TrimSpace(codingAgentChatE2EFlags.vertexJudgeModel); model != "" {
		return model
	}
	if model := strings.TrimSpace(os.Getenv("VERTEX_FINAL_EXTRACTION_JUDGE_MODEL")); model != "" {
		return model
	}
	return vertex.ModelGemini31ProPreview
}

func defaultCodingAgentFinalJudgeForbidden() []string {
	return []string{
		"unified_completion",
		"tool_call_start",
		"tool_call_end",
		"tool_call_delta",
		"execute_shell_command",
		"api-bridge",
		"stdout",
		"stderr",
		"exit_code",
		"tmux_session",
		"terminal_id",
		"Type your message",
		"Do not use tools",
	}
}

func mergeCodingAgentFinalJudgeForbidden(extra []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range append(defaultCodingAgentFinalJudgeForbidden(), extra...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func codingAgentFinalJudgeSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pass":              map[string]interface{}{"type": "boolean"},
			"matches_user_goal": map[string]interface{}{"type": "boolean"},
			"formatting_ok":     map[string]interface{}{"type": "boolean"},
			"noise_free":        map[string]interface{}{"type": "boolean"},
			"reason":            map[string]interface{}{"type": "string"},
		},
		"required": []interface{}{
			"pass",
			"matches_user_goal",
			"formatting_ok",
			"noise_free",
			"reason",
		},
	}
}

func buildCodingAgentFinalJudgePrompt(c codingAgentFinalJudgeCase) string {
	return fmt.Sprintf(`Judge whether EXTRACTED_FINAL is the clean final assistant response selected by the app from RAW_PROVIDER_OUTPUT.

Provider: %s
User goal: %s
Expected note: %s
Must contain these fragments: %s
Must not contain these noise fragments: %s

Pass criteria:
- EXTRACTED_FINAL answers the latest user goal using the final assistant response in RAW_PROVIDER_OUTPUT.
- It preserves meaningful formatting from the final answer, including line breaks, bullets, code blocks, and paths.
- It omits app event JSON, terminal chrome, prompts, thought/process headers, tool cards, MCP names, shell/curl/header fragments, JSON tool output, and earlier assistant drafts.
- Do not require exact wording if the extraction is semantically the same and formatting is preserved.

Return JSON exactly like:
{"pass":true,"matches_user_goal":true,"formatting_ok":true,"noise_free":true,"reason":"short reason"}

RAW_PROVIDER_OUTPUT:
%s

EXTRACTED_FINAL:
%s
`, c.Provider, c.UserGoal, c.ExpectedNote, strings.Join(c.MustContain, " | "), strings.Join(c.Forbidden, " | "), truncateCodingAgentJudgeContext(c.RawContext), c.Extracted)
}

func truncateCodingAgentJudgeContext(s string) string {
	const max = 12000
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return "[truncated to last app-level terminal/event rows]\n" + string(runes[len(runes)-max:])
}

func extractCodingAgentJudgeJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end >= start {
		return s[start : end+1]
	}
	return s
}

type codingAgentNoopJudgeLogger struct{}

func (codingAgentNoopJudgeLogger) Infof(string, ...any)          {}
func (codingAgentNoopJudgeLogger) Errorf(string, ...any)         {}
func (codingAgentNoopJudgeLogger) Debugf(string, ...interface{}) {}
