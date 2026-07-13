package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"

	"github.com/gorilla/mux"
)

type SessionActivityTreeEvent struct {
	ID                string                 `json:"id"`
	Type              string                 `json:"type"`
	Timestamp         time.Time              `json:"timestamp"`
	ExecutionID       string                 `json:"execution_id,omitempty"`
	ParentExecutionID string                 `json:"parent_execution_id,omitempty"`
	ExecutionKind     string                 `json:"execution_kind,omitempty"`
	Title             string                 `json:"title,omitempty"`
	Summary           string                 `json:"summary,omitempty"`
	Payload           map[string]interface{} `json:"payload,omitempty"`
}

type SessionActivityTreeNode struct {
	ExecutionID       string                     `json:"execution_id"`
	ParentExecutionID string                     `json:"parent_execution_id,omitempty"`
	SessionID         string                     `json:"session_id"`
	Source            string                     `json:"source,omitempty"`
	Kind              string                     `json:"kind"`
	Name              string                     `json:"name"`
	Status            string                     `json:"status"`
	StartedAt         time.Time                  `json:"started_at"`
	CompletedAt       *time.Time                 `json:"completed_at,omitempty"`
	Error             string                     `json:"error,omitempty"`
	Metadata          map[string]string          `json:"metadata,omitempty"`
	Events            []SessionActivityTreeEvent `json:"events,omitempty"`
	ToolEventCount    int                        `json:"tool_event_count,omitempty"`
	HiddenEventCount  int                        `json:"hidden_event_count,omitempty"`
	Children          []*SessionActivityTreeNode `json:"children,omitempty"`
}

type SessionActivityTreeResponse struct {
	SessionID string                      `json:"session_id"`
	Root      *SessionActivityTreeNode    `json:"root"`
	Summary   SessionExecutionTreeSummary `json:"summary"`
}

func cloneActivityNodeFromExecution(node *SessionExecutionTreeNode, byID map[string]*SessionActivityTreeNode) *SessionActivityTreeNode {
	if node == nil {
		return nil
	}
	out := &SessionActivityTreeNode{
		ExecutionID:       node.ExecutionID,
		ParentExecutionID: node.ParentExecutionID,
		SessionID:         node.SessionID,
		Source:            node.Source,
		Kind:              node.Kind,
		Name:              node.Name,
		Status:            node.Status,
		StartedAt:         node.StartedAt,
		CompletedAt:       node.CompletedAt,
		Error:             node.Error,
		Metadata:          cloneSessionExecutionMetadata(node.Metadata),
	}
	byID[out.ExecutionID] = out
	for _, child := range node.Children {
		if cloned := cloneActivityNodeFromExecution(child, byID); cloned != nil {
			out.Children = append(out.Children, cloned)
		}
	}
	return out
}

func isActivityVisibleEvent(eventType string) bool {
	if strings.TrimSpace(eventType) == "" {
		return false
	}
	return internalevents.STRUCTURAL_EVENTS[eventType]
}

func isActivityToolDetailEvent(eventType string) bool {
	switch eventType {
	case "tool_call_start", "tool_call_end", "tool_call_error", "tool_call":
		return true
	default:
		return false
	}
}

func activityEventTitle(eventType string, payload map[string]interface{}) string {
	switch eventType {
	case "todo_task_route_selected":
		return firstSessionExecutionString(
			stringValue(payload["selected_route_name"]),
			stringValue(payload["selected_route_id"]),
			"Route selected",
		)
	case "pre_validation_completed":
		return firstSessionExecutionString(stringValue(payload["step_title"]), "Pre-validation completed")
	case "routing_evaluated":
		return firstSessionExecutionString(stringValue(payload["step_title"]), "Routing evaluated")
	case "todo_steps_extracted":
		return "Todo steps extracted"
	case "todo_task_item_created", "todo_task_item_updated", "todo_task_item_completed":
		return firstSessionExecutionString(stringValue(payload["title"]), stringValue(payload["todo_title"]), eventType)
	case "workflow_start":
		return "Workflow started"
	case "workflow_end":
		return "Workflow completed"
	case "workflow_error":
		return "Workflow failed"
	case "batch_execution_start":
		return "Batch started"
	case "batch_execution_end":
		return "Batch completed"
	case "batch_group_start":
		return firstSessionExecutionString(stringValue(payload["group_name"]), "Group started")
	case "batch_group_end":
		return firstSessionExecutionString(stringValue(payload["group_name"]), "Group completed")
	default:
		return eventType
	}
}

