package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/pkg/events"
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

// runVariableExtractionPhase extracts variables from objective (with optional human feedback and existing variables for update mode)
// Returns: (manifest, templatedObjective, updatedConversationHistory, error)
// In UPDATE mode: If successful, variables were already approved via human_feedback tool, so caller should skip requestVariableApproval
func (hcpo *HumanControlledTodoPlannerOrchestrator) runVariableExtractionPhase(ctx context.Context, iteration int, humanFeedback string, conversationHistory []llmtypes.MessageContent, existingVariables *VariablesManifest) (*VariablesManifest, string, []llmtypes.MessageContent, error) {
	if existingVariables != nil {
		hcpo.GetLogger().Infof("🔍 Starting variable extraction in UPDATE mode (attempt %d)", iteration)
	} else {
		hcpo.GetLogger().Infof("🔍 Starting variable extraction from objective (attempt %d)", iteration)
	}

	// Create variable extraction agent (uses default orchestrator LLM config)
	extractionAgent, err := hcpo.createVariableExtractionAgent(ctx)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create variable extraction agent: %w", err)
	}

	// Prepare template variables
	extractionTemplateVars := map[string]string{
		"Objective":     hcpo.GetObjective(),
		"WorkspacePath": hcpo.GetWorkspacePath(),
	}

	// Add existing variables JSON if in update mode (similar to planning agent's ExistingPlanJSON)
	if existingVariables != nil {
		existingVariablesJSON, err := json.MarshalIndent(existingVariables, "", "  ")
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to marshal existing variables to JSON: %v", err)
		} else {
			extractionTemplateVars["ExistingVariablesJSON"] = string(existingVariablesJSON)
			hcpo.GetLogger().Infof("✅ Passing existing variables contents in template (UPDATE mode)")
		}
	}

	// Determine user message based on whether this is first attempt or revision
	// - For first attempt: Use "Extract variables..." instruction
	// - For revisions: Use human feedback if provided, otherwise use instruction
	var userMessage string
	if humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
		// Revision attempt: Use human feedback as user message
		userMessage = humanFeedback
		hcpo.GetLogger().Infof("📝 Using human feedback as user message for variable extraction (attempt %d)", iteration)
	} else {
		// First attempt: Use static instruction
		userMessage = "Extract variables from the objective and call submit_variable_extraction_response tool with the structured output."
		hcpo.GetLogger().Infof("📝 Using default instruction for variable extraction (attempt %d)", iteration)
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
		manifest, updatedHistory, err = extractionAgentTyped.ExecuteStructuredUpdate(ctx, extractionTemplateVars, conversationHistory, userMessage, hcpo.ReadWorkspaceFile, hcpo.WriteWorkspaceFile)
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
						hcpo.GetLogger().Infof("📝 Variable extraction agent in UPDATE mode: human_feedback called, waiting for user approval. Continuing conversation.")
						feedbackError := fmt.Errorf("VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:%s", "Please continue with the variable updates after reviewing the proposed changes.")
						return nil, "", nonStructuredErr.UpdatedHistory, feedbackError
					}
					// Other conversational responses in UPDATE mode - continue conversation
					hcpo.GetLogger().Infof("📝 Variable extraction agent in UPDATE mode returned conversational response. Continuing conversation.")
					feedbackError := fmt.Errorf("VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:%s", nonStructuredErr.TextResponse)
					return nil, "", nonStructuredErr.UpdatedHistory, feedbackError
				}

				// CREATE mode: Display the text response to the user and request feedback
				hcpo.GetLogger().Infof("📝 Variable extraction agent returned conversational text instead of structured output. Displaying to user for feedback.")

				// Generate unique request ID
				requestID := fmt.Sprintf("variable_extraction_text_response_%d_%d", iteration, time.Now().UnixNano())

				// Display the text response and request feedback
				approved, feedback, feedbackErr := hcpo.RequestHumanFeedback(
					ctx,
					requestID,
					"The variable extraction agent provided the following response instead of a structured output. Please provide feedback to help it generate a proper structured response:",
					nonStructuredErr.TextResponse,
					hcpo.getSessionID(),
					hcpo.getWorkflowID(),
				)

				if feedbackErr != nil {
					return nil, "", nil, fmt.Errorf("failed to request human feedback for variable extraction text response: %w", feedbackErr)
				}

				// If user approved (clicked Approve button), treat as no feedback and continue
				// Otherwise, use the feedback for next attempt
				if approved {
					hcpo.GetLogger().Infof("✅ User approved variable extraction text response, but no structured output was generated. This is unexpected - returning error.")
					return nil, "", nil, fmt.Errorf("variable extraction agent returned text response but user approved without providing feedback to generate structured output")
				}

				// User provided feedback - return a special error that the loop can detect and handle
				// Use a specific error prefix that the loop will recognize
				// The updated history from the agent's response is included so conversation continues properly
				feedbackError := fmt.Errorf("VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:%s", feedback)
				hcpo.GetLogger().Infof("🔄 [DEBUG] Returning feedback error from runVariableExtractionPhase: %s", feedbackError.Error())
				return nil, "", nonStructuredErr.UpdatedHistory, feedbackError
			}
		}
		// For other errors, return as-is
		return nil, "", nil, fmt.Errorf("variable extraction failed: %w", err)
	}

	// Store manifest in orchestrator for future use
	hcpo.variablesManifest = manifest
	hcpo.templatedObjective = manifest.Objective

	// In UPDATE mode, variables.json is already updated by the tools, so we don't need to save again
	// In CREATE mode, we need to save the manifest to file
	if existingVariables == nil {
		// CREATE mode: Save to file for persistence and debugging
		variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
		variablesJSON, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to marshal variables manifest to JSON: %v (continuing anyway)", err)
		} else {
			if err := hcpo.WriteWorkspaceFile(ctx, variablesPath, string(variablesJSON)); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to save variables.json to file: %v (continuing anyway)", err)
			} else {
				hcpo.GetLogger().Infof("💾 Saved variables.json to %s for persistence", variablesPath)
			}
		}
	} else {
		// UPDATE mode: Variables were already updated by update_variable/update_objective tools
		hcpo.GetLogger().Infof("✅ Variables updated via tools in UPDATE mode (conversation has %d messages)", len(updatedHistory))
	}

	hcpo.GetLogger().Infof("✅ Extracted %d variables from objective (conversation has %d messages)", len(manifest.Variables), len(updatedHistory))
	return manifest, manifest.Objective, updatedHistory, nil
}

