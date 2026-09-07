package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	orchestratorevents "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
)

func uiJSON(v interface{}) string { data, _ := json.Marshal(v); return string(data) }
func uiError(err error) string {
	return uiJSON(map[string]interface{}{"status": "rejected", "code": err.Error(), "visible": false})
}

func (api *StreamingAPI) registerUIControlTools(registrar definitionToolRegistrar, session, workspace string) error {
	b := api.uiBroker()
	b.setScope(session, workspace)
	props := map[string]interface{}{
		"view":                    map[string]interface{}{"type": "string", "enum": workflowWorkspaceViewIDs()},
		"action":                  map[string]interface{}{"type": "string", "enum": []string{"open", "expand"}},
		"target":                  map[string]interface{}{"type": "string", "maxLength": 256, "description": "For flow/open: exact plan step ID. For report/open: exact top-level report tab label. For notify/expand: run_summary or pulse_review. Omit for other view openings."},
		"idempotency_key":         map[string]interface{}{"type": "string", "maxLength": 128, "description": "Reuse for a retry of this exact action; omit to generate a fresh request."},
		"expected_state_revision": map[string]interface{}{"type": "integer", "minimum": 0},
	}
	schema := func(p map[string]interface{}, required ...string) map[string]interface{} {
		return map[string]interface{}{"type": "object", "properties": p, "required": required, "additionalProperties": false}
	}
	if err := registrar.RegisterCustomTool("list_ui_capabilities", "Discover presentation-only actions on the bound AgentWorks workspace. Only advertised actions are implemented. Other products, deep targets and refresh are not yet supported by this acknowledged protocol; never infer support. No MCP connections or external sends.", schema(map[string]interface{}{}), func(context.Context, map[string]interface{}) (string, error) {
		_, err := b.snapshot(session)
		availability := "available"
		if err != nil {
			availability = err.Error()
		}
		return uiJSON(map[string]interface{}{"schema_version": uiControlContract.Version, "product": uiControlContract.Product, "availability": availability, "views": uiControlContract.Views}), nil
	}, "workflow_ui"); err != nil {
		return err
	}
	if err := registrar.RegisterCustomTool("get_ui_state", "Inspect the bound browser's bounded presentation state (no DOM/content/secrets). State is a recent browser observation, not proof that a human read it. An absent or ambiguous browser is explicitly unavailable.", schema(map[string]interface{}{}), func(context.Context, map[string]interface{}) (string, error) {
		s, err := b.snapshot(session)
		if err != nil {
			return uiError(err), nil
		}
		return uiJSON(s), nil
	}, "workflow_ui"); err != nil {
		return err
	}
	if err := registrar.RegisterCustomTool("perform_ui_action", "Perform one semantic presentation action and wait up to 10 seconds for a browser receipt. Discover capabilities first. applied confirms the view shell rendered, not that every data request succeeded. Notify expand confirms its instructions are visible. accepted/applying/expired are NOT success. Never retry an unknown outcome with a new key; use get_ui_action_result. Does not send, save, run, delete or connect anything.", schema(props, "view", "action"), func(ctx context.Context, args map[string]interface{}) (string, error) {
		return api.performUIAction(ctx, session, workspace, args)
	}, "workflow_ui"); err != nil {
		return err
	}
	return registrar.RegisterCustomTool("get_ui_action_result", "Read an action receipt without executing it again. Results are retained for ten minutes; unknown_request is not permission to blindly retry.", schema(map[string]interface{}{"request_id": map[string]interface{}{"type": "string"}}, "request_id"), func(_ context.Context, args map[string]interface{}) (string, error) {
		id, _ := args["request_id"].(string)
		a, err := b.result(session, id)
		if err != nil {
			return uiError(err), nil
		}
		return uiJSON(a), nil
	}, "workflow_ui")
}

func (api *StreamingAPI) performUIAction(ctx context.Context, session, workspace string, args map[string]interface{}) (string, error) {
	b := api.uiBroker()
	if b.scope(session) != workspace {
		return uiError(fmt.Errorf("inactive_scope")), nil
	}
	view, _ := args["view"].(string)
	action, _ := args["action"].(string)
	target, _ := args["target"].(string)
	key, _ := args["idempotency_key"].(string)
	var revision *int64
	if raw, ok := args["expected_state_revision"]; ok {
		n, ok := raw.(float64)
		if !ok || n < 0 || n != float64(int64(n)) {
			return uiError(fmt.Errorf("invalid_revision")), nil
		}
		v := int64(n)
		revision = &v
	}
	a, fresh, err := b.submit(session, view, action, target, key, revision)
	if err != nil {
		return uiError(err), nil
	}
	if fresh {
		api.emitAgentProfileEvent(session, &orchestratorevents.PresentationUpdatedEvent{PresentationID: a.RequestID, Kind: "workflow.ui-action", WorkspacePath: workspace, Title: "Workspace action requested", Payload: map[string]interface{}{"request_id": a.RequestID}})
	}
	if !uiTerminal(a.Status) {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-a.done:
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	result, err := b.result(session, a.RequestID)
	if err != nil {
		return uiError(err), nil
	}
	return uiJSON(result), nil
}
