package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// VariablesExtractedEvent represents the event when variables are extracted from objective
type VariablesExtractedEvent struct {
	events.BaseEventData
	Variables          []Variable `json:"variables"`
	TemplatedObjective string     `json:"templated_objective"`
	WorkspacePath      string     `json:"workspace_path"`       // Workspace path for file operations (required)
	RunFolder          string     `json:"run_folder,omitempty"` // Run folder name for run-specific configs
}

// GetEventType returns the event type for VariablesExtractedEvent
func (e *VariablesExtractedEvent) GetEventType() events.EventType {
	return events.VariablesExtracted
}

// VariableManager manages variable extraction and state independently from controller
type VariableManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Variable extraction LLM config (optional preset)
	presetVariableExtractionLLM *AgentLLMConfig

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string
}

// NewVariableManager creates a new VariableManager
func NewVariableManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetVariableExtractionLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *VariableManager {
	return &VariableManager{
		BaseOrchestrator:            baseOrchestrator,
		presetVariableExtractionLLM: presetVariableExtractionLLM,
		sessionID:                   sessionID,
		workflowID:                  workflowID,
	}
}

// runVariableExtractionPhase extracts variables from objective (with optional human feedback and existing variables for update mode)
// Returns: (manifest, templatedObjective, updatedConversationHistory, error)
// In UPDATE mode: If successful, variables were already approved via human_feedback tool, so caller should skip requestVariableApproval
func (vm *VariableManager) runVariableExtractionPhase(ctx context.Context, objective string, iteration int, humanFeedback string, conversationHistory []llmtypes.MessageContent, existingVariables *VariablesManifest) (*VariablesManifest, string, []llmtypes.MessageContent, error) {
	if existingVariables != nil {
		vm.GetLogger().Infof("🔍 Starting variable extraction in UPDATE mode (attempt %d)", iteration)
	} else {
		vm.GetLogger().Infof("🔍 Starting variable extraction from objective (attempt %d)", iteration)
	}

	// Create variable extraction agent (uses default orchestrator LLM config)
	extractionAgent, err := vm.createVariableExtractionAgent(ctx)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create variable extraction agent: %w", err)
	}

	// Prepare template variables
	extractionTemplateVars := map[string]string{
		"Objective":     vm.GetObjective(),
		"WorkspacePath": vm.GetWorkspacePath(),
	}

	// Add existing variables JSON if in update mode (similar to planning agent's ExistingPlanJSON)
	if existingVariables != nil {
		existingVariablesJSON, err := json.MarshalIndent(existingVariables, "", "  ")
		if err != nil {
			vm.GetLogger().Warnf("⚠️ Failed to marshal existing variables to JSON: %v", err)
		} else {
			extractionTemplateVars["ExistingVariablesJSON"] = string(existingVariablesJSON)
			vm.GetLogger().Infof("✅ Passing existing variables contents in template (UPDATE mode)")
		}
	}

	// Determine user message based on whether this is first attempt or revision
	// - For first attempt: Use "Extract variables..." instruction
	// - For revisions: Use human feedback if provided, otherwise use instruction
	var userMessage string
	if humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
		// Revision attempt: Use human feedback as user message
		userMessage = humanFeedback
		vm.GetLogger().Infof("📝 Using human feedback as user message for variable extraction (attempt %d)", iteration)
	} else {
		// First attempt: Use static instruction
		userMessage = "Extract variables from the objective and call submit_variable_extraction_response tool with the structured output."
		vm.GetLogger().Infof("📝 Using default instruction for variable extraction (attempt %d)", iteration)
	}

	// Execute variable extraction - use ExecuteStructuredUpdate in UPDATE mode, ExecuteStructured in CREATE mode
	extractionAgentTyped, ok := extractionAgent.(*VariableExtractionAgent)
	if !ok {
		return nil, "", nil, fmt.Errorf("failed to cast variable extraction agent to correct type")
	}

	var manifest *VariablesManifest
	var updatedHistory []llmtypes.MessageContent

	if existingVariables != nil {
		// UPDATE mode: Use conversational approach with tools
		manifest, updatedHistory, err = extractionAgentTyped.ExecuteStructuredUpdate(ctx, extractionTemplateVars, conversationHistory, userMessage, vm.ReadWorkspaceFile, vm.WriteWorkspaceFile)
	} else {
		// CREATE mode: Use structured output via tool
		manifest, updatedHistory, err = extractionAgentTyped.ExecuteStructured(ctx, extractionTemplateVars, conversationHistory, userMessage)
	}
	if err != nil {
		// Check if this is a non-structured response error (text response instead of structured output)
		if agents.IsNonStructuredResponseError(err) {
			var nonStructuredErr *agents.NonStructuredResponseError
			if errors.As(err, &nonStructuredErr) {
				// In UPDATE mode, conversational responses are expected - the agent uses human_feedback and update tools
				// NonStructuredResponseError is NOT an error in UPDATE mode - it's the normal conversational flow
				// In CREATE mode, we expect structured output, so conversational responses need user feedback
				if existingVariables != nil {
					// UPDATE mode: This is expected - agent is being conversational
					// Continue the conversation by returning the error with updated history
					// The loop will handle it by continuing with the conversation history
					errMsg := nonStructuredErr.OriginalError.Error()
					if strings.Contains(errMsg, "human feedback requested") {
						// Agent called human_feedback but hasn't called update tools yet - this is expected behavior
						// User approves via human_feedback tool, then agent calls update tools in same or next turn
						vm.GetLogger().Infof("📝 Variable extraction agent in UPDATE mode: human_feedback called, waiting for user approval. Continuing conversation.")
						feedbackError := fmt.Errorf("VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:%s", "Please continue with the variable updates after reviewing the proposed changes.")
						return nil, "", nonStructuredErr.UpdatedHistory, feedbackError
					}
					// Other conversational responses in UPDATE mode - continue conversation
					vm.GetLogger().Infof("📝 Variable extraction agent in UPDATE mode returned conversational response. Continuing conversation.")
					feedbackError := fmt.Errorf("VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:%s", nonStructuredErr.TextResponse)
					return nil, "", nonStructuredErr.UpdatedHistory, feedbackError
				}

				// CREATE mode: Display the text response to the user and request feedback
				vm.GetLogger().Infof("📝 Variable extraction agent returned conversational text instead of structured output. Displaying to user for feedback.")

				// Generate unique request ID
				requestID := fmt.Sprintf("variable_extraction_text_response_%d_%d", iteration, time.Now().UnixNano())

				// Display the text response and request feedback
				approved, feedback, feedbackErr := vm.RequestHumanFeedback(
					ctx,
					requestID,
					"The variable extraction agent provided the following response instead of a structured output. Please provide feedback to help it generate a proper structured response:",
					nonStructuredErr.TextResponse,
					vm.sessionID,
					vm.workflowID,
				)

				if feedbackErr != nil {
					return nil, "", nil, fmt.Errorf("failed to request human feedback for variable extraction text response: %w", feedbackErr)
				}

				// If user approved (clicked Approve button), treat as no feedback and continue
				// Otherwise, use the feedback for next attempt
				if approved {
					vm.GetLogger().Infof("✅ User approved variable extraction text response, but no structured output was generated. This is unexpected - returning error.")
					return nil, "", nil, fmt.Errorf("variable extraction agent returned text response but user approved without providing feedback to generate structured output")
				}

				// User provided feedback - return a special error that the loop can detect and handle
				// Use a specific error prefix that the loop will recognize
				// The updated history from the agent's response is included so conversation continues properly
				feedbackError := fmt.Errorf("VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:%s", feedback)
				vm.GetLogger().Infof("🔄 [DEBUG] Returning feedback error from runVariableExtractionPhase: %s", feedbackError.Error())
				return nil, "", nonStructuredErr.UpdatedHistory, feedbackError
			}
		}
		// For other errors, return as-is
		return nil, "", nil, fmt.Errorf("variable extraction failed: %w", err)
	}

	// In UPDATE mode, variables.json is already updated by the tools, so we don't need to save again
	// In CREATE mode, we need to save the manifest to file
	if existingVariables == nil {
		// CREATE mode: Save to file for persistence and debugging
		variablesPath := fmt.Sprintf("%s/variables/variables.json", vm.GetWorkspacePath())
		variablesJSON, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			vm.GetLogger().Warnf("⚠️ Failed to marshal variables manifest to JSON: %v (continuing anyway)", err)
		} else {
			if err := vm.WriteWorkspaceFile(ctx, variablesPath, string(variablesJSON)); err != nil {
				vm.GetLogger().Warnf("⚠️ Failed to save variables.json to file: %v (continuing anyway)", err)
			} else {
				vm.GetLogger().Infof("💾 Saved variables.json to %s for persistence", variablesPath)
			}
		}
	} else {
		// UPDATE mode: Variables were already updated by update_variable/update_objective tools
		vm.GetLogger().Infof("✅ Variables updated via tools in UPDATE mode (conversation has %d messages)", len(updatedHistory))
	}

	vm.GetLogger().Infof("✅ Extracted %d variables from objective (conversation has %d messages)", len(manifest.Variables), len(updatedHistory))
	return manifest, manifest.Objective, updatedHistory, nil
}

