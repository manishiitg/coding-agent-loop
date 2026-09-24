package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	todo_creation_human "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

// Trigger links let Crews and workflows call each other in every direction
// (crew→crew, crew→workflow, workflow→crew, workflow→workflow) through the
// existing secretless internal trigger bindings:
//
//   - connect_to_target reuses or creates the binding on the target that
//     names this caller,
//   - call_target fires it and returns the run ID immediately,
//   - get_target_run polls that run,
//   - and, unless notify=false, the platform resumes the caller's own chat
//     with an [AUTO-NOTIFICATION] carrying the final result, failure, or
//     timeout — the same mechanism trigger_and_auto_notify uses.
//
// Targets are any Crew on the server (Crews are shared read-write) and
// workflows the requester owns or can edit; workflow readers and Crew
// Run-mode readers cannot trigger.

const (
	triggerTargetDefaultTimeout = 60 * time.Minute
	triggerTargetMaxTimeout     = 24 * time.Hour
	triggerTargetResultLimit    = 16 * 1024
	triggerTargetEvent          = "agentworks.call"
)

// triggerTargetPollInterval is how often the auto-notify watcher polls a
// target run. A package var so tests can shorten it.
var triggerTargetPollInterval = 10 * time.Second

type triggerTarget struct {
	Kind        string // triggerCallerCrew or triggerCallerWorkflow
	Path        string
	Label       string
	CrewID      string
	CrewProfile string
	// CrewOwner is the user whose workspace holds a Crew target. Crews are
	// shared server-wide, so it may differ from the requester.
	CrewOwner string
	Manifest  *WorkflowManifest
}

// ownerOr returns the Crew target's owner, falling back to userID.
func (t triggerTarget) ownerOr(userID string) string {
	if strings.TrimSpace(t.CrewOwner) != "" {
		return t.CrewOwner
	}
	return userID
}

func (t triggerTarget) describe() map[string]interface{} {
	return map[string]interface{}{"kind": t.Kind, "name": t.Label, "workspace_path": t.Path}
}

// triggerLinkCaller identifies the calling Crew or workflow.
type triggerLinkCaller struct {
	Stamp triggerCaller
	Label string
	Path  string
}

// resolveTriggerTarget finds one Crew on the server, or one workflow the user
// may open, by exact workspace path, ID, name, identity name, or folder name.
// A "crew:" / "workflow:" prefix (as inserted by #crew:/#workflow: tags)
// restricts the kind. Ambiguous names fail with the candidates listed.
func resolveTriggerTarget(ctx context.Context, claims *UserClaims, raw string) (triggerTarget, error) {
	query := strings.TrimSpace(raw)
	query = strings.TrimPrefix(query, "#")
	wantKind := ""
	lower := strings.ToLower(query)
	for _, kind := range []string{triggerCallerCrew, triggerCallerWorkflow} {
		if strings.HasPrefix(lower, kind+":") {
			wantKind = kind
			query = strings.TrimSpace(query[len(kind)+1:])
			break
		}
	}
	if query == "" {
		return triggerTarget{}, fmt.Errorf("target is required: pass a Crew or workflow name or its exact workspace_path")
	}
	userID := ""
	if claims != nil {
		userID = strings.TrimSpace(claims.UserID)
	}
	needle := strings.ToLower(strings.Trim(strings.TrimSpace(query), "/"))
	canonicalNeedle := strings.ToLower(strings.Trim(canonicalChatHistoryWorkspacePath(userID, query), "/"))
	matches := func(values ...string) bool {
		for _, value := range values {
			value = strings.ToLower(strings.Trim(strings.TrimSpace(value), "/"))
			if value != "" && (value == needle || value == canonicalNeedle) {
				return true
			}
		}
		return false
	}
	var found []triggerTarget
	if wantKind != triggerCallerWorkflow {
		crews, err := listAccessibleCrewProjects(ctx, userID, "")
		if err != nil {
			return triggerTarget{}, err
		}
		for _, crew := range crews {
			id, _ := crew["id"].(string)
			name, _ := crew["name"].(string)
			crewPath, _ := crew["workspace_path"].(string)
			identity := ""
			if value, ok := crew["identity"].(accessibleProjectIdentity); ok {
				identity = value.Name
			}
			if !matches(id, name, identity, crewPath, path.Base(crewPath)) {
				continue
			}
			owner := userID
			if crewOwner, ok := crewProjectOwnerID(crewPath); ok {
				owner = crewOwner
			}
			found = append(found, triggerTarget{Kind: triggerCallerCrew, Path: crewPath, Label: firstNonEmptyTrimmed(identity, name), CrewID: id, CrewProfile: "work", CrewOwner: owner})
		}
	}
	if wantKind != triggerCallerCrew {
		discovered, err := DiscoverWorkflowManifests(ctx)
		if err != nil {
			return triggerTarget{}, err
		}
		for _, workflow := range filterWorkflowManifestsForUser(claims, discovered) {
			workflowPath := strings.TrimSuffix(strings.TrimSpace(workflow.WorkspacePath), "/")
			if !matches(workflow.Manifest.ID, workflow.Manifest.Label, workflowPath, path.Base(workflowPath)) {
				continue
			}
			if workflow.MyAccess != WorkflowAccessOwner && workflow.MyAccess != WorkflowAccessWrite {
				return triggerTarget{}, fmt.Errorf("you only have read access to workflow %q; its owner or an editor must connect it", firstNonEmptyTrimmed(workflow.Manifest.Label, workflowPath))
			}
			found = append(found, triggerTarget{Kind: triggerCallerWorkflow, Path: workflowPath, Label: firstNonEmptyTrimmed(workflow.Manifest.Label, workflowPath), Manifest: workflow.Manifest})
		}
	}
	switch len(found) {
	case 0:
		return triggerTarget{}, fmt.Errorf("no Crew or workflow you can edit matches %q; call list_accessible_workflows to find its exact workspace_path", query)
	case 1:
		return found[0], nil
	default:
		names := make([]string, 0, len(found))
		for _, item := range found {
			names = append(names, item.Kind+" "+item.Label+" ("+item.Path+")")
		}
		sort.Strings(names)
		return triggerTarget{}, fmt.Errorf("%q matches more than one target: %s; pass the exact workspace_path", query, strings.Join(names, "; "))
	}
}

