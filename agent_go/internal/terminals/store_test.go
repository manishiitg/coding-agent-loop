package terminals

import (
	"testing"
	"time"

	storeevents "mcp-agent-builder-go/agent_go/internal/events"

	agentevents "github.com/manishiitg/mcpagent/events"
)

func TestStoreTracksTerminalChunksByOwner(t *testing.T) {
	store := NewStore()
	now := time.Now()

	store.HandleEvent("session-1", storeevents.Event{
		Type:          "streaming_chunk",
		Timestamp:     now,
		SessionID:     "session-1",
		ExecutionID:   "exec-1",
		ExecutionKind: "workflow_step",
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingChunk,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: map[string]interface{}{
						"kind":          "terminal",
						"step_id":       "step-sentry-evidence",
						"step_title":    "Pull Sentry Evidence",
						"workflow_path": "Workflow/rtsrca",
					},
				},
				Content:    "screen one",
				ChunkIndex: 3,
			},
		},
	})

	snapshots := store.List("session-1")
	if len(snapshots) != 1 {
		t.Fatalf("expected one terminal snapshot, got %d", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.TerminalID != "session-1:exec-1" {
		t.Fatalf("unexpected terminal id: %s", snapshot.TerminalID)
	}
	if snapshot.Content != "screen one" {
		t.Fatalf("unexpected content: %q", snapshot.Content)
	}
	if !snapshot.Active {
		t.Fatalf("expected terminal to be active")
	}
	if snapshot.State != "running" {
		t.Fatalf("unexpected state: %q", snapshot.State)
	}
	if snapshot.Label != "Pull Sentry Evidence" {
		t.Fatalf("unexpected label: %q", snapshot.Label)
	}
	if snapshot.Scope != "workflow_step" {
		t.Fatalf("unexpected scope: %q", snapshot.Scope)
	}
	if snapshot.WorkflowName != "Rtsrca" {
		t.Fatalf("unexpected workflow name: %q", snapshot.WorkflowName)
	}
	if snapshot.StepID != "step-sentry-evidence" {
		t.Fatalf("unexpected step id: %q", snapshot.StepID)
	}
	if snapshot.StepName != "Pull Sentry Evidence" {
		t.Fatalf("unexpected step name: %q", snapshot.StepName)
	}
	if snapshot.DisplayTitle != "Rtsrca -> Pull Sentry Evidence" {
		t.Fatalf("unexpected display title: %q", snapshot.DisplayTitle)
	}
	if snapshot.Status.StatusText == "" {
		t.Fatalf("expected derived status text")
	}
}

func TestSnapshotWithContextFillsReadableDisplay(t *testing.T) {
	snapshot := Snapshot{
		TerminalID:    "session-1:main:7f6d",
		SessionID:     "session-1",
		OwnerID:       "main:7f6d",
		ExecutionKind: "main_agent",
		Label:         "main:7f6d",
		Scope:         "execution",
		Status:        Status{ToolSummary: "api-bridge x2"},
	}

	enriched := snapshot.WithContext(Context{
		WorkflowLabel: "Upwork",
		WorkspacePath: "Workflow/upwork",
		ExecutionName: "Workflow builder",
	})

	if enriched.DisplayTitle != "Upwork -> Workflow builder" {
		t.Fatalf("display title = %q", enriched.DisplayTitle)
	}
	if enriched.DisplayMeta != "Main agent" {
		t.Fatalf("display meta = %q", enriched.DisplayMeta)
	}
	if enriched.WorkflowName != "Upwork" {
		t.Fatalf("workflow name = %q", enriched.WorkflowName)
	}
	if enriched.AgentName != "Workflow builder" {
		t.Fatalf("agent name = %q", enriched.AgentName)
	}
}

func TestStoreIgnoresOlderTerminalChunks(t *testing.T) {
	store := NewStore()

	for _, item := range []struct {
		index   int
		content string
	}{
		{index: 5, content: "new screen"},
		{index: 4, content: "old screen"},
	} {
		store.HandleEvent("session-1", storeevents.Event{
			Type:        "streaming_chunk",
			SessionID:   "session-1",
			ExecutionID: "exec-1",
			Data: &agentevents.AgentEvent{
				Type: agentevents.StreamingChunk,
				Data: &agentevents.StreamingChunkEvent{
					BaseEventData: agentevents.BaseEventData{
						Metadata: map[string]interface{}{"kind": "terminal"},
					},
					Content:    item.content,
					ChunkIndex: item.index,
				},
			},
		})
	}

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.Content != "new screen" {
		t.Fatalf("older chunk overwrote newer content: %q", snapshot.Content)
	}
}

func TestStoreAllowsChunkIndexResetForNewTurn(t *testing.T) {
	store := NewStore()
	now := time.Now()

	store.HandleEvent("session-1", terminalEventAt("streaming_chunk", "exec-1", "old turn", 12, now))
	store.HandleEvent("session-1", terminalEventAt("streaming_chunk", "exec-1", "new turn", 1, now.Add(time.Second)))

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.Content != "new turn" {
		t.Fatalf("new turn did not update terminal content: %q", snapshot.Content)
	}
	if snapshot.ChunkIndex != 1 {
		t.Fatalf("chunk index = %d, want 1", snapshot.ChunkIndex)
	}
}

func TestStoreMarksTerminalInactiveOnStreamingEnd(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", "screen", 1))
	store.HandleEvent("session-1", storeevents.Event{
		Type:        "streaming_end",
		SessionID:   "session-1",
		ExecutionID: "exec-1",
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingEnd,
			Data: &agentevents.StreamingEndEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: map[string]interface{}{"kind": "terminal"},
				},
			},
		},
	})

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.Active {
		t.Fatalf("expected terminal to be inactive")
	}
	if snapshot.State != "completed" {
		t.Fatalf("expected completed state, got %q", snapshot.State)
	}
}

