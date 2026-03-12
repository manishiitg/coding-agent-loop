package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for execution debugger
var executionDebuggerSystemTemplate = MustRegisterTemplate("executionDebuggerSystem", `# Execution Debugger Agent

You are a read-only execution analysis assistant. Help the user understand what happened during workflow execution by answering questions about execution results, logs, and plan state.

## 🤖 ROLE
Answer user questions about execution results, logs, outputs, and plan state. You are a **read-only analyst** — you do NOT modify any files, plans, or configurations.

## ⚠️ RULES
1. **Read-Only**: You MUST NOT modify any files. You have no write access. Do not attempt to update plans, learnings, or any workspace files.
2. **Answer Directly**: For general questions, answer from the plan context provided below — don't read files unless needed.
3. **Read Files Only When Needed**: Only read execution logs/files if user asks about failures, debugging, or "why did X happen".
4. **Conversational**: Use 'human_feedback' to continue the conversation. Ask follow-up questions if the user's query is ambiguous.
5. **No Plan Modifications**: Do not suggest using plan modification tools. You do not have them.

## 📋 CONTEXT

### Workspace Information
- **Workspace**: {{.WorkspacePath}}
- **Selected Run**: {{.RunPathRelative}}

### Current Plan
{{if .PlanJSON}}` + "```json\n{{.PlanJSON}}\n```" + `{{else}}No plan provided.{{end}}

### Execution Summary
{{.ExecutionResultsSummary}}

---

## 📁 STEP TYPES & DATA LAYOUT

The plan supports 6 step types. Each saves data differently. All paths below are relative to the workspace root, prefixed with '{{.RunPathRelative}}/'.

### 1. Regular Steps (type: "regular")
The most common step type. An LLM agent executes instructions and writes output files.

**Folder paths:**
- 'execution/step-{X}/' — step output files
- 'logs/step-{X}/' — validation and execution logs

**Key files:**
- 'execution/step-{X}/{context_output}' — the actual result file (name from plan.json 'context_output' field, e.g. 'output.json')
- 'execution/step-{X}/step_done.json' — completion marker with timestamp
- 'logs/step-{X}/validation-{N}.json' — validation attempts (N=1,2,...). Contains: 'is_success_criteria_met', 'execution_status' (COMPLETED/PARTIAL/FAILED/INCOMPLETE), 'reasoning', 'feedback', 'evidence'
- 'logs/step-{X}/execution/execution-attempt-{A}-iteration-{I}.json' — execution result (A=attempt, I=loop iteration)
- 'logs/step-{X}/execution/execution-attempt-{A}-iteration-{I}-conversation.json' — **full LLM conversation** with all tool calls and responses

**Loop steps** (has_loop: true): Same folder, iterations tracked in filename pattern 'execution-attempt-{A}-iteration-{I}.json'. I increments each loop cycle (0, 1, 2...).

### 2. Conditional Steps (type: "conditional")
Evaluates a condition question, then runs either 'if_true_steps' or 'if_false_steps' branch. The wrapper itself is NOT executed — only the branch steps run.

**Folder paths:**
- 'logs/step-{X}/' — conditional evaluation result (wrapper)
- 'execution/step-{X}-if-true-{idx}/' — true branch step outputs (idx=0,1,2...)
- 'execution/step-{X}-if-false-{idx}/' — false branch step outputs (idx=0,1,2...)
- 'logs/step-{X}-if-true-{idx}/' — true branch step logs
- 'logs/step-{X}-if-false-{idx}/' — false branch step logs

**Key files:**
- 'logs/step-{X}/conditional-evaluation.json' — the decision: 'condition_result' (true/false), 'condition_reason', 'branch_executed' ("if_true" or "if_false"), 'condition_question'
- Branch step files follow the same pattern as regular steps (validation-{N}.json, execution-attempt-{A}-iteration-{I}.json, etc.)

### 3. Decision Steps (type: "decision")
Like a regular step but with two phases: first **executes** (produces output), then **evaluates** the output to make a routing decision (true/false → jump to different next step).

**Folder paths:**
- 'execution/step-{X}-decision/' — execution output
- 'logs/step-{X}/' — execution and evaluation logs

**Key files:**
- 'execution/step-{X}-decision/{context_output}' — the step's output
- 'logs/step-{X}/decision-execution.json' — execution phase output
- 'logs/step-{X}/decision-evaluation.json' — evaluation result: 'decision_result' (true/false), 'decision_reasoning', 'decision_evaluation_question', 'if_true_next_step_id', 'if_false_next_step_id'
- Standard execution logs (execution-attempt-{A}-iteration-{I}.json, validation-{N}.json)

### 4. Orchestration Steps (type: "orchestration")
A loop: execute main step → evaluate → select a sub-agent route → execute sub-agent → repeat until success criteria met.

**Folder paths:**
- 'execution/step-{X}-orchestration/' — main orchestration step output (deprecated, may be in 'execution/step-{X}/' instead)
- 'execution/step-{X}-sub-agent-{idx}/' — each sub-agent's output (idx=0,1,2...)
- 'logs/step-{X}/' — main step logs
- 'logs/step-{X}-sub-agent-{idx}/' — sub-agent logs

**Key files:**
- 'logs/step-{X}/orchestration-execution.json' — **JSONL file** (one JSON object per line). Each line records: 'iteration', 'selected_route_id', 'selected_route_name', 'success_criteria_met', 'reason'. Check for repeated route selections (potential infinite loop).
- Sub-agent folders have standard execution logs (validation-{N}.json, execution-attempt files)

### 5. Todo Task Steps (type: "todo_task")
Manages a dynamic markdown task list. An orchestrator agent creates tasks and delegates to predefined sub-agents or generic agents. Completion is **validation-driven** (step completes when validation_schema passes).

**Folder paths:**
- 'execution/step-{X}/' — main step output including tasks.md
- 'execution/step-{X}-sub-agent-{idx}/' — predefined sub-agent outputs (have learnings/prevalidation)
- 'execution/step-{X}-generic-agent-{idx}/' — generic agent outputs (dynamic tasks, no learnings)
- 'logs/step-{X}/' — main step logs
- 'logs/step-{X}-sub-agent-{idx}/' — sub-agent logs
- 'logs/step-{X}-generic-agent-{idx}/' — generic agent logs

**Key files:**
- 'execution/step-{X}/tasks.md' — **markdown task list** with checkboxes showing completion status
- 'logs/step-{X}/orchestration-execution.json' — JSONL of routing decisions (which sub-agent was called for which todo)
- Sub-agent and generic-agent folders have standard execution logs

### 6. Human Input Steps (type: "human_input")
Collects user input (text, yes/no, or multiple choice). No LLM execution or validation. Minimal files.

**Folder paths:**
- 'execution/step-{X}/' — minimal (just step_done.json)

---

## 📁 OTHER DATA SOURCES (Read-Only)

### Progress Tracking: '{{.RunPathRelative}}/execution/steps_done.json'
Tracks which steps completed: 'completed_step_indices' (array), 'branch_steps' (which branch taken), 'validation_failures' (retry counts per step), 'archival_counts'.

### Plan: 'planning/plan.json'
Step definitions with 'context_dependencies' (input files from prior steps), 'context_output' (output filename), 'success_criteria', 'validation_schema'.

### Step Config: 'planning/step_config.json'
Per-step agent settings: LLM provider/model, code execution mode, selected servers/tools, disable_validation, disable_learning, orchestration_max_iterations.

### Learnings: 'learnings/'
- 'learnings/{step_id}/learnings.md' — consolidated learnings per step
- 'learnings/{step_id}/learnings_metadata.json' — success counts, confidence
- 'learnings/shared_learnings.md' — shared across all steps

### Knowledgebase: 'knowledgebase/'
Persistent user-provided reference files, shared across all runs.

### Evaluation: 'evaluation/'
- 'evaluation/evaluation_plan.json' — evaluation criteria
- 'evaluation/runs/{iteration}/evaluation_report.json' — scored assessment: 'total_score', 'score_percentage', 'step_scores[]' with 'reasoning' and 'evidence'
- 'evaluation/learnings/' — evaluation-specific learnings

---

## 📖 ANALYSIS GUIDE

<details>
<summary>Step Failures</summary>
1. Check 'logs/step-X/validation-{N}.json' — read 'execution_status' and 'reasoning' to understand why validation failed
2. Check 'logs/step-X/execution/execution-attempt-{A}-iteration-{I}.json' — read the execution result
3. If deeper analysis needed, read the '-conversation.json' file for the full agent conversation including tool calls
4. Check plan.json 'context_dependencies' to see if a prerequisite step's output was missing
</details>

<details>
<summary>Output Inspection</summary>
- Read files in 'execution/step-X/' to see what each step produced
- Compare with plan.json 'context_output' field to see what was expected
- Check 'step_done.json' to confirm step completed
- Check 'steps_done.json' for overall progress
</details>

<details>
<summary>Conditional/Decision Steps</summary>
- Conditional: Read 'logs/step-X/conditional-evaluation.json' for which branch was taken and why
- Decision: Read 'logs/step-X/decision-evaluation.json' for the routing decision and reasoning
- Check if the wrong branch was taken by comparing condition result with expected behavior
</details>

<details>
<summary>Orchestration/TodoTask Steps</summary>
- Read 'logs/step-X/orchestration-execution.json' (JSONL) for routing decisions — check for infinite loops (same route selected repeatedly)
- For todo_task: read 'execution/step-X/tasks.md' to see task list progress
- Check individual sub-agent logs in 'logs/step-X-sub-agent-{i}/' or 'logs/step-X-generic-agent-{i}/'
</details>

<details>
<summary>Validation Failures</summary>
- Pre-Validation (Structural): Checks 'validation_schema' in plan.json — verifies file existence and JSON structure
- LLM Validation (Authenticity): Checks 'success_criteria' — LLM judges if criteria are met
- For todo_task steps: pre-validation passing is the PRIMARY completion signal
- Read validation-{N}.json for detailed reasoning
</details>

<details>
<summary>Evaluation Reports</summary>
- Location: 'evaluation/runs/{runFolder}/evaluation_report.json'
- Contains: total_score, score_percentage, step_scores[] with reasoning
- Low scores (< 50%) indicate steps needing attention
</details>
`)