// sameTarget reports whether the caller is the target itself.
func (c triggerLinkCaller) isTarget(target triggerTarget) bool {
	if !strings.EqualFold(c.Stamp.Type, target.Kind) {
		return false
	}
	if target.Kind == triggerCallerCrew {
		return strings.TrimSpace(c.Stamp.ID) == strings.TrimSpace(target.CrewID)
	}
	return target.Manifest != nil && strings.TrimSpace(c.Stamp.ID) == strings.TrimSpace(target.Manifest.ID)
}

// crewTargetMessage is the standard inbound trigger message on a Crew that
// another Crew or workflow calls.
func crewTargetMessage(caller triggerLinkCaller) string {
	kind := "Crew"
	switch caller.Stamp.Type {
	case triggerCallerWorkflow:
		kind = "workflow"
	case triggerCallerUser:
		kind = "external connection"
	}
	return fmt.Sprintf("The connected %s %q sent you a task. The task is in the payload's `task` field; any extra input is under `payload`. Do the task, then end with a clear, self-contained final answer: it is returned to the caller.", kind, caller.Label)
}

// connectTriggerTarget reuses the internal binding on target that names this
// caller or creates the standard one. It returns the binding's trigger ID and
// whether it was created.
func (api *StreamingAPI) connectTriggerTarget(ctx context.Context, userID string, caller triggerLinkCaller, target triggerTarget) (string, bool, error) {
	return api.connectTriggerTargetWith(ctx, userID, caller, target, "", "", "")
}

// connectTriggerTargetWith is connectTriggerTarget with an optional custom
// Crew trigger: when instructions are given, a new trigger with that name,
// message and run destination is created on the target Crew instead of
// reusing the standard binding. The trigger is an ordinary entry in the
// target's own trigger list; its name and caller stamp record who created it.
func (api *StreamingAPI) connectTriggerTargetWith(ctx context.Context, userID string, caller triggerLinkCaller, target triggerTarget, name, instructions, destination string) (string, bool, error) {
	if caller.isTarget(target) {
		return "", false, fmt.Errorf("a %s cannot connect to itself", target.Kind)
	}
	switch target.Kind {
	case triggerCallerCrew:
		if api.productSchedules == nil {
			return "", false, fmt.Errorf("Crew triggers are unavailable")
		}
		custom := strings.TrimSpace(instructions) != ""
		if !custom {
			triggers, err := api.productSchedules.projectWebhookConfigs(ctx, target.ownerOr(userID), target.CrewProfile, target.CrewID)
			if err != nil {
				return "", false, err
			}
			for _, trigger := range triggers {
				if trigger.IsInternal() && trigger.Enabled && trigger.Caller.matchesAnyPresented(caller.Stamp) {
					return trigger.ID, false, nil
				}
			}
		}
		message := crewTargetMessage(caller)
		if custom {
			message = strings.TrimSpace(instructions) + "\n\n" + message
		}
		stamp := caller.Stamp
		response, _, err := api.productSchedules.saveProductWebhookConfig(ctx, target.ownerOr(userID), productWebhookRequest{
			ProfileID: target.CrewProfile, ProjectID: target.CrewID,
			Name: firstNonEmptyTrimmed(name, "Called by "+caller.Label), Message: message,
			Enabled: true, RunDestination: firstNonEmptyTrimmed(destination, runDestinationCrewChat), Kind: triggerKindInternal, Caller: &stamp,
		}, "")
		if err != nil {
			return "", false, err
		}
		return response.ID, true, nil
	case triggerCallerWorkflow:
		return workflowTriggerBinding(ctx, api, target.Path, target.Manifest, caller.Stamp, "Called by "+caller.Label)
	default:
		return "", false, fmt.Errorf("unknown target kind %q", target.Kind)
	}
}

