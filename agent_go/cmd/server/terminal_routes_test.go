package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
