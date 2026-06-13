package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	internalevents "mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/internal/terminals"
	"mcp-agent-builder-go/agent_go/pkg/fsutil"
)

// ChatHistorySession is the metadata returned by the list endpoint.
type ChatHistorySession struct {
	SessionID        string                      `json:"session_id"`
	AgentMode        string                      `json:"agent_mode"`
	Runtime          *ChatHistoryAgentRuntime    `json:"runtime,omitempty"`
	WorkshopMode     string                      `json:"workshop_mode,omitempty"`
	Status           string                      `json:"status"`
	Query            string                      `json:"query,omitempty"`
	UserID           string                      `json:"user_id"`
	WorkspacePath    string                      `json:"workspace_path,omitempty"`
	ConversationPath string                      `json:"conversation_path"`
	CreatedAt        string                      `json:"created_at"`
	UpdatedAt        string                      `json:"updated_at"`
	MessageCount     int                         `json:"message_count"`
	PreviewMessages  []ChatHistoryPreviewMessage `json:"preview_messages,omitempty"`
}

// ChatHistoryAgentRuntime records enough information to reopen a previous chat
// with its original runtime when that runtime supports native resume.
type ChatHistoryAgentRuntime struct {
	Kind               string                       `json:"kind,omitempty"`
	Provider           string                       `json:"provider,omitempty"`
	ModelID            string                       `json:"model_id,omitempty"`
	Transport          string                       `json:"transport,omitempty"`
	ExternalSessionID  string                       `json:"external_session_id,omitempty"`
	ResumeSupported    bool                         `json:"resume_supported"`
	ResumeFlag         string                       `json:"resume_flag,omitempty"`
	ProjectDirID       string                       `json:"project_dir_id,omitempty"`
	WorkspacePath      string                       `json:"workspace_path,omitempty"`
	WorkshopMode       string                       `json:"workshop_mode,omitempty"`
	CapturedAt         string                       `json:"captured_at,omitempty"`
	AgentSessionHandle *mcpagent.AgentSessionHandle `json:"agent_session_handle,omitempty"`
	// SystemPrompt and AppendedSystemPrompts capture the assembled prompt
	// the agent was running with at chat-save time so a restore that has
	// no live /api/query yet (e.g., the chat-history auto-restore path that
	// only relaunches the tmux pane) can still SetSystemPrompt the right
	// content. Without this, coding-CLI adapters project the agent's
	// default mcpagent base prompt into .agents/rules/mlp-system.md /
	// .cursor/rules/mlp-system.mdc / AGENTS.md / GEMINI.md / .claude/rules/
	// instead of the workflow-builder workshop template, and the user sees
	// "wrong content" in the rules file after a restart. Saved on every
	// captureChatHistoryAgentRuntime; re-applied in
	// seedCodingAgentRuntimeFromRestoredConversation before any launch.
	SystemPrompt          string   `json:"system_prompt,omitempty"`
	AppendedSystemPrompts []string `json:"appended_system_prompts,omitempty"`
	// ServerName / SelectedTools / BrowserMode capture the agent's MCP server+tool
	// selection and browser capability so a restore can replay the same bridge
	// catalog (incl. agent_browser) instead of re-deriving it. Empty on sessions
	// saved before this was persisted — restore falls back to the workflow
	// manifest for those.
	ServerName    string   `json:"server_name,omitempty"`
	SelectedTools []string `json:"selected_tools,omitempty"`
	BrowserMode   string   `json:"browser_mode,omitempty"`
}

type ChatHistoryPreviewMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type ChatHistoryCleanupResult struct {
	DeletedCount int      `json:"deleted_count"`
	DeletedPaths []string `json:"deleted_paths"`
	Cutoff       string   `json:"cutoff"`
	Scope        string   `json:"scope"`
}

type restoredChatHistoryPersistTarget struct {
	SessionID        string
	ConversationPath string
	History          []llmtypes.MessageContent
	Runtime          *ChatHistoryAgentRuntime
}

const (
	maxPersistedChatHistoryUIEvents = 200
	maxChatHistoryFallbackScan      = 1000
	maxChatHistoryTerminalSnapshots = 1
	maxChatHistoryTerminalBytes     = 512 * 1024
	maxChatHistoryTerminalLines     = 10000
)

// chatHistoryRoot returns the workspace-relative path to a user's chat_history root.
func chatHistoryRoot(userID string) string {
	return fmt.Sprintf("_users/%s/chat_history", sanitizeUserIDForPath(userID))
}

func chatHistoryConversationFileName(sessionID string) string {
	return fmt.Sprintf("session-%s-conversation.json", sanitizeChatHistorySessionID(sessionID))
}

func chatHistoryConversationDate(t time.Time) string {
	return t.Format("2006-01-02")
}

func chatHistoryConversationPath(userID, sessionID string, t time.Time) string {
	return pathpkg.Join(chatHistoryRoot(userID), chatHistoryConversationDate(t), chatHistoryConversationFileName(sessionID))
}

// persistChatConversation saves an already persistence-cleaned conversation in
// the same format as the workflow builder: a single conversation.json with the
// full message history. Called inline right after finalHistory is captured.
func (api *StreamingAPI) persistChatConversation(sessionID, agentMode, userID string, persistedHistory []llmtypes.MessageContent, runtime *ChatHistoryAgentRuntime, uiEvents []internalevents.Event) {
	api.persistChatConversationToPath(sessionID, agentMode, userID, persistedHistory, runtime, uiEvents, "")
}

func (api *StreamingAPI) persistChatConversationToPath(sessionID, agentMode, userID string, persistedHistory []llmtypes.MessageContent, runtime *ChatHistoryAgentRuntime, uiEvents []internalevents.Event, conversationPath string) {
	api.persistChatConversationToPathWithTerminalSession(sessionID, sessionID, agentMode, userID, persistedHistory, runtime, uiEvents, conversationPath)
}

func (api *StreamingAPI) persistChatConversationToPathWithTerminalSession(sessionID, terminalSnapshotSessionID, agentMode, userID string, persistedHistory []llmtypes.MessageContent, runtime *ChatHistoryAgentRuntime, uiEvents []internalevents.Event, conversationPath string) {
	if len(persistedHistory) == 0 {
		return
	}
	if userID == "" {
		userID = "default"
	}
	logCtx := newServerLogContext("", "", agentMode, userID, "", sessionID)

	now := time.Now()
	convData := map[string]interface{}{
		"session_id":           sessionID,
		"agent_mode":           agentMode,
		"conversation_history": persistedHistory,
		"updated_at":           now.Format(time.RFC3339),
	}
	if runtime != nil {
		convData["runtime"] = runtime
	}
	snapshotSessionID := strings.TrimSpace(terminalSnapshotSessionID)
	if snapshotSessionID == "" {
		snapshotSessionID = sessionID
	}
	if terminalSnapshots := api.captureChatHistoryTerminalSnapshots(snapshotSessionID, runtime); len(terminalSnapshots) > 0 {
		convData["terminal_snapshots"] = terminalSnapshots
	}
	uiEvents = trimChatHistoryUIEvents(uiEvents)
	if len(uiEvents) > 0 {
		convData["ui_events"] = uiEvents
	}

	convJSON, err := json.MarshalIndent(convData, "", "  ")
	if err != nil {
		logfWithContext(logCtx, "[CHAT_HISTORY] Failed to marshal conversation for %s: %v", sessionID, err)
		return
	}

	convPath := strings.TrimSpace(conversationPath)
	if convPath == "" {
		convPath = chatHistoryConversationPath(userID, sessionID, now)
	}
	if err := writeRawFileToWorkspace(context.Background(), convPath, string(convJSON)); err != nil {
		logfWithContext(logCtx, "[CHAT_HISTORY] Failed to write %s: %v", convPath, err)
		return
	}

	logfWithContext(logCtx, "[CHAT_HISTORY] Saved conversation (%d messages) to %s", len(persistedHistory), convPath)
}

var captureChatHistoryTerminalPaneLines = captureTerminalPaneLines

func (api *StreamingAPI) captureChatHistoryTerminalSnapshots(sessionID string, runtime *ChatHistoryAgentRuntime) []terminals.Snapshot {
	sessionID = strings.TrimSpace(sessionID)
	if api == nil || sessionID == "" {
		return nil
	}

	candidates := make([]terminals.Snapshot, 0)
	if api.terminalStore != nil {
		for _, snapshot := range api.terminalStore.List(sessionID) {
			if strings.TrimSpace(snapshot.Content) == "" || !chatHistoryTerminalSnapshotIsTmux(snapshot) {
				continue
			}
			prepared, ok := prepareChatHistoryTerminalSnapshot(snapshot)
			if ok {
				candidates = append(candidates, prepared)
			}
		}
	}
	if len(candidates) == 0 {
		if snapshot, ok := api.captureChatHistoryRuntimeTmuxSnapshot(sessionID, runtime); ok {
			candidates = append(candidates, snapshot)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		iMain := chatHistoryTerminalSnapshotIsMainAgent(candidates[i])
		jMain := chatHistoryTerminalSnapshotIsMainAgent(candidates[j])
		if iMain != jMain {
			return iMain
		}
		return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
	})
	if len(candidates) > maxChatHistoryTerminalSnapshots {
		candidates = candidates[:maxChatHistoryTerminalSnapshots]
	}
	return candidates
}

