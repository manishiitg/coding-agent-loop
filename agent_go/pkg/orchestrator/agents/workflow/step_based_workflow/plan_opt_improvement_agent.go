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
	mcpagent "github.com/manishiitg/mcpagent/agent"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for plan improvement - panics at startup if invalid
var improvementSystemTemplate = MustRegisterTemplate("improvementSystem", `# Plan Improvement Agent

You are a plan improvement assistant. Help the user with their questions about the plan and make improvements when requested.

## 🤖 ROLE
Answer user questions directly. Only analyze logs/files when the user specifically asks about execution results, failures, or debugging.

## ⚠️ RULES
1. **Answer Directly**: For general questions, answer from the plan context provided below - don't read files unless needed.
2. **Confirm Before Changes**: Use 'human_feedback' BEFORE any plan modifications.
3. **Read Files Only When Needed**: Only read execution logs/files if user asks about failures, debugging, or "why did X happen".
4. **Concrete Criteria**: When updating success criteria, make them file-verifiable (counts, samples).

## 📋 CONTEXT

### Workspace Information
- **Workspace**: {{.WorkspacePath}}
- **Selected Run**: {{.RunPathRelative}}
{{if .AllowedPaths}}- **Allowed Paths**: {{.AllowedPaths}}{{end}}

### Current Plan
{{if .PlanJSON}}` + "```json\n{{.PlanJSON}}\n```" + `{{else}}No plan provided.{{end}}

### Execution Summary
{{.ExecutionResultsSummary}}

---

## 📁 FILE LOCATIONS (read only when needed)
- **Plan file**: 'planning/plan.json'
- **Step config**: 'planning/step_config.json' — per-step LLM, tool, and mode settings
- **Learnings**: 'learnings/' and 'learnings/{step_id}/'
- **Execution outputs**: '{{.RunPathRelative}}/execution/' — step output files
- **Logs (for debugging)**: '{{.RunPathRelative}}/logs/' — only read if user asks about failures
- **Progress**: '{{.RunPathRelative}}/execution/steps_done.json' — which steps completed, branch decisions, retry counts
- **Knowledgebase**: 'knowledgebase/' — persistent files across runs
- **Evaluation reports**: 'evaluation/runs/{runFolder}/evaluation_report.json'

### Step Folder Naming (inside execution/ and logs/)
- Regular steps: 'step-{X}/' (X = 1-based step number)
- Conditional branches: 'step-{X}-if-true-{idx}/', 'step-{X}-if-false-{idx}/'
- Decision steps: 'step-{X}-decision/'
- Routing steps: 'step-{X}/' (same as regular - routing evaluation stored in logs)
- Sub-agents (orchestration/todo_task): 'step-{X}-sub-agent-{idx}/'
- Generic agents (todo_task only): 'step-{X}-generic-agent-{idx}/'

### Key Log Files Per Step Type
- **All steps**: 'logs/step-X/validation-{N}.json' (validation attempts), 'logs/step-X/execution/execution-attempt-{A}-iteration-{I}.json' (execution result), plus '-conversation.json' (full LLM chat)
- **Conditional**: 'logs/step-X/conditional-evaluation.json' — condition_result (true/false), condition_reason, branch_executed
- **Decision**: 'logs/step-X/decision-evaluation.json' — decision_result, decision_reasoning, routing targets
- **Orchestration/TodoTask**: 'logs/step-X/orchestration-execution.json' — JSONL file, one line per iteration with selected_route_id, success_criteria_met
- **Routing**: 'logs/step-X/routing-evaluation.json' -- selected_route_id, reasoning, routing_question, routes
- **TodoTask**: 'execution/step-X/tasks.md' — markdown task list with checkbox progress

## 🛠️ PLAN MODIFICATION TOOLS
Use 'update_*', 'add_*', 'delete_plan_steps', etc. ONLY after user approval via 'human_feedback'.

### Top-Level Steps
- 'update_regular_step': Update regular steps by step_id (title, description, success_criteria, validation_schema, etc.)
- 'update_todo_task_step': Update todo_task steps by step_id (title, description, todo_task_step fields)
- 'update_routing_step': Update routing steps by step_id (routing_question, routes, default_route_id)
- 'add_plan_step', 'add_routing_step', 'delete_plan_steps': Add/remove top-level steps

### Nested Sub-Agent Steps (inside todo_task or orchestration steps)
**IMPORTANT**: Sub-agent steps inside 'predefined_routes' are NOT top-level steps. Use route-specific tools:
- 'update_todo_task_route': Update a route AND its nested sub_agent_step
  - Parameters: parent_step_id (the todo_task step), existing_route_id, sub_agent_step (full step definition)
- 'add_todo_task_route': Add a new route with sub_agent_step to a todo_task step
- 'delete_todo_task_route': Remove a route from a todo_task step

Example: To update 'publish-notion-report' inside todo_task step 'codebase-inventory-tasks':
'''json
{
  "parent_step_id": "codebase-inventory-tasks",
  "existing_route_id": "publish-notion-report",
  "sub_agent_step": {
    "type": "regular",
    "id": "publish-notion-report",
    "title": "Updated Title",
    "description": "Updated description...",
    "success_criteria": "Updated criteria...",
    "context_dependencies": [],
    "context_output": "result.json",
    "has_loop": false
  }
}
'''

---

## 📖 REFERENCE: Analysis Checklists (use when debugging)

<details>
<summary>Conditional/Decision Steps</summary>
- Conditional: Read 'logs/step-X/conditional-evaluation.json' — which branch was taken and why ('condition_result', 'condition_reason', 'branch_executed')
- Decision: Read 'logs/step-X/decision-evaluation.json' — routing decision after execution ('decision_result', 'decision_reasoning')
- Branch step logs are in 'logs/step-X-if-true-{idx}/' or 'logs/step-X-if-false-{idx}/'
</details>

<details>
<summary>Orchestration Steps</summary>
- Main Orchestrator: Check 'logs/step-X/orchestration-execution.json' (JSONL) for routing decisions and infinite loops (same route selected repeatedly)
- Sub-Agents: Check 'logs/step-X-sub-agent-{i}/' for sub-agent issues
</details>

<details>
<summary>Routing Steps</summary>
- Read 'logs/step-X/routing-evaluation.json' -- which route was selected and why ('selected_route_id', 'reasoning')
- If execute-then-route mode: also check 'logs/step-X/routing-execution.json' for execution output before routing
- Two modes: Execute-then-route (has description) executes first then picks route; Pure routing (no description) evaluates prior context only
</details>

<details>
<summary>Todo Task Steps</summary>
- Task Progress: Check '{{.RunPathRelative}}/execution/step-X/tasks.md' for task list status
- Routing: Check 'logs/step-X/orchestration-execution.json' for which sub-agent handled which todo
- Sub-Agents: Check 'logs/step-X-sub-agent-{i}/' for predefined agent issues
- Generic Agent: Check 'logs/step-X-generic-agent-{i}/' for dynamic task issues
- **Completion**: todo_task steps complete when 'validation_schema' passes (not success_criteria)
</details>

<details>
<summary>Validation Failures</summary>
- Pre-Validation (Structural): Update 'validation_schema' (file exists, JSON format)
- LLM Validation (Authenticity): Update 'success_criteria' to focus on execution history

**validation_schema structure:**
'''json
{
  "validation_schema": {
    "files": [{
      "file_name": "output.json",
      "must_exist": true,
      "json_checks": [{
        "path": "$.field",
        "must_exist": true,
        "value_type": "string|number|boolean|array|object",
        "min_length": 1, "max_length": 100,
        "pattern": "^regex$"
      }]
    }]
  }
}
'''
**CRITICAL for todo_task steps**: validation_schema is the PRIMARY completion signal. Step completes when validation passes.
</details>

<details>
<summary>JSON File Size Issues</summary>
If JSON > 100KB causes parsing failures, split into: structured JSON + markdown file reference
Example: {"summary": "brief", "details_file": "step_X_details.md"}
</details>

<details>
<summary>Evaluation Reports</summary>
- Location: 'evaluation/{{.RunPathRelative}}/evaluation_report.json'
- Contains: total_score, score_percentage, step_scores[] with reasoning
- Use low scores (< 50%) to identify steps needing better success criteria
</details>

{{if .IsCodeExecutionMode}}
## 🛠️ AVAILABLE TOOLS

### Plan Modification Tools
**Always call 'human_feedback' first to confirm changes before using any of these.**

#### Update Steps
- 'update_regular_step(existing_step_id, ...)' — Update fields of a regular step
- 'update_conditional_step(existing_step_id, ...)' — Update a conditional step
- 'update_decision_step(existing_step_id, ...)' — Update a decision step
- 'update_routing_step(existing_step_id, ...)' — Update a routing step
- 'update_human_input_step(existing_step_id, ...)' — Update a human input step
- 'update_todo_task_step(existing_step_id, ...)' — Update a todo task step
- 'update_validation_schema(existing_step_id, validation_schema)' — Update a step's validation schema
- 'update_success_criteria(existing_step_id, success_criteria)' — Update a step's success criteria

#### Add Steps
- 'add_regular_step' — Add a standard execution step
- 'add_conditional_step' — Add an if/else branch step
- 'add_decision_step' — Add an execute-then-route step
- 'add_loop_step' — Add a repeating step
- 'add_human_input_step' — Add a step that blocks for user input
- 'add_routing_step' — Add an N-way LLM routing step
- 'add_todo_task_step' — Add a todo-task orchestration step

#### Delete Steps
- 'delete_plan_steps(step_ids[])' — Delete steps by their IDs

#### Conditional Branch Tools
- 'convert_step_to_conditional(step_id, condition_question, if_true_steps, if_false_steps)'
- 'convert_conditional_to_regular(step_id)'
- 'add_branch_steps(parent_step_id, branch_type, new_steps[])'
- 'update_branch_steps(parent_step_id, branch_type, updated_steps[])'
- 'delete_branch_steps(parent_step_id, branch_type, deleted_step_ids[])'

#### Todo Task Route Tools
- 'add_todo_task_route(parent_step_id, new_route)'
- 'update_todo_task_route(parent_step_id, existing_route_id, ...)'
- 'delete_todo_task_route(parent_step_id, deleted_route_id)'

### Workspace Tools
- 'execute_shell_command(command)' — Run shell commands using full workspace-relative paths
- 'human_feedback(question, response_type)' — Ask user for approval or input (**call before any plan change**)
{{end}}`)