// requestVariableApproval requests human approval for extracted variables
func (vm *VariableManager) requestVariableApproval(ctx context.Context, manifest *VariablesManifest, revisionAttempt int) (bool, string, error) {
	vm.GetLogger().Infof("⏸️ Requesting human approval for extracted variables (attempt %d)", revisionAttempt)

	// Format variables for display
	var variablesSummary strings.Builder
	variablesSummary.WriteString(fmt.Sprintf("Extracted %d variables from objective:\n\n", len(manifest.Variables)))

	for _, variable := range manifest.Variables {
		variablesSummary.WriteString(fmt.Sprintf("- **{{%s}}**: %s\n", variable.Name, variable.Description))
		variablesSummary.WriteString(fmt.Sprintf("  - Value: %s\n", variable.Value))
		variablesSummary.WriteString("\n")
	}

	variablesSummary.WriteString(fmt.Sprintf("\n**Templated Objective**:\n%s", manifest.Objective))

	// Generate unique request ID
	requestID := fmt.Sprintf("variable_approval_%d_%d", revisionAttempt, time.Now().UnixNano())

	// Use common human feedback function
	return vm.RequestHumanFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Please review the extracted variables (attempt %d). Are these correct or do you want to provide feedback for refinement?", revisionAttempt),
		variablesSummary.String(),
		vm.sessionID,
		vm.workflowID,
	)
}

