package server

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func reconnectMessage(role llmtypes.ChatMessageType, text string) llmtypes.MessageContent {
	return llmtypes.MessageContent{Role: role, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: text}}}
}

func TestPolicyReconnectCarriesRetryContextWithoutOldTools(t *testing.T) {
	history := []llmtypes.MessageContent{
		reconnectMessage(llmtypes.ChatMessageTypeSystem, "obsolete system permissions"),
		reconnectMessage(llmtypes.ChatMessageTypeHuman, "Test Notion search"),
		{Role: llmtypes.ChatMessageTypeAI, Parts: []llmtypes.ContentPart{llmtypes.ToolCall{ID: "old-call", FunctionCall: &llmtypes.FunctionCall{Name: "obsolete_tool"}}}},
		reconnectMessage(llmtypes.ChatMessageTypeTool, "large old tool output"),
		reconnectMessage(llmtypes.ChatMessageTypeAI, "Notion is connected, but this chat only has Jam in scope."),
	}
	handoff := buildModeChangeConversationContext("workshop", "workshop", "Workflow/test/conversation.json", history)
	for _, expected := range []string{"Test Notion search", "Notion is connected", "current tool permissions", "Parts[].Text"} {
		if !strings.Contains(handoff, expected) {
			t.Fatalf("handoff is missing %q", expected)
		}
	}
	for _, excluded := range []string{"obsolete system permissions", "obsolete_tool", "large old tool output"} {
		if strings.Contains(handoff, excluded) {
			t.Fatalf("old runtime content leaked: %q", excluded)
		}
	}
	if strings.Index(handoff, "Test Notion search") > strings.Index(handoff, "Notion is connected") {
		t.Fatal("dialogue order reversed")
	}
	current := []llmtypes.MessageContent{
		reconnectMessage(llmtypes.ChatMessageTypeHuman, handoff),
		reconnectMessage(llmtypes.ChatMessageTypeHuman, "try it again once"),
		reconnectMessage(llmtypes.ChatMessageTypeAI, "Notion search succeeded"),
	}
	merged := mergeModeChangedChatHistory(history, current)
	if len(merged) != len(history)+2 {
		t.Fatal("reconnect must keep all earlier turns and append only the new exchange")
	}
	// A second policy refresh in the same process must still see the original
	// request, without nesting the previous synthetic handoff.
	next := buildModeChangeConversationContext("workshop", "run", "", merged)
	if !strings.Contains(next, "Test Notion search") || strings.Count(next, "[WORKFLOW CHAT HANDOFF]") != 1 {
		t.Fatal("repeated reconnect lost context or nested transport context")
	}
}

func TestPolicyReconnectBoundsDialogueAndSkipsToolNoise(t *testing.T) {
	var history []llmtypes.MessageContent
	for i := 0; i < 200; i++ {
		history = append(history, reconnectMessage(llmtypes.ChatMessageTypeAI, strings.Repeat("界", 10000)))
	}
	history = append(history, reconnectMessage(llmtypes.ChatMessageTypeHuman, "retry the Notion check"))
	for i := 0; i < 100; i++ {
		history = append(history, reconnectMessage(llmtypes.ChatMessageTypeTool, strings.Repeat("tool output", 1000)))
	}
	got := buildModeChangeConversationContext("run", "workshop", "", history)
	if !strings.Contains(got, "retry the Notion check") || !utf8.ValidString(got) || len(got) > maxCodingAgentFallbackBytes {
		t.Fatalf("invalid bounded handoff: bytes=%d validUTF8=%v", len(got), utf8.ValidString(got))
	}
}

func TestPolicyReconnectKeepsContextWithHeavilyEscapedLatestReply(t *testing.T) {
	history := []llmtypes.MessageContent{
		reconnectMessage(llmtypes.ChatMessageTypeHuman, "Test Notion search"),
		reconnectMessage(llmtypes.ChatMessageTypeAI, strings.Repeat("<>&\x01", 10000)+"Please retry Notion"),
	}
	got := buildModeChangeConversationContext("workshop", "workshop", "", history)
	if !strings.Contains(got, "Test Notion search") || !strings.Contains(got, "Please retry Notion") || len(got) > maxCodingAgentFallbackBytes {
		t.Fatal("large encoded reply displaced the request or exceeded the context budget")
	}
}
