package server

import "strings"

// Set after user secrets: execution context is platform metadata inherited by
// shell executors and saved-script children, not a workflow-specific variable.
func (api *StreamingAPI) setPlaywrightExecutionContext(env map[string]string, sessionID string, req QueryRequest) {
	active, _ := api.getActiveSession(sessionID)
	origin := playwrightExecutionContext(sessionID, req, active)
	parent := req.ParentSessionID
	if active != nil && active.ParentSessionID != "" {
		parent = active.ParentSessionID
	}
	seen := map[string]bool{sessionID: true}
	for origin == "builder" && parent != "" && !seen[parent] {
		seen[parent] = true
		ancestor, _ := api.getActiveSession(parent)
		if ancestor == nil {
			break
		}
		origin = playwrightExecutionContext(parent, QueryRequest{}, ancestor)
		parent = ancestor.ParentSessionID
	}
	env["AGENTWORKS_EXECUTION_CONTEXT"] = origin
}

func playwrightExecutionContext(sessionID string, req QueryRequest, active *ActiveSessionInfo) string {
	trigger := req.TriggeredBy
	if active != nil && active.TriggeredBy != "" {
		trigger = active.TriggeredBy
	}
	if req.BotPlatform != "" || active != nil && active.BotPlatform != "" {
		return "bot"
	}
	if strings.EqualFold(strings.TrimSpace(trigger), "webhook") {
		return "webhook"
	}
	if isScheduledSessionIdentity(sessionID, trigger) {
		return "schedule"
	}
	if req.PulseLifecycleTurn {
		return "pulse"
	}
	if req.IsAutoNotification {
		return "notification"
	}
	return "builder"
}