func activityEventSummary(eventType string, payload map[string]interface{}, errText string) string {
	if errText != "" {
		return errText
	}
	switch eventType {
	case "todo_task_route_selected":
		return firstSessionExecutionString(
			stringValue(payload["todo_title"]),
			stringValue(payload["todo_id_to_execute"]),
			stringValue(payload["next_action"]),
		)
	case "pre_validation_completed":
		return firstSessionExecutionString(stringValue(payload["status"]), stringValue(payload["summary"]))
	case "routing_evaluated":
		return firstSessionExecutionString(stringValue(payload["selected_route_name"]), stringValue(payload["selected_route_id"]))
	default:
		return firstSessionExecutionString(
			stringValue(payload["summary"]),
			stringValue(payload["message"]),
			stringValue(payload["content"]),
		)
	}
}

func toSessionActivityTreeEvent(event internalevents.Event, payload map[string]interface{}) SessionActivityTreeEvent {
	return SessionActivityTreeEvent{
		ID:                event.ID,
		Type:              event.Type,
		Timestamp:         event.Timestamp,
		ExecutionID:       event.ExecutionID,
		ParentExecutionID: event.ParentExecutionID,
		ExecutionKind:     event.ExecutionKind,
		Title:             activityEventTitle(event.Type, payload),
		Summary:           activityEventSummary(event.Type, payload, event.Error),
		Payload:           payload,
	}
}

func activityOwnerIDForEvent(event internalevents.Event, nodes map[string]*SessionActivityTreeNode, rootID string) string {
	if executionID := strings.TrimSpace(event.ExecutionID); executionID != "" {
		if _, ok := nodes[executionID]; ok {
			return executionID
		}
	}
	if parentExecutionID := strings.TrimSpace(event.ParentExecutionID); parentExecutionID != "" {
		if _, ok := nodes[parentExecutionID]; ok {
			return parentExecutionID
		}
	}
	return rootID
}

func (api *StreamingAPI) buildSessionActivityTree(session *ActiveSessionInfo) *SessionActivityTreeResponse {
	executionTree := api.buildSessionExecutionTree(session)
	if executionTree == nil || executionTree.Root == nil {
		return nil
	}

	nodes := map[string]*SessionActivityTreeNode{}
	root := cloneActivityNodeFromExecution(executionTree.Root, nodes)
	if root == nil {
		return nil
	}

	if api == nil || api.eventStore == nil {
		return &SessionActivityTreeResponse{
			SessionID: executionTree.SessionID,
			Root:      root,
			Summary:   executionTree.Summary,
		}
	}

	rootID := root.ExecutionID
	for _, event := range api.eventStore.GetAllEventsRaw(session.SessionID) {
		if event.Type == "streaming_start" || event.Type == "streaming_chunk" || event.Type == "streaming_end" {
			continue
		}
		ownerID := activityOwnerIDForEvent(event, nodes, rootID)
		owner := nodes[ownerID]
		if owner == nil {
			owner = root
		}

		if isActivityToolDetailEvent(event.Type) {
			owner.ToolEventCount++
			continue
		}
		if !isActivityVisibleEvent(event.Type) {
			owner.HiddenEventCount++
			continue
		}

		owner.Events = append(owner.Events, toSessionActivityTreeEvent(event, eventPayloadMap(event)))
	}

	return &SessionActivityTreeResponse{
		SessionID: executionTree.SessionID,
		Root:      root,
		Summary:   executionTree.Summary,
	}
}

// handleGetSessionActivityTree returns a backend-owned activity tree for one session.
// GET /api/sessions/{session_id}/activity-tree
func (api *StreamingAPI) handleGetSessionActivityTree(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	sessionID := mux.Vars(r)["session_id"]
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	currentUserID := GetUserIDFromContext(r.Context())
	activeSession, exists := api.getActiveSession(sessionID)
	var sessionCopy ActiveSessionInfo
	if exists {
		if activeSession.UserID != "" && activeSession.UserID != currentUserID {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		sessionCopy = *activeSession
	} else {
		if api.eventStore == nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		if owner := api.eventStore.GetSessionOwner(sessionID); owner != "" && owner != currentUserID {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		} else if owner == "" && currentUserID != GetDefaultUserID() {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		sessionFromEvents := synthesizeSessionInfoFromEvents(sessionID, api.eventStore.GetAllEventsRaw(sessionID))
		if sessionFromEvents == nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		sessionCopy = *sessionFromEvents
	}

	tree := api.buildSessionActivityTree(&sessionCopy)
	if tree == nil {
		http.Error(w, "Activity tree not available", http.StatusNotFound)
		return
	}

	if err := json.NewEncoder(w).Encode(tree); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}
