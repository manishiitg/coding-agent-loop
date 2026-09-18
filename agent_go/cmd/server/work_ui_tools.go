package server

import (
	"strings"
)

var workUIControlContract = uiContract{
	Version: uiControlContract.Version,
	Product: "work",
	Views: []uiViewCapability{
		{ID: "report", Label: "Dashboard", Actions: []string{"open", "refresh"}},
		{ID: "database", Label: "Database", Actions: []string{"open", "refresh"}},
		{ID: "browser", Label: "Browser", Actions: []string{"open", "refresh"}},
		{ID: "costs", Label: "Costs and usage", Actions: []string{"open", "refresh"}},
		{ID: "schedules", Label: "Schedules", TargetKind: "view_section", Actions: []string{"open", "refresh"}, Targets: []string{"schedules", "webhooks"}},
		{ID: "files", Label: "Files", Actions: []string{"open", "refresh"}},
		{ID: "skills", Label: "Skills", Actions: []string{"open", "refresh"}},
		{ID: "secrets", Label: "Secrets", Actions: []string{"open", "refresh"}},
		{ID: "mcp", Label: "MCP servers", Actions: []string{"open", "refresh"}},
		{ID: "llm", Label: "Agent configuration", Actions: []string{"open", "refresh"}},
		{ID: "bots", Label: "Bots", Actions: []string{"open", "refresh"}},
		{ID: "folders", Label: "Attached folders", Actions: []string{"open", "refresh"}},
	},
}

func registerWorkUIAllowed(req QueryRequest) bool {
	trigger := strings.ToLower(strings.TrimSpace(req.TriggeredBy))
	return strings.EqualFold(strings.TrimSpace(req.AgentProfileID), "work") &&
		(trigger == "" || trigger == "manual" || req.UserInteractiveContinuation) && strings.TrimSpace(req.BotPlatform) == "" &&
		strings.TrimSpace(req.ParentSessionID) == "" && strings.TrimSpace(req.SessionKind) == "" &&
		!req.IsAutoNotification
}

func (api *StreamingAPI) registerOpenWorkWorkspaceViewTool(registrar definitionToolRegistrar, userID, session, workspace string) error {
	workspace = canonicalChatHistoryWorkspacePath(userID, workspace)
	return api.registerUIControlToolsForContract(registrar, session, workspace, workUIControlContract)
}
