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

// TestMultiAgentChatFullConversation_ClaudeCode is a single multi-turn
// conversation that exercises every capability the multi-agent chat prompt
// is supposed to surface: schedule management, secret management, memory
// save/recall, employees + workflow assignments context, and workflow
// context inspection. The whole flow runs in ONE conversation (the adapter
// threads claude-code's native session via NativeSessionID), so each turn
// gets to depend on prior context if the LLM is steered correctly.
//
// What this proves end-to-end:
//   - The full assembled system prompt (delegation rules + memory
//     instructions + synthetic employees/workflow context) holds together
//     and steers the LLM on every relevant axis.
//   - get_reference_doc pointers actually steer the model on schedule and
//     secret asks (same as the per-capability tests above, but verified in
//     a real session not just a one-shot).
//   - Memory tool surface (save_memory / recall_memory) shows up in the
//     model's plan when the user explicitly asks to remember / recall.
//   - Auto-injected employees context lets the model resolve "who handles
//     X workflow?" by name without re-asking the user.
//   - Auto-injected workflow context lets the model describe a workflow's
//     purpose / structure without inventing details.
//
// Gating:
//   - RUN_MULTIAGENT_REFDOC_CC_E2E=1 (shared with the per-capability test).
//   - `claude` binary on PATH.
//
// Cost: uses Claude subscription, not API credits.
func TestMultiAgentChatFullConversation_ClaudeCode(t *testing.T) {
	if os.Getenv("RUN_MULTIAGENT_REFDOC_CC_E2E") == "" {
		t.Skip("set RUN_MULTIAGENT_REFDOC_CC_E2E=1 to run this claude-code multi-turn e2e")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude binary not found on PATH: %v", err)
	}
	model := strings.TrimSpace(os.Getenv("CLAUDE_CODE_REFDOC_MODEL"))
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	// Build the full system prompt the chat session would normally see.
	// Delegation prompt: real (cheat sheets + pointers).
	// Memory prompt: real.
	// Employees + workflow context: synthetic — production reads files
	// at request time, but for an isolated e2e test we inject fixed
	// content with distinctive names/tokens we can grep for.
	delegationPrompt := virtualtools.GetMultiAgentDelegationInstructionsWithUser("Chats", "default")
	memoryPrompt := virtualtools.GetMemoryInstructions("_users/default/memory")
	const employeeName = "Priya"
	const employeeWorkflow = "Workflow/bot-whatsapp-customer-support"
	const workflowDescription = "an automated customer-support bot that triages incoming WhatsApp messages, classifies intent, and routes to the right handler step (refund, escalation, FAQ-bot, human-agent)"
	employeesSection := `
## Current Employees & Workflow Assignments

This workspace has the following employees with their assigned workflows. If the user's message names any employee below, treat that employee's assigned workflows as the primary source of truth and inspect the relevant workflow folder to ground your answer.

- **` + employeeName + `** (` + "`emp-001`" + `)
  - ` + "`" + employeeWorkflow + "`" + `
- **Arjun** (` + "`emp-002`" + `)
  - ` + "`Workflow/data-ingestion-pipeline`" + `
`
	workflowContextSection := `
## Workflow Context (Read-Only)

The following workflow(s) have been selected as reference context for this conversation. You have read-only access — read files, list directories, but cannot modify.

### Workflow: bot-whatsapp-customer-support
**Workspace Path:** ` + "`" + employeeWorkflow + "/`" + `

**Workflow Manifest (workflow.json):**
This workflow is ` + workflowDescription + `. It runs continuously and updates ` + "`db/whatsapp_tickets.json`" + ` with each handled message. Owner: ` + employeeName + `.

**Key Steps:**
- step-ingest-whatsapp: pulls new WhatsApp messages from the inbox
- step-classify-intent: LLM classifies each message into one of (refund, escalation, faq, human-handoff)
- step-route-handler: dispatches each message to the appropriate sub-agent
`
	systemPrompt := delegationPrompt + employeesSection + workflowContextSection + memoryPrompt

	adapter := claudecodeadapter.NewClaudeCodeExperimentalAdapter(model, &e2eMockLogger{})
	t.Cleanup(func() {
		_ = claudecodeadapter.CleanupClaudeCodeExperimentalSessions(context.Background())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	// Running conversation history. The system prompt is set on the first
	// message; subsequent turns inherit it via the claude-code session.
	history := []llmtypes.MessageContent{
		{Role: llmtypes.ChatMessageTypeSystem, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: systemPrompt}}},
	}

	type turn struct {
		name          string
		userMsg       string
		mustMentionAny [][]string // groups: response must contain at least one phrase from EACH group
		mustNotMention []string   // response must NOT contain these (red flags)
	}
	turns := []turn{
		{
			name:    "1_schedule_request",
			userMsg: "Hi. I want to schedule a multi-agent task that runs every weekday at 9:00 AM. Walk me through exactly what tools you would call, in order, to set this up.",
			mustMentionAny: [][]string{
				{"get_reference_doc"},
				{"schedule-management"},
			},
		},
		{
			name:    "2_secret_storage",
			userMsg: "Different topic — I also want to save a Slack API token as SLACK_TOKEN. What tool do you call first, before doing anything else?",
			mustMentionAny: [][]string{
				{"get_reference_doc"},
				{"secret-management"},
			},
		},
		{
			name:    "3_memory_save",
			userMsg: "Please remember for future sessions: I prefer all my notifications routed to the Slack #ops channel rather than email. Save that as a preference.",
			mustMentionAny: [][]string{
				{"save_memory"},
			},
		},
		{
			name:    "4_memory_recall",
			userMsg: "What preference did I ask you to save earlier in this conversation? If you need to, recall it from memory.",
			mustMentionAny: [][]string{
				// Either it remembers from session context OR it states intent to recall from memory.
				{"slack", "#ops", "recall_memory", "notifications"},
			},
		},
		{
			name:    "5_employees_lookup",
			userMsg: "Who handles the bot-whatsapp-customer-support workflow? Use the employee context you already have — don't ask me, just answer from what's loaded.",
			mustMentionAny: [][]string{
				// Either name or workflow path. Should NOT invent another name.
				{strings.ToLower(employeeName)},
			},
			mustNotMention: []string{
				"I don't know", "I don't have", "cannot find", "no information",
			},
		},
		{
			name:    "6_workflow_context_inspect",
			userMsg: "Briefly describe what the bot-whatsapp-customer-support workflow does, based on the workflow context loaded in this session. Don't make anything up — only use what's already in the system prompt.",
			mustMentionAny: [][]string{
				// Should reference the distinctive content from the synthetic workflow context.
				{"whatsapp", "customer-support", "triage", "refund", "escalation", "faq"},
			},
		},
	}

	successCount := 0
	for i, turn := range turns {
		turnNum := i + 1
		t.Run(turn.name, func(t *testing.T) {
			history = append(history, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: turn.userMsg}},
			})

			t.Logf("→ turn %d: %s", turnNum, truncateRefdocLog(turn.userMsg, 200))
			t0 := time.Now()
			resp, err := adapter.GenerateContent(ctx, history)
			elapsed := time.Since(t0)
			if err != nil {
				t.Fatalf("turn %d GenerateContent: %v", turnNum, err)
			}
			if len(resp.Choices) == 0 {
				t.Fatalf("turn %d: no choices", turnNum)
			}
			text := resp.Choices[0].Content
			t.Logf("← turn %d (%v): %s", turnNum, elapsed, truncateRefdocLog(text, 800))

			// Append assistant response to history so subsequent turns
			// see prior context.
			history = append(history, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeAI,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: text}},
			})

			lower := strings.ToLower(text)
			passed := true
			for _, group := range turn.mustMentionAny {
				hit := false
				for _, phrase := range group {
					if strings.Contains(lower, strings.ToLower(phrase)) {
						hit = true
						break
					}
				}
				if !hit {
					t.Errorf("turn %d (%s): response missing any of %v\n  response: %s", turnNum, turn.name, group, truncateRefdocLog(text, 500))
					passed = false
				}
			}
			for _, bad := range turn.mustNotMention {
				if strings.Contains(lower, strings.ToLower(bad)) {
					t.Errorf("turn %d (%s): response contains forbidden phrase %q\n  response: %s", turnNum, turn.name, bad, truncateRefdocLog(text, 500))
					passed = false
				}
			}
			if passed {
				successCount++
				t.Logf("✅ turn %d passed", turnNum)
			}
		})
	}

	t.Logf("Final: %d/%d turns passed", successCount, len(turns))
}
