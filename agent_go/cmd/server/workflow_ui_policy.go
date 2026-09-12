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
	mode := "workshop"
	if req.ExecutionOptions != nil && req.ExecutionOptions.WorkshopMode != "" {
		mode = req.ExecutionOptions.WorkshopMode
	}
	policy := resolveWorkflowChatPolicy(mode, session, req, active, false)
	return policy.allows("workspace_ui")

}

func (api *StreamingAPI) registerWorkflowUIForCaller(registrar definitionToolRegistrar, phase, session, workspace string, req QueryRequest, readOnly bool) error {
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
	// Viewing the workspace does not grant connection-editing authority.
	mode := "workshop"
	if req.ExecutionOptions != nil && req.ExecutionOptions.WorkshopMode != "" {
		mode = req.ExecutionOptions.WorkshopMode
	}
	policy := resolveWorkflowChatPolicy(mode, session, req, active, readOnly)
	if !policy.allows("secret_management") {
		return nil
	}
	return api.registerGmailConnectionManagementTools(registrar, session, workspace)
}