var improvementUserTemplate = MustRegisterTemplate("improvementUser", `{{if .UserImprovementRequest}}{{.UserImprovementRequest}}{{else}}What would you like help with regarding the plan?{{end}}`)

// WorkflowPlanImprovementTemplate holds template variables for plan improvement prompts
type WorkflowPlanImprovementTemplate struct {
	WorkspacePath           string
	RunPathRelative         string
	RunWorkspacePath        string
	PlanJSON                string
	ExecutionResultsSummary string
	AllowedPaths            string
	UserImprovementRequest  string
	IsCodeExecutionMode     bool
}

// WorkflowPlanImprovementAgent analyzes execution results and provides feedback for plan improvement
type WorkflowPlanImprovementAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator // Reference to base orchestrator for RequestHumanFeedback
}

// NewWorkflowPlanImprovementAgent creates a new plan improvement agent
func NewWorkflowPlanImprovementAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *WorkflowPlanImprovementAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanImprovementAgentType,
		eventBridge,
	)

	return &WorkflowPlanImprovementAgent{
		BaseOrchestratorAgent: baseAgent,
		baseOrchestrator:      baseOrchestrator,
	}
}

// PlanImprovementManager manages plan improvement agent creation independently from controller
type PlanImprovementManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Plan improvement LLM config (optional preset)
	presetPlanImprovementLLM *AgentLLMConfig
	// Phase LLM config (fallback for plan improvement if presetPlanImprovementLLM not set)
	presetPhaseLLM *AgentLLMConfig

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string

	// Whether to reference knowledgebase folder in prompts (default: true)
	useKnowledgebase bool
}

// NewPlanImprovementManager creates a new PlanImprovementManager
func NewPlanImprovementManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	presetPlanImprovementLLM *AgentLLMConfig,
	presetPhaseLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
	useKnowledgebase bool,
) *PlanImprovementManager {
	return &PlanImprovementManager{
		BaseOrchestrator:         baseOrchestrator,
		presetPlanImprovementLLM: presetPlanImprovementLLM,
		presetPhaseLLM:           presetPhaseLLM,
		sessionID:                sessionID,
		workflowID:               workflowID,
		useKnowledgebase:         useKnowledgebase,
	}
}

