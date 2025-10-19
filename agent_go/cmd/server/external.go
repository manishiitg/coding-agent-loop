package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/events"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/pkg/database"
	unifiedevents "mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/orchestrator"
	orchtypes "mcp-agent/agent_go/pkg/orchestrator/types"

	eventbridge "mcp-agent/agent_go/cmd/server/event_bridge"
	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
)

// --- EXTERNAL API TYPES ---

// ExecutePresetRequest represents a request to execute a preset
type ExecutePresetRequest struct {
	PresetID                  string                            `json:"preset_id"`
	Phase                     string                            `json:"phase,omitempty"`                       // Optional: for workflow mode
	OrchestratorExecutionMode orchtypes.ExecutionMode           `json:"orchestrator_execution_mode,omitempty"` // Required for orchestrator mode
	WorkflowSelectedOptions   *database.WorkflowSelectedOptions `json:"workflow_selected_options,omitempty"`   // Required for workflow mode
}

// ExecutePresetResponse represents the response for preset execution
type ExecutePresetResponse struct {
	SessionID  string `json:"session_id"`
	ObserverID string `json:"observer_id"`
	PresetID   string `json:"preset_id"`
	AgentMode  string `json:"agent_mode"`
	Phase      string `json:"phase,omitempty"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	// Add validation info
	ValidatedParameters map[string]interface{} `json:"validated_parameters,omitempty"`
}

// CancelExecutionRequest represents a request to cancel an execution
type CancelExecutionRequest struct {
	SessionID string `json:"session_id"`
}

// CancelExecutionResponse represents the response for execution cancellation
type CancelExecutionResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

// --- EXTERNAL API HANDLERS ---

// handleExecutePreset handles external preset execution requests
func (api *StreamingAPI) handleExecutePreset(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Parse request body
	var req ExecutePresetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate preset_id is provided
	if req.PresetID == "" {
		http.Error(w, "preset_id is required", http.StatusBadRequest)
		return
	}

	// Fetch preset from database
	preset, err := api.chatDB.GetPresetQuery(r.Context(), req.PresetID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, fmt.Sprintf("Preset not found: %s", req.PresetID), http.StatusNotFound)
		} else {
			http.Error(w, fmt.Sprintf("Failed to fetch preset: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Validate agent mode is workflow or orchestrator
	if preset.AgentMode != "workflow" && preset.AgentMode != "orchestrator" {
		http.Error(w, fmt.Sprintf("Invalid agent mode for external API: %s. Only 'workflow' and 'orchestrator' are supported", preset.AgentMode), http.StatusBadRequest)
		return
	}

	// Validate required parameters based on agent mode
	if preset.AgentMode == "orchestrator" {
		if req.OrchestratorExecutionMode == "" {
			http.Error(w, "orchestrator_execution_mode is required for orchestrator presets", http.StatusBadRequest)
			return
		}
		// Validate execution mode is valid
		if !req.OrchestratorExecutionMode.IsValid() {
			http.Error(w, fmt.Sprintf("Invalid orchestrator_execution_mode: %s. Must be 'sequential_execution' or 'parallel_execution'", req.OrchestratorExecutionMode), http.StatusBadRequest)
			return
		}
		log.Printf("[EXTERNAL API] Validated orchestrator execution mode: %s", req.OrchestratorExecutionMode.String())
	} else if preset.AgentMode == "workflow" {
		if req.WorkflowSelectedOptions == nil {
			http.Error(w, "workflow_selected_options is required for workflow presets", http.StatusBadRequest)
			return
		}
		// Validate workflow selected options structure
		if len(req.WorkflowSelectedOptions.Selections) == 0 {
			http.Error(w, "workflow_selected_options.selections cannot be empty", http.StatusBadRequest)
			return
		}
		// Validate that execution strategy is provided
		hasExecutionStrategy := false
		for _, selection := range req.WorkflowSelectedOptions.Selections {
			if selection.Group == "execution_strategy" {
				hasExecutionStrategy = true
				break
			}
		}
		if !hasExecutionStrategy {
			http.Error(w, "workflow_selected_options must include an execution_strategy selection", http.StatusBadRequest)
			return
		}
		log.Printf("[EXTERNAL API] Validated workflow selected options with %d selections", len(req.WorkflowSelectedOptions.Selections))
	}

	// Determine execution phase
	executionPhase := ""
	if preset.AgentMode == "workflow" {
		// For workflow mode, determine phase
		if req.Phase != "" {
			// Validate provided phase
			validPhases := []string{
				database.WorkflowStatusPreVerification,
				database.WorkflowStatusPostVerification,
				database.WorkflowStatusPostVerificationTodoRefinement,
			}
			isValid := false
			for _, validPhase := range validPhases {
				if req.Phase == validPhase {
					isValid = true
					break
				}
			}
			if !isValid {
				http.Error(w, fmt.Sprintf("Invalid phase: %s. Valid phases: %v", req.Phase, validPhases), http.StatusBadRequest)
				return
			}
			executionPhase = req.Phase
		} else {
			// No phase provided - check database for existing workflow
			workflow, err := api.chatDB.GetWorkflowByPresetQueryID(r.Context(), req.PresetID)
			if err == nil && workflow != nil {
				executionPhase = workflow.WorkflowStatus
			} else {
				// Default to pre-verification if no workflow exists
				executionPhase = database.WorkflowStatusPreVerification
			}
		}
	}
	// For orchestrator mode, phase is ignored (runs all phases sequentially)

	// Generate unique session ID
	sessionID := fmt.Sprintf("external_%s_%d", req.PresetID, time.Now().UnixNano())

	// Register observer for polling
	observer := api.observerManager.RegisterObserver(sessionID)
	observerID := observer.ID

	log.Printf("[EXTERNAL API] Created session %s for preset %s (observer: %s, mode: %s, phase: %s)",
		sessionID, req.PresetID, observerID, preset.AgentMode, executionPhase)

	// Create chat session in database
	chatSession, err := api.chatDB.CreateChatSession(r.Context(), &database.CreateChatSessionRequest{
		SessionID:     sessionID,
		Title:         preset.Label, // Use preset label as title
		AgentMode:     preset.AgentMode,
		PresetQueryID: preset.ID,
	})
	if err != nil {
		log.Printf("[EXTERNAL API] Failed to create chat session: %v", err)
		// Continue without chat session - events won't be stored but query can proceed
	} else {
		log.Printf("[EXTERNAL API] Created chat session: %s", chatSession.ID)
	}

	// Track active session
	api.trackActiveSession(sessionID, observerID, preset.AgentMode, preset.Query)

	// Create validated parameters info
	validatedParams := make(map[string]interface{})
	if preset.AgentMode == "orchestrator" {
		validatedParams["orchestrator_execution_mode"] = req.OrchestratorExecutionMode.String()
		validatedParams["execution_strategy"] = req.OrchestratorExecutionMode.GetLabel()
	} else if preset.AgentMode == "workflow" {
		validatedParams["workflow_selected_options_count"] = len(req.WorkflowSelectedOptions.Selections)
		validatedParams["phase_id"] = req.WorkflowSelectedOptions.PhaseID
		// Extract execution strategy from selections
		for _, selection := range req.WorkflowSelectedOptions.Selections {
			if selection.Group == "execution_strategy" {
				validatedParams["execution_strategy"] = selection.OptionValue
				break
			}
		}
	}

	// Return immediate response
	response := ExecutePresetResponse{
		SessionID:           sessionID,
		ObserverID:          observerID,
		PresetID:            req.PresetID,
		AgentMode:           preset.AgentMode,
		Phase:               executionPhase,
		Status:              "started",
		Message:             "Execution started successfully",
		ValidatedParameters: validatedParams,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}

	// Launch execution in background
	go api.executePresetInBackground(sessionID, observerID, preset, executionPhase, &req)
}

// executePresetInBackground executes the preset in the background
func (api *StreamingAPI) executePresetInBackground(sessionID, observerID string, preset *database.PresetQuery, executionPhase string, req *ExecutePresetRequest) {
	// Record start time
	startTime := time.Now()

	// Initialize Langfuse tracing
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}
	tracer := observability.GetTracer(tracingProvider)
	traceName := fmt.Sprintf("external-preset-execution: %s", preset.Label)
	traceID := tracer.StartTrace(traceName, map[string]interface{}{
		"preset_id":   preset.ID,
		"agent_mode":  preset.AgentMode,
		"phase":       executionPhase,
		"session_id":  sessionID,
		"observer_id": observerID,
	})

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	// Parse preset servers
	var selectedServers []string
	if preset.SelectedServers != "" {
		if err := json.Unmarshal([]byte(preset.SelectedServers), &selectedServers); err != nil {
			log.Printf("[EXTERNAL API] Failed to parse selected servers: %v", err)
			selectedServers = []string{"all"}
		}
	} else {
		selectedServers = []string{"all"}
	}

	// Parse preset LLM config
	var llmConfig *orchestrator.LLMConfig
	var provider, model string
	if len(preset.LLMConfig) > 0 {
		var presetLLMConfig database.PresetLLMConfig
		if err := json.Unmarshal(preset.LLMConfig, &presetLLMConfig); err != nil {
			log.Printf("[EXTERNAL API] Failed to parse LLM config: %v", err)
		} else {
			provider = presetLLMConfig.Provider
			model = presetLLMConfig.ModelID
			llmConfig = &orchestrator.LLMConfig{
				Provider: provider,
				ModelID:  model,
			}
		}
	}

	// Use defaults if not provided
	if provider == "" {
		provider = api.config.Provider
	}
	if model == "" {
		model = api.config.ModelID
	}

	log.Printf("[EXTERNAL API] Executing preset: mode=%s, provider=%s, model=%s, servers=%v",
		preset.AgentMode, provider, model, selectedServers)

	// Execute based on agent mode
	if preset.AgentMode == "orchestrator" {
		api.executeOrchestratorPreset(ctx, sessionID, observerID, preset, selectedServers, provider, model, llmConfig, traceID, tracer, startTime, req)
	} else if preset.AgentMode == "workflow" {
		api.executeWorkflowPreset(ctx, sessionID, observerID, preset, executionPhase, selectedServers, provider, model, llmConfig, traceID, tracer, startTime, req)
	}
}

// executeOrchestratorPreset executes an orchestrator preset
func (api *StreamingAPI) executeOrchestratorPreset(
	ctx context.Context,
	sessionID, observerID string,
	preset *database.PresetQuery,
	selectedServers []string,
	provider, model string,
	llmConfig *orchestrator.LLMConfig,
	traceID observability.TraceID,
	tracer observability.Tracer,
	startTime time.Time,
	req *ExecutePresetRequest,
) {
	log.Printf("[EXTERNAL API] Starting orchestrator execution for session %s", sessionID)

	// Create orchestrator agent event bridge
	orchestratorAgentEventBridge := &eventbridge.OrchestratorAgentEventBridge{
		BaseEventBridge: &eventbridge.BaseEventBridge{
			EventStore:      api.eventStore,
			ObserverManager: api.observerManager,
			ObserverID:      observerID,
			SessionID:       sessionID,
			Logger:          api.logger,
			ChatDB:          api.chatDB,
			BridgeName:      "orchestrator_agent",
		},
	}

	// Create selected options from user request
	selectedOptions := &orchtypes.PlannerSelectedOptions{
		Selections: []orchtypes.PlannerSelectedOption{
			{
				OptionID:    req.OrchestratorExecutionMode.String(),
				OptionLabel: req.OrchestratorExecutionMode.GetLabel(),
				OptionValue: req.OrchestratorExecutionMode.String(),
				Group:       "execution_strategy", // Correct group name matching PlannerOrchestrator.GetExecutionMode()
			},
		},
	}
	log.Printf("[EXTERNAL API] Using orchestrator execution mode: %s", req.OrchestratorExecutionMode.String())

	// Create custom tools (workspace + human tools)
	workspaceTools := virtualtools.CreateWorkspaceTools()
	workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()
	humanTools := virtualtools.CreateHumanTools()
	humanExecutors := virtualtools.CreateHumanToolExecutors()

	allTools := append(workspaceTools, humanTools...)
	allExecutors := make(map[string]interface{})
	for name, executor := range workspaceExecutors {
		allExecutors[name] = executor
	}
	for name, executor := range humanExecutors {
		allExecutors[name] = executor
	}

	// Create fresh orchestrator
	orchestrator, err := orchtypes.NewPlannerOrchestrator(
		ctx,
		provider,
		model,
		api.configPath,
		api.temperature,
		"orchestrator",
		api.workspaceRoot,
		api.logger,
		api.internalLLM,
		orchestratorAgentEventBridge,
		tracer,
		selectedServers,
		selectedOptions,
		allTools,
		allExecutors,
		llmConfig,
		100, // maxTurns
	)
	if err != nil {
		log.Printf("[EXTERNAL API] Failed to create orchestrator: %v", err)
		api.emitExecutionError(observerID, sessionID, "orchestrator", preset.Query, err, startTime, traceID, tracer)
		return
	}

	// Store orchestrator for guidance injection
	api.storePlannerOrchestrator(sessionID, orchestrator)

	// Create cancellable context
	orchestratorCtx, orchestratorCancel := context.WithCancel(context.Background())
	api.orchestratorContextMux.Lock()
	api.orchestratorContexts[sessionID] = orchestratorCancel
	api.orchestratorContextMux.Unlock()

	defer func() {
		api.orchestratorContextMux.Lock()
		delete(api.orchestratorContexts, sessionID)
		api.orchestratorContextMux.Unlock()
	}()

	// Execute orchestrator flow
	log.Printf("[EXTERNAL API] Executing orchestrator flow for query: %s", preset.Query)
	result, err := orchestrator.Execute(orchestratorCtx, preset.Query, "", nil)

	if err != nil {
		log.Printf("[EXTERNAL API] Orchestrator execution failed: %v", err)
		api.updateSessionStatus(sessionID, "error")
		api.emitExecutionError(observerID, sessionID, "orchestrator", preset.Query, err, startTime, traceID, tracer)
		return
	}

	// Update session status to completed
	api.updateSessionStatus(sessionID, "completed")

	// End trace
	tracer.EndTrace(traceID, map[string]interface{}{
		"status": "completed",
	})

	// Emit completion event
	api.emitCompletionEvent(observerID, sessionID, "orchestrator", preset.Query, result, startTime)

	log.Printf("[EXTERNAL API] Orchestrator execution completed for session %s", sessionID)
}

// executeWorkflowPreset executes a workflow preset
func (api *StreamingAPI) executeWorkflowPreset(
	ctx context.Context,
	sessionID, observerID string,
	preset *database.PresetQuery,
	executionPhase string,
	selectedServers []string,
	provider, model string,
	llmConfig *orchestrator.LLMConfig,
	traceID observability.TraceID,
	tracer observability.Tracer,
	startTime time.Time,
	req *ExecutePresetRequest,
) {
	log.Printf("[EXTERNAL API] Starting workflow execution for session %s (phase: %s)", sessionID, executionPhase)

	// Create workflow event bridge
	workflowEventBridge := &eventbridge.WorkflowEventBridge{
		BaseEventBridge: &eventbridge.BaseEventBridge{
			EventStore:      api.eventStore,
			ObserverManager: api.observerManager,
			ObserverID:      observerID,
			SessionID:       sessionID,
			Logger:          api.logger,
			ChatDB:          api.chatDB,
			BridgeName:      "workflow",
		},
	}

	// Create custom tools (workspace + human tools)
	workspaceTools := virtualtools.CreateWorkspaceTools()
	workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()
	humanTools := virtualtools.CreateHumanTools()
	humanExecutors := virtualtools.CreateHumanToolExecutors()

	allTools := append(workspaceTools, humanTools...)
	allExecutors := make(map[string]interface{})
	for name, executor := range workspaceExecutors {
		allExecutors[name] = executor
	}
	for name, executor := range humanExecutors {
		allExecutors[name] = executor
	}

	// Create workflow orchestrator
	workflowOrchestrator, err := orchtypes.NewWorkflowOrchestrator(
		ctx,
		provider,
		model,
		api.mcpConfigPath,
		api.temperature,
		"workflow",
		api.logger,
		api.internalLLM,
		workflowEventBridge,
		tracer,
		selectedServers,
		allTools,
		allExecutors,
		llmConfig,
		100, // maxTurns
	)
	if err != nil {
		log.Printf("[EXTERNAL API] Failed to create workflow orchestrator: %v", err)
		api.emitExecutionError(observerID, sessionID, "workflow", preset.Query, err, startTime, traceID, tracer)
		return
	}

	// Store workflow orchestrator for guidance injection
	api.storeWorkflowOrchestrator(sessionID, workflowOrchestrator)

	// Create cancellable context
	workflowCtx, workflowCancel := context.WithCancel(context.Background())
	api.orchestratorContextMux.Lock()
	api.orchestratorContexts[sessionID] = workflowCancel
	api.orchestratorContextMux.Unlock()

	defer func() {
		api.orchestratorContextMux.Lock()
		delete(api.orchestratorContexts, sessionID)
		api.orchestratorContextMux.Unlock()
	}()

	// Get workflow status and selected options from user request (with database fallback)
	workflowStatus := executionPhase
	var selectedOptions *database.WorkflowSelectedOptions

	// Use user-provided selected options first
	if req.WorkflowSelectedOptions != nil {
		selectedOptions = req.WorkflowSelectedOptions
		log.Printf("[EXTERNAL API] Using workflow selected options from user request with %d selections", len(selectedOptions.Selections))
	} else {
		// Fallback to database if no user options provided
		workflow, err := api.chatDB.GetWorkflowByPresetQueryID(ctx, preset.ID)
		if err == nil && workflow != nil {
			workflowStatus = workflow.WorkflowStatus
			selectedOptions = workflow.SelectedOptions
			log.Printf("[EXTERNAL API] Using workflow status from database: %s", workflowStatus)
		}
	}

	// Set workspace path
	workflowWorkspacePath := "default_workspace" // Use default workspace path

	// Prepare options for the Execute method
	workflowOptions := map[string]interface{}{
		"workflowStatus":  workflowStatus,
		"selectedOptions": selectedOptions,
	}

	// Execute workflow
	log.Printf("[EXTERNAL API] Executing workflow for query: %s (status: %s)", preset.Query, workflowStatus)
	_, err = workflowOrchestrator.Execute(
		workflowCtx,
		preset.Query,
		workflowWorkspacePath,
		workflowOptions,
	)

	if err != nil {
		log.Printf("[EXTERNAL API] Workflow execution failed: %v", err)
		api.updateSessionStatus(sessionID, "error")
		api.emitExecutionError(observerID, sessionID, "workflow", preset.Query, err, startTime, traceID, tracer)
		return
	}

	// Update session status to completed
	api.updateSessionStatus(sessionID, "completed")

	// End trace
	tracer.EndTrace(traceID, map[string]interface{}{
		"status": "completed",
	})

	log.Printf("[EXTERNAL API] Workflow execution completed for session %s", sessionID)
}

// handleCancelExecution handles external execution cancellation requests
func (api *StreamingAPI) handleCancelExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Parse request body
	var req CancelExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate session_id is provided
	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	// Check if session exists
	activeSession, exists := api.getActiveSession(req.SessionID)
	if !exists {
		http.Error(w, fmt.Sprintf("Session not found: %s", req.SessionID), http.StatusNotFound)
		return
	}

	log.Printf("[EXTERNAL API] Cancelling execution for session %s (mode: %s)", req.SessionID, activeSession.AgentMode)

	// Cancel execution based on mode
	if activeSession.AgentMode == "orchestrator" {
		// Cancel orchestrator context
		api.orchestratorContextMux.Lock()
		if cancelFunc, exists := api.orchestratorContexts[req.SessionID]; exists {
			cancelFunc()
			delete(api.orchestratorContexts, req.SessionID)
			log.Printf("[EXTERNAL API] Cancelled orchestrator execution for session %s", req.SessionID)
		}
		api.orchestratorContextMux.Unlock()
	} else if activeSession.AgentMode == "workflow" {
		// Cancel workflow context
		api.orchestratorContextMux.Lock()
		if cancelFunc, exists := api.orchestratorContexts[req.SessionID]; exists {
			cancelFunc()
			delete(api.orchestratorContexts, req.SessionID)
			log.Printf("[EXTERNAL API] Cancelled workflow execution for session %s", req.SessionID)
		}
		api.orchestratorContextMux.Unlock()
	}

	// Update session status to cancelled
	api.updateSessionStatus(req.SessionID, "cancelled")

	// Return success response
	response := CancelExecutionResponse{
		SessionID: req.SessionID,
		Status:    "cancelled",
		Message:   "Execution cancelled gracefully",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// --- HELPER FUNCTIONS ---

// emitExecutionError emits an error completion event
func (api *StreamingAPI) emitExecutionError(
	observerID, sessionID, agentMode, query string,
	err error,
	startTime time.Time,
	traceID observability.TraceID,
	tracer observability.Tracer,
) {
	// Update database
	if api.chatDB != nil {
		api.chatDB.UpdateChatSession(context.Background(), sessionID, &database.UpdateChatSessionRequest{
			Status: "error",
		})
	}

	// End trace
	tracer.EndTrace(traceID, map[string]interface{}{
		"status": "failed",
		"error":  err.Error(),
	})

	// Emit error completion event
	errorEventData := unifiedevents.NewUnifiedCompletionEventWithError(
		"external",
		agentMode,
		query,
		err.Error(),
		time.Since(startTime),
		0,
	)

	agentEvent := unifiedevents.NewAgentEvent(errorEventData)
	agentEvent.SessionID = observerID

	serverErrorEvent := events.Event{
		ID:        fmt.Sprintf("external_error_%s_%d", sessionID, time.Now().UnixNano()),
		Type:      string(unifiedevents.EventTypeUnifiedCompletion),
		Timestamp: time.Now(),
		Data:      agentEvent,
		SessionID: observerID,
	}
	api.eventStore.AddEvent(observerID, serverErrorEvent)
	log.Printf("[EXTERNAL API] Emitted error completion event for session %s", sessionID)
}

// emitCompletionEvent emits a completion event
func (api *StreamingAPI) emitCompletionEvent(
	observerID, sessionID, agentMode, query, result string,
	startTime time.Time,
) {
	completionEventData := unifiedevents.NewUnifiedCompletionEvent(
		"external",
		agentMode,
		query,
		result,
		"completed",
		time.Since(startTime),
		1,
	)

	agentEvent := unifiedevents.NewAgentEvent(completionEventData)
	agentEvent.SessionID = observerID

	serverCompletionEvent := events.Event{
		ID:        fmt.Sprintf("external_completion_%s_%d", sessionID, time.Now().UnixNano()),
		Type:      string(unifiedevents.EventTypeUnifiedCompletion),
		Timestamp: time.Now(),
		Data:      agentEvent,
		SessionID: observerID,
	}
	api.eventStore.AddEvent(observerID, serverCompletionEvent)
	log.Printf("[EXTERNAL API] Emitted completion event for session %s", sessionID)
}