var executionDebuggerUserTemplate = MustRegisterTemplate("executionDebuggerUser", `{{if .UserRequest}}{{.UserRequest}}{{else}}What would you like to know about the execution results?{{end}}`)

// WorkflowExecutionDebuggerAgent analyzes execution results and answers questions (read-only)
type WorkflowExecutionDebuggerAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator
}

// NewWorkflowExecutionDebuggerAgent creates a new execution debugger agent
func NewWorkflowExecutionDebuggerAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *WorkflowExecutionDebuggerAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionQAAgentType,
		eventBridge,
	)

	return &WorkflowExecutionDebuggerAgent{
		BaseOrchestratorAgent: baseAgent,
		baseOrchestrator:      baseOrchestrator,
	}
}

// ExecutionDebuggerManager manages execution debugger agent creation
type ExecutionDebuggerManager struct {
	*orchestrator.BaseOrchestrator
	presetLLM  *AgentLLMConfig
	sessionID  string
	workflowID string
}

// NewExecutionDebuggerManager creates a new ExecutionDebuggerManager
func NewExecutionDebuggerManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *ExecutionDebuggerManager {
	return &ExecutionDebuggerManager{
		BaseOrchestrator: baseOrchestrator,
		presetLLM:        presetLLM,
		sessionID:        sessionID,
		workflowID:       workflowID,
	}
}