// createPlanImprovementAgent creates and sets up a plan improvement agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
// workspacePath: current workspace path (may be a subdirectory like workspace/runs/iteration-1)
// originalWorkspacePath: original workspace root (for accessing learnings/ and planning/ folders)
func (pim *PlanImprovementManager) createPlanImprovementAgent(ctx context.Context, workspacePath string, originalWorkspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read-only access to specific folders only
	// Plan improvement agent needs read access to:
	// - Current workspace: for execution results and logs (execution/, logs/)
	// - Original workspace learnings/: for learnings analysis (shared and step-specific learnings)
	// - Original workspace planning/: for reading plan.json
	currentWorkspacePath := workspacePath
	learningsPath := fmt.Sprintf("%s/learnings", originalWorkspacePath)
	planningPath := fmt.Sprintf("%s/planning", originalWorkspacePath)

	// Build read paths list - explicit read-only access
	// Use current workspace for execution/logs, original workspace for learnings/planning
	// Knowledgebase folder: knowledgebase/ (persistent files across runs, at workspace root)
	// Evaluation folder: scoped to specific iteration being analyzed
	knowledgebasePath := getKnowledgebasePath(originalWorkspacePath)
	runsPath := fmt.Sprintf("%s/runs", originalWorkspacePath)

	// Extract iteration folder from workspacePath to scope evaluation access
	// workspacePath = {originalWorkspacePath}/runs/iteration-X or {originalWorkspacePath}/runs/iteration-X/group-Y
	// We need to extract the relative path after "runs/" to match evaluation folder structure
	iterationFolder := strings.TrimPrefix(workspacePath, originalWorkspacePath+"/runs/")
	if iterationFolder == workspacePath {
		// Fallback: try without leading slash
		iterationFolder = strings.TrimPrefix(workspacePath, originalWorkspacePath+"runs/")
	}

	// Evaluation paths - scoped to specific iteration
	evaluationPlanPath := fmt.Sprintf("%s/evaluation/evaluation_plan.json", originalWorkspacePath)    // Read evaluation plan definition
	evaluationLearningsPath := fmt.Sprintf("%s/evaluation/learnings", originalWorkspacePath)          // Read evaluation learnings (shared)
	evaluationRunPath := fmt.Sprintf("%s/evaluation/runs/%s", originalWorkspacePath, iterationFolder) // Read only this iteration's evaluation report

	// Logs and execution paths - explicit paths for debugging access
	// These are needed because the folder guard may not resolve relative paths properly
	logsPath := fmt.Sprintf("%s/logs", currentWorkspacePath)                               // Logs inside current run workspace (e.g., runs/iteration-1/repo/logs)
	executionPath := fmt.Sprintf("%s/execution", currentWorkspacePath)                     // Execution outputs inside current run workspace
	runIterationLogsPath := fmt.Sprintf("%s/runs/%s/logs", originalWorkspacePath, iterationFolder) // Explicit logs path using iteration folder

	readPaths := []string{
		currentWorkspacePath,    // Read execution results and logs from current workspace
		runsPath,                // Read access to all runs
		knowledgebasePath,       // Read knowledgebase folder (persistent files across runs)
		learningsPath,           // Read learnings from original workspace (shared and step-specific)
		planningPath,            // Read plan.json from original workspace
		evaluationPlanPath,      // Read evaluation plan definition
		evaluationLearningsPath, // Read evaluation learnings (shared across runs)
		evaluationRunPath,       // Read evaluation report for THIS iteration only
		logsPath,                // Explicit logs path for debugging
		executionPath,           // Explicit execution path for outputs
		runIterationLogsPath,    // Explicit logs path using runs/iteration folder format
	}

	pim.GetLogger().Info(fmt.Sprintf("📊 Evaluation access scoped to iteration: %s", iterationFolder))

	// Step-specific learnings are always enabled - folders are at workspace root
	// The learningsPath already covers these since they're under learnings/
	pim.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings enabled - agent can access step-specific folders in learnings/{step_id}/ (covered by learnings/ read/write path)"))

	// Write paths: learnings folder for updating learnings, plan modifications via custom tools
	// Plan modifications are done via custom tools (not workspace tools), and the tool executors handle file writing directly
	writePaths := []string{
		learningsPath, // Write access to learnings folder for updating learnings
	}

	// Set folder guard with read and write access
	pim.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	pim.GetLogger().Info(fmt.Sprintf("📊 Setting folder guard for plan improvement agent:"))
	pim.GetLogger().Info(fmt.Sprintf("   ✅ Read paths (%d): %v", len(readPaths), readPaths))
	pim.GetLogger().Info(fmt.Sprintf("   ✅ Write paths (%d): %v (learnings folder for updates, plan updates via custom tools)", len(writePaths), writePaths))
	pim.GetLogger().Info(fmt.Sprintf("   📝 Plan updates are done via custom tools, not workspace tools"))

	// Determine LLM config: Priority: presetPlanImprovementLLM > presetLearningLLM > orchestrator default
	var llmConfigToUse *orchestrator.LLMConfig
	orchestratorLLMConfig := pim.GetLLMConfig()
	if pim.presetPlanImprovementLLM != nil && pim.presetPlanImprovementLLM.Provider != "" && pim.presetPlanImprovementLLM.ModelID != "" {
		// Initialize fallbacks/apiKeys with safe defaults
		var fallbacks []orchestrator.LLMModel
		var apiKeys *orchestrator.APIKeys

		// Only copy from orchestratorLLMConfig if it's not nil
		if orchestratorLLMConfig != nil {
			fallbacks = orchestratorLLMConfig.Fallbacks
			apiKeys = orchestratorLLMConfig.APIKeys
		}

		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: pim.presetPlanImprovementLLM.Provider,
				ModelID:  pim.presetPlanImprovementLLM.ModelID,
			},
			Fallbacks: fallbacks, // Preserve fallbacks from orchestrator (or nil if orchestrator config is nil)
			APIKeys:   apiKeys,   // Preserve API keys from orchestrator (or nil if orchestrator config is nil)
		}
		pim.GetLogger().Info(fmt.Sprintf("🔧 Using preset default plan improvement LLM: %s/%s", pim.presetPlanImprovementLLM.Provider, pim.presetPlanImprovementLLM.ModelID))
	} else if pim.presetPhaseLLM != nil && pim.presetPhaseLLM.Provider != "" && pim.presetPhaseLLM.ModelID != "" {
		// Fallback to phase LLM if plan improvement LLM not set
		var fallbacks []orchestrator.LLMModel
		var apiKeys *orchestrator.APIKeys

		// Only copy from orchestratorLLMConfig if it's not nil
		if orchestratorLLMConfig != nil {
			fallbacks = orchestratorLLMConfig.Fallbacks
			apiKeys = orchestratorLLMConfig.APIKeys
		}

		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: pim.presetPhaseLLM.Provider,
				ModelID:  pim.presetPhaseLLM.ModelID,
			},
			Fallbacks: fallbacks,
			APIKeys:   apiKeys,
		}
		pim.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM as fallback for plan improvement: %s/%s", pim.presetPhaseLLM.Provider, pim.presetPhaseLLM.ModelID))
	} else {
		return nil, fmt.Errorf("no valid LLM configuration found for plan improvement agent: presetPlanImprovementLLM and presetPhaseLLM are both empty or invalid")
	}

	// Use minimal workspace tools (shell_command + human) for phase agent
	allTools, allExecutors := pim.BaseOrchestrator.PreparePhaseAgentTools()

	// Create agent config with the selected LLM config
	config := pim.CreateStandardAgentConfigWithLLM("plan-improvement-agent", 100, agents.OutputFormatStructured, llmConfigToUse)

	// Enable MCP tools from preset if available
	// Plan improvement agent can now access MCP servers configured in the preset
	selectedServers := pim.GetSelectedServers()
	selectedTools := pim.GetSelectedTools()
	mcpConfigPath := pim.GetMCPConfigPath()

	// Plan improvement agent uses simple agent mode (no code execution, no tool search)
	// Even with MCP tools, we don't need code execution for plan debugging
	// Phase agents always use simple mode UNLESS the provider requires code execution (claude-code, gemini-cli)
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(pim.presetPhaseLLM)
	config.UseToolSearchMode = false

	if len(selectedServers) > 0 && mcpConfigPath != "" {
		// Use preset's MCP configuration (simple agent mode)
		config.ServerNames = selectedServers
		config.SelectedTools = selectedTools
		config.MCPConfigPath = mcpConfigPath
		config.MCPSessionID = pim.GetMCPSessionID() // Share MCP connections with other agents
		pim.GetLogger().Info("🔧 Enabling MCP tools for plan improvement agent (simple agent mode):")
		pim.GetLogger().Info(fmt.Sprintf("   📡 Servers: %v", selectedServers))
		pim.GetLogger().Info(fmt.Sprintf("   🔧 Tools: %v", selectedTools))
	} else {
		// Fall back to no MCP servers (workspace tools only)
		config.ServerNames = []string{mcpclient.NoServers}
		pim.GetLogger().Info("🔧 No MCP servers configured in preset - plan improvement agent using workspace tools only")
	}

	// Large output virtual tools are enabled for plan improvement (agent may generate large feedback reports)

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewWorkflowPlanImprovementAgent(cfg, logger, tracer, eventBridge, pim.BaseOrchestrator)
	}

	// Use base orchestrator's CreateAndSetupStandardAgentWithConfig to avoid code duplication
	// This handles initialization, event bridge connection, and tool registration
	// Set overwriteSystemPrompt to true for plan improvement agent (replaces default MCP prompt with agent-specific prompt)
	agent, err := pim.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"plan-improvement",
		0, 0, // step, iteration
		"plan-improvement", // stepID (use phase name for phase-only agents)
		createAgentFunc,
		allTools,
		allExecutors,
		true, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup plan improvement agent: %w", err)
	}

	return agent, nil
}

