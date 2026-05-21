package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	agentevents "github.com/manishiitg/mcpagent/events"

	storeevents "mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/internal/terminals"
)

func TestTerminalRoutesCloseAndDismissMismatchedOwnerTerminal(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-e2e"
	tmuxSession := "mlp-claude-code-exp-test"

	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "⏺ Review complete\n\n✻ Cogitated for 4m 37s\n❯", 12))

	before := terminalRouteList(t, api, sessionID)
	if len(before.Terminals) != 1 {
		t.Fatalf("before terminal count = %d, want 1", len(before.Terminals))
	}
	terminalID := before.Terminals[0].TerminalID
	if terminalID != sessionID+":workflow-step:review-plan" {
		t.Fatalf("terminal id = %q, want chunk owner terminal id", terminalID)
	}
	if !before.Terminals[0].Active || before.Terminals[0].State != "running" {
		t.Fatalf("before terminal state = active:%v state:%q, want active running", before.Terminals[0].Active, before.Terminals[0].State)
	}

	store.HandleEvent(sessionID, terminalRouteEndEvent(sessionID, "review-plan", tmuxSession, 300))

	after := terminalRouteList(t, api, sessionID)
	if len(after.Terminals) != 1 {
		t.Fatalf("after terminal count = %d, want 1", len(after.Terminals))
	}
	if after.Terminals[0].TerminalID != terminalID {
		t.Fatalf("terminal id changed after end event: got %q want %q", after.Terminals[0].TerminalID, terminalID)
	}
	if after.Terminals[0].Active {
		t.Fatalf("expected terminal to be inactive after mismatched-owner end event")
	}
	if after.Terminals[0].State != "closing" {
		t.Fatalf("terminal state = %q, want closing", after.Terminals[0].State)
	}
	if after.Terminals[0].RetentionSeconds != 300 || after.Terminals[0].ClosesAt == nil {
		t.Fatalf("retention = %d closes_at=%v, want 300 and closes_at", after.Terminals[0].RetentionSeconds, after.Terminals[0].ClosesAt)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/terminals/"+terminalID, nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleDismissTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}

	removed := terminalRouteList(t, api, sessionID)
	if len(removed.Terminals) != 0 {
		t.Fatalf("terminal should be dismissed, got %d", len(removed.Terminals))
	}
}

func TestTerminalRoutesListCapsContentButGetKeepsFullContent(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-large"
	terminalID := sessionID + ":workflow-step:review-plan"
	tmuxSession := "mlp-gemini-cli-int-test"
	tailMarker := "latest terminal tail marker"
	fullContent := strings.Repeat("old terminal output line\n", 5000) + tailMarker

	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, fullContent, 44))

	listed := terminalRouteList(t, api, sessionID)
	if len(listed.Terminals) != 1 {
		t.Fatalf("listed terminal count = %d, want 1", len(listed.Terminals))
	}
	listContent := listed.Terminals[0].Content
	if len(listContent) > listTerminalContentMaxBytes+128 {
		t.Fatalf("list content len = %d, want capped near %d", len(listContent), listTerminalContentMaxBytes)
	}
	if !strings.Contains(listContent, tailMarker) {
		t.Fatalf("list content should keep latest terminal output tail")
	}
	if !strings.Contains(listContent, "truncated") {
		t.Fatalf("list content should indicate truncation")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/terminals/"+terminalID, nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleGetTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var full terminals.Snapshot
	if err := json.NewDecoder(rec.Body).Decode(&full); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if full.Content != fullContent {
		t.Fatalf("get content len = %d, want full len %d", len(full.Content), len(fullContent))
	}
}

func TestTerminalRoutesListCanReturnMetadataOnly(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-metadata"
	tmuxSession := "mlp-claude-code-exp-test"

	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "large pane content", 7))

	req := httptest.NewRequest(http.MethodGet, "/api/terminals?session_id="+sessionID+"&content=none", nil)
	rec := httptest.NewRecorder()
	api.handleListTerminals(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var response listTerminalsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(response.Terminals) != 1 {
		t.Fatalf("terminal count = %d, want 1", len(response.Terminals))
	}
	if response.Terminals[0].Content != "" {
		t.Fatalf("metadata-only list content = %q, want empty", response.Terminals[0].Content)
	}
	if response.Terminals[0].ChunkIndex != 7 {
		t.Fatalf("chunk index = %d, want metadata preserved", response.Terminals[0].ChunkIndex)
	}
}

