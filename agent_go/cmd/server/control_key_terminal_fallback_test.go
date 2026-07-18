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

	storeevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
)

func TestControlKeyFallsBackToCompletedLiveMainTerminal(t *testing.T) {
	for _, key := range []string{"Up", "Down", "Enter", "Escape"} {
		t.Run(key, func(t *testing.T) {
			store := terminals.NewStore()
			sessionID := "session-control-fallback-" + strings.ToLower(key)
			tmuxSession := "mlp-codex-cli-int-" + strings.ToLower(key)
			store.HandleEvent(sessionID, controlKeyMainTerminalEvent(sessionID, tmuxSession))
			store.HandleEvent(sessionID, controlKeyMainTerminalEndEvent(sessionID, tmuxSession))
			if snapshots := store.ListRaw(sessionID); len(snapshots) != 1 {
				t.Fatalf("fixture snapshots = %#v", snapshots)
			} else if !codingAgentSnapshotIsMainAgent(snapshots[0]) || strings.TrimSpace(snapshots[0].TmuxSession) == "" || snapshots[0].ProcessState != "live" {
				t.Fatalf("fixture is not a live main terminal: %#v", snapshots[0])
			}

			api := &StreamingAPI{terminalStore: store}
			var gotArgs []string
			oldRun := runTerminalTmuxCommand
			runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
				gotArgs = append([]string(nil), args...)
				return nil
			}
			t.Cleanup(func() { runTerminalTmuxCommand = oldRun })

			req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/control", strings.NewReader(`{"key":"`+key+`"}`))
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			rec := httptest.NewRecorder()
			api.handleControlKey(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
			}
			if got := strings.Join(gotArgs, " "); got != "send-keys -t "+tmuxSession+" "+key {
				t.Fatalf("tmux args = %q", got)
			}
			var response ControlKeyResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !response.Success || response.Key != key {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestControlKeyDoesNotTargetClosedTerminal(t *testing.T) {
	store := terminals.NewStore()
	sessionID := "session-control-closed"
	tmuxSession := "mlp-codex-cli-int-closed"
	terminalID := sessionID + ":main:" + sessionID
	store.HandleEvent(sessionID, controlKeyMainTerminalEvent(sessionID, tmuxSession))
	store.HandleEvent(sessionID, controlKeyMainTerminalEndEvent(sessionID, tmuxSession))
	store.MarkProcessClosed(terminalID, "test closed")

	api := &StreamingAPI{terminalStore: store}
	oldRun := runTerminalTmuxCommand
	runTerminalTmuxCommand = func(context.Context, string, ...string) error {
		t.Fatal("closed terminal must not receive a key")
		return nil
	}
	t.Cleanup(func() { runTerminalTmuxCommand = oldRun })

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/control", strings.NewReader(`{"key":"Down"}`))
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rec := httptest.NewRecorder()
	api.handleControlKey(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func controlKeyMainTerminalEvent(sessionID, tmuxSession string) storeevents.Event {
	return storeevents.Event{
		Type:          "streaming_chunk",
		Timestamp:     time.Now(),
		SessionID:     sessionID,
		ExecutionID:   "main:" + sessionID,
		ExecutionKind: "main_agent",
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingChunk,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{Metadata: map[string]interface{}{
					"kind":           "terminal",
					"tmux_session":   tmuxSession,
					"owner_id":       "main:" + sessionID,
					"execution_kind": "main_agent",
					"scope":          "main",
				}},
				Content:    "CLI prompt",
				ChunkIndex: 1,
			},
		},
	}
}

func controlKeyMainTerminalEndEvent(sessionID, tmuxSession string) storeevents.Event {
	return storeevents.Event{
		Type:          "streaming_end",
		Timestamp:     time.Now(),
		SessionID:     sessionID,
		ExecutionID:   "main:" + sessionID,
		ExecutionKind: "main_agent",
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingEnd,
			Data: &agentevents.StreamingEndEvent{BaseEventData: agentevents.BaseEventData{Metadata: map[string]interface{}{
				"kind":           "terminal",
				"tmux_session":   tmuxSession,
				"owner_id":       "main:" + sessionID,
				"execution_kind": "main_agent",
				"scope":          "main",
			}}},
		},
	}
}
