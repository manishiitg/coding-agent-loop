package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/mux"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

const listTerminalContentMaxBytes = 64 * 1024
const terminalTmuxActionTimeout = 5 * time.Second

var runTerminalTmuxCommand = func(ctx context.Context, stdin string, args ...string) error {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, message)
	}
	return nil
}

var runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, message)
	}
	return string(output), nil
}

var forceCompleteCodingAgentTmuxSession = llmproviders.ForceCompleteCodingAgentTmuxSession

type listTerminalsResponse struct {
	Terminals []terminals.Snapshot `json:"terminals"`
	Total     int                  `json:"total"`
}

type sendTerminalInputRequest struct {
	Text   string `json:"text"`
	Submit bool   `json:"submit,omitempty"`
}

type sendTerminalKeyRequest struct {
	Key string `json:"key"`
}

type terminalActionResponse struct {
	OK       bool               `json:"ok"`
	Terminal terminals.Snapshot `json:"terminal,omitempty"`
}

// handleListTerminals returns current view-only terminal snapshots.
// GET /api/terminals?session_id=<session>
func (api *StreamingAPI) handleListTerminals(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if api.terminalStore == nil {
		_ = json.NewEncoder(w).Encode(listTerminalsResponse{Terminals: []terminals.Snapshot{}})
		return
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	contentMode := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("content")))
	snapshots := api.terminalStore.List(sessionID)
	filtered := make([]terminals.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.SessionID == "" {
			continue
		}
		if api.canAccessTerminalSession(r, snapshot.SessionID) {
			filtered = append(filtered, compactTerminalSnapshotForList(api.enrichTerminalSnapshot(snapshot), contentMode))
		}
	}

	_ = json.NewEncoder(w).Encode(listTerminalsResponse{
		Terminals: filtered,
		Total:     len(filtered),
	})
}

// handleGetTerminal returns one current view-only terminal snapshot.
// GET /api/terminals/{terminal_id}
func (api *StreamingAPI) handleGetTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if api.terminalStore == nil {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}

	terminalID := strings.TrimSpace(mux.Vars(r)["terminal_id"])
	if terminalID == "" {
		http.Error(w, "Terminal ID is required", http.StatusBadRequest)
		return
	}
	snapshot, ok := api.terminalStore.Get(terminalID)
	if !ok || !api.canAccessTerminalSession(r, snapshot.SessionID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}

	_ = json.NewEncoder(w).Encode(api.enrichTerminalSnapshot(snapshot))
}

// handleDismissTerminal removes one terminal snapshot from the UI.
// DELETE /api/terminals/{terminal_id}
func (api *StreamingAPI) handleDismissTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if api.terminalStore == nil {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}

	terminalID := strings.TrimSpace(mux.Vars(r)["terminal_id"])
	if terminalID == "" {
		http.Error(w, "Terminal ID is required", http.StatusBadRequest)
		return
	}
	snapshot, ok := api.terminalStore.Get(terminalID)
	if !ok || !api.canAccessTerminalSession(r, snapshot.SessionID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	if !api.terminalStore.Dismiss(terminalID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"dismissed": true})
}

// handleCompleteTerminal marks one terminal snapshot complete and, when the
// backing provider wait loop is still active, asks it to return through the
// normal execution path. The workflow controller then runs validation,
// learning/KB hooks, progress updates, and auto-notification itself.
// POST /api/terminals/{terminal_id}/complete
func (api *StreamingAPI) handleCompleteTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	before, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}
	snapshot, ok := api.terminalStore.MarkCompleted(before.TerminalID)
	if !ok {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	if before.Active && strings.TrimSpace(before.TmuxSession) != "" {
		forceCompleteCodingAgentTmuxSession(before.TmuxSession)
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(snapshot)})
}

// handleFailTerminal marks one view-only terminal snapshot failed.
// POST /api/terminals/{terminal_id}/fail
func (api *StreamingAPI) handleFailTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	before, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}
	snapshot, ok := api.terminalStore.MarkFailed(before.TerminalID)
	if !ok {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(snapshot)})
}

// handleRefreshTerminal captures the current tmux pane and updates the snapshot.
// POST /api/terminals/{terminal_id}/refresh
func (api *StreamingAPI) handleRefreshTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		http.Error(w, "Terminal has no tmux session", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	content, err := captureTerminalPane(ctx, snapshot.TmuxSession)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	updated, ok := api.terminalStore.RefreshContent(snapshot.TerminalID, content)
	if !ok {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(updated)})
}

// handleKillTerminal kills the backing tmux session and marks the UI snapshot failed.
// POST /api/terminals/{terminal_id}/kill
func (api *StreamingAPI) handleKillTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		http.Error(w, "Terminal has no tmux session", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	if err := runTerminalTmuxCommand(ctx, "", "kill-session", "-t", snapshot.TmuxSession); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	updated, ok := api.terminalStore.MarkFailed(snapshot.TerminalID)
	if !ok {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(updated)})
}

// handleSendTerminalInput pastes text into the terminal's tmux pane.
// POST /api/terminals/{terminal_id}/input
func (api *StreamingAPI) handleSendTerminalInput(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}

	var req sendTerminalInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid terminal input request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Text) == "" && !req.Submit {
		http.Error(w, "Text or submit=true is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		http.Error(w, "Terminal has no tmux session", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	if req.Text != "" {
		if err := pasteTerminalText(ctx, snapshot.TmuxSession, req.Text); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	if req.Submit {
		if err := sendTerminalKey(ctx, snapshot.TmuxSession, "enter"); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(snapshot)})
}

