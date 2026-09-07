package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	pkgevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
)

func TestCodingAgentPersistentInteractiveFlags(t *testing.T) {
	tests := []struct {
		name            string
		provider        string
		allowPersistent bool
		structured      bool
		wantClaudeCode  bool
		wantCodexCLI    bool
		wantCursorCLI   bool
		wantPiCLI       bool
	}{
		{
			name:            "claude code chat gets persistent tmux",
			provider:        string(llm.ProviderClaudeCode),
			allowPersistent: true,
			wantClaudeCode:  true,
		},
		{
			name:            "codex chat gets persistent tmux",
			provider:        string(llm.ProviderCodexCLI),
			allowPersistent: true,
			wantCodexCLI:    true,
		},
		{
			name:            "cursor chat uses structured transport",
			provider:        string(llm.ProviderCursorCLI),
			allowPersistent: true,
			structured:      true,
		},
		{
			name:            "cursor workflow builder gets persistent tmux",
			provider:        string(llm.ProviderCursorCLI),
			allowPersistent: true,
			wantCursorCLI:   true,
		},
		{
			name:            "pi chat gets persistent tmux",
			provider:        string(llm.ProviderPiCLI),
			allowPersistent: true,
			wantPiCLI:       true,
		},
		{
			name:            "non coding provider never gets tmux",
			provider:        string(llm.ProviderOpenAI),
			allowPersistent: true,
		},
		{
			name:     "noninteractive coding agent never persists",
			provider: string(llm.ProviderClaudeCode),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClaudeCode, gotCodexCLI, gotCursorCLI, gotPiCLI := codingAgentPersistentInteractiveFlags(tt.provider, tt.allowPersistent, tt.structured)
			if gotClaudeCode != tt.wantClaudeCode || gotCodexCLI != tt.wantCodexCLI || gotCursorCLI != tt.wantCursorCLI || gotPiCLI != tt.wantPiCLI {
				t.Fatalf("flags = (%v, %v, %v, %v), want (%v, %v, %v, %v)", gotClaudeCode, gotCodexCLI, gotCursorCLI, gotPiCLI, tt.wantClaudeCode, tt.wantCodexCLI, tt.wantCursorCLI, tt.wantPiCLI)
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
			gotClaudeCode, gotCodexCLI, gotCursorCLI, gotPiCLI := codingAgentPersistentInteractiveFlags(string(contract.Provider), true, false)
			count := 0
			for _, enabled := range []bool{gotClaudeCode, gotCodexCLI, gotCursorCLI, gotPiCLI} {
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

func TestCodingAgentUsesStructuredTransportForChat(t *testing.T) {
	tests := []struct {
		name            string
		provider        string
		policy          string
		workflowBuilder bool
		wantStructured  bool
	}{
		{name: "Cursor ordinary chat stays structured", provider: "cursor-cli", wantStructured: true},
		{name: "Cursor workflow builder uses tmux", provider: "cursor-cli", workflowBuilder: true},
		{name: "Cursor workflow builder overrides structured profile", provider: "cursor-cli", policy: "structured", workflowBuilder: true},
		{name: "Claude workflow builder keeps provider default", provider: "claude-code", workflowBuilder: true},
		{name: "explicit structured Claude remains structured", provider: "claude-code", policy: "structured", workflowBuilder: true, wantStructured: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := codingAgentUsesStructuredTransportForChat(tc.provider, tc.policy, tc.workflowBuilder); got != tc.wantStructured {
				t.Fatalf("structured = %v, want %v", got, tc.wantStructured)
			}
		})
	}
}

func TestCodingAgentUsesStructuredTransport(t *testing.T) {
	if !codingAgentUsesStructuredTransport(string(llm.ProviderCursorCLI)) {
		t.Fatal("Cursor must use structured transport")
	}
	if codingAgentUsesStructuredTransport(string(llm.ProviderClaudeCode)) {
		t.Fatal("Claude Code must retain its configured transport")
	}
}

func TestCodingAgentUsesStructuredTransportForPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, provider, policy string
		want                   bool
	}{
		{"auto keeps Cursor structured default", "cursor-cli", "auto", true},
		{"auto keeps Claude tmux default", "claude-code", "auto", false},
		{"Video Studio policy structures Claude", "claude-code", "structured", true},
		{"Video Studio policy structures Codex", "codex-cli", "structured", true},
		{"explicit tmux overrides Cursor default", "cursor-cli", "tmux", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := codingAgentUsesStructuredTransportForPolicy(tc.provider, tc.policy); got != tc.want {
				t.Fatalf("codingAgentUsesStructuredTransportForPolicy(%q, %q) = %v, want %v", tc.provider, tc.policy, got, tc.want)
			}
		})
	}
}

func TestCodingAgentRequestAllowsPersistentInteractive(t *testing.T) {
	tests := []struct {
		name      string
		req       *QueryRequest
		sessionID string
		want      bool
	}{
		{name: "ordinary user chat", req: &QueryRequest{AgentMode: "multi-agent"}, sessionID: "chat-1", want: true},
		{name: "ordinary workflow builder chat", req: &QueryRequest{AgentMode: "workflow_phase", PhaseID: "workflow-builder"}, sessionID: "builder-chat-1", want: true},
		{name: "scheduled workflow builder phase is not interactive", req: &QueryRequest{AgentMode: "workflow_phase", PhaseID: "workflow-builder", TriggeredBy: "cron"}, sessionID: "schedule-builder-1"},
		{name: "scheduled trigger", req: &QueryRequest{TriggeredBy: "cron"}, sessionID: "chat-1"},
		{name: "scheduled session id", req: &QueryRequest{}, sessionID: "schedule-manual--123"},
		{name: "scheduled consecutive turns retain native CLI", req: &QueryRequest{TriggeredBy: "cron", KeepNativeSessionAlive: true}, sessionID: "schedule-manual--123", want: true},
		{name: "background child", req: &QueryRequest{ParentSessionID: "parent-1"}, sessionID: "child-1"},
		{name: "background child never retains native CLI", req: &QueryRequest{ParentSessionID: "parent-1", KeepNativeSessionAlive: true}, sessionID: "schedule-manual--123"},
		{name: "typed pulse stage", req: &QueryRequest{SessionKind: "pulse_reviewer"}, sessionID: "review-1"},
		{name: "converted schedule keeps identity and becomes persistent", req: &QueryRequest{UserInteractiveContinuation: true}, sessionID: "schedule-manual--123", want: true},
		{name: "auto notification cannot promote schedule", req: &QueryRequest{UserInteractiveContinuation: true, IsAutoNotification: true}, sessionID: "schedule-manual--123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codingAgentRequestAllowsPersistentInteractive(tt.req, tt.sessionID); got != tt.want {
				t.Fatalf("allows persistent = %v, want %v", got, tt.want)
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

func TestDelegatedCodingAgentRuntimeFolderIsPerAgent(t *testing.T) {
	got := delegatedCodingAgentRuntimeFolder("alice", "Save Memory/agent #1")
	want := "_users/alice/Chats/.agents/Save-Memory-agent-1"
	if got != want {
		t.Fatalf("delegated runtime folder = %q, want %q", got, want)
	}

	got = delegatedCodingAgentRuntimeFolder("../bad-user", "worker")
	want = "_users/default/Chats/.agents/worker"
	if got != want {
		t.Fatalf("delegated runtime folder with unsafe user = %q, want %q", got, want)
	}
}

func TestTopLevelTierModelDoesNotOverrideExplicitChatLLM(t *testing.T) {
	t.Setenv("WORKSPACE_API_URL", "http://127.0.0.1:9999")
	req := QueryRequest{
		Provider: "codex-cli",
		ModelID:  "high",
		LLMConfig: &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: "codex-cli",
				ModelID:  "high",
			},
		},
		DelegationTierConfig: &virtualtools.DelegationTierConfig{
			High: &virtualtools.TierModel{
				Provider: "claude-code",
				ModelID:  "claude-sonnet-4-6",
			},
		},
	}

	gotProvider, gotModel, _, applied := applyTopLevelDelegationModel(context.Background(), req, "codex-cli", "high", nil)
	if applied {
		t.Fatal("tier model was applied despite an explicit chat LLM selection")
	}
	if gotProvider != "codex-cli" || gotModel != "high" {
		t.Fatalf("resolved chat LLM = %s/%s, want codex-cli/high", gotProvider, gotModel)
	}
}

func TestTopLevelTierModelAppliesWhenChatLLMIsMissing(t *testing.T) {
	t.Setenv("WORKSPACE_API_URL", "http://127.0.0.1:9999")
	req := QueryRequest{
		DelegationTierConfig: &virtualtools.DelegationTierConfig{
			High: &virtualtools.TierModel{
				Provider: "claude-code",
				ModelID:  "claude-sonnet-4-6",
			},
		},
	}

	gotProvider, gotModel, _, applied := applyTopLevelDelegationModel(context.Background(), req, "", "", nil)
	if !applied {
		t.Fatal("tier model was not applied for a request with no chat LLM selection")
	}
	if gotProvider != "claude-code" || gotModel != "claude-sonnet-4-6" {
		t.Fatalf("resolved chat LLM = %s/%s, want claude-code/claude-sonnet-4-6", gotProvider, gotModel)
	}
}

func TestProviderProfileOverridesStaleExplicitChatLLM(t *testing.T) {
	t.Setenv("WORKSPACE_API_URL", "http://127.0.0.1:9999")
	req := QueryRequest{
		Provider: "openrouter",
		ModelID:  "grok-1",
		LLMConfig: &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: "openrouter",
				ModelID:  "grok-1",
			},
		},
		DelegationTierConfig: &virtualtools.DelegationTierConfig{
			SchemaVersion: 2,
			Mode:          "provider_profile",
			Provider:      "codex-cli",
		},
	}

	gotProvider, gotModel, _, applied := applyTopLevelDelegationModel(
		context.Background(), req, "openrouter", "grok-1", nil,
	)
	if !applied {
		t.Fatal("provider profile did not override the stale explicit chat LLM")
	}
	if gotProvider != "codex-cli" || gotModel != "gpt-5.6-sol" {
		t.Fatalf("resolved chat LLM = %s/%s, want codex-cli/gpt-5.6-sol", gotProvider, gotModel)
	}
}

