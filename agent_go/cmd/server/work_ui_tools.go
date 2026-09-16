package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

var workUIControlContract = uiContract{
	Version: uiControlContract.Version,
	Product: "work",
	Views: []uiViewCapability{
		{ID: "report", Label: "Dashboard", Actions: []string{"open"}},
		{ID: "database", Label: "Database", Actions: []string{"open"}},
		{ID: "browser", Label: "Browser", Actions: []string{"open"}},
		{ID: "costs", Label: "Costs and usage", Actions: []string{"open"}},
		{ID: "schedules", Label: "Schedules", Actions: []string{"open"}},
		{ID: "files", Label: "Files", Actions: []string{"open"}},
		{ID: "skills", Label: "Skills", Actions: []string{"open"}},
		{ID: "secrets", Label: "Secrets", Actions: []string{"open"}},
		{ID: "mcp", Label: "MCP servers", Actions: []string{"open"}},
		{ID: "llm", Label: "Agent configuration", Actions: []string{"open"}},
		{ID: "bots", Label: "Bots", Actions: []string{"open"}},
		{ID: "folders", Label: "Attached folders", Actions: []string{"open"}},
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
	// Product conversation records carry the physical per-user workspace path,
	// while the Work surface identifies its DOM host with the public Chats/Work
	// path returned by the workspace API. UI control is presentation-only, so
	// bind it to that canonical browser identifier; filesystem tools continue to
	// receive the user-scoped physical path.
	workspace = canonicalChatHistoryWorkspacePath(userID, workspace)
	if err := api.registerUIControlToolsForContract(registrar, session, workspace, workUIControlContract); err != nil {
		return err
	}
	lines := make([]string, 0, len(workUIControlContract.Views))
	for _, view := range workUIControlContract.Views {
		lines = append(lines, fmt.Sprintf("%s — %s", view.ID, view.Label))
	}
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"view": map[string]interface{}{"type": "string", "enum": uiContractViewIDs(workUIControlContract)},
		},
		"required":             []string{"view"},
		"additionalProperties": false,
	}
	description := "Open a Work workspace view in the right-side panel and wait for the browser to acknowledge that the panel rendered. Use it after creating or discussing an artifact so the user can see it: open `report` for the project Dashboard, `files` for workspace files, `browser` for live browser work, or the matching setup view after configuration. Only status=applied confirms success. This presentation tool does not save, run, send, connect, or modify anything. Views:\n" + strings.Join(lines, "\n")
	if err := registrar.RegisterCustomTool("open_workspace_view", description, params, func(ctx context.Context, args map[string]interface{}) (string, error) {
		if api.uiBroker().scope(session) != workspace {
			return uiError(fmt.Errorf("inactive_scope")), nil
		}
		view, _ := args["view"].(string)
		return api.performUIActionForContract(ctx, session, workspace, workUIControlContract, map[string]interface{}{"view": strings.ToLower(strings.TrimSpace(view)), "action": "open"})
	}, "product_ui"); err != nil {
		return err
	}
	refreshDescription := "Request a reload of a Work workspace view after changing what it shows. This legacy event is unverified and opens the view if needed."
	return registrar.RegisterCustomTool("refresh_workspace_view", refreshDescription, params, func(_ context.Context, args map[string]interface{}) (string, error) {
		if api.uiBroker().scope(session) != workspace {
			return uiError(fmt.Errorf("inactive_scope")), nil
		}
		view, _ := args["view"].(string)
		view = strings.ToLower(strings.TrimSpace(view))
		if err := validateUIActionForContract(workUIControlContract, view, "open", ""); err != nil {
			return uiError(err), nil
		}
		event, err := workspaceViewAction(view, workspace, "refresh", "")
		if err != nil {
			return "", err
		}
		api.emitAgentProfileEvent(session, event)
		out, _ := json.Marshal(map[string]interface{}{"status": "requested", "visible": false, "receipt": "unverified_legacy_presentation", "refreshed": view, "label": event.Title})
		return string(out), nil
	}, "product_ui")
}