func TestTerminalRoutesStructuredWorkflowSnapshotIncludesToolEvents(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-structured"
	ownerID := "workflow-step:workflow-full-1:check-cdp"
	metadata := map[string]interface{}{
		"kind":               "terminal",
		"execution_owner_id": ownerID,
		"execution_kind":     "workflow_step",
		"scope":              "workflow_step",
		"workflow_path":      "Workflow/upwork",
		"workflow_name":      "upwork",
		"current_step_id":    "check-cdp",
		"step_name":          "Check CDP connection",
		"step_index":         1,
		"step_total":         28,
		"step_transport":     "structured",
		"provider":           "gemini-cli",
	}

	store.HandleEvent(sessionID, terminalRouteStructuredChunkEvent(
		sessionID,
		ownerID,
		"$ gemini --output-format stream-json model=auto msgs=2\n> user: Verify CDP",
		1,
		metadata,
	))
	store.HandleEvent(sessionID, terminalRouteToolStartEvent(
		sessionID,
		ownerID,
		"call-1",
		"mcp_api-bridge_execute_shell_command",
		`{"command":"env | grep MCP_API_TOKEN"}`,
		metadata,
	))
	store.HandleEvent(sessionID, terminalRouteToolEndEvent(
		sessionID,
		ownerID,
		"call-1",
		"mcp_api-bridge_execute_shell_command",
		`{"stdout":"MCP_API_TOKEN=secret-token\nCDP status check successful. Version: Chrome/148.0.7778.169"}`,
		metadata,
	))
	store.HandleEvent(sessionID, terminalRouteStructuredChunkEvent(
		sessionID,
		ownerID,
		"$ gemini --output-format stream-json model=auto msgs=2\n> user: Verify CDP\n[done · 29.9s · 83242 in · 1240 out]",
		2,
		metadata,
	))

	req := httptest.NewRequest(http.MethodGet, "/api/terminals?session_id="+sessionID+"&content=full", nil)
	rec := httptest.NewRecorder()
	api.handleListTerminals(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var response listTerminalsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(response.Terminals) != 1 {
		t.Fatalf("terminal count = %d, want 1", len(response.Terminals))
	}
	terminal := response.Terminals[0]
	if terminal.TerminalID != sessionID+":"+ownerID {
		t.Fatalf("terminal id = %q, want %q", terminal.TerminalID, sessionID+":"+ownerID)
	}
	if terminal.StepTransport != "structured" {
		t.Fatalf("step transport = %q, want structured", terminal.StepTransport)
	}
	if terminal.Status.ProviderLabel != "Gemini CLI" {
		t.Fatalf("provider label = %q, want Gemini CLI", terminal.Status.ProviderLabel)
	}
	if !strings.Contains(terminal.Content, "$ gemini --output-format stream-json") {
		t.Fatalf("terminal content missing Gemini command:\n%s", terminal.Content)
	}
	if !strings.Contains(terminal.Content, "→ tool: mcp_api-bridge_execute_shell_command") {
		t.Fatalf("terminal content missing tool start row:\n%s", terminal.Content)
	}
	if !strings.Contains(terminal.Content, "✓ result mcp_api-bridge_execute_shell_command") {
		t.Fatalf("terminal content missing tool result row:\n%s", terminal.Content)
	}
	if strings.Contains(terminal.Content, "secret-token") {
		t.Fatalf("terminal content leaked MCP token:\n%s", terminal.Content)
	}
	if !strings.Contains(terminal.Content, "MCP_API_TOKEN=[redacted]") {
		t.Fatalf("terminal content missing redacted token marker:\n%s", terminal.Content)
	}
	toolIdx := strings.Index(terminal.Content, "→ tool:")
	doneIdx := strings.Index(terminal.Content, "[done")
	if toolIdx < 0 || doneIdx < 0 || toolIdx > doneIdx {
		t.Fatalf("tool rows should appear before done footer:\n%s", terminal.Content)
	}
}

func TestTerminalRoutesCompleteTerminalMarksSnapshotCompleted(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-complete"
	terminalID := sessionID + ":workflow-step:review-plan"

	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", "mlp-claude-test", "still working", 12))

	req := httptest.NewRequest(http.MethodPost, "/api/terminals/"+terminalID+"/complete", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleCompleteTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}

	var response terminalActionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode complete response: %v", err)
	}
	if !response.OK {
		t.Fatalf("response ok = false")
	}
	if response.Terminal.Active || response.Terminal.State != "completed" {
		t.Fatalf("terminal active/state = %v/%q, want false/completed", response.Terminal.Active, response.Terminal.State)
	}
}

