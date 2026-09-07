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
		kind := r.URL.Query().Get("kind")

		sessions, err := ListChatHistorySessionsByKind(userID, kind, limit, offset, workspacePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
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
		// to shelling out (e.g. the CLI emits "tool(s) [run_full_workflow] not
		// found" and runs python3 main.py instead).
		//
		// If the tmux pane is gone, defer the launch to the user's next
		// /api/query, which registers the phase tools first and then launches
		// the CLI — same path a fresh chat takes, no race.
		// During the isolation rollout, defer attachment until /api/query has
		// rebuilt and checked the current user/mode/private runtime identity.
		// Otherwise a saved live pane can bypass the native-resume migration.
		if workflowCLIIsolationEnabled() && strings.HasPrefix(runtime.WorkspacePath, "Workflow/") {
			_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: false, Reason: "private_runtime_requires_query"})
			return
		}
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

		// A resume reads the durable record, but a builder chat continued
		// through retained live input has turns that exist only in the
		// coding CLI's own transcript (PLAT-178). Catch the record up first so
		// Recent/Resume shows where the conversation actually ended, not the
		// last full-turn snapshot. Best-effort: unsupported providers and
		// missing transcripts leave the record untouched.
		if parsePositiveQueryInt(r, "resume_turns") > 0 && strings.TrimSpace(workspacePath) != "" {
			api.syncWorkflowBuilderConversationFromNativeTranscript(r.Context(), sessionID, workspacePath)
		}

		data, err := ReadChatHistoryConversation(userID, sessionID, workspacePath)
		if err != nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		// The previous-chats panel renders only the tail of a conversation, so
		// let it ask for just that. Without this it downloaded the whole file --
		// 1.3 MB for a real builder session, nearly all of it ui_events -- and
		// threw away everything but the last handful of messages client-side.
		if limit := parsePositiveQueryInt(r, "resume_turns"); limit > 0 {
			includeUIEvents := r.URL.Query().Get("include_ui_events") == "1"
			var rawUIEvents []byte
			if includeUIEvents {
				rawUIEvents = chatHistoryUIEvents(data)
			}
			data = projectChatHistoryConversationForResumePage(data, limit, parseNonNegativeQueryInt(r, "resume_offset"))
			// A scheduled run is restored as a read-only conversation, so its
			// saved UI-event tail is the only existing record of its child-agent
			// messages and tool calls. Keep the normal resume projection small by
			// default, but return the compact, displayable trace when the caller
			// explicitly asks to reconstruct that run.
			if includeUIEvents {
				data = attachChatHistoryUIEventsForResume(data, rawUIEvents)
			}
		} else if limit := parsePositiveQueryInt(r, "preview_messages"); limit > 0 {
			data = trimChatHistoryConversationForPreview(data, limit)
		} else {
			data = filterClaudeLocalCommandHistory(data)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}
}

// mustReadChatHistoryUIEvents extracts the durable UI-event tail from the
// original conversation document. Resume first projects messages to a bounded
// view, then this helper attaches only events the formatted conversation can
// use. Keeping this opt-in avoids making ordinary chat restore download the
// high-volume terminal/stream trace.
func chatHistoryUIEvents(data []byte) []byte {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	return doc["ui_events"]
}

