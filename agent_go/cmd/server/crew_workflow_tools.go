package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// registerCrewWorkflowRunTools gives a Crew run the workflow side of the
// bidirectional integration: list an attached workflow's triggers, run one
// through a platform-internal binding scoped to this Crew, and poll the run.
// No secret is ever presented or exposed; the binding caller stamp (this
// Crew's project ID plus profile) is the authorization, checked on dispatch
// and on every poll. Binding creation reuses the workflow trigger handler,
// so it enforces the same owner-or-write permission as Builder.
func (api *StreamingAPI) registerCrewWorkflowRunTools(registrar definitionToolRegistrar, userID, sessionID, workspacePath string) error {
	if api.scheduler == nil {
		return nil
	}
	_ = sessionID
	register := func(name, description string, parameters map[string]interface{}, execute func(context.Context, map[string]interface{}) (string, error)) error {
		return registrar.RegisterCustomTool(name, description, parameters, execute, "work_workflow_reference_tools")
	}
	claims := &UserClaims{UserID: strings.TrimSpace(userID)}
	if record := directoryUserFor(userID, "", ""); record != nil {
		claims.Username = record.Username
		claims.Email = record.Email
	}
	withClaims := func(ctx context.Context) context.Context {
		copy := *claims
		return context.WithValue(ctx, UserContextKey, &copy)
	}
	caller := func(ctx context.Context) (triggerCaller, error) {
		return crewWorkflowRunCaller(ctx, workspacePath)
	}
	attached := func(ctx context.Context, target string) (*WorkflowManifest, error) {
		return crewAttachedWorkflowManifest(ctx, workspacePath, target)
	}

	if err := register("list_attached_workflows", "List the workflows attached to this Crew as durable read-only context, with their workspace paths, labels, and IDs. Pass one exact workspace_path to list_workflow_triggers, run_workflow_trigger, or get_workflow_trigger_run. Attach more with attach_workflow_reference.", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		refs, err := readWorkWorkflowReferences(ctx, workspacePath)
		if err != nil {
			return "", fmt.Errorf("Crew references are unavailable")
		}
		type attachedWorkflow struct {
			WorkspacePath string `json:"workspace_path"`
			Label         string `json:"label,omitempty"`
			WorkflowID    string `json:"workflow_id,omitempty"`
			Available     bool   `json:"available"`
		}
		attached := make([]attachedWorkflow, 0, len(refs))
		for _, ref := range refs {
			entry := attachedWorkflow{WorkspacePath: ref}
			if manifest, exists, err := ReadWorkflowManifest(ctx, ref); err == nil && exists && manifest != nil {
				entry.Label = manifest.Label
				entry.WorkflowID = manifest.ID
				entry.Available = true
			}
			attached = append(attached, entry)
		}
		encoded, err := json.MarshalIndent(map[string]interface{}{"workflows": attached}, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}

	if err := register("list_workflow_triggers", "List the triggers of one workflow attached to this Crew, with their IDs, names, enabled state, and kind. Pass an exact workspace_path from list_attached_workflows; the workflow must already be attached. Never reveals trigger secrets.", map[string]interface{}{
		"type": "object", "required": []string{"workspace_path"}, "properties": map[string]interface{}{
			"workspace_path": map[string]interface{}{"type": "string", "description": "Exact attached workflow workspace_path from list_attached_workflows."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		target, _ := args["workspace_path"].(string)
		if _, err := attached(ctx, target); err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/workflow-webhooks?workspace_path="+url.QueryEscape(strings.TrimSuffix(strings.TrimSpace(target), "/")), nil)
		if err != nil {
			return "", err
		}
		rec := &accessToolResponse{header: make(http.Header)}
		api.scheduler.listWorkflowWebhooks(rec, req)
		if rec.status >= 400 {
			return "", fmt.Errorf("list triggers failed (%d): %s", rec.status, strings.TrimSpace(rec.String()))
		}
		return rec.String(), nil
	}); err != nil {
		return err
	}

	if err := register("run_workflow_trigger", "Run one attached workflow through a platform-internal trigger binding scoped to this Crew. Omit trigger_id to reuse this Crew's binding or create one (creating requires workflow owner or write access); pass trigger_id to use a specific internal trigger already bound to this Crew. Payloads must be JSON within 1 MiB. Returns the run ID; poll it with get_workflow_trigger_run. Reuse delivery_id when retrying the same delivery.", map[string]interface{}{
		"type": "object", "required": []string{"workspace_path"}, "properties": map[string]interface{}{
			"workspace_path": map[string]interface{}{"type": "string", "description": "Exact attached workflow workspace_path from list_attached_workflows."},
			"trigger_id":     map[string]interface{}{"type": "string", "description": "Internal trigger ID bound to this Crew; omit to reuse or create it."},
			"payload":        map[string]interface{}{"type": "object", "description": "JSON delivery payload within 1 MiB."},
			"delivery_id":    map[string]interface{}{"type": "string", "description": "Stable idempotency key; omit for a new delivery."},
			"event":          map[string]interface{}{"type": "string", "description": "Event type; defaults to crew.trigger."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		crew, err := caller(ctx)
		if err != nil {
			return "", err
		}
		target, _ := args["workspace_path"].(string)
		manifest, err := attached(ctx, target)
		if err != nil {
			return "", err
		}
		triggerID, _ := args["trigger_id"].(string)
		if strings.TrimSpace(triggerID) == "" {
			triggerID, err = crewWorkflowTriggerBinding(ctx, api, strings.TrimSuffix(strings.TrimSpace(target), "/"), manifest, crew)
			if err != nil {
				return "", err
			}
			manifest, err = attached(ctx, target)
			if err != nil {
				return "", err
			}
		}
		payload := map[string]interface{}{}
		if raw, ok := args["payload"]; ok {
			typed, ok := raw.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("payload must be a JSON object")
			}
			payload = typed
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("invalid payload")
		}
		deliveryID, _ := args["delivery_id"].(string)
		if strings.TrimSpace(deliveryID) == "" {
			deliveryID = "crew-" + uuid.NewString()
		}
		event, _ := args["event"].(string)
		if strings.TrimSpace(event) == "" {
			event = "crew.trigger"
		}
		delivery, err := api.scheduler.dispatchInternalWorkflowTrigger(ctx, internalWorkflowTriggerCall{
			WorkflowID: manifest.ID, TriggerID: strings.TrimSpace(triggerID),
			Caller: crew, DeliveryID: deliveryID, Event: event, Payload: data,
		})
		if err != nil {
			return "", crewWorkflowRunError(err)
		}
		encoded, err := json.MarshalIndent(map[string]interface{}{
			"status": delivery.Status, "run_id": delivery.RunID,
			"delivery_id": delivery.DeliveryID, "duplicate": delivery.Duplicate,
			"trigger_id": strings.TrimSpace(triggerID),
		}, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}

	return register("get_workflow_trigger_run", "Poll one workflow run started by run_workflow_trigger. Returns its status, terminal flag, step outputs, progress, and signed artifact links. The binding must still exist and still name this Crew; revoked or rebound triggers fail fast.", map[string]interface{}{
		"type": "object", "required": []string{"workspace_path", "trigger_id", "run_id"}, "properties": map[string]interface{}{
			"workspace_path": map[string]interface{}{"type": "string", "description": "Exact attached workflow workspace_path from list_attached_workflows."},
			"trigger_id":     map[string]interface{}{"type": "string", "description": "Internal trigger ID from run_workflow_trigger."},
			"run_id":         map[string]interface{}{"type": "string", "description": "Run ID from run_workflow_trigger."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = withClaims(ctx)
		crew, err := caller(ctx)
		if err != nil {
			return "", err
		}
		target, _ := args["workspace_path"].(string)
		manifest, err := attached(ctx, target)
		if err != nil {
			return "", err
		}
		triggerID, _ := args["trigger_id"].(string)
		runID, _ := args["run_id"].(string)
		if strings.TrimSpace(triggerID) == "" || strings.TrimSpace(runID) == "" {
			return "", fmt.Errorf("trigger_id and run_id are required")
		}
		result, err := api.scheduler.readInternalWorkflowTriggerRun(ctx, strings.TrimSuffix(strings.TrimSpace(target), "/"), manifest, strings.TrimSpace(triggerID), strings.TrimSpace(runID), crew)
		if err != nil {
			return "", crewWorkflowRunError(err)
		}
		encoded, err := json.MarshalIndent(result, "", "  ")
		return string(encoded), err
	})
}

// crewWorkflowRunCaller stamps the calling Crew from its own product manifest
// so bindings and polls authorize against the exact project the run belongs
// to, never a caller-supplied ID.
func crewWorkflowRunCaller(ctx context.Context, crewWorkspacePath string) (triggerCaller, error) {
	raw, exists, err := readFileFromWorkspace(ctx, strings.TrimSuffix(strings.TrimSpace(crewWorkspacePath), "/")+"/product.json")
	if err != nil || !exists {
		return triggerCaller{}, fmt.Errorf("Crew project is unavailable")
	}
	var manifest productProjectManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return triggerCaller{}, fmt.Errorf("Crew project is unavailable")
	}
	if strings.TrimSpace(manifest.ID) == "" {
		return triggerCaller{}, fmt.Errorf("Crew project is unavailable")
	}
	return triggerCaller{Type: triggerCallerCrew, ID: strings.TrimSpace(manifest.ID), ProfileID: normalizeInternalProfileID(manifest.Product)}, nil
}

// crewAttachedWorkflowManifest resolves an attached workflow target: the path
// must be attached to the Crew, the user must still have access, and the
// manifest must still exist. Detached, revoked, or deleted workflows fail
// fast before any trigger runs.
func crewAttachedWorkflowManifest(ctx context.Context, crewWorkspacePath, target string) (*WorkflowManifest, error) {
	path := strings.TrimSuffix(strings.TrimSpace(target), "/")
	if path == "" {
		return nil, fmt.Errorf("workspace_path is required")
	}
	refs, err := readWorkWorkflowReferences(ctx, crewWorkspacePath)
	if err != nil {
		return nil, fmt.Errorf("Crew references are unavailable")
	}
	found := false
	for _, ref := range refs {
		if strings.TrimSuffix(strings.TrimSpace(ref), "/") == path {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("workflow %q is not attached to this Crew; attach it with attach_workflow_reference first", path)
	}
	if _, err := authorizeWorkflowContextPaths(ctx, []string{path}); err != nil {
		return nil, fmt.Errorf("workflow %q is unavailable or access denied", path)
	}
	manifest, exists, err := ReadWorkflowManifest(ctx, path)
	if err != nil || !exists || manifest == nil {
		return nil, fmt.Errorf("workflow %q is unavailable", path)
	}
	return manifest, nil
}

// crewWorkflowTriggerBinding reuses this Crew's internal binding on the
// workflow or creates one with the same full-workflow defaults Builder uses.
// Creation goes through the trigger handler, so group, route, and caller
// validation plus owner-or-write permission match every other writer.
func crewWorkflowTriggerBinding(ctx context.Context, api *StreamingAPI, workspacePath string, manifest *WorkflowManifest, crew triggerCaller) (string, error) {
	for _, sched := range manifest.Schedules {
		if sched.ScheduleType != "webhook" || !isInternalTriggerKind(sched.Kind) {
			continue
		}
		if sched.Caller.matchesPresented(triggerCallerCrew, crew) {
			return sched.ID, nil
		}
	}
	payload := map[string]interface{}{
		"workspace_path": workspacePath, "name": "Crew invocation",
		"enabled": true, "route_selections": map[string]string{}, "group_names": []string{"default"},
		"kind":   triggerKindInternal,
		"caller": map[string]interface{}{"type": crew.Type, "id": crew.ID, "profile_id": crew.ProfileID},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("cannot create the Crew binding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/workflow-webhooks?workspace_path="+url.QueryEscape(workspacePath), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	rec := &accessToolResponse{header: make(http.Header)}
	api.scheduler.saveWorkflowWebhook(rec, req)
	if rec.status == http.StatusForbidden {
		return "", fmt.Errorf("creating the Crew binding needs workflow owner or write access; ask the workflow owner to create an internal trigger bound to this Crew")
	}
	if rec.status >= 400 {
		return "", fmt.Errorf("cannot create the Crew binding (%d): %s", rec.status, strings.TrimSpace(rec.String()))
	}
	var created workflowWebhookResponse
	if err := json.Unmarshal(rec.Bytes(), &created); err != nil || strings.TrimSpace(created.ID) == "" {
		return "", fmt.Errorf("cannot create the Crew binding")
	}
	return created.ID, nil
}

// crewWorkflowRunError translates internal dispatch failures into the
// actionable messages Crews act on; unknown errors pass through unchanged.
func crewWorkflowRunError(err error) error {
	switch {
	case err == nil:
		return nil
	case strings.Contains(err.Error(), ErrInternalCallerMismatch.Error()):
		return fmt.Errorf("trigger binding no longer names this Crew; ask the workflow owner to rebind it")
	case strings.Contains(err.Error(), ErrInternalTriggerDisabled.Error()):
		return fmt.Errorf("trigger is disabled; ask the workflow owner to enable it")
	case strings.Contains(err.Error(), ErrInternalTriggerNotFound.Error()):
		return fmt.Errorf("trigger not found on the attached workflow")
	case strings.Contains(err.Error(), ErrInternalTriggerRunGone.Error()):
		return fmt.Errorf("run not found; it may have expired")
	case strings.Contains(err.Error(), ErrInternalTriggerPayload.Error()):
		return fmt.Errorf("payload must be JSON within 1 MiB")
	case strings.Contains(err.Error(), ErrWebhookConcurrencyLimit.Error()):
		return fmt.Errorf("workflow is busy; retry with the same delivery_id")
	default:
		return err
	}
}