// createVariableExtractionAgent creates the variable extraction agent using base orchestrator functions
// This refactored version uses CreateAndSetupStandardAgent to avoid code duplication
func (vm *VariableManager) createVariableExtractionAgent(ctx context.Context) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from workspace (read-only), writes only to variables
	baseWorkspacePath := vm.GetWorkspacePath()
	variablesPath := fmt.Sprintf("%s/variables", baseWorkspacePath)

	// Read from base workspace (to understand objective), write only to variables folder
	readPaths := []string{baseWorkspacePath}
	writePaths := []string{variablesPath}
	vm.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	vm.GetLogger().Infof("🔒 Setting folder guard for variable extraction agent - Read paths: %v, Write paths: %v", readPaths, writePaths)

	// Determine LLM config: Priority: preset default > orchestrator default
	var llmConfigToUse *orchestrator.LLMConfig
	orchestratorLLMConfig := vm.GetLLMConfig()
	if vm.presetVariableExtractionLLM != nil && vm.presetVariableExtractionLLM.Provider != "" && vm.presetVariableExtractionLLM.ModelID != "" {
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              vm.presetVariableExtractionLLM.Provider,
			ModelID:               vm.presetVariableExtractionLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,        // Preserve fallback models from orchestrator
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback, // Preserve cross-provider fallback
			APIKeys:               orchestratorLLMConfig.APIKeys,               // Preserve API keys from orchestrator
		}
		vm.GetLogger().Infof("🔧 Using preset default variable extraction LLM: %s/%s", vm.presetVariableExtractionLLM.Provider, vm.presetVariableExtractionLLM.ModelID)
	} else {
		llmConfigToUse = orchestratorLLMConfig
		vm.GetLogger().Infof("🔧 Using orchestrator default variable extraction LLM: %s/%s", vm.GetProvider(), vm.GetModel())
	}

	// Create agent config with the selected LLM config
	config := vm.CreateStandardAgentConfigWithLLM("variable-extraction-agent", vm.GetMaxTurns(), agents.OutputFormatStructured, llmConfigToUse)

	// Disable large output virtual tools for variable extraction agent
	disabled := false
	config.EnableLargeOutputVirtualTools = &disabled
	vm.GetLogger().Infof("🔧 Disabling large output virtual tools for variable extraction agent")

	// Variable extraction agent doesn't need MCP servers - pure LLM extraction
	config.ServerNames = []string{mcpclient.NoServers}

	// Wrapper function to match OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewVariableExtractionAgent(cfg, logger, tracer, eventBridge)
	}

	// Use base orchestrator's CreateAndSetupStandardAgentWithConfig to avoid code duplication
	// This handles initialization, event bridge connection, and tool registration
	// Set overwriteSystemPrompt to true for variable extraction agent (replaces default MCP prompt with agent-specific prompt)
	agent, err := vm.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"variable_extraction",
		0, 0, // step, iteration
		createAgentFunc,
		vm.WorkspaceTools,
		vm.WorkspaceToolExecutors,
		true, // overwriteSystemPrompt: true - replace default prompt with agent-specific prompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create variable extraction agent: %w", err)
	}

	return agent, nil
}

