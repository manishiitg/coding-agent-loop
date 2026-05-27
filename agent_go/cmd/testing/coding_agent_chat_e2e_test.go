package testing

import (
	"strings"
	stdtesting "testing"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"
)

func TestDefaultCodingAgentE2EModelIncludesCodingCLIProviders(st *stdtesting.T) {
	tests := map[string]string{
		"gemini-cli":   "gemini-3.1-flash-lite",
		"codex-cli":    "gpt-5.3-codex-spark",
		"cursor-cli":   "cursor-cli",
		"agy-cli":      "agy-cli",
		"claude-code":  "claude-code",
		"opencode-cli": "opencode-cli",
	}

	for provider, want := range tests {
		st.Run(provider, func(st *stdtesting.T) {
			if got := defaultCodingAgentE2EModel(provider); got != want {
				st.Fatalf("defaultCodingAgentE2EModel(%q) = %q, want %q", provider, got, want)
			}
		})
	}
}

func TestProviderSupportsTmuxLossResumeE2EIncludesNativeResumeProviders(st *stdtesting.T) {
	cases := map[string]bool{
		"claude-code":  true,
		"codex-cli":    true,
		"agy-cli":      true,
		"cursor-cli":   false,
		"gemini-cli":   false,
		"opencode-cli": false,
	}

	for provider, want := range cases {
		st.Run(provider, func(st *stdtesting.T) {
			if got := providerSupportsTmuxLossResumeE2E(provider); got != want {
				st.Fatalf("providerSupportsTmuxLossResumeE2E(%q) = %v, want %v", provider, got, want)
			}
		})
	}
}

func TestEventsProveProviderRequiresTypedProviderEvent(st *stdtesting.T) {
	events := []map[string]interface{}{
		{
			"type": "user_message",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"content": `{"provider":"codex-cli"}`,
				},
			},
		},
		{
			"type": "tool_call_start",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"args": `{"provider":"codex-cli"}`,
				},
			},
		},
	}
	if eventsProveProvider(events, "codex-cli") {
		st.Fatalf("provider proof accepted echoed request/tool payload")
	}

	events = append(events, map[string]interface{}{
		"type": "agent_start",
		"data": map[string]interface{}{
			"data": map[string]interface{}{
				"provider": "codex-cli",
			},
		},
	})
	if !eventsProveProvider(events, "codex-cli") {
		st.Fatalf("provider proof rejected typed agent_start provider")
	}

	events = []map[string]interface{}{
		{
			"type": "unified_completion",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"metadata": map[string]interface{}{
						"provider": "claude-code",
					},
				},
			},
		},
	}
	if !eventsProveProvider(events, "claude-code") {
		st.Fatalf("provider proof rejected typed unified_completion metadata provider")
	}

	events = []map[string]interface{}{
		{
			"type": "llm_generation_end",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"metadata": map[string]interface{}{
						"coding_provider_session_handle": map[string]interface{}{
							"provider": "gemini-cli",
						},
					},
				},
			},
		},
	}
	if !eventsProveProvider(events, "gemini-cli") {
		st.Fatalf("provider proof rejected typed coding provider session handle")
	}
}

func TestExtractUnifiedCompletionFinalUsesDocumentedShape(st *stdtesting.T) {
	events := []map[string]interface{}{
		{
			"type": "tool_call_end",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"final_result": "wrong nested tool value",
				},
			},
		},
		{
			"type": "unified_completion",
			"data": map[string]interface{}{
				"data": map[string]interface{}{
					"final_result": "right answer",
				},
			},
		},
	}
	if got := extractUnifiedCompletionFinal(events); got != "right answer" {
		st.Fatalf("extractUnifiedCompletionFinal() = %q, want right answer", got)
	}
}

func TestCodingAgentFinalJudgePromptIncludesAppLevelContract(st *stdtesting.T) {
	prompt := buildCodingAgentFinalJudgePrompt(codingAgentFinalJudgeCase{
		Provider:     "codex-cli",
		RawContext:   "raw terminal\nunified_completion noise",
		Extracted:    "DONE_TOKEN",
		UserGoal:     "Reply with DONE_TOKEN.",
		MustContain:  []string{"DONE_TOKEN"},
		Forbidden:    []string{"unified_completion"},
		ExpectedNote: "app-level final result",
	})
	for _, want := range []string{
		"Provider: codex-cli",
		"Reply with DONE_TOKEN.",
		"Must contain these fragments: DONE_TOKEN",
		"Must not contain these noise fragments: unified_completion",
		"EXTRACTED_FINAL:",
		"DONE_TOKEN",
	} {
		if !strings.Contains(prompt, want) {
			st.Fatalf("judge prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestExtractCodingAgentJudgeJSONObjectAllowsWrappedJSON(st *stdtesting.T) {
	input := "judge output\n```json\n{\"pass\":true}\n```\n"
	if got := extractCodingAgentJudgeJSONObject(input); got != `{"pass":true}` {
		st.Fatalf("extractCodingAgentJudgeJSONObject() = %q", got)
	}
}

func TestDefaultCodingAgentFinalJudgeForbiddenIncludesAppNoise(st *stdtesting.T) {
	forbidden := defaultCodingAgentFinalJudgeForbidden()
	for _, want := range []string{"unified_completion", "tool_call_start", "execute_shell_command", "tmux_session"} {
		found := false
		for _, got := range forbidden {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			st.Fatalf("default forbidden list missing %q: %v", want, forbidden)
		}
	}
}

func TestMergeCodingAgentFinalJudgeForbiddenAddsDynamicPriorTurnNoise(st *stdtesting.T) {
	forbidden := mergeCodingAgentFinalJudgeForbidden([]string{"OLD_FINAL_TOKEN", "execute_shell_command", ""})
	counts := map[string]int{}
	for _, got := range forbidden {
		counts[got]++
	}
	if counts["OLD_FINAL_TOKEN"] != 1 {
		st.Fatalf("merged forbidden list missing dynamic prior-turn token: %v", forbidden)
	}
	if counts["execute_shell_command"] != 1 {
		st.Fatalf("merged forbidden list should de-dupe default entries: %v", forbidden)
	}
}

func TestCodingAgentVertexJudgeModelDefaultsToGemini31Pro(st *stdtesting.T) {
	old := codingAgentChatE2EFlags.vertexJudgeModel
	codingAgentChatE2EFlags.vertexJudgeModel = ""
	st.Cleanup(func() { codingAgentChatE2EFlags.vertexJudgeModel = old })
	st.Setenv("VERTEX_FINAL_EXTRACTION_JUDGE_MODEL", "")

	if got := codingAgentVertexJudgeModel(); got != vertex.ModelGemini31ProPreview {
		st.Fatalf("codingAgentVertexJudgeModel() = %q, want %q", got, vertex.ModelGemini31ProPreview)
	}
}
