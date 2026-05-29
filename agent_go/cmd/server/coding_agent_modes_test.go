package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	internalevents "mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"

	pkgevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
)

func TestCodingAgentPersistentInteractiveFlags(t *testing.T) {
	tests := []struct {
		name            string
		provider        string
		wantClaudeCode  bool
		wantCodexCLI    bool
		wantGeminiCLI   bool
		wantCursorCLI   bool
		wantAgyCLI      bool
		wantOpenCodeCLI bool
	}{
		{
			name:           "claude code chat gets persistent tmux",
			provider:       string(llm.ProviderClaudeCode),
			wantClaudeCode: true,
		},
		{
			name:         "codex chat gets persistent tmux",
			provider:     string(llm.ProviderCodexCLI),
			wantCodexCLI: true,
		},
		{
			name:          "gemini chat gets persistent tmux",
			provider:      string(llm.ProviderGeminiCLI),
			wantGeminiCLI: true,
		},
		{
			name:          "cursor chat gets persistent tmux",
			provider:      string(llm.ProviderCursorCLI),
			wantCursorCLI: true,
		},
		{
			name:       "agy chat gets persistent tmux",
			provider:   string(llm.ProviderAgyCLI),
			wantAgyCLI: true,
		},
		{
			name:            "opencode chat is json based",
			provider:        string(llm.ProviderOpenCodeCLI),
			wantOpenCodeCLI: false,
		},
		{
			name:     "non coding provider never gets tmux",
			provider: string(llm.ProviderOpenAI),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClaudeCode, gotCodexCLI, gotGeminiCLI, gotCursorCLI, gotAgyCLI, gotOpenCodeCLI := codingAgentPersistentInteractiveFlags(tt.provider)
			if gotClaudeCode != tt.wantClaudeCode || gotCodexCLI != tt.wantCodexCLI || gotGeminiCLI != tt.wantGeminiCLI || gotCursorCLI != tt.wantCursorCLI || gotAgyCLI != tt.wantAgyCLI || gotOpenCodeCLI != tt.wantOpenCodeCLI {
				t.Fatalf("flags = (%v, %v, %v, %v, %v, %v), want (%v, %v, %v, %v, %v, %v)", gotClaudeCode, gotCodexCLI, gotGeminiCLI, gotCursorCLI, gotAgyCLI, gotOpenCodeCLI, tt.wantClaudeCode, tt.wantCodexCLI, tt.wantGeminiCLI, tt.wantCursorCLI, tt.wantAgyCLI, tt.wantOpenCodeCLI)
			}
		})
	}
}

func TestCodingAgentPersistentInteractiveFlagsCoverTmuxContracts(t *testing.T) {
	for _, contract := range llm.CodingAgentProviderContracts() {
		if contract.Transport != llm.CodingAgentTransportTmux {
			continue
		}
		t.Run(string(contract.Provider), func(t *testing.T) {
			gotClaudeCode, gotCodexCLI, gotGeminiCLI, gotCursorCLI, gotAgyCLI, gotOpenCodeCLI := codingAgentPersistentInteractiveFlags(string(contract.Provider))
			count := 0
			for _, enabled := range []bool{gotClaudeCode, gotCodexCLI, gotGeminiCLI, gotCursorCLI, gotAgyCLI, gotOpenCodeCLI} {
				if enabled {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("provider %q enables %d persistent flags, want exactly one", contract.Provider, count)
			}
		})
	}
}

func TestCodingAgentClaudeCodeChatTransport(t *testing.T) {
	if got := codingAgentClaudeCodeChatTransport(string(llm.ProviderClaudeCode)); got != llm.ClaudeCodeTransportTmux {
		t.Fatalf("claude-code chat transport = %q, want %q", got, llm.ClaudeCodeTransportTmux)
	}
	if got := codingAgentClaudeCodeChatTransport(string(llm.ProviderCodexCLI)); got != "" {
		t.Fatalf("non-Claude chat transport = %q, want empty", got)
	}
}

func TestCodingAgentWorkspaceWorkingDirUsesWorkspaceDocsRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	got := codingAgentWorkspaceWorkingDir("_users/alice/Chats")
	want := filepath.Join(root, "_users", "alice", "Chats")
	if got != want {
		t.Fatalf("working dir = %q, want %q", got, want)
	}
}

