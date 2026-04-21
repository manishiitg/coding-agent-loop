package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

const (
	sessionExecutionRootKind          = "session"
	sessionExecutionMainAgentKind     = "main_agent"
	sessionExecutionSyntheticTurnKind = "synthetic_turn"

	sessionExecutionDisplayBusy    = "busy"
	sessionExecutionDisplayIdle    = "idle"
	sessionExecutionDisplayStopped = "stopped"
)

type SessionExecutionTreeNode struct {
	ExecutionID       string                      `json:"execution_id"`
	ParentExecutionID string                      `json:"parent_execution_id,omitempty"`
	SessionID         string                      `json:"session_id"`
	Source            string                      `json:"source,omitempty"`
	Kind              string                      `json:"kind"`
	Name              string                      `json:"name"`
	Status            string                      `json:"status"`
	StartedAt         time.Time                   `json:"started_at"`
	CompletedAt       *time.Time                  `json:"completed_at,omitempty"`
	Error             string                      `json:"error,omitempty"`
	Metadata          map[string]string           `json:"metadata,omitempty"`
	Children          []*SessionExecutionTreeNode `json:"children,omitempty"`
}

type SessionExecutionTreeSummary struct {
	SessionID                   string `json:"session_id"`
	SessionStatus               string `json:"session_status"`
	DisplayStatus               string `json:"display_status"`
	IsSessionBusy               bool   `json:"is_session_busy"`
	RunningCount                int    `json:"running_count"`
	CompletedCount              int    `json:"completed_count"`
	FailedCount                 int    `json:"failed_count"`
	CanceledCount               int    `json:"canceled_count"`
	HasRunningMainAgent         bool   `json:"has_running_main_agent"`
	HasRunningBackgroundAgents  bool   `json:"has_running_background_agents"`
	HasRunningTrackedExecutions bool   `json:"has_running_tracked_executions"`
}

type SessionExecutionTreeResponse struct {
	SessionID string                      `json:"session_id"`
	Root      *SessionExecutionTreeNode   `json:"root"`
	Summary   SessionExecutionTreeSummary `json:"summary"`
}

func cloneSessionExecutionMetadata(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}

func cloneTrackedExecution(exec *TrackedWorkflowExecution) *TrackedWorkflowExecution {
	if exec == nil {
		return nil
	}
	copied := *exec
	copied.Metadata = cloneTrackedMetadata(exec.Metadata)
	return &copied
}

func (api *StreamingAPI) trackedExecutionsForSession(sessionID string) []*TrackedWorkflowExecution {
	api.trackedWorkflowExecutionsMux.RLock()
	defer api.trackedWorkflowExecutionsMux.RUnlock()

	list := make([]*TrackedWorkflowExecution, 0, len(api.trackedWorkflowExecutions))
	for _, exec := range api.trackedWorkflowExecutions {
		if exec == nil || exec.SessionID != sessionID {
			continue
		}
		list = append(list, cloneTrackedExecution(exec))
	}
	return list
}

func sessionExecutionSortLess(a, b *SessionExecutionTreeNode) bool {
	if a == nil || b == nil {
		return a != nil
	}
	if a.Status == trackedExecutionStatusRunning && b.Status != trackedExecutionStatusRunning {
		return true
	}
	if a.Status != trackedExecutionStatusRunning && b.Status == trackedExecutionStatusRunning {
		return false
	}
	if !a.StartedAt.Equal(b.StartedAt) {
		return a.StartedAt.Before(b.StartedAt)
	}
	return a.ExecutionID < b.ExecutionID
}

func sortSessionExecutionTree(node *SessionExecutionTreeNode) {
	if node == nil || len(node.Children) == 0 {
		return
	}
	sort.Slice(node.Children, func(i, j int) bool {
		return sessionExecutionSortLess(node.Children[i], node.Children[j])
	})
	for _, child := range node.Children {
		sortSessionExecutionTree(child)
	}
}

