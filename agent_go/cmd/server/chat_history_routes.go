package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	agentevents "github.com/manishiitg/mcpagent/events"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	storeevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
)

// ChatHistoryRoutes registers chat history endpoints.
func ChatHistoryRoutes(router *mux.Router, api *StreamingAPI) {
	r := router.PathPrefix("/api/chat-history").Subrouter()
	r.HandleFunc("/sessions", listChatHistoryHandler(api)).Methods("GET")
	r.HandleFunc("/sessions/cleanup", cleanupChatHistoryHandler(api)).Methods("DELETE")
	r.HandleFunc("/restored-terminal", startRestoredTerminalHandler(api)).Methods("POST", "OPTIONS")
	r.HandleFunc("/sessions/{session_id}", getChatHistoryConversationHandler(api)).Methods("GET")
	r.HandleFunc("/sessions/{session_id}", deleteChatHistorySessionHandler(api)).Methods("DELETE")
}

type startRestoredTerminalRequest struct {
	SessionID                     string `json:"session_id"`
	RestoredConversationPath      string `json:"restored_conversation_path,omitempty"`
	RestoredConversationSessionID string `json:"restored_conversation_session_id,omitempty"`
	WorkspacePath                 string `json:"workspace_path,omitempty"`
}

type startRestoredTerminalResponse struct {
	OK       bool                `json:"ok"`
	Started  bool                `json:"started"`
	Reason   string              `json:"reason,omitempty"`
	Terminal *terminals.Snapshot `json:"terminal,omitempty"`
}

func listChatHistoryHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		if userID == "" {
			userID = "default"
		}

		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		workspacePath := r.URL.Query().Get("workspace_path")

		sessions, err := ListChatHistorySessions(userID, limit, offset, workspacePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessions": sessions,
		})
	}
}

func startRestoredTerminalHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if api == nil || api.terminalStore == nil {
			_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: false, Reason: "terminal_store_unavailable"})
			return
		}

		var req startRestoredTerminalRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		if req.SessionID == "" {
			http.Error(w, "session_id is required", http.StatusBadRequest)
			return
		}

		userID := GetUserIDFromContext(r.Context())
		if userID == "" {
			userID = "default"
		}
		persistedTerminalSnapshots, _, snapshotErr := restoredTerminalSnapshots(userID, req)
		if snapshotErr != nil {
			api.logRestoredTerminalf("restore session=%s failed to read persisted terminal snapshots: %v", req.SessionID, snapshotErr)
		}
		runtime, ok, err := restoredTerminalRuntime(userID, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok || runtime == nil {
			if terminal, started, reason := api.restorePersistedTerminalSnapshot(r.Context(), req.SessionID, nil, persistedTerminalSnapshots); started {
				api.logRestoredTerminalInfof("restore session=%s tier=persisted_snapshot result=started no_runtime=true", req.SessionID)
				_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: true, Terminal: terminal})
				return
			} else if reason != "" {
				api.logRestoredTerminalInfof("restore session=%s tier=persisted_snapshot result=skip reason=%s no_runtime=true", req.SessionID, reason)
			}
			api.logRestoredTerminalInfof("restore session=%s user=%s path=%q result=fail reason=runtime_not_found", req.SessionID, userID, req.RestoredConversationPath)
			_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: false, Reason: "runtime_not_found"})
			return
		}

		// Single structured entry describing what we're about to try.
		// Captures the data the 3-tier fallback actually keys off so a
		// failed restore can be diagnosed without re-running.
		recordedTmuxSession, _, _ := restoredRuntimeTmuxSession(runtime)
		api.logRestoredTerminalInfof("restore session=%s user=%s kind=%s provider=%s transport=%s external_session_id=%q tmux_session=%q workspace=%q",
			req.SessionID, userID, runtime.Kind, runtime.Provider,
			restoredRuntimeCodingAgentTransport(runtime),
			strings.TrimSpace(runtime.ExternalSessionID),
			recordedTmuxSession,
			runtime.WorkspacePath,
		)

		// Only the attach-existing tier is safe to run at restore: it reuses
		// a live tmux pane without launching a new CLI process. The two launch
		// launch-based fallback tiers used to fire here, but they hit a
		// tool-registration race —
		// the CLI caches its tool catalog via get_api_spec at launch, before
		// /api/query has registered phase-specific tools like run_full_workflow
		// or execute_step. The CLI then never sees those tools and falls back
		// to shelling out (e.g. agy emits "tool(s) [run_full_workflow] not
		// found" and runs python3 main.py instead).
		//
		// If the tmux pane is gone, defer the launch to the user's next
		// /api/query, which registers the phase tools first and then launches
		// the CLI — same path a fresh chat takes, no race.
		var fallbackReason string
		if terminal, started, reason := api.attachRestoredExistingTmuxTerminal(r.Context(), req.SessionID, runtime); started {
			api.logRestoredTerminalInfof("restore session=%s tier=attach_existing result=started", req.SessionID)
			_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: true, Terminal: terminal})
			return
		} else if reason != "" {
			api.logRestoredTerminalInfof("restore session=%s tier=attach_existing result=skip reason=%s", req.SessionID, reason)
			fallbackReason = reason
		}

		if terminal, started, reason := api.restorePersistedTerminalSnapshot(r.Context(), req.SessionID, runtime, persistedTerminalSnapshots); started {
			api.logRestoredTerminalInfof("restore session=%s tier=persisted_snapshot result=started", req.SessionID)
			_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: true, Terminal: terminal})
			return
		} else if reason != "" {
			api.logRestoredTerminalInfof("restore session=%s tier=persisted_snapshot result=skip reason=%s", req.SessionID, reason)
		}

		if fallbackReason == "" {
			fallbackReason = "tmux_session_not_running"
		}
		api.logRestoredTerminalInfof("restore session=%s result=defer_to_query final_reason=%s (launch tiers skipped to avoid tool-registration race)", req.SessionID, fallbackReason)
		_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: false, Reason: fallbackReason})
	}
}