// PlanImprovementOnly runs only the plan improvement phase (standalone, independent from other phases)
// This is a separate workflow phase that can be run independently
// runPath is optional - if provided (e.g., "runs/iteration-11" or "iteration-11"), it will be used directly
// If not provided or invalid, the function will ask the user via human feedback
func (pim *PlanImprovementManager) PlanImprovementOnly(ctx context.Context, originalWorkspacePath string, runPath string) (string, error) {
	pim.GetLogger().Info(fmt.Sprintf("📊 Starting standalone plan improvement for workspace: %s", originalWorkspacePath))

	// Store original workspace path
	var validatedRunPath string
	var err error

	// If runPath is provided, try to validate it first
	if runPath != "" {
		pim.GetLogger().Info(fmt.Sprintf("📊 Using provided run path: %s", runPath))
		validatedRunPath, err = pim.validateRunPath(ctx, originalWorkspacePath, runPath)
		if err != nil {
			pim.GetLogger().Warn(fmt.Sprintf("⚠️ Provided run path validation failed: %v, falling back to user input", err))
			// Fall back to asking user
			validatedRunPath, err = pim.requestAndValidateFullPath(ctx, originalWorkspacePath)
			if err != nil {
				return "", fmt.Errorf("failed to get validated path: %w", err)
			}
		} else {
			pim.GetLogger().Info(fmt.Sprintf("✅ Successfully validated provided run path: %s", validatedRunPath))
		}
	} else {
		// No run path provided - request and validate full path from user via blocking human feedback
		pim.GetLogger().Info(fmt.Sprintf("📊 Requesting full path from user for plan improvement analysis"))
		validatedRunPath, err = pim.requestAndValidateFullPath(ctx, originalWorkspacePath)
		if err != nil {
			return "", fmt.Errorf("failed to get validated path: %w", err)
		}
	}

	// Keep orchestrator workspace rooted at the workspace root.
	// Run artifacts live under workspace root: runs/<iteration>/...
	// This matches how execution/decision step logging writes files (uses workspacePath/runs/<selectedRunFolder>/...).
	pim.SetWorkspacePath(originalWorkspacePath)
	runWorkspacePath := fmt.Sprintf("%s/%s", originalWorkspacePath, validatedRunPath)
	pim.GetLogger().Info(fmt.Sprintf("✅ Using validated run path (relative to workspace root): %s", validatedRunPath))
	pim.GetLogger().Info(fmt.Sprintf("✅ Workspace root set to: %s", originalWorkspacePath))
	pim.GetLogger().Info(fmt.Sprintf("✅ Run workspace path set to: %s", runWorkspacePath))

	// Check if plan.json exists - REQUIRED for plan improvement
	// Plan.json should be in the original workspace, not in the user-provided path
	planPath := fmt.Sprintf("%s/planning/plan.json", originalWorkspacePath)
	planExist, existingPlan, err := pim.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing plan: %w", err)
	}
	if !planExist {
		return "", fmt.Errorf("plan.json not found at %s - planning must be run first as a separate phase", planPath)
	}

	// Plan exists - use it for plan improvement
	pim.GetLogger().Info(fmt.Sprintf("✅ Found plan.json with %d steps for plan improvement", len(existingPlan.Steps)))

	// Ask user what they want to improve via blocking human feedback BEFORE starting the agent
	pim.GetLogger().Info(fmt.Sprintf("📊 Requesting user input on what they want to improve"))
	userImprovementRequest, err := pim.requestUserImprovementGoal(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get improvement goal from user: %w", err)
	}
	pim.GetLogger().Info(fmt.Sprintf("✅ User improvement request: %s", userImprovementRequest))

	// Count sub-agents in orchestration steps before filtering
	totalSubAgentsBefore := countSubAgents(existingPlan)
	if totalSubAgentsBefore > 0 {
		pim.GetLogger().Info(fmt.Sprintf("📊 Found %d sub-agent(s) in orchestration steps", totalSubAgentsBefore))
	}

	// Filter out human input steps before passing to agent (they don't need optimization)
	filteredPlan := filterHumanInputSteps(existingPlan)
	humanInputCount := len(existingPlan.Steps) - len(filteredPlan.Steps)
	if humanInputCount > 0 {
		pim.GetLogger().Info(fmt.Sprintf("🔍 Filtered out %d human input step(s) from plan (no optimization needed)", humanInputCount))
	}

	// Count sub-agents after filtering
	totalSubAgentsAfter := countSubAgents(filteredPlan)
	if totalSubAgentsAfter > 0 {
		pim.GetLogger().Info(fmt.Sprintf("📊 After filtering: %d sub-agent(s) remain in orchestration steps", totalSubAgentsAfter))
	}
	if totalSubAgentsBefore != totalSubAgentsAfter {
		pim.GetLogger().Info(fmt.Sprintf("⚠️ Sub-agent count changed: %d → %d (some may have been human input steps)", totalSubAgentsBefore, totalSubAgentsAfter))
	}

	// Prepare plan JSON for template (using filtered plan)
	planJSONBytes, err := json.MarshalIndent(filteredPlan, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan to JSON: %w", err)
	}

	// Create execution results summary based on the selected run folder.
	// Execution/logs live under runs/<run>/..., while plan/learnings are at workspace root.
	// Conditionally include knowledgebase folder information based on preset setting.
	knowledgebaseSection := ""
	knowledgebaseExploreItem := ""
	if pim.useKnowledgebase {
		knowledgebaseSection = fmt.Sprintf(`
Knowledgebase folder (shared across all runs):
- %s/knowledgebase/ - persistent files across all runs (templates, reference data, configurations - NEVER deleted during cleanup)
`, originalWorkspacePath)
		knowledgebaseExploreItem = fmt.Sprintf(`
- Knowledgebase files in %s/knowledgebase/ (persistent across all runs, at workspace root)`, originalWorkspacePath)
	}

	executionResultsSummary := fmt.Sprintf(
		`Workspace root: %s
Selected run folder: %s

Run folder contains:
- %s/execution/ - step execution outputs
- %s/logs/ - validation and execution logs
%s
Use execute_shell_command with 'ls' to explore:
- Execution result files in %s/execution/%s
- Detailed logs in %s/logs/step-X/ including:
  * validation-{N}.json - validation responses for each validation attempt
  * execution/execution-attempt-{N}-iteration-{M}.json - execution results with retry/loop information
  * execution/execution-attempt-{N}-iteration-{M}-conversation.json - full conversation history for each execution attempt

Learnings are stored at workspace root:
- learnings/
- learnings/{step_id}/ (regular steps, using step IDs from plan.json)
- learnings/{step_id}/ (branch steps, using step IDs from plan.json where step_id is the branch step's own ID)
- learnings/{step_id}/ (orchestration sub-agents, using step IDs from plan.json where step_id is the sub-agent's own ID)
- learnings/{step_id}/ (todo task sub-agents, using step IDs from plan.json where step_id is the sub-agent's own ID)

Plan is stored at:
- planning/plan.json

Evaluation data (LLM-scored quality assessments) - ACCESS SCOPED TO THIS ITERATION ONLY:
- evaluation/evaluation_plan.json - defines evaluation criteria (shared)
- evaluation/learnings/{step_id}/ - learnings from evaluation runs (shared)
- evaluation/%s/evaluation_report.json - scored report for THIS iteration
  * Contains: total_score, max_possible_score, score_percentage, step_scores[]
  * Each step_scores entry has: step_id, score, max_score, reasoning, evidence, success_criteria
  * Use low-scoring steps (<50%%) to identify plan improvements needed
  * Read 'reasoning' and 'evidence' fields to understand WHY steps scored poorly
  * NOTE: You can only access evaluation data for the selected iteration, not other iterations`,
		originalWorkspacePath,
		validatedRunPath,
		validatedRunPath,
		validatedRunPath,
		knowledgebaseSection,
		validatedRunPath,
		knowledgebaseExploreItem,
		validatedRunPath,
		validatedRunPath, // For evaluation path
	)

	// Create plan improvement agent with run workspace path (for reading run artifacts)
	// and original workspace path (for reading planning/ and learnings/).
	planImprovementAgent, err := pim.createPlanImprovementAgent(ctx, runWorkspacePath, originalWorkspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to create plan improvement agent: %w", err)
	}

	// Prepare template variables
	// Use workspace root for plan/learnings, and runs/<run> for execution/logs.
	// Include evaluation/ for reading evaluation reports and plans.
	allowedPaths := fmt.Sprintf("['planning/', 'learnings/', '%s/', 'evaluation/']", validatedRunPath)
	planImprovementTemplateVars := map[string]string{
		// Workspace root (plan/learnings live here)
		"WorkspacePath": originalWorkspacePath,
		// Run information (execution/logs live here)
		"RunPathRelative":         validatedRunPath, // e.g. "runs/iteration-11"
		"RunWorkspacePath":        runWorkspacePath, // absolute path
		"ValidatedRunPath":        validatedRunPath, // backward-compat for prompt/template usage
		"OriginalWorkspacePath":   originalWorkspacePath,
		"PlanJSON":                string(planJSONBytes),
		"ExecutionResultsSummary": executionResultsSummary,
		"AllowedPaths":            allowedPaths,
		"SessionID":               pim.sessionID,
		"WorkflowID":              pim.workflowID,
		"UserImprovementRequest":  userImprovementRequest, // User's improvement goal from blocking feedback
		"IsCodeExecutionMode":     fmt.Sprintf("%v", requiresCodeExecutionForProvider(pim.presetPhaseLLM)),
	}

	// Execute plan improvement agent
	pim.GetLogger().Info(fmt.Sprintf("📊 Executing plan improvement agent..."))
	result, conversationHistory, err := planImprovementAgent.Execute(ctx, planImprovementTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("plan improvement agent execution failed: %w", err)
	}

	pim.GetLogger().Info(fmt.Sprintf("✅ Plan improvement completed successfully"))
	pim.GetLogger().Info(fmt.Sprintf("📊 Plan improvement result: %s", result))

	_ = conversationHistory // Conversation history not used for standalone plan improvement

	return result, nil
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the shared readPlanFromFile helper which acquires planFileMutex for thread-safe access
func (pim *PlanImprovementManager) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {
	pim.GetLogger().Info(fmt.Sprintf("🔍 Checking for existing plan at %s", planPath))

	// Extract workspace path from planPath (planPath is workspacePath/planning/plan.json)
	// readPlanFromFile expects workspacePath and constructs the path internally
	workspacePath := filepath.Dir(filepath.Dir(planPath))

	// Use the shared readPlanFromFile helper which acquires planFileMutex for thread-safe access
	plan, err := readPlanFromFile(ctx, workspacePath, pim.ReadWorkspaceFile)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			pim.GetLogger().Info(fmt.Sprintf("📋 No existing plan found: %v", err))
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	pim.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(plan.Steps)))
	return true, plan, nil
}