// ExecutionDebuggerOnly runs only the execution debugger phase
func (edm *ExecutionDebuggerManager) ExecutionDebuggerOnly(ctx context.Context, originalWorkspacePath string, runPath string) (string, error) {
	edm.GetLogger().Info(fmt.Sprintf("🔍 Starting execution debugger for workspace: %s", originalWorkspacePath))

	// Set workspace path
	edm.SetWorkspacePath(originalWorkspacePath)

	// Validate run path — if not provided, ask the user
	if runPath == "" {
		edm.GetLogger().Info("📊 No run path provided, asking user...")
		requestID := fmt.Sprintf("exec_debug_run_path_%d", time.Now().UnixNano())
		_, userPath, err := edm.RequestHumanFeedback(
			ctx, requestID,
			fmt.Sprintf("Please provide the run folder path to analyze (relative to runs/ folder).\n\nExamples: 'iteration-11', 'iteration-11/group-7'\n\nWorkspace: %s", originalWorkspacePath),
			"", edm.sessionID, edm.workflowID,
		)
		if err != nil {
			return "", fmt.Errorf("failed to get run path from user: %w", err)
		}
		runPath = strings.TrimSpace(userPath)
		if runPath == "" {
			return "", fmt.Errorf("run path is required for execution debugger")
		}
	}
	// Basic validation of run path format
	if !strings.HasPrefix(runPath, "runs/") && !strings.HasPrefix(runPath, "iteration-") {
		runPath = "runs/" + runPath
	}

	// Read Plan
	planContent, err := edm.ReadWorkspaceFile(ctx, "planning/plan.json")
	if err != nil {
		edm.GetLogger().Warn(fmt.Sprintf("⚠️ Plan not found: %v", err))
		planContent = "{}" // Continue with empty plan
	}

	// Compute relative run path for display
	relRunPath := strings.TrimPrefix(runPath, "runs/")
	if strings.HasPrefix(relRunPath, "/") {
		relRunPath = relRunPath[1:]
	}

	// Ask user what they want to know
	userRequest, err := edm.requestUserGoal(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get user goal: %w", err)
	}

	// Create Agent
	agent, err := edm.createExecutionDebuggerAgent(ctx, originalWorkspacePath, runPath)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	// Prepare template vars
	templateVars := map[string]string{
		"WorkspacePath":           originalWorkspacePath,
		"RunPathRelative":         relRunPath,
		"PlanJSON":                planContent,
		"ExecutionResultsSummary": fmt.Sprintf("Run: %s", relRunPath),
		"UserRequest":             userRequest,
		"SessionID":               edm.sessionID,
		"WorkflowID":              edm.workflowID,
	}

	// Execute
	edm.GetLogger().Info("🔍 Executing execution debugger agent...")
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	return result, nil
}

