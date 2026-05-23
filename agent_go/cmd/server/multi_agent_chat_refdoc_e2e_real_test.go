package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	claudecodeadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/claudecode"

	"mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

// TestMultiAgentChatPromptSteersToReferenceDocs is a real-LLM e2e test for
// the multi-agent chat prompt refactor. It verifies that the new
// schedule/secret cheat-sheet+pointer pattern actually steers the LLM to
// call get_reference_doc(kind="...") before performing rare-path actions,
// instead of inventing the file format / tool semantics from memory.
//
// What it does:
//  1. Builds the same system prompt the multi-agent chat session would see
//     (GetMultiAgentDelegationInstructionsWithUser).
//  2. Defines the get_reference_doc tool with the same schema the live agent
//     exposes (kind: enum of valid reference kinds).
//  3. Sends a "schedule a daily task" user message OR a "store a secret"
//     user message via the Anthropic API with claude-haiku.
//  4. Asserts the model's first response contains a tool_use block for
//     get_reference_doc with the expected kind. The system prompt pointer
//     ("call get_reference_doc(kind=\"schedule-management\") first") only
//     produces correct behavior if the LLM actually parses and acts on it.
//
// Gating:
//   - RUN_MULTIAGENT_REFDOC_E2E=1 to run (off by default — costs API tokens).
//   - ANTHROPIC_API_KEY required.
//   - ANTHROPIC_REFDOC_MODEL optional override (defaults to claude-haiku-4-5,
//     the cheapest model that's still smart enough to follow tool guidance).
//
// Cost: roughly $0.001 per case (two cases, ~10k input tokens each on
// claude-haiku-4-5).
func TestMultiAgentChatPromptSteersToReferenceDocs(t *testing.T) {
	if os.Getenv("RUN_MULTIAGENT_REFDOC_E2E") == "" {
		t.Skip("set RUN_MULTIAGENT_REFDOC_E2E=1 to run this real-LLM e2e (costs API tokens)")
	}
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY required")
	}
	model := strings.TrimSpace(os.Getenv("ANTHROPIC_REFDOC_MODEL"))
	if model == "" {
		model = "claude-haiku-4-5"
	}

	systemPrompt := virtualtools.GetMultiAgentDelegationInstructionsWithUser("Chats", "default")

	// get_reference_doc tool — matches the schema RegisterReferenceDocTool
	// publishes in production. Kind enum lists the multi-agent-accessible
	// reference docs (schedule-management, secret-management).
	tools := []anthropic.ToolUnionParam{
		{
			OfTool: &anthropic.ToolParam{
				Name:        "get_reference_doc",
				Description: anthropic.String("Load the full reference documentation for a workshop concept (schedule-management, secret-management, etc.)."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"kind": map[string]any{
							"type":        "string",
							"enum":        []string{"schedule-management", "secret-management"},
							"description": "Which reference doc to load.",
						},
					},
					Required: []string{"kind"},
				},
			},
		},
	}

	type caseSpec struct {
		name       string
		userMsg    string
		expectKind string
	}
	cases := []caseSpec{
		{
			name:       "schedule_request_triggers_schedule_doc_load",
			userMsg:    "I'd like to schedule a multi-agent task that runs every weekday at 9:00 AM. The task should send me a daily summary. Set it up.",
			expectKind: "schedule-management",
		},
		{
			name:       "secret_storage_request_triggers_secret_doc_load",
			userMsg:    "Please store my Slack API token. The value is sk-test-1234-fake-not-real. Save it as SLACK_TOKEN.",
			expectKind: "secret-management",
		},
	}

	client := anthropic.NewClient(anthropicoption.WithAPIKey(apiKey))

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
				Model:     anthropic.Model(model),
				MaxTokens: 1024,
				System: []anthropic.TextBlockParam{
					{Text: systemPrompt},
				},
				Messages: []anthropic.MessageParam{
					anthropic.NewUserMessage(anthropic.NewTextBlock(tc.userMsg)),
				},
				Tools: tools,
			})
			if err != nil {
				t.Fatalf("anthropic Messages.New: %v", err)
			}

			t.Logf("response stop_reason=%q, content blocks=%d", msg.StopReason, len(msg.Content))

			foundRefDocCall := false
			var calledKind string
			for _, block := range msg.Content {
				switch v := block.AsAny().(type) {
				case anthropic.ToolUseBlock:
					if v.Name == "get_reference_doc" {
						var input struct {
							Kind string `json:"kind"`
						}
						if err := json.Unmarshal(v.Input, &input); err != nil {
							t.Errorf("decode tool input: %v (raw=%s)", err, string(v.Input))
							continue
						}
						calledKind = input.Kind
						if input.Kind == tc.expectKind {
							foundRefDocCall = true
						}
					}
				case anthropic.TextBlock:
					// For debugging — show what the model said before/instead of the tool call.
					if v.Text != "" {
						t.Logf("model text: %s", truncateRefdocLog(v.Text, 240))
					}
				}
			}

			if !foundRefDocCall {
				t.Errorf("expected get_reference_doc(kind=%q) before performing action; calledKind=%q stop_reason=%q",
					tc.expectKind, calledKind, msg.StopReason)
				t.Logf("Full response blocks: %s", dumpBlocks(msg.Content))
			} else {
				t.Logf("✅ agent called get_reference_doc(kind=%q) before action", tc.expectKind)
			}
		})
	}
}