// filterHumanInputSteps removes human input steps from the plan (including from branch steps)
// Human input steps don't need optimization - they only ask questions and block for user input
func filterHumanInputSteps(plan *PlanningResponse) *PlanningResponse {
	filteredPlan := &PlanningResponse{
		Steps: make([]PlanStepInterface, 0, len(plan.Steps)),
	}

	// Helper function to recursively filter steps (handles branch steps)
	var filterStep func(step PlanStepInterface) PlanStepInterface
	filterStep = func(step PlanStepInterface) PlanStepInterface {
		// Skip human input steps
		if isHumanInputStep(step) {
			return nil
		}

		// Handle conditional steps (they have branch steps that also need filtering)
		if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
			filteredConditional := &ConditionalPlanStep{
				Type:              conditionalStep.Type,
				CommonStepFields:  conditionalStep.CommonStepFields,
				ConditionQuestion: conditionalStep.ConditionQuestion,
				ConditionContext:  conditionalStep.ConditionContext,
				IfTrueNextStepID:  conditionalStep.IfTrueNextStepID,
				IfFalseNextStepID: conditionalStep.IfFalseNextStepID,
				IfTrueSteps:       make([]PlanStepInterface, 0),
				IfFalseSteps:      make([]PlanStepInterface, 0),
				AgentConfigs:      conditionalStep.AgentConfigs,
			}

			// Filter if_true_steps
			for _, branchStep := range conditionalStep.IfTrueSteps {
				if filteredBranchStep := filterStep(branchStep); filteredBranchStep != nil {
					filteredConditional.IfTrueSteps = append(filteredConditional.IfTrueSteps, filteredBranchStep)
				}
			}

			// Filter if_false_steps
			for _, branchStep := range conditionalStep.IfFalseSteps {
				if filteredBranchStep := filterStep(branchStep); filteredBranchStep != nil {
					filteredConditional.IfFalseSteps = append(filteredConditional.IfFalseSteps, filteredBranchStep)
				}
			}

			return filteredConditional
		}

		// Handle orchestration steps (they have sub-agent steps that also need filtering)
		if orchestrationStep, ok := step.(*OrchestrationPlanStep); ok {
			filteredOrchestration := &OrchestrationPlanStep{
				Type:                orchestrationStep.Type,
				ID:                  orchestrationStep.ID,
				Title:               orchestrationStep.Title,
				OrchestrationStep:   filterStep(orchestrationStep.OrchestrationStep),
				NextStepID:          orchestrationStep.NextStepID,
				OrchestrationRoutes: make([]PlanOrchestrationRoute, 0, len(orchestrationStep.OrchestrationRoutes)),
				AgentConfigs:        orchestrationStep.AgentConfigs,
			}

			// Filter orchestration routes (sub-agents)
			// IMPORTANT: We keep ALL non-human-input sub-agents - they need optimization
			for _, route := range orchestrationStep.OrchestrationRoutes {
				// Skip only if sub-agent is a human input step
				if isHumanInputStep(route.SubAgentStep) {
					continue // Skip this route (human input sub-agent)
				}

				// Recursively filter sub-agent step (in case it has nested structures like conditional branches)
				filteredSubAgentStep := filterStep(route.SubAgentStep)
				if filteredSubAgentStep != nil {
					// Create a copy of the route with filtered sub-agent
					filteredRoute := PlanOrchestrationRoute{
						RouteID:       route.RouteID,
						RouteName:     route.RouteName,
						Condition:     route.Condition,
						SubAgentStep:  filteredSubAgentStep,
						ContextToPass: route.ContextToPass,
					}
					filteredOrchestration.OrchestrationRoutes = append(filteredOrchestration.OrchestrationRoutes, filteredRoute)
				}
			}

			return filteredOrchestration
		}

		// Handle todo task steps (they have sub-agent steps in predefined_routes that also need filtering)
		if todoTaskStep, ok := step.(*TodoTaskPlanStep); ok {
			filteredTodoTask := &TodoTaskPlanStep{
				Type:               todoTaskStep.Type,
				ID:                 todoTaskStep.ID,
				Title:              todoTaskStep.Title,
				TodoTaskStep:       filterStep(todoTaskStep.TodoTaskStep),
				EnableGenericAgent: todoTaskStep.EnableGenericAgent,
				NextStepID:         todoTaskStep.NextStepID,
				PredefinedRoutes:   make([]PlanOrchestrationRoute, 0, len(todoTaskStep.PredefinedRoutes)),
				AgentConfigs:       todoTaskStep.AgentConfigs,
			}

			// Filter predefined routes (sub-agents)
			for _, route := range todoTaskStep.PredefinedRoutes {
				// Skip only if sub-agent is a human input step
				if isHumanInputStep(route.SubAgentStep) {
					continue // Skip this route (human input sub-agent)
				}

				// Recursively filter sub-agent step (in case it has nested structures like conditional branches)
				filteredSubAgentStep := filterStep(route.SubAgentStep)
				if filteredSubAgentStep != nil {
					// Create a copy of the route with filtered sub-agent
					filteredRoute := PlanOrchestrationRoute{
						RouteID:       route.RouteID,
						RouteName:     route.RouteName,
						Condition:     route.Condition,
						SubAgentStep:  filteredSubAgentStep,
						ContextToPass: route.ContextToPass,
					}
					filteredTodoTask.PredefinedRoutes = append(filteredTodoTask.PredefinedRoutes, filteredRoute)
				}
			}

			return filteredTodoTask
		}

		// For all other step types, keep as-is (regular, decision, loop steps)
		return step
	}

	// Filter all top-level steps
	for _, step := range plan.Steps {
		if filteredStep := filterStep(step); filteredStep != nil {
			filteredPlan.Steps = append(filteredPlan.Steps, filteredStep)
		}
	}

	return filteredPlan
}