// createExecutionDebuggerAgent creates the agent with read-only access
func (edm *ExecutionDebuggerManager) createExecutionDebuggerAgent(ctx context.Context, workspacePath string, runPath string) (agents.OrchestratorAgent, error) {
	// Paths — all read-only
	currentWorkspacePath := workspacePath
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	knowledgebasePath := getKnowledgebasePath(workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	planningPath := fmt.Sprintf("%s/planning", workspacePath)
	evaluationPath := fmt.Sprintf("%s/evaluation", workspacePath)

	readPaths := []string{
		currentWorkspacePath,
		runsPath,
		knowledgebasePath,
		learningsPath,
		planningPath,
		evaluationPath,
	}

	// Completely read-only — no write paths
	writePaths := []string{}

	edm.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	edm.GetLogger().Info(fmt.Sprintf("🔍 Setting folder guard for execution debugger agent - Read paths: %v, Write paths: %v (read-only)", readPaths, writePaths))

	// LLM Config
	var llmConfigToUse *orchestrator.LLMConfig
	if edm.presetLLM != nil && edm.presetLLM.Provider != "" && edm.presetLLM.ModelID != "" {
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: edm.presetLLM.Provider,
				ModelID:  edm.presetLLM.ModelID,
			},
			Fallbacks: edm.GetFallbacks(),
			APIKeys:   edm.GetAPIKeys(),
		}
		edm.GetLogger().Info(fmt.Sprintf("🔧 Using preset LLM for execution debugger: %s/%s", edm.presetLLM.Provider, edm.presetLLM.ModelID))
	} else {
		return nil, fmt.Errorf("no valid LLM configuration found for execution debugger agent")
	}

	// Agent config
	config := edm.CreateStandardAgentConfigWithLLM("execution-debugger-agent", 100, agents.OutputFormatStructured, llmConfigToUse)
	// Phase agents always use simple mode UNLESS the provider requires code execution (claude-code, gemini-cli)
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(edm.presetLLM)
	config.UseToolSearchMode = false

	// MCP Servers (use preset if available)
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

	// Use minimal workspace tools (shell_command + human) for phase agent
	phaseTools, phaseExecutors := edm.BaseOrchestrator.PreparePhaseAgentTools()

	// Create wrapper
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewWorkflowExecutionDebuggerAgent(cfg, logger, tracer, eventBridge, edm.BaseOrchestrator)
	}

	// Setup agent
	agent, err := edm.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"execution-qa",
		0, 0,
		"execution-qa",
		createAgentFunc,
		phaseTools,
		phaseExecutors,
		true,
	)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// Execute implements OrchestratorAgent interface for the execution debugger
func (agent *WorkflowExecutionDebuggerAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Base agent setup
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}

	// NO modification tools registered — this agent is read-only

	// Templates
	var systemPrompt, userMessage strings.Builder
	if err := executionDebuggerSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := executionDebuggerUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}

	// Execution loop
	maxIterations := 20
	iteration := 0
	currentResult := ""
	currentConversationHistory := conversationHistory
	sessionID := templateVars["SessionID"]
	workflowID := templateVars["WorkflowID"]

	// Emit start event
	if agent.baseOrchestrator != nil {
		eventBridge := agent.baseOrchestrator.GetContextAwareBridge()
		if eventBridge != nil {
			startedEvent := &orchestrator_events.OrchestratorAgentStartEvent{
				BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
				AgentType:     "execution-qa",
				AgentName:     "execution-debugger-agent",
				Objective:     "Analyze execution results (read-only)",
				InputData:     templateVars,
			}
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type:      orchestrator_events.OrchestratorAgentStart,
				Timestamp: time.Now(),
				Data:      startedEvent,
			})
		}
	}

	for iteration < maxIterations {
		iteration++

		logger := baseAgent.Agent().Logger
		if logger != nil {
			logger.Info(fmt.Sprintf("🔍 Execution Debugger iteration %d/%d", iteration, maxIterations))
		}

		inputProcessor := func(map[string]string) string { return userMessage.String() }

		result, updatedHistory, err := agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, currentConversationHistory, struct{}{}, systemPrompt.String(), true)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedHistory

		// NO plan modification check — read-only agent

		// Ask user if they want to continue
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			requestID := fmt.Sprintf("exec_debug_continue_%d_%d", iteration, time.Now().UnixNano())
			approved, feedback, err := agent.baseOrchestrator.RequestHumanFeedback(
				ctx, requestID,
				fmt.Sprintf("Analysis complete (iteration %d/%d). Approve to finish, or provide another question.", iteration, maxIterations),
				currentResult,
				sessionID,
				workflowID,
			)
			if err != nil {
				break
			}
			if approved {
				break
			}
			if feedback != "" {
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
				AgentType:     "execution-qa",
				AgentName:     "execution-debugger-agent",
				Objective:     "Analyze execution results (read-only)",
				Result:        currentResult,
				Success:       true,
			}
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type:      orchestrator_events.OrchestratorAgentEnd,
				Timestamp: time.Now(),
				Data:      completedEvent,
			})
		}
	}

	return currentResult, currentConversationHistory, nil
}

func (edm *ExecutionDebuggerManager) requestUserGoal(ctx context.Context) (string, error) {
	requestID := fmt.Sprintf("exec_debug_goal_%d", time.Now().UnixNano())
	approved, goal, err := edm.RequestHumanFeedback(
		ctx, requestID,
		"What would you like to know about the execution results?",
		"", edm.sessionID, edm.workflowID,
	)
	if err != nil {
		return "", err
	}
	if approved && goal == "" {
		return "Analyze the execution results and provide a summary of what happened.", nil
	}
	return goal, nil
}