func TestTerminalRoutesCompleteTerminalSignalsProviderWaitLoop(t *testing.T) {
	terminalStore := terminals.NewStore()
	sessionID := "session-terminal-complete-force"
	ownerID := "workflow-step:exec-review:review-plan"
	terminalID := sessionID + ":" + ownerID
	tmuxSession := "mlp-claude-test"
	api := &StreamingAPI{terminalStore: terminalStore}

	var forcedSession string
	oldForceComplete := forceCompleteCodingAgentTmuxSession
	forceCompleteCodingAgentTmuxSession = func(sessionName string) bool {
		forcedSession = sessionName
		return true
	}
	defer func() { forceCompleteCodingAgentTmuxSession = oldForceComplete }()

	terminalStore.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, ownerID, tmuxSession, "final answer\n❯", 12))

	req := httptest.NewRequest(http.MethodPost, "/api/terminals/"+terminalID+"/complete", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleCompleteTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}

	if forcedSession != tmuxSession {
		t.Fatalf("force-complete tmux session = %q, want %q", forcedSession, tmuxSession)
	}
}

func TestTerminalRoutesFailTerminalMarksSnapshotFailed(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-fail"
	terminalID := sessionID + ":workflow-step:review-plan"

	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", "mlp-claude-test", "still working", 12))

	req := httptest.NewRequest(http.MethodPost, "/api/terminals/"+terminalID+"/fail", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleFailTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fail status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}

	var response terminalActionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode fail response: %v", err)
	}
	if response.Terminal.Active || response.Terminal.State != "failed" {
		t.Fatalf("terminal active/state = %v/%q, want false/failed", response.Terminal.Active, response.Terminal.State)
	}
}

func TestTerminalRoutesRefreshTerminalCapturesTmuxPane(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-refresh"
	terminalID := sessionID + ":workflow-step:review-plan"
	tmuxSession := "mlp-codex-cli-int-test"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "old pane", 2))

	var gotArgs []string
	oldRunOutput := runTerminalTmuxOutputCommand
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		gotArgs = append([]string(nil), args...)
		return "fresh pane", nil
	}
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()

	req := httptest.NewRequest(http.MethodPost, "/api/terminals/"+terminalID+"/refresh", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleRefreshTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if got := strings.Join(gotArgs, " "); got != "capture-pane -p -t "+tmuxSession+" -S -2000" {
		t.Fatalf("tmux args = %q, want capture-pane", got)
	}
	var response terminalActionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if response.Terminal.Content != "fresh pane" {
		t.Fatalf("terminal content = %q, want refreshed content", response.Terminal.Content)
	}
}

func TestTerminalRoutesKillTerminalKillsTmuxAndMarksFailed(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-kill"
	terminalID := sessionID + ":workflow-step:review-plan"
	tmuxSession := "mlp-claude-code-exp-test"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "pane", 2))

	var gotArgs []string
	oldRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}
	defer func() { runTerminalTmuxCommand = oldRun }()

	req := httptest.NewRequest(http.MethodPost, "/api/terminals/"+terminalID+"/kill", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleKillTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("kill status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if got := strings.Join(gotArgs, " "); got != "kill-session -t "+tmuxSession {
		t.Fatalf("tmux args = %q, want kill-session", got)
	}
	var response terminalActionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode kill response: %v", err)
	}
	if response.Terminal.Active || response.Terminal.State != "failed" {
		t.Fatalf("terminal active/state = %v/%q, want false/failed", response.Terminal.Active, response.Terminal.State)
	}
}

func TestTerminalRoutesSendInputUsesTmuxPasteAndOptionalEnter(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-input"
	terminalID := sessionID + ":workflow-step:review-plan"
	tmuxSession := "mlp-gemini-cli-int-test"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "pane", 2))

	type call struct {
		stdin string
		args  []string
	}
	var calls []call
	oldRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		calls = append(calls, call{stdin: stdin, args: append([]string(nil), args...)})
		return nil
	}
	defer func() { runTerminalTmuxCommand = oldRun }()

	req := httptest.NewRequest(http.MethodPost, "/api/terminals/"+terminalID+"/input", strings.NewReader(`{"text":"debug message","submit":true}`))
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleSendTerminalInput(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("send input status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if len(calls) != 3 {
		t.Fatalf("tmux call count = %d, want 3: %#v", len(calls), calls)
	}
	if calls[0].stdin != "debug message" || len(calls[0].args) < 3 || calls[0].args[0] != "load-buffer" {
		t.Fatalf("first tmux call = %#v, want load-buffer with stdin", calls[0])
	}
	if calls[1].args[0] != "paste-buffer" || !containsString(calls[1].args, tmuxSession) {
		t.Fatalf("second tmux call = %#v, want paste-buffer into session", calls[1])
	}
	if got := strings.Join(calls[2].args, " "); got != "send-keys -t "+tmuxSession+" C-m" {
		t.Fatalf("third tmux call = %q, want enter", got)
	}
}