func (api *StreamingAPI) captureChatHistoryRuntimeTmuxSnapshot(sessionID string, runtime *ChatHistoryAgentRuntime) (terminals.Snapshot, bool) {
	tmuxSession, ok, reason := restoredRuntimeTmuxSession(runtime)
	if !ok {
		if reason != "" && reason != "agent_session_handle_missing" && reason != "not_tmux_transport" {
			api.logRestoredTerminalInfof("terminal snapshot runtime fallback skipped session=%s reason=%s", sessionID, reason)
		}
		return terminals.Snapshot{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
	defer cancel()
	content, err := captureChatHistoryTerminalPaneLines(ctx, tmuxSession, terminalDefaultDetailHistoryLines)
	if err != nil {
		api.logRestoredTerminalf("Failed to capture runtime tmux snapshot session=%s tmux_session=%s: %v", sessionID, tmuxSession, err)
		return terminals.Snapshot{}, false
	}

	provider := strings.TrimSpace(runtime.Provider)
	if provider == "" && runtime.AgentSessionHandle != nil {
		provider = strings.TrimSpace(runtime.AgentSessionHandle.Provider.Provider)
	}
	label := "Restored terminal"
	if provider != "" {
		label = "Restored " + provider
	}
	now := time.Now()
	snapshot := terminals.Snapshot{
		TerminalID:    sessionID + ":main:" + sessionID,
		SessionID:     sessionID,
		OwnerID:       "main:" + sessionID,
		ExecutionID:   "main:" + sessionID,
		ExecutionKind: "main_agent",
		Label:         label,
		Scope:         "main_agent",
		WorkflowPath:  strings.TrimSpace(runtime.WorkspacePath),
		StepID:        "main_agent:" + sessionID,
		StepTransport: "tmux",
		TmuxSession:   tmuxSession,
		Content:       content,
		ContentSource: "tmux_capture",
		Active:        false,
		State:         "stale",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return prepareChatHistoryTerminalSnapshot(snapshot)
}

func prepareChatHistoryTerminalSnapshot(snapshot terminals.Snapshot) (terminals.Snapshot, bool) {
	content := terminals.RedactSensitiveTerminalText(snapshot.Content)
	content = boundChatHistoryTerminalContent(content)
	if strings.TrimSpace(content) == "" {
		return terminals.Snapshot{}, false
	}
	snapshot.Content = content
	snapshot.Rows = nil
	snapshot.Active = false
	if strings.TrimSpace(snapshot.State) == "" || snapshot.State == "running" || snapshot.State == "closing" {
		snapshot.State = "stale"
	}
	snapshot.ClosesAt = nil
	snapshot.RetentionSeconds = 0
	if snapshot.ContentSource == "" {
		snapshot.ContentSource = "tmux_capture"
	}
	return snapshot, true
}

func boundChatHistoryTerminalContent(content string) string {
	content = terminalContentTail(content, maxChatHistoryTerminalBytes)
	if maxChatHistoryTerminalLines <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxChatHistoryTerminalLines {
		return content
	}
	return strings.Join(lines[len(lines)-maxChatHistoryTerminalLines:], "\n")
}

func chatHistoryTerminalSnapshotIsTmux(snapshot terminals.Snapshot) bool {
	if strings.TrimSpace(snapshot.TmuxSession) != "" {
		return true
	}
	transport := strings.ToLower(strings.TrimSpace(snapshot.StepTransport))
	if transport == "tmux" {
		return true
	}
	source := strings.ToLower(strings.TrimSpace(snapshot.ContentSource))
	return source == "tmux_pipe" || source == "tmux_capture"
}

func chatHistoryTerminalSnapshotIsMainAgent(snapshot terminals.Snapshot) bool {
	if strings.Contains(snapshot.TerminalID, ":turn-") {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(snapshot.ExecutionKind))
	scope := strings.ToLower(strings.TrimSpace(snapshot.Scope))
	return kind == "main_agent" || kind == "main" || kind == "chat" ||
		scope == "main_agent" || scope == "main" || scope == "chat" ||
		strings.HasPrefix(strings.TrimSpace(snapshot.OwnerID), "main:")
}

func (api *StreamingAPI) resolveRestoredCodingConversationPersistTarget(userID, currentSessionID, restoredConversationPath, restoredConversationSessionID, workspacePath, currentProvider, currentWorkshopMode string) (*restoredChatHistoryPersistTarget, bool, error) {
	currentSessionID = strings.TrimSpace(currentSessionID)
	restoredConversationPath = strings.TrimSpace(restoredConversationPath)
	restoredConversationSessionID = strings.TrimSpace(restoredConversationSessionID)
	if userID == "" {
		userID = "default"
	}

	var target *restoredChatHistoryPersistTarget
	var ok bool
	var err error
	if restoredConversationPath != "" {
		target, ok, err = readRestoredChatHistoryPersistTargetFromPath(userID, restoredConversationPath)
	} else if restoredConversationSessionID != "" {
		target, ok, err = readRestoredChatHistoryPersistTargetForSession(userID, restoredConversationSessionID, workspacePath)
	} else if api != nil {
		target, ok = api.rememberedRestoredConversationPersistTarget(currentSessionID)
		if ok {
			target, ok, err = readRestoredChatHistoryPersistTargetFromPath(userID, target.ConversationPath)
		}
	}
	if err != nil || !ok || target == nil {
		return nil, false, err
	}
	if strings.TrimSpace(target.SessionID) == "" || strings.TrimSpace(target.ConversationPath) == "" {
		return nil, false, nil
	}
	if !shouldPersistIntoRestoredCodingConversation(target.Runtime, currentProvider, currentWorkshopMode) {
		if api != nil {
			api.forgetRestoredConversationPersistTarget(currentSessionID)
		}
		return nil, false, nil
	}
	if api != nil && currentSessionID != "" {
		api.rememberRestoredConversationPersistTarget(currentSessionID, *target)
	}
	return target, true, nil
}

func shouldPersistIntoRestoredCodingConversation(runtime *ChatHistoryAgentRuntime, currentProvider, currentWorkshopMode string) bool {
	if runtime == nil || runtime.Kind != "coding_agent" {
		return false
	}
	hasAgentSessionHandle := runtime.AgentSessionHandle != nil && !runtime.AgentSessionHandle.Empty()
	if !runtime.ResumeSupported && !hasAgentSessionHandle {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(runtime.Provider))
	if provider == "" && hasAgentSessionHandle {
		provider = strings.ToLower(strings.TrimSpace(runtime.AgentSessionHandle.Provider.Provider))
	}
	currentProvider = strings.ToLower(strings.TrimSpace(currentProvider))
	if provider == "" || (currentProvider != "" && provider != currentProvider) {
		return false
	}
	runtimeWorkshopMode := normalizeChatHistoryWorkshopMode(runtime.WorkshopMode)
	currentWorkshopMode = normalizeChatHistoryWorkshopMode(currentWorkshopMode)
	if runtimeWorkshopMode != "" && currentWorkshopMode != "" && runtimeWorkshopMode != currentWorkshopMode {
		return false
	}
	return restoredRuntimeCodingAgentTransport(runtime) != ""
}

func (api *StreamingAPI) rememberRestoredConversationPersistTarget(currentSessionID string, target restoredChatHistoryPersistTarget) {
	currentSessionID = strings.TrimSpace(currentSessionID)
	if api == nil || currentSessionID == "" || strings.TrimSpace(target.ConversationPath) == "" {
		return
	}
	api.restoredConversationPersistMux.Lock()
	defer api.restoredConversationPersistMux.Unlock()
	if api.restoredConversationPersistTargets == nil {
		api.restoredConversationPersistTargets = make(map[string]restoredChatHistoryPersistTarget)
	}
	api.restoredConversationPersistTargets[currentSessionID] = restoredChatHistoryPersistTarget{
		SessionID:        target.SessionID,
		ConversationPath: target.ConversationPath,
		Runtime:          target.Runtime,
	}
}

func (api *StreamingAPI) rememberedRestoredConversationPersistTarget(currentSessionID string) (*restoredChatHistoryPersistTarget, bool) {
	currentSessionID = strings.TrimSpace(currentSessionID)
	if api == nil || currentSessionID == "" {
		return nil, false
	}
	api.restoredConversationPersistMux.RLock()
	defer api.restoredConversationPersistMux.RUnlock()
	target, ok := api.restoredConversationPersistTargets[currentSessionID]
	if !ok || strings.TrimSpace(target.ConversationPath) == "" {
		return nil, false
	}
	return &target, true
}

func (api *StreamingAPI) forgetRestoredConversationPersistTarget(currentSessionID string) {
	currentSessionID = strings.TrimSpace(currentSessionID)
	if api == nil || currentSessionID == "" {
		return
	}
	api.restoredConversationPersistMux.Lock()
	defer api.restoredConversationPersistMux.Unlock()
	delete(api.restoredConversationPersistTargets, currentSessionID)
}

func readRestoredChatHistoryPersistTargetFromPath(userID, conversationPath string) (*restoredChatHistoryPersistTarget, bool, error) {
	normalizedPath, ok := normalizeRestoredChatHistoryConversationPath(userID, conversationPath)
	if !ok {
		return nil, false, nil
	}
	data, exists, err := readChatHistoryConversationDataFromPath(normalizedPath)
	if err != nil || !exists {
		return nil, exists, err
	}
	return parseRestoredChatHistoryPersistTarget(data, chatHistorySessionIDFromConversationPath(userID, normalizedPath), normalizedPath)
}

func readRestoredChatHistoryPersistTargetForSession(userID, sessionID, workspacePath string) (*restoredChatHistoryPersistTarget, bool, error) {
	sessionID = sanitizeChatHistorySessionID(sessionID)
	if sessionID == "" {
		return nil, false, nil
	}
	data, err := ReadChatHistoryConversation(userID, sessionID, workspacePath)
	if err != nil {
		if strings.Contains(err.Error(), "conversation not found") {
			return nil, false, nil
		}
		return nil, false, err
	}
	conversationPath := ""
	if path, ok, err := FindChatHistoryConversationPathForSession(userID, sessionID, workspacePath); err != nil {
		return nil, false, err
	} else if ok {
		conversationPath = path
	}
	return parseRestoredChatHistoryPersistTarget([]byte(data), sessionID, conversationPath)
}

func parseRestoredChatHistoryPersistTarget(data []byte, fallbackSessionID, conversationPath string) (*restoredChatHistoryPersistTarget, bool, error) {
	var raw struct {
		SessionID string                    `json:"session_id"`
		Runtime   *ChatHistoryAgentRuntime  `json:"runtime,omitempty"`
		Mode      string                    `json:"workshop_mode,omitempty"`
		History   []llmtypes.MessageContent `json:"conversation_history"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, err
	}
	sessionID := sanitizeChatHistorySessionID(raw.SessionID)
	if sessionID == "" {
		sessionID = sanitizeChatHistorySessionID(fallbackSessionID)
	}
	if sessionID == "" || strings.TrimSpace(conversationPath) == "" {
		return nil, false, nil
	}
	if raw.Runtime != nil && raw.Runtime.WorkshopMode == "" {
		raw.Runtime.WorkshopMode = normalizeChatHistoryWorkshopMode(raw.Mode)
	}
	normalizeChatHistoryRuntime(raw.Runtime)
	return &restoredChatHistoryPersistTarget{
		SessionID:        sessionID,
		ConversationPath: conversationPath,
		History:          raw.History,
		Runtime:          raw.Runtime,
	}, true, nil
}

func chatHistorySessionIDFromConversationPath(userID, conversationPath string) string {
	conversationPath = strings.Trim(pathpkg.Clean(filepath.ToSlash(conversationPath)), "/")
	if id, ok := chatHistorySessionIDFromWorkspacePath(chatHistoryRoot(userID), conversationPath); ok {
		return id
	}
	if id := chatHistorySessionIDFromFileName(pathpkg.Base(conversationPath)); id != "" {
		return id
	}
	if pathpkg.Base(conversationPath) == "conversation.json" {
		return sanitizeChatHistorySessionID(pathpkg.Base(pathpkg.Dir(conversationPath)))
	}
	return ""
}

func mergeRestoredChatHistory(existing, incoming []llmtypes.MessageContent) []llmtypes.MessageContent {
	if len(existing) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return existing
	}
	if chatHistoryHasPrefix(incoming, existing) {
		return incoming
	}
	maxOverlap := len(existing)
	if len(incoming) < maxOverlap {
		maxOverlap = len(incoming)
	}
	for overlap := maxOverlap; overlap > 0; overlap-- {
		matched := true
		for i := 0; i < overlap; i++ {
			if !chatHistoryMessagesEqual(existing[len(existing)-overlap+i], incoming[i]) {
				matched = false
				break
			}
		}
		if matched {
			merged := make([]llmtypes.MessageContent, 0, len(existing)+len(incoming)-overlap)
			merged = append(merged, existing...)
			merged = append(merged, incoming[overlap:]...)
			return merged
		}
	}
	merged := make([]llmtypes.MessageContent, 0, len(existing)+len(incoming))
	merged = append(merged, existing...)
	merged = append(merged, incoming...)
	return merged
}

func chatHistoryHasPrefix(history, prefix []llmtypes.MessageContent) bool {
	if len(prefix) > len(history) {
		return false
	}
	for i := range prefix {
		if !chatHistoryMessagesEqual(history[i], prefix[i]) {
			return false
		}
	}
	return true
}

func chatHistoryMessagesEqual(a, b llmtypes.MessageContent) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && string(aJSON) == string(bJSON)
}

func trimChatHistoryUIEvents(uiEvents []internalevents.Event) []internalevents.Event {
	if len(uiEvents) <= maxPersistedChatHistoryUIEvents {
		return uiEvents
	}
	trimmed := make([]internalevents.Event, maxPersistedChatHistoryUIEvents)
	copy(trimmed, uiEvents[len(uiEvents)-maxPersistedChatHistoryUIEvents:])
	return trimmed
}

// ListChatHistorySessions returns persisted session metadata for a user, newest first.
func ListChatHistorySessions(userID string, limit, offset int, workspacePath string) ([]ChatHistorySession, error) {
	if userID == "" {
		userID = "default"
	}
	root := chatHistoryRoot(userID)
	workspacePath = normalizeChatHistoryWorkspacePath(workspacePath)

	if workspacePath != "" {
		if sessions, ok, err := listWorkflowScopedChatHistorySessionsFromDisk(userID, root, workspacePath, limit, offset); ok || err != nil {
			return sessions, err
		}
	}

	if sessions, ok, err := listChatHistorySessionsFromDisk(userID, root, "", limit, offset); ok || err != nil {
		return sessions, err
	}

	filePaths, err := listWorkspaceFilesRecursive(context.Background(), root)
	if err != nil {
		return nil, err
	}

	sessionsByID := make(map[string]ChatHistorySession)
	for _, convPath := range filePaths {
		sessionID, ok := chatHistorySessionIDFromWorkspacePath(root, convPath)
		if !ok {
			continue
		}
		data, exists, err := readFileFromWorkspace(context.Background(), convPath)
		if err != nil || !exists {
			continue
		}

		if workspacePath != "" && !chatHistoryDataMatchesWorkspace(data, workspacePath) {
			continue
		}

		session, ok := parseLocalChatHistorySession(userID, root, workspacePath, sessionID, data, time.Now())
		if !ok {
			continue
		}
		session.ConversationPath = convPath
		if existing, ok := sessionsByID[session.SessionID]; !ok || session.UpdatedAt > existing.UpdatedAt || (session.UpdatedAt == existing.UpdatedAt && session.ConversationPath > existing.ConversationPath) {
			sessionsByID[session.SessionID] = session
		}
	}

	sessions := make([]ChatHistorySession, 0, len(sessionsByID))
	for _, session := range sessionsByID {
		sessions = append(sessions, session)
	}

	// Sort by UpdatedAt descending
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})

	// Apply pagination
	if offset >= len(sessions) {
		return []ChatHistorySession{}, nil
	}
	sessions = sessions[offset:]
	if limit > 0 && limit < len(sessions) {
		sessions = sessions[:limit]
	}

	return sessions, nil
}

type localChatHistoryFile struct {
	sessionID     string
	convPath      string
	workspacePath string
	modTime       time.Time
}

// listChatHistorySessionsFromDisk avoids hundreds of workspace-API reads when
// the agent server and workspace docs are on the same machine. It sorts by the
// conversation file mtime first, then reads only the requested page.
func listChatHistorySessionsFromDisk(userID, workspaceRoot, workflowPath string, limit, offset int) ([]ChatHistorySession, bool, error) {
	baseDir, ok := resolveLocalChatHistoryDir(workspaceRoot)
	if !ok {
		return nil, false, nil
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ChatHistorySession{}, true, nil
		}
		return nil, true, err
	}

	filesBySession := make(map[string]localChatHistoryFile)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryName := entry.Name()

		// Legacy layout: chat_history/<session-id>/conversation.json
		legacyConvPath := filepath.Join(baseDir, entryName, "conversation.json")
		if info, err := os.Stat(legacyConvPath); err == nil && !info.IsDir() {
			addLocalChatHistoryFile(filesBySession, localChatHistoryFile{
				sessionID:     entryName,
				convPath:      legacyConvPath,
				workspacePath: pathpkg.Join(workspaceRoot, entryName, "conversation.json"),
				modTime:       info.ModTime(),
			})
		}

		// Date-bucket layout: chat_history/YYYY-MM-DD/session-<id>-conversation.json
		dateDir := filepath.Join(baseDir, entryName)
		matches, err := filepath.Glob(filepath.Join(dateDir, "session-*-conversation.json"))
		if err != nil {
			return nil, true, err
		}
		for _, convPath := range matches {
			info, err := os.Stat(convPath)
			if err != nil || info.IsDir() {
				continue
			}
			sessionID := chatHistorySessionIDFromFileName(filepath.Base(convPath))
			if sessionID == "" {
				continue
			}
			addLocalChatHistoryFile(filesBySession, localChatHistoryFile{
				sessionID:     sessionID,
				convPath:      convPath,
				workspacePath: pathpkg.Join(workspaceRoot, entryName, filepath.Base(convPath)),
				modTime:       info.ModTime(),
			})
		}
	}

	files := make([]localChatHistoryFile, 0, len(filesBySession))
	for _, file := range filesBySession {
		files = append(files, file)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	if offset >= len(files) {
		return []ChatHistorySession{}, true, nil
	}
	files = files[offset:]
	if limit > 0 && limit < len(files) {
		files = files[:limit]
	}

	sessions := make([]ChatHistorySession, 0, len(files))
	for _, file := range files {
		session, ok := readLocalChatHistorySession(userID, workspaceRoot, workflowPath, file)
		if ok {
			sessions = append(sessions, session)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})
	return sessions, true, nil
}

func addLocalChatHistoryFile(filesBySession map[string]localChatHistoryFile, file localChatHistoryFile) {
	if file.sessionID == "" {
		return
	}
	if existing, ok := filesBySession[file.sessionID]; !ok || file.modTime.After(existing.modTime) || (file.modTime.Equal(existing.modTime) && file.workspacePath > existing.workspacePath) {
		filesBySession[file.sessionID] = file
	}
}

func chatHistorySessionIDFromFileName(fileName string) string {
	if !strings.HasPrefix(fileName, "session-") || !strings.HasSuffix(fileName, "-conversation.json") {
		return ""
	}
	sessionID := strings.TrimSuffix(strings.TrimPrefix(fileName, "session-"), "-conversation.json")
	return sanitizeChatHistorySessionID(sessionID)
}

func chatHistorySessionIDFromWorkspacePath(root, convPath string) (string, bool) {
	root = strings.Trim(pathpkg.Clean(root), "/")
	convPath = strings.Trim(pathpkg.Clean(filepath.ToSlash(convPath)), "/")
	if root == "" || convPath == "" || !strings.HasPrefix(convPath, root+"/") {
		return "", false
	}
	rel := strings.TrimPrefix(convPath, root+"/")
	parts := strings.Split(rel, "/")
	if len(parts) == 2 && parts[1] == "conversation.json" {
		sessionID := sanitizeChatHistorySessionID(parts[0])
		return sessionID, sessionID != ""
	}
	if len(parts) == 2 {
		sessionID := chatHistorySessionIDFromFileName(parts[1])
		return sessionID, sessionID != ""
	}
	return "", false
}

func listWorkflowScopedChatHistorySessionsFromDisk(userID, chatHistoryRootPath, workflowPath string, limit, offset int) ([]ChatHistorySession, bool, error) {
	all := make([]ChatHistorySession, 0)

	// Workflow builder files are the most precise source for /resume inside a
	// workflow. Do not include global chat_history matches here: those can
	// mention this workflow in pasted context while belonging to another chat.
	// Pass the page budget so only the requested page of conversation files is
	// read+parsed (previews are expensive); the rest are only stat'd.
	readBudget := 0
	if limit > 0 {
		readBudget = limit + offset
	}
	if builderSessions, ok := listWorkflowBuilderHistoryFromDisk(userID, workflowPath, readBudget); ok {
		all = append(all, builderSessions...)
	}

	return paginateChatHistorySessions(all, limit, offset), true, nil
}

// listWorkflowBuilderHistoryFromDisk returns builder chat sessions for a workflow.
// readBudget caps how many conversation files are actually READ+PARSED (the costly
// part — preview building): we stat every file (cheap) and dedupe to the latest
// file per session by filename+mtime WITHOUT reading, sort by mtime, then read
// only the top readBudget. readBudget<=0 reads all (unlimited list).
func listWorkflowBuilderHistoryFromDisk(userID, workflowPath string, readBudget int) ([]ChatHistorySession, bool) {
	workflowDir, ok := resolveLocalWorkflowDir(workflowPath)
	if !ok {
		return nil, false
	}
	matches, err := workflowBuilderConversationFiles(workflowDir)
	if err != nil {
		return nil, false
	}

	// Cheap pass: stat only, dedupe to the latest file per session id (parsed
	// from the filename — no file read).
	type fileRef struct {
		convPath string
		mtime    time.Time
	}
	latest := make(map[string]fileRef)
	for _, convPath := range matches {
		info, err := os.Stat(convPath)
		if err != nil {
			continue
		}
		sessionID := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(convPath), "session-"), "-conversation.json")
		if cur, ok := latest[sessionID]; !ok || info.ModTime().After(cur.mtime) {
			latest[sessionID] = fileRef{convPath: convPath, mtime: info.ModTime()}
		}
	}

	type sessionRef struct {
		id  string
		ref fileRef
	}
	refs := make([]sessionRef, 0, len(latest))
	for id, r := range latest {
		refs = append(refs, sessionRef{id: id, ref: r})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ref.mtime.After(refs[j].ref.mtime) })
	if readBudget > 0 && readBudget < len(refs) {
		refs = refs[:readBudget]
	}

	// Read+parse only the page.
	sessions := make([]ChatHistorySession, 0, len(refs))
	for _, sr := range refs {
		data, err := os.ReadFile(sr.ref.convPath)
		if err != nil {
			continue
		}
		session, ok := parseLocalChatHistorySession(userID, workflowPath, workflowPath, sr.id, string(data), sr.ref.mtime)
		if !ok {
			continue
		}
		session.AgentMode = "workflow"
		session.ConversationPath = workflowRelativeConversationPath(workflowPath, workflowDir, sr.ref.convPath)
		sessions = append(sessions, session)
	}
	return sessions, true
}

func workflowBuilderConversationFiles(workflowDir string) ([]string, error) {
	patterns := []string{
		filepath.Join(workflowDir, "builder", "session-*-conversation.json"),
		filepath.Join(workflowDir, "builder", "conversation", "*", "session-*-conversation.json"),
	}
	out := make([]string, 0)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		out = append(out, matches...)
	}
	return out, nil
}

func workflowRelativeConversationPath(workflowPath, workflowDir, convPath string) string {
	rel, err := filepath.Rel(workflowDir, convPath)
	if err != nil {
		return pathpkg.Join(workflowPath, "builder", filepath.Base(convPath))
	}
	return pathpkg.Join(workflowPath, filepath.ToSlash(rel))
}

func resolveLocalChatHistoryDir(workspaceRoot string) (string, bool) {
	candidates := []string{
		filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(workspaceRoot)),
		filepath.Join("workspace-docs", filepath.FromSlash(workspaceRoot)),
		filepath.Join("..", "workspace-docs", filepath.FromSlash(workspaceRoot)),
		filepath.Join("/app/workspace-docs", filepath.FromSlash(workspaceRoot)),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func resolveLocalWorkflowDir(workflowPath string) (string, bool) {
	workflowPath = normalizeChatHistoryWorkspacePath(workflowPath)
	if workflowPath == "" {
		return "", false
	}
	candidates := []string{
		filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(workflowPath)),
		filepath.Join("workspace-docs", filepath.FromSlash(workflowPath)),
		filepath.Join("..", "workspace-docs", filepath.FromSlash(workflowPath)),
		filepath.Join("/app/workspace-docs", filepath.FromSlash(workflowPath)),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func readLocalChatHistorySession(userID, workspaceRoot, workflowPath string, file localChatHistoryFile) (ChatHistorySession, bool) {
	data, err := os.ReadFile(file.convPath)
	if err != nil {
		return ChatHistorySession{}, false
	}
	session, ok := parseLocalChatHistorySession(userID, workspaceRoot, workflowPath, file.sessionID, string(data), file.modTime)
	if ok && file.workspacePath != "" {
		session.ConversationPath = file.workspacePath
	}
	return session, ok
}

func parseLocalChatHistorySession(userID, workspaceRoot, workflowPath, fallbackSessionID, data string, fallbackUpdatedAt time.Time) (ChatHistorySession, bool) {
	var raw struct {
		SessionID string                    `json:"session_id"`
		AgentMode string                    `json:"agent_mode"`
		Runtime   *ChatHistoryAgentRuntime  `json:"runtime,omitempty"`
		Mode      string                    `json:"workshop_mode,omitempty"`
		History   []llmtypes.MessageContent `json:"conversation_history"`
		CreatedAt string                    `json:"created_at"`
		UpdatedAt string                    `json:"updated_at"`
	}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return ChatHistorySession{}, false
	}
	if raw.SessionID == "" {
		raw.SessionID = fallbackSessionID
	}
	if raw.UpdatedAt == "" {
		raw.UpdatedAt = raw.CreatedAt
	}
	if raw.UpdatedAt == "" {
		raw.UpdatedAt = fallbackUpdatedAt.Format(time.RFC3339)
	}
	if raw.CreatedAt == "" {
		raw.CreatedAt = raw.UpdatedAt
	}
	raw.Mode = normalizeChatHistoryWorkshopMode(raw.Mode)
	if raw.Runtime != nil && raw.Runtime.WorkshopMode == "" {
		raw.Runtime.WorkshopMode = raw.Mode
	}
	normalizeChatHistoryRuntime(raw.Runtime)

	query := firstHumanText(raw.History)
	if len(query) > 200 {
		query = query[:200] + "..."
	}

	return ChatHistorySession{
		SessionID:        raw.SessionID,
		AgentMode:        raw.AgentMode,
		Runtime:          raw.Runtime,
		WorkshopMode:     raw.Mode,
		Query:            query,
		UserID:           userID,
		WorkspacePath:    workflowPath,
		ConversationPath: pathpkg.Join(workspaceRoot, raw.SessionID, "conversation.json"),
		CreatedAt:        raw.CreatedAt,
		UpdatedAt:        raw.UpdatedAt,
		MessageCount:     len(raw.History),
		PreviewMessages:  chatHistoryPreviewMessages(raw.History),
	}, true
}

// normalizeChatHistoryWorkshopMode canonicalizes a mode string from any of
// the supported input forms into one of the two backend mode names:
// "workshop", "run", or "" (unknown / unset).
//
// MERGE NOTE: "builder", "optimizer", and "reporting" are legacy input
// names. All three map to "workshop", the unified mode introduced in Step 5
// of the prompt-restructure migration. Persisted sessions saved before the
// merge still arrive with the legacy names — they continue to load and
// behave like workshop sessions because the merged tool list is a strict
// superset of all three pre-merge surfaces.
func normalizeChatHistoryWorkshopMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "workshop", "builder", "optimizer", "reporting":
		return "workshop"
	case "run":
		return "run"
	case "ask", "debugger", "runner":
		return "run"
	case "eval", "output":
		return "workshop"
	default:
		return ""
	}
}

func paginateChatHistorySessions(sessions []ChatHistorySession, limit, offset int) []ChatHistorySession {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})
	if offset >= len(sessions) {
		return []ChatHistorySession{}
	}
	sessions = sessions[offset:]
	if limit > 0 && limit < len(sessions) {
		sessions = sessions[:limit]
	}
	return sessions
}

func chatHistoryDataMatchesWorkspace(data, workflowPath string) bool {
	workflowPath = normalizeChatHistoryWorkspacePath(workflowPath)
	if workflowPath == "" {
		return true
	}
	workflowName := pathpkg.Base(workflowPath)
	return strings.Contains(data, workflowPath) ||
		strings.Contains(data, filepath.ToSlash(filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(workflowPath)))) ||
		strings.Contains(data, "/workspace-docs/"+workflowPath) ||
		strings.Contains(data, "Workflow/"+workflowName+"/")
}

func normalizeChatHistoryWorkspacePath(workspacePath string) string {
	workspacePath = strings.TrimSpace(strings.Trim(workspacePath, "/"))
	if workspacePath == "" {
		return ""
	}
	cleaned := pathpkg.Clean(workspacePath)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return ""
	}
	return cleaned
}

func firstHumanText(history []llmtypes.MessageContent) string {
	firstText := ""
	for _, msg := range history {
		role := strings.ToLower(strings.TrimSpace(string(msg.Role)))
		if role != "human" && role != "user" {
			continue
		}
		for _, part := range msg.Parts {
			if text := chatHistoryPartText(part); text != "" {
				cleaned := cleanChatHistoryQuery(text)
				if firstText == "" {
					firstText = cleaned
				}
				if !isLowSignalChatHistoryQuery(cleaned) {
					return cleaned
				}
			}
		}
	}
	return firstText
}

func isLowSignalChatHistoryQuery(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = strings.Trim(normalized, ".!?,;:-_ \t\n\r")
	if normalized == "" {
		return true
	}
	if len([]rune(normalized)) <= 2 {
		return true
	}
	lowSignal := map[string]bool{
		"hi":              true,
		"hello":           true,
		"hey":             true,
		"ok":              true,
		"okay":            true,
		"thanks":          true,
		"thank you":       true,
		"yes":             true,
		"no":              true,
		"done":            true,
		"lets start":      true,
		"let's start":     true,
		"start":           true,
		"continue":        true,
		"please continue": true,
	}
	return lowSignal[normalized]
}

func chatHistoryPreviewMessages(history []llmtypes.MessageContent) []ChatHistoryPreviewMessage {
	const maxPreviewMessages = 6
	const maxPreviewChars = 360

	messages := []ChatHistoryPreviewMessage{}
	for _, msg := range history {
		role := strings.ToLower(strings.TrimSpace(string(msg.Role)))
		if role != "human" && role != "user" && role != "ai" && role != "assistant" {
			continue
		}
		textParts := []string{}
		for _, part := range msg.Parts {
			if text := strings.TrimSpace(chatHistoryPartText(part)); text != "" {
				textParts = append(textParts, text)
			}
		}
		text := strings.TrimSpace(strings.Join(textParts, "\n\n"))
		if text == "" {
			continue
		}
		text = cleanChatHistoryQuery(text)
		if shouldSkipChatHistoryPreviewText(text) {
			continue
		}
		if len(text) > maxPreviewChars {
			text = strings.TrimSpace(text[:maxPreviewChars]) + "..."
		}
		displayRole := role
		if displayRole == "user" {
			displayRole = "human"
		}
		if displayRole == "assistant" {
			displayRole = "ai"
		}
		messages = append(messages, ChatHistoryPreviewMessage{
			Role: displayRole,
			Text: text,
		})
	}
	if len(messages) > maxPreviewMessages {
		messages = messages[len(messages)-maxPreviewMessages:]
	}
	return messages
}

func chatHistoryPartText(part interface{}) string {
	switch v := part.(type) {
	case llmtypes.TextContent:
		return v.Text
	case map[string]interface{}:
		if t, ok := v["Text"].(string); ok {
			return t
		}
		if t, ok := v["text"].(string); ok {
			return t
		}
	}
	return ""
}

func shouldSkipChatHistoryPreviewText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "[AUTO-NOTIFICATION]") ||
		strings.HasPrefix(trimmed, "[Previous tool call") ||
		strings.HasPrefix(trimmed, "[Previous tool result")
}

var restoredConversationContextMarkers = []string{
	"\n\nPrevious workflow-builder conversation file:",
	"\n\nPrevious builder chat file available:",
	"\n\nPrevious conversation file:",
}

func cleanChatHistoryQuery(text string) string {
	for _, marker := range restoredConversationContextMarkers {
		if idx := strings.Index(text, marker); idx >= 0 {
			return strings.TrimSpace(text[:idx])
		}
	}
	return strings.TrimSpace(text)
}

func appendRestoredConversationContext(query, path string) string {
	query = strings.TrimSpace(query)
	path = strings.TrimSpace(path)
	if path == "" {
		return query
	}
	for _, marker := range restoredConversationContextMarkers {
		if strings.Contains(query, strings.TrimSpace(marker)) {
			return query
		}
	}
	return query + "\n\nPrevious conversation file: " + path + "\nThis file is JSON with a top-level conversation_history array. User messages have Role \"human\" or \"user\" and text in Parts[].Text; assistant replies have Role \"ai\" or \"assistant\". Scan conversation_history from the end for recent user/assistant Text parts."
}

func shouldAttachRestoredConversationFallback(runtime *ChatHistoryAgentRuntime, currentProvider, currentWorkshopMode string) bool {
	if runtime == nil {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(runtime.Kind), "coding_agent") {
		return true
	}
	if !runtime.ResumeSupported || (strings.TrimSpace(runtime.ExternalSessionID) == "" && strings.TrimSpace(runtime.ProjectDirID) == "") {
		return true
	}

	runtimeProvider := strings.ToLower(strings.TrimSpace(runtime.Provider))
	provider := strings.ToLower(strings.TrimSpace(currentProvider))
	if runtimeProvider == "" || (provider != "" && runtimeProvider != provider) {
		return true
	}

	runtimeMode := normalizeChatHistoryWorkshopMode(runtime.WorkshopMode)
	mode := normalizeChatHistoryWorkshopMode(currentWorkshopMode)
	if runtimeMode != "" && mode != "" && runtimeMode != mode {
		return true
	}

	return false
}

func FindChatHistoryConversationPathForSession(userID, sessionID, workspacePath string) (string, bool, error) {
	sessionID = sanitizeChatHistorySessionID(sessionID)
	if sessionID == "" {
		return "", false, nil
	}
	sessions, err := ListChatHistorySessions(userID, maxChatHistoryFallbackScan, 0, workspacePath)
	if err != nil {
		return "", false, err
	}
	for _, session := range sessions {
		if session.SessionID == sessionID && strings.TrimSpace(session.ConversationPath) != "" {
			return session.ConversationPath, true, nil
		}
	}
	return "", false, nil
}

// cleanChatHistoryForPersistence removes hidden prompt context that the frontend
// appends for model-only use. Persisting that context causes chained /resume
// selections to point at older conversations instead of the visible user turn.
func cleanChatHistoryForPersistence(history []llmtypes.MessageContent) []llmtypes.MessageContent {
	if len(history) == 0 {
		return history
	}
	cleaned := make([]llmtypes.MessageContent, len(history))
	for i, msg := range history {
		cleaned[i] = msg
		role := strings.ToLower(strings.TrimSpace(string(msg.Role)))
		if role != "human" && role != "user" {
			continue
		}
		if len(msg.Parts) == 0 {
			continue
		}
		parts := make([]llmtypes.ContentPart, len(msg.Parts))
		copy(parts, msg.Parts)
		changed := false
		for partIndex, part := range parts {
			if textPart, ok := part.(llmtypes.TextContent); ok {
				cleanText := cleanChatHistoryQuery(textPart.Text)
				if cleanText != textPart.Text {
					parts[partIndex] = llmtypes.TextContent{Text: cleanText}
					changed = true
				}
			}
		}
		if changed {
			cleaned[i].Parts = parts
		}
	}
	return cleaned
}

// ReadChatHistoryConversation reads the persisted conversation JSON for a session.
func ReadChatHistoryConversation(userID, sessionID, workspacePath string) (json.RawMessage, error) {
	if userID == "" {
		userID = "default"
	}
	sessionID = sanitizeChatHistorySessionID(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("conversation not found")
	}
	workspacePath = normalizeChatHistoryWorkspacePath(workspacePath)
	if workspacePath != "" {
		if data, ok, err := readWorkflowScopedChatHistoryConversationDirect(sessionID, workspacePath); ok || err != nil {
			return data, err
		}
		if data, ok, err := readWorkflowScopedChatHistoryConversationFromWorkspace(sessionID, workspacePath); ok || err != nil {
			return data, err
		}
		sessions, err := ListChatHistorySessions(userID, maxChatHistoryFallbackScan, 0, workspacePath)
		if err != nil {
			return nil, err
		}
		for _, session := range sessions {
			if session.SessionID != sessionID || session.ConversationPath == "" {
				continue
			}
			data, exists, err := readFileFromWorkspace(context.Background(), session.ConversationPath)
			if err != nil {
				return nil, err
			}
			if exists {
				return json.RawMessage(data), nil
			}
		}
	}
	if data, ok, err := readUserChatHistoryConversationDirect(userID, sessionID); ok || err != nil {
		return data, err
	}
	if data, ok, err := readUserChatHistoryConversationFromWorkspace(userID, sessionID); ok || err != nil {
		return data, err
	}
	convPath := pathpkg.Join(chatHistoryRoot(userID), sessionID, "conversation.json")
	data, exists, err := readFileFromWorkspace(context.Background(), convPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("conversation not found")
	}
	return json.RawMessage(data), nil
}

func ReadChatHistoryRuntimeFromPath(userID, conversationPath string) (*ChatHistoryAgentRuntime, bool, error) {
	normalizedPath, ok := normalizeRestoredChatHistoryConversationPath(userID, conversationPath)
	if !ok {
		return nil, false, nil
	}

	data, exists, err := readChatHistoryConversationDataFromPath(normalizedPath)
	if err != nil || !exists {
		return nil, exists, err
	}

	runtime, err := chatHistoryRuntimeFromJSON(data)
	if err != nil {
		return nil, true, err
	}
	return runtime, true, nil
}

func ReadChatHistoryRuntimeForSession(userID, sessionID, workspacePath string) (*ChatHistoryAgentRuntime, bool, error) {
	data, err := ReadChatHistoryConversation(userID, sessionID, workspacePath)
	if err != nil {
		if strings.Contains(err.Error(), "conversation not found") {
			return nil, false, nil
		}
		return nil, false, err
	}
	runtime, err := chatHistoryRuntimeFromJSON(data)
	if err != nil {
		return nil, true, err
	}
	return runtime, true, nil
}

func ReadChatHistoryTerminalSnapshotsFromPath(userID, conversationPath string) ([]terminals.Snapshot, bool, error) {
	normalizedPath, ok := normalizeRestoredChatHistoryConversationPath(userID, conversationPath)
	if !ok {
		return nil, false, nil
	}

	data, exists, err := readChatHistoryConversationDataFromPath(normalizedPath)
	if err != nil || !exists {
		return nil, exists, err
	}

	snapshots, err := chatHistoryTerminalSnapshotsFromJSON(data)
	if err != nil {
		return nil, true, err
	}
	return snapshots, true, nil
}

func ReadChatHistoryTerminalSnapshotsForSession(userID, sessionID, workspacePath string) ([]terminals.Snapshot, bool, error) {
	data, err := ReadChatHistoryConversation(userID, sessionID, workspacePath)
	if err != nil {
		if strings.Contains(err.Error(), "conversation not found") {
			return nil, false, nil
		}
		return nil, false, err
	}
	snapshots, err := chatHistoryTerminalSnapshotsFromJSON(data)
	if err != nil {
		return nil, true, err
	}
	return snapshots, true, nil
}

func readChatHistoryConversationDataFromPath(normalizedPath string) ([]byte, bool, error) {
	normalizedPath = strings.TrimSpace(filepath.ToSlash(normalizedPath))
	if normalizedPath == "" {
		return nil, false, nil
	}
	localPath := filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(normalizedPath))
	if localData, err := os.ReadFile(localPath); err == nil {
		return localData, true, nil
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	workspaceData, exists, err := readFileFromWorkspace(context.Background(), normalizedPath)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}
	return []byte(workspaceData), true, nil
}

func chatHistoryTerminalSnapshotsFromJSON(data []byte) ([]terminals.Snapshot, error) {
	var raw struct {
		TerminalSnapshots []terminals.Snapshot `json:"terminal_snapshots,omitempty"`
		TerminalSnapshot  *terminals.Snapshot  `json:"terminal_snapshot,omitempty"`
		SessionID         string               `json:"session_id,omitempty"`
		Runtime           *ChatHistoryAgentRuntime
		UIEvents          []chatHistoryTerminalUIEvent `json:"ui_events,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	snapshots := raw.TerminalSnapshots
	if len(snapshots) == 0 && raw.TerminalSnapshot != nil {
		snapshots = []terminals.Snapshot{*raw.TerminalSnapshot}
	}
	out := make([]terminals.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		prepared, ok := prepareChatHistoryTerminalSnapshot(snapshot)
		if ok {
			out = append(out, prepared)
		}
	}
	if len(out) == 0 {
		out = chatHistoryTerminalSnapshotsFromUIEvents(raw.SessionID, raw.Runtime, raw.UIEvents)
	}
	return out, nil
}

type chatHistoryTerminalUIEvent struct {
	Type          string                 `json:"type,omitempty"`
	Timestamp     time.Time              `json:"timestamp,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
	ExecutionID   string                 `json:"execution_id,omitempty"`
	ExecutionKind string                 `json:"execution_kind,omitempty"`
	Data          *chatHistoryAgentEvent `json:"data,omitempty"`
}

type chatHistoryAgentEvent struct {
	Type      string                         `json:"type,omitempty"`
	Timestamp time.Time                      `json:"timestamp,omitempty"`
	SessionID string                         `json:"session_id,omitempty"`
	Content   string                         `json:"content,omitempty"`
	Metadata  map[string]interface{}         `json:"metadata,omitempty"`
	Data      *chatHistoryStreamingEventData `json:"data,omitempty"`
}

type chatHistoryStreamingEventData struct {
	Timestamp  time.Time              `json:"timestamp,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	Content    string                 `json:"content,omitempty"`
	ChunkIndex int                    `json:"chunk_index,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

func chatHistoryTerminalSnapshotsFromUIEvents(sessionID string, runtime *ChatHistoryAgentRuntime, events []chatHistoryTerminalUIEvent) []terminals.Snapshot {
	if len(events) == 0 {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)

	var selected terminals.Snapshot
	for _, event := range events {
		snapshot, ok := chatHistoryTerminalSnapshotFromUIEvent(sessionID, runtime, event)
		if !ok {
			continue
		}
		if selected.Content == "" || persistedTerminalSnapshotPreferred(snapshot, selected) {
			selected = snapshot
		}
	}
	if strings.TrimSpace(selected.Content) == "" {
		return nil
	}
	return []terminals.Snapshot{selected}
}

func chatHistoryTerminalSnapshotFromUIEvent(sessionID string, runtime *ChatHistoryAgentRuntime, event chatHistoryTerminalUIEvent) (terminals.Snapshot, bool) {
	if event.Data == nil {
		return terminals.Snapshot{}, false
	}
	metadata := event.Data.Metadata
	if event.Data.Data != nil && len(event.Data.Data.Metadata) > 0 {
		metadata = event.Data.Data.Metadata
	}
	if strings.ToLower(strings.TrimSpace(chatHistoryMetadataString(metadata, "kind"))) != "terminal" {
		return terminals.Snapshot{}, false
	}
	eventType := strings.ToLower(strings.TrimSpace(event.Type))
	agentEventType := strings.ToLower(strings.TrimSpace(event.Data.Type))
	if eventType != "streaming_chunk" && agentEventType != "streaming_chunk" {
		return terminals.Snapshot{}, false
	}
	tmuxSession := chatHistoryMetadataString(metadata,
		"tmux_session",
		"codex_interactive_session",
		"claude_code_interactive_session",
		"gemini_interactive_session",
	)
	if tmuxSession == "" {
		return terminals.Snapshot{}, false
	}

	content := event.Data.Content
	chunkIndex := 0
	if event.Data.Data != nil {
		if strings.TrimSpace(event.Data.Data.Content) != "" {
			content = event.Data.Data.Content
		}
		chunkIndex = event.Data.Data.ChunkIndex
	}
	if strings.TrimSpace(content) == "" {
		return terminals.Snapshot{}, false
	}

	if sessionID == "" {
		sessionID = strings.TrimSpace(event.SessionID)
	}
	if sessionID == "" && event.Data.Data != nil {
		sessionID = strings.TrimSpace(event.Data.Data.SessionID)
	}
	if sessionID == "" {
		return terminals.Snapshot{}, false
	}

	provider := chatHistoryMetadataString(metadata, "provider")
	workflowPath := ""
	if runtime != nil {
		if provider == "" {
			provider = strings.TrimSpace(runtime.Provider)
		}
		workflowPath = strings.TrimSpace(runtime.WorkspacePath)
	}
	label := "Restored terminal"
	if provider != "" {
		label = "Restored " + provider
	}
	updatedAt := chatHistoryTerminalEventTime(event)
	snapshot := terminals.Snapshot{
		TerminalID:    sessionID + ":main:" + sessionID,
		SessionID:     sessionID,
		OwnerID:       "main:" + sessionID,
		ExecutionID:   strings.TrimSpace(event.ExecutionID),
		ExecutionKind: strings.TrimSpace(event.ExecutionKind),
		Label:         label,
		Scope:         "main_agent",
		WorkflowPath:  workflowPath,
		StepID:        "main_agent:" + sessionID,
		StepTransport: "tmux",
		TmuxSession:   tmuxSession,
		Content:       content,
		ContentSource: "tmux_capture",
		ChunkIndex:    chunkIndex,
		Active:        false,
		State:         "stale",
		CreatedAt:     updatedAt,
		UpdatedAt:     updatedAt,
	}
	if snapshot.ExecutionID == "" {
		snapshot.ExecutionID = "main:" + sessionID
	}
	if snapshot.ExecutionKind == "" {
		snapshot.ExecutionKind = "main_agent"
	}
	return prepareChatHistoryTerminalSnapshot(snapshot)
}

func chatHistoryTerminalEventTime(event chatHistoryTerminalUIEvent) time.Time {
	if !event.Timestamp.IsZero() {
		return event.Timestamp
	}
	if event.Data != nil {
		if !event.Data.Timestamp.IsZero() {
			return event.Data.Timestamp
		}
		if event.Data.Data != nil && !event.Data.Data.Timestamp.IsZero() {
			return event.Data.Data.Timestamp
		}
	}
	return time.Now()
}

func chatHistoryMetadataString(metadata map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func chatHistoryRuntimeFromJSON(data []byte) (*ChatHistoryAgentRuntime, error) {
	var raw struct {
		Runtime *ChatHistoryAgentRuntime `json:"runtime,omitempty"`
		Mode    string                   `json:"workshop_mode,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Runtime == nil {
		return nil, nil
	}
	if raw.Runtime.WorkshopMode == "" {
		raw.Runtime.WorkshopMode = normalizeChatHistoryWorkshopMode(raw.Mode)
	}
	normalizeChatHistoryRuntime(raw.Runtime)
	return raw.Runtime, nil
}

func normalizeChatHistoryRuntime(runtime *ChatHistoryAgentRuntime) {
	if runtime == nil {
		return
	}
	runtime.Provider = strings.ToLower(strings.TrimSpace(runtime.Provider))
	runtime.ModelID = strings.TrimSpace(runtime.ModelID)
	runtime.Transport = strings.ToLower(strings.TrimSpace(runtime.Transport))
	if runtime.AgentSessionHandle != nil && !runtime.AgentSessionHandle.Empty() {
		handle := runtime.AgentSessionHandle.Provider
		if runtime.Provider == "" {
			runtime.Provider = strings.ToLower(strings.TrimSpace(handle.Provider))
		}
		if runtime.ModelID == "" {
			runtime.ModelID = strings.TrimSpace(handle.Model)
		}
		if runtime.Transport == "" {
			runtime.Transport = strings.ToLower(strings.TrimSpace(handle.Transport))
		}
		if runtime.ExternalSessionID == "" {
			runtime.ExternalSessionID = strings.TrimSpace(handle.NativeSessionID)
		}
		if runtime.ProjectDirID == "" {
			runtime.ProjectDirID = strings.TrimSpace(handle.ProjectDirID)
		}
		if runtime.ExternalSessionID != "" || runtime.ProjectDirID != "" {
			runtime.ResumeSupported = true
		}
	}
	if runtime.Transport == "" && runtime.Provider != "" {
		if contract, ok := llmproviders.GetCodingAgentProviderContract(llmproviders.Provider(runtime.Provider), runtime.ModelID); ok {
			runtime.Transport = strings.ToLower(string(contract.Transport))
		}
	}
}

func normalizeRestoredChatHistoryConversationPath(userID, conversationPath string) (string, bool) {
	conversationPath = strings.TrimSpace(filepath.ToSlash(conversationPath))
	if conversationPath == "" {
		return "", false
	}
	if idx := strings.LastIndex(conversationPath, "/workspace-docs/"); idx >= 0 {
		conversationPath = conversationPath[idx+len("/workspace-docs/"):]
	}
	conversationPath = strings.TrimPrefix(conversationPath, "/")
	cleaned := pathpkg.Clean(conversationPath)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", false
	}
	userRoot := chatHistoryRoot(userID)
	if cleaned == userRoot || strings.HasPrefix(cleaned, userRoot+"/") {
		return cleaned, true
	}
	if strings.HasPrefix(cleaned, "Workflow/") && strings.Contains(cleaned, "/builder/") && strings.HasSuffix(cleaned, ".json") {
		return cleaned, true
	}
	return "", false
}

func readUserChatHistoryConversationDirect(userID, sessionID string) (json.RawMessage, bool, error) {
	root := chatHistoryRoot(userID)
	baseDir, ok := resolveLocalChatHistoryDir(root)
	if !ok {
		return nil, false, nil
	}

	patterns := []string{
		filepath.Join(baseDir, "*", chatHistoryConversationFileName(sessionID)),
		filepath.Join(baseDir, sessionID, "conversation.json"),
	}
	type candidate struct {
		path      string
		modTime   time.Time
		updatedAt string
	}
	var latest *candidate
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, true, err
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			data, err := os.ReadFile(match)
			if err != nil {
				return nil, true, err
			}
			updatedAt := chatHistoryUpdatedAtFromJSON(string(data))
			if latest == nil || updatedAt > latest.updatedAt || (updatedAt == latest.updatedAt && info.ModTime().After(latest.modTime)) || (updatedAt == latest.updatedAt && info.ModTime().Equal(latest.modTime) && match > latest.path) {
				latest = &candidate{path: match, modTime: info.ModTime(), updatedAt: updatedAt}
			}
		}
	}
	if latest == nil {
		return nil, false, nil
	}
	data, err := os.ReadFile(latest.path)
	if err != nil {
		return nil, true, err
	}
	return json.RawMessage(data), true, nil
}

