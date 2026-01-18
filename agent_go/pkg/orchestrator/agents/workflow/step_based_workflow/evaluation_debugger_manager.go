package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "mcpagent/agent"
	baseevents "mcpagent/events"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for evaluation debugger
var evaluationDebuggerSystemTemplate = MustRegisterTemplate("evaluationDebuggerSystem", `# Evaluation Debugger Agent

You are an evaluation debugging assistant. Help the user improve their evaluation plan based on execution results.

## 🤖 ROLE
Answer user questions about the evaluation plan and report. Make improvements to the evaluation plan when requested.

## ⚠️ RULES
1. **Answer Directly**: For general questions, answer from the context provided below.
2. **Confirm Before Changes**: Use 'human_feedback' BEFORE any plan modifications.
3. **Read Files Only When Needed**: Only read logs/files if user asks for deep debugging.
4. **Concrete Criteria**: When updating success criteria, make them file-verifiable and specific.

## 📋 CONTEXT

### Workspace Information
- **Workspace**: {{.WorkspacePath}}
- **Selected Run**: {{.RunPathRelative}}

### Evaluation Plan
{{if .EvaluationPlanJSON}}` + "```json\n{{.EvaluationPlanJSON}}\n```" + `{{else}}No evaluation plan provided.{{end}}

### Evaluation Report (Latest)
{{if .EvaluationReportJSON}}` + "```json\n{{.EvaluationReportJSON}}\n```" + `{{else}}No evaluation report found for this run.{{end}}

### Execution Summary
{{.ExecutionResultsSummary}}

---

## 📁 FILE LOCATIONS
- **Evaluation Plan**: 'evaluation/evaluation_plan.json'
- **Evaluation Report**: 'evaluation/{{.RunPathRelative}}/evaluation_report.json'
- **Execution outputs**: '{{.RunPathRelative}}/execution/'
- **Learnings**: 'evaluation/learnings/'

## 🛠️ MODIFICATION TOOLS
Use 'update_evaluation_step', 'add_evaluation_step', 'delete_evaluation_steps' ONLY after user approval via 'human_feedback'.

## 📖 ANALYSIS GUIDE
- **Low Scores**: Check steps with scores < 50%. Read 'reasoning' in the report.
- **Criteria Issues**: If reasoning says "criteria too vague", update 'success_criteria' in the plan.
- **Missing Evidence**: If reasoning says "file not found", check if the step checks for the right output files.
`)

var evaluationDebuggerUserTemplate = MustRegisterTemplate("evaluationDebuggerUser", `{{if .UserRequest}}{{.UserRequest}}{{else}}What would you like to improve in the evaluation plan?{{end}}`)

// WorkflowEvaluationDebuggerAgent analyzes evaluation results and provides feedback
type WorkflowEvaluationDebuggerAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator
}

// NewWorkflowEvaluationDebuggerAgent creates a new evaluation debugger agent
func NewWorkflowEvaluationDebuggerAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *WorkflowEvaluationDebuggerAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerEvaluationDebuggerAgentType,
		eventBridge,
	)

	return &WorkflowEvaluationDebuggerAgent{
		BaseOrchestratorAgent: baseAgent,
		baseOrchestrator:      baseOrchestrator,
	}
}

// EvaluationDebuggerManager manages evaluation debugger agent creation
type EvaluationDebuggerManager struct {
	*orchestrator.BaseOrchestrator
	presetLLM  *AgentLLMConfig
	sessionID  string
	workflowID string
}

// NewEvaluationDebuggerManager creates a new EvaluationDebuggerManager
func NewEvaluationDebuggerManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *EvaluationDebuggerManager {
	return &EvaluationDebuggerManager{
		BaseOrchestrator: baseOrchestrator,
		presetLLM:        presetLLM,
		sessionID:        sessionID,
		workflowID:       workflowID,
	}
}

