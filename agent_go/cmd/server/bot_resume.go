package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

func (api *StreamingAPI) resolveBotResumeTarget(ctx context.Context, userID, selector string, filter services.BotResumeFilter) (*services.BotResumeTarget, error) {
	selector = strings.ToLower(strings.TrimSpace(selector))
	if selector == "" {
		selector = "latest"
	}

	targets, err := api.listBotResumeTargets(ctx, userID, filter)
	if err != nil {
		return nil, err
	}

	if selector == "latest" {
		if len(targets) == 0 {
			return nil, nil
		}
		return &targets[0], nil
	}
	if n, ok := parseBotResumeOrdinal(selector); ok {
		if n < 1 || n > len(targets) {
			return nil, nil
		}
		return &targets[n-1], nil
	}

	var matches []services.BotResumeTarget
	for _, target := range targets {
		sessionID := strings.ToLower(strings.TrimSpace(target.SessionID))
		if sessionID == selector || strings.HasPrefix(sessionID, selector) {
			matches = append(matches, target)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("session selector %q matches %d chats; use a longer session id", selector, len(matches))
	}
	return &matches[0], nil
}

func (api *StreamingAPI) listBotResumeTargets(ctx context.Context, userID string, filter services.BotResumeFilter) ([]services.BotResumeTarget, error) {
	if filter.ProfileID != "" && filter.WorkspaceUserID != "" {
		userID = filter.WorkspaceUserID
	}
	api.activeSessionsMux.RLock()
	candidates := make([]ActiveSessionInfo, 0, len(api.activeSessions))
	for _, session := range api.activeSessions {
		if session == nil {
			continue
		}
		if userID != "" && session.UserID != "" && session.UserID != userID {
			continue
		}
		candidates = append(candidates, *api.buildActiveSessionInfoSummary(session))
	}
	api.activeSessionsMux.RUnlock()

	filter.WorkspacePath = strings.TrimSpace(filter.WorkspacePath)
	filter.PresetQueryID = strings.TrimSpace(filter.PresetQueryID)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].LastActivity.After(candidates[j].LastActivity)
	})
	targets := make([]services.BotResumeTarget, 0, len(candidates))
	updated := map[string]time.Time{}
	for _, session := range candidates {
		if !botResumeCandidateStatus(session.Status) || !botResumeCandidateMode(session.AgentMode) {
			continue
		}
		if strings.TrimSpace(session.BotPlatform) != "" {
			continue
		}
		if filter.WorkspacePath != "" && !workspacePathsMatchForUser(userID, session.WorkspacePath, filter.WorkspacePath) {
			continue
		}
		if filter.PresetQueryID != "" && strings.TrimSpace(session.PresetQueryID) != filter.PresetQueryID {
			continue
		}
		if target := botResumeTargetFromActive(&session); target != nil {
			if filter.ProfileID != "" {
				target.ProfileRoute = &services.ProfileRoute{ProfileID: filter.ProfileID, ConversationKey: filter.ConversationKey, WorkspaceUserID: userID, UploadFolder: filter.WorkspacePath}
			}
			updated[target.SessionID] = session.LastActivity
			targets = append(targets, *target)
		}
	}
	if filter.WorkspacePath != "" {
		seen := map[string]bool{}
		for _, target := range targets {
			seen[target.SessionID] = true
		}
		for _, saved := range chatHistorySessionsByID(userID, filter.WorkspacePath) {
			if seen[saved.SessionID] || chatHistorySessionWorkspace(saved) != normalizeConversationWorkspace(filter.WorkspacePath) {
				continue
			}
			target := services.BotResumeTarget{SessionID: saved.SessionID, UserID: userID, AgentMode: saved.AgentMode, Status: "completed", Query: firstNonEmptyTrimmed(saved.Title, saved.Query), WorkspacePath: filter.WorkspacePath, PresetQueryID: filter.PresetQueryID, WorkshopMode: saved.WorkshopMode}
			if filter.ProfileID != "" {
				target.ProfileRoute = &services.ProfileRoute{ProfileID: filter.ProfileID, ConversationKey: filter.ConversationKey, WorkspaceUserID: userID, UploadFolder: filter.WorkspacePath}
			}
			updated[target.SessionID], _ = time.Parse(time.RFC3339Nano, saved.UpdatedAt)
			targets = append(targets, target)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if updated[targets[i].SessionID].Equal(updated[targets[j].SessionID]) {
			return targets[i].SessionID < targets[j].SessionID
		}
		return updated[targets[i].SessionID].After(updated[targets[j].SessionID])
	})
	return targets, nil
}

func botResumeCandidateMode(agentMode string) bool {
	switch strings.TrimSpace(agentMode) {
	case "multi-agent", "workflow_phase", "workflow", "":
		return true
	default:
		return false
	}
}

func botResumeCandidateStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "running", "paused", "completed", "inactive", "waiting_for_input", "awaiting_plan_approval":
		return true
	default:
		return false
	}
}

func botResumeTargetFromActive(session *ActiveSessionInfo) *services.BotResumeTarget {
	if session == nil {
		return nil
	}
	phaseID := strings.TrimSpace(session.PhaseID)
	if strings.TrimSpace(session.AgentMode) == "workflow_phase" && phaseID == "" {
		phaseID = workflowtypes.WorkflowStatusWorkflowBuilder
	}
	activity := strings.TrimSpace(session.CurrentExecutionName)
	if activity == "" && session.HasRunningBackgroundAgents {
		if session.RunningBackgroundAgentCount > 1 {
			activity = fmt.Sprintf("%d background agents running", session.RunningBackgroundAgentCount)
		} else {
			activity = "background work running"
		}
	}
	return &services.BotResumeTarget{
		SessionID:     strings.TrimSpace(session.SessionID),
		UserID:        strings.TrimSpace(session.UserID),
		AgentMode:     strings.TrimSpace(session.AgentMode),
		Status:        botResumeStatusWithActivity(session.Status, session.LastActivity),
		Query:         strings.TrimSpace(session.Query),
		WorkspacePath: strings.TrimSpace(session.WorkspacePath),
		PresetQueryID: strings.TrimSpace(session.PresetQueryID),
		PhaseID:       phaseID,
		WorkshopMode:  strings.TrimSpace(session.WorkshopMode),
		WorkflowName: firstNonEmptyTrimmed(
			session.WorkflowName,
			session.WorkflowLabel,
			session.PresetName,
		),
		Activity:              activity,
		HasBackgroundActivity: session.HasRunningBackgroundAgents,
	}
}

// firstNonEmptyTrimmed returns the first argument that is non-empty after
// trimming surrounding whitespace.
func firstNonEmptyTrimmed(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func botResumeStatusWithActivity(status string, lastActivity time.Time) string {
	status = strings.TrimSpace(status)
	if status != "" {
		return status
	}
	if lastActivity.IsZero() {
		return "completed"
	}
	return "running"
}

func parseBotResumeOrdinal(selector string) (int, bool) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return 0, false
	}
	n := 0
	for _, r := range selector {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