func readUserChatHistoryConversationFromWorkspace(userID, sessionID string) (json.RawMessage, bool, error) {
	root := chatHistoryRoot(userID)
	filePaths, err := listWorkspaceFilesRecursive(context.Background(), root)
	if err != nil {
		return nil, false, err
	}
	var latestData string
	var latestUpdatedAt string
	var latestPath string
	for _, convPath := range filePaths {
		candidateSessionID, ok := chatHistorySessionIDFromWorkspacePath(root, convPath)
		if !ok || candidateSessionID != sessionID {
			continue
		}
		data, exists, err := readFileFromWorkspace(context.Background(), convPath)
		if err != nil {
			return nil, false, err
		}
		if !exists {
			continue
		}
		updatedAt := chatHistoryUpdatedAtFromJSON(data)
		if latestData == "" || updatedAt > latestUpdatedAt || (updatedAt == latestUpdatedAt && convPath > latestPath) {
			latestData = data
			latestUpdatedAt = updatedAt
			latestPath = convPath
		}
	}
	if latestData == "" {
		return nil, false, nil
	}
	return json.RawMessage(latestData), true, nil
}

func readWorkflowScopedChatHistoryConversationFromWorkspace(sessionID, workspacePath string) (json.RawMessage, bool, error) {
	workspacePath = normalizeChatHistoryWorkspacePath(workspacePath)
	if sessionID == "" || workspacePath == "" {
		return nil, false, nil
	}

	ctx := context.Background()
	fileName := fmt.Sprintf("session-%s-conversation.json", sessionID)
	candidatePaths := []string{
		pathpkg.Join(workspacePath, "builder", fileName),
	}

	conversationRoot := pathpkg.Join(workspacePath, "builder", "conversation")
	dateFolders, err := listWorkspaceChildFolderNames(ctx, conversationRoot)
	if err != nil {
		return nil, false, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dateFolders)))
	for _, dateFolder := range dateFolders {
		dateFolder = strings.TrimSpace(dateFolder)
		if dateFolder == "" || dateFolder == "." || dateFolder == ".." {
			continue
		}
		candidatePaths = append(candidatePaths, pathpkg.Join(conversationRoot, dateFolder, fileName))
	}

	var latestData string
	var latestUpdatedAt string
	var latestPath string
	for _, candidatePath := range candidatePaths {
		data, exists, err := readFileFromWorkspace(ctx, candidatePath)
		if err != nil {
			return nil, false, err
		}
		if !exists {
			continue
		}
		updatedAt := chatHistoryUpdatedAtFromJSON(data)
		if latestData == "" || updatedAt > latestUpdatedAt || (updatedAt == latestUpdatedAt && candidatePath > latestPath) {
			latestData = data
			latestUpdatedAt = updatedAt
			latestPath = candidatePath
		}
	}
	if latestData == "" {
		return nil, false, nil
	}
	return json.RawMessage(latestData), true, nil
}