// EvaluationDebuggerOnly runs only the evaluation debugger phase
func (edm *EvaluationDebuggerManager) EvaluationDebuggerOnly(ctx context.Context, originalWorkspacePath string, runPath string) (string, error) {
	edm.GetLogger().Info(fmt.Sprintf("📊 Starting evaluation debugger for workspace: %s", originalWorkspacePath))

	// Validate run path
	if runPath == "" {
		return "", fmt.Errorf("run path is required for evaluation debugger")
	}
	// Basic validation of run path format
	if !strings.HasPrefix(runPath, "runs/") && !strings.HasPrefix(runPath, "iteration-") {
		// Try to fix it if it's just "iteration-X"
		runPath = "runs/" + runPath
	}

	// Set workspace path
	edm.SetWorkspacePath(originalWorkspacePath)

	// Read Evaluation Plan
	evalPlanPath := "evaluation/evaluation_plan.json"
	evalPlanContent, err := edm.ReadWorkspaceFile(ctx, evalPlanPath)
	if err != nil {
		return "", fmt.Errorf("evaluation plan not found at %s: %w", evalPlanPath, err)
	}

	// Read Evaluation Report for the run
	// Remove "runs/" prefix for report path construction if needed, but the standard path is evaluation/<run_folder>/evaluation_report.json
	// runPath usually is "runs/iteration-X". We want "iteration-X".
	relRunPath := strings.TrimPrefix(runPath, "runs/")
	if strings.HasPrefix(relRunPath, "/") {
		relRunPath = relRunPath[1:]
	}
	
	evalReportPath := fmt.Sprintf("evaluation/runs/%s/evaluation_report.json", relRunPath)
	evalReportContent, err := edm.ReadWorkspaceFile(ctx, evalReportPath)
	if err != nil {
		edm.GetLogger().Warn(fmt.Sprintf("⚠️ Evaluation report not found at %s: %v", evalReportPath, err))
		evalReportContent = "{}" // Continue with empty report
	}

	// Ask user what they want to improve
	userRequest, err := edm.requestUserGoal(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get user goal: %w", err)
	}

	// Create Agent
	agent, err := edm.createEvaluationDebuggerAgent(ctx, originalWorkspacePath, runPath)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	// Prepare template vars
	templateVars := map[string]string{
		"WorkspacePath":           originalWorkspacePath,
		"RunPathRelative":         relRunPath,
		"EvaluationPlanJSON":      evalPlanContent,
		"EvaluationReportJSON":    evalReportContent,
		"ExecutionResultsSummary": fmt.Sprintf("Run: %s", relRunPath),
		"UserRequest":             userRequest,
		"SessionID":               edm.sessionID,
		"WorkflowID":              edm.workflowID,
	}

	// Execute
	edm.GetLogger().Info(fmt.Sprintf("📊 Executing evaluation debugger agent..."))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	return result, nil
}

