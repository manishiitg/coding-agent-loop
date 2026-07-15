package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
)

const claudeCodeProviderID = "claude-code"

type workflowProviderCredentialRequest struct {
	WorkspacePath  string `json:"workspace_path"`
	EncryptedValue string `json:"encrypted_value"`
}

type workflowProviderCredentialStatus struct {
	Configured bool       `json:"configured"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

func (api *StreamingAPI) handleGetWorkflowClaudeCodeCredential(w http.ResponseWriter, r *http.Request) {
	workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace_path"))
	if workspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	userID := GetUserIDFromContext(r.Context())
	credential, err := api.chatStore.GetWorkflowProviderCredential(r.Context(), userID, workspacePath, claudeCodeProviderID)
	if err != nil {
		http.Error(w, "Failed to load workflow provider credential", http.StatusInternalServerError)
		return
	}
	status := workflowProviderCredentialStatus{Configured: credential != nil}
	if credential != nil {
		updatedAt := credential.UpdatedAt
		status.UpdatedAt = &updatedAt
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (api *StreamingAPI) handleStoreWorkflowClaudeCodeCredential(w http.ResponseWriter, r *http.Request) {
	var req workflowProviderCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.WorkspacePath = strings.TrimSpace(req.WorkspacePath)
	req.EncryptedValue = strings.TrimSpace(req.EncryptedValue)
	if req.WorkspacePath == "" || req.EncryptedValue == "" {
		http.Error(w, "workspace_path and encrypted_value are required", http.StatusBadRequest)
		return
	}

	userID := GetUserIDFromContext(r.Context())
	if api.workflowHasBusySession(userID, req.WorkspacePath) {
		http.Error(w, "Stop the active workflow before changing its Claude Code token", http.StatusConflict)
		return
	}
	token, err := decryptSecretValue(req.EncryptedValue, userID)
	if err != nil {
		http.Error(w, "Invalid encrypted credential", http.StatusBadRequest)
		return
	}
	if err := validateClaudeCodeOAuthToken(r.Context(), token); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := api.chatStore.UpsertWorkflowProviderCredential(r.Context(), userID, req.WorkspacePath, claudeCodeProviderID, req.EncryptedValue); err != nil {
		http.Error(w, "Failed to store workflow provider credential", http.StatusInternalServerError)
		return
	}
	api.closeIdleWorkflowClaudeCodeSessions(userID, req.WorkspacePath, "workflow Claude Code token changed")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (api *StreamingAPI) handleDeleteWorkflowClaudeCodeCredential(w http.ResponseWriter, r *http.Request) {
	workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace_path"))
	if workspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	userID := GetUserIDFromContext(r.Context())
	if api.workflowHasBusySession(userID, workspacePath) {
		http.Error(w, "Stop the active workflow before removing its Claude Code token", http.StatusConflict)
		return
	}
	if err := api.chatStore.DeleteWorkflowProviderCredential(r.Context(), userID, workspacePath, claudeCodeProviderID); err != nil {
		http.Error(w, "Failed to delete workflow provider credential", http.StatusInternalServerError)
		return
	}
	api.closeIdleWorkflowClaudeCodeSessions(userID, workspacePath, "workflow Claude Code token removed")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func validateClaudeCodeOAuthToken(parent context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("Claude Code token is required")
	}
	configDir, err := os.MkdirTemp("", "claude-auth-check-*")
	if err != nil {
		return fmt.Errorf("could not prepare Claude Code credential check")
	}
	defer os.RemoveAll(configDir)

	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "auth", "status", "--json")
	cmd.Env = claudeCredentialCheckEnv(os.Environ(), configDir, token)
	output, err := cmd.Output()
	if err != nil {
		if errorsIsCommandNotFound(err) {
			return fmt.Errorf("Claude Code CLI is not installed")
		}
		return fmt.Errorf("Claude Code did not accept the workflow token")
	}
	var status struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return fmt.Errorf("Claude Code returned an unreadable authentication status")
	}
	if !status.LoggedIn || status.AuthMethod != "oauth_token" {
		return fmt.Errorf("Claude Code did not recognize this as an OAuth token")
	}
	return nil
}

func claudeCredentialCheckEnv(base []string, configDir, token string) []string {
	blocked := map[string]bool{
		"ANTHROPIC_API_KEY":       true,
		"ANTHROPIC_AUTH_TOKEN":    true,
		"ANTHROPIC_BASE_URL":      true,
		"CLAUDE_CODE_OAUTH_TOKEN": true,
		"CLAUDE_CONFIG_DIR":       true,
	}
	out := make([]string, 0, len(base)+2)
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if blocked[key] {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "CLAUDE_CONFIG_DIR="+configDir, "CLAUDE_CODE_OAUTH_TOKEN="+token)
}

func errorsIsCommandNotFound(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr)
}

func (api *StreamingAPI) loadWorkflowClaudeCodeOAuthToken(ctx context.Context, userID, workflowPath string) (*string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(workflowPath) == "" {
		return nil, nil
	}
	credential, err := api.chatStore.GetWorkflowProviderCredential(ctx, userID, workflowPath, claudeCodeProviderID)
	if err != nil || credential == nil {
		return nil, err
	}
	token, err := decryptSecretValue(credential.EncryptedValue, userID)
	if err != nil {
		return nil, fmt.Errorf("decrypt workflow Claude Code credential: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}
	return &token, nil
}

// workflowProviderAPIKeys clones the normal provider configuration and adds
// private credentials for exactly one user/workflow. It is the only Builder
// path allowed to populate ClaudeCodeOAuthToken.
func (api *StreamingAPI) workflowProviderAPIKeys(ctx context.Context, userID, workflowPath string, base *llm.ProviderAPIKeys) (*llm.ProviderAPIKeys, error) {
	keys := base.Clone()
	if keys == nil {
		keys = &llm.ProviderAPIKeys{}
	}
	token, err := api.loadWorkflowClaudeCodeOAuthToken(ctx, userID, workflowPath)
	if err != nil {
		// Preserve unrelated provider credentials while failing closed for the
		// workflow token itself. Callers can then decide whether this workflow
		// operation should stop or continue with Claude Code's saved login.
		keys.ClaudeCodeOAuthToken = nil
		return keys, err
	}
	keys.ClaudeCodeOAuthToken = token
	return keys, nil
}

func normalizeWorkflowCredentialPath(workflowPath string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimSpace(workflowPath), "\\", "/"), "/")
}

func (api *StreamingAPI) workflowSessionIDs(userID, workflowPath string) []string {
	wantedUser := strings.TrimSpace(userID)
	wanted := normalizeWorkflowCredentialPath(workflowPath)
	if wantedUser == "" || wanted == "" {
		return nil
	}
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()
	ids := make([]string, 0)
	for sessionID, session := range api.activeSessions {
		if session == nil || strings.TrimSpace(session.UserID) != wantedUser || normalizeWorkflowCredentialPath(session.WorkspacePath) != wanted {
			continue
		}
		ids = append(ids, sessionID)
	}
	return ids
}

func (api *StreamingAPI) workflowHasBusySession(userID, workflowPath string) bool {
	for _, sessionID := range api.workflowSessionIDs(userID, workflowPath) {
		hasBackgroundAgents := api.bgAgentRegistry != nil && api.bgAgentRegistry.HasRunningAgents(sessionID)
		if api.isSessionBusy(sessionID) || api.canSteerSession(sessionID) || api.hasRunningTrackedExecutionForSession(sessionID) || hasBackgroundAgents {
			return true
		}
	}
	return false
}

func (api *StreamingAPI) closeIdleWorkflowClaudeCodeSessions(userID, workflowPath, reason string) {
	for _, sessionID := range api.workflowSessionIDs(userID, workflowPath) {
		llmproviders.CloseClaudeCodeInteractiveSessionForOwner(sessionID, reason)
		if api.terminalStore == nil {
			continue
		}
		for _, snapshot := range api.terminalStore.ListRaw(sessionID) {
			if !strings.HasPrefix(strings.TrimSpace(snapshot.TmuxSession), "mlp-claude-code") {
				continue
			}
			llmproviders.CloseClaudeCodeInteractiveSessionByTmux(snapshot.TmuxSession, reason)
			api.terminalStore.MarkProcessClosed(snapshot.TerminalID, reason)
		}
	}
}
