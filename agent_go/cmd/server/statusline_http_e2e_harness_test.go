package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	agentevents "github.com/manishiitg/mcpagent/events"

	storeevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
)

// statusLineE2ECase describes one CLI provider for the shared real-CLI statusline
// HTTP e2e. Adding a provider = adding a case: the harness owns the
// run → EventStore → terminalStore → GET /api/terminals chain and the shared
// assertions, so a new provider only supplies how to run it and pull its
// statusline.
type statusLineE2ECase struct {
	name    string // subtest / log label, e.g. "agy"
	envGate string // env var that must be set to opt in
	binary  string // CLI binary that must be in PATH
	cleanup func() // optional teardown (e.g. kill tmux sessions)
	// run performs a real CLI turn under sessionID and returns the statusline the
	// CLI produced — typically GenerateContent followed by GetStatusLine.
	run func(ctx context.Context, sessionID string) (*llmtypes.StatusLine, error)
	// wantStatusExtras asserts the provider surfaced generic statusline extras
	// (e.g. plan rate-limit usage) under status_meta.status_extras, reaching the
	// HTTP response after the full clone/fan-out chain.
	wantStatusExtras bool
}

// runStatusLineHTTPE2E drives the full consumer chain the frontend depends on:
// real CLI run → StatusLine → real EventStore (incl. CloneAgentEvent) →
// terminalStore fan-out → GET /api/terminals JSON, and asserts the telemetry
// survived end to end. Gated exactly like the sibling *_real_test.go e2e tests.
func runStatusLineHTTPE2E(t *testing.T, c statusLineE2ECase) {
	t.Helper()
	if os.Getenv(c.envGate) == "" {
		t.Skipf("set %s=1 to run the %s statusline HTTP e2e", c.envGate, c.name)
	}
	if _, err := exec.LookPath(c.binary); err != nil {
		t.Skipf("%s binary not found: %v", c.binary, err)
	}
	if c.cleanup != nil {
		t.Cleanup(c.cleanup)
	}

	sessionID := "test-session-statusline-" + c.name + "-001"
	tmuxSession := "mlp-" + c.name + "-statusline-e2e"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sl, err := c.run(ctx, sessionID)
	if err != nil {
		t.Fatalf("%s run: %v", c.name, err)
	}
	if sl == nil || sl.Provider == "" {
		t.Fatalf("%s: statusline missing provider: %+v", c.name, sl)
	}

	// Wire the real consumer chain exactly as the server does.
	store := terminals.NewStore()
	es := storeevents.NewEventStore(100)
	es.SetEventAddedCallback(store.HandleEvent)
	api := &StreamingAPI{terminalStore: store, eventStore: es}

	// Seed the coding-agent pane, then drive the real statusline payload through
	// the event store (the production AddEvent path, incl. CloneAgentEvent).
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, tmuxSession, c.name+" pane", 1))
	es.AddEvent(sessionID, storeevents.Event{
		Type:      "status_line",
		SessionID: sessionID,
		Timestamp: time.Now(),
		Data: &agentevents.AgentEvent{
			Type: agentevents.StreamingStatusLine,
			Data: &agentevents.StreamingStatusLineEvent{
				Provider:                 sl.Provider,
				Model:                    sl.Model,
				TmuxSession:              tmuxSession,
				InputTokens:              sl.InputTokens,
				OutputTokens:             sl.OutputTokens,
				CacheCreationInputTokens: sl.CacheCreationInputTokens,
				CacheReadInputTokens:     sl.CacheReadInputTokens,
				TotalInputTokens:         sl.TotalInputTokens,
				TotalOutputTokens:        sl.TotalOutputTokens,
				CostUSD:                  sl.CostUSD,
				Metadata:                 sl.Metadata,
			},
		},
	})

	// HTTP roundtrip — exactly what the frontend polls.
	req := httptest.NewRequest(http.MethodGet, "/api/terminals?session_id="+sessionID+"&content=none", nil)
	rec := httptest.NewRecorder()
	api.handleListTerminals(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/terminals status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var resp listTerminalsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v\nbody=%s", err, rec.Body.String())
	}
	if len(resp.Terminals) == 0 {
		t.Fatalf("%s: no terminals returned for the session", c.name)
	}
	st := resp.Terminals[0].Status

	if st.ProviderLabel == "" {
		t.Fatalf("%s: provider label missing in HTTP response: %+v", c.name, st)
	}
	// No duplicated provider name (the "X · X" placeholder-model regression).
	if dup := sl.Provider + " · " + sl.Provider; strings.Contains(st.ProviderLabel, dup) {
		t.Fatalf("%s: provider label is duplicated: %q", c.name, st.ProviderLabel)
	}
	if st.InputTokens == 0 && st.OutputTokens == 0 && st.TotalInputTokens == 0 && st.CostUSD == 0 {
		t.Fatalf("%s: no telemetry reached /api/terminals: %+v", c.name, st)
	}

	// Generic statusline extras (e.g. "5h 24%", "7d 41%") arrive as JSON
	// strings under status_meta.status_extras — i.e. []interface{} after the
	// HTTP roundtrip. Decode them so the assertion sees real display segments.
	var statusExtras []string
	if raw, ok := st.StatusMeta["status_extras"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				statusExtras = append(statusExtras, s)
			}
		}
	}
	if c.wantStatusExtras && len(statusExtras) == 0 {
		t.Fatalf("%s: expected status_extras (plan rate-limit usage) in status_meta after HTTP roundtrip, got none: status_meta=%+v", c.name, st.StatusMeta)
	}

	t.Logf("✅ %s statusline HTTP e2e: provider=%q in=%d out=%d cacheRead=%d cost=$%.6f model=%q extras=%v",
		c.name, st.ProviderLabel, st.InputTokens, st.OutputTokens, st.CacheReadInputTokens, st.CostUSD, sl.Model, statusExtras)
}

// statusLineOKPrompt elicits a quick single-turn CLI run that still produces a
// statusline.
func statusLineOKPrompt() []llmtypes.MessageContent {
	return []llmtypes.MessageContent{
		{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
			llmtypes.TextContent{Text: "Reply with exactly the word OK and nothing else."},
		}},
	}
}

func statusLineModelOr(envKey, def string) string {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	return def
}