// createEvaluationDebuggerAgent creates the agent
func (edm *EvaluationDebuggerManager) createEvaluationDebuggerAgent(ctx context.Context, workspacePath string, runPath string) (agents.OrchestratorAgent, error) {
	// Paths
	evaluationPath := fmt.Sprintf("%s/evaluation", workspacePath)
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	
	// Read paths: evaluation/, runs/
	readPaths := []string{evaluationPath, runsPath}
	// Write paths: evaluation/ (to update plan)
	writePaths := []string{evaluationPath}

	edm.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	// LLM Config
	var llmConfigToUse *orchestrator.LLMConfig
	orchestratorLLMConfig := edm.GetLLMConfig()
	
	if edm.presetLLM != nil {
		var fallbacks []orchestrator.LLMModel
		var apiKeys *orchestrator.APIKeys
		if orchestratorLLMConfig != nil {
			fallbacks = orchestratorLLMConfig.Fallbacks
			apiKeys = orchestratorLLMConfig.APIKeys
		}
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: edm.presetLLM.Provider,
				ModelID:  edm.presetLLM.ModelID,
			},
			Fallbacks: fallbacks,
			APIKeys:   apiKeys,
		}
	} else {
		return nil, fmt.Errorf("no valid LLM config for evaluation debugger")
	}

	// Tools
	config := edm.CreateStandardAgentConfigWithLLM("evaluation-debugger-agent", 100, agents.OutputFormatStructured, llmConfigToUse)
	config.UseCodeExecutionMode = false
	
	// MCP Servers (use preset if available, else none)
	selectedServers := edm.GetSelectedServers()
	selectedTools := edm.GetSelectedTools()
	mcpConfigPath := edm.GetMCPConfigPath()
	
	if len(selectedServers) > 0 && mcpConfigPath != "" {
		config.ServerNames = selectedServers
		config.SelectedTools = selectedTools
		config.MCPConfigPath = mcpConfigPath
		config.MCPSessionID = edm.GetMCPSessionID()
	} else {
		config.ServerNames = []string{mcpclient.NoServers}
	}

	// Create wrapper
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewWorkflowEvaluationDebuggerAgent(cfg, logger, tracer, eventBridge, edm.BaseOrchestrator)
	}

	// Setup agent
	agent, err := edm.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"evaluation-debugger",
		0, 0,
		"evaluation-debugger",
		createAgentFunc,
		edm.WorkspaceTools,
		edm.WorkspaceToolExecutors,
		true,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (agent *WorkflowEvaluationDebuggerAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Base agent setup
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}
	mcpAgent := baseAgent.Agent()

	// Get file operations
	readFile := agent.baseOrchestrator.ReadWorkspaceFile
	writeFile := agent.baseOrchestrator.WriteWorkspaceFile
	moveFile := agent.baseOrchestrator.MoveWorkspaceFile
	logger := mcpAgent.Logger

	// Register tools
	workspacePath := agent.baseOrchestrator.GetWorkspacePath()
	if err := registerEvaluationModificationTools(mcpAgent, workspacePath, logger, readFile, writeFile, moveFile); err != nil {
		return "", nil, err
	}

	// Templates
	var systemPrompt, userMessage strings.Builder
	if err := evaluationDebuggerSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := evaluationDebuggerUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}

	// Execution loop
	maxIterations := 20
	iteration := 0
	currentResult := ""
	currentConversationHistory := conversationHistory

	// Emit start event
	if agent.baseOrchestrator != nil {
		eventBridge := agent.baseOrchestrator.GetContextAwareBridge()
		if eventBridge != nil {
			startedEvent := &orchestrator_events.OrchestratorAgentStartEvent{
				BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
				AgentType:     "evaluation-debugger",
				AgentName:     "evaluation-debugger-agent",
				Objective:     "Debug and improve evaluation plan",
				InputData:     templateVars,
			}
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type: orchestrator_events.OrchestratorAgentStart,
				Timestamp: time.Now(),
				Data: startedEvent,
			})
		}
	}

	for iteration < maxIterations {
		iteration++
		logger.Info(fmt.Sprintf("📊 Evaluation Debugger iteration %d/%d", iteration, maxIterations))

		inputProcessor := func(map[string]string) string { return userMessage.String() }
		
		// Use empty struct for template data since we already processed the templates
		result, updatedHistory, err := agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, currentConversationHistory, struct{}{}, systemPrompt.String(), true)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedHistory

		// Ask user if they want to continue
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			requestID := fmt.Sprintf("eval_debug_continue_%d_%d", iteration, time.Now().UnixNano())
			approved, feedback, err := agent.baseOrchestrator.RequestHumanFeedback(
				ctx, requestID, 
				fmt.Sprintf("Analysis complete (iteration %d/%d). Continue?", iteration, maxIterations),
				currentResult,
				agent.baseOrchestrator.GetMCPSessionID(), // Use accessor method instead of direct field access
				"evaluation-debug-workflow",
			)
			if err != nil || approved {
				break
			}
			if feedback != "" {
				// Create a new user message builder for the feedback
				var feedbackBuilder strings.Builder
				feedbackBuilder.WriteString(feedback)
				userMessage = feedbackBuilder
			}
		} else {
			break
		}
	}

	// Emit completion event
	if agent.baseOrchestrator != nil {
		eventBridge := agent.baseOrchestrator.GetContextAwareBridge()
		if eventBridge != nil {
			completedEvent := &orchestrator_events.OrchestratorAgentEndEvent{
				BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
				AgentType:     "evaluation-debugger",
				AgentName:     "evaluation-debugger-agent",
				Objective:     "Debug and improve evaluation plan",
				Result:        currentResult,
				Success:       true,
			}
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type: orchestrator_events.OrchestratorAgentEnd,
				Timestamp: time.Now(),
				Data: completedEvent,
			})
		}
	}

	return currentResult, currentConversationHistory, nil
}