func TestRecordLiveCodingAgentUserMessageCapturesVisibleEvent(t *testing.T) {
	tests := []struct {
		name     string
		provider llm.Provider
	}{
		{name: "claude code", provider: llm.ProviderClaudeCode},
		{name: "codex cli", provider: llm.ProviderCodexCLI},
		{name: "cursor cli", provider: llm.ProviderCursorCLI},
		{name: "pi cli", provider: llm.ProviderPiCLI},
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

func TestHandleLiveInputMessageRoutesThroughAgentDelivery(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "queued-delivery-session"
	runningAgent := testCodingAgent(llm.ProviderOpenAI, "gpt-5")
	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() {}},
		agentCancelMux:   sync.RWMutex{},
	}

	body := bytes.NewBufferString(`{"message":"send this through delivery"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rr := httptest.NewRecorder()

	api.handleLiveInputMessage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	var response LiveInputResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DeliveryStatus != "queued_for_injection" {
		t.Fatalf("delivery_status = %q, want queued_for_injection", response.DeliveryStatus)
	}
}

func TestHandleLiveInputMessageBusyCodingAgentDeliversExactlyOnce(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "busy-codex-live-input"
	const message = "apply this follow-up while processing"
	runningAgent := testCodingAgent(llm.ProviderCodexCLI, "gpt-5.6-sol")
	var deliveryCalls atomic.Int32
	var nextTurnCalls atomic.Int32
	cancelCalled := false
	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() { cancelCalled = true }},
		agentCancelMux:   sync.RWMutex{},
		internalUserMessageDeliveryHandler: func(_ context.Context, gotAgent *mcpagent.Agent, req mcpagent.UserMessageDeliveryRequest) (mcpagent.UserMessageDeliveryResult, error) {
			deliveryCalls.Add(1)
			if gotAgent != runningAgent {
				t.Fatalf("delivery agent = %p, want retained agent %p", gotAgent, runningAgent)
			}
			if req.SessionID != sessionID || req.Message != message || req.Intent != mcpagent.UserMessageDeliveryIntentLiveInput {
				t.Fatalf("delivery request = %#v", req)
			}
			return mcpagent.UserMessageDeliveryResult{
				Provider:       llm.ProviderCodexCLI,
				DeliveryStatus: mcpagent.UserMessageDeliveryStatusSentToCLI,
				Transport:      llm.CodingAgentTransportTmux,
			}, nil
		},
		internalQueryHandler: func(http.ResponseWriter, *http.Request) {
			nextTurnCalls.Add(1)
		},
	}

	body := bytes.NewBufferString(`{"message":"` + message + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rr := httptest.NewRecorder()
	started := time.Now()

	api.handleLiveInputMessage(rr, req)
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("busy live-input acknowledgement took %s, want under 1s", elapsed)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var response LiveInputResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DeliveryStatus != "sent_to_cli" || response.MessageID == "" {
		t.Fatalf("response = %#v, want confirmed CLI delivery", response)
	}
	if got := deliveryCalls.Load(); got != 1 {
		t.Fatalf("delivery calls = %d, want exactly 1", got)
	}
	if got := nextTurnCalls.Load(); got != 0 {
		t.Fatalf("next-turn calls = %d, want 0 after confirmed live delivery", got)
	}
	if cancelCalled || !api.hasActiveTurnCancel(sessionID) {
		t.Fatal("confirmed live delivery must keep the current foreground turn active")
	}

	events := store.GetAllEventsRaw(sessionID)
	if len(events) != 1 {
		t.Fatalf("recorded events = %d, want exactly 1", len(events))
	}
	payload, ok := events[0].Data.Data.(*pkgevents.UserMessageEvent)
	if !ok {
		t.Fatalf("event payload = %T, want *UserMessageEvent", events[0].Data.Data)
	}
	if payload.Content != message || payload.Metadata["message_id"] != response.MessageID || payload.Metadata["delivery_status"] != "sent_to_cli" {
		t.Fatalf("recorded live-input payload = %#v", payload)
	}
}

func TestSyntheticTurnRunningAgentRegistrationDeliversLiveInputAndPreservesReplacement(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "synthetic-codex-live-input"
	const message = "apply this while the synthetic turn is processing"
	syntheticAgent := testCodingAgent(llm.ProviderCodexCLI, "gpt-5.6-sol")
	var deliveryCalls atomic.Int32
	var nextTurnCalls atomic.Int32

	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{},
		runningAgentsMux: sync.RWMutex{},
		// executeSyntheticTurn installs this cancel handle before registering
		// the stored agent, so live-input treats it as the active turn.
		agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() {}},
		agentCancelMux:   sync.RWMutex{},
		internalUserMessageDeliveryHandler: func(_ context.Context, gotAgent *mcpagent.Agent, req mcpagent.UserMessageDeliveryRequest) (mcpagent.UserMessageDeliveryResult, error) {
			deliveryCalls.Add(1)
			if gotAgent != syntheticAgent {
				t.Fatalf("delivery agent = %p, want synthetic agent %p", gotAgent, syntheticAgent)
			}
			if req.SessionID != sessionID || req.Message != message || req.Intent != mcpagent.UserMessageDeliveryIntentLiveInput {
				t.Fatalf("delivery request = %#v", req)
			}
			return mcpagent.UserMessageDeliveryResult{
				Provider:       llm.ProviderCodexCLI,
				DeliveryStatus: mcpagent.UserMessageDeliveryStatusSentToCLI,
				Transport:      llm.CodingAgentTransportTmux,
			}, nil
		},
		internalQueryHandler: func(http.ResponseWriter, *http.Request) {
			nextTurnCalls.Add(1)
		},
	}

	cleanupSyntheticRegistration := api.registerRunningAgentForTurn(sessionID, syntheticAgent)
	body := bytes.NewBufferString(`{"message":"` + message + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rr := httptest.NewRecorder()

	api.handleLiveInputMessage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var response LiveInputResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DeliveryStatus != "sent_to_cli" {
		t.Fatalf("delivery_status = %q, want sent_to_cli", response.DeliveryStatus)
	}
	if got := deliveryCalls.Load(); got != 1 {
		t.Fatalf("delivery calls = %d, want exactly 1", got)
	}
	if got := nextTurnCalls.Load(); got != 0 {
		t.Fatalf("next-turn calls = %d, want 0", got)
	}

	// Reproduce the completion-boundary race: a newer turn replaces the map
	// entry before the synthetic turn's deferred cleanup runs. Old cleanup must
	// leave the newer agent registered.
	newerAgent := testCodingAgent(llm.ProviderCodexCLI, "gpt-5.6-sol")
	api.runningAgentsMux.Lock()
	api.runningAgents[sessionID] = newerAgent
	api.runningAgentsMux.Unlock()
	cleanupSyntheticRegistration()

	api.runningAgentsMux.RLock()
	gotAgent := api.runningAgents[sessionID]
	api.runningAgentsMux.RUnlock()
	if gotAgent != newerAgent {
		t.Fatalf("synthetic cleanup removed/replaced newer agent: got %p, want %p", gotAgent, newerAgent)
	}
}

func TestHandleLiveInputMessageCompletionBoundaryChoosesExactlyOneRoute(t *testing.T) {
	t.Run("completion during confirmed delivery stays live-only", func(t *testing.T) {
		store := internalevents.NewEventStore(10)
		defer store.Stop()

		const sessionID = "completion-during-delivery"
		runningAgent := testCodingAgent(llm.ProviderCodexCLI, "gpt-5.6-sol")
		var deliveryCalls atomic.Int32
		var nextTurnCalls atomic.Int32
		var api *StreamingAPI
		api = &StreamingAPI{
			eventStore:       store,
			runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
			runningAgentsMux: sync.RWMutex{},
			agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() {}},
			agentCancelMux:   sync.RWMutex{},
			internalUserMessageDeliveryHandler: func(_ context.Context, _ *mcpagent.Agent, _ mcpagent.UserMessageDeliveryRequest) (mcpagent.UserMessageDeliveryResult, error) {
				deliveryCalls.Add(1)
				api.agentCancelMux.Lock()
				delete(api.agentCancelFuncs, sessionID)
				api.agentCancelMux.Unlock()
				return mcpagent.UserMessageDeliveryResult{Provider: llm.ProviderCodexCLI, DeliveryStatus: mcpagent.UserMessageDeliveryStatusSentToCLI}, nil
			},
			internalQueryHandler: func(http.ResponseWriter, *http.Request) { nextTurnCalls.Add(1) },
		}

		body := bytes.NewBufferString(`{"message":"boundary follow-up"}`)
		req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body), map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()
		api.handleLiveInputMessage(rr, req)

		if rr.Code != http.StatusOK || deliveryCalls.Load() != 1 || nextTurnCalls.Load() != 0 {
			t.Fatalf("status=%d delivery=%d next_turn=%d body=%s", rr.Code, deliveryCalls.Load(), nextTurnCalls.Load(), rr.Body.String())
		}
		if got := len(store.GetAllEventsRaw(sessionID)); got != 1 {
			t.Fatalf("recorded events = %d, want exactly 1 live-input event", got)
		}
	})

	t.Run("lost active session never also starts a fallback", func(t *testing.T) {
		const sessionID = "completion-loses-delivery"
		runningAgent := testCodingAgent(llm.ProviderCodexCLI, "gpt-5.6-sol")
		var deliveryCalls atomic.Int32
		var nextTurnCalls atomic.Int32
		var api *StreamingAPI
		api = &StreamingAPI{
			runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
			runningAgentsMux: sync.RWMutex{},
			agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() {}},
			agentCancelMux:   sync.RWMutex{},
			internalUserMessageDeliveryHandler: func(_ context.Context, _ *mcpagent.Agent, _ mcpagent.UserMessageDeliveryRequest) (mcpagent.UserMessageDeliveryResult, error) {
				deliveryCalls.Add(1)
				api.agentCancelMux.Lock()
				delete(api.agentCancelFuncs, sessionID)
				api.agentCancelMux.Unlock()
				return mcpagent.UserMessageDeliveryResult{}, errors.New("tmux session completed during delivery")
			},
			internalQueryHandler: func(http.ResponseWriter, *http.Request) { nextTurnCalls.Add(1) },
		}

		body := bytes.NewBufferString(`{"message":"boundary follow-up"}`)
		req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body), map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()
		api.handleLiveInputMessage(rr, req)

		if rr.Code != http.StatusConflict || deliveryCalls.Load() != 1 || nextTurnCalls.Load() != 0 {
			t.Fatalf("status=%d delivery=%d next_turn=%d body=%s", rr.Code, deliveryCalls.Load(), nextTurnCalls.Load(), rr.Body.String())
		}
	})

	t.Run("completion before request queues only a next turn", func(t *testing.T) {
		const sessionID = "completed-before-delivery"
		runningAgent := testCodingAgent(llm.ProviderCodexCLI, "gpt-5.6-sol")
		var deliveryCalls atomic.Int32
		var nextTurnCalls atomic.Int32
		nextTurnDone := make(chan struct{})
		api := &StreamingAPI{
			runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
			runningAgentsMux: sync.RWMutex{},
			agentCancelFuncs: map[string]context.CancelFunc{},
			agentCancelMux:   sync.RWMutex{},
			lastQueryRequests: map[string]QueryRequest{
				sessionID: {AgentMode: "multi-agent", Provider: string(llm.ProviderCodexCLI), ModelID: "gpt-5.6-sol"},
			},
			internalUserMessageDeliveryHandler: func(_ context.Context, _ *mcpagent.Agent, _ mcpagent.UserMessageDeliveryRequest) (mcpagent.UserMessageDeliveryResult, error) {
				deliveryCalls.Add(1)
				return mcpagent.UserMessageDeliveryResult{}, nil
			},
			internalQueryHandler: func(w http.ResponseWriter, _ *http.Request) {
				nextTurnCalls.Add(1)
				_ = json.NewEncoder(w).Encode(QueryResponse{QueryID: "boundary-next-turn"})
				close(nextTurnDone)
			},
		}

		body := bytes.NewBufferString(`{"message":"boundary follow-up"}`)
		req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body), map[string]string{"session_id": sessionID})
		rr := httptest.NewRecorder()
		api.handleLiveInputMessage(rr, req)
		select {
		case <-nextTurnDone:
		case <-time.After(time.Second):
			t.Fatal("queued next turn did not start")
		}

		if rr.Code != http.StatusOK || deliveryCalls.Load() != 0 || nextTurnCalls.Load() != 1 {
			t.Fatalf("status=%d delivery=%d next_turn=%d body=%s", rr.Code, deliveryCalls.Load(), nextTurnCalls.Load(), rr.Body.String())
		}
		var response LiveInputResponse
		if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if response.DeliveryStatus != "next_turn_started" {
			t.Fatalf("delivery status = %q, want next_turn_started", response.DeliveryStatus)
		}
	})
}