// handleSendTerminalKey sends a small allowlisted key to the terminal's tmux pane.
// POST /api/terminals/{terminal_id}/key
func (api *StreamingAPI) handleSendTerminalKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	snapshot, ok := api.requireAccessibleTerminal(w, r)
	if !ok {
		return
	}

	var req sendTerminalKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid terminal key request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(snapshot.TmuxSession) == "" {
		http.Error(w, "Terminal has no tmux session", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTmuxActionTimeout)
	defer cancel()
	if err := sendTerminalKey(ctx, snapshot.TmuxSession, req.Key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(terminalActionResponse{OK: true, Terminal: api.enrichTerminalSnapshot(snapshot)})
}

func (api *StreamingAPI) requireAccessibleTerminal(w http.ResponseWriter, r *http.Request) (terminals.Snapshot, bool) {
	if api.terminalStore == nil {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return terminals.Snapshot{}, false
	}
	terminalID := strings.TrimSpace(mux.Vars(r)["terminal_id"])
	if terminalID == "" {
		http.Error(w, "Terminal ID is required", http.StatusBadRequest)
		return terminals.Snapshot{}, false
	}
	snapshot, ok := api.terminalStore.Get(terminalID)
	if !ok || !api.canAccessTerminalSession(r, snapshot.SessionID) {
		http.Error(w, "Terminal not found", http.StatusNotFound)
		return terminals.Snapshot{}, false
	}
	return snapshot, true
}

func pasteTerminalText(ctx context.Context, tmuxSession, text string) error {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return fmt.Errorf("tmux session is required")
	}
	bufferName := fmt.Sprintf("mcp-terminal-ui-%d", time.Now().UnixNano())
	if err := runTerminalTmuxCommand(ctx, text, "load-buffer", "-b", bufferName, "-"); err != nil {
		return fmt.Errorf("failed to load terminal input into tmux buffer: %w", err)
	}
	if err := runTerminalTmuxCommand(ctx, "", "paste-buffer", "-d", "-p", "-r", "-b", bufferName, "-t", tmuxSession); err != nil {
		return fmt.Errorf("failed to paste terminal input into tmux session: %w", err)
	}
	return nil
}

func captureTerminalPane(ctx context.Context, tmuxSession string) (string, error) {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return "", fmt.Errorf("tmux session is required")
	}
	content, err := runTerminalTmuxOutputCommand(ctx, "capture-pane", "-p", "-t", tmuxSession, "-S", "-2000")
	if err != nil {
		return "", fmt.Errorf("failed to capture terminal pane: %w", err)
	}
	return content, nil
}

func sendTerminalKey(ctx context.Context, tmuxSession, key string) error {
	tmuxSession = strings.TrimSpace(tmuxSession)
	if tmuxSession == "" {
		return fmt.Errorf("tmux session is required")
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter", "return":
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "C-m")
	case "esc", "escape":
		return runTerminalTmuxCommand(ctx, "", "send-keys", "-t", tmuxSession, "Escape")
	default:
		return fmt.Errorf("unsupported terminal key %q", key)
	}
}

func (api *StreamingAPI) enrichTerminalSnapshot(snapshot terminals.Snapshot) terminals.Snapshot {
	active, exists := api.getActiveSession(snapshot.SessionID)
	if !exists || active == nil {
		return snapshot.WithContext(terminals.Context{})
	}
	enriched := api.buildActiveSessionInfoSummary(active)
	if enriched == nil {
		return snapshot.WithContext(terminals.Context{})
	}
	return snapshot.WithContext(terminals.Context{
		WorkflowName:  enriched.WorkflowName,
		WorkflowLabel: enriched.WorkflowLabel,
		WorkspacePath: enriched.WorkspacePath,
		ExecutionName: enriched.CurrentExecutionName,
	})
}

func (api *StreamingAPI) canAccessTerminalSession(r *http.Request, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}

	currentUserID := GetUserIDFromContext(r.Context())
	activeSession, exists := api.getActiveSession(sessionID)
	if exists {
		return activeSession.UserID == "" || activeSession.UserID == currentUserID
	}
	if api.eventStore == nil {
		return currentUserID == GetDefaultUserID()
	}
	owner := api.eventStore.GetSessionOwner(sessionID)
	if owner != "" {
		return owner == currentUserID
	}
	return currentUserID == GetDefaultUserID()
}

func compactTerminalSnapshotForList(snapshot terminals.Snapshot, contentMode string) terminals.Snapshot {
	switch contentMode {
	case "none", "metadata":
		snapshot.Content = ""
	case "full":
		return snapshot
	default:
		snapshot.Content = terminalContentTail(snapshot.Content, listTerminalContentMaxBytes)
	}
	return snapshot
}

func terminalContentTail(content string, maxBytes int) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}

	start := len(content) - maxBytes
	for start < len(content) && !utf8.RuneStart(content[start]) {
		start++
	}

	tail := content[start:]
	if newline := strings.IndexByte(tail, '\n'); newline >= 0 && newline < 4096 {
		tail = tail[newline+1:]
	}
	return "[terminal output truncated; showing latest output]\n" + tail
}
