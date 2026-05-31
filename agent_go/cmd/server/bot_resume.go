package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	slackservice "mcp-agent-builder-go/agent_go/cmd/server/services"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

func (api *StreamingAPI) resolveBotResumeTarget(ctx context.Context, userID, selector string, filter slackservice.BotResumeFilter) (*slackservice.BotResumeTarget, error) {
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

	var matches []slackservice.BotResumeTarget
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

func (api *StreamingAPI) listBotResumeTargets(ctx context.Context, userID string, filter slackservice.BotResumeFilter) ([]slackservice.BotResumeTarget, error) {
	_ = ctx
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
	targets := make([]slackservice.BotResumeTarget, 0, len(candidates))
	for _, session := range candidates {
		if !botResumeCandidateStatus(session.Status) || !botResumeCandidateMode(session.AgentMode) {
			continue
		}
		if strings.TrimSpace(session.BotPlatform) != "" {
			continue
		}
		if filter.WorkspacePath != "" && strings.TrimSpace(session.WorkspacePath) != filter.WorkspacePath {
			continue
		}
		if filter.PresetQueryID != "" && strings.TrimSpace(session.PresetQueryID) != filter.PresetQueryID {
			continue
		}
		if target := botResumeTargetFromActive(&session); target != nil {
			targets = append(targets, *target)
		}
	}
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

func botResumeTargetFromActive(session *ActiveSessionInfo) *slackservice.BotResumeTarget {
	if session == nil {
		return nil
	}
	phaseID := strings.TrimSpace(session.PhaseID)
	if strings.TrimSpace(session.AgentMode) == "workflow_phase" && phaseID == "" {
		phaseID = workflowtypes.WorkflowStatusWorkflowBuilder
	}
	return &slackservice.BotResumeTarget{
		SessionID:     strings.TrimSpace(session.SessionID),
		UserID:        strings.TrimSpace(session.UserID),
		AgentMode:     strings.TrimSpace(session.AgentMode),
		Status:        botResumeStatusWithActivity(session.Status, session.LastActivity),
		Query:         strings.TrimSpace(session.Query),
		WorkspacePath: strings.TrimSpace(session.WorkspacePath),
		PresetQueryID: strings.TrimSpace(session.PresetQueryID),
		PhaseID:       phaseID,
		WorkshopMode:  strings.TrimSpace(session.WorkshopMode),
	}
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