// checkExistingVariables checks if variables.json already exists and loads it
func (vm *VariableManager) checkExistingVariables(ctx context.Context, variablesPath string) (bool, *VariablesManifest, error) {
	vm.GetLogger().Infof("🔍 Checking for existing variables at %s", variablesPath)

	// Try to read variables.json
	variablesContent, err := vm.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		// Check if it's a "file not found" error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			vm.GetLogger().Infof("📋 No existing variables found: %w", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing variables: %w", err)
	}

	// Parse the existing variables manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		vm.GetLogger().Warnf("⚠️ Failed to parse existing variables.json: %w", err)
		return false, nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	vm.GetLogger().Infof("✅ Found existing variables.json with %d variables", len(manifest.Variables))
	return true, &manifest, nil
}

// LoadVariableValues loads variable values from variables.json file
// Public method that accepts BaseOrchestrator, workspacePath, and runWorkspacePath as parameters
func LoadVariableValues(ctx context.Context, bo *orchestrator.BaseOrchestrator, workspacePath, runWorkspacePath string) (map[string]string, error) {
	// Try to load from run folder first (run-specific variables), then fallback to workspace default
	runVariablesPath := fmt.Sprintf("%s/variables/variables.json", runWorkspacePath)
	workspaceVariablesPath := fmt.Sprintf("%s/variables/variables.json", workspacePath)

	var variablesContent string
	var err error

	// Try run folder first
	variablesContent, err = bo.ReadWorkspaceFile(ctx, runVariablesPath)
	if err != nil {
		// Fallback to workspace folder
		variablesContent, err = bo.ReadWorkspaceFile(ctx, workspaceVariablesPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read variables.json from both locations: %w", err)
		}
		bo.GetLogger().Infof("📁 Loaded variables from workspace folder: %s", workspaceVariablesPath)
	} else {
		bo.GetLogger().Infof("📁 Loaded variables from runs folder: %s", runVariablesPath)
	}

	// Parse variables.json to get current values
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	// Load values into the variableValues map
	variableValues := make(map[string]string)
	for _, variable := range manifest.Variables {
		variableValues[variable.Name] = variable.Value
	}

	bo.GetLogger().Infof("✅ Loaded variable values from variables.json: %d variables", len(variableValues))
	return variableValues, nil
}

// ResolveVariables replaces {{VARIABLE}} placeholders with actual values
// Public method that accepts variableValues as parameter
func ResolveVariables(text string, variableValues map[string]string) string {
	if variableValues == nil {
		return text // No variables to resolve
	}

	resolved := text
	for varName, varValue := range variableValues {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		resolved = strings.ReplaceAll(resolved, placeholder, varValue)
	}
	return resolved
}

// ResolveVariablesArray resolves variables in an array of strings
// Public method that accepts variableValues as parameter
func ResolveVariablesArray(arr []string, variableValues map[string]string) []string {
	if variableValues == nil {
		return arr // No variables to resolve
	}

	resolved := make([]string, len(arr))
	for i, item := range arr {
		resolved[i] = ResolveVariables(item, variableValues)
	}
	return resolved
}