func (edm *EvaluationDebuggerManager) requestUserGoal(ctx context.Context) (string, error) {
	requestID := fmt.Sprintf("eval_debug_goal_%d", time.Now().UnixNano())
	approved, goal, err := edm.RequestHumanFeedback(
		ctx, requestID,
		"What would you like to improve or debug in the evaluation?",
		"", edm.sessionID, edm.workflowID,
	)
	if err != nil {
		return "", err
	}
	if approved && goal == "" {
		return "Analyze the evaluation results and suggest improvements.", nil
	}
	return goal, nil
}

// --- Tools Implementation ---

func readEvaluationPlanFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*EvaluationPlan, error) {
	planPath := filepath.Join("evaluation", "evaluation_plan.json")
	content, err := readFile(ctx, planPath)
	if err != nil {
		return nil, err
	}
	var plan EvaluationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func writeEvaluationPlanToFile(ctx context.Context, workspacePath string, plan *EvaluationPlan, writeFile func(context.Context, string, string) error) error {
	planPath := filepath.Join("evaluation", "evaluation_plan.json")
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(ctx, planPath, string(data))
}

func registerEvaluationModificationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	moveFile func(context.Context, string, string) error,
) error {
	
	// Update Evaluation Step
	updateSchema := `{
		"type": "object",
		"properties": {
			"existing_step_id": {"type": "string", "description": "ID of the step to update"},
			"title": {"type": "string"},
			"description": {"type": "string"},
			"success_criteria": {"type": "string"}
		},
		"required": ["existing_step_id"]
	}`
	updateParams, _ := parseSchemaForToolParameters(updateSchema)
	mcpAgent.RegisterCustomTool(
		"update_evaluation_step",
		"Update an evaluation step. Provide existing_step_id and fields to change.",
		updateParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepID, _ := args["existing_step_id"].(string)
			plan, err := readEvaluationPlanFromFile(ctx, workspacePath, readFile)
			if err != nil { return "", err }
			
			for i, step := range plan.Steps {
				if step.ID == stepID {
					if t, ok := args["title"].(string); ok && t != "" { plan.Steps[i].Title = t }
					if d, ok := args["description"].(string); ok && d != "" { plan.Steps[i].Description = d }
					if s, ok := args["success_criteria"].(string); ok && s != "" { plan.Steps[i].SuccessCriteria = s }
					
					if err := writeEvaluationPlanToFile(ctx, workspacePath, plan, writeFile); err != nil { return "", err }
					return fmt.Sprintf("Updated step %s", stepID), nil
				}
			}
			return "", fmt.Errorf("step %s not found", stepID)
		},
		"workflow",
	)

	// Add Evaluation Step
	addSchema := `{
		"type": "object",
		"properties": {
			"id": {"type": "string"},
			"title": {"type": "string"},
			"description": {"type": "string"},
			"success_criteria": {"type": "string"}
		},
		"required": ["id", "title", "description", "success_criteria"]
	}`
	addParams, _ := parseSchemaForToolParameters(addSchema)
	mcpAgent.RegisterCustomTool(
		"add_evaluation_step",
		"Add a new evaluation step to the plan.",
		addParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			newStep := &EvaluationStep{
				ID: args["id"].(string),
				Title: args["title"].(string),
				Description: args["description"].(string),
				SuccessCriteria: args["success_criteria"].(string),
			}
			plan, err := readEvaluationPlanFromFile(ctx, workspacePath, readFile)
			if err != nil { return "", err }
			
			plan.Steps = append(plan.Steps, newStep)
			if err := writeEvaluationPlanToFile(ctx, workspacePath, plan, writeFile); err != nil { return "", err }
			return fmt.Sprintf("Added step %s", newStep.ID), nil
		},
		"workflow",
	)

	return nil
}
