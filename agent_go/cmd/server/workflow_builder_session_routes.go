package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	agentEvents "github.com/manishiitg/mcpagent/events"

	storeEvents "mcp-agent-builder-go/agent_go/internal/events"
)

const restoredBuilderConversationMessageLimit = 50

type workflowBuilderSessionResponse struct {
	Success            bool              `json:"success"`
	Source             string            `json:"source"`
	SessionID          string            `json:"session_id,omitempty"`
	PhaseID            string            `json:"phase_id,omitempty"`
	Status             string            `json:"status"`
	DisplayStatus      string            `json:"display_status,omitempty"`
	PresetQueryID      string            `json:"preset_query_id,omitempty"`
	WorkspacePath      string            `json:"workspace_path,omitempty"`
	WorkflowName       string            `json:"workflow_name,omitempty"`
	UpdatedAt          string            `json:"updated_at,omitempty"`
	ConversationPath   string            `json:"conversation_path,omitempty"`
	Events             []json.RawMessage `json:"events"`
	Total              int               `json:"total"`
	LastProcessedIndex int               `json:"last_processed_index"`
}

type builderConversationLog struct {
	SessionID           string                       `json:"session_id"`
	PhaseID             string                       `json:"phase_id"`
	UpdatedAt           string                       `json:"updated_at"`
	ConversationHistory []builderConversationMessage `json:"conversation_history"`
}

type builderConversationMessage struct {
	Role  string                    `json:"Role"`
	Parts []builderConversationPart `json:"Parts"`
}

type builderConversationPart struct {
	Text string `json:"Text"`
}

// handleGetWorkflowBuilderSession is the backend source of truth for restoring
// the Workflow Builder chat. It prefers a live EventStore session and only falls
// back to durable builder conversation files when no live session matches.
func (api *StreamingAPI) handleGetWorkflowBuilderSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	presetQueryID := strings.TrimSpace(r.URL.Query().Get("preset_query_id"))
	workspacePath := strings.Trim(strings.TrimSpace(r.URL.Query().Get("workspace_path")), "/")
	if workspacePath == "" && presetQueryID != "" {
		if resolved, err := api.resolveWorkspacePathFromPreset(r.Context(), presetQueryID); err == nil {
			workspacePath = strings.Trim(strings.TrimSpace(resolved), "/")
		}
	}

	if presetQueryID == "" && workspacePath == "" {
		http.Error(w, "preset_query_id or workspace_path is required", http.StatusBadRequest)
		return
	}

	if live := api.findLiveWorkflowBuilderSession(r.Context(), presetQueryID, workspacePath); live != nil {
		_ = json.NewEncoder(w).Encode(live)
		return
	}

	restored, err := api.restoreLatestBuilderConversation(r.Context(), presetQueryID, workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to restore builder conversation: %v", err), http.StatusInternalServerError)
		return
	}
	if restored != nil {
		_ = json.NewEncoder(w).Encode(restored)
		return
	}

	_ = json.NewEncoder(w).Encode(workflowBuilderSessionResponse{
		Success:            true,
		Source:             "none",
		Status:             "idle",
		PresetQueryID:      presetQueryID,
		WorkspacePath:      workspacePath,
		WorkflowName:       workflowNameFromWorkspacePath(workspacePath),
		Events:             []json.RawMessage{},
		LastProcessedIndex: -1,
	})
}

func (api *StreamingAPI) findLiveWorkflowBuilderSession(ctx context.Context, presetQueryID, workspacePath string) *workflowBuilderSessionResponse {
	currentUserID := GetUserIDFromContext(ctx)

	var chosen *ActiveSessionInfo
	api.activeSessionsMux.RLock()
	for _, session := range api.activeSessions {
		if session == nil || session.AgentMode != "workflow_phase" {
			continue
		}
		if session.UserID != "" && session.UserID != currentUserID {
			continue
		}
		if !workflowBuilderSessionMatches(session, presetQueryID, workspacePath) {
			continue
		}
		if chosen == nil || session.LastActivity.After(chosen.LastActivity) {
			copySession := *session
			chosen = &copySession
		}
	}
	api.activeSessionsMux.RUnlock()

	if chosen == nil {
		return nil
	}

	enriched := api.buildActiveSessionInfoSummary(chosen)
	if enriched != nil {
		chosen = enriched
	}

	rawEvents := []json.RawMessage{}
	lastProcessedIndex := -1
	if api.eventStore != nil {
		result := api.eventStore.GetEvents(chosen.SessionID, storeEvents.GetEventsOptions{SinceIndex: -1})
		rawEvents = marshalBuilderEvents(result.Events)
		lastProcessedIndex = result.LastProcessedIndex
	}
	if lastProcessedIndex < 0 && len(rawEvents) > 0 {
		lastProcessedIndex = len(rawEvents) - 1
	}

	return &workflowBuilderSessionResponse{
		Success:            true,
		Source:             "live",
		SessionID:          chosen.SessionID,
		PhaseID:            "workflow-builder",
		Status:             coalesceString(chosen.Status, "running"),
		DisplayStatus:      "busy",
		PresetQueryID:      coalesceString(chosen.PresetQueryID, presetQueryID),
		WorkspacePath:      coalesceString(chosen.WorkspacePath, workspacePath),
		WorkflowName:       coalesceString(chosen.WorkflowName, chosen.WorkflowLabel, chosen.PresetName, workflowNameFromWorkspacePath(coalesceString(chosen.WorkspacePath, workspacePath))),
		UpdatedAt:          chosen.LastActivity.Format(time.RFC3339Nano),
		Events:             rawEvents,
		Total:              len(rawEvents),
		LastProcessedIndex: lastProcessedIndex,
	}
}

