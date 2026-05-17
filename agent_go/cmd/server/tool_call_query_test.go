package server

import (
	"strings"
	"testing"

	"mcp-agent-builder-go/agent_go/internal/events"

	"github.com/manishiitg/mcpagent/toolcalllog"
)

func TestFormatToolCallSummariesIncludesLiveStepBridgeSnapshots(t *testing.T) {
	store := events.NewEventStore(100)
	defer store.Stop()

	api := &StreamingAPI{eventStore: store}
	stepID := "step-query-live"
	bridgeSessionID := "sub-exec-step-query-live-123"
	toolcalllog.Clear(bridgeSessionID)
	t.Cleanup(func() { toolcalllog.Clear(bridgeSessionID) })

	toolCallID := toolcalllog.RecordStart(bridgeSessionID, "execute_shell_command", `{"command":"sleep 10"}`)

	query := formatToolCallSummaries(api)
	summary := query("main-chat-session", "workshop-step-query-live-999", stepID, "")
	for _, want := range []string{"[RUNNING] execute_shell_command", toolCallID, bridgeSessionID, `"command":"sleep 10"`} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}

	detail := query("main-chat-session", "workshop-step-query-live-999", stepID, toolCallID)
	for _, want := range []string{"execute_shell_command", "session_id: " + bridgeSessionID, `"command":"sleep 10"`} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail)
		}
	}

	toolcalllog.RecordEnd(bridgeSessionID, toolCallID, "execute_shell_command", `{"command":"sleep 10"}`, `{"stdout":"ok"}`, toolcalllog.Snapshot(bridgeSessionID)[0].StartedAt)
	summary = query("main-chat-session", "workshop-step-query-live-999", stepID, "")
	for _, want := range []string{"[DONE] execute_shell_command", `{"stdout":"ok"}`} {
		if !strings.Contains(summary, want) {
			t.Fatalf("completed summary missing %q:\n%s", want, summary)
		}
	}
}