// truncateRefdocLog shortens a long string for test log lines so the failure
// message stays readable.
func truncateRefdocLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// dumpBlocks renders message content blocks compactly for debugging.
func dumpBlocks(blocks []anthropic.ContentBlockUnion) string {
	var sb strings.Builder
	for i, b := range blocks {
		switch v := b.AsAny().(type) {
		case anthropic.ToolUseBlock:
			sb.WriteString(fmt.Sprintf("[%d] tool_use name=%q input=%s\n", i, v.Name, string(v.Input)))
		case anthropic.TextBlock:
			sb.WriteString(fmt.Sprintf("[%d] text=%q\n", i, truncateRefdocLog(v.Text, 200)))
		default:
			sb.WriteString(fmt.Sprintf("[%d] %T\n", i, v))
		}
	}
	return sb.String()
}

// TestMultiAgentChatPromptSteersToReferenceDocs_ClaudeCode is the local-CLI
// variant of the refdoc steering test. It uses the Claude Code CLI adapter
// (which routes through the user's local `claude` binary and consumes the
// user's Claude subscription — no ANTHROPIC_API_KEY needed).
//
// Unlike the Anthropic SDK variant, Claude Code CLI does its tool calling
// via MCP servers configured externally; the adapter does NOT accept
// inline tool definitions. So this test can't verify a literal tool_use
// block was emitted. Instead it verifies the **model's stated intent** —
// does the text response mention calling `get_reference_doc(kind="...")`
// with the expected kind? That proves the inline cheat-sheet pointer is
// strong enough to steer the LLM's plan, which is what we care about.
//
// Gating:
//   - RUN_MULTIAGENT_REFDOC_CC_E2E=1 to run.
//   - `claude` CLI binary must be on PATH.
//   - CLAUDE_CODE_REFDOC_MODEL override (default: claude-haiku-4-5-20251001).
//
// Cost: uses the user's local Claude subscription, not API credits.
func TestMultiAgentChatPromptSteersToReferenceDocs_ClaudeCode(t *testing.T) {
	if os.Getenv("RUN_MULTIAGENT_REFDOC_CC_E2E") == "" {
		t.Skip("set RUN_MULTIAGENT_REFDOC_CC_E2E=1 to run this claude-code CLI e2e")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude binary not found on PATH: %v", err)
	}
	model := strings.TrimSpace(os.Getenv("CLAUDE_CODE_REFDOC_MODEL"))
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	systemPrompt := virtualtools.GetMultiAgentDelegationInstructionsWithUser("Chats", "default")
	adapter := claudecodeadapter.NewClaudeCodeExperimentalAdapter(model, &e2eMockLogger{})
	t.Cleanup(func() {
		_ = claudecodeadapter.CleanupClaudeCodeExperimentalSessions(context.Background())
	})

	type caseSpec struct {
		name         string
		userMsg      string
		expectKind   string
		mustMention  []string // additional phrases that should appear in the response
	}
	cases := []caseSpec{
		{
			name:    "schedule_request_describes_schedule_doc_load",
			userMsg: "I want to schedule a multi-agent task that runs every weekday at 9 AM. Walk me through exactly what tools you would call, in order, to set this up. Be specific about tool names and arguments.",
			expectKind: "schedule-management",
			mustMention: []string{"get_reference_doc"},
		},
		{
			name:    "secret_storage_describes_secret_doc_load",
			userMsg: "I want to save a Slack API token as SLACK_TOKEN. Walk me through exactly what tools you would call, in order, to store it correctly. Be specific about tool names and arguments.",
			expectKind: "secret-management",
			mustMention: []string{"get_reference_doc"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer cancel()

			resp, err := adapter.GenerateContent(ctx, []llmtypes.MessageContent{
				{Role: llmtypes.ChatMessageTypeSystem, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: systemPrompt}}},
				{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: tc.userMsg}}},
			})
			if err != nil {
				t.Fatalf("claude-code GenerateContent: %v", err)
			}
			if len(resp.Choices) == 0 {
				t.Fatal("no choices in response")
			}
			text := resp.Choices[0].Content
			t.Logf("model response (%d chars):\n%s", len(text), truncateRefdocLog(text, 1200))

			// Stated-intent check: does the response mention calling
			// get_reference_doc with the expected kind? Allow common
			// formatting variations (kind=..., kind: ..., kind:"...").
			lower := strings.ToLower(text)
			kindMentioned := strings.Contains(lower, strings.ToLower(tc.expectKind))
			refDocMentioned := strings.Contains(lower, "get_reference_doc")
			if !refDocMentioned {
				t.Errorf("expected response to mention get_reference_doc; got text=%s", truncateRefdocLog(text, 600))
			}
			if !kindMentioned {
				t.Errorf("expected response to mention kind %q; got text=%s", tc.expectKind, truncateRefdocLog(text, 600))
			}
			for _, phrase := range tc.mustMention {
				if !strings.Contains(lower, strings.ToLower(phrase)) {
					t.Errorf("expected response to mention %q; got text=%s", phrase, truncateRefdocLog(text, 600))
				}
			}
			if refDocMentioned && kindMentioned {
				t.Logf("✅ model stated intent to call get_reference_doc(kind=%q) before action", tc.expectKind)
			}
		})
	}
}
