package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	storeevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

// externalRunToolHints documents the heavily used proxied run tools for the
// catalog. Display-only: membership comes from product.yaml run.tools, and a
// tool without a hint here still proxies with the generic description.
var externalRunToolHints = map[string]string{
	"execute_step":       "Start one workflow step in the background. Arguments: step_id (required; plan step ID or positional like '1'), group_name, human_input, script_parameters (object), tier (high|medium|low).",
	"run_full_workflow":  "Run all steps end-to-end for one variable group. Arguments: group_name (required), human_inputs (object keyed by step ID), route_selections (object keyed by routing step ID).",
	"run_in_background":  "Start a background agent task. Arguments: name (required), instruction (required), access_mode, agent_type, completion_mode.",
	"send_step_message":  "Steer a live execution. Arguments: execution_id (required), message (required).",
	"stop_step":          "Stop one execution. Arguments: execution_id (required).",
	"stop_all_executions": "Stop all running executions in the run session. No arguments.",
	"query_step":         "One-off live status check for a tracked execution. Arguments: step_id or execution_id.",
}

// externalRunProxyTool builds the catalog entry for a run.tools name with no
// native implementation. The schema carries only the external routing keys;
// every other argument passes through to the run-mode tool call verbatim,
// flat, so a future native implementation keeps the same call shape.
func externalRunProxyTool(name string) externalTool {
	description := "Run-mode tool `" + name + "` (requires the runs:execute scope): start a new pinned Run-mode session, or continue session_id, and instruct it to call this tool with the remaining arguments as its parameters. Poll run_status for completion."
	if hint, ok := externalRunToolHints[name]; ok {
		description += " " + hint
	}
	return externalTool{
		Name:        name,
		Description: description,
		executes:    true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"workflow_id": externalString("Workflow ID returned by list_workflows. Never a filesystem path."),
				"session_id":  map[string]any{"type": "string", "description": "Existing run session ID to continue. Omit to start a new run session."},
			},
			"required":             []any{"workflow_id"},
			"additionalProperties": true,
		},
	}
}

