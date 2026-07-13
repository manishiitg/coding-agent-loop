package terminals

import (
	"testing"
	"time"

	agentevents "github.com/manishiitg/mcpagent/events"
	storeevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

// TestStatusLineE2EThroughEventStore wires the store to the EventStore exactly
// as the server does (eventStore.SetEventAddedCallback(terminalStore.HandleEvent))
// and drives a status_line event through the real AddEvent path — including the
// production CloneAgentEvent deep-copy. It asserts the FULL telemetry payload
// reaches the terminal snapshot (no field dropped), that the update is scoped to
// the owning tmux pane, and that a subsequent pane refresh does not wipe it.
func TestStatusLineE2EThroughEventStore(t *testing.T) {
	es := storeevents.NewEventStore(100)
	ts := NewStore()
	es.SetEventAddedCallback(ts.HandleEvent) // the real server wiring

	now := time.Now()

	// Seed a coding-agent pane through the event store (the live event path).
	es.AddEvent("session-1", terminalEventWithMetadata("exec-1", "agy pane", 1,
		map[string]interface{}{"tmux_session": "mlp-agy-1"}, now))

	// A second, unrelated pane in the same session — must NOT receive telemetry.
	es.AddEvent("session-1", terminalEventWithMetadata("exec-2", "other pane", 1,
		map[string]interface{}{"tmux_session": "mlp-agy-2"}, now))

	// Emit the status_line event carrying the complete StatusLine payload.
	es.AddEvent("session-1", storeevents.Event{
		Type:      "status_line",
		SessionID: "session-1",
		Timestamp: now,
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingStatusLine,
			Data: &agentevents.StreamingStatusLineEvent{
				Provider:                 "agy-cli",
				TmuxSession:              "mlp-agy-1",
				InputTokens:              15000,
				OutputTokens:             273,
				CacheCreationInputTokens: 1200,
				CacheReadInputTokens:     48000,
				TotalInputTokens:         63000,
				TotalOutputTokens:        900,
				Metadata: map[string]interface{}{
					"context_window": map[string]interface{}{"total_input_tokens": 200000},
				},
			},
		},
	})

	snap, ok := ts.Get("session-1:exec-1")
	if !ok {
		t.Fatal("expected terminal session-1:exec-1")
	}
	st := snap.Status
	if st.ProviderLabel != "agy-cli" {
		t.Errorf("ProviderLabel = %q, want agy-cli (no placeholder model)", st.ProviderLabel)
	}
	if st.InputTokens != 15000 || st.OutputTokens != 273 {
		t.Errorf("tokens = %d in / %d out, want 15000 / 273", st.InputTokens, st.OutputTokens)
	}
	if st.CacheCreationInputTokens != 1200 || st.CacheReadInputTokens != 48000 {
		t.Errorf("cache tokens = %d create / %d read, want 1200 / 48000", st.CacheCreationInputTokens, st.CacheReadInputTokens)
	}
	if st.TotalInputTokens != 63000 || st.TotalOutputTokens != 900 {
		t.Errorf("totals = %d in / %d out, want 63000 / 900", st.TotalInputTokens, st.TotalOutputTokens)
	}
	if st.StatusMeta == nil {
		t.Fatal("StatusMeta dropped; raw provider extras must survive")
	}
	cw, _ := st.StatusMeta["context_window"].(map[string]interface{})
	if cw == nil || intFromAny(cw["total_input_tokens"]) != 200000 {
		t.Errorf("StatusMeta.context_window not preserved: %+v", st.StatusMeta)
	}

	// The unrelated pane must remain untouched.
	other, _ := ts.Get("session-1:exec-2")
	if other.Status.InputTokens != 0 || other.Status.CacheReadInputTokens != 0 || other.Status.StatusMeta != nil {
		t.Errorf("unrelated pane received telemetry: %+v", other.Status)
	}

	// A pane refresh (DeriveStatus rebuild) must NOT wipe the telemetry.
	es.AddEvent("session-1", terminalEventWithMetadata("exec-1", "agy pane - updated", 2,
		map[string]interface{}{"tmux_session": "mlp-agy-1"}, now.Add(time.Second)))

	after, _ := ts.Get("session-1:exec-1")
	if after.Status.InputTokens != 15000 || after.Status.OutputTokens != 273 {
		t.Errorf("telemetry wiped on refresh: %d in / %d out", after.Status.InputTokens, after.Status.OutputTokens)
	}
	if after.Status.CacheReadInputTokens != 48000 || after.Status.TotalInputTokens != 63000 {
		t.Errorf("cache/total wiped on refresh: cacheRead=%d totalIn=%d", after.Status.CacheReadInputTokens, after.Status.TotalInputTokens)
	}
	if after.Status.ProviderLabel != "agy-cli" {
		t.Errorf("ProviderLabel wiped on refresh: %q", after.Status.ProviderLabel)
	}
	if after.Status.StatusMeta == nil {
		t.Error("StatusMeta wiped on refresh")
	}
}

func intFromAny(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
