package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	agentevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/mcpagent/mcpclient"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	storeevents "mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/internal/terminals"
	agent "mcp-agent-builder-go/agent_go/pkg/agentwrapper"
	"mcp-agent-builder-go/agent_go/pkg/common"
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
		runtime, ok, err := restoredTerminalRuntime(userID, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok || runtime == nil {
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

		var fallbackReason string
		if terminal, started, reason := api.attachRestoredExistingTmuxTerminal(r.Context(), req.SessionID, runtime); started {
			api.logRestoredTerminalInfof("restore session=%s tier=attach_existing result=started", req.SessionID)
			_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: true, Terminal: terminal})
			return
		} else if reason != "" {
			api.logRestoredTerminalInfof("restore session=%s tier=attach_existing result=skip reason=%s", req.SessionID, reason)
			fallbackReason = reason
		}

		if terminal, started, reason := api.startRestoredTerminalFromInMemoryAgent(r.Context(), req.SessionID, runtime); started {
			api.logRestoredTerminalInfof("restore session=%s tier=in_memory_agent result=started", req.SessionID)
			_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: true, Terminal: terminal})
			return
		} else if reason != "" {
			api.logRestoredTerminalInfof("restore session=%s tier=in_memory_agent result=skip reason=%s", req.SessionID, reason)
			fallbackReason = reason
		}

		if terminal, started, reason := api.startRestoredTerminalFromNewAgent(r.Context(), req.SessionID, userID, runtime); started {
			api.logRestoredTerminalInfof("restore session=%s tier=new_agent result=started", req.SessionID)
			_ = json.NewEncoder(w).Encode(startRestoredTerminalResponse{OK: true, Started: true, Terminal: terminal})
			return
		} else if reason != "" {
			api.logRestoredTerminalInfof("restore session=%s tier=new_agent result=skip reason=%s", req.SessionID, reason)
			fallbackReason = reason
		}

		if fallbackReason == "" {
			fallbackReason = "terminal_transport_unavailable"
		}
		api.logRestoredTerminalInfof("restore session=%s result=fail final_reason=%s", req.SessionID, fallbackReason)
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

func (api *StreamingAPI) startRestoredTerminalFromInMemoryAgent(ctx context.Context, sessionID string, runtime *ChatHistoryAgentRuntime) (*terminals.Snapshot, bool, string) {
	if api == nil || runtime == nil {
		return nil, false, "api_unavailable"
	}
	if !restoredRuntimeUsesLaunchableTerminalTransport(runtime) {
		return nil, false, "not_tmux_transport"
	}
	api.sessionAgentsMux.RLock()
	llmAgent := api.sessionAgents[sessionID]
	api.sessionAgentsMux.RUnlock()
	if llmAgent == nil || llmAgent.GetUnderlyingAgent() == nil {
		return nil, false, "agent_not_in_memory"
	}
	return api.startRestoredTerminalFromAgent(ctx, sessionID, runtime, llmAgent.GetUnderlyingAgent())
}

