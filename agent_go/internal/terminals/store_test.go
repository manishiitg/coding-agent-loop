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
						"step_type":     "todo_task",
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
	if snapshot.StepType != "todo_task" {
		t.Fatalf("unexpected step type: %q", snapshot.StepType)
	}
	if snapshot.DisplayTitle != "Rtsrca -> step-sentry-evidence" {
		t.Fatalf("unexpected display title: %q", snapshot.DisplayTitle)
	}
	if snapshot.DisplayMeta != "Todo task · Workflow step" {
		t.Fatalf("unexpected display meta: %q", snapshot.DisplayMeta)
	}
	if snapshot.Status.StatusText == "" {
		t.Fatalf("expected derived status text")
	}
}

func TestStoreCollapsesSnapshotsForSameTmuxSession(t *testing.T) {
	store := NewStore()
	now := time.Now()

	store.HandleEvent("session-1", terminalEventWithMetadata(
		"workflow-step:workflow-full-1:route-pick-topic",
		"route topic screen",
		1,
		map[string]interface{}{
			"tmux_session":    "shared-pane",
			"current_step_id": "route-pick-topic",
			"execution_kind":  "workflow_step",
			"scope":           "workflow_step",
			"workflow_path":   "Workflow/instagram",
		},
		now,
	))
	store.HandleEvent("session-1", terminalEventWithMetadata(
		"workflow-step:workflow-full-1:step-create-reel",
		"create reel screen",
		2,
		map[string]interface{}{
			"tmux_session":    "shared-pane",
			"current_step_id": "route-pick-topic",
			"execution_kind":  "workflow_step",
			"scope":           "workflow_step",
			"workflow_path":   "Workflow/instagram",
		},
		now.Add(time.Second),
	))

	snapshots := store.List("session-1")
	if len(snapshots) != 1 {
		t.Fatalf("expected one terminal snapshot for one tmux pane, got %d", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.TerminalID != "session-1:workflow-step:workflow-full-1:step-create-reel" {
		t.Fatalf("unexpected terminal id: %s", snapshot.TerminalID)
	}
	if snapshot.Content != "create reel screen" {
		t.Fatalf("unexpected content: %q", snapshot.Content)
	}
	if snapshot.StepID != "step-create-reel" {
		t.Fatalf("step id should come from workflow-step owner, got %q", snapshot.StepID)
	}
	if _, ok := store.Get("session-1:workflow-step:workflow-full-1:route-pick-topic"); ok {
		t.Fatalf("old terminal alias for the same tmux pane should be removed")
	}
}

func TestStorePrefersWorkflowStepOwnerOverStaleCurrentStepMetadata(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", terminalEventWithMetadata(
		"workflow-step:workflow-full-1:step-create-reel",
		"screen",
		1,
		map[string]interface{}{
			"current_step_id": "route-pick-topic",
			"execution_kind":  "workflow_step",
			"scope":           "workflow_step",
			"workflow_path":   "Workflow/instagram",
		},
		time.Time{},
	))

	snapshot, ok := store.Get("session-1:workflow-step:workflow-full-1:step-create-reel")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.StepID != "step-create-reel" {
		t.Fatalf("step id = %q, want step-create-reel", snapshot.StepID)
	}
	if snapshot.DisplayTitle != "Instagram -> step-create-reel" {
		t.Fatalf("display title = %q", snapshot.DisplayTitle)
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

func TestSnapshotWithContextKeepsWorkflowStepIDInTitle(t *testing.T) {
	snapshot := Snapshot{
		TerminalID:    "session-1:workflow-step:run:bid-submit",
		SessionID:     "session-1",
		OwnerID:       "workflow-step:run:bid-submit",
		ExecutionKind: "workflow_step",
		StepID:        "bid-submit",
		Scope:         "workflow_step",
	}

	enriched := snapshot.WithContext(Context{
		WorkflowLabel: "upwork",
		WorkspacePath: "Workflow/upwork",
		ExecutionName: "full-workflow [daily-bid / iteration-0]",
	})

	if enriched.DisplayTitle != "upwork -> bid-submit" {
		t.Fatalf("display title = %q", enriched.DisplayTitle)
	}
	if enriched.StepName != "" {
		t.Fatalf("step name should not be filled from execution name when step id exists, got %q", enriched.StepName)
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

func TestStorePrunesExpiredClosingTerminalsOnListAndGet(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", "screen", 1))
	store.HandleEvent("session-1", terminalEndEvent("exec-1", map[string]interface{}{"terminal_retention_seconds": 300}))

	terminalID := "session-1:exec-1"
	expiredAt := time.Now().Add(-time.Second)
	store.mu.Lock()
	snapshot := store.byID[terminalID]
	snapshot.ClosesAt = &expiredAt
	store.byID[terminalID] = snapshot
	store.mu.Unlock()

	if _, ok := store.Get(terminalID); ok {
		t.Fatalf("expired closing terminal should be pruned on Get")
	}
	if snapshots := store.List("session-1"); len(snapshots) != 0 {
		t.Fatalf("expired closing terminal should be pruned on List, got %d", len(snapshots))
	}
}

func TestStoreDetectsFailedTerminalState(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", "LLM Generation Error\nSTATUS: FAILED", 1))
	store.HandleEvent("session-1", terminalEndEvent("exec-1", nil))

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.State != "failed" {
		t.Fatalf("state = %q, want failed", snapshot.State)
	}
}

func TestStoreKeepsActiveTerminalRunningWhenScreenContainsFailureText(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", "Checking if the browser failed to start...\n• Working (2m • esc to interrupt)", 1))

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.State != "running" {
		t.Fatalf("state = %q, want running", snapshot.State)
	}
}

func TestStoreDetectsGeminiDebugConsoleFatalState(t *testing.T) {
	store := NewStore()
	screen := `▝▜▄ Gemini CLI v0.42.0
Debug Console (F12 to close)
This is an unexpected error. Please file a bug report.
CRITICAL: Unhandled Promise Rejection!
Reason: Error: ENAMETOOLONG: name too long, lstat
'/private/var/folders/xc/gemini-cli-project-session-sub-exec-route-design-plan/fixyo.urflow", TOP LEVEL (REQUIRED)'`
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", screen, 1))

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.State != "failed" {
		t.Fatalf("active fatal pane state = %q, want failed", snapshot.State)
	}

	store.HandleEvent("session-1", terminalEndEvent("exec-1", nil))
	snapshot, ok = store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot after end")
	}
	if snapshot.Active {
		t.Fatalf("fatal terminal should not remain active")
	}
	if snapshot.State != "failed" {
		t.Fatalf("ended fatal pane state = %q, want failed", snapshot.State)
	}
}

func TestStoreKeepsTerminalRunningWhenEndArrivesDuringBusyPane(t *testing.T) {
	store := NewStore()
	screen := `╭──────────────────────────────────────────────────────────────────────────╮
│ ✓ agent_browser (api-bridge MCP Server) {"action":"wait","timeout":10} │
╰──────────────────────────────────────────────────────────────────────────╯
ERROR: Custom tool execution failed: command timed out after 2m0s

⊷ agent_browser (api-bridge MCP Server) {"action":"wait","timeout":10}

⠇ Thinking... (esc to cancel, 14m 30s)
──────────────────────────────────────────────────────────────────────────────
 >   Type your message or @path/to/file
workspace (/directory)                         sandbox                  /model
/tmp/project                                   no sandbox       Auto (Gemini 3)`
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", screen, 1))
	store.HandleEvent("session-1", terminalEndEvent("exec-1", nil))

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if !snapshot.Active {
		t.Fatalf("terminal should remain active while pane is still thinking")
	}
	if snapshot.State != "running" {
		t.Fatalf("state = %q, want running", snapshot.State)
	}
}

func TestStoreCompletedStatusWinsOverPreviousFailureText(t *testing.T) {
	store := NewStore()
	screen := "PRE-VALIDATION FAILED on retry 1\nRecovered and wrote cdp_status.json\nSTATUS: COMPLETED"
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", screen, 1))
	store.HandleEvent("session-1", terminalEndEvent("exec-1", nil))

	snapshot, ok := store.Get("session-1:exec-1")
	if !ok {
		t.Fatalf("expected terminal snapshot")
	}
	if snapshot.State != "completed" {
		t.Fatalf("state = %q, want completed", snapshot.State)
	}
}

func TestStoreDismissRemovesTerminalAndSuppressesFutureChunks(t *testing.T) {
	store := NewStore()
	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", "screen one", 1))

	if !store.Dismiss("session-1:exec-1") {
		t.Fatalf("expected dismiss to remove terminal")
	}
	if _, ok := store.Get("session-1:exec-1"); ok {
		t.Fatalf("terminal should be removed after dismiss")
	}

	store.HandleEvent("session-1", terminalEvent("streaming_chunk", "exec-1", "screen two", 2))
	if _, ok := store.Get("session-1:exec-1"); ok {
		t.Fatalf("dismissed terminal should not be recreated by future chunks")
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
	return terminalEventWithMetadataAt(eventType, executionID, content, chunkIndex, nil, timestamp)
}

func terminalEventWithMetadata(executionID, content string, chunkIndex int, metadata map[string]interface{}, timestamp time.Time) storeevents.Event {
	return terminalEventWithMetadataAt("streaming_chunk", executionID, content, chunkIndex, metadata, timestamp)
}

func terminalEventWithMetadataAt(eventType, executionID, content string, chunkIndex int, metadata map[string]interface{}, timestamp time.Time) storeevents.Event {
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metadata["kind"] = "terminal"
	return storeevents.Event{
		Type:        eventType,
		Timestamp:   timestamp,
		SessionID:   "session-1",
		ExecutionID: executionID,
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingChunk,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: metadata,
				},
				Content:    content,
				ChunkIndex: chunkIndex,
			},
		},
	}
}

func terminalEndEvent(executionID string, metadata map[string]interface{}) storeevents.Event {
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metadata["kind"] = "terminal"
	return storeevents.Event{
		Type:        "streaming_end",
		SessionID:   "session-1",
		ExecutionID: executionID,
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingEnd,
			Data: &agentevents.StreamingEndEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: metadata,
				},
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

func TestDeriveStatusPreservesMultilineAssistantPreview(t *testing.T) {
	screen := `▝▜▄ Gemini CLI v0.42.0
✦ Here is what I found:
- First useful detail
- Second useful detail
- Third useful detail

╭────────────────────────────╮
│ ✓ execute_shell_command (api-bridge MCP Server) {"command":"pwd"} │
╰────────────────────────────╯
? for shortcuts`

	status := DeriveStatus(screen, map[string]interface{}{"provider": "gemini-cli"})
	want := "Here is what I found:\n- First useful detail\n- Second useful detail\n- Third useful detail"
	if status.AssistantPreview != want {
		t.Fatalf("assistant preview = %q, want %q", status.AssistantPreview, want)
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