func chatHistoryUpdatedAtFromJSON(data string) string {
	var raw struct {
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return ""
	}
	return strings.TrimSpace(raw.UpdatedAt)
}

func sanitizeChatHistorySessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || sessionID == "." || sessionID == ".." ||
		strings.Contains(sessionID, "/") || strings.Contains(sessionID, "\\") {
		return ""
	}
	return sessionID
}

func readWorkflowScopedChatHistoryConversationDirect(sessionID, workspacePath string) (json.RawMessage, bool, error) {
	workflowDir, ok := resolveLocalWorkflowDir(workspacePath)
	if !ok {
		return nil, false, nil
	}
	fileName := fmt.Sprintf("session-%s-conversation.json", sessionID)
	patterns := []string{
		filepath.Join(workflowDir, "builder", fileName),
		filepath.Join(workflowDir, "builder", "conversation", "*", fileName),
	}

	type candidate struct {
		path    string
		modTime time.Time
	}
	var latest *candidate
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, true, err
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			if latest == nil || info.ModTime().After(latest.modTime) || (info.ModTime().Equal(latest.modTime) && match > latest.path) {
				latest = &candidate{path: match, modTime: info.ModTime()}
			}
		}
	}
	if latest == nil {
		return nil, false, nil
	}
	data, err := os.ReadFile(latest.path)
	if err != nil {
		return nil, true, err
	}
	return json.RawMessage(data), true, nil
}

