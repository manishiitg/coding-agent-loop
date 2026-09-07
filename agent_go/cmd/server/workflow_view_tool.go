package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	orchestratorevents "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
)

// workflowWorkspaceViews mirrors the toolbar's view registry in
// frontend/src/components/workflow/workspaceViews.ts (a frontend test keeps
// the two lists identical). The agent opens one of these for the user with
// open_workspace_view; the workflow page switches the right-hand pane.
var workflowWorkspaceViews = func() []struct{ ID, Label, About string } {
	views := make([]struct{ ID, Label, About string }, 0, len(uiControlContract.Views))
	for _, view := range uiControlContract.Views {
		about := view.Label
		if view.ID == "pulse" {
			about = "Needs your decision cards, saved answers and their application history, review findings and Pulse status"
		}
		views = append(views, struct{ ID, Label, About string }{view.ID, view.Label, about})
	}
	return views
}()

// WorkflowViewPresentationKind is the presentation kind the workflow page
// reacts to by opening a workspace view.
const WorkflowViewPresentationKind = "workflow.view"

func workflowWorkspaceViewIDs() []string {
	ids := make([]string, 0, len(workflowWorkspaceViews))
	for _, v := range workflowWorkspaceViews {
		ids = append(ids, v.ID)
	}
	return ids
}

// workspaceViewPresentation is the event that opens a view for the user.
func workspaceViewPresentation(view, workspacePath string) (*orchestratorevents.PresentationUpdatedEvent, error) {
	return workspaceViewAction(view, workspacePath, "open", "")
}

// workspaceViewAction builds the open or refresh event for a view; the page
// reads payload.action to tell them apart, and payload.target for what to
// focus inside the view.
func workspaceViewAction(view, workspacePath, action, target string) (*orchestratorevents.PresentationUpdatedEvent, error) {
	view = strings.TrimSpace(strings.ToLower(view))
	for _, v := range workflowWorkspaceViews {
		if v.ID != view {
			continue
		}
		label := "Open requested"
		if action == "refresh" {
			label = "Refresh requested"
		}
		detail := v.Label
		payload := map[string]interface{}{"view": v.ID, "action": action}
		if target = strings.TrimSpace(target); target != "" {
			payload["target"] = target
			detail = v.Label + " · " + target
		}
		return &orchestratorevents.PresentationUpdatedEvent{
			PresentationID: "workspace-view:" + v.ID + ":" + action,
			Kind:           WorkflowViewPresentationKind,
			Title:          v.Label,
			WorkspacePath:  workspacePath,
			Payload:        payload,
			Activity:       &orchestratorevents.PresentationActivity{Label: label, Destination: "the workspace pane", Detail: detail},
		}, nil
	}
	return nil, fmt.Errorf("unknown view %q; one of: %s", view, strings.Join(workflowWorkspaceViewIDs(), ", "))
}

// registerOpenWorkspaceViewTool gives the workflow agent the toolbar: it can
// open any workspace view on the right for the user (the report after
// building it, the costs when asked about spend, the schedules after adding
// one) instead of describing where to click.
func (api *StreamingAPI) registerOpenWorkspaceViewTool(registrar definitionToolRegistrar, sessionID, workspacePath string) error {
	if err := api.registerUIControlTools(registrar, sessionID, workspacePath); err != nil {
		return err
	}
	var lines []string
	for _, v := range workflowWorkspaceViews {
		lines = append(lines, fmt.Sprintf("%s — %s (%s)", v.ID, v.Label, v.About))
	}
	description := "Browser-acknowledged workspace opening, using the same protocol as perform_ui_action. Only status=applied confirms success. For Plan, pass an exact step ID as target to select it and open its details. For Report, pass an exact top-level report tab label as target; the platform delivers report.focus/report:focus to the report. For Files, pass a workspace-relative file path as target to load that file in the right-side viewer. Other deep targets are unsupported and rejected. Open one of the workspace views on the right side of the user's screen, the same views as the toolbar above the chat. " +
		"Use it when what you are talking about is on one of them: after you build or update the report, open `report`; when the user asks about spend, open `costs`; " +
		"after adding a schedule, open `schedules`. To request a reload use refresh_workspace_view (legacy, unverified). No sends, saves, or workflow execution. Views:\n" + strings.Join(lines, "\n")
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"view":   map[string]interface{}{"type": "string", "enum": workflowWorkspaceViewIDs(), "description": "which view to open"},
			"target": map[string]interface{}{"type": "string", "maxLength": 1024, "description": "For open: exact Plan step ID with view=flow, exact top-level report tab label with view=report, or workspace-relative file path with view=files. For legacy refresh: optional view-specific target; rendering is unverified."},
		},
		"required": []string{"view"},
	}
	if err := registrar.RegisterCustomTool("open_workspace_view", description, params, func(ctx context.Context, args map[string]interface{}) (string, error) {
		if api.uiBroker().scope(sessionID) != workspacePath {
			return uiError(fmt.Errorf("inactive_scope")), nil
		}
		view, _ := args["view"].(string)
		target, _ := args["target"].(string)
		return api.performUIAction(ctx, sessionID, workspacePath, map[string]interface{}{"view": strings.ToLower(strings.TrimSpace(view)), "action": "open", "target": target})
	}, "workflow_ui"); err != nil {
		return err
	}
	refreshDescription := "Legacy, unverified refresh request; requested is NOT proof that fresh content loaded. Reload a workspace view after you changed what it shows: the report after editing db/reports/index.html, the database after writing rows, schedules after adding one, files after writing them. " +
		"Opens the view first if it is not on screen. Same view names and the same optional `target` as open_workspace_view."
	return registrar.RegisterCustomTool("refresh_workspace_view", refreshDescription, params, func(_ context.Context, args map[string]interface{}) (string, error) {
		if api.uiBroker().scope(sessionID) != workspacePath {
			return uiError(fmt.Errorf("inactive_scope")), nil
		}
		view, _ := args["view"].(string)
		target, _ := args["target"].(string)
		event, err := workspaceViewAction(view, workspacePath, "refresh", target)
		if err != nil {
			return "", err
		}
		api.emitAgentProfileEvent(sessionID, event)
		out, _ := json.Marshal(map[string]interface{}{"status": "requested", "visible": false, "receipt": "unverified_legacy_presentation", "refreshed": event.Payload["view"], "label": event.Title})
		return string(out), nil
	}, "workflow_ui")
}
