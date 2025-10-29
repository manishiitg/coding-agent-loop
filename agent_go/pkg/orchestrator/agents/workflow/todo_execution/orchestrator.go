package todo_execution

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// TodoStepsExtractedEvent represents todo steps extracted from a plan
type TodoStepsExtractedEvent struct {
	events.BaseEventData
	TotalStepsExtracted int        `json:"total_steps_extracted"`
	ExtractedSteps      []TodoStep `json:"extracted_steps"`
	ExtractionMethod    string     `json:"extraction_method"`
	PlanSource          string     `json:"plan_source"`
}

// GetEventType implements events.EventData interface
func (e *TodoStepsExtractedEvent) GetEventType() events.EventType {
	return events.TodoStepsExtracted
}

// TodoExecutionOrchestrator manages the multi-agent todo execution process
type TodoExecutionOrchestrator struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
}

// NewTodoExecutionOrchestrator creates a new multi-agent todo execution orchestrator
func NewTodoExecutionOrchestrator(
	provider string,
	model string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	selectedTools []string, // NEW parameter
	mcpConfigPath string,
	llmConfig *orchestrator.LLMConfig,
	maxTurns int,
	logger utils.ExtendedLogger,
	_ observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
) (*TodoExecutionOrchestrator, error) {

	// Create base workflow orchestrator
	baseOrchestrator, err := orchestrator.NewBaseOrchestrator(
		logger,
		eventBridge,
		orchestrator.OrchestratorTypeWorkflow,
		provider,
		model,
		mcpConfigPath,
		temperature,
		agentMode,
		selectedServers,
		selectedTools, // Pass through actual selected tools
		llmConfig,     // llmConfig passed from caller
		maxTurns,
		customTools,
		customToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create base orchestrator: %w", err)
	}

	return &TodoExecutionOrchestrator{
		BaseOrchestrator: baseOrchestrator,
	}, nil
}