func findWorkflowScopedChatHistoryConversationPath(sessionID, workspacePath string) (string, bool, error) {
	sessionID = sanitizeChatHistorySessionID(sessionID)
	workspacePath = normalizeChatHistoryWorkspacePath(workspacePath)
	if sessionID == "" || workspacePath == "" {
		return "", false, nil
	}

	fileName := fmt.Sprintf("session-%s-conversation.json", sessionID)
	if workflowDir, ok := resolveLocalWorkflowDir(workspacePath); ok {
		patterns := []string{
			filepath.Join(workflowDir, "builder", fileName),
			filepath.Join(workflowDir, "builder", "conversation", "*", fileName),
		}
		type candidate struct {
			path    string
			modTime time.Time
		}
		var latest *candidate
		for _, pattern := range patterns {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return "", false, err
			}
			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil || info.IsDir() {
					continue
				}
				if latest == nil || info.ModTime().After(latest.modTime) || (info.ModTime().Equal(latest.modTime) && match > latest.path) {
					latest = &candidate{path: match, modTime: info.ModTime()}
				}
			}
		}
		if latest != nil {
			return workspaceRelativePathFromLocalPath(latest.path), true, nil
		}
	}

	conversationRoot := pathpkg.Join(workspacePath, "builder", "conversation")
	filePaths, err := listWorkspaceFilesRecursive(context.Background(), conversationRoot)
	if err != nil {
		return "", false, err
	}
	latestPath := ""
	for _, candidatePath := range filePaths {
		candidatePath = filepath.ToSlash(candidatePath)
		if pathpkg.Base(candidatePath) != fileName {
			continue
		}
		if latestPath == "" || candidatePath > latestPath {
			latestPath = candidatePath
		}
	}
	if latestPath == "" {
		return "", false, nil
	}
	return workspaceRelativePathFromLocalPath(latestPath), true, nil
}

