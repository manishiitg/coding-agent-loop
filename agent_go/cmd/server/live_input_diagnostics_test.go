package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
	agentevents "github.com/manishiitg/mcpagent/events"
)

func TestWriteLiveInputUnavailableIncludesSanitizedTmuxSnapshot(t *testing.T) {
	const (
		sessionID   = "session-live-diagnostic"
		tmuxSession = "mlp-pi-cli-diagnostic"
		secret      = "diagnostic-secret-value"
	)
	store := terminals.NewStore()
	event := terminalRouteChunkEvent(sessionID, "main:"+sessionID, tmuxSession, "stored pane", 1)
	event.ExecutionKind = "main_agent"
	chunk := event.Data.Data.(*agentevents.StreamingChunkEvent)
	chunk.Metadata["execution_kind"] = "main_agent"
	chunk.Metadata["scope"] = "main_agent"
	store.HandleEvent(sessionID, event)
	api := &StreamingAPI{terminalStore: store}

	originalCapture := runTerminalTmuxOutputCommand
	t.Cleanup(func() { runTerminalTmuxOutputCommand = originalCapture })
	var captureArgs []string
	runTerminalTmuxOutputCommand = func(_ context.Context, args ...string) (string, error) {
		captureArgs = append([]string(nil), args...)
		return "MCP_API_TOKEN=" + secret + "\nassistant complete\nπ • ✅ api-bridge_execute_shell_command\n", nil
	}

	recorder := httptest.NewRecorder()
	api.writeLiveInputUnavailable(recorder, sessionID, "", "context deadline exceeded")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	if got := strings.Join(captureArgs, " "); got != "capture-pane -p -J -t "+tmuxSession {
		t.Fatalf("capture args = %q", got)
	}

	var response liveInputUnavailableResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "live_input_unavailable" || response.Provider != "pi-cli" {
		t.Fatalf("response identity = %#v", response)
	}
	if response.TerminalSnapshot == nil || !strings.Contains(response.TerminalSnapshot.Content, "✅ api-bridge_execute_shell_command") {
		t.Fatalf("terminal snapshot missing completed tool status: %#v", response.TerminalSnapshot)
	}
	if strings.Contains(recorder.Body.String(), secret) {
		t.Fatalf("response leaked terminal secret: %s", recorder.Body.String())
	}
	if !strings.Contains(response.TechnicalDetails, "tmux: "+tmuxSession) || !strings.Contains(response.TechnicalDetails, "[redacted]") {
		t.Fatalf("technical details missing tmux/redaction: %q", response.TechnicalDetails)
	}
}

func TestBoundedRedactedLiveInputSnapshotKeepsTail(t *testing.T) {
	lines := make([]string, liveInputDiagnosticMaxLines+5)
	for index := range lines {
		lines[index] = "line"
	}
	lines[0] = "oldest-omitted"
	lines[len(lines)-1] = "π • ✅ final-tool"
	got := boundedRedactedLiveInputSnapshot(strings.Join(lines, "\n"))
	if strings.Contains(got, "oldest-omitted") {
		t.Fatalf("snapshot kept content outside line bound: %q", got)
	}
	if !strings.Contains(got, "[snapshot truncated") || !strings.Contains(got, "π • ✅ final-tool") {
		t.Fatalf("snapshot did not preserve annotated tail: %q", got)
	}
}