// countSubAgents counts all sub-agents in orchestration steps (recursively)
func countSubAgents(plan *PlanningResponse) int {
	count := 0
	for _, step := range plan.Steps {
		if orchestrationStep, ok := step.(*OrchestrationPlanStep); ok {
			count += len(orchestrationStep.OrchestrationRoutes)
			// Recursively count sub-agents in conditional steps (if any sub-agents are conditional)
			for _, route := range orchestrationStep.OrchestrationRoutes {
				if conditionalSubAgent, ok := route.SubAgentStep.(*ConditionalPlanStep); ok {
					// Count branch steps in conditional sub-agents
					count += len(conditionalSubAgent.IfTrueSteps)
					count += len(conditionalSubAgent.IfFalseSteps)
				}
			}
		}

		// Handle todo task steps (predefined routes are sub-agents)
		if todoTaskStep, ok := step.(*TodoTaskPlanStep); ok {
			count += len(todoTaskStep.PredefinedRoutes)
			// Recursively count sub-agents in conditional steps (if any sub-agents are conditional)
			for _, route := range todoTaskStep.PredefinedRoutes {
				if conditionalSubAgent, ok := route.SubAgentStep.(*ConditionalPlanStep); ok {
					// Count branch steps in conditional sub-agents
					count += len(conditionalSubAgent.IfTrueSteps)
					count += len(conditionalSubAgent.IfFalseSteps)
				}
			}
		}

		// Also check if conditional steps contain orchestration or todo task steps
		if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
			for _, branchStep := range conditionalStep.IfTrueSteps {
				if branchOrchestration, ok := branchStep.(*OrchestrationPlanStep); ok {
					count += len(branchOrchestration.OrchestrationRoutes)
				}
				if branchTodoTask, ok := branchStep.(*TodoTaskPlanStep); ok {
					count += len(branchTodoTask.PredefinedRoutes)
				}
			}
			for _, branchStep := range conditionalStep.IfFalseSteps {
				if branchOrchestration, ok := branchStep.(*OrchestrationPlanStep); ok {
					count += len(branchOrchestration.OrchestrationRoutes)
				}
				if branchTodoTask, ok := branchStep.(*TodoTaskPlanStep); ok {
					count += len(branchTodoTask.PredefinedRoutes)
				}
			}
		}
	}
	return count
}

// requestUserImprovementGoal asks the user what they want to improve via blocking human feedback
// Returns the user's improvement request/goal
func (pim *PlanImprovementManager) requestUserImprovementGoal(ctx context.Context) (string, error) {
	// Generate unique request ID
	requestID := fmt.Sprintf("plan_improvement_goal_%d", time.Now().UnixNano())

	promptMessage := `What would you like to improve in the plan?

Examples:
- "Fix the validation criteria for step 3 - it's not checking the right files"
- "The orchestration step is looping forever, help me fix the exit conditions"
- "Step 2 is failing because the JSON output is too large"
- "Improve success criteria to be more specific and file-verifiable"

Please describe what you want to improve or debug:`

	// Request human feedback (blocking call)
	approved, userGoal, err := pim.RequestHumanFeedback(
		ctx,
		requestID,
		promptMessage,
		"",
		pim.sessionID,
		pim.workflowID,
	)
	if err != nil {
		return "", fmt.Errorf("failed to get improvement goal from user: %w", err)
	}

	// If user clicked Approve without providing a goal, use a default
	if approved && strings.TrimSpace(userGoal) == "" {
		return "Analyze the plan and execution results for potential improvements.", nil
	}

	// Clean up the goal
	userGoal = strings.TrimSpace(userGoal)
	if userGoal == "" {
		return "Analyze the plan and execution results for potential improvements.", nil
	}

	return userGoal, nil
}

// requestAndValidateFullPath asks the user for the path via blocking human feedback and validates it has execution/ folder
// User provides paths like "iteration-11" or "iteration-11/group-7" (relative to runs/)
// Returns the full path relative to original workspace (e.g., "runs/iteration-11" or "runs/iteration-11/group-7")
func (pim *PlanImprovementManager) requestAndValidateFullPath(ctx context.Context, originalWorkspacePath string) (string, error) {
	maxAttempts := 5
	attempt := 0

	for attempt < maxAttempts {
		attempt++

		// Generate unique request ID
		requestID := fmt.Sprintf("plan_improvement_full_path_%d_%d", attempt, time.Now().UnixNano())

		// Ask user for the path
		var promptMessage string
		if attempt == 1 {
			promptMessage = fmt.Sprintf("Please provide the folder path to analyze (relative to runs/ folder).\n\nExamples:\n- 'iteration-11' - to analyze a specific iteration\n- 'iteration-11/group-7' - to analyze a specific group (using group ID)\n- 'iteration-11/production' - to analyze a specific group (using display name)\n\nThe path must contain an execution/ folder.\n\nThe workspace root is: %s", originalWorkspacePath)
		} else {
			promptMessage = fmt.Sprintf("The path you provided doesn't exist or doesn't contain an execution/ folder. Please provide a valid path relative to runs/ folder (attempt %d/%d).\n\nExamples: 'iteration-11', 'iteration-11/group-7', or 'iteration-11/production'\n\nThe workspace root is: %s", attempt, maxAttempts, originalWorkspacePath)
		}

		// Request human feedback (blocking call)
		approved, userPath, err := pim.RequestHumanFeedback(
			ctx,
			requestID,
			promptMessage,
			"",
			pim.sessionID,
			pim.workflowID,
		)
		if err != nil {
			return "", fmt.Errorf("failed to get path from user: %w", err)
		}

		// If user clicked Approve without providing a path, treat as cancellation
		if approved && strings.TrimSpace(userPath) == "" {
			return "", fmt.Errorf(fmt.Sprintf("user approved without providing a path"), nil)
		}

		// Clean up the path (remove leading/trailing spaces and slashes, remove runs/ prefix if included)
		userPath = strings.TrimSpace(userPath)
		userPath = strings.TrimPrefix(userPath, "runs/")
		userPath = strings.TrimPrefix(userPath, "/")
		userPath = strings.TrimSuffix(userPath, "/")

		if userPath == "" {
			pim.GetLogger().Warn(fmt.Sprintf("⚠️ Empty path provided, asking again"))
			continue
		}

		// Construct full path: runs/{userPath}
		// Examples: runs/iteration-11 or runs/iteration-11/group-7
		fullPath := fmt.Sprintf("%s/runs/%s", originalWorkspacePath, userPath)
		executionPath := fmt.Sprintf("%s/execution", fullPath)

		pim.GetLogger().Info(fmt.Sprintf("🔍 Validating path: %s (full path: %s, checking execution: %s)", userPath, fullPath, executionPath))

		// First check if the base path exists
		_, err = pim.ListWorkspaceFiles(ctx, fullPath)
		if err != nil {
			pim.GetLogger().Warn(fmt.Sprintf("⚠️ Path validation failed: %s does not exist: %v", fullPath, err))
			continue
		}

		// Check if execution/ folder exists in this path
		files, err := pim.ListWorkspaceFiles(ctx, executionPath)
		if err != nil {
			// execution/ folder doesn't exist
			pim.GetLogger().Warn(fmt.Sprintf("⚠️ Path validation failed: %s does not contain an execution/ folder: %v", fullPath, err))
			continue
		}

		// Path exists and has execution/ folder
		pim.GetLogger().Info(fmt.Sprintf("✅ Validated path: runs/%s (found %d items in execution/ folder)", userPath, len(files)))

		// Return the full path relative to original workspace (e.g., "runs/iteration-11")
		return fmt.Sprintf("runs/%s", userPath), nil
	}

	return "", fmt.Errorf("failed to get valid path after %d attempts", maxAttempts)
}

// validateRunPath validates a run path without asking the user
// runPath can be in format "runs/iteration-11", "iteration-11", or "iteration-11/group-7"
// Returns the full path relative to original workspace (e.g., "runs/iteration-11") if valid
func (pim *PlanImprovementManager) validateRunPath(ctx context.Context, originalWorkspacePath string, runPath string) (string, error) {
	// Clean up the path (remove leading/trailing spaces and slashes, remove runs/ prefix if included)
	cleanedPath := strings.TrimSpace(runPath)
	cleanedPath = strings.TrimPrefix(cleanedPath, "runs/")
	cleanedPath = strings.TrimPrefix(cleanedPath, "/")
	cleanedPath = strings.TrimSuffix(cleanedPath, "/")

	if cleanedPath == "" {
		return "", fmt.Errorf("empty run path provided")
	}

	// Construct full path: runs/{cleanedPath}
	// Examples: runs/iteration-11 or runs/iteration-11/group-7
	fullPath := fmt.Sprintf("%s/runs/%s", originalWorkspacePath, cleanedPath)
	executionPath := fmt.Sprintf("%s/execution", fullPath)

	pim.GetLogger().Info(fmt.Sprintf("🔍 Validating run path: %s (full path: %s, checking execution: %s)", cleanedPath, fullPath, executionPath))

	// First check if the base path exists
	_, err := pim.ListWorkspaceFiles(ctx, fullPath)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s: %w", fullPath, err)
	}

	// Check if execution/ folder exists in this path
	files, err := pim.ListWorkspaceFiles(ctx, executionPath)
	if err != nil {
		return "", fmt.Errorf("path does not contain an execution/ folder: %s: %w", fullPath, err)
	}

	// Path exists and has execution/ folder
	pim.GetLogger().Info(fmt.Sprintf("✅ Validated run path: runs/%s (found %d items in execution/ folder)", cleanedPath, len(files)))

	// Return the full path relative to original workspace (e.g., "runs/iteration-11")
	return fmt.Sprintf("runs/%s", cleanedPath), nil
}