func (api *StreamingAPI) startRestoredTerminalFromAgent(ctx context.Context, sessionID string, runtime *ChatHistoryAgentRuntime, underlyingAgent *mcpagent.Agent) (*terminals.Snapshot, bool, string) {
	if api == nil || runtime == nil || underlyingAgent == nil {
		return nil, false, "underlying_agent_missing"
	}
	provider := strings.ToLower(strings.TrimSpace(runtime.Provider))
	if provider == "" && runtime.AgentSessionHandle != nil {
		provider = strings.ToLower(strings.TrimSpace(runtime.AgentSessionHandle.Provider.Provider))
	}
	if !api.seedCodingAgentRuntimeFromRestoredConversation(sessionID, provider, "", runtime, underlyingAgent) {
		return nil, false, "seed_failed"
	}

	// PHASE_TOOL_RACE_DIAGNOSTIC: This restore path launches the CLI process
	// before any /api/query runs. The workflow-phase tool registrations
	// (RegisterRunFullWorkflowTool, RegisterWorkshopChatTools, etc., over in
	// server.go's workflow-builder case) only fire inside /api/query. If the
	// CLI caches its tool catalog at launch via get_api_spec, it won't see
	// run_full_workflow / execute_step until the next launch AFTER a real
	// /api/query has registered them. See PHASE_TOOL_REGISTER_* logs in
	// server.go to compare timing.
	log.Printf("[PHASE_TOOL_RACE] AUTO_RESTORE_LAUNCH starting for session=%s provider=%s — no phase-specific tool registration runs before this",
		sessionID, provider)
	launchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	launchStart := time.Now()
	handle, err := underlyingAgent.StartCodingAgentTransportSession(launchCtx)
	log.Printf("[PHASE_TOOL_RACE] AUTO_RESTORE_LAUNCH completed for session=%s in %s (err=%v)",
		sessionID, time.Since(launchStart), err)
	if err != nil || handle == nil {
		if err != nil {
			api.logRestoredTerminalf("Failed to start restored coding-agent tmux transport for session %s: %v", sessionID, err)
			return nil, false, "transport_start_failed"
		}
		return nil, false, "transport_handle_missing"
	}
	if terminal, started, reason := api.materializeRestoredTmuxTerminal(ctx, sessionID, runtime, handle.TmuxSession); started {
		return terminal, true, ""
	} else if reason != "" {
		return nil, false, reason
	}
	return nil, false, "terminal_snapshot_not_created"
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

func (api *StreamingAPI) startRestoredTerminalFromNewAgent(ctx context.Context, sessionID, userID string, runtime *ChatHistoryAgentRuntime) (*terminals.Snapshot, bool, string) {
	if api == nil || runtime == nil {
		return nil, false, "api_unavailable"
	}
	if !restoredRuntimeUsesLaunchableTerminalTransport(runtime) {
		return nil, false, "not_tmux_transport"
	}
	provider := strings.ToLower(strings.TrimSpace(runtime.Provider))
	modelID := strings.TrimSpace(runtime.ModelID)
	workingDir := ""
	if runtime.AgentSessionHandle != nil && !runtime.AgentSessionHandle.Empty() {
		handle := runtime.AgentSessionHandle.Provider
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(handle.Provider))
		}
		if modelID == "" {
			modelID = strings.TrimSpace(handle.Model)
		}
		workingDir = strings.TrimSpace(handle.WorkingDir)
	}
	if provider == "" {
		return nil, false, "provider_missing"
	}
	if modelID == "" {
		modelID = provider
	}
	if workingDir == "" {
		workspaceFolder := strings.TrimSpace(runtime.WorkspacePath)
		if workspaceFolder == "" {
			workspaceFolder = perUserChatsFolderFor(userID)
		}
		workingDir = codingAgentWorkspaceWorkingDir(workspaceFolder)
	}

	// Replay the original session's MCP server+tool selection so the coding-agent
	// bridge exposes the same catalog. A restored agent built with NoServers
	// leaves get_api_spec empty ("server not available"), so the resumed CLI loses
	// all its tools. Prefer the selection persisted in the runtime (the exact set
	// the original agent ran with). For sessions saved before that was persisted
	// (empty ServerName), fall back to the workflow manifest for this workspace,
	// then to the full configured set so the bridge catalog is never empty.
	restoredServerName := strings.TrimSpace(runtime.ServerName)
	restoredSelectedTools := runtime.SelectedTools
	restoredBrowserMode := strings.TrimSpace(runtime.BrowserMode)
	var restoredSecretNames []string
	restoredWorkspacePath := strings.TrimSpace(runtime.WorkspacePath)
	// Always consult the workflow manifest: it's the source of truth for
	// workflow-scoped secrets, and the fallback for server/tool/browser
	// selection on sessions saved before those were persisted in the runtime.
	if restoredWorkspacePath != "" {
		if manifest, found, mErr := ReadWorkflowManifest(ctx, restoredWorkspacePath); mErr == nil && found {
			caps := manifest.Capabilities
			restoredSecretNames = caps.SelectedSecrets
			if restoredServerName == "" {
				if len(caps.SelectedServers) > 0 {
					restoredServerName = strings.Join(caps.SelectedServers, ",")
				}
				restoredSelectedTools = caps.SelectedTools
			}
			if restoredBrowserMode == "" {
				restoredBrowserMode = strings.TrimSpace(caps.BrowserMode)
			}
		}
	}
	if restoredServerName == "" {
		restoredServerName = mcpclient.AllServers
	}
	// Replay the session's browser capability so browser-backed tools work and
	// context-aware shell blocking knows a browser is available (mirrors the
	// fresh-session SetSessionBrowserMode call in the query handler).
	if restoredBrowserMode != "" && restoredBrowserMode != "none" {
		common.SetSessionBrowserMode(sessionID, restoredBrowserMode)
	}

	claudeCodePersistent, codexPersistent, geminiPersistent, cursorPersistent, agyPersistent, openCodePersistent := codingAgentPersistentInteractiveFlags(provider)
	cfg := agent.LLMAgentConfig{
		Name:                                   "restored-terminal-agent",
		ServerName:                             restoredServerName,
		SelectedTools:                          restoredSelectedTools,
		ConfigPath:                             api.mcpConfigPath,
		Provider:                               llm.Provider(provider),
		ModelID:                                modelID,
		ToolChoice:                             "auto",
		StreamingChunkSize:                     50,
		UseCodeExecutionMode:                   true,
		ClaudeCodePersistentInteractiveSession: claudeCodePersistent,
		CodexPersistentInteractiveSession:      codexPersistent,
		GeminiPersistentInteractiveSession:     geminiPersistent,
		CursorPersistentInteractiveSession:     cursorPersistent,
		AgyPersistentInteractiveSession:        agyPersistent,
		CursorBridgeToolsMode:                  cursorPersistent,
		OpenCodePersistentInteractiveSession:   openCodePersistent,
		ClaudeCodeTransport:                    codingAgentClaudeCodeChatTransport(provider),
		CodingAgentWorkingDir:                  workingDir,
		APIKeys:                                MergedProviderAPIKeys(ctx),
		SessionID:                              sessionID,
		UserID:                                 userID,
	}
	llmAgent, err := agent.NewLLMAgentWrapper(ctx, cfg, nil, api.logger)
	if err != nil {
		return nil, false, "agent_create_failed"
	}
	underlyingAgent := llmAgent.GetUnderlyingAgent()
	if underlyingAgent == nil {
		return nil, false, "underlying_agent_missing"
	}
	if api.eventStore != nil {
		underlyingAgent.AddEventListener(storeevents.NewEventObserverWithLogger(api.eventStore, sessionID, api.logger))
	}
	underlyingAgent.AddEventListener(newCostObserver(api.costLedger, sessionID, userID, "multi-agent"))
	api.runningAgentsMux.Lock()
	if api.runningAgents != nil {
		api.runningAgents[sessionID] = underlyingAgent
	}
	api.runningAgentsMux.Unlock()
	api.sessionAgentsMux.Lock()
	if api.sessionAgents != nil {
		api.sessionAgents[sessionID] = llmAgent
	}
	api.sessionAgentsMux.Unlock()

	// Re-register the session-scoped virtual tools a fresh session would have
	// (secret-aware workspace tools + browser tools) so the resumed agent's
	// bridge catalog matches. Must happen before the transport session launches.
	api.registerRestoredCodingAgentTools(ctx, underlyingAgent, sessionID, userID, restoredWorkspacePath, restoredBrowserMode, restoredSecretNames)

	if terminal, started, reason := api.startRestoredTerminalFromAgent(ctx, sessionID, runtime, underlyingAgent); started {
		return terminal, true, ""
	} else if reason != "" {
		return nil, false, reason
	}
	return nil, false, "tmux_start_failed"
}