func restoredTerminalRuntime(userID string, req startRestoredTerminalRequest) (*ChatHistoryAgentRuntime, bool, error) {
	if path := strings.TrimSpace(req.RestoredConversationPath); path != "" {
		return ReadChatHistoryRuntimeFromPath(userID, path)
	}
	if sessionID := strings.TrimSpace(req.RestoredConversationSessionID); sessionID != "" {
		return ReadChatHistoryRuntimeForSession(userID, sessionID, strings.TrimSpace(req.WorkspacePath))
	}
	return nil, false, nil
}

func restoredTerminalSnapshots(userID string, req startRestoredTerminalRequest) ([]terminals.Snapshot, bool, error) {
	if path := strings.TrimSpace(req.RestoredConversationPath); path != "" {
		return ReadChatHistoryTerminalSnapshotsFromPath(userID, path)
	}
	if sessionID := strings.TrimSpace(req.RestoredConversationSessionID); sessionID != "" {
		return ReadChatHistoryTerminalSnapshotsForSession(userID, sessionID, strings.TrimSpace(req.WorkspacePath))
	}
	return nil, false, nil
}

func restoredRuntimeTmuxSession(runtime *ChatHistoryAgentRuntime) (string, bool, string) {
	if runtime == nil || runtime.AgentSessionHandle == nil || runtime.AgentSessionHandle.Empty() {
		return "", false, "agent_session_handle_missing"
	}
	handle := runtime.AgentSessionHandle.Provider
	if restoredRuntimeCodingAgentTransport(runtime) != string(llmproviders.CodingAgentTransportTmux) {
		return "", false, "not_tmux_transport"
	}
	tmuxSession := strings.TrimSpace(handle.TmuxSession)
	if tmuxSession == "" {
		return "", false, "tmux_session_missing"
	}
	return tmuxSession, true, ""
}

func restoredRuntimeCodingAgentTransport(runtime *ChatHistoryAgentRuntime) string {
	if runtime == nil || runtime.Kind != "coding_agent" {
		return ""
	}
	if transport := strings.ToLower(strings.TrimSpace(runtime.Transport)); transport != "" {
		return transport
	}
	provider := strings.ToLower(strings.TrimSpace(runtime.Provider))
	modelID := strings.TrimSpace(runtime.ModelID)
	if runtime.AgentSessionHandle != nil && !runtime.AgentSessionHandle.Empty() {
		handle := runtime.AgentSessionHandle.Provider
		if transport := strings.ToLower(strings.TrimSpace(handle.Transport)); transport != "" {
			return transport
		}
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(handle.Provider))
		}
		if modelID == "" {
			modelID = strings.TrimSpace(handle.Model)
		}
	}
	if provider == "" {
		return ""
	}
	contract, ok := llmproviders.GetCodingAgentProviderContract(llmproviders.Provider(provider), modelID)
	if !ok {
		return ""
	}
	return strings.ToLower(string(contract.Transport))
}