func TestTopLevelTierModelDoesNotOverrideExplicitChatLLM(t *testing.T) {
	req := QueryRequest{
		Provider: "gemini-cli",
		ModelID:  "gemini-3.1-flash-lite",
		LLMConfig: &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: "gemini-cli",
				ModelID:  "gemini-3.1-flash-lite",
			},
		},
		DelegationTierConfig: &virtualtools.DelegationTierConfig{
			High: &virtualtools.TierModel{
				Provider: "claude-code",
				ModelID:  "claude-sonnet-4-6",
			},
		},
	}

	gotProvider, gotModel, _, applied := applyTopLevelTierModelIfNoExplicitLLM(context.Background(), req, "gemini-cli", "gemini-3.1-flash-lite", nil)
	if applied {
		t.Fatal("tier model was applied despite an explicit chat LLM selection")
	}
	if gotProvider != "gemini-cli" || gotModel != "gemini-3.1-flash-lite" {
		t.Fatalf("resolved chat LLM = %s/%s, want gemini-cli/gemini-3.1-flash-lite", gotProvider, gotModel)
	}
}

func TestTopLevelTierModelAppliesWhenChatLLMIsMissing(t *testing.T) {
	req := QueryRequest{
		DelegationTierConfig: &virtualtools.DelegationTierConfig{
			High: &virtualtools.TierModel{
				Provider: "claude-code",
				ModelID:  "claude-sonnet-4-6",
			},
		},
	}

	gotProvider, gotModel, _, applied := applyTopLevelTierModelIfNoExplicitLLM(context.Background(), req, "", "", nil)
	if !applied {
		t.Fatal("tier model was not applied for a request with no chat LLM selection")
	}
	if gotProvider != "claude-code" || gotModel != "claude-sonnet-4-6" {
		t.Fatalf("resolved chat LLM = %s/%s, want claude-code/claude-sonnet-4-6", gotProvider, gotModel)
	}
}

func TestRecordLiveCodingAgentUserMessageCapturesVisibleEvent(t *testing.T) {
	tests := []struct {
		name     string
		provider llm.Provider
	}{
		{name: "claude code", provider: llm.ProviderClaudeCode},
		{name: "codex cli", provider: llm.ProviderCodexCLI},
		{name: "gemini cli", provider: llm.ProviderGeminiCLI},
		{name: "cursor cli", provider: llm.ProviderCursorCLI},
		{name: "agy cli", provider: llm.ProviderAgyCLI},
		{name: "opencode cli", provider: llm.ProviderOpenCodeCLI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := internalevents.NewEventStore(10)
			defer store.Stop()

			sessionID := "live-coding-session-" + string(tt.provider)
			api := &StreamingAPI{eventStore: store}
			sub := store.Subscribe(sessionID)
			defer store.Unsubscribe(sessionID, sub)

			api.recordLiveCodingAgentUserMessage(sessionID, "show exact sequence item", string(tt.provider), "test-message-id", "sent_to_cli")

			var delivered internalevents.Event
			select {
			case delivered = <-sub.Ch:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for live coding user_message event")
			}
			assertLiveCodingUserMessageEvent(t, delivered, sessionID, string(tt.provider))

			raw := store.GetAllEventsRaw(sessionID)
			if len(raw) != 1 {
				t.Fatalf("raw event count = %d, want 1", len(raw))
			}
			assertLiveCodingUserMessageEvent(t, raw[0], sessionID, string(tt.provider))

			poll := store.GetEvents(sessionID, internalevents.GetEventsOptions{SinceIndex: -1})
			if len(poll.Events) != 1 {
				t.Fatalf("poll event count = %d, want 1", len(poll.Events))
			}
			assertLiveCodingUserMessageEvent(t, poll.Events[0], sessionID, string(tt.provider))
		})
	}
}