// requestVariableApproval requests human approval for extracted variables
func (hcpo *HumanControlledTodoPlannerOrchestrator) requestVariableApproval(ctx context.Context, manifest *VariablesManifest, revisionAttempt int) (bool, string, error) {
	hcpo.GetLogger().Infof("⏸️ Requesting human approval for extracted variables (attempt %d)", revisionAttempt)

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
	return hcpo.RequestHumanFeedback(
		ctx,
		requestID,
		fmt.Sprintf("Please review the extracted variables (attempt %d). Are these correct or do you want to provide feedback for refinement?", revisionAttempt),
		variablesSummary.String(),
		hcpo.getSessionID(),
		hcpo.getWorkflowID(),
	)
}

// createVariableExtractionAgent creates the variable extraction agent (uses default orchestrator LLM config)
func (hcpo *HumanControlledTodoPlannerOrchestrator) createVariableExtractionAgent(ctx context.Context) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from workspace (read-only), writes only to variables
	baseWorkspacePath := hcpo.GetWorkspacePath()
	variablesPath := fmt.Sprintf("%s/variables", baseWorkspacePath)

	// Read from base workspace (to understand objective), write only to variables folder
	// Note: Using base workspace as read path allows reading from root, but we restrict writes to variables/
	readPaths := []string{baseWorkspacePath}
	writePaths := []string{variablesPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for variable extraction agent - Read paths: %v, Write paths: %v (variables automatically readable via writePaths)", readPaths, writePaths)

	// Use default orchestrator LLM config for variable extraction
	llmConfigToUse := hcpo.GetLLMConfig()
	hcpo.GetLogger().Infof("🔧 Using default orchestrator LLM config for variable extraction: %s/%s", hcpo.GetProvider(), hcpo.GetModel())

	// Create agent config with the selected LLM config
	config := hcpo.CreateStandardAgentConfigWithLLM("variable-extraction-agent", hcpo.GetMaxTurns(), agents.OutputFormatStructured, llmConfigToUse)

	// Variable extraction agent doesn't need MCP servers - pure LLM extraction
	config.ServerNames = []string{mcpclient.NoServers}

	// Create agent using provided factory function
	agent := NewVariableExtractionAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize variable extraction agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for variable-extraction-agent")
	}

	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for variable-extraction-agent")
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for variable-extraction-agent")
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext("variable_extraction", 0, 0, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to variable_extraction (agent %s)", baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for variable-extraction-agent")
	}

	// Register custom tools if available
	if hcpo.WorkspaceTools != nil && hcpo.WorkspaceToolExecutors != nil {
		wrappedExecutors := hcpo.WrapWorkspaceToolsWithFolderGuard(hcpo.WorkspaceToolExecutors)
		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for variable-extraction-agent (%s mode)", len(hcpo.WorkspaceTools), baseAgent.GetMode())

		for _, tool := range hcpo.WorkspaceTools {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
					)
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for variable-extraction-agent (%s mode)", baseAgent.GetMode())
	}

	return agent, nil
}