// ExecuteTodos orchestrates the multi-agent todo execution process
func (teo *TodoExecutionOrchestrator) ExecuteTodos(ctx context.Context, objective, workspacePath, runOption string) (string, error) {
	teo.GetLogger().Infof("🚀 Starting multi-agent todo execution for objective: %s", objective)

	// Set objective and workspace path directly
	teo.SetObjective(objective)

	// Resolve selected run folder based on run option (in Go code, not in prompts)
	selectedRunFolder, err := teo.resolveSelectedRunFolder(ctx, workspacePath, runOption)
	if err != nil {
		return "", fmt.Errorf("failed to resolve run folder: %w", err)
	}
	teo.GetLogger().Infof("📁 Selected run folder: %s", selectedRunFolder)

	// Set workspace path to include the run folder
	runWorkspacePath := filepath.Join(workspacePath, "runs", selectedRunFolder)
	teo.SetWorkspacePath(runWorkspacePath)

	// Parse todo_final.md into structured steps using plan reader agent
	teo.GetLogger().Infof("📖 Parsing todo_final.md into structured steps using plan reader agent")
	// Read todo_final.md from workspace root (not from run folder)
	todoFinalPath := filepath.Join(workspacePath, "todo_final.md")
	content, err := teo.ReadWorkspaceFile(ctx, todoFinalPath)
	if err != nil {
		return "", fmt.Errorf("failed to read todo_final.md: %w", err)
	}

	// Revision loop for plan reader with human feedback
	maxPlanRevisions := 5
	var humanFeedback string
	var conversationHistory []llms.MessageContent
	var steps []TodoStep

	for revisionAttempt := 1; revisionAttempt <= maxPlanRevisions; revisionAttempt++ {
		teo.GetLogger().Infof("🔄 Plan reader revision attempt %d/%d", revisionAttempt, maxPlanRevisions)

		// Use plan reader agent to parse todo_final.md
		planReaderAgent, err := teo.createPlanReaderAgent(ctx, revisionAttempt)
		if err != nil {
			return "", fmt.Errorf("failed to create plan reader agent: %w", err)
		}

		// Prepare template variables for plan reader agent
		templateVars := map[string]string{
			"Objective":     objective,
			"WorkspacePath": workspacePath,
			"PlanMarkdown":  content,
			"FileType":      "todo_final",
		}

		// Add human feedback to conversation if provided
		if humanFeedback != "" {
			feedbackMessage := llms.MessageContent{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextContent{Text: humanFeedback}},
			}
			conversationHistory = append(conversationHistory, feedbackMessage)
			teo.GetLogger().Infof("📝 Added human feedback to conversation history for revision %d", revisionAttempt)
		}

		// Execute plan reader agent to get structured response
		// The agent will detect variables and use human_feedback tool internally
		// The agent handles variable resolution internally and doesn't need conversation history accumulation
		planningResponse, err := planReaderAgent.ExecuteStructured(ctx, templateVars, conversationHistory)
		if err != nil {
			return "", fmt.Errorf("failed to parse todo_final.md with plan reader agent: %w", err)
		}

		// Convert PlanningResponse.Steps to TodoStep array
		steps = make([]TodoStep, len(planningResponse.Steps))
		for i, step := range planningResponse.Steps {
			steps[i] = TodoStep{
				Title:               step.Title,
				Description:         step.Description,
				SuccessCriteria:     step.SuccessCriteria,
				WhyThisStep:         step.WhyThisStep,
				ContextDependencies: step.ContextDependencies,
				ContextOutput:       string(step.ContextOutput), // Convert FlexibleContextOutput to string
				SuccessPatterns:     step.SuccessPatterns,
				FailurePatterns:     step.FailurePatterns,
			}
		}

		teo.GetLogger().Infof("📋 Parsed %d steps from todo_final.md (attempt %d)", len(steps), revisionAttempt)

		// Emit todo steps extracted event (so frontend can display the extracted steps)
		teo.emitTodoStepsExtractedEvent(ctx, steps, "todo_final_md")

		// Request human approval for the extracted steps
		approved, feedback, err := teo.requestStepsApproval(ctx, steps, revisionAttempt)
		if err != nil {
			return "", fmt.Errorf("failed to get approval for extracted steps: %w", err)
		}

		if approved {
			teo.GetLogger().Infof("✅ Steps approved by user on attempt %d, proceeding with execution", revisionAttempt)
			break // Exit revision loop
		}

		// User rejected with feedback - prepare for retry
		teo.GetLogger().Infof("🔄 User requested revision (attempt %d/%d): %s", revisionAttempt, maxPlanRevisions, feedback)
		humanFeedback = feedback // Store feedback for next attempt

		if revisionAttempt >= maxPlanRevisions {
			return fmt.Sprintf("Max plan reader revision attempts (%d) reached. Final feedback: %s", maxPlanRevisions, feedback), nil
		}
	}

	// Execute each step individually with validation feedback loop
	var executionResults []string
	var validationResults []string

	for i, step := range steps {
		teo.GetLogger().Infof("🔄 Executing step %d/%d: %s", i+1, len(steps), step.Title)

		var executionResult string
		var validationResult string
		maxAttempts := 3
		attempt := 1

		for attempt <= maxAttempts {
			teo.GetLogger().Infof("🔄 Attempt %d/%d for step %d", attempt, maxAttempts, i+1)

			// Execute this specific step
			var err error
			var conversationHistory []llms.MessageContent
			executionResult, conversationHistory, err = teo.runStepExecutionPhase(ctx, step, i+1, len(steps), selectedRunFolder, runOption, validationResult)
			if err != nil {
				teo.GetLogger().Warnf("⚠️ Step %d execution failed (attempt %d): %v", i+1, attempt, err)
				executionResult = fmt.Sprintf("Step %d execution failed (attempt %d): %v", i+1, attempt, err)
				conversationHistory = nil
			}

			// Validate this specific step
			validationResponse, err := teo.runStepValidationPhase(ctx, step, i+1, len(steps), executionResult, conversationHistory)
			if err != nil {
				teo.GetLogger().Warnf("⚠️ Step %d validation failed (attempt %d): %v", i+1, attempt, err)
				validationResult = fmt.Sprintf("Step %d validation failed (attempt %d): %v", i+1, attempt, err)
				break
			}

			// Check if validation passed
			if validationResponse.IsObjectiveSuccessCriteriaMet {
				teo.GetLogger().Infof("✅ Step %d completed successfully on attempt %d", i+1, attempt)
				validationResult = fmt.Sprintf("Step %d validation passed: %s", i+1, validationResponse.Feedback)
				break
			} else {
				teo.GetLogger().Infof("⚠️ Step %d validation failed on attempt %d: %s", i+1, attempt, validationResponse.Feedback)
				validationResult = validationResponse.Feedback

				if attempt < maxAttempts {
					teo.GetLogger().Infof("🔄 Retrying step %d with feedback: %s", i+1, validationResponse.Feedback)
				} else {
					teo.GetLogger().Warnf("❌ Step %d failed after %d attempts", i+1, maxAttempts)
					validationResult = fmt.Sprintf("Step %d failed after %d attempts. Final feedback: %s", i+1, maxAttempts, validationResponse.Feedback)
				}
			}

			attempt++
		}

		executionResults = append(executionResults, executionResult)
		validationResults = append(validationResults, validationResult)
	}

	duration := time.Since(teo.GetStartTime())
	teo.GetLogger().Infof("✅ Multi-agent todo execution completed in %v", duration)

	return teo.formatStepResults(executionResults, validationResults, len(steps)), nil
}