// registerRestoredCodingAgentTools re-registers the session-scoped virtual tools
// a fresh coding-agent session would have, so a restored agent's bridge exposes
// the same catalog: secret-aware workspace tools (so shell commands see SECRET_*
// env) and, when a browser mode is active, the workspace browser tools. Executors
// are wrapped in a conservative folder guard (the session's workspace folder is
// writable, everything else read-only) since restore doesn't persist the original
// fine-grained grants. Skills are applied separately via the system prompt.
func (api *StreamingAPI) registerRestoredCodingAgentTools(ctx context.Context, ag *mcpagent.Agent, sessionID, userID, workspacePath, browserMode string, secretNames []string) {
	if ag == nil {
		return
	}
	// Conservative folder guard: the session's workspace folder is writable,
	// everything else read-only (restore doesn't persist the original
	// fine-grained grants). Supplied as a closure so the shared registrars apply
	// it without owning guard policy.
	var guard codingToolGuard
	if guardFolder := strings.Trim(strings.TrimSpace(workspacePath), "/"); guardFolder != "" {
		guard = func(execs codingAgentToolExecutors) codingAgentToolExecutors {
			return wrapExecutorsWithPlanFolderGuard(execs, guardFolder, nil)
		}
	}

	// Secret-aware workspace tools — bake the workflow's secrets in as SECRET_*
	// env so the resumed agent's shell commands have them.
	secretEnvVars := map[string]string{}
	for _, s := range api.loadSelectedSecrets(ctx, userID, workspacePath, secretNames) {
		secretEnvVars["SECRET_"+s.Name] = s.Value
	}
	wsExecutors, _ := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv(userID, sessionID, secretEnvVars)
	if guard != nil {
		wsExecutors = guard(wsExecutors)
	}
	wsCategory := virtualtools.GetWorkspaceAdvancedToolCategory()
	if err := registerCodingToolGroup(ag.RegisterCustomTool, virtualtools.CreateWorkspaceAdvancedTools(), wsExecutors, func(string) string { return wsCategory }, nil); err != nil {
		api.logRestoredTerminalf("restore workspace tool registration: %v", err)
	}

	// Browser tools when a browser mode is active (shared with the fresh path).
	if browserMode == "headless" || browserMode == "cdp" {
		cdpPort := 0
		if browserMode == "cdp" {
			cdpPort = 9222
		}
		if err := registerCodingBrowserTools(ag, sessionID, cdpPort, guard); err != nil {
			api.logRestoredTerminalf("restore browser tool registration: %v", err)
		}
	}
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