func workspaceRelativePathFromLocalPath(localPath string) string {
	slashPath := filepath.ToSlash(localPath)
	root := filepath.ToSlash(fsutil.WorkspaceDocsRoot())
	if rel, err := filepath.Rel(root, localPath); err == nil {
		rel = filepath.ToSlash(rel)
		if rel != "." && !strings.HasPrefix(rel, "../") && rel != ".." {
			return rel
		}
	}
	if idx := strings.LastIndex(slashPath, "/workspace-docs/"); idx >= 0 {
		return slashPath[idx+len("/workspace-docs/"):]
	}
	return strings.TrimPrefix(slashPath, "/")
}

func DeleteChatHistorySession(userID, sessionID, workspacePath string) (ChatHistoryCleanupResult, error) {
	if userID == "" {
		userID = "default"
	}
	sessionID = sanitizeChatHistorySessionID(sessionID)
	workspacePath = normalizeChatHistoryWorkspacePath(workspacePath)
	result := ChatHistoryCleanupResult{
		DeletedPaths: []string{},
		Scope:        "global",
	}
	if workspacePath != "" {
		result.Scope = workspacePath
	}
	if sessionID == "" {
		return result, fmt.Errorf("invalid session id")
	}

	if workspacePath != "" {
		return deleteWorkflowChatHistorySession(result, sessionID, workspacePath)
	}
	return deleteUserChatHistorySession(result, userID, sessionID)
}