// checkExistingVariables checks if variables.json already exists and loads it
func (hcpo *HumanControlledTodoPlannerOrchestrator) checkExistingVariables(ctx context.Context, variablesPath string) (bool, *VariablesManifest, error) {
	hcpo.GetLogger().Infof("🔍 Checking for existing variables at %s", variablesPath)

	// Try to read variables.json
	variablesContent, err := hcpo.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		// Check if it's a "file not found" error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Infof("📋 No existing variables found: %w", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing variables: %w", err)
	}

	// Parse the existing variables manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to parse existing variables.json: %w", err)
		return false, nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	hcpo.GetLogger().Infof("✅ Found existing variables.json with %d variables", len(manifest.Variables))
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

// loadVariableValues is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) loadVariableValues(ctx context.Context) error {
	variableValues, err := LoadVariableValues(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
	if err != nil {
		return err
	}
	hcpo.variableValues = variableValues
	return nil
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

// resolveVariables is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) resolveVariables(text string) string {
	return ResolveVariables(text, hcpo.variableValues)
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

// formatVariableNames is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatVariableNames() string {
	return FormatVariableNames(hcpo.variablesManifest)
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

// formatVariableValues is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatVariableValues() string {
	return FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues)
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

// emitVariablesExtractedEvent is a private wrapper that uses receiver fields (for backward compatibility)
func (hcpo *HumanControlledTodoPlannerOrchestrator) emitVariablesExtractedEvent(ctx context.Context, variables []Variable, templatedObjective string) {
	// Use default workspace path from orchestrator
	EmitVariablesExtractedEvent(ctx, hcpo.BaseOrchestrator, variables, templatedObjective, "", hcpo.GetWorkspacePath())
}

