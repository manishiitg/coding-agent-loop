package server

import (
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

// UI control is an interactive host capability, not a WorkshopMode or write
// permission. Schedules deliberately use workflow-builder too, and read-only
// human Builders use run mode, so neither phase nor mode alone is sufficient.
// Decide at definition construction, before the CLI snapshots its tool catalog;
// do not bolt a second per-turn tool-name allowlist onto get_api_spec.
func workflowUICallerAllowed(phase, session string, req QueryRequest, active *ActiveSessionInfo) bool {
	if phase != workflowtypes.WorkflowStatusWorkflowBuilder || strings.TrimSpace(req.AgentProfileID) != "" {
		return false
	}
	if req.IsAutoNotification || strings.TrimSpace(req.ParentSessionID) != "" || strings.TrimSpace(req.SessionKind) != "" || strings.TrimSpace(req.BotPlatform) != "" {
		return false
	}
	if active != nil && (strings.TrimSpace(active.ParentSessionID) != "" || strings.TrimSpace(active.SessionKind) != "" || strings.TrimSpace(active.BotPlatform) != "") {
		return false
	}
	// Explicit "make interactive" keeps the scheduled session ID by design.
	// Merely opening/observing a schedule or retaining its native CLI is NOT
	// promotion. Child/Pulse/bot restrictions above still apply.
	if req.UserInteractiveContinuation {
		return true
	}
	if isScheduledSessionIdentity(session, req.TriggeredBy) {
		return false
	}
	if active != nil && isScheduledSessionIdentity(session, active.TriggeredBy) {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(req.TriggeredBy), "auto_notification")
}

func (api *StreamingAPI) registerWorkflowUIForCaller(registrar definitionToolRegistrar, phase, session, workspace string, req QueryRequest) error {
	active, _ := api.getActiveSession(session)
	if !workflowUICallerAllowed(phase, session, req, active) {
		// Invalidates any old lease if the same session changes role. The new
		// definition has none of the six UI tools; no bindings can revive it.
		api.uiBroker().setScope(session, "")
		return nil
	}
	if err := api.registerOpenWorkspaceViewTool(registrar, session, workspace); err != nil {
		return err
	}
	// Same gate as the UI tools above: widening a Gmail connection's grants
	// must only ever be something a real interactive user in this exact
	// workflow-builder chat asked for, never a scheduled/bot/sub-agent
	// session silently expanding its own permissions.
	return api.registerGmailConnectionManagementTools(registrar, session, workspace)
}