// runStepExecutionPhase executes a single step using the execution agent
func (teo *TodoExecutionOrchestrator) runStepExecutionPhase(ctx context.Context, step TodoStep, stepNumber, totalSteps int, selectedRunFolder, runOption, previousFeedback string) (string, []llms.MessageContent, error) {
	executionAgent, err := teo.createExecutionAgent(ctx, step.Title, stepNumber, 0)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create execution agent: %w", err)
	}

	// Prepare template variables for this specific step
	templateVars := map[string]string{
		"StepNumber":              fmt.Sprintf("%d", stepNumber),
		"TotalSteps":              fmt.Sprintf("%d", totalSteps),
		"StepTitle":               step.Title,
		"StepDescription":         step.Description,
		"StepSuccessCriteria":     step.SuccessCriteria,
		"StepWhyThisStep":         step.WhyThisStep,
		"StepContextDependencies": strings.Join(step.ContextDependencies, ", "),
		"StepContextOutput":       step.ContextOutput,
		"StepSuccessPatterns":     strings.Join(step.SuccessPatterns, "\n- "),
		"StepFailurePatterns":     strings.Join(step.FailurePatterns, "\n- "),
		"WorkspacePath":           teo.GetWorkspacePath(), // This now includes runs/{folder}
		"RunOption":               runOption,
		"PreviousFeedback":        previousFeedback,
	}

	executionResult, conversationHistory, err := executionAgent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", nil, fmt.Errorf("step %d execution failed: %w", stepNumber, err)
	}

	// Store execution result with conversation history
	// Format: "EXECUTION_RESULT:|<result>|CONVERSATION_HISTORY:|<history_json>"
	// This allows validation agent to access both the result and the full conversation
	return executionResult, conversationHistory, nil
}

// runStepValidationPhase validates a single step's execution using the validation agent
func (teo *TodoExecutionOrchestrator) runStepValidationPhase(ctx context.Context, step TodoStep, stepNumber, totalSteps int, executionResult string, conversationHistory []llms.MessageContent) (*ValidationResponse, error) {
	validationAgent, err := teo.createValidationAgent(ctx, step.Title, stepNumber, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create validation agent: %w", err)
	}

	// Cast to TodoValidationAgent to access ExecuteStructured method
	todoValidationAgent, ok := validationAgent.(*TodoValidationAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast validation agent to TodoValidationAgent")
	}

	// Format conversation history as string for template variable
	conversationHistoryStr := formatConversationHistory(conversationHistory)

	// Prepare template variables for this specific step
	templateVars := map[string]string{
		"StepNumber":          fmt.Sprintf("%d", stepNumber),
		"TotalSteps":          fmt.Sprintf("%d", totalSteps),
		"StepTitle":           step.Title,
		"StepDescription":     step.Description,
		"StepSuccessCriteria": step.SuccessCriteria,
		"WorkspacePath":       teo.GetWorkspacePath(), // This now includes runs/{folder}
		"ExecutionOutput":     conversationHistoryStr, // Pass conversation history instead of just result
	}

	validationResponse, err := todoValidationAgent.ExecuteStructured(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, fmt.Errorf("step %d validation failed: %w", stepNumber, err)
	}

	return validationResponse, nil
}

