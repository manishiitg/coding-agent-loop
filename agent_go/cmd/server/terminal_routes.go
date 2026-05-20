package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

const listTerminalContentMaxBytes = 64 * 1024

type listTerminalsResponse struct {
	Terminals []terminals.Snapshot `json:"terminals"`
	Total     int                  `json:"total"`
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
