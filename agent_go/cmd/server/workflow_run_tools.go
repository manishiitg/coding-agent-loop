package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// createWorkflowRunTools returns the tool definitions for workflow run management.
func createWorkflowRunTools() []llmtypes.Tool {
	return []llmtypes.Tool{
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "run_workflow",
				Description: "Run a full workflow execution in the background. Returns an execution ID immediately — you'll be notified when it completes. The workflow runs all steps for the specified group.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"workflow_path": map[string]interface{}{
							"type":        "string",
							"description": "Workspace-relative workflow path (e.g. 'Workflow/ICICI BANK PARSING')",
						},
						"group_name": map[string]interface{}{
							"type":        "string",
							"description": "Variable group name to run (e.g. 'icici', 'group-1'). Read the workflow's variables/variables.json to see available groups.",
						},
						"instructions": map[string]interface{}{
							"type":        "string",
							"description": "Optional context or instructions for the workflow agent (e.g. 'only process Q1 data', 'skip validation'). Passed as the user message to the workflow.",
						},
						"route_selections": map[string]interface{}{
							"type":                 "object",
							"description":          "Optional deterministic routing selections keyed by routing step ID. Each value may be a route_id or a unique next_step_id.",
							"additionalProperties": map[string]interface{}{"type": "string"},
						},
					},
					Required: []string{"workflow_path", "group_name"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "run_step",
				Description: "Run a single workflow step in the background. Returns an execution ID immediately — you'll be notified when it completes.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"workflow_path": map[string]interface{}{
							"type":        "string",
							"description": "Workspace-relative workflow path (e.g. 'Workflow/ICICI BANK PARSING')",
						},
						"step_id": map[string]interface{}{
							"type":        "string",
							"description": "Step ID from plan.json (e.g. 'step-parse-data', '1', 'step-1')",
						},
						"group_name": map[string]interface{}{
							"type":        "string",
							"description": "Variable group name to run (e.g. 'icici', 'group-1')",
						},
						"instructions": map[string]interface{}{
							"type":        "string",
							"description": "Optional context or instructions for the step agent (e.g. 'use the new API endpoint', 'focus on error handling').",
						},
						"route_selections": map[string]interface{}{
							"type":                 "object",
							"description":          "Optional deterministic routing selections keyed by routing step ID. Each value may be a route_id or a unique next_step_id.",
							"additionalProperties": map[string]interface{}{"type": "string"},
						},
					},
					Required: []string{"workflow_path", "step_id", "group_name"},
				},
			},
		},
		{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "stop_workflow_run",
				Description: "Stop a background workflow execution started by run_workflow or run_step. Use the agent_id returned by those tools.",
				Parameters: &llmtypes.Parameters{
					Type: "object",
					Properties: map[string]interface{}{
						"agent_id": map[string]interface{}{
							"type":        "string",
							"description": "The agent_id returned by run_workflow or run_step.",
						},
					},
					Required: []string{"agent_id"},
				},
			},
		},
	}
}

// createWorkflowRunExecutors returns the tool executors for workflow run management.
// api is needed to call startSessionInternal and access the background agent registry.
func createWorkflowRunExecutors(api *StreamingAPI) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	return map[string]func(ctx context.Context, args map[string]interface{}) (string, error){
		"run_workflow": func(ctx context.Context, args map[string]interface{}) (string, error) {
			return handleRunWorkflow(ctx, api, args)
		},
		"run_step": func(ctx context.Context, args map[string]interface{}) (string, error) {
			return handleRunStep(ctx, api, args)
		},
		"stop_workflow_run": func(ctx context.Context, args map[string]interface{}) (string, error) {
			return handleStopWorkflowRun(ctx, api, args)
		},
	}
}