func TestStoreMarksTerminalClosingWithRetention(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", "screen", 1))
	store.HandleEvent("session-1", storeevents.Event{
		Type:        "streaming_end",
		SessionID:   "session-1",
		ExecutionID: "exec-1",
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingEnd,
			Data: &agentevents.StreamingEndEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: map[string]interface{}{
						"kind":                       "terminal",
						"terminal_retention_seconds": 300,
					},
				},
			},
		},
	})

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.Active {
		t.Fatalf("expected terminal to be inactive")
	}
	if snapshot.State != "closing" {
		t.Fatalf("state = %q, want closing", snapshot.State)
	}
	if snapshot.RetentionSeconds != 300 {
		t.Fatalf("retention_seconds = %d, want 300", snapshot.RetentionSeconds)
	}
	if snapshot.ClosesAt == nil {
		t.Fatalf("expected closes_at")
	}
	if time.Until(*snapshot.ClosesAt) < 4*time.Minute {
		t.Fatalf("closes_at is too soon: %s", snapshot.ClosesAt.Format(time.RFC3339))
	}
}

func TestStoreDetectsFailedTerminalState(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", "LLM Generation Error\nSTATUS: FAILED", 1))

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.State != "failed" {
		t.Fatalf("state = %q, want failed", snapshot.State)
	}
}

func TestStoreIgnoresNonTerminalStreamingChunks(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", storeevents.Event{
		Type:        "streaming_chunk",
		SessionID:   "session-1",
		ExecutionID: "exec-1",
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingChunk,
			Data: &agentevents.StreamingChunkEvent{
				Content:    "regular assistant text",
				ChunkIndex: 1,
			},
		},
	})

	if got := store.List("session-1"); len(got) != 0 {
		t.Fatalf("expected no terminal snapshots, got %d", len(got))
	}
}

func terminalEvent(eventType, executionID, content string, chunkIndex int) storeevents.Event {
	return terminalEventAt(eventType, executionID, content, chunkIndex, time.Time{})
}

func terminalEventAt(eventType, executionID, content string, chunkIndex int, timestamp time.Time) storeevents.Event {
	return storeevents.Event{
		Type:        eventType,
		Timestamp:   timestamp,
		SessionID:   "session-1",
		ExecutionID: executionID,
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingChunk,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: map[string]interface{}{"kind": "terminal"},
				},
				Content:    content,
				ChunkIndex: chunkIndex,
			},
		},
	}
}

func TestDeriveStatusExtractsClaudeAssistantPreview(t *testing.T) {
	screen := `╭─── Claude Code v2.1.143 ───╮
❯ check the current state
  Calling api-bridge 2 times… (ctrl+o to expand)
⏺ Let me check the current state of saved jobs and then run the bidding workflow.

  Calling api-bridge 3 times… (ctrl+o to expand)
✻ Sautéed for 8s
❯`

	status := DeriveStatus(screen, map[string]interface{}{"provider": "claude-code"})
	if status.ProviderLabel != "Claude Code" {
		t.Fatalf("provider = %q", status.ProviderLabel)
	}
	if status.AssistantPreview != "Let me check the current state of saved jobs and then run the bidding workflow." {
		t.Fatalf("assistant preview = %q", status.AssistantPreview)
	}
	if status.ToolSummary != "api-bridge x3" {
		t.Fatalf("tool summary = %q", status.ToolSummary)
	}
}

func TestDeriveStatusExtractsGeminiAssistantPreview(t *testing.T) {
	screen := `▝▜▄ Gemini CLI v0.42.0
╭────────────────────────────╮
│ ✓ execute_shell_command (api-bridge MCP Server) {"command":"pwd"} │
╰────────────────────────────╯
✦ My current working directory is /Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs.
? for shortcuts
Shift+Tab to accept edits`

	status := DeriveStatus(screen, map[string]interface{}{"provider": "gemini-cli"})
	if status.ProviderLabel != "Gemini CLI" {
		t.Fatalf("provider = %q", status.ProviderLabel)
	}
	if status.AssistantPreview != "My current working directory is /Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs." {
		t.Fatalf("assistant preview = %q", status.AssistantPreview)
	}
	if status.ToolSummary != "execute_shell_command" {
		t.Fatalf("tool summary = %q", status.ToolSummary)
	}
}

func TestDeriveStatusKeepsCodexConservative(t *testing.T) {
	screen := `╭────────────────────────────╮
│ >_ OpenAI Codex (v0.130.0) │
╰────────────────────────────╯
• Called codex.list_mcp_resources({"cursor":""})
  └ {"resources": []}
• Working (6m 32s • esc to interrupt)
`

	status := DeriveStatus(screen, map[string]interface{}{"provider": "codex-cli"})
	if status.ProviderLabel != "Codex CLI" {
		t.Fatalf("provider = %q", status.ProviderLabel)
	}
	if status.AssistantPreview != "" {
		t.Fatalf("codex assistant preview should stay empty when ambiguous, got %q", status.AssistantPreview)
	}
	if status.StatusText != "Codex CLI is working" {
		t.Fatalf("status text = %q", status.StatusText)
	}
	if status.ToolSummary != "codex.list_mcp_resources" {
		t.Fatalf("tool summary = %q", status.ToolSummary)
	}
}