// Execute implements the OrchestratorAgent interface
func (agent *WorkflowPlanImprovementAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	planJSON := templateVars["PlanJSON"]
	executionResultsSummary := templateVars["ExecutionResultsSummary"]
	runPathRelative := templateVars["RunPathRelative"]
	validatedRunPath := templateVars["ValidatedRunPath"]
	runWorkspacePath := templateVars["RunWorkspacePath"]

	// runPathRelative is required for correct log/execution context; fail fast if missing
	if strings.TrimSpace(runPathRelative) == "" {
		return "", nil, fmt.Errorf("RunPathRelative is required for plan improvement but was empty")
	}

	// Provide default allowed paths if not present
	allowedPaths := templateVars["AllowedPaths"]
	if allowedPaths == "" {
		// Allow access to the entire runs/ folder for comparative analysis
		allowedPaths = "['planning/', 'learnings/', 'runs/']"
	}

	// Prepare template variables
	isCodeExecutionMode := agent.GetConfig().UseCodeExecutionMode
	planImprovementTemplateVars := map[string]string{
		"WorkspacePath":           workspacePath,
		"RunPathRelative":         runPathRelative,
		"RunWorkspacePath":        runWorkspacePath,
		"PlanJSON":                planJSON,
		"ExecutionResultsSummary": executionResultsSummary,
		"AllowedPaths":            allowedPaths,
		"ValidatedRunPath":        validatedRunPath,
		"SessionID":               templateVars["SessionID"],
		"WorkflowID":              templateVars["WorkflowID"],
		"UserImprovementRequest":  templateVars["UserImprovementRequest"],
		"IsCodeExecutionMode":     fmt.Sprintf("%v", isCodeExecutionMode),
	}

	// Create template data for plan improvement
	templateData := WorkflowPlanImprovementTemplate{
		WorkspacePath:           workspacePath,
		RunPathRelative:         runPathRelative,
		RunWorkspacePath:        runWorkspacePath,
		PlanJSON:                planJSON,
		ExecutionResultsSummary: executionResultsSummary,
		AllowedPaths:            allowedPaths,
		UserImprovementRequest:  templateVars["UserImprovementRequest"],
		IsCodeExecutionMode:     isCodeExecutionMode,
	}

	// Get logger and MCP agent from base agent
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	var logger loggerv2.Logger
	var mcpAgent *mcpagent.Agent
	if baseAgent != nil {
		mcpAgent = baseAgent.Agent()
		if mcpAgent != nil && mcpAgent.Logger != nil {
			logger = mcpAgent.Logger
		}
	}

	if mcpAgent == nil {
		return "", nil, fmt.Errorf(fmt.Sprintf("MCP agent is not initialized"), nil)
	}

	// Get readFile, writeFile, and moveFile functions from base orchestrator
	// We need to access the base orchestrator to get these methods
	// Since agent has baseOrchestrator reference, we can use it
	var readFile func(context.Context, string) (string, error)
	var writeFile func(context.Context, string, string) error
	var moveFile func(context.Context, string, string) error
	if agent.baseOrchestrator != nil {
		readFile = agent.baseOrchestrator.ReadWorkspaceFile
		writeFile = agent.baseOrchestrator.WriteWorkspaceFile
		moveFile = agent.baseOrchestrator.MoveWorkspaceFile
	} else {
		return "", nil, fmt.Errorf(fmt.Sprintf("base orchestrator is not initialized"), nil)
	}

	// Reset changelog session at the start of plan improvement agent execution
	// This ensures all changes during this execution are written to the same changelog file
	resetChangelogSession()

	// Store initial plan state BEFORE agent execution for changelog comparison
	// This captures the state before any modifications are made
	var initialPlan *PlanningResponse
	existingPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		// If plan doesn't exist or can't be read, use empty plan
		initialPlan = &PlanningResponse{Steps: []PlanStep{}}
		if logger != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to read initial plan for changelog comparison: %v, using empty plan", err))
		}
	} else {
		// Deep copy the existing plan to avoid mutations
		planJSONBytes, err := json.Marshal(existingPlan)
		if err == nil {
			var copiedPlan PlanningResponse
			if err := json.Unmarshal(planJSONBytes, &copiedPlan); err == nil {
				initialPlan = &copiedPlan
				if logger != nil {
					logger.Info(fmt.Sprintf("📝 Stored initial plan state (%d steps) for changelog comparison", len(initialPlan.Steps)))
				}
			} else {
				// Deep copy failed - use empty plan to ensure changelog generation still works
				initialPlan = &PlanningResponse{Steps: []PlanStep{}}
				if logger != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to deep copy initial plan: %v, using empty plan", err))
				}
			}
		} else {
			// Marshal failed - use empty plan to ensure changelog generation still works
			initialPlan = &PlanningResponse{Steps: []PlanStep{}}
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to marshal initial plan: %v, using empty plan", err))
			}
		}
	}

	// Register all plan modification tools using shared function
	// Pass unlock function to automatically unlock learnings when plan is modified
	// Use base orchestrator to create unlock function (plan improvement agent only has base orchestrator)
	// Note: For plan improvement agent, we use workspacePath (which is the original workspace root)
	// since learnings are stored in the original workspace, not in run folders
	var unlockLearningsFunc func(context.Context, string, int) error
	if agent.baseOrchestrator != nil {
		// Use workspacePath for unlock operations (learnings are in workspace root)
		unlockLearningsFunc = createUnlockLearningsFunctionFromBase(agent.baseOrchestrator, workspacePath)
	}
	if err := registerPlanModificationTools(mcpAgent, workspacePath, logger, readFile, writeFile, moveFile, "plan improvement agent", unlockLearningsFunc); err != nil {
		return "", nil, err
	}

	// Update code execution registry to include newly registered plan modification tools
	// Without this, CLI providers (claude-code, gemini-cli) won't see the plan modification tools
	// because the registry was already built during CreateAndSetupStandardAgentWithConfig with only the initial tools
	if agent.GetConfig().UseCodeExecutionMode {
		if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to update code execution registry with plan modification tools: %v", err))
			}
		} else {
			if logger != nil {
				logger.Info("✅ Code execution registry updated with plan modification tools for CLI provider")
			}
		}
	}

	// Generate system prompt and user message separately
	systemPrompt := agent.planImprovementSystemPromptProcessor(planImprovementTemplateVars)
	userMessage := agent.planImprovementUserMessageProcessor(planImprovementTemplateVars)

	// Maximum iterations for plan improvement analysis
	maxIterations := 20
	iteration := 0
	currentResult := ""
	currentConversationHistory := conversationHistory

	// Extract sessionID and workflowID from template vars
	sessionID := templateVars["SessionID"]
	workflowID := templateVars["WorkflowID"]

	// Emit plan improvement started event
	if agent.baseOrchestrator != nil {
		eventBridge := agent.baseOrchestrator.GetContextAwareBridge()
		if eventBridge != nil {
			startedEvent := &orchestrator_events.OrchestratorAgentStartEvent{
				BaseEventData: baseevents.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				AgentType: "plan-improvement",
				AgentName: "plan-improvement-agent",
				Objective: "Improve plan based on execution results and user feedback",
				InputData: planImprovementTemplateVars,
			}
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type:      orchestrator_events.OrchestratorAgentStart,
				Timestamp: time.Now(),
				Data:      startedEvent,
			})
			if logger != nil {
				logger.Info(fmt.Sprintf("📤 Emitted plan improvement started event"))
			}
		}
	}

	// Main execution loop with blocking human feedback
	for iteration < maxIterations {
		iteration++
		if logger != nil {
			logger.Info(fmt.Sprintf("📊 Plan improvement agent iteration %d/%d", iteration, maxIterations))
		}

		// Create a simple input processor that returns the user message
		inputProcessor := func(map[string]string) string {
			return userMessage
		}

		// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
		result, updatedConversationHistory, err := agent.ExecuteWithTemplateValidation(ctx, planImprovementTemplateVars, inputProcessor, currentConversationHistory, templateData, systemPrompt, true)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedConversationHistory

		// Check if plan modification tools were called in this iteration and emit event immediately
		// This ensures the frontend is notified of plan changes right away, not waiting for agent completion
		if agent.baseOrchestrator != nil {
			// Extract tool calls from this iteration's conversation history
			toolCalls := ExtractToolCallsFromMessages(updatedConversationHistory)
			planUpdateToolCalled := false
			for _, toolName := range toolCalls {
				if IsPlanModificationTool(toolName) || IsStepConfigModificationTool(toolName) {
					planUpdateToolCalled = true
					break
				}
			}

			if planUpdateToolCalled {
				if logger != nil {
					logger.Info(fmt.Sprintf("🔍 [PlanImprovementAgent] Plan modification tool detected in iteration %d, emitting event immediately", iteration))
				}
				CheckAndEmitPlanUpdateEvent(ctx, agent.baseOrchestrator, updatedConversationHistory, workspacePath, readFile)
			}
		}

		// Generate changelog from plan diff (AFTER each iteration, BEFORE human feedback)
		// This captures ALL changes made during the agent execution session so far in one comprehensive changelog entry
		// Ensure initialPlan is never nil (safety check)
		if initialPlan == nil {
			initialPlan = &PlanningResponse{Steps: []PlanStep{}}
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ initialPlan was nil, using empty plan for changelog generation"))
			}
		}

		planResponse, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to read plan for changelog generation (iteration %d): %v", iteration, err))
			}
		} else {
			if logger != nil {
				logger.Info(fmt.Sprintf("📝 Generating changelog after iteration %d: initialPlan has %d steps, planResponse has %d steps", iteration, len(initialPlan.Steps), len(planResponse.Steps)))
			}
			if err := generateChangelogFromPlanDiff(ctx, workspacePath, initialPlan, planResponse, readFile, writeFile, logger); err != nil {
				if logger != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to generate changelog from plan diff (iteration %d): %v", iteration, err))
				}
				// Don't fail the entire operation if changelog generation fails
			} else {
				if logger != nil {
					logger.Info(fmt.Sprintf("✅ Changelog generation completed successfully after iteration %d", iteration))
				}
			}
		}

		// After execution, ask if user wants to continue (blocking feedback)
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			if logger != nil {
				logger.Info(fmt.Sprintf("📊 Plan improvement agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations))
			}

			// Generate unique request ID
			requestID := fmt.Sprintf("plan_improvement_continue_%d_%d", iteration, time.Now().UnixNano())

			// Request human feedback (blocking call)
			approved, feedback, err := agent.baseOrchestrator.RequestHumanFeedback(
				ctx,
				requestID,
				fmt.Sprintf("Plan improvement analysis is complete (iteration %d/%d). Would you like to ask more questions about the plan or request additional improvements?", iteration, maxIterations),
				currentResult,
				sessionID,
				workflowID,
			)
			if err != nil {
				if logger != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to get user feedback: %v", err))
				}
				// Continue without blocking if feedback fails
				break
			}

			// If user clicked Approve button, we're done
			if approved {
				if logger != nil {
					logger.Info(fmt.Sprintf("✅ User approved - plan improvement complete"))
				}
				break
			}

			// User provided feedback/question - always pass it to the agent and continue
			if feedback != "" && strings.TrimSpace(feedback) != "" {
				if logger != nil {
					logger.Info(fmt.Sprintf("📝 User provided feedback: %s", feedback))
				}
				// Use feedback directly as user message for next iteration
				// Note: BaseAgent.Execute() will automatically add it to conversation history
				userMessage = feedback
			} else {
				// No feedback provided but not approved - continue with same message
				if logger != nil {
					logger.Info(fmt.Sprintf("ℹ️ No feedback provided, continuing with same context"))
				}
			}
		} else {
			// Reached max iterations or no base orchestrator
			if logger != nil {
				logger.Info(fmt.Sprintf("📊 Reached maximum iterations (%d) or no base orchestrator, ending conversation", maxIterations))
			}
			break
		}
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("📊 Plan improvement completed after %d iterations", iteration))
	}

	// Final changelog generation (safety measure - ensures changelog is written even if user never responded to blocking feedback)
	// This captures the final state after all iterations complete
	// Ensure initialPlan is never nil (safety check)
	if initialPlan == nil {
		initialPlan = &PlanningResponse{Steps: []PlanStep{}}
		if logger != nil {
			logger.Warn(fmt.Sprintf("⚠️ initialPlan was nil, using empty plan for final changelog generation"))
		}
	}

	planResponse, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		if logger != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to read plan for final changelog generation: %v", err))
		}
	} else {
		if logger != nil {
			logger.Info(fmt.Sprintf("📝 Generating final changelog after all iterations: initialPlan has %d steps, planResponse has %d steps", len(initialPlan.Steps), len(planResponse.Steps)))
		}
		if err := generateChangelogFromPlanDiff(ctx, workspacePath, initialPlan, planResponse, readFile, writeFile, logger); err != nil {
			if logger != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to generate final changelog from plan diff: %v", err))
			}
			// Don't fail the entire operation if changelog generation fails
		} else {
			if logger != nil {
				logger.Info(fmt.Sprintf("✅ Final changelog generation completed successfully"))
			}
		}
	}

	// Check if plan modification tools were called and emit event if needed
	// This ensures the frontend is notified of plan changes
	if logger != nil {
		logger.Info(fmt.Sprintf("🔍 [PlanImprovementAgent] Calling CheckAndEmitPlanUpdateEvent (baseOrchestrator: %v, conversationHistory length: %d)", agent.baseOrchestrator != nil, len(currentConversationHistory)))
	}
	CheckAndEmitPlanUpdateEvent(ctx, agent.baseOrchestrator, currentConversationHistory, workspacePath, readFile)
	if logger != nil {
		logger.Info(fmt.Sprintf("🔍 [PlanImprovementAgent] CheckAndEmitPlanUpdateEvent call completed"))
	}

	// Emit plan improvement completed event
	if agent.baseOrchestrator != nil {
		eventBridge := agent.baseOrchestrator.GetContextAwareBridge()
		if eventBridge != nil {
			completedEvent := &orchestrator_events.OrchestratorAgentEndEvent{
				BaseEventData: baseevents.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				AgentType: "plan-improvement",
				AgentName: "plan-improvement-agent",
				Objective: "Improve plan based on execution results and user feedback",
				Result:    currentResult,
				Success:   true,
				InputData: planImprovementTemplateVars,
			}
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type:      orchestrator_events.OrchestratorAgentEnd,
				Timestamp: time.Now(),
				Data:      completedEvent,
			})
			if logger != nil {
				logger.Info(fmt.Sprintf("📤 Emitted plan improvement completed event"))
			}
		}
	}

	return currentResult, currentConversationHistory, nil
}

// planImprovementSystemPromptProcessor creates the system prompt for plan improvement
func (agent *WorkflowPlanImprovementAgent) planImprovementSystemPromptProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := improvementSystemTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing improvement system prompt template: " + err.Error()
	}
	return result.String()
}

func (agent *WorkflowPlanImprovementAgent) planImprovementUserMessageProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := improvementUserTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing improvement user message template: " + err.Error()
	}
	return result.String()
}
