package testing

import stdtesting "testing"

func TestDefaultCodingAgentE2EModelIncludesCursorCLI(st *stdtesting.T) {
	tests := map[string]string{
		"gemini-cli":   "gemini-3.1-flash-lite",
		"codex-cli":    "gpt-5.3-codex-spark",
		"cursor-cli":   "cursor-cli",
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