// callableTargetTriggers lists the target's enabled internal triggers bound
// to this caller.
func (api *StreamingAPI) callableTargetTriggers(ctx context.Context, userID string, caller triggerLinkCaller, target triggerTarget) ([]map[string]interface{}, error) {
	out := []map[string]interface{}{}
	switch target.Kind {
	case triggerCallerCrew:
		triggers, err := api.productSchedules.projectWebhookConfigs(ctx, target.ownerOr(userID), target.CrewProfile, target.CrewID)
		if err != nil {
			return nil, err
		}
		for _, trigger := range triggers {
			if trigger.IsInternal() && trigger.Enabled && trigger.Caller.matchesAnyPresented(caller.Stamp) {
				out = append(out, map[string]interface{}{"trigger_id": trigger.ID, "name": trigger.Name, "message": trigger.Message})
			}
		}
	case triggerCallerWorkflow:
		manifest, exists, err := ReadWorkflowManifest(ctx, target.Path)
		if err != nil || !exists || manifest == nil {
			return nil, fmt.Errorf("workflow is unavailable")
		}
		for _, sched := range manifest.Schedules {
			if sched.ScheduleType == "webhook" && sched.IsInternalTrigger() && sched.Enabled && sched.Caller.matchesAnyPresented(caller.Stamp) {
				out = append(out, map[string]interface{}{"trigger_id": sched.ID, "name": sched.Name})
			}
		}
	}
	return out, nil
}

// triggerTargetRunState is one poll of a target run.
type triggerTargetRunState struct {
	Terminal bool
	Failed   bool
	Status   string
	Result   string
	Raw      interface{}
}

func (api *StreamingAPI) readTriggerTargetRun(ctx context.Context, userID string, caller triggerLinkCaller, target triggerTarget, triggerID, runID string) (triggerTargetRunState, error) {
	switch target.Kind {
	case triggerCallerCrew:
		status, err := api.productSchedules.getInternalProductTriggerRun(ctx, userID, target.CrewProfile, target.CrewID, triggerID, runID, caller.Stamp)
		if err != nil {
			return triggerTargetRunState{}, crewWorkflowRunError(err)
		}
		state := triggerTargetRunState{Terminal: status.Terminal, Status: status.Status, Raw: status}
		state.Failed = status.Terminal && !strings.EqualFold(status.Status, "success")
		if state.Failed {
			state.Result = firstNonEmptyTrimmed(status.Error, "no error detail recorded")
		} else {
			state.Result = strings.TrimSpace(status.FinalResponse)
		}
		return state, nil
	case triggerCallerWorkflow:
		manifest, exists, err := ReadWorkflowManifest(ctx, target.Path)
		if err != nil || !exists || manifest == nil {
			return triggerTargetRunState{}, fmt.Errorf("workflow is unavailable")
		}
		result, err := api.scheduler.readInternalWorkflowTriggerRun(ctx, target.Path, manifest, triggerID, runID, caller.Stamp)
		if err != nil {
			return triggerTargetRunState{}, crewWorkflowRunError(err)
		}
		state := triggerTargetRunState{Terminal: result.Terminal, Status: result.Status, Raw: result}
		state.Failed = result.Terminal && (strings.TrimSpace(result.Error) != "" || workflowRunStatusFailed(result.Status))
		encoded, _ := json.MarshalIndent(map[string]interface{}{"status": result.Status, "error": result.Error, "steps": result.Steps, "run_folder": result.RunFolder}, "", "  ")
		state.Result = string(encoded)
		return state, nil
	default:
		return triggerTargetRunState{}, fmt.Errorf("unknown target kind %q", target.Kind)
	}
}