func TestHandleLiveInputMessageRejectsStaleRetainedCodingAgent(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "stale-claude-session"
	runningAgent := testCodingAgent(llm.ProviderClaudeCode, "claude-sonnet-4-6")
	api := &StreamingAPI{
		eventStore:       store,
		terminalStore:    terminals.NewStore(),
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		sessionBusy:      map[string]bool{sessionID: true},
		sessionBusySince: map[string]time.Time{sessionID: time.Now().Add(-time.Minute)},
		sessionBusyMu:    sync.RWMutex{},
	}
	api.terminalStore.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "mlp-claude-live", "claude ready\n> ", 1))

	body := bytes.NewBufferString(`{"message":"this should become a new turn"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rr := httptest.NewRecorder()

	api.handleLiveInputMessage(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409 explicit delivery failure", rr.Code, rr.Body.String())
	}
	if api.isSessionBusy(sessionID) {
		t.Fatal("stale retained session should clear stale busy state even though delivery was rejected")
	}
	if got := len(store.GetAllEventsRaw(sessionID)); got != 0 {
		t.Fatalf("failed delivery recorded %d events, want 0", got)
	}
}

func TestCanSteerSessionRequiresActiveForegroundTurn(t *testing.T) {
	sessionID := "foreground-session"
	runningAgent := testCodingAgent(llm.ProviderClaudeCode, "claude-sonnet-4-6")
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

// A retained agent object without a matching provider-native tmux registration
// is stale. Live delivery must fail explicitly and let /api/query start a clean
// resumed turn instead of silently parking the message in a steer queue.
func TestTryDeliverQueryAsLiveInputBusyStaleCodingAgentFallsThrough(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "busy-coding-session"
	runningAgent := testCodingAgent(llm.ProviderClaudeCode, "claude-sonnet-4-6")
	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() {}}, // active foreground turn → busy
		agentCancelMux:   sync.RWMutex{},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/query", nil)
	req.Header.Set("X-Session-ID", sessionID)
	rr := httptest.NewRecorder()

	if api.tryDeliverQueryAsLiveInput(rr, req, sessionID, "steer me into the running turn", "query_test_busy") {
		t.Fatalf("tryDeliverQueryAsLiveInput = true for stale coding-agent registration; want normal-turn fallback. body=%s", rr.Body.String())
	}
	if got := len(store.GetAllEventsRaw(sessionID)); got != 0 {
		t.Fatalf("recorded %d events, want 0 for an unconfirmed send", got)
	}
}

// Single-entry routing: a retained coding-agent CLI should accept the next
// message when there is no foreground-turn/busy proof but the live tmux pane is
// still registered. The CLI owns how to handle the input in its tmux session.
func TestTryDeliverQueryAsLiveInputRetainedCodingAgentWithStaleTmuxFallsThrough(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "retained-coding-session"
	runningAgent := testCodingAgent(llm.ProviderClaudeCode, "claude-sonnet-4-6")
	api := &StreamingAPI{
		eventStore:       store,
		terminalStore:    terminals.NewStore(),
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{}, // no active turn
		agentCancelMux:   sync.RWMutex{},
	}
	api.terminalStore.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "mlp-claude-retained", "claude ready\n> ", 1))

	req := httptest.NewRequest(http.MethodPost, "/api/query", nil)
	rr := httptest.NewRecorder()
	if api.tryDeliverQueryAsLiveInput(rr, req, sessionID, "next message", "query_test_retained") {
		t.Fatalf("tryDeliverQueryAsLiveInput = true for stale provider registration; want normal-turn fallback. body=%s", rr.Body.String())
	}
	if got := len(store.GetAllEventsRaw(sessionID)); got != 0 {
		t.Fatalf("failed retained CLI delivery recorded %d events, want 0", got)
	}
}

func TestTryDeliverQueryAsLiveInputReactivatesSettledRetainedTmux(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "settled-retained-coding-session"
	const terminalID = sessionID + ":main:" + sessionID
	runningAgent := testCodingAgent(llm.ProviderClaudeCode, "claude-sonnet-4-6")
	terminalStore := terminals.NewStore()
	terminalStore.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(
		time.Now(),
		sessionID,
		"main:"+sessionID,
		"mlp-claude-retained",
	))
	if _, ok := terminalStore.MarkTurnCompleted(terminalID); !ok {
		t.Fatal("expected to settle retained terminal before follow-up")
	}

	api := &StreamingAPI{
		eventStore:       store,
		terminalStore:    terminalStore,
		activeSessions:   map[string]*ActiveSessionInfo{sessionID: {SessionID: sessionID, Status: "completed"}},
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{},
		agentCancelMux:   sync.RWMutex{},
		internalUserMessageDeliveryHandler: func(_ context.Context, _ *mcpagent.Agent, _ mcpagent.UserMessageDeliveryRequest) (mcpagent.UserMessageDeliveryResult, error) {
			t.Fatal("live main tmux must be delivered through the durable provider path before the Go Agent map")
			return mcpagent.UserMessageDeliveryResult{}, nil
		},
		internalRetainedTerminalInputHandler: func(_ context.Context, provider llmproviders.Provider, modelID, ownerSessionID, message string) error {
			if provider != llmproviders.ProviderClaudeCode || modelID != "" || ownerSessionID != sessionID || message != "continue the retained turn" {
				t.Fatalf("retained delivery = provider=%q model=%q session=%q message=%q", provider, modelID, ownerSessionID, message)
			}
			return nil
		},
	}
	store.SetEventAddedCallback(func(ownerSessionID string, event internalevents.Event) {
		terminalStore.HandleEventWithChange(ownerSessionID, event)
		api.observeRetainedMainTurnEvent(ownerSessionID, event)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/query", nil)
	rr := httptest.NewRecorder()
	if !api.tryDeliverQueryAsLiveInput(rr, req, sessionID, "continue the retained turn", "query_retained_follow_up") {
		t.Fatalf("settled retained tmux did not accept follow-up: status=%d body=%s", rr.Code, rr.Body.String())
	}

	snapshot, ok := terminalStore.GetRaw(terminalID)
	if !ok {
		t.Fatal("reactivated terminal snapshot is missing")
	}
	if !snapshot.Active || snapshot.State != "running" || snapshot.ProcessState != "live" {
		t.Fatalf("terminal lifecycle = active %v state %q process %q, want running/live", snapshot.Active, snapshot.State, snapshot.ProcessState)
	}
	if !api.sessionHasRetainedCodingTmux(sessionID) || !api.sessionHasLiveMainCodingTmux(sessionID) {
		t.Fatal("reactivated retained main tmux must remain visible to routing and activity monitor")
	}
	if got := api.activeSessions[sessionID].Status; got != "running" {
		t.Fatalf("session status = %q, want running after confirmed follow-up delivery", got)
	}
}

func TestTryDeliverQueryAsLiveInputRetainedCodingAgentWithoutLiveTmuxFallsThrough(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "retained-coding-no-tmux-session"
	runningAgent := testCodingAgent(llm.ProviderClaudeCode, "claude-sonnet-4-6")
	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{}, // no active turn
		agentCancelMux:   sync.RWMutex{},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/query", nil)
	rr := httptest.NewRecorder()
	if api.tryDeliverQueryAsLiveInput(rr, req, sessionID, "next message", "query_test_retained_no_tmx") {
		t.Fatal("tryDeliverQueryAsLiveInput = true without an active foreground turn or live tmux; want normal /api/query path")
	}
	if got := len(store.GetAllEventsRaw(sessionID)); got != 0 {
		t.Fatalf("stale retained CLI fall-through recorded %d events, want 0", got)
	}
}

func TestTryDeliverQueryAsLiveInputNoRetainedAgentFallsThrough(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "missing-coding-session"
	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{},
		runningAgentsMux: sync.RWMutex{},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/query", nil)
	rr := httptest.NewRecorder()
	if api.tryDeliverQueryAsLiveInput(rr, req, sessionID, "first message", "query_test_missing") {
		t.Fatal("tryDeliverQueryAsLiveInput = true without a retained agent; want normal /api/query path")
	}
	if got := len(store.GetAllEventsRaw(sessionID)); got != 0 {
		t.Fatalf("missing-agent fall-through recorded %d events, want 0", got)
	}
}

func TestHandleLiveInputMessageDeliversDirectlyToLiveMainTmuxWithoutAgent(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "live-main-without-agent"
	const terminalID = sessionID + ":main:" + sessionID
	terminalStore := terminals.NewStore()
	terminalStore.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(
		time.Now(), sessionID, "main:"+sessionID, "mlp-claude-retained-direct",
	))
	if _, ok := terminalStore.MarkTurnCompleted(terminalID); !ok {
		t.Fatal("expected retained main terminal to settle before follow-up")
	}

	type liveInputCall struct {
		provider llmproviders.Provider
		modelID  string
		session  string
		message  string
	}
	calls := make(chan liveInputCall, 1)
	api := &StreamingAPI{
		eventStore:       store,
		terminalStore:    terminalStore,
		activeSessions:   map[string]*ActiveSessionInfo{sessionID: {SessionID: sessionID, Status: "completed"}},
		runningAgents:    map[string]*mcpagent.Agent{},
		runningAgentsMux: sync.RWMutex{},
		internalRetainedTerminalInputHandler: func(_ context.Context, provider llmproviders.Provider, modelID, ownerSessionID, message string) error {
			calls <- liveInputCall{provider: provider, modelID: modelID, session: ownerSessionID, message: message}
			return nil
		},
	}
	store.SetEventAddedCallback(func(ownerSessionID string, event internalevents.Event) {
		terminalStore.HandleEventWithChange(ownerSessionID, event)
		api.observeRetainedMainTurnEvent(ownerSessionID, event)
	})

	body := bytes.NewBufferString(`{"message":"deliver directly to the retained Claude pane"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rr := httptest.NewRecorder()

	api.handleLiveInputMessage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var response LiveInputResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Success || response.DeliveryStatus != "sent_to_cli" || response.Provider != string(llm.ProviderClaudeCode) {
		t.Fatalf("response = %#v, want confirmed Claude CLI delivery", response)
	}

	select {
	case got := <-calls:
		if got.provider != llmproviders.ProviderClaudeCode || got.modelID != "" || got.session != sessionID || got.message != "deliver directly to the retained Claude pane" {
			t.Fatalf("retained terminal delivery = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("retained terminal live-input sender was not called")
	}

	snapshot, ok := terminalStore.GetRaw(terminalID)
	if !ok || !snapshot.Active || snapshot.ProcessState != "live" || snapshot.State != "running" {
		t.Fatalf("terminal lifecycle after retained delivery = %#v, want active running/live", snapshot)
	}
	if got := api.activeSessions[sessionID].Status; got != "running" {
		t.Fatalf("session status = %q, want running", got)
	}
	if !api.isSessionBusy(sessionID) {
		t.Fatal("confirmed retained-terminal turn must be authoritatively busy while it is running")
	}
	if runtime, _ := api.authoritativeRuntimeSnapshot(sessionID); runtime.Phase != runtimePhaseRunning {
		t.Fatalf("runtime phase = %q (%s), want running", runtime.Phase, runtime.Reason)
	}
	if got := len(store.GetAllEventsRaw(sessionID)); got != 1 {
		t.Fatalf("recorded event count = %d, want 1", got)
	}

	// A child terminal completion is part of the formatted transcript but must
	// never settle the retained main-agent turn.
	childExecutionID := "workflow-step:child-review"
	store.AddEvent(sessionID, codingAgentTmuxReaperChunkEvent(
		time.Now(), sessionID, childExecutionID, "mlp-claude-child",
	))
	store.AddEvent(sessionID, internalevents.Event{
		Type:          "streaming_end",
		Timestamp:     time.Now(),
		SessionID:     sessionID,
		ExecutionID:   childExecutionID,
		ExecutionKind: "workflow_step",
		Data: &pkgevents.AgentEvent{
			Type: pkgevents.StreamingEnd,
			Data: &pkgevents.StreamingEndEvent{BaseEventData: pkgevents.BaseEventData{Metadata: map[string]interface{}{
				"kind": "terminal", "tmux_session": "mlp-claude-child",
				"execution_kind": "workflow_step", "scope": "workflow_step",
			}}},
		},
	})
	if !api.isSessionBusy(sessionID) {
		t.Fatal("child completion incorrectly settled the retained main-agent turn")
	}

	// The main terminal's structured end event is the same boundary consumed by
	// the Formatted view. It settles the logical turn but retains the live tmux.
	store.AddEvent(sessionID, internalevents.Event{
		Type:          "streaming_end",
		Timestamp:     time.Now(),
		SessionID:     sessionID,
		ExecutionID:   "main:" + sessionID,
		ExecutionKind: "main_agent",
		Data: &pkgevents.AgentEvent{
			Type: pkgevents.StreamingEnd,
			Data: &pkgevents.StreamingEndEvent{BaseEventData: pkgevents.BaseEventData{Metadata: map[string]interface{}{
				"kind": "terminal", "tmux_session": "mlp-claude-retained-direct",
				"execution_kind": "main_agent", "scope": "main_agent",
			}}},
		},
	})
	if api.isSessionBusy(sessionID) {
		t.Fatal("structured main-agent completion did not clear retained-turn busy state")
	}
	if got := api.activeSessions[sessionID].Status; got != "completed" {
		t.Fatalf("session status after structured completion = %q, want completed", got)
	}
	if runtime, _ := api.authoritativeRuntimeSnapshot(sessionID); runtime.Phase != runtimePhaseCompleted {
		t.Fatalf("runtime phase after structured completion = %q (%s), want completed", runtime.Phase, runtime.Reason)
	}
	settled, ok := terminalStore.GetRaw(terminalID)
	if !ok || settled.Active || settled.ProcessState != "live" {
		t.Fatalf("retained terminal after structured completion = %#v, want inactive with live process", settled)
	}
}

func TestRetainedTerminalDeliveryUsesLiveProviderAfterAutomationSwitch(t *testing.T) {
	const sessionID = "workflow-provider-switched"
	terminalStore := terminals.NewStore()
	terminalStore.HandleEvent(sessionID, codingAgentTmuxReaperChunkEvent(
		time.Now(), sessionID, "main:"+sessionID, "mlp-codex-cli-int-switched",
	))

	api := &StreamingAPI{
		terminalStore: terminalStore,
		lastQueryRequests: map[string]QueryRequest{
			sessionID: {
				Provider: string(llm.ProviderClaudeCode),
				ModelID:  "claude-opus-5",
				LLMConfig: &orchestrator.LLMConfig{Primary: orchestrator.LLMModel{
					Provider: string(llm.ProviderClaudeCode),
					ModelID:  "claude-opus-5",
				}},
			},
		},
		internalRetainedTerminalInputHandler: func(_ context.Context, provider llmproviders.Provider, modelID, ownerSessionID, message string) error {
			if provider != llmproviders.ProviderCodexCLI {
				t.Fatalf("provider = %q, want live Codex tmux provider", provider)
			}
			if modelID != "" {
				t.Fatalf("model = %q, want stale Claude model discarded", modelID)
			}
			if ownerSessionID != sessionID || message != "continue with Codex" {
				t.Fatalf("delivery session=%q message=%q", ownerSessionID, message)
			}
			return nil
		},
	}

	provider, handled, err := api.deliverRetainedMainTerminalInput(context.Background(), sessionID, "continue with Codex")
	if err != nil || !handled || provider != string(llm.ProviderCodexCLI) {
		t.Fatalf("delivery provider=%q handled=%v err=%v", provider, handled, err)
	}
}

func TestRetainedTurnCompletionUsesSidecarFinalResponse(t *testing.T) {
	const sessionID = "retained-sidecar-completion"
	const finalResponse = "This is the final assistant response from the sidecar."
	startedAt := time.Now().Add(-2 * time.Second)
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	api := &StreamingAPI{
		eventStore: store,
		retainedMainTurns: map[string]time.Time{
			sessionID: startedAt,
		},
		retainedMainTurnExecutionIDs: map[string]string{
			sessionID: "live-turn:test",
		},
		internalRetainedTurnFinalResponseReader: func(provider llmproviders.Provider, ownerSessionID string, turnStart time.Time) string {
			if provider != llmproviders.ProviderCodexCLI || ownerSessionID != sessionID {
				t.Fatalf("sidecar lookup provider=%q owner=%q", provider, ownerSessionID)
			}
			if !turnStart.Equal(startedAt) {
				t.Fatalf("sidecar lookup turn start=%v, want %v", turnStart, startedAt)
			}
			return finalResponse
		},
	}

	api.emitRetainedMainTurnStreamCompletion(sessionID, terminals.Snapshot{
		TerminalID:  sessionID + ":main:" + sessionID,
		TmuxSession: "mlp-codex-cli-int-retained",
	}, llmproviders.ProviderCodexCLI, "completed", "")

	recorded := store.GetAllEventsRaw(sessionID)
	if len(recorded) != 1 {
		t.Fatalf("recorded events=%d, want 1", len(recorded))
	}
	agentEvent := recorded[0].Data
	if agentEvent == nil {
		t.Fatal("event data is nil, want AgentEvent")
	}
	completion, ok := agentEvent.Data.(*pkgevents.UnifiedCompletionEvent)
	if !ok {
		t.Fatalf("agent event data type=%T, want *events.UnifiedCompletionEvent", agentEvent.Data)
	}
	if completion.FinalResult != finalResponse || completion.Status != "completed" {
		t.Fatalf("completion=%#v", completion)
	}
	if completion.Metadata["source"] != "coding_agent_sidecar" {
		t.Fatalf("completion source=%v", completion.Metadata["source"])
	}
}

// Idle-composer detection and a closed control stream that already sees a
// durable final response can both independently decide the same retained
// turn just finished, and both call emitRetainedMainTurnStreamCompletion for
// it -- reported live as the identical "Agent · Response" answer rendered
// twice in a row. The second call for the same turn must be a no-op.
func TestRetainedTurnCompletionSkipsDuplicateForSameTurn(t *testing.T) {
	const sessionID = "retained-duplicate-completion"
	const finalResponse = "The full route run completed successfully end-to-end."
	startedAt := time.Now().Add(-2 * time.Second)
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	api := &StreamingAPI{
		eventStore: store,
		retainedMainTurns: map[string]time.Time{
			sessionID: startedAt,
		},
		retainedMainTurnExecutionIDs: map[string]string{
			sessionID: "live-turn:test",
		},
		internalRetainedTurnFinalResponseReader: func(llmproviders.Provider, string, time.Time) string {
			return finalResponse
		},
	}

	snapshot := terminals.Snapshot{TerminalID: sessionID + ":main:" + sessionID, TmuxSession: "mlp-codex-cli-int-retained"}
	// The idle-composer path fires first...
	api.emitRetainedMainTurnStreamCompletion(sessionID, snapshot, llmproviders.ProviderCodexCLI, "completed", "")
	// ...then the control-stream-closed path independently reaches the same
	// conclusion for the same turn a few seconds later.
	api.emitRetainedMainTurnStreamCompletion(sessionID, snapshot, llmproviders.ProviderCodexCLI, "completed", "")

	recorded := store.GetAllEventsRaw(sessionID)
	if len(recorded) != 1 {
		t.Fatalf("recorded events=%d, want 1 (duplicate completion for the same turn must be suppressed)", len(recorded))
	}
}

// A genuinely new turn (retainedMainTurns advances to a new start time) must
// still get its own completion -- the guard is per-turn, not per-session.
func TestRetainedTurnCompletionEmitsAgainForANewTurn(t *testing.T) {
	const sessionID = "retained-new-turn-completion"
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	api := &StreamingAPI{
		eventStore: store,
		retainedMainTurns: map[string]time.Time{
			sessionID: time.Now().Add(-2 * time.Second),
		},
		retainedMainTurnExecutionIDs: map[string]string{
			sessionID: "live-turn:test",
		},
		internalRetainedTurnFinalResponseReader: func(llmproviders.Provider, string, time.Time) string {
			return "first turn's answer"
		},
	}
	snapshot := terminals.Snapshot{TerminalID: sessionID + ":main:" + sessionID, TmuxSession: "mlp-codex-cli-int-retained"}
	api.emitRetainedMainTurnStreamCompletion(sessionID, snapshot, llmproviders.ProviderCodexCLI, "completed", "")

	// A new turn starts, advancing the tracked start time.
	api.retainedMainTurns[sessionID] = time.Now()
	api.internalRetainedTurnFinalResponseReader = func(llmproviders.Provider, string, time.Time) string {
		return "second turn's answer"
	}
	api.emitRetainedMainTurnStreamCompletion(sessionID, snapshot, llmproviders.ProviderCodexCLI, "completed", "")

	recorded := store.GetAllEventsRaw(sessionID)
	if len(recorded) != 2 {
		t.Fatalf("recorded events=%d, want 2 (a new turn must still get its own completion)", len(recorded))
	}
}

// API/LLM unchanged: even when busy, a non-coding (API) agent must NOT be
// short-circuited — those keep their frontend steer-vs-queue path.
func TestTryDeliverQueryAsLiveInputSkipsNonCodingAgent(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "busy-llm-session"
	runningAgent := testCodingAgent(llm.ProviderOpenAI, "gpt-5")
	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() {}}, // busy
		agentCancelMux:   sync.RWMutex{},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/query", nil)
	rr := httptest.NewRecorder()
	if api.tryDeliverQueryAsLiveInput(rr, req, sessionID, "llm message", "query_test_llm") {
		t.Fatal("tryDeliverQueryAsLiveInput = true for an API/LLM agent; want false (API/LLM routing unchanged)")
	}
}

func TestRequestLLMConfigOverridesManifestOnlyForAgentProfileSource(t *testing.T) {
	req := QueryRequest{
		LLMConfig: &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{Provider: "claude-code", ModelID: "claude-sonnet-5"},
		},
	}
	if requestLLMConfigOverridesManifest(req) {
		t.Fatal("untagged request LLM config should not override workflow manifest phase LLM")
	}

	req.LLMConfigSource = llmConfigSourceAgentProfile
	if !requestLLMConfigOverridesManifest(req) {
		t.Fatal("agent-profile LLM config should override workflow manifest phase LLM")
	}

	// A scheduler Pulse turn no longer overrides the manifest: the whole
	// scheduled conversation stays on the Builder model, and pulse_llm is
	// applied by the workshop to the background review agents instead.
	req.LLMConfigSource = "scheduled_pulse"
	if requestLLMConfigOverridesManifest(req) {
		t.Fatal("the retired scheduled_pulse source must not override the workflow manifest")
	}

	req.LLMConfigSource = "manual"
	if requestLLMConfigOverridesManifest(req) {
		t.Fatal("manual request LLM config should keep workflow manifest as source of truth")
	}
}

func TestSessionInputLaneSerializesRapidInteractiveSubmits(t *testing.T) {
	sessionID := "rapid-submit-session"
	api := &StreamingAPI{
		sessionInputLanes: make(map[string]*sessionInputLane),
	}

	releaseFirst := api.lockSessionInputLane(sessionID)
	attemptingSecond := make(chan struct{})
	acquiredSecond := make(chan struct{})
	releasedSecond := make(chan struct{})

	go func() {
		close(attemptingSecond)
		releaseSecond := api.lockSessionInputLane(sessionID)
		close(acquiredSecond)
		releaseSecond()
		close(releasedSecond)
	}()

	<-attemptingSecond
	select {
	case <-acquiredSecond:
		t.Fatal("second submit acquired the same session input lane before the first released it")
	case <-time.After(75 * time.Millisecond):
	}

	releaseFirst()
	select {
	case <-acquiredSecond:
	case <-time.After(time.Second):
		t.Fatal("second submit did not acquire the input lane after the first released it")
	}
	<-releasedSecond
	api.sessionInputLanesMu.Lock()
	remainingLanes := len(api.sessionInputLanes)
	api.sessionInputLanesMu.Unlock()
	if remainingLanes != 0 {
		t.Fatalf("session input lane registry retained %d idle lane(s), want 0", remainingLanes)
	}
}

func TestLiveInputDoesNotWaitForQueryLaunchLane(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "live-input-during-launch"
	runningAgent := testCodingAgent(llm.ProviderClaudeCode, "claude-sonnet-4-6")
	api := &StreamingAPI{
		eventStore:        store,
		terminalStore:     terminals.NewStore(),
		runningAgents:     map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux:  sync.RWMutex{},
		agentCancelFuncs:  map[string]context.CancelFunc{sessionID: func() {}},
		agentCancelMux:    sync.RWMutex{},
		sessionInputLanes: make(map[string]*sessionInputLane),
	}
	api.terminalStore.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, "missing-test-tmux", "claude ready\n> ", 1))

	releaseLaunchLane := api.lockSessionInputLane(sessionID)
	defer releaseLaunchLane()

	body := bytes.NewBufferString(`{"message":"must not wait behind launch"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", body)
	req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		api.handleLiveInputMessage(rr, req)
		close(done)
	}()

	select {
	case <-done:
		if rr.Code != http.StatusConflict {
			t.Fatalf("status = %d body=%s, want explicit delivery failure without blocking", rr.Code, rr.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("live input blocked behind the long-running query launch lane")
	}
}

func TestStartNextTurnFromLiveInputAcknowledgesBeforeQueuedTurnRuns(t *testing.T) {
	const sessionID = "queued-next-turn"
	handlerStarted := make(chan *http.Request, 1)
	releaseHandler := make(chan struct{})
	handlerDone := make(chan struct{})

	api := &StreamingAPI{
		lastQueryRequests: map[string]QueryRequest{
			sessionID: {
				AgentMode: "multi-agent",
				Provider:  string(llm.ProviderCodexCLI),
				ModelID:   "gpt-5.6-sol",
			},
		},
		internalQueryHandler: func(w http.ResponseWriter, req *http.Request) {
			handlerStarted <- req
			<-releaseHandler
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(QueryResponse{QueryID: "queued-query"})
			close(handlerDone)
		},
	}

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", nil).WithContext(requestCtx)
	req.Header.Set("X-Session-ID", sessionID)
	rr := httptest.NewRecorder()

	returned := make(chan bool, 1)
	go func() {
		returned <- api.startNextTurnFromLiveInput(rr, req, sessionID, "send after this turn", nil)
	}()

	select {
	case handled := <-returned:
		if !handled {
			t.Fatal("startNextTurnFromLiveInput returned false")
		}
	case <-time.After(time.Second):
		t.Fatal("live-input acknowledgement waited for the queued next turn")
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var response LiveInputResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.DeliveryStatus != "next_turn_started" || response.MessageID == "" {
		t.Fatalf("response = %#v, want accepted next turn with message ID", response)
	}

	var queuedReq *http.Request
	select {
	case queuedReq = <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("queued next-turn handler did not start")
	}
	cancelRequest()
	if err := queuedReq.Context().Err(); err != nil {
		t.Fatalf("queued next-turn context inherited live-input cancellation: %v", err)
	}
	close(releaseHandler)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("queued next-turn handler did not finish")
	}
}

func TestStartNextTurnFromLiveInputReturnsConflictWhenBuilderChatActuallyBusy(t *testing.T) {
	const sessionID = "queued-next-turn"
	now := time.Now().UTC()
	api := &StreamingAPI{
		lastQueryRequests: map[string]QueryRequest{
			sessionID: {
				AgentMode:      "workflow_phase",
				PhaseID:        "workflow-builder",
				SelectedFolder: "Workflow/social-media",
				Provider:       string(llm.ProviderClaudeCode),
				ModelID:        "claude-opus-4-8",
			},
		},
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"builder-chat": {
				ExecutionID:   "builder-chat",
				SessionID:     "different-builder-session",
				Source:        trackedExecutionSourceWorkshopBackground,
				Kind:          "workflow_builder_task",
				WorkspacePath: "Workflow/social-media",
				PhaseID:       "workflow-builder",
				Status:        trackedExecutionStatusRunning,
				TriggeredBy:   "workflow_builder",
				StartedAt:     now,
			},
		},
		internalQueryHandler: func(w http.ResponseWriter, req *http.Request) {
			t.Fatal("queued next-turn handler must not start when builder chat is busy")
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", nil)
	req.Header.Set("X-Session-ID", sessionID)
	rr := httptest.NewRecorder()

	if !api.startNextTurnFromLiveInput(rr, req, sessionID, "send after this turn", nil) {
		t.Fatal("startNextTurnFromLiveInput returned false")
	}
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "workflow_busy") {
		t.Fatalf("body = %s, want workflow_busy", rr.Body.String())
	}
}

func TestStartNextTurnFromLiveInputDoesNotBlockScheduledMessageSequence(t *testing.T) {
	const sessionID = "interactive-builder-session"
	now := time.Now().UTC()
	handled := make(chan QueryRequest, 1)
	api := &StreamingAPI{
		lastQueryRequests: map[string]QueryRequest{
			sessionID: {
				AgentMode:      "workflow_phase",
				PhaseID:        "workflow-builder",
				SelectedFolder: "Workflow/social-media",
				Provider:       string(llm.ProviderClaudeCode),
				ModelID:        "claude-opus-4-8",
			},
		},
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"scheduled-message-sequence": {
				ExecutionID:   "msgseq-execute-allocate-execute-and-verify-1",
				SessionID:     "schedule-cron--social-media_1",
				Source:        trackedExecutionSourceWorkshopBackground,
				Kind:          "workflow_builder_task",
				WorkspacePath: "Workflow/social-media",
				PhaseID:       "workflow-builder",
				Status:        trackedExecutionStatusRunning,
				TriggeredBy:   "workflow_builder",
				StartedAt:     now,
			},
		},
		internalQueryHandler: func(w http.ResponseWriter, req *http.Request) {
			var got QueryRequest
			if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
				t.Errorf("decode queued request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			handled <- got
			w.WriteHeader(http.StatusOK)
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/live-input", nil)
	req.Header.Set("X-Session-ID", sessionID)
	rr := httptest.NewRecorder()

	if !api.startNextTurnFromLiveInput(rr, req, sessionID, "tell me the strategies", nil) {
		t.Fatal("startNextTurnFromLiveInput returned false")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rr.Code, rr.Body.String())
	}

	select {
	case got := <-handled:
		if got.Query != "tell me the strategies" || got.Message != "tell me the strategies" {
			t.Fatalf("queued query = %#v, want the live-input message", got)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduled message-sequence prevented the interactive continuation from starting")
	}
}

func TestQueryRequestForContinuationRestoresWorkflowPhaseContext(t *testing.T) {
	req := QueryRequest{
		AgentMode:     "multi-agent", // adapted engine mode inside handleQuery
		PhaseID:       "workflow-builder",
		PresetQueryID: "wf-marketing",
		Query:         "continue the workflow",
	}

	got := queryRequestForContinuation(req, true, "Workflow/llmprovideropensourcemarketing")
	if got.AgentMode != "workflow_phase" {
		t.Fatalf("agent mode = %q, want workflow_phase", got.AgentMode)
	}
	if got.PhaseID != req.PhaseID || got.PresetQueryID != req.PresetQueryID {
		t.Fatalf("workflow identity changed: phase=%q preset=%q", got.PhaseID, got.PresetQueryID)
	}
	if got.SelectedFolder != "Workflow/llmprovideropensourcemarketing" {
		t.Fatalf("selected folder = %q, want workflow folder", got.SelectedFolder)
	}
}

func TestQueryRequestForContinuationLeavesMultiAgentContextUnchanged(t *testing.T) {
	req := QueryRequest{AgentMode: "multi-agent", Query: "continue chat", SelectedFolder: "custom"}
	got := queryRequestForContinuation(req, false, "Workflow/ignored")
	if got.AgentMode != req.AgentMode || got.SelectedFolder != req.SelectedFolder {
		t.Fatalf("non-workflow continuation changed: got=%#v want=%#v", got, req)
	}
}

func TestQueryRequestWithEffectiveRuntimeReplacesStaleProviderWithoutMutatingSource(t *testing.T) {
	sourceConfig := &orchestrator.LLMConfig{Primary: orchestrator.LLMModel{
		Provider: string(llm.ProviderClaudeCode),
		ModelID:  "claude-opus-5",
	}}
	req := QueryRequest{
		Provider:  string(llm.ProviderClaudeCode),
		ModelID:   "claude-opus-5",
		LLMConfig: sourceConfig,
	}

	got := queryRequestWithEffectiveRuntime(req, string(llm.ProviderCodexCLI), "gpt-5.6-sol")
	if got.Provider != string(llm.ProviderCodexCLI) || got.ModelID != "gpt-5.6-sol" {
		t.Fatalf("effective runtime = %s/%s", got.Provider, got.ModelID)
	}
	if got.LLMConfig == nil || got.LLMConfig.Primary.Provider != string(llm.ProviderCodexCLI) || got.LLMConfig.Primary.ModelID != "gpt-5.6-sol" {
		t.Fatalf("stored LLM config = %#v", got.LLMConfig)
	}
	if sourceConfig.Primary.Provider != string(llm.ProviderClaudeCode) || sourceConfig.Primary.ModelID != "claude-opus-5" {
		t.Fatalf("source request config was mutated: %#v", sourceConfig.Primary)
	}
}

func TestAgentWorksChatQueriesUseInteractiveInputLane(t *testing.T) {
	if !shouldSerializeInteractiveQueryInput(QueryRequest{AgentMode: "multi-agent"}) {
		t.Fatal("AgentWorks multi-agent query must use the session input lane")
	}
	if !shouldSerializeInteractiveQueryInput(QueryRequest{AgentMode: "simple"}) {
		t.Fatal("simple chat query must use the session input lane")
	}
	if !shouldSerializeInteractiveQueryInput(QueryRequest{AgentMode: "multi-agent", IsAutoNotification: true}) {
		t.Fatal("auto-notification turns must share the same full-turn lane")
	}
}

func TestIdleCompletionDoesNotCompleteStaleBusyTurn(t *testing.T) {
	sessionID := "stale-busy-session"
	api := &StreamingAPI{
		sessionBusy:      map[string]bool{sessionID: true},
		sessionBusySince: map[string]time.Time{sessionID: time.Now().Add(-autoNotificationStaleBusyAfter - time.Second)},
		sessionBusyMu:    sync.RWMutex{},
		agentCancelFuncs: map[string]context.CancelFunc{},
		agentCancelMux:   sync.RWMutex{},
		runningAgents:    map[string]*mcpagent.Agent{},
		runningAgentsMux: sync.RWMutex{},
	}

	if api.shouldCompleteIdleForegroundSession(sessionID, "running", false) {
		t.Fatal("stale busy turn should not be completed by passive idle polling")
	}
	if !api.isSessionBusy(sessionID) {
		t.Fatal("idle-completion check must not clear the busy flag")
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
	if event.Type != string(pkgevents.UserMessage) {
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
