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

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/fsutil"
)

// ChatHistorySession is the metadata returned by the list endpoint.
type ChatHistorySession struct {
	SessionID        string `json:"session_id"`
	AgentMode        string `json:"agent_mode"`
	Status           string `json:"status"`
	Query            string `json:"query,omitempty"`
	UserID           string `json:"user_id"`
	WorkspacePath    string `json:"workspace_path,omitempty"`
	ConversationPath string `json:"conversation_path"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	MessageCount     int    `json:"message_count"`
}

type ChatHistoryCleanupResult struct {
	DeletedCount int      `json:"deleted_count"`
	DeletedPaths []string `json:"deleted_paths"`
	Cutoff       string   `json:"cutoff"`
	Scope        string   `json:"scope"`
}

// chatHistoryRoot returns the workspace-relative path to a user's chat_history root.
func chatHistoryRoot(userID string) string {
	return fmt.Sprintf("_users/%s/chat_history", sanitizeUserIDForPath(userID))
}

// persistChatConversation saves the conversation in the same format as the
// workflow builder: a single conversation.json with the full message history.
// Called inline (not async) right after finalHistory is captured.
func (api *StreamingAPI) persistChatConversation(sessionID, agentMode, userID string, history []llmtypes.MessageContent) {
	if len(history) == 0 {
		return
	}
	if userID == "" {
		userID = "default"
	}
	logCtx := newServerLogContext("", "", agentMode, userID, "", sessionID)

	convData := map[string]interface{}{
		"session_id":           sessionID,
		"agent_mode":           agentMode,
		"conversation_history": history,
		"updated_at":           time.Now().Format(time.RFC3339),
	}

	convJSON, err := json.MarshalIndent(convData, "", "  ")
	if err != nil {
		logfWithContext(logCtx, "[CHAT_HISTORY] Failed to marshal conversation for %s: %v", sessionID, err)
		return
	}

	convPath := pathpkg.Join(chatHistoryRoot(userID), sessionID, "conversation.json")
	if err := writeRawFileToWorkspace(context.Background(), convPath, string(convJSON)); err != nil {
		logfWithContext(logCtx, "[CHAT_HISTORY] Failed to write %s: %v", convPath, err)
		return
	}

	logfWithContext(logCtx, "[CHAT_HISTORY] Saved conversation (%d messages) to %s", len(history), convPath)
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

	sessionIDs, err := listWorkspaceChildFolderNames(context.Background(), root)
	if err != nil {
		return nil, err
	}

	var sessions []ChatHistorySession
	for _, sessionID := range sessionIDs {
		convPath := pathpkg.Join(root, sessionID, "conversation.json")
		data, exists, err := readFileFromWorkspace(context.Background(), convPath)
		if err != nil || !exists {
			continue
		}

		var raw struct {
			SessionID string                    `json:"session_id"`
			AgentMode string                    `json:"agent_mode"`
			History   []llmtypes.MessageContent `json:"conversation_history"`
			UpdatedAt string                    `json:"updated_at"`
		}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}

		query := firstHumanText(raw.History)
		if len(query) > 200 {
			query = query[:200] + "..."
		}

		if workspacePath != "" && !chatHistoryDataMatchesWorkspace(data, workspacePath) {
			continue
		}

		sessions = append(sessions, ChatHistorySession{
			SessionID:        raw.SessionID,
			AgentMode:        raw.AgentMode,
			Query:            query,
			UserID:           userID,
			WorkspacePath:    workspacePath,
			ConversationPath: convPath,
			UpdatedAt:        raw.UpdatedAt,
			MessageCount:     len(raw.History),
		})
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
	sessionID string
	convPath  string
	modTime   time.Time
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

	files := make([]localChatHistoryFile, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		convPath := filepath.Join(baseDir, sessionID, "conversation.json")
		info, err := os.Stat(convPath)
		if err != nil {
			continue
		}
		files = append(files, localChatHistoryFile{
			sessionID: sessionID,
			convPath:  convPath,
			modTime:   info.ModTime(),
		})
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

func listWorkflowScopedChatHistorySessionsFromDisk(userID, chatHistoryRootPath, workflowPath string, limit, offset int) ([]ChatHistorySession, bool, error) {
	all := make([]ChatHistorySession, 0)

	// Workflow builder files are the most precise source for /resume inside a
	// workflow. Do not include global chat_history matches here: those can
	// mention this workflow in pasted context while belonging to another chat.
	if builderSessions, ok := listWorkflowBuilderHistoryFromDisk(userID, workflowPath); ok {
		all = append(all, builderSessions...)
	}

	return paginateChatHistorySessions(all, limit, offset), true, nil
}

func listWorkflowBuilderHistoryFromDisk(userID, workflowPath string) ([]ChatHistorySession, bool) {
	workflowDir, ok := resolveLocalWorkflowDir(workflowPath)
	if !ok {
		return nil, false
	}
	matches, err := workflowBuilderConversationFiles(workflowDir)
	if err != nil {
		return nil, false
	}
	bySessionID := make(map[string]ChatHistorySession)
	for _, convPath := range matches {
		data, err := os.ReadFile(convPath)
		if err != nil {
			continue
		}
		info, err := os.Stat(convPath)
		if err != nil {
			continue
		}
		sessionID := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(convPath), "session-"), "-conversation.json")
		session, ok := parseLocalChatHistorySession(userID, workflowPath, workflowPath, sessionID, string(data), info.ModTime())
		if !ok {
			continue
		}
		session.AgentMode = "workflow"
		session.ConversationPath = workflowRelativeConversationPath(workflowPath, workflowDir, convPath)
		if existing, ok := bySessionID[session.SessionID]; !ok || session.UpdatedAt > existing.UpdatedAt {
			bySessionID[session.SessionID] = session
		}
	}
	sessions := make([]ChatHistorySession, 0, len(bySessionID))
	for _, session := range bySessionID {
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
	return parseLocalChatHistorySession(userID, workspaceRoot, workflowPath, file.sessionID, string(data), file.modTime)
}

func parseLocalChatHistorySession(userID, workspaceRoot, workflowPath, fallbackSessionID, data string, fallbackUpdatedAt time.Time) (ChatHistorySession, bool) {
	var raw struct {
		SessionID string                    `json:"session_id"`
		AgentMode string                    `json:"agent_mode"`
		History   []llmtypes.MessageContent `json:"conversation_history"`
		UpdatedAt string                    `json:"updated_at"`
	}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return ChatHistorySession{}, false
	}
	if raw.SessionID == "" {
		raw.SessionID = fallbackSessionID
	}
	if raw.UpdatedAt == "" {
		raw.UpdatedAt = fallbackUpdatedAt.Format(time.RFC3339)
	}

	query := firstHumanText(raw.History)
	if len(query) > 200 {
		query = query[:200] + "..."
	}

	return ChatHistorySession{
		SessionID:        raw.SessionID,
		AgentMode:        raw.AgentMode,
		Query:            query,
		UserID:           userID,
		WorkspacePath:    workflowPath,
		ConversationPath: pathpkg.Join(workspaceRoot, raw.SessionID, "conversation.json"),
		UpdatedAt:        raw.UpdatedAt,
		MessageCount:     len(raw.History),
	}, true
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
	for _, msg := range history {
		if msg.Role != "human" {
			continue
		}
		for _, part := range msg.Parts {
			switch v := part.(type) {
			case llmtypes.TextContent:
				if v.Text != "" {
					return cleanChatHistoryQuery(v.Text)
				}
			case map[string]interface{}:
				if t, ok := v["Text"].(string); ok && t != "" {
					return cleanChatHistoryQuery(t)
				}
			}
		}
	}
	return ""
}

func cleanChatHistoryQuery(text string) string {
	markers := []string{
		"\n\nPrevious workflow-builder conversation file:",
		"\n\nPrevious builder chat file available:",
	}
	for _, marker := range markers {
		if idx := strings.Index(text, marker); idx >= 0 {
			return strings.TrimSpace(text[:idx])
		}
	}
	return strings.TrimSpace(text)
}

// ReadChatHistoryConversation reads the persisted conversation.json for a session.
func ReadChatHistoryConversation(userID, sessionID string) (json.RawMessage, error) {
	if userID == "" {
		userID = "default"
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
		sessionDir := filepath.Join(baseDir, entry.Name())
		convPath := filepath.Join(sessionDir, "conversation.json")
		info, err := os.Stat(convPath)
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if workspacePath != "" {
			data, err := os.ReadFile(convPath)
			if err != nil || !chatHistoryDataMatchesWorkspace(string(data), workspacePath) {
				continue
			}
		}
		if err := os.RemoveAll(sessionDir); err != nil {
			return result, err
		}
		result.DeletedCount++
		result.DeletedPaths = append(result.DeletedPaths, pathpkg.Join(root, entry.Name()))
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
		info, err := os.Stat(convPath)
		if err != nil || !info.ModTime().Before(cutoff) {
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