// formatStepResults formats the step-by-step execution results
func (teo *TodoExecutionOrchestrator) formatStepResults(executionResults, validationResults []string, totalSteps int) string {
	return fmt.Sprintf(`# Todo Execution Report

## Execution Summary
- **Objective**: %s
- **Duration**: %v
- **Workspace**: %s
- **Total Steps**: %d
- **Steps Executed**: %d

## Step-by-Step Results

%s

## Overall Status
All steps have been executed and validated individually. Each step was processed with its specific Success Patterns and Failure Patterns from the structured todo_final.md format.

## Evidence Files
- **Runs Folder**: %s/runs/
- **Execution Logs**: Available in runs folder
- **Results**: Available in runs folder
- **Evidence**: Available in runs folder

Focus on executing each step effectively using proven approaches and avoiding failed patterns.`,
		teo.GetObjective(), time.Since(teo.GetStartTime()), teo.GetWorkspacePath(), totalSteps, len(executionResults),
		func() string {
			var result strings.Builder
			for i := 0; i < len(executionResults); i++ {
				result.WriteString(fmt.Sprintf("### Step %d\n", i+1))
				result.WriteString(fmt.Sprintf("**Execution**: %s\n\n", executionResults[i]))
				if i < len(validationResults) {
					result.WriteString(fmt.Sprintf("**Validation**: %s\n\n", validationResults[i]))
				}
			}
			return result.String()
		}(), teo.GetWorkspacePath())
}

// Agent creation methods
func (teo *TodoExecutionOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup
	agent, err := teo.CreateAndSetupStandardAgent(
		ctx,
		"todo_execution",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoExecutionAgent(config, logger, tracer, eventBridge)
		},
		teo.WorkspaceTools,
		teo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (teo *TodoExecutionOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int) (agents.OrchestratorAgent, error) {
	// Use combined standardized agent creation and setup with no MCP servers
	// Validation agent only reads execution outputs and writes validation reports using workspace tools
	agent, err := teo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"validation-agent",
		phase,
		step,
		iteration,
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers - validation agent only uses workspace tools to read/write files
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewTodoValidationAgent(config, logger, tracer, eventBridge)
		},
		teo.WorkspaceTools,
		teo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// createPlanReaderAgent creates a plan reader agent for parsing todo_final.md
func (teo *TodoExecutionOrchestrator) createPlanReaderAgent(ctx context.Context, revisionAttempt int) (*PlanReaderAgent, error) {
	// Use CreateAndSetupStandardAgentWithCustomServers instead of manual initialization
	// This ensures custom tools (workspace + human) are properly registered
	agentInterface, err := teo.CreateAndSetupStandardAgentWithCustomServers(
		ctx,
		"plan-reader-agent",
		"plan_reading",
		0,               // No step number (plan reader reads all steps)
		revisionAttempt, // Use revision attempt as iteration
		teo.GetMaxTurns(),
		agents.OutputFormatStructured,
		[]string{mcpclient.NoServers}, // No MCP servers - plan reader only converts markdown to JSON using workspace tools
		func(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewPlanReaderAgent(config, logger, tracer, eventBridge)
		},
		teo.WorkspaceTools,
		teo.WorkspaceToolExecutors,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan reader agent: %w", err)
	}

	// Cast to PlanReaderAgent
	agent, ok := agentInterface.(*PlanReaderAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast agent to PlanReaderAgent")
	}

	return agent, nil
}

// Execute implements the Orchestrator interface
func (teo *TodoExecutionOrchestrator) Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error) {
	// Validate workspace path is provided
	if workspacePath == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	// Extract run option from options
	runOption := "create_new_runs_always" // default
	if ro, ok := options["RunOption"].(string); ok && ro != "" {
		runOption = ro
	}

	// Call the existing ExecuteTodos method
	return teo.ExecuteTodos(ctx, objective, workspacePath, runOption)
}