func restoredRuntimeUsesLaunchableTerminalTransport(runtime *ChatHistoryAgentRuntime) bool {
	return restoredRuntimeCodingAgentTransport(runtime) == string(llmproviders.CodingAgentTransportTmux)
}

func (api *StreamingAPI) attachRestoredExistingTmuxTerminal(ctx context.Context, sessionID string, runtime *ChatHistoryAgentRuntime) (*terminals.Snapshot, bool, string) {
	tmuxSession, tmuxOK, reason := restoredRuntimeTmuxSession(runtime)
	if !tmuxOK {
		return nil, false, reason
	}

	captureCtx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
	defer cancel()
	content, err := captureTerminalPane(captureCtx, tmuxSession)
	if err != nil {
		if isMissingTmuxTargetError(err) {
			return nil, false, "tmux_session_not_running"
		}
		return nil, false, "tmux_unavailable"
	}
	api.upsertRestoredTmuxTerminal(sessionID, runtime, tmuxSession, content)
	if snapshot, ok := api.findRestoredTerminalSnapshot(sessionID, tmuxSession); ok {
		enriched := api.enrichTerminalSnapshot(ctx, newTerminalPlanTypeResolver(ctx), snapshot)
		return &enriched, true, ""
	}
	return nil, false, "terminal_snapshot_not_created"
}

func (api *StreamingAPI) restorePersistedTerminalSnapshot(ctx context.Context, sessionID string, runtime *ChatHistoryAgentRuntime, snapshots []terminals.Snapshot) (*terminals.Snapshot, bool, string) {
	if api == nil || api.terminalStore == nil {
		return nil, false, "terminal_store_unavailable"
	}
	snapshot, ok := selectPersistedTerminalSnapshot(snapshots)
	if !ok {
		return nil, false, "persisted_terminal_snapshot_missing"
	}
	if runtime != nil {
		if snapshot.WorkflowPath == "" {
			snapshot.WorkflowPath = strings.TrimSpace(runtime.WorkspacePath)
		}
		if snapshot.Label == "" {
			provider := strings.TrimSpace(runtime.Provider)
			if provider == "" && runtime.AgentSessionHandle != nil {
				provider = strings.TrimSpace(runtime.AgentSessionHandle.Provider.Provider)
			}
			if provider != "" {
				snapshot.Label = "Restored " + provider
			}
		}
	}
	stored, ok := api.terminalStore.UpsertStaticSnapshot(sessionID, snapshot)
	if !ok {
		return nil, false, "persisted_terminal_snapshot_empty"
	}
	enriched := api.enrichTerminalSnapshot(ctx, newTerminalPlanTypeResolver(ctx), stored)
	return &enriched, true, ""
}

func selectPersistedTerminalSnapshot(snapshots []terminals.Snapshot) (terminals.Snapshot, bool) {
	var selected terminals.Snapshot
	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.Content) == "" {
			continue
		}
		if selected.Content == "" || persistedTerminalSnapshotPreferred(snapshot, selected) {
			selected = snapshot
		}
	}
	return selected, strings.TrimSpace(selected.Content) != ""
}

func persistedTerminalSnapshotPreferred(candidate, existing terminals.Snapshot) bool {
	candidateMain := chatHistoryTerminalSnapshotIsMainAgent(candidate)
	existingMain := chatHistoryTerminalSnapshotIsMainAgent(existing)
	if candidateMain != existingMain {
		return candidateMain
	}
	return candidate.UpdatedAt.After(existing.UpdatedAt)
}

func (api *StreamingAPI) materializeRestoredTmuxTerminal(ctx context.Context, sessionID string, runtime *ChatHistoryAgentRuntime, tmuxSession string) (*terminals.Snapshot, bool, string) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return nil, false, "tmux_session_missing"
	}

	// Always capture the live pane and upsert, regardless of whether a bare
	// snapshot already exists. The agent's own event stream may have created a
	// snapshot without workflow_path / provider metadata (which the
	// workflow-mode filter would hide), and the captured content is the most
	// current view of the restored session.
	captureCtx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
	defer cancel()
	content, err := captureTerminalPane(captureCtx, tmuxSession)
	if err != nil {
		if isMissingTmuxTargetError(err) {
			return nil, false, "tmux_session_not_running"
		}
		api.logRestoredTerminalf("Failed to capture restored tmux session %s for chat session %s: %v", tmuxSession, sessionID, err)
		// A capture failure shouldn't fail the whole restore if a usable
		// snapshot already exists — fall back to it rather than erroring out.
		if snapshot, ok := api.findRestoredTerminalSnapshot(sessionID, tmuxSession); ok {
			enriched := api.enrichTerminalSnapshot(ctx, newTerminalPlanTypeResolver(ctx), snapshot)
			return &enriched, true, ""
		}
		return nil, false, "tmux_unavailable"
	}
	api.upsertRestoredTmuxTerminal(sessionID, runtime, tmuxSession, content)
	if snapshot, ok := api.findRestoredTerminalSnapshot(sessionID, tmuxSession); ok {
		enriched := api.enrichTerminalSnapshot(ctx, newTerminalPlanTypeResolver(ctx), snapshot)
		return &enriched, true, ""
	}
	return nil, false, "terminal_snapshot_not_created"
}