// ExtractVariablesOnly runs only the variable extraction phase (standalone, independent from CreateTodoList)
// This is a separate workflow phase that can be run independently
func (hcpo *HumanControlledTodoPlannerOrchestrator) ExtractVariablesOnly(ctx context.Context, objective, workspacePath string) (string, error) {
	hcpo.GetLogger().Infof("🔍 Starting standalone variable extraction for objective: %s", objective)

	// Set objective and workspace path
	hcpo.SetObjective(objective)
	hcpo.SetWorkspacePath(workspacePath)

	// Check if variables.json already exists
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := hcpo.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing variables: %w", err)
		variablesExist = false
	}

	var variablesManifest *VariablesManifest
	var templatedObjective string

	// If variables exist, emit event immediately so UI can display them while user decides what to do
	if variablesExist {
		hcpo.emitVariablesExtractedEvent(ctx, existingVariablesManifest.Variables, existingVariablesManifest.Objective)
		hcpo.GetLogger().Infof("🔍 Emitted variables event for UI display (%d variables)", len(existingVariablesManifest.Variables))
	}

	// If variables exist, ask user if they want to use them, extract new ones, or update existing
	if variablesExist {
		requestID := fmt.Sprintf("existing_variables_decision_%d", time.Now().UnixNano())
		variableOptions := []string{
			"Use Existing Variables",    // Option 0: Use existing variables as-is
			"Extract New Variables",     // Option 1: Delete everything and extract new
			"Update Existing Variables", // Option 2: Update existing variables with feedback
		}
		variableChoice, err := hcpo.RequestMultipleChoiceFeedback(
			ctx,
			requestID,
			"Found existing variables.json. What would you like to do?",
			variableOptions,
			fmt.Sprintf("Variables file: %s\nFound %d variables", variablesPath, len(existingVariablesManifest.Variables)),
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing variables: %w", err)
			// Default to using existing variables
			variableChoice = "option0"
		}

		switch variableChoice {
		case "option0":
			// Use existing variables
			hcpo.GetLogger().Infof("✅ User chose to use existing variables")
			variablesManifest = existingVariablesManifest
			hcpo.variablesManifest = existingVariablesManifest
			templatedObjective = existingVariablesManifest.Objective
			// Event already emitted above when variables were found

		case "option1":
			// Extract new variables - cleanup everything and extract fresh
			hcpo.GetLogger().Infof("🔄 User chose to extract new variables, cleaning up existing variables file")
			if err := hcpo.DeleteWorkspaceFile(ctx, variablesPath); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to delete existing variables file: %v (will be overwritten during extraction)", err)
			} else {
				hcpo.GetLogger().Infof("🗑️ Deleted existing variables file: %s", variablesPath)
			}
			variablesExist = false // Trigger variable extraction

		case "option2":
			// Update existing variables - request feedback and update with existing context
			hcpo.GetLogger().Infof("🔄 User chose to update existing variables, requesting update feedback")

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
			approved, updateFeedback, err := hcpo.RequestHumanFeedback(
				ctx,
				updateFeedbackID,
				"What would you like to update in the existing variables? Please describe the changes or improvements you want.",
				fmt.Sprintf("Current variables location: %s\nFound %d variables\n\n%s\n\nYour feedback will be used to guide the update of variables while preserving unchanged ones.", variablesPath, len(existingVariablesManifest.Variables), variablesSummary.String()),
				hcpo.getSessionID(),
				hcpo.getWorkflowID(),
			)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to get update feedback: %v, proceeding without specific update guidance", err)
				updateFeedback = ""
			} else if approved {
				hcpo.GetLogger().Infof("ℹ️ User approved without providing update feedback, will update variables without specific guidance")
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
			hcpo.GetLogger().Infof("📝 Using initial update feedback for first extraction attempt: %s", variableFeedback)

			for revisionAttempt := 1; revisionAttempt <= maxVariableRevisions; revisionAttempt++ {
				hcpo.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

				var err error
				variablesManifest, templatedObjective, variableConversationHistory, err = hcpo.runVariableExtractionPhase(ctx, revisionAttempt, variableFeedback, variableConversationHistory, existingVariablesForUpdate)
				if err != nil {
					errMsg := err.Error()
					feedbackPrefix := "VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:"
					if strings.Contains(errMsg, feedbackPrefix) {
						parts := strings.Split(errMsg, feedbackPrefix)
						if len(parts) > 1 {
							extractedFeedback := strings.TrimSpace(parts[1])
							variableFeedback = extractedFeedback
							if revisionAttempt >= maxVariableRevisions {
								hcpo.GetLogger().Warnf("⚠️ Max variable extraction revision attempts (%d) reached", maxVariableRevisions)
								templatedObjective = objective
								break
							}
							continue
						}
					}
					hcpo.GetLogger().Warnf("⚠️ Variable extraction failed: %v", err)
					templatedObjective = objective
					break
				}

				// Request human approval for extracted variables
				approved, feedback, err := hcpo.requestVariableApproval(ctx, variablesManifest, revisionAttempt)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Variable approval request failed: %v, will retry", err)
					approved = false
					feedback = fmt.Sprintf("Error getting approval: %v", err)
				}

				if approved {
					hcpo.GetLogger().Infof("✅ Variables approved by human")
					hcpo.emitVariablesExtractedEvent(ctx, variablesManifest.Variables, templatedObjective)
					// Mark variables as existing so the CREATE mode loop doesn't run again
					variablesExist = true
					break
				}

				// Variables rejected with feedback for revision
				hcpo.GetLogger().Infof("🔄 Variable revision requested (attempt %d/%d): %s", revisionAttempt, maxVariableRevisions, feedback)
				variableFeedback = feedback

				if revisionAttempt >= maxVariableRevisions {
					hcpo.GetLogger().Warnf("⚠️ Max variable revision attempts (%d) reached", maxVariableRevisions)
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
			hcpo.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

			var err error
			variablesManifest, templatedObjective, variableConversationHistory, err = hcpo.runVariableExtractionPhase(ctx, revisionAttempt, variableFeedback, variableConversationHistory, nil)
			if err != nil {
				errMsg := err.Error()
				feedbackPrefix := "VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:"
				if strings.Contains(errMsg, feedbackPrefix) {
					parts := strings.Split(errMsg, feedbackPrefix)
					if len(parts) > 1 {
						extractedFeedback := strings.TrimSpace(parts[1])
						variableFeedback = extractedFeedback
						if revisionAttempt >= maxVariableRevisions {
							hcpo.GetLogger().Warnf("⚠️ Max variable extraction revision attempts (%d) reached", maxVariableRevisions)
							templatedObjective = objective
							break
						}
						continue
					}
				}
				hcpo.GetLogger().Warnf("⚠️ Variable extraction failed: %v", err)
				templatedObjective = objective
				break
			}

			// Request human approval for extracted variables
			approved, feedback, err := hcpo.requestVariableApproval(ctx, variablesManifest, revisionAttempt)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Variable approval request failed: %v, will retry", err)
				approved = false
				feedback = fmt.Sprintf("Error getting approval: %v", err)
			}

			if approved {
				hcpo.GetLogger().Infof("✅ Variables approved by human")
				hcpo.emitVariablesExtractedEvent(ctx, variablesManifest.Variables, templatedObjective)
				// Mark variables as existing so the loop doesn't run again
				variablesExist = true
				break
			}

			// Variables rejected with feedback for revision
			hcpo.GetLogger().Infof("🔄 Variable revision requested (attempt %d/%d): %s", revisionAttempt, maxVariableRevisions, feedback)
			variableFeedback = feedback

			if revisionAttempt >= maxVariableRevisions {
				hcpo.GetLogger().Warnf("⚠️ Max variable revision attempts (%d) reached", maxVariableRevisions)
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

// ExtractVariablesForPlanning runs variable extraction as part of the planning workflow
// Returns: (variablesManifest, templatedObjective, shouldEmitEvent, error)
// shouldEmitEvent indicates if VariablesExtractedEvent should be emitted (true if variables were extracted/used)
func (hcpo *HumanControlledTodoPlannerOrchestrator) ExtractVariablesForPlanning(ctx context.Context, objective string, planExists bool) (*VariablesManifest, string, bool, error) {
	hcpo.GetLogger().Infof("🔍 Starting variable extraction for planning workflow")

	// Check if variables.json already exists
	variablesPath := fmt.Sprintf("%s/variables/variables.json", hcpo.GetWorkspacePath())
	variablesExist, existingVariablesManifest, err := hcpo.checkExistingVariables(ctx, variablesPath)
	if err != nil {
		hcpo.GetLogger().Warnf("⚠️ Failed to check for existing variables: %w", err)
		variablesExist = false
	}

	var variablesManifest *VariablesManifest
	var templatedObjective string
	var shouldEmitEvent bool

	// If variables exist, ask user if they want to use them, extract new ones, or update existing
	if variablesExist {
		requestID := fmt.Sprintf("existing_variables_decision_%d", time.Now().UnixNano())
		variableOptions := []string{
			"Use Existing Variables",    // Option 0: Use existing variables as-is
			"Extract New Variables",     // Option 1: Delete everything and extract new
			"Update Existing Variables", // Option 2: Update existing variables with feedback
		}
		variableChoice, err := hcpo.RequestMultipleChoiceFeedback(
			ctx,
			requestID,
			"Found existing variables.json. What would you like to do?",
			variableOptions,
			fmt.Sprintf("Variables file: %s\nFound %d variables", variablesPath, len(existingVariablesManifest.Variables)),
			hcpo.getSessionID(),
			hcpo.getWorkflowID(),
		)
		if err != nil {
			hcpo.GetLogger().Warnf("⚠️ Failed to get user decision for existing variables: %w", err)
			variableChoice = "option0"
		}

		switch variableChoice {
		case "option0":
			// Use existing variables
			hcpo.GetLogger().Infof("✅ User chose to use existing variables")
			variablesManifest = existingVariablesManifest
			hcpo.variablesManifest = existingVariablesManifest
			templatedObjective = existingVariablesManifest.Objective
			// Emit event only if plan doesn't exist (if plan exists, event was already emitted together)
			if !planExists {
				shouldEmitEvent = true
			}

		case "option1":
			// Extract new variables
			hcpo.GetLogger().Infof("🔄 User chose to extract new variables, cleaning up existing variables file")
			if err := hcpo.DeleteWorkspaceFile(ctx, variablesPath); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to delete existing variables file: %v (will be overwritten during extraction)", err)
			} else {
				hcpo.GetLogger().Infof("🗑️ Deleted existing variables file: %s", variablesPath)
			}
			variablesExist = false // Trigger variable extraction

		case "option2":
			// Update existing variables
			hcpo.GetLogger().Infof("🔄 User chose to update existing variables, requesting update feedback")

			var variablesSummary strings.Builder
			variablesSummary.WriteString(fmt.Sprintf("Current variables (%d total):\n\n", len(existingVariablesManifest.Variables)))
			for _, variable := range existingVariablesManifest.Variables {
				variablesSummary.WriteString(fmt.Sprintf("- **{{%s}}**: %s\n", variable.Name, variable.Description))
				variablesSummary.WriteString(fmt.Sprintf("  - Value: %s\n", variable.Value))
				variablesSummary.WriteString("\n")
			}
			variablesSummary.WriteString(fmt.Sprintf("\n**Templated Objective**:\n%s", existingVariablesManifest.Objective))

			updateFeedbackID := fmt.Sprintf("variable_update_feedback_%d", time.Now().UnixNano())
			approved, updateFeedback, err := hcpo.RequestHumanFeedback(
				ctx,
				updateFeedbackID,
				"What would you like to update in the existing variables? Please describe the changes or improvements you want.",
				fmt.Sprintf("Current variables location: %s\nFound %d variables\n\n%s\n\nYour feedback will be used to guide the update of variables while preserving unchanged ones.", variablesPath, len(existingVariablesManifest.Variables), variablesSummary.String()),
				hcpo.getSessionID(),
				hcpo.getWorkflowID(),
			)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to get update feedback: %v, proceeding without specific update guidance", err)
				updateFeedback = ""
			} else if approved {
				hcpo.GetLogger().Infof("ℹ️ User approved without providing update feedback, will update variables without specific guidance")
				updateFeedback = ""
			} else if updateFeedback != "" {
				hcpo.GetLogger().Infof("📝 Received update feedback: %s", updateFeedback)
			}

			variablesExist = false
			existingVariablesForUpdate := existingVariablesManifest
			initialUpdateFeedback := updateFeedback

			// Run variable extraction in update mode
			maxVariableRevisions := 10
			var variableFeedback string
			var variableConversationHistory []llmtypes.MessageContent

			variableFeedback = initialUpdateFeedback
			hcpo.GetLogger().Infof("📝 Using initial update feedback for first extraction attempt: %s", variableFeedback)

			for revisionAttempt := 1; revisionAttempt <= maxVariableRevisions; revisionAttempt++ {
				hcpo.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

				var err error
				variablesManifest, templatedObjective, variableConversationHistory, err = hcpo.runVariableExtractionPhase(ctx, revisionAttempt, variableFeedback, variableConversationHistory, existingVariablesForUpdate)
				if err != nil {
					errMsg := err.Error()
					feedbackPrefix := "VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:"
					if strings.Contains(errMsg, feedbackPrefix) {
						parts := strings.Split(errMsg, feedbackPrefix)
						if len(parts) > 1 {
							extractedFeedback := strings.TrimSpace(parts[1])
							variableFeedback = extractedFeedback
							if revisionAttempt >= maxVariableRevisions {
								hcpo.GetLogger().Warnf("⚠️ Max variable extraction revision attempts (%d) reached, continuing without variables", maxVariableRevisions)
								templatedObjective = objective
								break
							}
							continue
						}
					}
					hcpo.GetLogger().Warnf("⚠️ Variable extraction failed: %v, continuing without variables", err)
					templatedObjective = objective
					break
				}

				// In UPDATE mode, if runVariableExtractionPhase returns successfully, variables were already approved via human_feedback tool
				// So we skip requestVariableApproval and proceed directly
				if existingVariablesForUpdate != nil {
					// UPDATE mode: Variables already approved via human_feedback tool, proceed directly
					hcpo.GetLogger().Infof("✅ Variables updated and approved via human_feedback tool in UPDATE mode, proceeding to planning")
					shouldEmitEvent = true
					break
				}

				// CREATE mode: Request approval via separate approval dialog
				approved, feedback, err := hcpo.requestVariableApproval(ctx, variablesManifest, revisionAttempt)
				if err != nil {
					hcpo.GetLogger().Warnf("⚠️ Variable approval request failed: %v, will retry", err)
					approved = false
					feedback = fmt.Sprintf("Error getting approval: %v", err)
				}

				if approved {
					hcpo.GetLogger().Infof("✅ Variables approved by human, proceeding to planning")
					shouldEmitEvent = true
					break
				}

				hcpo.GetLogger().Infof("🔄 Variable revision requested (attempt %d/%d): %s", revisionAttempt, maxVariableRevisions, feedback)
				variableFeedback = feedback

				if revisionAttempt >= maxVariableRevisions {
					hcpo.GetLogger().Warnf("⚠️ Max variable revision attempts (%d) reached, using extracted variables", maxVariableRevisions)
					break
				}
			}

		default:
			hcpo.GetLogger().Warnf("⚠️ Unknown variable choice: %s, defaulting to use existing variables", variableChoice)
			variablesManifest = existingVariablesManifest
			hcpo.variablesManifest = existingVariablesManifest
			templatedObjective = existingVariablesManifest.Objective
		}
	}

	// Extract variables if they don't exist or user wants to re-extract
	if !variablesExist {
		maxVariableRevisions := 10
		var variableFeedback string
		var variableConversationHistory []llmtypes.MessageContent

		for revisionAttempt := 1; revisionAttempt <= maxVariableRevisions; revisionAttempt++ {
			hcpo.GetLogger().Infof("🔄 Variable extraction attempt %d/%d", revisionAttempt, maxVariableRevisions)

			var err error
			variablesManifest, templatedObjective, variableConversationHistory, err = hcpo.runVariableExtractionPhase(ctx, revisionAttempt, variableFeedback, variableConversationHistory, nil)
			if err != nil {
				errMsg := err.Error()
				feedbackPrefix := "VARIABLE_EXTRACTION_TEXT_RESPONSE_FEEDBACK:"
				if strings.Contains(errMsg, feedbackPrefix) {
					parts := strings.Split(errMsg, feedbackPrefix)
					if len(parts) > 1 {
						extractedFeedback := strings.TrimSpace(parts[1])
						variableFeedback = extractedFeedback
						if revisionAttempt >= maxVariableRevisions {
							hcpo.GetLogger().Warnf("⚠️ Max variable extraction revision attempts (%d) reached, continuing without variables", maxVariableRevisions)
							templatedObjective = objective
							break
						}
						continue
					}
				}
				hcpo.GetLogger().Warnf("⚠️ Variable extraction failed: %v, continuing without variables", err)
				templatedObjective = objective
				break
			}

			approved, feedback, err := hcpo.requestVariableApproval(ctx, variablesManifest, revisionAttempt)
			if err != nil {
				hcpo.GetLogger().Warnf("⚠️ Variable approval request failed: %v, will retry", err)
				approved = false
				feedback = fmt.Sprintf("Error getting approval: %v", err)
			}

			if approved {
				hcpo.GetLogger().Infof("✅ Variables approved by human, proceeding to planning")
				shouldEmitEvent = true
				break
			}

			hcpo.GetLogger().Infof("🔄 Variable revision requested (attempt %d/%d): %s", revisionAttempt, maxVariableRevisions, feedback)
			variableFeedback = feedback

			if revisionAttempt >= maxVariableRevisions {
				hcpo.GetLogger().Warnf("⚠️ Max variable revision attempts (%d) reached, using extracted variables", maxVariableRevisions)
				break
			}
		}
	}

	// Store manifest in orchestrator for access by other methods
	if variablesManifest != nil {
		hcpo.variablesManifest = variablesManifest
	}

	return variablesManifest, templatedObjective, shouldEmitEvent, nil
}