func workflowRunStatusFailed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "canceled", "cancelled", "timeout", "timed_out", "stopped":
		return true
	}
	return false
}

func truncateTriggerTargetResult(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= triggerTargetResultLimit {
		return value
	}
	return value[:triggerTargetResultLimit] + "\n[result truncated]"
}

func triggerTargetTimeout(value interface{}) (time.Duration, error) {
	if value == nil {
		return triggerTargetDefaultTimeout, nil
	}
	minutes, ok := value.(float64)
	if !ok {
		if integer, isInt := value.(int); isInt {
			minutes, ok = float64(integer), true
		}
	}
	if !ok || minutes < 1 || time.Duration(minutes)*time.Minute > triggerTargetMaxTimeout {
		return 0, fmt.Errorf("timeout_minutes must be between 1 and %d", int(triggerTargetMaxTimeout/time.Minute))
	}
	return time.Duration(minutes) * time.Minute, nil
}

// startTriggerTargetWatch registers a background execution in the caller's
// chat and polls the target run until it ends, fails, or times out; the
// existing auto-notification pipeline then resumes the caller's chat with the
// result. Like trigger_and_auto_notify, the watch does not survive a server
// restart; get_target_run still works afterwards.
func (api *StreamingAPI) startTriggerTargetWatch(parentReq QueryRequest, sessionID, userID string, caller triggerLinkCaller, target triggerTarget, triggerID, runID string, timeout time.Duration) (string, error) {
	if api == nil || api.bgAgentRegistry == nil {
		return "", fmt.Errorf("auto-notification is unavailable in this chat")
	}
	name := "Call " + target.Kind + " " + target.Label
	executionID := "target-run-" + api.bgAgentRegistry.NextID(name)
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	parentExecutionID := api.currentConversationTurnExecutionID(sessionID)
	if strings.TrimSpace(parentExecutionID) == "" {
		parentExecutionID = "session:" + sessionID
	}
	notifier := &workshopExecutionBgNotifier{
		api: api, sessionID: sessionID, workspacePath: parentReq.SelectedFolder,
		presetQueryID: parentReq.PresetQueryID, userID: userID,
	}
	notifier.OnExecutionStart(todo_creation_human.WorkshopExecutionStart{
		ID: executionID, ParentExecutionID: parentExecutionID, Name: name,
		Kind: "trigger_auto_notify", Cancel: cancel,
		Metadata: map[string]string{
			"execution_type": "trigger-auto-notify",
			"timeout":        timeout.String(),
			"target_kind":    target.Kind,
			"target_path":    target.Path,
			"run_id":         runID,
		},
	})
	registered := api.bgAgentRegistry.Get(sessionID, executionID)
	if registered == nil || registered.GetStatus() == BGAgentCanceled {
		cancel()
		return "", fmt.Errorf("auto-notification could not be registered")
	}
	claims := &UserClaims{UserID: userID}
	go func() {
		defer cancel()
		pollCtx := context.WithValue(runCtx, UserContextKey, claims)
		header := fmt.Sprintf("%s %q run %s (trigger %s)", target.Kind, target.Label, runID, triggerID)
		ticker := time.NewTicker(triggerTargetPollInterval)
		defer ticker.Stop()
		lastErr := error(nil)
		for {
			state, err := api.readTriggerTargetRun(pollCtx, userID, caller, target, triggerID, runID)
			if err == nil && state.Terminal {
				result := truncateTriggerTargetResult(state.Result)
				if state.Failed {
					notifier.OnExecutionComplete(executionID, name, result, nil, fmt.Errorf("%s ended %s: %s", header, state.Status, result))
					return
				}
				if result == "" {
					result = "(no final response recorded)"
				}
				notifier.OnExecutionComplete(executionID, name, header+" finished ("+state.Status+").\n\nFinal result:\n"+result, nil, nil)
				return
			}
			lastErr = err
			select {
			case <-runCtx.Done():
				detail := "it was still running"
				if lastErr != nil {
					detail = "last poll error: " + lastErr.Error()
				}
				if runCtx.Err() == context.DeadlineExceeded {
					notifier.OnExecutionComplete(executionID, name, "", nil, fmt.Errorf("%s did not finish within %s (%s); check it later with get_target_run", header, timeout, detail))
				} else {
					notifier.OnExecutionComplete(executionID, name, "", nil, context.Canceled)
				}
				return
			case <-ticker.C:
			}
		}
	}()
	return executionID, nil
}

