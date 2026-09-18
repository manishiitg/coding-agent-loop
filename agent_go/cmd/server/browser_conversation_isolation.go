package server

import (
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

func bindConversationBrowserIsolation(sessionID, userID, selectedWorkspace string, profile *resolvedAgentProfile) {
	workspace := normalizeConversationWorkspace(selectedWorkspace)
	if strings.HasPrefix(workspace, "Workflow/") {
		common.BindSessionBrowserIsolationForWorkflow(sessionID, userID, workspace)
		return
	}
	if profile != nil && strings.EqualFold(strings.TrimSpace(profile.Definition.ID), "work") && workspace != "" {
		common.BindSessionBrowserIsolationForUserWorkspace(sessionID, userID, workspace)
		return
	}
	common.BindSessionBrowserIsolation(sessionID, userID)
}