// GetType returns the orchestrator type
func (teo *TodoExecutionOrchestrator) GetType() string {
	return "todo_execution"
}

// resolveSelectedRunFolder determines which run folder to use based on the run option
func (teo *TodoExecutionOrchestrator) resolveSelectedRunFolder(ctx context.Context, workspacePath, runOption string) (string, error) {
	runsPath := filepath.Join(workspacePath, "runs")

	// Get current date for dated folders
	today := time.Now().Format("2006-01-02")

	switch runOption {
	case "use_same_run":
		// Check if runs directory exists
		exists, _ := teo.workspaceFileExists(ctx, runsPath)
		if !exists {
			// Create initial run folder
			selectedFolder := "initial"
			if err := teo.createRunFolderStructure(ctx, filepath.Join(runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// List existing run folders
		existingFolders, err := teo.listRunFolders(ctx, runsPath)
		if err != nil || len(existingFolders) == 0 {
			// Create initial folder if none exist
			selectedFolder := "initial"
			if err := teo.createRunFolderStructure(ctx, filepath.Join(runsPath, selectedFolder)); err != nil {
				return "", err
			}
			return selectedFolder, nil
		}

		// Return the latest folder (alphabetically sorted, so latest date/name)
		sort.Strings(existingFolders)
		return existingFolders[len(existingFolders)-1], nil

	case "create_new_runs_always":
		// Always create a new dated folder with incremental number
		counter := 1
		for {
			selectedFolder := fmt.Sprintf("%s-iteration-%d", today, counter)
			fullPath := filepath.Join(runsPath, selectedFolder)

			exists, _ := teo.workspaceFileExists(ctx, fullPath)
			if !exists {
				if err := teo.createRunFolderStructure(ctx, fullPath); err != nil {
					return "", err
				}
				return selectedFolder, nil
			}
			counter++
		}

	case "create_new_run_once_daily":
		// Check if today's folder exists
		prefix := today + "-"
		existingFolders, _ := teo.listRunFolders(ctx, runsPath)

		// Look for today's folder
		for _, folder := range existingFolders {
			if strings.HasPrefix(folder, prefix) {
				teo.GetLogger().Infof("📁 Using existing today's run folder: %s", folder)
				return folder, nil
			}
		}

		// Create new folder for today
		selectedFolder := fmt.Sprintf("%s-initial", today)
		fullPath := filepath.Join(runsPath, selectedFolder)
		if err := teo.createRunFolderStructure(ctx, fullPath); err != nil {
			return "", err
		}
		return selectedFolder, nil

	default:
		return "", fmt.Errorf("unknown run option: %s", runOption)
	}
}

// workspaceFileExists checks if a file or directory exists in the workspace
func (teo *TodoExecutionOrchestrator) workspaceFileExists(ctx context.Context, path string) (bool, error) {
	// Try to list the directory to check if it exists
	_, err := teo.ReadWorkspaceFile(ctx, filepath.Join(path, ".keep"))
	if err == nil {
		return true, nil
	}

	// Try to read the directory itself by listing parent
	parent := filepath.Dir(path)
	filename := filepath.Base(path)

	// List files in parent directory
	files, err := teo.listWorkspaceFiles(ctx, parent)
	if err != nil {
		return false, err
	}

	for _, file := range files {
		if file == filename || strings.HasPrefix(file, filename) {
			return true, nil
		}
	}

	return false, nil
}

// listWorkspaceFiles lists files in a directory (helper for workspaceFileExists)
func (teo *TodoExecutionOrchestrator) listWorkspaceFiles(ctx context.Context, path string) ([]string, error) {
	// This is a simplified version - in production, you'd use actual workspace tools
	// For now, return empty list to trigger folder creation
	return []string{}, nil
}

// listRunFolders lists existing run folder names
func (teo *TodoExecutionOrchestrator) listRunFolders(ctx context.Context, runsPath string) ([]string, error) {
	// This would typically use workspace tools to list directories
	// For now, return empty to trigger creation
	return []string{}, nil
}

// createRunFolderStructure creates the basic structure for a run folder
func (teo *TodoExecutionOrchestrator) createRunFolderStructure(ctx context.Context, runPath string) error {
	// Create .keep file to ensure directory is created
	keepFile := filepath.Join(runPath, ".keep")
	if err := teo.WriteWorkspaceFile(ctx, keepFile, "# This file ensures the run folder exists"); err != nil {
		return fmt.Errorf("failed to create run folder: %w", err)
	}

	// The actual folder creation will happen when files are written
	teo.GetLogger().Infof("✅ Created run folder structure: %s", runPath)
	return nil
}

// formatConversationHistory formats conversation history for template usage
func formatConversationHistory(conversationHistory []llms.MessageContent) string {
	var result strings.Builder

	for _, message := range conversationHistory {
		// Skip system messages
		if message.Role == llms.ChatMessageTypeSystem {
			continue
		}

		switch message.Role {
		case llms.ChatMessageTypeHuman:
			result.WriteString("## Human Message\n")
		case llms.ChatMessageTypeAI:
			result.WriteString("## Assistant Response\n")
		case llms.ChatMessageTypeTool:
			result.WriteString("## Tool Response\n")
		default:
			result.WriteString("## Message\n")
		}

		for _, part := range message.Parts {
			switch p := part.(type) {
			case llms.TextContent:
				result.WriteString(p.Text)
				result.WriteString("\n\n")
			case llms.ToolCall:
				result.WriteString("### Tool Call\n")
				result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.FunctionCall.Name))
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ID))
				if p.FunctionCall.Arguments != "" {
					result.WriteString(fmt.Sprintf("**Arguments:** %s\n", p.FunctionCall.Arguments))
				}
				result.WriteString("\n")
			case llms.ToolCallResponse:
				result.WriteString("### Tool Response\n")
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ToolCallID))
				if p.Name != "" {
					result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.Name))
				}
				result.WriteString(fmt.Sprintf("**Response:** %s\n", p.Content))
				result.WriteString("\n")
			default:
				// Handle any other content types
				result.WriteString(fmt.Sprintf("**Unknown Content Type:** %T\n", p))
			}
		}
		result.WriteString("---\n\n")
	}

	return result.String()
}