func (api *StreamingAPI) logRestoredTerminalf(format string, args ...interface{}) {
	if api == nil || api.logger == nil {
		return
	}
	api.logger.Warn(fmt.Sprintf("[CHAT_HISTORY] "+format, args...))
}

// logRestoredTerminalInfof is the info-level sibling of
// logRestoredTerminalf. Used to trace the 3-tier resume-terminal
// fallback (attach existing → in-memory agent → fresh agent) so a
// failed restore can be diagnosed from the server log without
// rebuilding. Keep these one-liners structured (key=value) so grep
// for a session ID surfaces the full decision trail.
func (api *StreamingAPI) logRestoredTerminalInfof(format string, args ...interface{}) {
	if api == nil || api.logger == nil {
		return
	}
	api.logger.Info(fmt.Sprintf("[CHAT_HISTORY] "+format, args...))
}

func (api *StreamingAPI) upsertRestoredTmuxTerminal(sessionID string, runtime *ChatHistoryAgentRuntime, tmuxSession, content string) {
	if api == nil || api.terminalStore == nil {
		return
	}
	provider := strings.TrimSpace(runtime.Provider)
	if provider == "" && runtime.AgentSessionHandle != nil {
		provider = strings.TrimSpace(runtime.AgentSessionHandle.Provider.Provider)
	}
	now := time.Now()
	api.terminalStore.HandleEvent(sessionID, storeevents.Event{
		Type:          "streaming_chunk",
		Timestamp:     now,
		SessionID:     sessionID,
		ExecutionKind: "main_agent",
		Data: &agentevents.AgentEvent{
			Type:      agentevents.StreamingChunk,
			Timestamp: now,
			SessionID: sessionID,
			Data: &agentevents.StreamingChunkEvent{
				BaseEventData: agentevents.BaseEventData{
					Timestamp: now,
					SessionID: sessionID,
					Metadata: map[string]interface{}{
						"kind":           "terminal",
						"provider":       provider,
						"tmux_session":   tmuxSession,
						"execution_kind": "main_agent",
						"scope":          "main_agent",
						"step_transport": "tmux",
						"title":          "Restored " + provider,
						"workflow_path":  strings.TrimSpace(runtime.WorkspacePath),
					},
				},
				Content:    content,
				ChunkIndex: 0,
			},
		},
	})
}

func (api *StreamingAPI) findRestoredTerminalSnapshot(sessionID, tmuxSession string) (terminals.Snapshot, bool) {
	if api == nil || api.terminalStore == nil {
		return terminals.Snapshot{}, false
	}
	tmuxSession = strings.TrimSpace(tmuxSession)
	for _, snapshot := range api.terminalStore.List(sessionID) {
		if tmuxSession == "" || strings.TrimSpace(snapshot.TmuxSession) == tmuxSession {
			return snapshot, true
		}
	}
	return terminals.Snapshot{}, false
}

func cleanupChatHistoryHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		if userID == "" {
			userID = "default"
		}

		days := 14
		if v := r.URL.Query().Get("older_than_days"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				days = n
			}
		}
		workspacePath := r.URL.Query().Get("workspace_path")

		result, err := DeleteChatHistoryOlderThan(userID, days, workspacePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result":  result,
		})
	}
}

func getChatHistoryConversationHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		if userID == "" {
			userID = "default"
		}
		sessionID := mux.Vars(r)["session_id"]
		workspacePath := r.URL.Query().Get("workspace_path")

		data, err := ReadChatHistoryConversation(userID, sessionID, workspacePath)
		if err != nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}
}

func deleteChatHistorySessionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		if userID == "" {
			userID = "default"
		}
		sessionID := mux.Vars(r)["session_id"]
		workspacePath := r.URL.Query().Get("workspace_path")

		result, err := DeleteChatHistorySession(userID, sessionID, workspacePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if result.DeletedCount == 0 {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result":  result,
		})
	}
}