// registerTriggerLinkTools registers connect_to_target, call_target and
// get_target_run for one calling Crew or workflow Builder chat.
func (api *StreamingAPI) registerTriggerLinkTools(registrar definitionToolRegistrar, userID, sessionID string, parentReq QueryRequest, resolveCaller func(context.Context) (triggerLinkCaller, error)) error {
	if api == nil || api.scheduler == nil || api.productSchedules == nil {
		return nil
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
	register := func(name, description string, parameters map[string]interface{}, execute func(context.Context, map[string]interface{}) (string, error)) error {
		return registrar.RegisterCustomTool(name, description, parameters, execute, "trigger_link_tools")
	}
	setup := func(ctx context.Context, args map[string]interface{}) (context.Context, triggerLinkCaller, triggerTarget, error) {
		ctx = withClaims(ctx)
		caller, err := resolveCaller(ctx)
		if err != nil {
			return ctx, triggerLinkCaller{}, triggerTarget{}, err
		}
		raw, _ := args["target"].(string)
		target, err := resolveTriggerTarget(ctx, claims, raw)
		if err != nil {
			return ctx, caller, triggerTarget{}, err
		}
		if caller.isTarget(target) {
			return ctx, caller, target, fmt.Errorf("a %s cannot call itself", target.Kind)
		}
		return ctx, caller, target, nil
	}
	targetSchema := map[string]interface{}{"type": "string", "description": "The Crew or workflow to call: its name, a #crew:<name> / #workflow:<name> tag from the user's message, or its exact workspace_path."}

	if err := register("connect_to_target", "Connect this chat's Crew or workflow to another Crew or workflow so it can call it: reuses the target's internal trigger bound to this caller, or creates a standard one (no public URL or secret). To create a purpose-specific trigger on a target Crew, pass name and instructions (what that Crew should do whenever it is called); it appears in that Crew's own trigger list, named for and bound to this caller. Targets are any Crew on the server and workflows you own or can edit; creating new triggers on a Crew or reusing its existing ones is always allowed. Returns the trigger_id and the target's triggers this caller may use. Use when the user tags #crew:<name> or #workflow:<name> and asks to connect, link, or be able to call it.", map[string]interface{}{
		"type": "object", "required": []string{"target"}, "properties": map[string]interface{}{
			"target":          targetSchema,
			"name":            map[string]interface{}{"type": "string", "description": "Crew targets only: name for a new purpose-specific trigger."},
			"instructions":    map[string]interface{}{"type": "string", "description": "Crew targets only: standing instructions the target Crew follows on every call through this new trigger."},
			"run_destination": map[string]interface{}{"type": "string", "enum": []string{runDestinationCrewChat, runDestinationIsolated}, "description": "Crew targets only: crew_chat (default, the Crew's main chat) or isolated (a fresh chat per call)."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx, caller, target, err := setup(ctx, args)
		if err != nil {
			return "", err
		}
		name, _ := args["name"].(string)
		instructions, _ := args["instructions"].(string)
		destination, _ := args["run_destination"].(string)
		if target.Kind != triggerCallerCrew && (strings.TrimSpace(instructions) != "" || strings.TrimSpace(destination) != "") {
			return "", fmt.Errorf("instructions and run_destination apply to Crew targets only; a workflow runs its own plan with the payload")
		}
		triggerID, created, err := api.connectTriggerTargetWith(ctx, userID, caller, target, name, instructions, destination)
		if err != nil {
			return "", err
		}
		callable, err := api.callableTargetTriggers(ctx, userID, caller, target)
		if err != nil {
			return "", err
		}
		encoded, err := json.MarshalIndent(map[string]interface{}{
			"target": target.describe(), "trigger_id": triggerID, "created": created,
			"callable_triggers": callable,
			"next":              "Call it with call_target; the result comes back to this chat automatically.",
		}, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}

	if err := register("call_target", "Send a task to another Crew or workflow and return immediately with its run_id. Connects first when needed (see connect_to_target). A Crew target runs the task as a turn in its own Crew chat (queued if it is busy); a workflow target runs the workflow with the payload. Unless notify=false, this chat is resumed with an [AUTO-NOTIFICATION] carrying the target's final result, failure, or timeout: after calling, tell the user what was sent and end your turn instead of waiting. Poll early with get_target_run. Reuse delivery_id only when retrying the same request.", map[string]interface{}{
		"type": "object", "required": []string{"target", "task"}, "properties": map[string]interface{}{
			"target":          targetSchema,
			"task":            map[string]interface{}{"type": "string", "description": "What the target should do, self-contained: it does not see this conversation."},
			"payload":         map[string]interface{}{"type": "object", "description": "Optional extra JSON input (within 1 MiB)."},
			"trigger_id":      map[string]interface{}{"type": "string", "description": "A trigger from connect_to_target's callable_triggers; omit to use the standard binding."},
			"delivery_id":     map[string]interface{}{"type": "string", "description": "Idempotency key; reuse only to retry the same request."},
			"notify":          map[string]interface{}{"type": "boolean", "description": "Resume this chat with the final result (default true)."},
			"timeout_minutes": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": int(triggerTargetMaxTimeout / time.Minute), "description": "How long to wait for the result before notifying a timeout (default 60)."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx, caller, target, err := setup(ctx, args)
		if err != nil {
			return "", err
		}
		task, _ := args["task"].(string)
		if strings.TrimSpace(task) == "" {
			return "", fmt.Errorf("task is required")
		}
		timeout, err := triggerTargetTimeout(args["timeout_minutes"])
		if err != nil {
			return "", err
		}
		notify := true
		if value, ok := args["notify"].(bool); ok {
			notify = value
		}
		triggerID, _ := args["trigger_id"].(string)
		triggerID = strings.TrimSpace(triggerID)
		if triggerID == "" {
			triggerID, _, err = api.connectTriggerTarget(ctx, userID, caller, target)
			if err != nil {
				return "", err
			}
		}
		body := map[string]interface{}{
			"task": strings.TrimSpace(task),
			"from": map[string]interface{}{"kind": caller.Stamp.Type, "name": caller.Label, "workspace_path": caller.Path},
		}
		if raw, ok := args["payload"]; ok && raw != nil {
			typed, ok := raw.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("payload must be a JSON object")
			}
			body["payload"] = typed
		}
		data, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("invalid payload")
		}
		deliveryID, _ := args["delivery_id"].(string)
		if strings.TrimSpace(deliveryID) == "" {
			deliveryID = "call-" + uuid.NewString()
		}
		var delivery internalTriggerDeliveryResult
		switch target.Kind {
		case triggerCallerCrew:
			delivery, err = api.productSchedules.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
				UserID: userID, ProfileID: target.CrewProfile, ProjectID: target.CrewID, TriggerID: triggerID,
				Caller: caller.Stamp, DeliveryID: deliveryID, Event: triggerTargetEvent, Payload: data, CallerLabel: caller.Label,
			})
		case triggerCallerWorkflow:
			delivery, err = api.scheduler.dispatchInternalWorkflowTrigger(ctx, internalWorkflowTriggerCall{
				WorkflowID: target.Manifest.ID, TriggerID: triggerID, Caller: caller.Stamp,
				DeliveryID: deliveryID, Event: triggerTargetEvent, Payload: data,
			})
		}
		if err != nil {
			return "", crewWorkflowRunError(err)
		}
		response := map[string]interface{}{
			"target": target.describe(), "status": delivery.Status, "run_id": delivery.RunID,
			"trigger_id": triggerID, "delivery_id": delivery.DeliveryID, "duplicate": delivery.Duplicate,
		}
		if notify {
			executionID, watchErr := api.startTriggerTargetWatch(parentReq, sessionID, userID, caller, target, triggerID, delivery.RunID, timeout)
			if watchErr != nil {
				response["auto_notification"] = "unavailable: " + watchErr.Error() + "; poll with get_target_run"
			} else {
				response["auto_notification"] = map[string]interface{}{"execution_id": executionID, "timeout_minutes": int(timeout / time.Minute)}
				response["next"] = "Tell the user what was sent and end your turn; the result arrives as an [AUTO-NOTIFICATION] in this chat."
			}
		}
		encoded, err := json.MarshalIndent(response, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}

	if err := register("send_to_target_run", "Send a follow-up message into a Crew run you started with call_target while it is still running, for extra details or a correction. The Crew receives it in its running chat: a live coding CLI takes it immediately, otherwise it runs right after the current turn. Workflow runs do not accept mid-run messages; start a new call_target instead.", map[string]interface{}{
		"type": "object", "required": []string{"target", "trigger_id", "run_id", "message"}, "properties": map[string]interface{}{
			"target":     targetSchema,
			"trigger_id": map[string]interface{}{"type": "string", "description": "trigger_id from call_target."},
			"run_id":     map[string]interface{}{"type": "string", "description": "run_id from call_target."},
			"message":    map[string]interface{}{"type": "string", "description": "The follow-up message, self-contained."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx, caller, target, err := setup(ctx, args)
		if err != nil {
			return "", err
		}
		triggerID, _ := args["trigger_id"].(string)
		runID, _ := args["run_id"].(string)
		message, _ := args["message"].(string)
		if strings.TrimSpace(triggerID) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(message) == "" {
			return "", fmt.Errorf("trigger_id, run_id and message are required")
		}
		if target.Kind != triggerCallerCrew {
			return "", fmt.Errorf("workflow runs do not accept mid-run messages; start a new call_target with the extra details")
		}
		result, err := api.sendToCrewTriggerRun(ctx, userID, caller, target, strings.TrimSpace(triggerID), strings.TrimSpace(runID), message)
		if err != nil {
			return "", err
		}
		encoded, err := json.MarshalIndent(result, "", "  ")
		return string(encoded), err
	}); err != nil {
		return err
	}

	return register("get_target_run", "Check a run started by call_target: status, whether it has finished, its final result (a Crew's final answer, or a workflow's step outputs), and while a Crew is still working, a short tail of what it is doing (recent_activity) without interrupting it.", map[string]interface{}{
		"type": "object", "required": []string{"target", "trigger_id", "run_id"}, "properties": map[string]interface{}{
			"target":     targetSchema,
			"trigger_id": map[string]interface{}{"type": "string", "description": "trigger_id from call_target."},
			"run_id":     map[string]interface{}{"type": "string", "description": "run_id from call_target."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx, caller, target, err := setup(ctx, args)
		if err != nil {
			return "", err
		}
		triggerID, _ := args["trigger_id"].(string)
		runID, _ := args["run_id"].(string)
		if strings.TrimSpace(triggerID) == "" || strings.TrimSpace(runID) == "" {
			return "", fmt.Errorf("trigger_id and run_id are required")
		}
		state, err := api.readTriggerTargetRun(ctx, userID, caller, target, strings.TrimSpace(triggerID), strings.TrimSpace(runID))
		if err != nil {
			return "", err
		}
		out := map[string]interface{}{
			"target": target.describe(), "run_id": strings.TrimSpace(runID), "status": state.Status,
			"terminal": state.Terminal, "failed": state.Failed, "result": truncateTriggerTargetResult(state.Result),
		}
		// What the Crew is doing right now, read from its session events
		// without interrupting it.
		if !state.Terminal && target.Kind == triggerCallerCrew {
			if activity := api.crewFunctionActivity(crewTargetRunSessionID(state)); activity != nil {
				out["recent_activity"] = activity
			}
		}
		encoded, err := json.MarshalIndent(out, "", "  ")
		return string(encoded), err
	})
}

// sendToCrewTriggerRun delivers a follow-up message from the caller into the
// Crew conversation running the caller's triggered task. It goes through the
// same /api/query handling as a person typing into that chat: a live coding
// CLI takes it as steering input, otherwise it waits in the durable turn queue
// and runs right after the current turn. Only crew_chat triggers qualify (an
// isolated trigger has no shared conversation to address), and only while the
// run is still running.
func (api *StreamingAPI) sendToCrewTriggerRun(ctx context.Context, userID string, caller triggerLinkCaller, target triggerTarget, triggerID, runID, message string) (map[string]interface{}, error) {
	status, err := api.productSchedules.getInternalProductTriggerRun(ctx, userID, target.CrewProfile, target.CrewID, triggerID, runID, caller.Stamp)
	if err != nil {
		return nil, crewWorkflowRunError(err)
	}
	if status.Terminal {
		return nil, fmt.Errorf("run %s already finished (%s); send a new task with call_target", runID, status.Status)
	}
	if strings.TrimSpace(status.SessionID) == "" || !strings.EqualFold(status.Status, "running") {
		return nil, fmt.Errorf("run %s has not started yet (%s); wait for it to start or put the details in a new call_target task", runID, status.Status)
	}
	profile, binding, _, trigger, err := api.productSchedules.findInternalProductTrigger(ctx, userID, target.CrewProfile, target.CrewID, triggerID)
	if err != nil {
		return nil, crewWorkflowRunError(err)
	}
	if strings.EqualFold(strings.TrimSpace(trigger.RunDestination), runDestinationIsolated) {
		return nil, fmt.Errorf("this trigger runs each call in its own isolated chat; mid-run messages need a crew_chat trigger")
	}
	ownerID := userID
	if owner, ok := crewProjectOwnerID(binding.WorkspacePath); ok {
		ownerID = owner
	}
	conversationBinding, err := resolveProductConversationBinding(ctx, ownerID, profile, target.CrewID)
	if err != nil {
		return nil, fmt.Errorf("Crew conversation is unavailable: %w", err)
	}
	conversation, err := defaultProductConversationRegistryStore().resolveOrCreate(ctx, ownerID, profile, conversationBinding, "")
	if err != nil {
		return nil, fmt.Errorf("Crew conversation is unavailable: %w", err)
	}
	if conversation.SessionID != status.SessionID {
		return nil, fmt.Errorf("the Crew's chat has moved to a new conversation since run %s started; send a new task with call_target", runID)
	}
	framed := fmt.Sprintf("[Follow-up from %s %q about its task in run %s]\n\n%s", caller.Stamp.Type, caller.Label, runID, strings.TrimSpace(message))
	req, err := queryRequestForAgentProfileChat(profile, AgentProfileChatRequest{Message: framed}, conversation)
	if err != nil {
		return nil, err
	}
	reqMap, err := queryRequestToMap(req)
	if err != nil {
		return nil, err
	}
	reqMap["triggered_by"] = "manual"
	reqMap["session_title"] = firstNonEmptyTrimmed(conversation.Title, profile.Name)
	body, err := json.Marshal(reqMap)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(internalBotRequestContext(context.WithoutCancel(ctx), ownerID, reqMap), http.MethodPost, "/api/query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Session-ID", conversation.SessionID)
	httpReq.Header.Set("X-User-ID", ownerID)
	httpReq.Header.Set("Idempotency-Key", "call-followup-"+uuid.NewString())
	recorder := httptest.NewRecorder()
	api.handleQuery(recorder, httpReq)
	if recorder.Code >= 400 {
		return nil, fmt.Errorf("the Crew did not accept the message (%d): %s", recorder.Code, strings.TrimSpace(recorder.Body.String()))
	}
	var response QueryResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &response)
	delivery := firstNonEmptyTrimmed(response.DeliveryStatus, response.Status, "accepted")
	return map[string]interface{}{
		"target": target.describe(), "run_id": runID, "delivery_status": delivery,
		"note": "Delivered into the Crew's running conversation. If it was queued, it runs right after the current turn and its answer appears in that Crew's chat; the auto-notification reports the triggered task's own final answer.",
	}, nil
}

// crewTriggerLinkCaller stamps a calling Crew from its own product manifest.
func crewTriggerLinkCaller(workspacePath string) func(context.Context) (triggerLinkCaller, error) {
	return func(ctx context.Context) (triggerLinkCaller, error) {
		stamp, err := crewWorkflowRunCaller(ctx, workspacePath)
		if err != nil {
			return triggerLinkCaller{}, err
		}
		label := path.Base(strings.TrimSuffix(strings.TrimSpace(workspacePath), "/"))
		if raw, exists, readErr := readFileFromWorkspace(ctx, strings.TrimSuffix(strings.TrimSpace(workspacePath), "/")+"/product.json"); readErr == nil && exists {
			var manifest productProjectManifest
			if json.Unmarshal([]byte(raw), &manifest) == nil {
				label = firstNonEmptyTrimmed(manifest.Identity.Name, manifest.Title, label)
			}
		}
		return triggerLinkCaller{Stamp: stamp, Label: label, Path: workspacePath}, nil
	}
}

// workflowTriggerLinkCaller stamps a calling workflow from its manifest.
func workflowTriggerLinkCaller(workspacePath string) func(context.Context) (triggerLinkCaller, error) {
	return func(ctx context.Context) (triggerLinkCaller, error) {
		manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
		if err != nil || !exists || manifest == nil || strings.TrimSpace(manifest.ID) == "" {
			return triggerLinkCaller{}, fmt.Errorf("workflow is unavailable or access denied")
		}
		if _, err := authorizeWorkflowContextPaths(ctx, []string{workspacePath}); err != nil {
			return triggerLinkCaller{}, fmt.Errorf("workflow is unavailable or access denied")
		}
		return triggerLinkCaller{
			Stamp: triggerCaller{Type: triggerCallerWorkflow, ID: strings.TrimSpace(manifest.ID)},
			Label: firstNonEmptyTrimmed(manifest.Label, workspacePath), Path: workspacePath,
		}, nil
	}
}