func deleteWorkflowChatHistorySession(result ChatHistoryCleanupResult, sessionID, workspacePath string) (ChatHistoryCleanupResult, error) {
	workflowDir, ok := resolveLocalWorkflowDir(workspacePath)
	if !ok {
		return result, nil
	}
	fileName := fmt.Sprintf("session-%s-conversation.json", sessionID)
	patterns := []string{
		filepath.Join(workflowDir, "builder", fileName),
		filepath.Join(workflowDir, "builder", "conversation", "*", fileName),
	}
	seen := map[string]bool{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return result, err
		}
		for _, convPath := range matches {
			if seen[convPath] {
				continue
			}
			seen[convPath] = true
			info, err := os.Stat(convPath)
			if err != nil || info.IsDir() {
				continue
			}
			if err := os.Remove(convPath); err != nil {
				return result, err
			}
			result.DeletedCount++
			result.DeletedPaths = append(result.DeletedPaths, workflowRelativeConversationPath(workspacePath, workflowDir, convPath))
			parentDir := filepath.Dir(convPath)
			if filepath.Base(filepath.Dir(parentDir)) == "conversation" {
				_ = os.Remove(parentDir)
			}
		}
	}
	return result, nil
}

func deleteUserChatHistorySession(result ChatHistoryCleanupResult, userID, sessionID string) (ChatHistoryCleanupResult, error) {
	root := chatHistoryRoot(userID)
	baseDir, ok := resolveLocalChatHistoryDir(root)
	if !ok {
		return result, nil
	}
	patterns := []string{
		filepath.Join(baseDir, "*", chatHistoryConversationFileName(sessionID)),
		filepath.Join(baseDir, sessionID, "conversation.json"),
	}
	seen := map[string]bool{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return result, err
		}
		for _, convPath := range matches {
			if seen[convPath] {
				continue
			}
			seen[convPath] = true
			info, err := os.Stat(convPath)
			if err != nil || info.IsDir() {
				continue
			}

			parentDir := filepath.Dir(convPath)
			if filepath.Base(convPath) == "conversation.json" && filepath.Base(parentDir) == sessionID {
				if err := os.RemoveAll(parentDir); err != nil {
					return result, err
				}
				result.DeletedCount++
				result.DeletedPaths = append(result.DeletedPaths, pathpkg.Join(root, sessionID))
				continue
			}

			if err := os.Remove(convPath); err != nil {
				return result, err
			}
			result.DeletedCount++
			result.DeletedPaths = append(result.DeletedPaths, pathpkg.Join(root, filepath.Base(parentDir), filepath.Base(convPath)))
			_ = os.Remove(parentDir)
		}
	}
	return result, nil
}