// FormatVariableNames formats the variables manifest into a human-readable string for agent prompts
// Public method that accepts manifest as parameter
func FormatVariableNames(manifest *VariablesManifest) string {
	if manifest == nil || len(manifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range manifest.Variables {
		builder.WriteString(fmt.Sprintf("- {{%s}} - %s\n", variable.Name, variable.Description))
	}
	return builder.String()
}

// FormatVariableValues formats the variables manifest with their actual values for agent prompts
// Public method that accepts manifest and variableValues as parameters
func FormatVariableValues(manifest *VariablesManifest, variableValues map[string]string) string {
	if manifest == nil || len(manifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range manifest.Variables {
		// Get the actual resolved value from variableValues map if available
		actualValue := variable.Value
		if variableValues != nil {
			if resolvedValue, exists := variableValues[variable.Name]; exists {
				actualValue = resolvedValue
			}
		}
		builder.WriteString(fmt.Sprintf("- {{%s}} = %s - %s\n", variable.Name, actualValue, variable.Description))
	}
	return builder.String()
}

// EmitVariablesExtractedEvent emits an event when variables are extracted from objective
// Public method that accepts BaseOrchestrator and other parameters
func EmitVariablesExtractedEvent(ctx context.Context, bo *orchestrator.BaseOrchestrator, variables []Variable, templatedObjective, runFolder, workspacePath string) {
	if bo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &VariablesExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Variables:          variables,
		TemplatedObjective: templatedObjective,
		WorkspacePath:      workspacePath,
		RunFolder:          runFolder,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.VariablesExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := bo.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit variables extracted event: %w", err)
	} else {
		bo.GetLogger().Infof("✅ Emitted variables extracted event: %d variables", len(variables))
	}
}

// emitVariablesExtractedEvent emits variables extracted event
func (vm *VariableManager) emitVariablesExtractedEvent(ctx context.Context, variables []Variable, templatedObjective string) {
	// Use default workspace path from orchestrator
	EmitVariablesExtractedEvent(ctx, vm.BaseOrchestrator, variables, templatedObjective, "", vm.GetWorkspacePath())
}

// ExtractVariablesOnly runs only the variable extraction phase (standalone, independent from CreateTodoList)
// This is a separate workflow phase that can be run independently
func (vm *VariableManager) ExtractVariablesOnly(ctx context.Context, objective, workspacePath string) (string, error) {
	vm.GetLogger().Infof("🔍 Starting standalone variable extraction for objective: %s", objective)

	// Set objective and workspace path
	vm.SetObjective(objective)
	vm.SetWorkspacePath(workspacePath)

	// Check if variables.json already exists
	variablesPath := fmt.Sprintf("%s/variables/variables.json", vm.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := vm.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		vm.GetLogger().Warnf("⚠️ Failed to check for existing variables: %w", err)
		variablesExist = false
	}

	var variablesManifest *VariablesManifest
	var templatedObjective string

	// If variables exist, emit event immediately so UI can display them while user decides what to do
	if variablesExist {
		vm.emitVariablesExtractedEvent(ctx, existingVariablesManifest.Variables, existingVariablesManifest.Objective)
		vm.GetLogger().Infof("🔍 Emitted variables event for UI display (%d variables)", len(existingVariablesManifest.Variables))
	}

	// If variables exist, ask user if they want to use them, extract new ones, or update existing
	if variablesExist {
		requestID := fmt.Sprintf("existing_variables_decision_%d", time.Now().UnixNano())
		variableOptions := []string{
			"Use Existing Variables",    // Option 0: Use existing variables as-is
			"Extract New Variables",     // Option 1: Delete everything and extract new
			"Update Existing Variables", // Option 2: Update existing variables with feedback
		}
		variableChoice, err := vm.RequestMultipleChoiceFeedback(
			ctx,
			requestID,
			"Found existing variables.json. What would you like to do?",
			variableOptions,
			fmt.Sprintf("Variables file: %s\nFound %d variables", variablesPath, len(existingVariablesManifest.Variables)),
			vm.sessionID,
			vm.workflowID,
		)
		if err != nil {
			vm.GetLogger().Warnf("⚠️ Failed to get user decision for existing variables: %w", err)
			// Default to using existing variables
			variableChoice = "option0"
		}

		switch variableChoice {
		case "option0":
			// Use existing variables
			vm.GetLogger().Infof("✅ User chose to use existing variables")
			variablesManifest = existingVariablesManifest
			// Note: variablesManifest is returned, caller should manage state
			templatedObjective = existingVariablesManifest.Objective
			// Event already emitted above when variables were found

		case "option1":
			// Extract new variables - cleanup everything and extract fresh
			vm.GetLogger().Infof("🔄 User chose to extract new variables, cleaning up existing variables file")
			if err := vm.DeleteWorkspaceFile(ctx, variablesPath); err != nil {
				vm.GetLogger().Warnf("⚠️ Failed to delete existing variables file: %v (will be overwritten during extraction)", err)
			} else {
				vm.GetLogger().Infof("🗑️ Deleted existing variables file: %s", variablesPath)
			}
			variablesExist = false // Trigger variable extraction

		case "option2":
			// Update existing variables - request feedback and update with existing context
			vm.GetLogger().Infof("🔄 User chose to update existing variables, requesting update feedback")

			// Format existing variables for display
			var variablesSummary strings.Builder
			variablesSummary.WriteString(fmt.Sprintf("Current variables (%d total):\n\n", len(existingVariablesManifest.Variables)))
			for _, variable := range existingVariablesManifest.Variables {
				variablesSummary.WriteString(fmt.Sprintf("- **{{%s}}**: %s\n", variable.Name, variable.Description))
				variablesSummary.WriteString(fmt.Sprintf("  - Value: %s\n", variable.Value))
				variablesSummary.WriteString("\n")
			}
			variablesSummary.WriteString(fmt.Sprintf("\n**Templated Objective**:\n%s", existingVariablesManifest.Objective))

			// Request human feedback about what they want to update
			updateFeedbackID := fmt.Sprintf("variable_update_feedback_%d", time.Now().UnixNano())
			approved, updateFeedback, err := vm.RequestHumanFeedback(
				ctx,
				updateFeedbackID,
				"What would you like to update in the existing variables? Please describe the changes or improvements you want.",
				fmt.Sprintf("Current variables location: %s\nFound %d variables\n\n%s\n\nYour feedback will be used to guide the update of variables while preserving unchanged ones.", variablesPath, len(existingVariablesManifest.Variables), variablesSummary.String()),
				vm.sessionID,
				vm.workflowID,
			)
			if err != nil {
				vm.GetLogger().Warnf("⚠️ Failed to get update feedback: %v, proceeding without specific update guidance", err)
				updateFeedback = ""
			} else if approved {
				vm.GetLogger().Infof("ℹ️ User approved without providing update feedback, will update variables without specific guidance")
				updateFeedback = ""
			}

			// Set flag to trigger update mode extraction
			variablesExist = false
			// Store existing variables and feedback for use in extraction loop
			existingVariablesForUpdate := existingVariablesManifest
			initialUpdateFeedback := updateFeedback

			// Run variable extraction in update mode
			maxVariableRevisions := 10
			var variableFeedback string
			var variableConversationHistory []llmtypes.MessageContent

			// Use initial update feedback for first attempt
			variableFeedback = initialUpdateFeedback
			vm.GetLogger().Infof("📝 Using initial update feedback for first extraction attempt: %s", variableFeedback)

			for revisionAttempt := 1; revisionAttempt <= maxVariableRevisions; revisionAttempt++ {
				vm.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

				var err error
				variablesManifest, templatedObjective, variableConversationHistory, err = vm.runVariableExtractionPhase(ctx, objective, revisionAttempt, variableFeedback, variableConversationHistory, existingVariablesForUpdate)
				if err != nil {
					errMsg := err.Error()
					feedbackPrefix := "VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:"
					if strings.Contains(errMsg, feedbackPrefix) {
						parts := strings.Split(errMsg, feedbackPrefix)
						if len(parts) > 1 {
							extractedFeedback := strings.TrimSpace(parts[1])
							variableFeedback = extractedFeedback
							if revisionAttempt >= maxVariableRevisions {
								vm.GetLogger().Warnf("⚠️ Max variable extraction revision attempts (%d) reached", maxVariableRevisions)
								templatedObjective = objective
								break
							}
							continue
						}
					}
					vm.GetLogger().Warnf("⚠️ Variable extraction failed: %v", err)
					templatedObjective = objective
					break
				}

				// Request human approval for extracted variables
				approved, feedback, err := vm.requestVariableApproval(ctx, variablesManifest, revisionAttempt)
				if err != nil {
					vm.GetLogger().Warnf("⚠️ Variable approval request failed: %v, will retry", err)
					approved = false
					feedback = fmt.Sprintf("Error getting approval: %v", err)
				}

				if approved {
					vm.GetLogger().Infof("✅ Variables approved by human")
					vm.emitVariablesExtractedEvent(ctx, variablesManifest.Variables, templatedObjective)
					// Mark variables as existing so the CREATE mode loop doesn't run again
					variablesExist = true
					break
				}

				// Variables rejected with feedback for revision
				vm.GetLogger().Infof("🔄 Variable revision requested (attempt %d/%d): %s", revisionAttempt, maxVariableRevisions, feedback)
				variableFeedback = feedback

				if revisionAttempt >= maxVariableRevisions {
					vm.GetLogger().Warnf("⚠️ Max variable revision attempts (%d) reached", maxVariableRevisions)
					break
				}
			}
		}
	}

	// Extract variables if they don't exist or user wants to re-extract
	if !variablesExist {
		maxVariableRevisions := 10
		var variableFeedback string
		var variableConversationHistory []llmtypes.MessageContent

		for revisionAttempt := 1; revisionAttempt <= maxVariableRevisions; revisionAttempt++ {
			vm.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

			var err error
			variablesManifest, templatedObjective, variableConversationHistory, err = vm.runVariableExtractionPhase(ctx, objective, revisionAttempt, variableFeedback, variableConversationHistory, nil)
			if err != nil {
				errMsg := err.Error()
				feedbackPrefix := "VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:"
				if strings.Contains(errMsg, feedbackPrefix) {
					parts := strings.Split(errMsg, feedbackPrefix)
					if len(parts) > 1 {
						extractedFeedback := strings.TrimSpace(parts[1])
						variableFeedback = extractedFeedback
						if revisionAttempt >= maxVariableRevisions {
							vm.GetLogger().Warnf("⚠️ Max variable extraction revision attempts (%d) reached", maxVariableRevisions)
							templatedObjective = objective
							break
						}
						continue
					}
				}
				vm.GetLogger().Warnf("⚠️ Variable extraction failed: %v", err)
				templatedObjective = objective
				break
			}

			// Request human approval for extracted variables
			approved, feedback, err := vm.requestVariableApproval(ctx, variablesManifest, revisionAttempt)
			if err != nil {
				vm.GetLogger().Warnf("⚠️ Variable approval request failed: %v, will retry", err)
				approved = false
				feedback = fmt.Sprintf("Error getting approval: %v", err)
			}

			if approved {
				vm.GetLogger().Infof("✅ Variables approved by human")
				vm.emitVariablesExtractedEvent(ctx, variablesManifest.Variables, templatedObjective)
				break
			}

			// Variables rejected with feedback for revision
			vm.GetLogger().Infof("🔄 Variable revision requested (attempt %d/%d): %s", revisionAttempt, maxVariableRevisions, feedback)
			variableFeedback = feedback

			if revisionAttempt >= maxVariableRevisions {
				vm.GetLogger().Warnf("⚠️ Max variable revision attempts (%d) reached", maxVariableRevisions)
				break
			}
		}
	}

	// Build result summary
	if variablesManifest != nil {
		var summary strings.Builder
		summary.WriteString("Variable extraction completed successfully.\n\n")
		summary.WriteString(fmt.Sprintf("Extracted %d variables:\n", len(variablesManifest.Variables)))
		for _, variable := range variablesManifest.Variables {
			summary.WriteString(fmt.Sprintf("- {{%s}}: %s (Value: %s)\n", variable.Name, variable.Description, variable.Value))
		}
		summary.WriteString(fmt.Sprintf("\nTemplated Objective:\n%s", templatedObjective))
		return summary.String(), nil
	}

	return "Variable extraction completed (no variables extracted).", nil
}
