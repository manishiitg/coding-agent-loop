package agent

import (
	"strings"
	"testing"

	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestSanitizeHistoryForPlainTextProviderConvertsToolMessages(t *testing.T) {
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{llmtypes.ToolCall{
				ID: "tool-1",
				FunctionCall: &llmtypes.FunctionCall{
					Name:      "execute_shell_command",
					Arguments: `{"command":"list_executions"}`,
				},
			}},
		},
		{
			Role: llmtypes.ChatMessageTypeTool,
			Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{
				ToolCallID: "tool-1",
				Name:       "execute_shell_command",
				Content:    `{"stdout":"done"}`,
			}},
		},
	}

	got := sanitizeHistoryForPlainTextProvider(messages)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[1].Role != llmtypes.ChatMessageTypeAI {
		t.Fatalf("expected tool role to be converted to ai, got %q", got[1].Role)
	}
	for _, msg := range got {
		for _, part := range msg.Parts {
			if _, ok := part.(llmtypes.ToolCall); ok {
				t.Fatalf("tool call was not converted to text: %#v", part)
			}
			if _, ok := part.(llmtypes.ToolCallResponse); ok {
				t.Fatalf("tool response was not converted to text: %#v", part)
			}
		}
	}
	if !strings.Contains(got[0].Parts[0].(llmtypes.TextContent).Text, "execute_shell_command") {
		t.Fatalf("expected converted tool call to include tool name, got %q", got[0].Parts[0].(llmtypes.TextContent).Text)
	}
}

func TestSanitizeHistoryForPlainTextProviderSplitsConsecutiveHumanMessages(t *testing.T) {
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "stop the background"}},
		},
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "[AUTO-NOTIFICATION] done"}},
		},
	}

	got := sanitizeHistoryForPlainTextProvider(messages)
	if len(got) != 3 {
		t.Fatalf("expected inserted assistant message, got %d messages", len(got))
	}
	if got[1].Role != llmtypes.ChatMessageTypeAI {
		t.Fatalf("expected inserted assistant role, got %q", got[1].Role)
	}
	if !strings.Contains(got[1].Parts[0].(llmtypes.TextContent).Text, "interrupted") {
		t.Fatalf("expected interruption acknowledgement, got %q", got[1].Parts[0].(llmtypes.TextContent).Text)
	}
}

func TestProviderNeedsPlainTextHistory(t *testing.T) {
	if !providerNeedsPlainTextHistory(llm.Provider("claude-code")) {
		t.Fatal("expected claude-code to use plain text history")
	}
	if providerNeedsPlainTextHistory(llm.Provider("anthropic")) {
		t.Fatal("did not expect anthropic to use plain text history")
	}
}