func TestHandleSteerMessageRoutesThroughAgentDelivery(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "structured-session"
	runningAgent := &mcpagent.Agent{ModelID: "opencode"}
	runningAgent.SetProvider(llm.ProviderOpenCodeCLI)
	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() {}},
		agentCancelMux:   sync.RWMutex{},
	}

	body := bytes.NewBufferString(`{"message":"send this through delivery"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/steer", body)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rr := httptest.NewRecorder()

	api.handleSteerMessage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	var response SteerMessageResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DeliveryStatus != "queued_for_injection" {
		t.Fatalf("delivery_status = %q, want queued_for_injection", response.DeliveryStatus)
	}
	queued := runningAgent.DrainSteerMessages()
	if len(queued) != 1 || queued[0] != "send this through delivery" {
		t.Fatalf("queued messages = %#v", queued)
	}
}

func TestHandleSteerMessageRejectsStaleStoredAgentWithoutActiveTurn(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "stale-claude-session"
	runningAgent := &mcpagent.Agent{ModelID: "claude-sonnet-4-6"}
	runningAgent.SetProvider(llm.ProviderClaudeCode)
	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		sessionBusy:      map[string]bool{sessionID: true},
		sessionBusySince: map[string]time.Time{sessionID: time.Now().Add(-time.Minute)},
		sessionBusyMu:    sync.RWMutex{},
	}

	body := bytes.NewBufferString(`{"message":"this should become a new turn"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/steer", body)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rr := httptest.NewRecorder()

	api.handleSteerMessage(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409 stale-turn conflict", rr.Code, rr.Body.String())
	}
	if api.isSessionBusy(sessionID) {
		t.Fatal("stale steer rejection should clear session busy so frontend can start the next turn")
	}
	if got := len(store.GetAllEventsRaw(sessionID)); got != 0 {
		t.Fatalf("stale steer rejection recorded %d events, want none", got)
	}
}

func TestCanSteerSessionRequiresActiveForegroundTurn(t *testing.T) {
	sessionID := "foreground-session"
	runningAgent := &mcpagent.Agent{ModelID: "claude-sonnet-4-6"}
	runningAgent.SetProvider(llm.ProviderClaudeCode)
	api := &StreamingAPI{
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{},
		agentCancelMux:   sync.RWMutex{},
	}

	if api.canSteerSession(sessionID) {
		t.Fatal("canSteerSession = true with only a retained agent object; want false until a foreground turn is active")
	}

	api.agentCancelMux.Lock()
	api.agentCancelFuncs[sessionID] = func() {}
	api.agentCancelMux.Unlock()
	if !api.canSteerSession(sessionID) {
		t.Fatal("canSteerSession = false with retained agent and active foreground cancel; want true")
	}
}

func TestDelegationStartEventParentsToBackgroundAgent(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "session-background-owner"
	backgroundAgentID := "bg-agent-123"
	delegationID := "delegation-child-456"
	api := &StreamingAPI{eventStore: store}

	api.emitDelegationStartEvent(sessionID, delegationID, 1, "inspect logs", "high", "claude-sonnet-4-6", []string{"api-bridge"}, backgroundAgentID, "worker")

	rawEvents := store.GetAllEventsRaw(sessionID)
	if len(rawEvents) != 1 {
		t.Fatalf("raw event count = %d, want 1", len(rawEvents))
	}
	event := rawEvents[0]
	if event.Type != "delegation_start" {
		t.Fatalf("event type = %q, want delegation_start", event.Type)
	}
	if event.Data == nil {
		t.Fatal("event data is nil")
	}
	wantParentID := sessionID + "_background_agent_started_" + backgroundAgentID
	if event.Data.ParentID != wantParentID {
		t.Fatalf("parent_id = %q, want %q", event.Data.ParentID, wantParentID)
	}
	if event.Data.CorrelationID != delegationID {
		t.Fatalf("correlation_id = %q, want %q", event.Data.CorrelationID, delegationID)
	}
}

func assertLiveCodingUserMessageEvent(t *testing.T, event internalevents.Event, sessionID, provider string) {
	t.Helper()
	if event.Type != string(pkgevents.UserMessageEventType) {
		t.Fatalf("event type = %q, want user_message", event.Type)
	}
	if event.SessionID != sessionID {
		t.Fatalf("event session = %q, want %q", event.SessionID, sessionID)
	}
	if event.Data == nil {
		t.Fatal("event data is nil")
	}
	if event.Data.Component != "coding_agent_live_input" {
		t.Fatalf("component = %q, want coding_agent_live_input", event.Data.Component)
	}
	msg, ok := event.Data.Data.(*pkgevents.UserMessageEvent)
	if !ok {
		t.Fatalf("event payload type = %T, want *UserMessageEvent", event.Data.Data)
	}
	if msg.Content != "show exact sequence item" {
		t.Fatalf("content = %q, want live message", msg.Content)
	}
	if msg.Role != "user" {
		t.Fatalf("role = %q, want user", msg.Role)
	}
	if msg.Metadata["source"] != "coding_agent_live_input" {
		t.Fatalf("metadata source = %#v, want coding_agent_live_input", msg.Metadata["source"])
	}
	if msg.Metadata["provider"] != provider {
		t.Fatalf("metadata provider = %#v, want %q", msg.Metadata["provider"], provider)
	}
	if msg.Metadata["message_id"] != "test-message-id" {
		t.Fatalf("metadata message_id = %#v, want test-message-id", msg.Metadata["message_id"])
	}
	if msg.Metadata["delivery_status"] != "sent_to_cli" {
		t.Fatalf("metadata delivery_status = %#v, want sent_to_cli", msg.Metadata["delivery_status"])
	}
}
