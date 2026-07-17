package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	pkgevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
)

func TestCodingAgentPersistentInteractiveFlags(t *testing.T) {
	tests := []struct {
		name           string
		provider       string
		wantClaudeCode bool
		wantCodexCLI   bool
		wantCursorCLI  bool
		wantAgyCLI     bool
		wantPiCLI      bool
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
			name:      "pi chat gets persistent tmux",
			provider:  string(llm.ProviderPiCLI),
			wantPiCLI: true,
		},
		{
			name:     "non coding provider never gets tmux",
			provider: string(llm.ProviderOpenAI),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClaudeCode, gotCodexCLI, gotCursorCLI, gotAgyCLI, gotPiCLI := codingAgentPersistentInteractiveFlags(tt.provider)
			if gotClaudeCode != tt.wantClaudeCode || gotCodexCLI != tt.wantCodexCLI || gotCursorCLI != tt.wantCursorCLI || gotAgyCLI != tt.wantAgyCLI || gotPiCLI != tt.wantPiCLI {
				t.Fatalf("flags = (%v, %v, %v, %v, %v), want (%v, %v, %v, %v, %v)", gotClaudeCode, gotCodexCLI, gotCursorCLI, gotAgyCLI, gotPiCLI, tt.wantClaudeCode, tt.wantCodexCLI, tt.wantCursorCLI, tt.wantAgyCLI, tt.wantPiCLI)
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
			gotClaudeCode, gotCodexCLI, gotCursorCLI, gotAgyCLI, gotPiCLI := codingAgentPersistentInteractiveFlags(string(contract.Provider))
			count := 0
			for _, enabled := range []bool{gotClaudeCode, gotCodexCLI, gotCursorCLI, gotAgyCLI, gotPiCLI} {
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
		{name: "agy cli", provider: llm.ProviderAgyCLI},
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
	runningAgent := &mcpagent.Agent{ModelID: "gpt-5"}
	runningAgent.SetProvider(llm.ProviderOpenAI)
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
	queued := runningAgent.DrainSteerMessages()
	if len(queued) != 1 || queued[0] != "send this through delivery" {
		t.Fatalf("queued messages = %#v", queued)
	}
}

func TestHandleLiveInputMessageBusyCodingAgentDeliversExactlyOnce(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "busy-codex-live-input"
	const message = "apply this follow-up while processing"
	runningAgent := &mcpagent.Agent{ModelID: "gpt-5.6-sol"}
	runningAgent.SetProvider(llm.ProviderCodexCLI)
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
	syntheticAgent := &mcpagent.Agent{ModelID: "gpt-5.6-sol"}
	syntheticAgent.SetProvider(llm.ProviderCodexCLI)
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
	newerAgent := &mcpagent.Agent{ModelID: "gpt-5.6-sol"}
	newerAgent.SetProvider(llm.ProviderCodexCLI)
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
		runningAgent := &mcpagent.Agent{ModelID: "gpt-5.6-sol"}
		runningAgent.SetProvider(llm.ProviderCodexCLI)
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
		runningAgent := &mcpagent.Agent{ModelID: "gpt-5.6-sol"}
		runningAgent.SetProvider(llm.ProviderCodexCLI)
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
		runningAgent := &mcpagent.Agent{ModelID: "gpt-5.6-sol"}
		runningAgent.SetProvider(llm.ProviderCodexCLI)
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
	runningAgent := &mcpagent.Agent{ModelID: "claude-sonnet-4-6"}
	runningAgent.SetProvider(llm.ProviderClaudeCode)
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

// A retained agent object without a matching provider-native tmux registration
// is stale. Live delivery must fail explicitly and let /api/query start a clean
// resumed turn instead of silently parking the message in a steer queue.
func TestTryDeliverQueryAsLiveInputBusyStaleCodingAgentFallsThrough(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "busy-coding-session"
	runningAgent := &mcpagent.Agent{ModelID: "claude-sonnet-4-6"}
	runningAgent.SetProvider(llm.ProviderClaudeCode)
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
	runningAgent := &mcpagent.Agent{ModelID: "claude-sonnet-4-6"}
	runningAgent.SetProvider(llm.ProviderClaudeCode)
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

func TestTryDeliverQueryAsLiveInputRetainedCodingAgentWithoutLiveTmuxFallsThrough(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "retained-coding-no-tmux-session"
	runningAgent := &mcpagent.Agent{ModelID: "claude-sonnet-4-6"}
	runningAgent.SetProvider(llm.ProviderClaudeCode)
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

// API/LLM unchanged: even when busy, a non-coding (API) agent must NOT be
// short-circuited — those keep their frontend steer-vs-queue path.
func TestTryDeliverQueryAsLiveInputSkipsNonCodingAgent(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "busy-llm-session"
	runningAgent := &mcpagent.Agent{ModelID: "gpt-5"}
	runningAgent.SetProvider(llm.ProviderOpenAI)
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

func TestRequestLLMConfigOverridesManifestOnlyForScheduledSources(t *testing.T) {
	req := QueryRequest{
		LLMConfig: &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{Provider: "claude-code", ModelID: "claude-sonnet-5"},
		},
	}
	if requestLLMConfigOverridesManifest(req) {
		t.Fatal("untagged request LLM config should not override workflow manifest phase LLM")
	}

	req.LLMConfigSource = llmConfigSourceScheduledPulse
	if !requestLLMConfigOverridesManifest(req) {
		t.Fatal("scheduled Pulse LLM config should override workflow manifest phase LLM")
	}

	req.LLMConfigSource = llmConfigSourceScheduledAutoImprove
	if !requestLLMConfigOverridesManifest(req) {
		t.Fatal("scheduled Goal Advisor LLM config should override the workflow Builder model")
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
	runningAgent := &mcpagent.Agent{ModelID: "claude-sonnet-4-6"}
	runningAgent.SetProvider(llm.ProviderClaudeCode)
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

func TestChiefOfStaffQueriesUseInteractiveInputLane(t *testing.T) {
	if !shouldSerializeInteractiveQueryInput(QueryRequest{AgentMode: "multi-agent"}) {
		t.Fatal("Chief of Staff multi-agent query must use the session input lane")
	}
	if !shouldSerializeInteractiveQueryInput(QueryRequest{AgentMode: "simple"}) {
		t.Fatal("legacy Chief of Staff simple query must use the session input lane")
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