func (api *StreamingAPI) buildSessionExecutionTree(session *ActiveSessionInfo) *SessionExecutionTreeResponse {
	if api == nil || session == nil || strings.TrimSpace(session.SessionID) == "" {
		return nil
	}

	rootID := "session:" + session.SessionID
	root := &SessionExecutionTreeNode{
		ExecutionID: rootID,
		SessionID:   session.SessionID,
		Source:      "session",
		Kind:        sessionExecutionRootKind,
		Name:        strings.TrimSpace(session.Query),
		Status:      session.Status,
		StartedAt:   session.CreatedAt,
		Metadata:    map[string]string{"agent_mode": session.AgentMode},
	}
	if root.Name == "" {
		root.Name = "Session"
	}

	nodes := map[string]*SessionExecutionTreeNode{
		rootID: root,
	}

	sessionBusy := api.isSessionBusy(session.SessionID)
	mainAgentRunning := api.canSteerSession(session.SessionID) || api.isSyntheticTurn(session.SessionID) || sessionBusy
	if mainAgentRunning || session.Status == "running" {
		kind := sessionExecutionMainAgentKind
		name := "Main Agent"
		if api.isSyntheticTurn(session.SessionID) || session.IsSyntheticTurn {
			kind = sessionExecutionSyntheticTurnKind
			name = "Main Agent (Auto-notification)"
		}
		nodes["main:"+session.SessionID] = &SessionExecutionTreeNode{
			ExecutionID:       "main:" + session.SessionID,
			ParentExecutionID: rootID,
			SessionID:         session.SessionID,
			Source:            "session",
			Kind:              kind,
			Name:              name,
			Status:            trackedExecutionStatusRunning,
			StartedAt:         session.LastActivity,
			Metadata:          map[string]string{"agent_mode": session.AgentMode},
		}
	}

	for _, exec := range api.trackedExecutionsForSession(session.SessionID) {
		if exec == nil {
			continue
		}
		parentID := rootID
		if exec.Metadata != nil {
			if candidate := strings.TrimSpace(exec.Metadata["parent_execution_id"]); candidate != "" {
				parentID = candidate
			}
		}
		name := strings.TrimSpace(exec.Title)
		if name == "" {
			name = strings.TrimSpace(exec.Name)
		}
		if name == "" {
			name = "Workflow Execution"
		}
		nodes[exec.ExecutionID] = &SessionExecutionTreeNode{
			ExecutionID:       exec.ExecutionID,
			ParentExecutionID: parentID,
			SessionID:         exec.SessionID,
			Source:            exec.Source,
			Kind:              exec.Kind,
			Name:              name,
			Status:            exec.Status,
			StartedAt:         exec.StartedAt,
			CompletedAt:       exec.CompletedAt,
			Error:             exec.LastError,
			Metadata:          cloneSessionExecutionMetadata(exec.Metadata),
		}
	}

	for _, agent := range api.bgAgentRegistry.GetAll(session.SessionID) {
		if agent == nil {
			continue
		}
		snap := agent.GetSnapshot()
		parentID := rootID
		if candidate := strings.TrimSpace(snap.ParentExecutionID); candidate != "" {
			parentID = candidate
		}
		kind := normalizeTrackedExecutionKind(snap.Kind)
		if kind == "" && snap.Metadata != nil {
			kind = normalizeTrackedExecutionKind(snap.Metadata["type"])
		}
		if kind == "" {
			kind = "background_agent"
		}
		name := strings.TrimSpace(snap.Name)
		if name == "" {
			name = "Background Agent"
		}
		nodes[snap.ID] = &SessionExecutionTreeNode{
			ExecutionID:       snap.ID,
			ParentExecutionID: parentID,
			SessionID:         snap.SessionID,
			Source:            "background_agent_registry",
			Kind:              kind,
			Name:              name,
			Status:            string(snap.Status),
			StartedAt:         snap.CreatedAt,
			CompletedAt:       snap.CompletedAt,
			Error:             snap.Error,
			Metadata:          cloneSessionExecutionMetadata(snap.Metadata),
		}
	}

	summary := SessionExecutionTreeSummary{
		SessionID:     session.SessionID,
		SessionStatus: session.Status,
		IsSessionBusy: sessionBusy,
		DisplayStatus: sessionExecutionDisplayIdle,
	}

	for nodeID, node := range nodes {
		if node == nil || nodeID == rootID {
			continue
		}
		parent := nodes[node.ParentExecutionID]
		if parent == nil {
			parent = root
			node.ParentExecutionID = rootID
		}
		parent.Children = append(parent.Children, node)

		switch node.Status {
		case trackedExecutionStatusRunning:
			summary.RunningCount++
		case trackedExecutionStatusCompleted:
			summary.CompletedCount++
		case trackedExecutionStatusFailed:
			summary.FailedCount++
		case trackedExecutionStatusCanceled:
			summary.CanceledCount++
		}

		if node.Status == trackedExecutionStatusRunning {
			switch node.Source {
			case "session":
				summary.HasRunningMainAgent = true
			case "background_agent_registry":
				summary.HasRunningBackgroundAgents = true
			default:
				summary.HasRunningTrackedExecutions = true
			}
		}
	}

	switch {
	case summary.HasRunningMainAgent || summary.HasRunningBackgroundAgents || summary.HasRunningTrackedExecutions || summary.IsSessionBusy:
		summary.DisplayStatus = sessionExecutionDisplayBusy
	case summary.CompletedCount > 0 || summary.FailedCount > 0 || summary.CanceledCount > 0 ||
		session.Status == "completed" || session.Status == "stopped" || session.Status == "inactive" || session.Status == "dismissed":
		summary.DisplayStatus = sessionExecutionDisplayStopped
	default:
		summary.DisplayStatus = sessionExecutionDisplayIdle
	}

	sortSessionExecutionTree(root)

	return &SessionExecutionTreeResponse{
		SessionID: session.SessionID,
		Root:      root,
		Summary:   summary,
	}
}

// handleGetSessionExecutionTree returns a backend-owned execution tree for one session.
// GET /api/sessions/{session_id}/execution-tree
func (api *StreamingAPI) handleGetSessionExecutionTree(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sessionID := mux.Vars(r)["session_id"]
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	currentUserID := GetUserIDFromContext(r.Context())
	activeSession, exists := api.getActiveSession(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	if activeSession.UserID != "" && activeSession.UserID != currentUserID {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	sessionCopy := *activeSession
	tree := api.buildSessionExecutionTree(&sessionCopy)
	if tree == nil {
		http.Error(w, "Execution tree not available", http.StatusNotFound)
		return
	}

	if err := json.NewEncoder(w).Encode(tree); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}