func DeleteChatHistoryOlderThan(userID string, olderThanDays int, workspacePath string) (ChatHistoryCleanupResult, error) {
	if userID == "" {
		userID = "default"
	}
	if olderThanDays <= 0 {
		olderThanDays = 14
	}
	root := chatHistoryRoot(userID)
	workspacePath = normalizeChatHistoryWorkspacePath(workspacePath)
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := ChatHistoryCleanupResult{
		DeletedPaths: []string{},
		Cutoff:       cutoff.Format(time.RFC3339),
		Scope:        "global",
	}
	if workspacePath != "" {
		result.Scope = workspacePath
	}

	if workspacePath != "" {
		if err := deleteOldWorkflowBuilderConversations(&result, workspacePath, cutoff); err != nil {
			return result, err
		}
		return result, nil
	}

	baseDir, ok := resolveLocalChatHistoryDir(root)
	if !ok {
		return result, nil
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryName := entry.Name()
		entryDir := filepath.Join(baseDir, entryName)

		// Legacy layout: chat_history/<session-id>/conversation.json
		legacyConvPath := filepath.Join(entryDir, "conversation.json")
		if shouldDelete, err := chatHistoryFileCleanupCandidate(legacyConvPath, cutoff); err != nil {
			return result, err
		} else if shouldDelete {
			if err := os.RemoveAll(entryDir); err != nil {
				return result, err
			}
			result.DeletedCount++
			result.DeletedPaths = append(result.DeletedPaths, pathpkg.Join(root, entryName))
			continue
		}

		// Date-bucket layout: chat_history/YYYY-MM-DD/session-<id>-conversation.json
		matches, err := filepath.Glob(filepath.Join(entryDir, "session-*-conversation.json"))
		if err != nil {
			return result, err
		}
		for _, convPath := range matches {
			shouldDelete, err := chatHistoryFileCleanupCandidate(convPath, cutoff)
			if err != nil {
				return result, err
			}
			if !shouldDelete {
				continue
			}
			if err := os.Remove(convPath); err != nil {
				return result, err
			}
			result.DeletedCount++
			result.DeletedPaths = append(result.DeletedPaths, pathpkg.Join(root, entryName, filepath.Base(convPath)))
		}
		_ = os.Remove(entryDir)
	}
	return result, nil
}

func deleteOldWorkflowBuilderConversations(result *ChatHistoryCleanupResult, workspacePath string, cutoff time.Time) error {
	workflowDir, ok := resolveLocalWorkflowDir(workspacePath)
	if !ok {
		return nil
	}
	matches, err := workflowBuilderConversationFiles(workflowDir)
	if err != nil {
		return err
	}
	for _, convPath := range matches {
		shouldDelete, err := chatHistoryFileCleanupCandidate(convPath, cutoff)
		if err != nil {
			return err
		}
		if !shouldDelete {
			continue
		}
		if err := os.Remove(convPath); err != nil {
			return err
		}
		result.DeletedCount++
		result.DeletedPaths = append(result.DeletedPaths, workflowRelativeConversationPath(workspacePath, workflowDir, convPath))
	}
	return nil
}

func chatHistoryFileCleanupCandidate(convPath string, cutoff time.Time) (bool, error) {
	info, err := os.Stat(convPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}

	data, err := os.ReadFile(convPath)
	if err != nil {
		return false, err
	}

	var raw struct {
		ConversationHistory []json.RawMessage `json:"conversation_history"`
		CreatedAt           string            `json:"created_at"`
		UpdatedAt           string            `json:"updated_at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, nil
	}
	if len(raw.ConversationHistory) == 0 {
		return false, nil
	}

	timestamp := info.ModTime()
	if parsed, ok := parseChatHistoryCleanupTime(raw.UpdatedAt); ok {
		timestamp = parsed
	} else if parsed, ok := parseChatHistoryCleanupTime(raw.CreatedAt); ok {
		timestamp = parsed
	}

	return timestamp.Before(cutoff), nil
}

func parseChatHistoryCleanupTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed, true
	}
	parsed, err = time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed, true
	}
	return time.Time{}, false
}