// emitTodoStepsExtractedEvent emits an event when todo steps are extracted from todo_final.md
func (teo *TodoExecutionOrchestrator) emitTodoStepsExtractedEvent(ctx context.Context, extractedSteps []TodoStep, planSource string) {
	if teo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &TodoStepsExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		TotalStepsExtracted: len(extractedSteps),
		ExtractedSteps:      extractedSteps,
		ExtractionMethod:    "plan_reader_agent",
		PlanSource:          planSource,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.TodoStepsExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := teo.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		teo.GetLogger().Warnf("⚠️ Failed to emit todo steps extracted event: %v", err)
	} else {
		teo.GetLogger().Infof("✅ Emitted todo steps extracted event: %d steps extracted", len(extractedSteps))
	}
}

// requestStepsApproval requests human approval for extracted steps before execution
// Returns: (approved bool, feedback string, error)
func (teo *TodoExecutionOrchestrator) requestStepsApproval(ctx context.Context, steps []TodoStep, revisionAttempt int) (bool, string, error) {
	teo.GetLogger().Infof("⏸️ Requesting human approval for %d extracted steps (revision attempt %d)", len(steps), revisionAttempt)

	// Generate unique request ID
	requestID := fmt.Sprintf("steps_approval_%d_%d", revisionAttempt, time.Now().UnixNano())

	// Request human approval using base orchestrator method
	// Simple question without detailed context (details are in the event)
	var question string
	if revisionAttempt == 1 {
		question = fmt.Sprintf("Review the %d extracted steps and approve to proceed with execution, or provide feedback for revision.", len(steps))
	} else {
		question = fmt.Sprintf("Review the revised steps (attempt %d). Approve to proceed or provide additional feedback.", revisionAttempt)
	}

	return teo.RequestHumanFeedback(
		ctx,
		requestID,
		question,
		"Steps have been extracted and displayed above. Can we proceed with execution?", // Simple context
		"todo_execution_session",
		teo.GetObjective(),
	)
}