func handleRunWorkflow(ctx context.Context, api *StreamingAPI, args map[string]interface{}) (string, error) {
	workflowPath, _ := args["workflow_path"].(string)
	if workflowPath == "" {
		return "", fmt.Errorf("workflow_path is required")
	}
	groupName, _ := args["group_name"].(string)
	if groupName == "" {
		return "", fmt.Errorf("group_name is required")
	}
	instructions, _ := args["instructions"].(string)
	routeSelections, err := parseRouteSelectionsArg(args["route_selections"])
	if err != nil {
		return "", err
	}

	return runWorkflowInternal(ctx, api, workflowPath, groupName, "", instructions, routeSelections)
}

func handleRunStep(ctx context.Context, api *StreamingAPI, args map[string]interface{}) (string, error) {
	workflowPath, _ := args["workflow_path"].(string)
	if workflowPath == "" {
		return "", fmt.Errorf("workflow_path is required")
	}
	stepID, _ := args["step_id"].(string)
	if stepID == "" {
		return "", fmt.Errorf("step_id is required")
	}
	groupName, _ := args["group_name"].(string)
	if groupName == "" {
		return "", fmt.Errorf("group_name is required")
	}
	instructions, _ := args["instructions"].(string)
	routeSelections, err := parseRouteSelectionsArg(args["route_selections"])
	if err != nil {
		return "", err
	}

	return runWorkflowInternal(ctx, api, workflowPath, groupName, stepID, instructions, routeSelections)
}

func parseRouteSelectionsArg(raw interface{}) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	rawMap, ok := raw.(map[string]interface{})
	if !ok {
		if typed, ok := raw.(map[string]string); ok {
			cleaned := make(map[string]string, len(typed))
			for stepID, routeID := range typed {
				stepID = strings.TrimSpace(stepID)
				routeID = strings.TrimSpace(routeID)
				if stepID == "" || routeID == "" {
					return nil, fmt.Errorf("route_selections entries must have non-empty step IDs and route values")
				}
				cleaned[stepID] = routeID
			}
			return cleaned, nil
		}
		return nil, fmt.Errorf("route_selections must be an object keyed by routing step ID")
	}

	cleaned := make(map[string]string, len(rawMap))
	for stepID, routeValue := range rawMap {
		stepID = strings.TrimSpace(stepID)
		value, ok := routeValue.(string)
		if !ok {
			return nil, fmt.Errorf("route_selections[%q] must be a string route_id or next_step_id", stepID)
		}
		value = strings.TrimSpace(value)
		if stepID == "" || value == "" {
			return nil, fmt.Errorf("route_selections entries must have non-empty step IDs and route values")
		}
		cleaned[stepID] = value
	}
	return cleaned, nil
}