func workflowBuilderSessionMatches(session *ActiveSessionInfo, presetQueryID, workspacePath string) bool {
	if presetQueryID != "" && strings.TrimSpace(session.PresetQueryID) == presetQueryID {
		return true
	}
	if workspacePath != "" && strings.Trim(strings.TrimSpace(session.WorkspacePath), "/") == workspacePath {
		return true
	}
	return false
}

func (api *StreamingAPI) restoreLatestBuilderConversation(ctx context.Context, presetQueryID, workspacePath string) (*workflowBuilderSessionResponse, error) {
	workspacePath = strings.Trim(strings.TrimSpace(workspacePath), "/")
	if workspacePath == "" {
		return nil, nil
	}

	builderFolder := workspacePath + "/builder"
	listing, exists, err := listWorkspaceFolder(ctx, builderFolder, 1)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	paths := []string{}
	collectWorkspaceFilePaths(listing, &paths)

	type candidate struct {
		path      string
		log       builderConversationLog
		updatedAt time.Time
	}
	candidates := []candidate{}
	for _, path := range paths {
		if !strings.HasPrefix(path, builderFolder+"/session-") || !strings.HasSuffix(path, "-conversation.json") {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, path)
		if err != nil {
			return nil, err
		}
		if !exists || strings.TrimSpace(content) == "" {
			continue
		}

		var log builderConversationLog
		if err := json.Unmarshal([]byte(content), &log); err != nil {
			continue
		}
		updatedAt := parseBuilderConversationUpdatedAt(log.UpdatedAt)
		candidates = append(candidates, candidate{path: path, log: log, updatedAt: updatedAt})
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].updatedAt.Equal(candidates[j].updatedAt) {
			return candidates[i].updatedAt.After(candidates[j].updatedAt)
		}
		return candidates[i].path > candidates[j].path
	})

	latest := candidates[0]
	rawEvents := builderConversationToRawEvents(latest.log)
	if len(rawEvents) == 0 {
		return nil, nil
	}

	sessionID := coalesceString(latest.log.SessionID, latest.path)
	updatedAt := latest.log.UpdatedAt
	if updatedAt == "" && !latest.updatedAt.IsZero() {
		updatedAt = latest.updatedAt.Format(time.RFC3339Nano)
	}

	return &workflowBuilderSessionResponse{
		Success:            true,
		Source:             "workspace",
		SessionID:          sessionID,
		PhaseID:            coalesceString(latest.log.PhaseID, "workflow-builder"),
		Status:             "completed",
		DisplayStatus:      "stopped",
		PresetQueryID:      presetQueryID,
		WorkspacePath:      workspacePath,
		WorkflowName:       workflowNameFromWorkspacePath(workspacePath),
		UpdatedAt:          updatedAt,
		ConversationPath:   latest.path,
		Events:             rawEvents,
		Total:              len(rawEvents),
		LastProcessedIndex: len(rawEvents) - 1,
	}, nil
}

func parseBuilderConversationUpdatedAt(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed
	}
	return time.Time{}
}

func builderConversationToRawEvents(log builderConversationLog) []json.RawMessage {
	sessionID := coalesceString(log.SessionID, fmt.Sprintf("builder-history-%d", time.Now().UnixNano()))
	timestamp := parseBuilderConversationUpdatedAt(log.UpdatedAt)
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	type displayMessage struct {
		originalIndex int
		role          string
		text          string
	}

	displayMessages := []displayMessage{}
	for index, message := range log.ConversationHistory {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		text := builderConversationMessageText(message)
		if text == "" || role == "system" {
			continue
		}
		if role != "human" && role != "user" && role != "ai" && role != "assistant" {
			continue
		}
		displayMessages = append(displayMessages, displayMessage{
			originalIndex: index,
			role:          role,
			text:          text,
		})
	}
	if len(displayMessages) > restoredBuilderConversationMessageLimit {
		displayMessages = displayMessages[len(displayMessages)-restoredBuilderConversationMessageLimit:]
	}

	rawEvents := []json.RawMessage{}
	for _, display := range displayMessages {
		eventType := ""
		payloadKey := ""
		switch display.role {
		case "human", "user":
			eventType = "user_message"
			payloadKey = "content"
		case "ai", "assistant":
			eventType = "unified_completion"
			payloadKey = "final_result"
		}

		data := map[string]interface{}{
			payloadKey:  display.text,
			"timestamp": timestamp.Format(time.RFC3339Nano),
		}
		if eventType == "unified_completion" {
			data["status"] = "completed"
		}

		raw, err := json.Marshal(map[string]interface{}{
			"id":          fmt.Sprintf("builder-history-%s-%d", sessionID, display.originalIndex),
			"type":        eventType,
			"timestamp":   timestamp,
			"session_id":  sessionID,
			"event_index": display.originalIndex,
			"data": map[string]interface{}{
				"type":      eventType,
				"timestamp": timestamp,
				"data":      data,
			},
		})
		if err == nil {
			rawEvents = append(rawEvents, raw)
		}
	}

	return rawEvents
}

func builderConversationMessageText(message builderConversationMessage) string {
	parts := []string{}
	for _, part := range message.Parts {
		text := strings.TrimSpace(part.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func marshalBuilderEvents(events []storeEvents.Event) []json.RawMessage {
	rawEvents := make([]json.RawMessage, 0, len(events))
	for _, event := range events {
		if event.Data != nil && event.Data.Type == "" {
			event.Data.Type = agentEvents.EventType(event.Type)
		}
		raw, err := json.Marshal(event)
		if err == nil {
			rawEvents = append(rawEvents, raw)
		}
	}
	return rawEvents
}

func coalesceString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