func TestTerminalRoutesSendEscKeyUsesTmuxSendKeys(t *testing.T) {
	store := terminals.NewStore()
	api := &StreamingAPI{terminalStore: store}
	sessionID := "session-terminal-esc"
	terminalID := sessionID + ":workflow-step:review-plan"
	tmuxSession := "mlp-claude-code-exp-test"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "pane", 2))

	var gotArgs []string
	oldRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}
	defer func() { runTerminalTmuxCommand = oldRun }()

	req := httptest.NewRequest(http.MethodPost, "/api/terminals/"+terminalID+"/key", strings.NewReader(`{"key":"esc"}`))
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleSendTerminalKey(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("send key status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if got := strings.Join(gotArgs, " "); got != "send-keys -t "+tmuxSession+" Escape" {
		t.Fatalf("tmux args = %q, want escape", got)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func terminalRouteList(t *testing.T, api *StreamingAPI, sessionID string) listTerminalsResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/terminals?session_id="+sessionID, nil)
	rec := httptest.NewRecorder()
	api.handleListTerminals(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var response listTerminalsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return response
}

func terminalRouteChunkEvent(sessionID, executionID, tmuxSession, content string, chunkIndex int) storeevents.Event {
	return storeevents.Event{
		Type:          "streaming_chunk",
		Timestamp:     time.Now(),
		SessionID:     sessionID,
		ExecutionID:   executionID,
		ExecutionKind: "workflow_step",
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingChunk,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: map[string]interface{}{
						"kind":            "terminal",
						"tmux_session":    tmuxSession,
						"current_step_id": "review-plan",
						"execution_kind":  "workflow_step",
						"scope":           "workflow_step",
						"workflow_path":   "Workflow/instagram",
					},
				},
				Content:    content,
				ChunkIndex: chunkIndex,
			},
		},
	}
}

func terminalRouteEndEvent(sessionID, executionID, tmuxSession string, retentionSeconds int) storeevents.Event {
	return storeevents.Event{
		Type:        "streaming_end",
		Timestamp:   time.Now(),
		SessionID:   sessionID,
		ExecutionID: executionID,
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingEnd,
			Data: &agentevents.StreamingEndEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: map[string]interface{}{
						"kind":                       "terminal",
						"tmux_session":               tmuxSession,
						"current_step_id":            "review-plan",
						"terminal_retention_seconds": retentionSeconds,
					},
				},
			},
		},
	}
}

func terminalRouteStructuredChunkEvent(sessionID, executionID, content string, chunkIndex int, metadata map[string]interface{}) storeevents.Event {
	return storeevents.Event{
		Type:          "streaming_chunk",
		Timestamp:     time.Now(),
		SessionID:     sessionID,
		ExecutionID:   executionID,
		ExecutionKind: "workflow_step",
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingChunk,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{
					Metadata: cloneTerminalRouteMetadata(metadata),
				},
				Content:    content,
				ChunkIndex: chunkIndex,
			},
		},
	}
}

func terminalRouteToolStartEvent(sessionID, executionID, toolCallID, toolName, args string, metadata map[string]interface{}) storeevents.Event {
	return storeevents.Event{
		Type:          string(agentevents.ToolCallStart),
		Timestamp:     time.Now(),
		SessionID:     sessionID,
		ExecutionID:   executionID,
		ExecutionKind: "workflow_step",
		Data: &agentevents.AgentEvent{
			Type: agentevents.ToolCallStart,
			Data: &agentevents.ToolCallStartEvent{
				BaseEventData: agentevents.BaseEventData{Metadata: cloneTerminalRouteMetadata(metadata)},
				ToolCallID:    toolCallID,
				ToolName:      toolName,
				ToolParams:    agentevents.ToolParams{Arguments: args},
			},
		},
	}
}

func terminalRouteToolEndEvent(sessionID, executionID, toolCallID, toolName, result string, metadata map[string]interface{}) storeevents.Event {
	return storeevents.Event{
		Type:          string(agentevents.ToolCallEnd),
		Timestamp:     time.Now(),
		SessionID:     sessionID,
		ExecutionID:   executionID,
		ExecutionKind: "workflow_step",
		Data: &agentevents.AgentEvent{
			Type: agentevents.ToolCallEnd,
			Data: &agentevents.ToolCallEndEvent{
				BaseEventData: agentevents.BaseEventData{Metadata: cloneTerminalRouteMetadata(metadata)},
				ToolCallID:    toolCallID,
				ToolName:      toolName,
				Result:        result,
			},
		},
	}
}

func cloneTerminalRouteMetadata(metadata map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