func handleStopWorkflowRun(ctx context.Context, api *StreamingAPI, args map[string]interface{}) (string, error) {
	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}

	parentSessionID, _ := ctx.Value(virtualtools.BGAgentSessionIDKey).(string)
	if parentSessionID == "" {
		return "", fmt.Errorf("session ID not available")
	}

	registry := api.bgAgentRegistry
	if registry == nil {
		return "", fmt.Errorf("background agent registry not available")
	}

	agent := registry.Get(parentSessionID, agentID)
	if agent == nil {
		return "", fmt.Errorf("workflow run agent %s not found", agentID)
	}
	snap := agent.GetSnapshot()
	if snap.Kind != "workflow_run_tool" && snap.Metadata["type"] != "workflow_run" {
		return "", fmt.Errorf("agent %s is not a workflow run", agentID)
	}

	wfSessionID := snap.Metadata["workflow_session_id"]
	if wfSessionID == "" {
		return "", fmt.Errorf("workflow session ID missing for agent %s; cannot stop workflow execution safely", agentID)
	}

	if snap.Status != BGAgentRunning {
		result := map[string]interface{}{
			"agent_id":            agentID,
			"workflow_session_id": wfSessionID,
			"status":              string(snap.Status),
			"message":             fmt.Sprintf("Workflow run %s is already %s.", agentID, snap.Status),
		}
		resultJSON, _ := json.MarshalIndent(result, "", "  ")
		return string(resultJSON), nil
	}

	if err := registry.CancelAgent(parentSessionID, agentID); err != nil {
		return "", err
	}

	canceledExecutions := api.stopWorkflowRunSession(wfSessionID)

	result := map[string]interface{}{
		"agent_id":               agentID,
		"workflow_session_id":    wfSessionID,
		"status":                 "canceled",
		"canceled_execution_ids": canceledExecutions,
		"message":                fmt.Sprintf("Workflow run %s has been stopped.", agentID),
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

func (api *StreamingAPI) stopWorkflowRunSession(sessionID string) []string {
	if sessionID == "" {
		return nil
	}

	api.markSessionStopped(sessionID)

	api.agentCancelMux.Lock()
	if cancelFunc, exists := api.agentCancelFuncs[sessionID]; exists {
		cancelFunc()
		delete(api.agentCancelFuncs, sessionID)
	}
	api.agentCancelMux.Unlock()

	api.updateSessionStatus(sessionID, "stopped")
	api.setSessionBusy(sessionID, false)
	api.setSyntheticTurn(sessionID, false)

	api.bgAgentRegistry.CancelAll(sessionID)

	api.pendingMu.Lock()
	delete(api.pendingCompletions, sessionID)
	api.pendingMu.Unlock()

	api.lastQueryMu.Lock()
	delete(api.lastQueryRequests, sessionID)
	api.lastQueryMu.Unlock()

	api.sessionAgentsMux.Lock()
	delete(api.sessionAgents, sessionID)
	api.sessionAgentsMux.Unlock()

	api.completionLoopStartedMu.Lock()
	delete(api.completionLoopStarted, sessionID)
	api.completionLoopStartedMu.Unlock()

	api.workflowObjectiveMux.Lock()
	delete(api.workflowObjectives, sessionID)
	api.workflowObjectiveMux.Unlock()

	api.sessionQueryIDMux.Lock()
	queryIDs := append([]string(nil), api.sessionQueryIDs[sessionID]...)
	delete(api.sessionQueryIDs, sessionID)
	api.sessionQueryIDMux.Unlock()

	if len(queryIDs) > 0 {
		api.workflowOrchestratorContextMux.Lock()
		for _, qid := range queryIDs {
			if cancelFunc, exists := api.workflowOrchestratorContexts[qid]; exists {
				cancelFunc()
				delete(api.workflowOrchestratorContexts, qid)
			}
		}
		api.workflowOrchestratorContextMux.Unlock()

		api.activeWorkflowExecutionsMux.Lock()
		for _, qid := range queryIDs {
			delete(api.activeWorkflowExecutions, qid)
		}
		api.activeWorkflowExecutionsMux.Unlock()
		api.cancelTrackedExecutionsForSession(sessionID)
	}

	mcpagent.CloseHTTPSession(sessionID)
	api.cleanupBrowserSessions(sessionID)
	return queryIDs
}

// runWorkflowInternal is the shared implementation for both run_workflow and run_step.
// When stepID is empty, it runs the full workflow. When set, it runs a single step.
// instructions is optional user context passed as the query to the workflow agent.
func runWorkflowInternal(ctx context.Context, api *StreamingAPI, workflowPath, groupName, stepID, instructions string, routeSelections map[string]string) (string, error) {
	// Load manifest to get capabilities
	caps, found, err := LoadManifestForExecution(context.Background(), workflowPath)
	if err != nil {
		return "", fmt.Errorf("failed to load workflow manifest: %w", err)
	}
	if !found {
		return "", fmt.Errorf("workflow.json not found at %s", workflowPath)
	}

	// Read the manifest for the label
	manifest, _, _ := ReadWorkflowManifest(context.Background(), workflowPath)
	workflowLabel := workflowPath
	if manifest != nil && manifest.Label != "" {
		workflowLabel = manifest.Label
	}

	// Build the query — use instructions if provided, otherwise default
	query := fmt.Sprintf("Execute workflow: %s", workflowLabel)
	if instructions != "" {
		query = fmt.Sprintf("Execute workflow: %s\n\nInstructions: %s", workflowLabel, instructions)
	}

	// Build the request map (same format as scheduler uses).
	// Deprecated: agent_mode "workflow" runs the workflow headlessly without the
	// Workflow Builder chat; the supported path is the builder chat
	// ("workflow_phase"). Kept for backward compatibility.
	reqMap := map[string]interface{}{
		"query":                   query,
		"agent_mode":              "workflow", // deprecated: headless run; use "workflow_phase" (builder chat)
		"selected_folder":         workflowPath,
		"triggered_by":            "chat_tool",
		"servers":                 caps.SelectedServers,
		"selected_tools":          caps.SelectedTools,
		"selected_skills":         caps.SelectedSkills,
		"browser_mode":            caps.BrowserMode,
		"use_code_execution_mode": caps.UseCodeExecutionMode,
	}

	// Add global secrets from manifest
	if caps.SelectedGlobalSecretNames != nil {
		reqMap["selected_global_secrets"] = caps.SelectedGlobalSecretNames
	}

	// Add LLM config if present
	if caps.LLMConfig != nil && caps.LLMConfig.Provider != "" && caps.LLMConfig.ModelID != "" {
		reqMap["llm_config"] = map[string]interface{}{
			"primary": map[string]interface{}{
				"provider": caps.LLMConfig.Provider,
				"model_id": caps.LLMConfig.ModelID,
			},
		}
	}

	// Execution options
	execOpts := map[string]interface{}{
		"run_mode":            "use_same_run",
		"selected_run_folder": "iteration-0",
		"execution_strategy":  "start_from_beginning",
		"enabled_group_names": []string{groupName},
	}
	if stepID != "" {
		execOpts["step_id"] = stepID
	}
	if len(routeSelections) > 0 {
		execOpts["route_selections"] = routeSelections
	}
	reqMap["execution_options"] = execOpts

	// Get session ID and user ID from context/session
	sessionID := ""
	if sid, ok := ctx.Value(virtualtools.BGAgentSessionIDKey).(string); ok {
		sessionID = sid
	}
	userID := "default"
	if sessionID != "" {
		api.activeSessionsMux.RLock()
		if sess, ok := api.activeSessions[sessionID]; ok && sess.UserID != "" {
			userID = sess.UserID
		}
		api.activeSessionsMux.RUnlock()
	}

	// Load user-stored secrets from manifest selection
	if len(caps.SelectedSecrets) > 0 {
		decryptedSecrets := api.loadSelectedSecrets(context.Background(), userID, workflowPath, caps.SelectedSecrets)
		if len(decryptedSecrets) > 0 {
			secretsList := make([]map[string]string, len(decryptedSecrets))
			for i, s := range decryptedSecrets {
				secretsList[i] = map[string]string{"name": s.Name, "value": s.Value}
			}
			reqMap["decrypted_secrets"] = secretsList
		}
	}

	// Generate a unique session ID for this workflow run
	wfSessionID := fmt.Sprintf("wfrun_%d", time.Now().UnixNano())

	// If this workflow is being launched from an existing builder chat session,
	// register the mapping so human_input steps can route questions back to the
	// builder instead of showing the blocking popup UI.
	if sessionID != "" {
		virtualtools.RegisterParentChat(wfSessionID, &virtualtools.ParentChatContext{
			SessionID:    sessionID,
			UserID:       userID,
			WorkflowPath: workflowPath,
			GroupName:    groupName,
		})
	}

	// Use the API's background agent registry directly (not from context — context has the querier wrapper)
	registry := api.bgAgentRegistry
	if registry == nil {
		virtualtools.UnregisterParentChat(wfSessionID)
		return "", fmt.Errorf("background agent registry not available")
	}

	// Create a cancellable context for the workflow run
	runCtx, cancel := context.WithCancel(context.Background())

	// Build the agent name
	agentName := fmt.Sprintf("Workflow: %s", workflowLabel)
	if stepID != "" {
		agentName = fmt.Sprintf("Step: %s (%s)", stepID, workflowLabel)
	}
	logCtx := newServerLogContext(workflowPath, groupName, "workflow", userID, "", wfSessionID)

	// Register in the background agent registry so list_agents/terminate_agent see it
	agentID := registry.NextID(agentName)
	parentExecutionID := ""
	if parentID, ok := ctx.Value(virtualtools.BackgroundAgentIDKey).(string); ok {
		parentExecutionID = parentID
	}
	bgAgent := &BackgroundAgent{
		ID:                agentID,
		ParentExecutionID: parentExecutionID,
		Name:              agentName,
		SessionID:         sessionID,
		Instruction: fmt.Sprintf("workflow_path=%s group=%s step=%s",
			workflowPath, groupName, stepID),
		Kind:      "workflow_run_tool",
		Status:    BGAgentRunning,
		CreatedAt: time.Now(),
		cancel:    cancel,
		Metadata: map[string]string{
			"type":                "workflow_run",
			"workflow_path":       workflowPath,
			"group_name":          groupName,
			"step_id":             stepID,
			"workflow_session_id": wfSessionID,
		},
	}
	registry.Register(sessionID, bgAgent)
	registry.GetNotificationChannel(sessionID)
	api.completionLoopStartedMu.Lock()
	if api.completionLoopStarted == nil {
		api.completionLoopStarted = make(map[string]bool)
	}
	if !api.completionLoopStarted[sessionID] {
		api.completionLoopStarted[sessionID] = true
		go api.backgroundCompletionLoop(sessionID)
	}
	api.completionLoopStartedMu.Unlock()
	api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_started", map[string]interface{}{
		"agent_id": agentID,
		"name":     agentName,
		"step_id":  stepID,
	})
	api.notifyBackgroundAgentStarted(sessionID, agentID)

	// Run the workflow in background
	go func() {
		logfWithContext(logCtx, "[WORKFLOW_RUN_TOOL] Starting %s (agent=%s session=%s)", agentName, agentID, wfSessionID)

		runErr := api.startSessionInternal(runCtx, reqMap, wfSessionID, userID, nil)

		now := time.Now()
		bgAgent.mu.Lock()
		if bgAgent.Status == BGAgentCanceled {
			logfWithContext(logCtx, "[WORKFLOW_RUN_TOOL] %s canceled", agentName)
		} else if runErr != nil {
			bgAgent.CompletedAt = &now
			bgAgent.Status = BGAgentFailed
			bgAgent.Error = runErr.Error()
			logfWithContext(logCtx, "[WORKFLOW_RUN_TOOL] %s failed: %v", agentName, runErr)
		} else {
			bgAgent.CompletedAt = &now
			bgAgent.Status = BGAgentCompleted
			bgAgent.Result = fmt.Sprintf("Workflow completed. Check %s/runs/iteration-0/%s/ for results.", workflowPath, groupName)
			logfWithContext(logCtx, "[WORKFLOW_RUN_TOOL] %s completed", agentName)
		}
		bgAgent.mu.Unlock()

		// Clean up the parent-chat mapping for this workflow run. The installed
		// SpawnListener (if any) is notified automatically and will stop
		// mirroring this child's events to the parent chat.
		virtualtools.UnregisterParentChat(wfSessionID)

		// Notify the orchestrator that this agent finished
		registry.NotifyCompletion(sessionID, agentID)
	}()

	result := map[string]interface{}{
		"agent_id":      agentID,
		"status":        "started",
		"workflow_path": workflowPath,
		"group_name":    groupName,
		"message":       fmt.Sprintf("%s started in background. You'll be notified when it completes.", agentName),
	}
	if stepID != "" {
		result["step_id"] = stepID
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}