func attachChatHistoryUIEventsForResume(projected, rawUIEvents []byte) []byte {
	if len(rawUIEvents) == 0 {
		return projected
	}
	var events []json.RawMessage
	if err := json.Unmarshal(rawUIEvents, &events); err != nil {
		return projected
	}
	kept := make([]json.RawMessage, 0, len(events))
	for _, event := range events {
		var header struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(event, &header) == nil && isFormattedResumeUIEventType(header.Type) {
			kept = append(kept, event)
		}
	}
	if len(kept) == 0 {
		return projected
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(projected, &doc); err != nil {
		return projected
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return projected
	}
	doc["ui_events"] = encoded
	return marshalChatHistoryProjectionOrOriginal(doc, projected)
}

// isFormattedResumeUIEventType intentionally excludes system prompts, token
// counters, raw terminal frames, and streaming chunks. Final messages and
// paired tool calls are enough to reconstruct what happened without turning a
// read-only schedule restore into a terminal dump.
func isFormattedResumeUIEventType(eventType string) bool {
	switch eventType {
	case "user_message", "llm_generation_end", "llm_generation_error", "unified_completion",
		"tool_call_start", "tool_call_end", "tool_call_error",
		"background_agent_started", "background_agent_completed", "background_agent_failed", "background_agent_terminated",
		"orchestrator_agent_start", "orchestrator_agent_end",
		"agent_start", "agent_end", "agent_error", "conversation_error", "workflow_error",
		"request_human_feedback", "blocking_human_feedback", "plan_approval":
		return true
	default:
		return false
	}
}

// projectChatHistoryConversationForResume returns the actual conversational
// turns needed by Formatted mode, without terminal snapshots, UI trace events,
// system prompts, tool results, or coding-provider tool-call marker messages.
//
// Coding CLIs can persist more than one meaningful assistant update between two
// user messages. Keep every ordinary text update in its original order: a long
// tool-heavy turn otherwise restores as only the final answer, even though the
// operator saw progress and intermediate conclusions while it ran. Function
// markers and tool results remain out of this conversational projection.
func projectChatHistoryConversationForResume(data []byte, maxTurns int) []byte {
	return projectChatHistoryConversationForResumePage(data, maxTurns, 0)
}

// projectChatHistoryConversationForResumePage returns one bounded page of
// conversational turns. offset counts from the newest end of the transcript:
// offset=0 is the latest page, and the next cursor moves toward earlier turns.
// That makes the formatted transcript's "Load earlier" control a real backend
// pagination action instead of a client-side scroll shortcut.
func projectChatHistoryConversationForResumePage(data []byte, maxTurns, offset int) []byte {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return data
	}
	delete(doc, "ui_events")
	delete(doc, "terminal_snapshots")

	rawHistory, ok := doc["conversation_history"]
	if !ok {
		return marshalChatHistoryProjectionOrOriginal(doc, data)
	}
	var history []json.RawMessage
	if err := json.Unmarshal(rawHistory, &history); err != nil {
		return data
	}

	type resumeMessage struct {
		raw   json.RawMessage
		order int
	}
	type turn struct {
		user       resumeMessage
		assistants []resumeMessage
	}
	turns := make([]turn, 0)
	var current *turn
	var assistantWithoutUser resumeMessage
	for order, raw := range history {
		role, text := chatHistoryMessageRoleAndText(raw)
		switch role {
		case "human", "user":
			if isClaudeLocalCommandRecord(text) {
				continue
			}
			turns = append(turns, turn{user: resumeMessage{raw: raw, order: order}})
			current = &turns[len(turns)-1]
		case "ai", "assistant":
			if text == "" || isPersistedToolCallMarker(text) {
				continue
			}
			if current != nil {
				current.assistants = append(current.assistants, resumeMessage{raw: raw, order: order})
			} else {
				assistantWithoutUser = resumeMessage{raw: raw, order: order}
			}
		}
	}

	totalTurns := len(turns)
	if offset < 0 {
		offset = 0
	}
	end := totalTurns - offset
	if end < 0 {
		end = 0
	}
	start := 0
	if maxTurns > 0 && end > maxTurns {
		start = end - maxTurns
	}
	turns = turns[start:end]
	projected := make([]json.RawMessage, 0, len(turns)*2+1)
	if len(turns) == 0 && len(assistantWithoutUser.raw) > 0 {
		projected = append(projected, annotateResumedChatMessage(assistantWithoutUser.raw, assistantWithoutUser.order, len(history)))
	}
	for _, item := range turns {
		projected = append(projected, annotateResumedChatMessage(item.user.raw, item.user.order, len(history)))
		for _, assistant := range item.assistants {
			projected = append(projected, annotateResumedChatMessage(assistant.raw, assistant.order, len(history)))
		}
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return data
	}
	doc["conversation_history"] = encoded
	// The formatted client uses these stable source positions only to interleave
	// readable assistant updates with the persisted tool trace. They are not
	// terminal events and do not affect the conversation's durable identity.
	doc["history_source_message_count"], _ = json.Marshal(len(history))
	pagination, err := json.Marshal(map[string]interface{}{
		"has_more":    start > 0,
		"next_offset": offset + len(turns),
		"start_turn":  start,
		"total_turns": totalTurns,
	})
	if err != nil {
		return data
	}
	doc["history_pagination"] = pagination
	return marshalChatHistoryProjectionOrOriginal(doc, data)
}