// externalRunProxy adapts one run-mode tool call to the existing chat
// runtime: it synthesizes a pinned Run-mode session (or continues the
// caller's own) and sends a single instruction turn invoking the tool. The
// external router has already authorized workflow access; session ownership
// and its workflow binding must additionally match, even for admins.
func (api *StreamingAPI) externalRunProxy(w http.ResponseWriter, r *http.Request, name string, args map[string]any, workflow DiscoveredWorkflow) {
	userID := strings.TrimSpace(GetUserIDFromContext(r.Context()))
	if userID == "" {
		externalError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	sessionID, err := externalBuilderString(args, "session_id")
	if err != nil || (sessionID != "" && sanitizeChatHistorySessionID(sessionID) != sessionID) {
		externalError(w, http.StatusBadRequest, "invalid_arguments", "Invalid session_id")
		return
	}
	claims := GetUserFromContext(r.Context())
	if claims.AccessToken != nil && sessionID != "" && !strings.HasPrefix(sessionID, accessTokenSessionPrefix(claims)) {
		externalError(w, http.StatusNotFound, "session_not_found", "This access token does not own that run session.")
		return
	}
	var persisted bool
	if sessionID != "" {
		if _, persisted, err = api.externalBuilderSession(r, workflow.WorkspacePath, sessionID); err != nil {
			externalError(w, http.StatusNotFound, "session_not_found", "Run session not found")
			return
		}
	}
	if workflow.Manifest == nil || workflow.Manifest.ID == "" {
		externalError(w, http.StatusConflict, "workflow_unavailable", "Workflow manifest is unavailable")
		return
	}
	if sessionID == "" {
		sessionID = uuid.NewString()
		if claims.AccessToken != nil {
			sessionID = accessTokenSessionPrefix(claims) + sessionID
		}
	}
	query := QueryRequest{
		Query: externalRunInstruction(name, args), AgentMode: "workflow_phase", PhaseID: "workflow-builder",
		PresetQueryID: workflow.Manifest.ID, SelectedFolder: workflow.WorkspacePath,
		PinRunMode: true, TriggeredBy: "external", SessionTitle: "External run: " + name,
		ExecutionOptions: &ExecutionOptions{WorkshopMode: "run"},
	}
	if persisted {
		query.RestoredConversationSessionID = sessionID
	}
	body, marshalErr := json.Marshal(query)
	if marshalErr != nil {
		externalError(w, http.StatusInternalServerError, "encoding_failed", "Cannot encode run request")
		return
	}
	forward := r.Clone(r.Context())
	forward.Method = http.MethodPost
	forward.URL.Path = "/api/query"
	forward.URL.RawQuery = ""
	forward.Header.Set("X-Session-ID", sessionID)
	forward.Header.Set("Content-Type", "application/json")
	forward.Body = io.NopCloser(bytes.NewReader(body))
	forward.ContentLength = int64(len(body))
	// handleQuery owns the normal asynchronous turn lifecycle and returns
	// its actual status/error. Do not wrap it in another background job.
	if claims.AccessToken != nil {
		api.watchAccessTokenSession(claims, sessionID, workflow.WorkspacePath, workflowAccessForManifest(claims, workflow.Manifest))
	}
	externalBuilderForward(w, forward, api.handleQuery)
	if claims.AccessToken != nil {
		// Close revocation/expiry races during setup, which may outlive the HTTP auth check.
		checkCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		allowed := accessTokenSessionAllowed(checkCtx, accessTokenSession{claims.AccessToken.ID, sessionID, workflow.WorkspacePath, workflowAccessForManifest(claims, workflow.Manifest)})
		cancel()
		if !allowed {
			api.cancelSessionRuntimeWork(sessionID, "access token no longer authorized", runtimePhaseCanceled)
		}
	}
}

// externalRunInstruction renders the single turn a proxied call sends. The
// tool's own arguments pass through verbatim; the scope guard is
// belt-and-braces behind the Run-mode tool surface, which withholds
// authoring tools regardless of what the prose says.
func externalRunInstruction(name string, args map[string]any) string {
	params := map[string]any{}
	for key, value := range args {
		if key == "workflow_id" || key == "session_id" {
			continue
		}
		params[key] = value
	}
	var sb strings.Builder
	sb.WriteString("Call the run-mode tool \"" + name + "\" now")
	if len(params) == 0 {
		sb.WriteString(" with no arguments")
	} else {
		raw, _ := json.Marshal(params)
		sb.WriteString(" with these arguments: " + string(raw))
	}
	sb.WriteString(". Use this tool for the requested action (you may read workflow files to resolve its arguments); do not modify the plan, configuration, schedules, secrets, or any file outside this run's own execution outputs. Report the tool's result, including any execution_id, run folder, or schedule run reference it returns.")
	return sb.String()
}

// externalRunCall serves the JSON-direct run operations: the run_status
// poller plus the native run.tools implementations.
func (api *StreamingAPI) externalRunCall(w http.ResponseWriter, r *http.Request, name string, args map[string]any, workflow DiscoveredWorkflow) {
	switch name {
	case "run_status":
		api.externalRunStatus(w, r, args, workflow)
	case "list_executions":
		executions := api.listRunningWorkflowExecutionsForWorkspace(workflow.WorkspacePath)
		if executions == nil {
			executions = []ActiveWorkflowExecution{}
		}
		externalJSON(w, map[string]any{"workflow_id": workflow.Manifest.ID, "executions": executions})
	case "list_schedules":
		api.externalListSchedules(w, r, workflow)
	case "get_schedule_runs":
		api.externalScheduleRuns(w, r, args, workflow)
	case "trigger_schedule":
		api.externalTriggerSchedule(w, r, args, workflow)
	default:
		externalError(w, http.StatusBadRequest, "unknown_tool", "Unknown run operation")
	}
}

// externalRunStatus mirrors the builder status poller for run sessions: one
// bounded event page plus session state and this session's executions. Run
// and Builder share the workflow-builder phase, so the same ownership and
// workflow-binding lookup applies.
func (api *StreamingAPI) externalRunStatus(w http.ResponseWriter, r *http.Request, args map[string]any, workflow DiscoveredWorkflow) {
	sessionID, err := externalBuilderString(args, "session_id")
	if err != nil || sessionID == "" || sanitizeChatHistorySessionID(sessionID) != sessionID {
		externalError(w, http.StatusBadRequest, "invalid_arguments", "Invalid session_id")
		return
	}
	claims := GetUserFromContext(r.Context())
	if claims.AccessToken != nil && !strings.HasPrefix(sessionID, accessTokenSessionPrefix(claims)) {
		externalError(w, http.StatusNotFound, "session_not_found", "This access token does not own that run session.")
		return
	}
	active, persisted, err := api.externalBuilderSession(r, workflow.WorkspacePath, sessionID)
	if err != nil {
		externalError(w, http.StatusNotFound, "session_not_found", "Run session not found")
		return
	}
	since, sinceErr := externalBuilderInt(args, "since_index", -1, -1, int(^uint(0)>>1))
	limit, limitErr := externalBuilderInt(args, "limit", 50, 1, 200)
	if sinceErr != nil || limitErr != nil {
		externalError(w, http.StatusBadRequest, "invalid_arguments", "since_index must be an integer >= -1 and limit an integer between 1 and 200")
		return
	}
	page := storeevents.ForwardEventPage{GetEventsResult: storeevents.GetEventsResult{Events: []storeevents.Event{}, LastProcessedIndex: -1}}
	if api.eventStore != nil {
		page = api.eventStore.GetForwardEventPage(sessionID, since, limit)
	}
	response := map[string]interface{}{
		"session_id": sessionID, "events": page.Events, "has_more": page.HasMore,
		"last_processed_index": page.LastProcessedIndex, "cursor_reset": page.CursorReset,
		"first_available_index": page.FirstAvailableIndex,
		"history_available":     persisted, "events_available": page.Exists,
	}
	pending := virtualtools.GetHumanFeedbackStore().PendingForSession(sessionID, time.Now())
	response["pending_inputs"] = pending
	response["needs_user_input"] = len(pending) > 0
	executions := []ActiveWorkflowExecution{}
	for _, execution := range api.listRunningWorkflowExecutionsForWorkspace(workflow.WorkspacePath) {
		if execution.SessionID == sessionID {
			executions = append(executions, execution)
		}
	}
	response["executions"] = executions
	if active != nil {
		response["session_status"] = active.Status
		response["workshop_mode"] = active.WorkshopMode
		response["can_steer"] = api.canSteerSession(sessionID)
		response["busy"] = api.isSessionBusy(sessionID)
		response["has_running_background_agents"] = api.bgAgentRegistry != nil && api.bgAgentRegistry.HasRunningAgents(sessionID)
		if api.runtimeCoordinator != nil {
			if state, ok := api.runtimeCoordinator.Snapshot(sessionID); ok {
				response["runtime_state"] = state
				response["display_status"] = sessionDisplayStatusFromRuntime(state).Status
			}
		}
		response["needs_user_input"] = len(pending) > 0 || active.NeedsUserInput
		if active.NeedsUserInput {
			response["waiting_event_type"] = active.WaitingEventType
			response["waiting_message"] = active.WaitingMessage
		}
	} else {
		// A durable transcript alone cannot establish the outcome of the
		// last turn after a restart. Do not incorrectly report completion.
		response["session_status"] = "inactive"
	}
	externalJSON(w, response)
}

// externalScheduleSummary projects the schedule fields an external caller
// needs to choose a trigger target. Payloads and webhook delivery config
// stay server-side.
type externalScheduleSummary struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ScheduleType    string   `json:"schedule_type"`
	CronExpression  string   `json:"cron_expression,omitempty"`
	Timezone        string   `json:"timezone,omitempty"`
	Enabled         bool     `json:"enabled"`
	GroupNames      []string `json:"group_names,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	WorkshopMode    string   `json:"workshop_mode,omitempty"`
	PulseMode       string   `json:"pulse_mode,omitempty"`
	CalendarItems   int      `json:"calendar_items,omitempty"`
	ConcurrencyMode string   `json:"concurrency_mode,omitempty"`
}

func (api *StreamingAPI) externalListSchedules(w http.ResponseWriter, r *http.Request, workflow DiscoveredWorkflow) {
	manifest, found, err := ReadWorkflowManifest(r.Context(), workflow.WorkspacePath)
	if err != nil || !found || manifest == nil {
		externalError(w, http.StatusBadGateway, "workspace_unavailable", "Workflow manifest is unavailable.")
		return
	}
	schedules := []externalScheduleSummary{}
	for _, sched := range manifest.Schedules {
		schedules = append(schedules, externalScheduleSummary{
			ID: sched.ID, Name: sched.Name, ScheduleType: scheduleTypeOrDefault(sched.ScheduleType),
			CronExpression: sched.CronExpression, Timezone: sched.Timezone, Enabled: sched.Enabled,
			GroupNames: sched.GroupNames, Mode: scheduleModeOrDefault(sched.Mode),
			WorkshopMode: sched.WorkshopMode, PulseMode: manifest.EffectivePulseMode(sched),
			CalendarItems: len(sched.CalendarItems), ConcurrencyMode: sched.ConcurrencyMode,
		})
	}
	externalJSON(w, map[string]any{"workflow_id": workflow.Manifest.ID, "schedules": schedules})
}

func (api *StreamingAPI) externalScheduleRuns(w http.ResponseWriter, r *http.Request, args map[string]any, workflow DiscoveredWorkflow) {
	scheduleID := externalArg(args, "schedule_id")
	manifest, found, err := ReadWorkflowManifest(r.Context(), workflow.WorkspacePath)
	if err != nil || !found || manifest == nil {
		externalError(w, http.StatusBadGateway, "workspace_unavailable", "Workflow manifest is unavailable.")
		return
	}
	known := false
	for _, sched := range manifest.Schedules {
		if sched.ID == scheduleID {
			known = true
			break
		}
	}
	if !known {
		externalError(w, http.StatusNotFound, "schedule_not_found", "Schedule does not exist on this workflow.")
		return
	}
	limit := externalInt(args, "limit", 50)
	offset := externalInt(args, "offset", 0)
	if limit < 1 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	runs, total, err := ListScheduleRuns(r.Context(), workflow.WorkspacePath, scheduleID, limit, offset)
	if err != nil {
		externalError(w, http.StatusBadGateway, "workspace_unavailable", "Schedule run history is unavailable.")
		return
	}
	if runs == nil {
		runs = []ScheduleRunEntry{}
	}
	end := offset + len(runs)
	externalJSON(w, map[string]any{"workflow_id": workflow.Manifest.ID, "schedule_id": scheduleID, "runs": runs, "total": total, "next_offset": end, "has_more": end < total})
}

func (api *StreamingAPI) externalTriggerSchedule(w http.ResponseWriter, r *http.Request, args map[string]any, workflow DiscoveredWorkflow) {
	if api.scheduler == nil {
		externalError(w, http.StatusServiceUnavailable, "scheduler_unavailable", "The scheduler is not running on this server.")
		return
	}
	runID, err := api.scheduler.TriggerNow(workflow.WorkspacePath, externalArg(args, "schedule_id"))
	if err != nil {
		message := err.Error()
		switch {
		case strings.Contains(message, "not found"):
			externalError(w, http.StatusNotFound, "schedule_not_found", "Schedule does not exist on this workflow.")
		case strings.Contains(message, "disabled"):
			externalError(w, http.StatusConflict, "schedule_disabled", message)
		case strings.Contains(message, "authenticated delivery") || strings.Contains(message, "not a webhook"):
			externalError(w, http.StatusBadRequest, "invalid_arguments", message)
		default:
			externalError(w, http.StatusBadGateway, "trigger_failed", message)
		}
		return
	}
	status := "started"
	if runID == "queued" {
		status = "queued"
	}
	externalJSON(w, map[string]any{"workflow_id": workflow.Manifest.ID, "schedule_id": externalArg(args, "schedule_id"), "run_id": runID, "status": status})
}