func annotateResumedChatMessage(raw json.RawMessage, order, sourceCount int) json.RawMessage {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return raw
	}
	encodedOrder, err := json.Marshal(order)
	if err != nil {
		return raw
	}
	encodedSourceCount, err := json.Marshal(sourceCount)
	if err != nil {
		return raw
	}
	message["resume_order"] = encodedOrder
	message["resume_source_message_count"] = encodedSourceCount
	annotated, err := json.Marshal(message)
	if err != nil {
		return raw
	}
	return annotated
}

func marshalChatHistoryProjectionOrOriginal(doc map[string]json.RawMessage, original []byte) []byte {
	out, err := json.Marshal(doc)
	if err != nil {
		return original
	}
	return out
}

// botProgressiveChatHistoryReader is services.ChatHistoryReaderFunc: every
// completed assistant ("ai" role) turn's text from a session's durable
// conversation_history, in order — read straight from the workspace file
// each poll, so it reflects the turn's progress exactly as far as it has
// actually gotten, no cache to go stale. workspacePath is left empty: a bot
// session is never workflow-scoped, so the per-user lookup by session id
// alone is enough (see ReadChatHistoryConversation).
func botProgressiveChatHistoryReader(_ context.Context, userID, sessionID string) ([]string, error) {
	raw, err := ReadChatHistoryConversation(userID, sessionID, "")
	if err != nil {
		return nil, err
	}
	var doc struct {
		ConversationHistory []json.RawMessage `json:"conversation_history"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(doc.ConversationHistory))
	for _, entry := range doc.ConversationHistory {
		role, text := chatHistoryMessageRoleAndText(entry)
		if role != "ai" || text == "" {
			continue
		}
		texts = append(texts, text)
	}
	return texts, nil
}

func chatHistoryMessageRoleAndText(raw json.RawMessage) (string, string) {
	var message struct {
		Role      string `json:"Role"`
		RoleLower string `json:"role"`
		Parts     []struct {
			Text      string `json:"Text"`
			TextLower string `json:"text"`
			Content   string `json:"Content"`
			ContentLo string `json:"content"`
		} `json:"Parts"`
		PartsLower []struct {
			Text      string `json:"Text"`
			TextLower string `json:"text"`
			Content   string `json:"Content"`
			ContentLo string `json:"content"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		return "", ""
	}
	role := strings.ToLower(strings.TrimSpace(message.Role))
	if role == "" {
		role = strings.ToLower(strings.TrimSpace(message.RoleLower))
	}
	parts := message.Parts
	if len(parts) == 0 {
		parts = message.PartsLower
	}
	var text strings.Builder
	for _, part := range parts {
		value := part.Text
		if value == "" {
			value = part.TextLower
		}
		if value == "" {
			value = part.Content
		}
		if value == "" {
			value = part.ContentLo
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(value)
	}
	return role, strings.TrimSpace(text.String())
}

func isPersistedToolCallMarker(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(normalized, "[previous tool call:") ||
		strings.HasPrefix(normalized, "[previous tool result:")
}

// parsePositiveQueryInt returns a positive integer query parameter, or 0 when
// absent or malformed (i.e. "no limit requested").
func parsePositiveQueryInt(r *http.Request, name string) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func parseNonNegativeQueryInt(r *http.Request, name string) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// trimChatHistoryConversationForPreview keeps the last `limit` conversation
// messages and drops the payloads a preview never reads.
//
// ui_events dominates the file (a 1.3 MB builder session was ~90% events) and
// terminal_snapshots carries a full pane capture; neither is used to render a
// message list. The document shape is otherwise preserved so the client parses
// it exactly as it parses an untrimmed one.
//
// Any parse failure returns the original bytes: a preview that is too large is
// a performance problem, but a preview that fails to load is a broken panel.
func trimChatHistoryConversationForPreview(data []byte, limit int) []byte {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return data
	}
	delete(doc, "ui_events")
	delete(doc, "terminal_snapshots")

	if raw, ok := doc["conversation_history"]; ok {
		var history []json.RawMessage
		if err := json.Unmarshal(raw, &history); err == nil {
			clean := make([]json.RawMessage, 0, len(history))
			for _, message := range history {
				role, text := chatHistoryMessageRoleAndText(message)
				if !isClaudeLocalCommandMessage(role, text) {
					clean = append(clean, message)
				}
			}
			if limit > 0 && len(clean) > limit {
				clean = clean[len(clean)-limit:]
			}
			trimmed, err := json.Marshal(clean)
			if err == nil {
				doc["conversation_history"] = trimmed
			}
		}
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return data
	}
	return out
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
