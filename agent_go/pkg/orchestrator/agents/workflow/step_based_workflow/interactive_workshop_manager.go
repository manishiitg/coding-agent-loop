package step_based_workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"os"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/shared"
	"mcp-agent-builder-go/agent_go/pkg/skills"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// knownWorkspaceToolNames lists workspace/system tool names that are NOT from MCP servers.
// Used by analyze_step to distinguish MCP tools from built-in tools in execution logs.
var knownWorkspaceToolNames = map[string]bool{
	"execute_shell_command":     true,
	"diff_patch_workspace_file": true,
	"read_image":                true,
	"read_pdf":                  true,
	"human_feedback":            true,
	"agent_browser":             true,
	"search_tools":              true,
	"add_tool":                  true,
}

// ============================================================================
// Workshop Step Hierarchy — recursive step discovery for inner steps
// ============================================================================

// WorkshopStepInfo describes a step in the plan, including its position in the hierarchy.
type WorkshopStepInfo struct {
	Step       PlanStepInterface
	ParentID   string // empty for top-level steps
	ParentType StepType
	BranchName string // e.g. "if_true", "if_false", "route:route-id", "todo_task_step"
	TopIndex   int    // 1-based index of the top-level step this belongs to (-1 if inner)
	IsOrphan   bool   // true for orphan steps (workshop-only, not in main execution flow)
}

// collectAllSteps returns a flat list of all steps in the plan, including inner steps
// from conditional branches, orchestration routes, and todo task sub-agents.
func collectAllSteps(steps []PlanStepInterface) []WorkshopStepInfo {
	var result []WorkshopStepInfo
	for i, step := range steps {
		result = append(result, WorkshopStepInfo{
			Step:     step,
			TopIndex: i + 1,
		})
		result = append(result, collectInnerSteps(step)...)
	}
	return result
}

// collectAllStepsWithOrphans returns a flat list of all steps including orphan steps.
func collectAllStepsWithOrphans(steps []PlanStepInterface, orphanSteps []PlanStepInterface) []WorkshopStepInfo {
	result := collectAllSteps(steps)
	for _, step := range orphanSteps {
		result = append(result, WorkshopStepInfo{
			Step:     step,
			TopIndex: -1,
			IsOrphan: true,
		})
		result = append(result, collectInnerSteps(step)...)
	}
	return result
}

// collectInnerSteps recursively extracts inner steps from a step.
func collectInnerSteps(step PlanStepInterface) []WorkshopStepInfo {
	var result []WorkshopStepInfo
	parentID := step.GetID()
	parentType := step.StepType()

	switch s := step.(type) {
	case *ConditionalPlanStep:
		for _, inner := range s.IfTrueSteps {
			result = append(result, WorkshopStepInfo{
				Step: inner, ParentID: parentID, ParentType: parentType,
				BranchName: "if_true", TopIndex: -1,
			})
			result = append(result, collectInnerSteps(inner)...)
		}
		for _, inner := range s.IfFalseSteps {
			result = append(result, WorkshopStepInfo{
				Step: inner, ParentID: parentID, ParentType: parentType,
				BranchName: "if_false", TopIndex: -1,
			})
			result = append(result, collectInnerSteps(inner)...)
		}
	case *OrchestrationPlanStep:
		if s.OrchestrationStep != nil {
			result = append(result, WorkshopStepInfo{
				Step: s.OrchestrationStep, ParentID: parentID, ParentType: parentType,
				BranchName: "orchestration_step", TopIndex: -1,
			})
			result = append(result, collectInnerSteps(s.OrchestrationStep)...)
		}
		for _, route := range s.OrchestrationRoutes {
			if route.SubAgentStep != nil {
				result = append(result, WorkshopStepInfo{
					Step: route.SubAgentStep, ParentID: parentID, ParentType: parentType,
					BranchName: fmt.Sprintf("route:%s", route.RouteID), TopIndex: -1,
				})
				result = append(result, collectInnerSteps(route.SubAgentStep)...)
			}
		}
	case *TodoTaskPlanStep:
		if s.TodoTaskStep != nil {
			result = append(result, WorkshopStepInfo{
				Step: s.TodoTaskStep, ParentID: parentID, ParentType: parentType,
				BranchName: "todo_task_step", TopIndex: -1,
			})
			result = append(result, collectInnerSteps(s.TodoTaskStep)...)
		}
		for _, route := range s.PredefinedRoutes {
			if route.SubAgentStep != nil {
				result = append(result, WorkshopStepInfo{
					Step: route.SubAgentStep, ParentID: parentID, ParentType: parentType,
					BranchName: fmt.Sprintf("route:%s", route.RouteID), TopIndex: -1,
				})
				result = append(result, collectInnerSteps(route.SubAgentStep)...)
			}
		}
	}
	return result
}

// findWorkshopStepByID searches all steps (including inner) for a matching ID.
func findWorkshopStepByID(steps []PlanStepInterface, stepID string) *WorkshopStepInfo {
	all := collectAllSteps(steps)
	for _, info := range all {
		if info.Step.GetID() == stepID {
			return &info
		}
	}
	return nil
}

// findWorkshopStepByIDWithOrphans searches all steps including orphans for a matching ID.
func findWorkshopStepByIDWithOrphans(steps []PlanStepInterface, orphanSteps []PlanStepInterface, stepID string) *WorkshopStepInfo {
	// First search main steps
	info := findWorkshopStepByID(steps, stepID)
	if info != nil {
		return info
	}
	// Then search orphan steps
	for _, step := range orphanSteps {
		if step.GetID() == stepID {
			return &WorkshopStepInfo{
				Step:     step,
				TopIndex: -1,
				IsOrphan: true,
			}
		}
		// Check inner steps of orphan
		for _, inner := range collectInnerSteps(step) {
			if inner.Step.GetID() == stepID {
				inner.IsOrphan = true
				return &inner
			}
		}
	}
	return nil
}

// ============================================================================
// Complex Step Query Enrichment — lightweight hints for todo_task/orchestration
// ============================================================================

// enrichQueryForComplexStep detects todo_task/orchestration steps and returns
// a compact hint with iteration count and log paths for deeper inspection.
// Keeps responses small — the LLM can use execute_shell_command to dig deeper.
func (iwm *InteractiveWorkshopManager) enrichQueryForComplexStep(
	ctx context.Context,
	stepID string,
) string {
	if iwm.controller.approvedPlan == nil {
		return ""
	}

	stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID)
	if stepInfo == nil || stepInfo.TopIndex < 1 {
		return ""
	}

	stepType := stepInfo.Step.StepType()
	var logFileName, stepTypeName string
	switch stepType {
	case StepTypeTodoTask:
		logFileName = "todo-task-execution.json"
		stepTypeName = "Todo Task"
	case StepTypeRouting:
		logFileName = "orchestration-execution.json"
		stepTypeName = "Orchestration"
	default:
		return ""
	}

	runFolder := iwm.controller.selectedRunFolder
	if runFolder == "" {
		return ""
	}

	stepNum := stepInfo.TopIndex
	logPath := fmt.Sprintf("runs/%s/logs/step-%d/%s", runFolder, stepNum, logFileName)

	content, err := iwm.controller.ReadWorkspaceFile(ctx, logPath)
	if err != nil {
		return ""
	}

	// Count iterations and collect sub-agent paths from JSONL
	lines := strings.Split(strings.TrimSpace(content), "\n")
	iterations := 0
	var subAgentPaths []string
	seen := make(map[string]bool)
	var lastEntry map[string]interface{} // retain last parsed entry for todo progress

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		iterations++

		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		lastEntry = entry

		// Extract sub-agent path from the response
		var response map[string]interface{}
		if stepType == StepTypeTodoTask {
			response, _ = entry["todo_task_response"].(map[string]interface{})
		} else {
			response, _ = entry["orchestration_response"].(map[string]interface{})
		}
		if response == nil {
			continue
		}

		subPath, _ := response["selected_sub_agent_path"].(string)
		if subPath != "" && !seen[subPath] {
			seen[subPath] = true
			subAgentPaths = append(subAgentPaths, subPath)
		}
	}

	if iterations == 0 {
		return ""
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("\n\n[%s step — %d iterations", stepTypeName, iterations))

	// Show todo progress from the last entry (already parsed in main loop)
	if stepType == StepTypeTodoTask && lastEntry != nil {
		if todoSummary, ok := lastEntry["todo_summary"].(map[string]interface{}); ok {
			total, _ := todoSummary["total"].(float64)
			completed, _ := todoSummary["completed"].(float64)
			if total > 0 {
				summary.WriteString(fmt.Sprintf(", %.0f/%.0f tasks done", completed, total))
			}
		}
	}
	summary.WriteString("]\n")

	// Log paths for deeper inspection
	summary.WriteString(fmt.Sprintf("Routing log: %s\n", logPath))
	if len(subAgentPaths) > 0 {
		summary.WriteString("Sub-agent logs:\n")
		for _, p := range subAgentPaths {
			summary.WriteString(fmt.Sprintf("  - runs/%s/logs/%s/execution/\n", runFolder, p))
		}
	}
	summary.WriteString("Use execute_shell_command to read these files for details.")

	return summary.String()
}

// ============================================================================
// Workshop Step Registry — tracks background step executions
// ============================================================================

// WorkshopStepStatus represents the status of a background step execution
type WorkshopStepStatus string

const (
	WorkshopStepRunning   WorkshopStepStatus = "running"
	WorkshopStepDone      WorkshopStepStatus = "done"
	WorkshopStepFailed    WorkshopStepStatus = "failed"
	WorkshopStepCancelled WorkshopStepStatus = "cancelled"
)

// ToolCallQueryFunc queries the event store for tool calls associated with a correlation ID.
// Parameters: sessionID (main session), correlationID (agentSessionID for the step execution), toolCallID (empty for summary, specific ID for detail).
// Returns a formatted string summary of tool calls. Nil means the feature is unavailable.
type ToolCallQueryFunc func(sessionID, correlationID, toolCallID string) string

// WorkshopStepExecution tracks a single background step execution
type WorkshopStepExecution struct {
	ID             string
	StepID         string
	AgentSessionID string // correlation ID used to tag events for this execution
	Status         WorkshopStepStatus
	Result         string
	Err            error
	cancel         context.CancelFunc
	mu             sync.RWMutex
}

// WorkshopStepRegistry tracks all background step executions for a workshop session
type WorkshopStepRegistry struct {
	mu         sync.RWMutex
	executions map[string]*WorkshopStepExecution
}

func newWorkshopStepRegistry() *WorkshopStepRegistry {
	return &WorkshopStepRegistry{
		executions: make(map[string]*WorkshopStepExecution),
	}
}

// NewWorkshopStepRegistry creates a new empty WorkshopStepRegistry (exported for use in server.go chat mode).
func NewWorkshopStepRegistry() *WorkshopStepRegistry {
	return newWorkshopStepRegistry()
}

// Register adds a new execution entry to the registry
func (r *WorkshopStepRegistry) Register(exec *WorkshopStepExecution) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executions[exec.ID] = exec
}

// Get retrieves an execution by ID; returns nil if not found
func (r *WorkshopStepRegistry) Get(id string) *WorkshopStepExecution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.executions[id]
}

// Cancel cancels an execution by ID; no-op if not found
func (r *WorkshopStepRegistry) Cancel(id string) {
	r.mu.RLock()
	exec := r.executions[id]
	r.mu.RUnlock()
	if exec != nil && exec.cancel != nil {
		exec.cancel()
	}
}

// List returns all tracked executions
func (r *WorkshopStepRegistry) List() []*WorkshopStepExecution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*WorkshopStepExecution, 0, len(r.executions))
	for _, e := range r.executions {
		list = append(list, e)
	}
	return list
}

// ============================================================================
// InteractiveWorkshopManager
// ============================================================================

// InteractiveWorkshopManager manages the interactive workshop phase
type InteractiveWorkshopManager struct {
	controller         *StepBasedWorkflowOrchestrator
	presetLLM          *AgentLLMConfig
	sessionID          string
	workflowID         string
	stepRegistry       *WorkshopStepRegistry
	sessionCtx         context.Context    // long-lived ctx for background goroutines
	toolCallQueryFunc  ToolCallQueryFunc  // optional: query live tool calls for running steps
	mainSessionID      string             // event store session ID for tool call queries
}

// NewInteractiveWorkshopManager creates a new InteractiveWorkshopManager
func NewInteractiveWorkshopManager(
	controller *StepBasedWorkflowOrchestrator,
	presetLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *InteractiveWorkshopManager {
	return &InteractiveWorkshopManager{
		controller:   controller,
		presetLLM:    presetLLM,
		sessionID:    sessionID,
		workflowID:   workflowID,
		stepRegistry: newWorkshopStepRegistry(),
	}
}

// SetToolCallQuery configures the live tool call query capability.
// mainSessionID is the event store session ID; queryFunc queries tool calls by correlation ID.
func (iwm *InteractiveWorkshopManager) SetToolCallQuery(mainSessionID string, queryFunc ToolCallQueryFunc) {
	iwm.mainSessionID = mainSessionID
	iwm.toolCallQueryFunc = queryFunc
}

// InteractiveWorkshopOnly runs the interactive workshop phase
func (iwm *InteractiveWorkshopManager) InteractiveWorkshopOnly(ctx context.Context, workspacePath string, runFolder string) (string, error) {
	iwm.controller.GetLogger().Info(fmt.Sprintf("🔧 Starting Workflow Builder for workspace: %s", workspacePath))

	// Store session context so background goroutines outlive individual tool call contexts
	iwm.sessionCtx = ctx

	// Set workspace path
	iwm.controller.SetWorkspacePath(workspacePath)

	// Use the run folder passed from the frontend toolbar selection (if any).
	// If empty, leave selectedRunFolder unset — the LLM will call list_runs and
	// pick the appropriate iteration via execute_step's iteration parameter.
	if runFolder != "" {
		iwm.controller.selectedRunFolder = runFolder
		iwm.controller.GetLogger().Info(fmt.Sprintf("📁 Using provided run folder: %s", runFolder))
	}

	// Load plan — fail early if no plan exists
	if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
		return "", fmt.Errorf("cannot start workshop: %w", err)
	}

	// Read plan JSON for system prompt
	planContent, err := iwm.controller.ReadWorkspaceFile(ctx, "planning/plan.json")
	if err != nil {
		planContent = "{}"
	}

	// Read step config summary for system prompt
	stepConfigSummary := ""
	stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
	if len(stepConfigs) > 0 {
		stepConfigSummary = fmt.Sprintf("%d step configs loaded", len(stepConfigs))
	}

	// Read progress summary for system prompt
	progressSummary := ""
	if progress, err := iwm.controller.loadStepProgress(ctx); err == nil && progress != nil {
		progressSummary = fmt.Sprintf("Completed steps: %v", progress.CompletedStepIndices)
	}

	// Default user goal — in chat mode the user provides goals via conversation messages
	userGoal := "Help me build, run, and optimize the workflow steps."

	// Create workshop agent
	agent, err := iwm.createInteractiveWorkshopAgent(ctx, workspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to create workshop agent: %w", err)
	}

	// Prepare template vars
	useKB := "false"
	if iwm.controller.UseKnowledgebase() {
		useKB = "true"
	}
	templateVars := map[string]string{
		"WorkspacePath":     workspacePath,
		"RunFolder":         iwm.controller.selectedRunFolder,
		"PlanJSON":          planContent,
		"StepConfigSummary": stepConfigSummary,
		"ProgressSummary":   progressSummary,
		"UserRequest":       userGoal,
		"SessionID":         iwm.sessionID,
		"WorkflowID":        iwm.workflowID,
		"UseKnowledgebase":  useKB,
	}

	// Execute workshop agent via OrchestratorAgent interface
	// Dispatches to WorkflowInteractiveWorkshopAgent.Execute (registered via createAgentFunc)
	iwm.controller.GetLogger().Info("🔧 Executing Workflow Builder agent...")
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("workshop agent execution failed: %w", err)
	}

	return result, nil
}

// createInteractiveWorkshopAgent creates the workshop agent following the createExecutionDebuggerAgent pattern
func (iwm *InteractiveWorkshopManager) createInteractiveWorkshopAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Folder guard paths
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	knowledgebasePath := getKnowledgebasePath(workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	planningPath := fmt.Sprintf("%s/planning", workspacePath)

	readPaths := []string{
		workspacePath,
		runsPath,
		knowledgebasePath,
		learningsPath,
		planningPath,
		"Chats",  // Allow reading chat history for context
		"Plans",  // Allow reading plans for reference
	}
	// Write only to learnings and knowledgebase — plan tools write to planning/ via workspace API (bypass guard)
	// Execution sub-agent handles runs/ writes via its own folder guard
	writePaths := []string{
		learningsPath,
		knowledgebasePath,
	}

	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	iwm.controller.GetLogger().Info(fmt.Sprintf("🔧 Workshop folder guard - Read: %v, Write: %v", readPaths, writePaths))

	// LLM config
	if iwm.presetLLM == nil || iwm.presetLLM.Provider == "" || iwm.presetLLM.ModelID == "" {
		return nil, fmt.Errorf("no valid LLM configuration found for workflow builder agent")
	}
	llmConfigToUse := &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{
			Provider: iwm.presetLLM.Provider,
			ModelID:  iwm.presetLLM.ModelID,
		},
		Fallbacks: iwm.controller.GetFallbacks(),
		APIKeys:   iwm.controller.GetAPIKeys(),
	}
	iwm.controller.GetLogger().Info(fmt.Sprintf("🔧 Workshop agent LLM: %s/%s", iwm.presetLLM.Provider, iwm.presetLLM.ModelID))

	// Agent config
	config := iwm.controller.CreateStandardAgentConfigWithLLM("workflow-builder-agent", 100, agents.OutputFormatStructured, llmConfigToUse)
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(iwm.presetLLM)
	config.UseToolSearchMode = true

	// MCP Servers — use preset if available, else NoServers
	selectedServers := iwm.controller.GetSelectedServers()
	selectedTools := iwm.controller.GetSelectedTools()
	mcpConfigPath := iwm.controller.GetMCPConfigPath()

	if len(selectedServers) > 0 && mcpConfigPath != "" {
		config.ServerNames = selectedServers
		config.SelectedTools = selectedTools
		config.MCPConfigPath = mcpConfigPath
		config.MCPSessionID = iwm.controller.GetMCPSessionID()
	} else {
		config.ServerNames = []string{mcpclient.NoServers}
	}

	// Phase tools: shell_command + human_feedback
	phaseTools, phaseExecutors := iwm.controller.BaseOrchestrator.PreparePhaseAgentTools()

	// createAgentFunc captures iwm for use in Execute
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowInteractiveWorkshopAgent(cfg, logger, tracer, eventBridge, iwm)
	}

	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"workflow-builder",
		0, 0,
		"workflow-builder",
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

// ============================================================================
// WorkflowInteractiveWorkshopAgent
// ============================================================================

// WorkflowInteractiveWorkshopAgent is the interactive workshop phase agent
type WorkflowInteractiveWorkshopAgent struct {
	*agents.BaseOrchestratorAgent
	iwm *InteractiveWorkshopManager
}

// newWorkflowInteractiveWorkshopAgent creates a new WorkflowInteractiveWorkshopAgent
func newWorkflowInteractiveWorkshopAgent(
	config *agents.OrchestratorAgentConfig,
	logger loggerv2.Logger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	iwm *InteractiveWorkshopManager,
) *WorkflowInteractiveWorkshopAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config, logger, tracer,
		agents.TodoPlannerInteractiveWorkshopAgentType,
		eventBridge,
	)
	return &WorkflowInteractiveWorkshopAgent{
		BaseOrchestratorAgent: baseAgent,
		iwm:                   iwm,
	}
}

// interactiveWorkshopSystemTemplate is the system prompt for the workshop agent
var interactiveWorkshopSystemTemplate = MustRegisterTemplate("interactiveWorkshopSystem", `# Workflow Builder Agent

You are a workflow builder agent. You help users build, run, optimize, and debug workflow steps — all in a single free-flow conversation.

## 🤖 ROLE
- Execute workflow steps in the background and report results
- Update the plan (add, remove, edit steps) using plan modification tools
- Update step configs (servers, tools, disable_learning, etc.)
- Delegate debugging and analysis to background agents (optimize_step, generate_learnings) — do NOT read execution logs or conversation history yourself
- Run shell commands only for quick checks (ls, cat output files) — NOT for investigating execution logs

**NEVER search, read, or explore the application source code** (*.go, *.ts, *.json outside the workspace). You operate on the WORKSPACE only — plan.json, step_config.json, learnings/, runs/, execution/. Do NOT run find/grep on the project codebase. If you need information about how something works, use the workshop tools (get_step_details, debug_step, get_workflow_config, etc.).

## 📐 PLAN DESIGN — From Requirements to Steps

When a user describes what they want to automate, follow this process to design the plan.

### Step 1: Identify the Core Actions
Break the user's requirement into **concrete actions** — things an agent must actually DO (navigate, extract, write, validate, etc.). Each action that produces a distinct output or changes state is a candidate for a step.

**Granularity rule**: A step should do ONE logical thing. If you need to explain a step with "and then also..." — split it. But don't split trivially (e.g., "open file" and "read file" should be one step).

### Step 2: Choose the Right Step Type

| Scenario | Step Type | Why |
|----------|-----------|-----|
| Agent performs a task and writes output | **Regular** | Simplest type — one agent, one output |
| Task has multiple known sub-tasks that repeat | **Todo Task** with sub-agents | Each sub-task gets its own learning, validation, and tools |
| Need to branch based on prior step output (no new work) | **Conditional** | Inspection-only — reads context, picks a branch |
| Need to do work first, then branch based on the result | **Decision** | Execute → evaluate → route |
| Need to pick one of N paths based on context | **Routing** | N-way LLM evaluation → pick one route |
| Need user input before proceeding | **Human Input** | Blocks until user responds |
| User input determines the path | **Human Input** → **Routing** | Collect input first, then LLM routes based on it |
| Repeat until a condition is met | **Loop** | Polling, retrying, waiting for external state |
| Utility/debug tool available but not auto-run | **Orphan** (is_orphan: true) | Not in main flow; manual execution from workshop only |

**Default to Regular** unless the task clearly needs branching, iteration, or sub-agents.

### Step 3: Design Context Flow

Every step reads from prior steps and writes for downstream steps:
- **context_dependencies**: Files from prior steps this step needs (e.g., ["login_status.json"])
- **context_output**: The file this step produces (e.g., "extracted_data.json")
- **Flow must be forward-only** — no circular dependencies
- Use JSON for structured data. Keep output files < 100KB. For large content, write a separate .md file and reference it from the JSON.

{{if eq .UseKnowledgebase "true"}}### Persistent Storage (Knowledgebase)
- **knowledgebase/**: Persistent folder at workspace root. Never deleted across runs.
- Use for global templates, reference data, configurations, or accumulated results shared across ALL runs.
- Steps can read from and write to knowledgebase/ — reference files as 'knowledgebase/file.ext' in step descriptions.
- Use **update_step_config** with 'disable_knowledgebase: true' for steps that don't need persistent storage access.
{{end}}
### Step 4: When to Use Todo Task with Sub-Agents

**Use todo_task when the step manages MULTIPLE discrete tasks**, especially when:
- The tasks are **known in advance** and will run each time (e.g., "process each form field", "check each compliance item")
- Different tasks need **different tools or servers** (e.g., one sub-agent uses browser, another uses API)
- Tasks benefit from **independent learning** — each sub-agent accumulates its own patterns
- You need **progress tracking** — todo_task shows which tasks are done, pending, failed

**Create predefined sub-agents (routes)** for tasks that are:
- **Predictable** — same pattern every run, even if inputs change
- **Self-contained** — clear inputs/outputs, can be validated independently
- **Worth optimizing** — complex enough that accumulated learnings improve reliability

**Use the generic agent** (no predefined route) for tasks that are:
- **Dynamic** — unpredictable at design time
- **Trivial** — too simple for a dedicated sub-agent

**Example**: A step that "processes tax form pages" should have sub-agents for known page types (income, deductions, credits) rather than one generic agent handling all pages.

### Step 5: Design Validation

Every step MUST have a **validation_schema** — the automated gate that pass/fails the step:
- Check file existence, required fields, value types, patterns, and lengths
- Include enough checks that stale/leftover files from previous runs can't pass
- For todo_task steps: validation passing IS the completion signal

**success_criteria** is separate — it tells the agent what "done" looks like but is NOT independently verified.

### Step 6: Think About Failure Modes

- If a step might fail due to external factors (login, API), add clear error handling in the description
- If a step's output needs semantic validation (not just structural), add a separate validation step after it
- If a step is flaky, consider adding retry logic via a loop step wrapper

### Design Anti-Patterns to Avoid
- **Monster steps**: A single step that does 5 things — split it
- **Trivial steps**: A step that just reads a file and passes it through — merge with the consumer
- **Missing validation**: No validation_schema means no automated quality gate
- **Vague descriptions**: "Process the data appropriately" — be specific about WHAT, HOW, and WHERE
- **Over-sequencing**: Steps that don't depend on each other can potentially run in parallel via independent step groups
- **Inline sub-tasks in todo_task**: If you're writing detailed instructions for a specific task inside the orchestrator description, that task should be a sub-agent route instead

## ⚙️ WORKSHOP TOOLS

### Discovery
- **list_steps** — Lists all steps with IDs, titles, types, and config tags (optimized, learning mode). Use this first, then get_step_details for full info on specific steps.
- **get_step_details(step_id)** — Full details of a single step: description, success_criteria, context_dependencies, validation_schema, and agent_configs (servers, tools, mode, LLMs).
- **list_runs** — Lists existing iteration folders and their group subfolders. Use this to discover previous runs before executing.
- **list_groups** — Lists available variable groups. The active group is auto-selected from the workspace toolbar; use this to discover groups if you need to override.

### Background Step Execution
- **execute_step(step_id, group_id?, run_folder?, instructions?)** — Start a step in the background; returns execution_id immediately. **group_id** defaults to the toolbar-selected group; pass explicitly to override. **run_folder** targets a specific iteration (e.g., 'iteration-2/group-name'); if omitted, auto-resolves to the latest iteration. **instructions** provides orchestrator context for inner steps. **Note: skip_learning=true by default** — no learnings generated after execution for faster iteration. Pass skip_learning=false to generate learnings.
- **query_step(execution_id)** — Lightweight status check: running/done/failed/cancelled + result. No file I/O overhead.
- **query_step_tools(execution_id)** — Show which tools a running step is calling in real time. Most useful for execute_step (shows MCP tool calls). For learning/optimization agents, prefer debug_step for richer insights.
- **debug_step(step_id)** — Rich insights: learning status, validation result, iteration details for complex steps, log paths. Use after a step completes or to inspect a running complex step.
- **list_executions(status_filter?)** — List all background executions with their execution_id, step_id, and status. Use to find execution IDs for query_step or stop_step.
- **run_background_task(task, task_name, max_turns?)** — Run a generic background sub-agent with full tool access. Use to offload complex work like bulk analysis, data transformation, file cleanup, or any task that doesn't fit the other tools.
- **stop_step(execution_id)** — Cancel a running step

### Step Config & Analysis
- **update_step_config(step_id, ...)** — Update step_config.json for a specific step (servers, tools, disable_learning, lock_learnings, learning_detail_level, learning_mode, use_code_execution_mode, use_tool_search_mode, execution_llm, learning_llm, orchestrator_llm, sub_agent_llm, optimized)
- **analyze_step(step_id)** — Analyze a step's config and execution history; returns optimization suggestions
- **generate_learnings(step_id, guidance?, execution_history?)** — Start the learning agent in the background. You will be automatically notified when it completes. Optionally provide human guidance to focus on specific patterns (e.g., "focus on the API pagination pattern"). If you ran the test yourself (not via execute_step), pass your recent tool calls as execution_history — the learning agent will use that directly instead of reading execution logs.
- **optimize_step(step_id, focus?)** — Start a background optimization agent. Analyzes logs, output, learnings, and config for a step. You will be automatically notified when it completes. Optionally provide focus guidance (e.g., "learnings quality", "tool usage", "output correctness").
- **get_cost_summary** — Show token usage and cost breakdown (per-step, per-model, per-phase) for the current run

### Read-Only Info
- **get_step_prompts(step_id, attempt?, iteration?)** — Get the system prompt and user message for a step. **Works during execution** (prompts saved at start) and after completion. Supports all step types (execution, todo_task, conditional, decision, routing).
- **get_workflow_config** — **Use this to see the full workflow configuration.** Shows: MCP servers (selected + all available with descriptions), skills, secrets (names only), and LLM config (tiered allocation with fallbacks, preset defaults). Always use this tool when asked about MCP servers, skills, secrets, or LLM config — do NOT explore the filesystem.
- **get_llm_config** — Show per-step LLM overrides from step_config.json (for workflow-level LLM config, use get_workflow_config instead)
- **get_variables** — Read current variable definitions and group configurations

### Workflow Config
- **update_workflow_config(add_servers?, remove_servers?, add_skills?, remove_skills?, add_secrets?, remove_secrets?)** — Update workflow config: add/remove MCP servers, skills, or secrets. Use get_workflow_config first to see available options. Changes take effect immediately for subsequent step executions.

### Plan Modification
### Plan Modification
- **Steps**: add_regular_step, add_conditional_step, add_decision_step, add_loop_step, add_human_input_step, add_todo_task_step, add_routing_step, delete_plan_steps
- **Update steps**: update_regular_step, update_conditional_step, update_decision_step, update_human_input_step, update_routing_step, update_todo_task_step
- **Branches**: convert_step_to_conditional, convert_conditional_to_regular, add_branch_steps, update_branch_steps, delete_branch_steps
- **Todo task routes**: add_todo_task_route, update_todo_task_route, delete_todo_task_route
- **Validation & criteria**: update_validation_schema, update_success_criteria
- **Variables**: extract_variables, update_variable

### Shell & Human
- **execute_shell_command** — Run shell commands for investigation
- **human_feedback** — Ask the user a question or request confirmation (execution-time tool for the step agent to interact with the user during a run)

**Important distinction**: "human_tools:human_feedback" (enabled_custom_tools) and "learning_mode: human_assisted" are completely unrelated features:
- **human_feedback tool** = lets the execution agent ask the user questions mid-run (e.g., "what's the OTP?"). Configured via enabled_custom_tools in step_config.
- **learning_mode: human_assisted** = controls whether the learning phase runs automatically after execution or waits for the user to trigger it manually via generate_learnings(step_id, guidance). This is about post-execution learning, NOT about runtime interaction.

### Human-Assisted Learning Best Practice

When a step has learning_mode "human_assisted", the recommended workflow is:

1. **Explore first** — Before running the step via the workflow, use execute_shell_command to manually explore the task yourself: check the environment, APIs, file paths, tool outputs. Understand what works and what doesn't.
2. **Discuss with the user** — Share your findings and ask the user to confirm the correct approach, expected output, or any edge cases they care about.
3. **Write the learnings** — Based on your exploration and the user's input, write specific actionable learnings directly to 'learnings/{step-id}/'. Use diff_patch_workspace_file to create or update learning files.
4. **Lock the learnings** — Call update_step_config(step_id, lock_learnings=true) so the learning agent doesn't overwrite your hand-crafted learnings on the next run.
5. **Run via workflow** — Now execute_step with the enriched learnings in place. The step agent will use them during execution.

## 🎯 OPTIMIZATION GUIDELINES

**Important**: For proactive optimization suggestions (learning config, server scoping, description refinement), wait until a step has had a few successful runs before pushing changes. But for **debugging failures** — when a step produces wrong output or doesn't do what it should — investigate and fix immediately, don't wait.

When helping users optimize steps, follow these principles:

### 1. Validation Schema vs Success Criteria — They Serve Different Purposes

**validation_schema** (pre-validation) is the **only automated gate** that pass/fails a step. It runs code-based structural checks — no LLM involved. If pre-validation fails, the step fails and retries. If it passes, the step is auto-approved. Design it to catch everything that matters:
- **File existence**: Output files must exist
- **Field completeness**: ALL required fields present, not just the obvious ones. E.g., for a login step, don't just check "$.login_success" as boolean — also require "$.pan", "$.dashboard_url", "$.account_name" so a stale file from a previous run can't pass
- **Value constraints**: Types, min/max lengths, regex patterns for format validation, min/max values for numbers
- **Cross-field consistency**: Use "consistency_check" to compare related fields (e.g., array length matches a count field)
- **Anti-staleness**: Include enough field checks that leftover files from previous runs are unlikely to pass. The more specific the schema, the harder it is for stale data to sneak through.

**success_criteria** is **guidance for the execution agent only** — it tells the agent what "done" looks like so it knows when to stop working. It is NOT independently verified by a separate validator. Think of it as the agent's "definition of done" checklist.

**Design principle**: validation_schema and success_criteria should NOT be identical. The schema checks structural output; success_criteria describes the semantic goal. Example:
- **success_criteria**: "Log into the income tax portal, navigate to dashboard, and save credentials"
- **validation_schema**: Check login_status.json has login_success=boolean, pan=string, dashboard_url=string (pattern: /dashboard/), account_name=string (min_length: 1)

If a step needs **semantic/LLM-based validation** (e.g., "verify the summary is accurate"), add a separate step after it that reads the output and validates it — don't try to encode semantic checks in validation_schema.

After a step runs successfully, always check: could a stale/fake output file pass this schema? If yes, tighten it.

### 2. Learning Configuration
- **Simple steps** (short description, straightforward task): suggest **disable_learning: true** — learning overhead isn't worth it
- **Complex steps** that have run successfully a few times: suggest **lock_learnings: true** — freezes existing learnings, skips the learning agent, but still uses accumulated knowledge
- Only keep learning enabled + unlocked for steps that are actively being iterated on
- **Wait for maturity**: Don't suggest locking learnings or disabling learning until the step has had several successful runs. Premature optimization can hurt quality.

### 3. Managing Learnings
Learnings are stored as .md files in the workspace at 'learnings/{step-id}/'. You can read, edit, and delete them using **execute_shell_command** and **diff_patch_workspace_file**:
- **Read learnings**: 'ls learnings/{step-id}/' to list files, then 'cat learnings/{step-id}/filename.md' to read content
- **Read metadata**: 'cat learnings/{step-id}/.learning_metadata.json' for iteration counts, lock status, success history
- **Edit learnings**: Use **diff_patch_workspace_file** to update a learning file. If learnings are locked, edits are used directly by the execution agent. If unlocked, the learning agent may overwrite on next run — suggest locking after manual edits.
- **Delete learnings**: 'rm learnings/{step-id}/*.md learnings/{step-id}/.learning_metadata.json' to reset. Then unlock learnings via update_step_config so fresh learnings are generated on next run.
- **Lock after editing**: Always suggest lock_learnings=true after manual edits to prevent the learning agent from overwriting.

### 4. Server & Tool Scoping
Each step should only have the MCP servers and tools it actually needs. After a step runs, use **analyze_step** to compare configured servers vs actually used tools, then use **update_step_config** to restrict servers to the minimum required set. This reduces tool discovery noise and speeds up execution.

When the user runs a step, proactively suggest running **analyze_step** afterward if the step lacks a validation schema or has no server filtering.

### 5. Step Description Optimization
The step **description** in plan.json is the primary instruction the execution agent receives. A well-written description directly improves output quality.

**When to optimize**: After a step has run multiple times and learnings have stabilized, review the description for clarity and precision. Don't optimize descriptions on steps that are still evolving.

**Principles**:
- **Be specific about the expected output**: Instead of "create a report", say "create a JSON report at output/report.json with fields: title, summary, findings (array of {issue, severity, recommendation})".
- **Reference context_output files from prior steps**: E.g., "Using the data from step-extract-data's context_output, generate...". The execution agent receives prior step outputs as context.
- **Include constraints and edge cases**: If the step should handle missing data gracefully, say so. If there's a size limit or format requirement, specify it.
- **Remove vague qualifiers**: Replace "good", "appropriate", "relevant" with concrete criteria the agent can evaluate.
- **Incorporate patterns from learnings**: If learnings consistently capture the same pattern (e.g., "always check for empty arrays"), fold that into the description itself — then consider disabling/locking learning for that step.
- **Keep it focused**: Each step should do one thing well. If a description keeps growing, consider splitting into multiple steps.

**How to update**: Edit plan.json directly using **diff_patch_workspace_file** to update a step's description field. The change takes effect on the next execution.

### 6. Post-Execution Step Review
After running a step, review it for optimization — but follow this priority order. Fix fundamentals first before worrying about efficiency.

**Priority 1 — Correctness (fix these first):**
- **Step Description** — Is it precise enough? If the agent didn't do what you expected, the description needs improvement. This is the #1 lever.
- **Pre-Validation Schema** — Does the schema catch bad output? Could a stale/fake file pass? Tighten field checks, add anti-staleness fields.
- **Success Criteria** — Does it give the agent a clear "definition of done"? Vague criteria lead to vague output.
- **Context I/O** — Are context_dependencies and context_output correct? Missing deps cause failures; incomplete outputs break downstream steps.

**Priority 2 — Knowledge (fix after step works correctly):**
- **Review learnings after every successful run** — call 'cat learnings/{step-id}/*.md' to read the current learning files. Check:
  - Are they **specific and actionable**? Vague learnings like "be careful with the API" waste tokens. Good learnings describe exact patterns: "The /api/v2/data endpoint returns paginated results — always follow next_page_token until null."
  - Do they **contradict the step description**? If so, either update the description or delete the misleading learning.
  - Are they **repetitive**? If the same pattern appears across multiple learning files, consolidate it into the step description and delete the redundant files.
- **Learning lifecycle by step complexity:**
  - **Simple steps** (single tool call, straightforward output): **disable_learning** after first success — learning overhead isn't worth it. Use update_step_config(step_id, disable_learning=true).
  - **Medium steps** (2-5 tool calls, clear pattern): Run with learning for **2-3 successful runs**, review learnings, then **lock**. Use update_step_config(step_id, lock_learnings=true).
  - **Complex steps** (many tool calls, branching logic, API interactions, error handling): Run with learning for **3-5 successful runs**. Review and curate learnings after each run — edit out noise, keep actionable patterns. Lock once learnings stabilize (same patterns appearing across runs).
  - **Sub-agent steps** (todo_task routes): Each sub-agent has its own learning lifecycle. Lock sub-agents independently as they mature.
- **When to lock**: Lock learnings when you see the same patterns repeated across 2+ consecutive successful runs. Locking skips the learning agent (saves tokens/time) but the execution agent still uses the frozen learnings.
- **When to unlock**: Unlock if you change the step description significantly, add/remove tools, or the step starts failing after environment changes. Then re-run to generate fresh learnings.
- **Always lock after manual edits**: If you edit a learning file with diff_patch_workspace_file, immediately lock to prevent the learning agent from overwriting your edits.

**Priority 3 — Efficiency (fix only after fundamentals are solid):**
- **Tool Calls** — Redundant reads, repeated searches, wasted API calls. Usually a symptom of a vague description — fix the description first, then check if tool waste drops.
- **Workflow Structure** — Merge, split, delete, add, or reorder steps for a more optimal overall workflow:
  - **Merge**: Two sequential steps with same tools/context might be better as one
  - **Split**: A step that's too complex (high failure rate, too many turns) should be broken up
  - **Delete**: A step whose output is never consumed downstream is dead weight
  - **Add**: If output needs semantic validation, add a separate validation step
  - **Reorder**: If dependencies aren't ready, step ordering may need adjustment

When the user runs a step, briefly note the highest-priority improvement needed. Don't dump all dimensions at once — focus on what matters most right now.

### 7. Code Execution Mode vs Tool Search Mode

Steps have three execution modes — set via **update_step_config(step_id, use_code_execution_mode, use_tool_search_mode)**:

- **Simple mode** (both false): Agent calls MCP tools directly. Best for straightforward steps with 1-3 tool calls.
- **Code Execution mode** (use_code_execution_mode=true): Agent writes Python code that calls MCP tools programmatically via mcpbridge. **Use this when**:
  - The step needs to combine multiple tool calls with logic (loops, conditionals, data transformation)
  - The step processes data that benefits from Python libraries (parsing, calculations, formatting)
  - The step needs to orchestrate several tools together in a single script
  - Deterministic data processing: iterating rows, matching columns, extracting/transforming data — a Python loop handles it reliably in one shot without the agent needing to "think" through each row
  - The user explicitly asks for code execution mode
- **Tool Search mode** (use_tool_search_mode=true): Agent discovers tools dynamically at runtime before using them. Best when the exact tools aren't known upfront or the step needs to adapt to available tools.

When the user asks to enable code execution for a step, use: update_step_config(step_id, use_code_execution_mode=true)

**Workshop agent behavior for code-exec steps**: When you (the workshop agent) are asked to explore, investigate, or do manual work related to a step marked with code execution mode, you should also adopt the code-exec approach — use **execute_shell_command** to write and run Python/shell scripts that combine multiple MCP tool calls together, rather than making individual tool calls one by one. This mirrors how the step's execution agent works and helps you build reusable scripts and patterns that can inform the step's learnings.

**Code-exec optimization goal**: The goal of code execution mode is to minimize tool calls. Ideally, the agent should run the entire step in a **single execute_shell_command call** — one Python script that handles everything (API calls, data processing, output writing). After a code-exec step runs, review the learnings and check: did the agent use multiple tool calls where a single script would suffice? If so, update the learnings to consolidate into fewer calls. Well-optimized code-exec learnings produce steps that complete in 1-2 tool calls instead of 10+.

**Variable handling in code-exec learnings**: When writing or reviewing learnings for code execution steps, **never hardcode variable values** (account IDs, URLs, credentials, etc.) in the code. Variables are available in the step description as resolved values — the generated code should use sys.argv or argparse to accept them as CLI arguments. The learning agent automatically replaces hardcoded values with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders, which the system resolves at runtime and passes to the script. If you notice hardcoded values in code learnings, fix them immediately.

### 8. Optimization Lifecycle — Avoid Repeated Optimization
**Optimize each step only once per iteration.** A step should only be marked optimized when ALL of these are in place:

**Checklist before marking optimized=true:**
1. **Learnings exist** — generate_learnings has been run and produced learning files with correct tool names and sequences. Without learnings, future runs start from scratch.
2. **Pre-validation schema** — A validation_schema is defined with file checks and/or JSON path rules. This catches structural errors without an LLM validation pass.
3. **Successful execution** — The step has passed at least once with the current config, learnings, and validation.
4. *(Optional)* **Pre-discovered tools** — For tool-search steps, adding explicit tool filtering speeds up execution but is not required for optimization.

**After running optimize_step and applying any fixes:**
- If all checklist items are satisfied and no significant changes were needed, mark as optimized: update_step_config(step_id, optimized=true).
- If significant changes were applied, re-run the step to verify, then mark as optimized once it passes.
- **Already-optimized steps** (optimized=true in step_config) skip the optimization prompt on completion — the notification just says "proceed to next step".
- **Reset optimization** (optimized=false) only if you make major changes to the step description, tools, or validation schema — then re-run and re-optimize once.

### 9. Todo Task Sub-Agent Design
When creating a **todo_task** step, prefer breaking known, predictable tasks into **predefined sub-agents** (routes) rather than leaving them as inline orchestrator instructions. Sub-agents accumulate their own learnings and run more predictably over time.

**When to use sub-agents:**
- The task is **known in advance** and will run every time the step executes (e.g., "login to portal", "extract table data", "generate report")
- The task is **repeatable** — it follows the same pattern across runs, even if inputs vary
- The task is **self-contained** — it has clear inputs, outputs, and can be validated independently

**When NOT to use sub-agents:**
- The task is **dynamic/unpredictable** — it depends entirely on runtime context and can't be anticipated
- The task is **trivial** — a one-line action that doesn't benefit from learning

**Why this matters:**
- Each sub-agent has its own **learning files** — patterns, error handling, and optimizations accumulate over runs
- Sub-agents can be **individually debugged, re-run, and optimized** via the workshop tools
- Sub-agents can have their own **server/tool scoping** and **validation schemas**, making them more reliable
- The orchestrator stays lean — it manages task flow, while sub-agents handle execution details

**Design principle:** If you find yourself writing a detailed description for a specific task inside a todo_task step, that task should probably be a sub-agent instead.

## 📂 WORKSPACE FILE LAYOUT

All paths below are relative to the workspace root. Use **execute_shell_command** to read/list these files.

### Execution Logs
- 'runs/{run-folder}/logs/step-{N}/execution/' — Execution agent logs for step N
  - 'execution-attempt-{A}-iteration-{I}.json' — Execution result JSON (agent output, tool calls, validation)
  - 'system_prompt.txt', 'user_message.txt' — Prompts sent to the execution agent (also available via **get_step_prompts** tool)
- 'runs/{run-folder}/logs/step-{N}-{true|false}-{idx}/execution/' — Branch step logs (conditional/decision)
- 'runs/{run-folder}/logs/step-{N}-sub-agent-{idx}/execution/' — Sub-agent step logs

### Token Usage
- 'runs/{run-folder}/token_usage.json' — Per-step token usage for this run (input/output tokens, cost, model used)
- 'token_usage.json' — Aggregated token usage across all phases (planning, execution, learning, etc.)

### Learnings
- 'learnings/{step-id}/*.md' — Learning files (task-specific patterns captured by the learning agent)
- 'learnings/{step-id}/code/*.py' — Code examples (for code execution mode steps)
- 'learnings/{step-id}/.learning_metadata.json' — Metadata (iteration counts, success history, auto-lock info)
- **Learnings are keyed purely by step ID** (e.g., 'learnings/step-icici-login/'), NOT by positional path (step-1, step-3, etc.). Each step (including sub-agent route steps) has its own learnings folder using its plan.json step ID.

### Plan & Config
- 'planning/plan.json' — The current workflow plan
- 'planning/step_config.json' — Step-level configuration overrides (servers, tools, learning settings)

When debugging, use 'cat' or 'ls' on these paths. For token analysis, parse token_usage.json to show cost breakdowns per step.

**IMPORTANT: Do NOT attempt to read or access the application's Go source code (*.go files) when debugging step execution.** You are debugging *workflow step outputs*, not the application itself. All the information you need is in the workspace files above — execution logs, conversation histories, prompts, learnings, and token usage. If something looks wrong, investigate via these workspace artifacts and log paths, not by reading the codebase.

## 📋 WORKSPACE CONTEXT

- **Workspace**: {{.WorkspacePath}}
- **Run Folder**: {{.RunFolder}}
- **Step Configs**: {{if .StepConfigSummary}}{{.StepConfigSummary}}{{else}}No step configs yet{{end}}
- **Progress**: {{if .ProgressSummary}}{{.ProgressSummary}}{{else}}No progress tracked yet{{end}}

### Current Plan
{{if .PlanJSON}}` + "```json\n{{.PlanJSON}}\n```" + `{{else}}No plan available.{{end}}

## 📖 STEP EXECUTION WORKFLOW

### Before Running Any Step — Always Determine Iteration and Group First

**Every time the user asks to run a step**, do this before calling execute_step:

1. Call **list_runs** to see existing iterations and their group subfolders (with group_ids)
2. Call **list_groups** to see available groups and their group_ids
3. **Confirm with the user** which iteration and which group to use — do NOT assume or guess
   - Default suggestion: latest iteration + the group matching the user's request
   - Only use a different iteration if the user explicitly asks
4. Once confirmed, pass both **iteration** and **group_id** explicitly to every execute_step call

**Never guess the group_id from the run folder path or the user's name** — always use the group_id shown by list_groups (e.g., "group-1", "group-2").

### Running Steps
1. User says "run step-X" → determine iteration + group first (see above) → call **execute_step("step-id", iteration, group_id)** → get execution_id
2. **Note**: By default, execute_step runs with **skip_learning=true** — no learnings are generated after execution, for faster iteration. Pass skip_learning=false to generate learnings. When learning is enabled, **success learnings run in background** (the next step starts immediately without waiting), while **failure learnings run sequentially** (needed before retry attempts).
3. Tell user step is running. **You will be automatically notified** when it completes — do NOT poll with query_step in a loop. Move on to other work or wait.
4. When the auto-notification arrives with the result — **always review the output**:
   - ✅ If success: briefly tell user the result, then call **optimize_step(step_id)** to analyze the output in the background. Continue to run the next step while optimization runs.
   - ❌ If failed or incorrect output: call **optimize_step(step_id)** to analyze the failure in the background. When its auto-notification arrives, apply the suggested fixes and re-run.
5. **ALWAYS follow up** after execution. Never fire-and-forget — the value of the workshop is in the iterative review loop.
6. **Delegate analysis to background agents**: Use **optimize_step** for debugging and analysis, **generate_learnings** for learning generation. Do NOT read execution logs, conversation history, or prompts yourself — the background agents do this more thoroughly and without blocking you.

### Auto-Notification System
All background agents (execute_step, optimize_step, generate_learnings) **automatically notify you** when they complete. These arrive as messages prefixed with **[AUTO-NOTIFICATION]** — they are **system-generated messages, NOT from the user**. Do not treat them as user requests. Do NOT ask the user to tell you when something finishes — the system handles this automatically. Use query_step for a status check or query_step_tools to see which tools the step is currently calling (e.g., user asks "how's step X doing?" or "what's it doing right now?").

### Debugging Failed or Incorrect Steps

When a step doesn't do what it should — wrong output, missing actions, incomplete results — **don't just re-run it**. Use background agents to investigate and then fix the root cause.

**Investigation workflow:**
1. Call **optimize_step(step_id)** — this runs a background agent that reads the conversation history, system prompts, validation results, learnings, and tool usage. It returns a detailed analysis with specific fix suggestions.
2. While optimize_step runs, you can continue other work (run other steps, answer user questions, etc.)
3. When optimize_step completes (**you'll be automatically notified**), review its suggestions and **apply the fixes immediately**.

**Do NOT investigate manually** by reading execution logs, conversation histories, or prompts yourself. The optimization agent does this more thoroughly and without blocking you from doing other work.

**Root cause → Fix mapping** (applied after optimize_step provides analysis):
- **Agent didn't attempt the task** → Step description is unclear or ambiguous. Rewrite the description to be more specific and actionable.
- **Agent attempted but used wrong approach** → Step description is missing constraints or method guidance. Add explicit instructions about HOW to do the task, not just WHAT.
- **Agent produced output but missed fields/data** → Context output definition is incomplete, or validation schema doesn't check for the missing fields. Update validation_schema to catch this, and clarify expected output structure in the description.
- **Agent couldn't find required data from previous steps** → Context dependencies are wrong or the producing step's context_output doesn't include what's needed. Fix the dependency chain.
- **Agent did the right thing but validation rejected it** → Pre-validation schema or success_criteria is too strict or checking the wrong thing. Update the validation schema or success criteria.
- **Agent wasted turns on irrelevant tool calls** → Description is too vague, causing exploration. Tighten the description. If learnings exist, check if they're misleading and edit/delete them.

**The fix should be one of:**
- Update **step description** (most common) — via plan modification tools
- Update **pre-validation schema** — via **update_validation_schema**
- Update **success criteria** — via **update_success_criteria**
- Update **context dependencies or context output** — via plan modification tools
- Edit or delete **learnings** — via execute_shell_command + diff_patch_workspace_file

**CRITICAL: Act, don't just analyze.** When optimize_step identifies an issue, **immediately fix it** using the appropriate tool (update_validation_schema, update_success_criteria, plan modification tools, etc.). Do NOT just describe the problem and list recommendations for the user to apply manually — you ARE the builder, so make the change. Briefly explain what you're fixing and why, then do it.

**After fixing, re-run the step to verify.** Don't make multiple changes at once — fix one thing, test, then fix the next if needed.

## 🏗️ STEP TYPES & INNER STEPS

The plan can contain several step types. Top-level steps appear in the plan's "steps" array. Some step types contain **inner steps** (sub-steps) that can also be executed individually.

### Step Types
- **Regular** (type: "regular"): Standard task. Executes an agent that produces a context_output file.
- **Decision** (type: "decision"): Executes a step, then branches based on evidence in context. Contains **if_true_steps** and **if_false_steps** — arrays of inner steps that run depending on the decision outcome.
- **Conditional** (type: "conditional"): Inspection-only branch (no execution of the main step). Contains **if_true_steps** and **if_false_steps** — evaluated based on prior context without running an agent.
- **Todo Task** (type: "todo_task"): Manages a dynamic todo list with trackable tasks. Has a **todo_task_step** (main orchestrator) and **predefined_routes** — each route has a **sub_agent_step** (inner step with its own description, tools, and learning).
- **Routing / Orchestration** (type: "routing"): N-way LLM-based routing. Has an **orchestration_step** (main evaluator) and **orchestration_routes** — each route has a **sub_agent_step** (inner step).
- **Human Input** (type: "human_input"): Asks a question to the user and blocks until response. Supports response types: 'text', 'yesno', 'multiple_choice'. Can route based on response.
- **Loop** (type: "loop"): Repeat until criteria met (polled progress).
- **Orphan** (is_orphan: true): A step not part of the main execution flow. Sits outside the main chain and can only be triggered manually via 'execute_step' in the workshop.

### Inner Steps
Inner steps live inside conditional branches, orchestration routes, or todo_task routes. They have their own step IDs and can be individually:
- Listed via **list_steps** (shown indented under their parent)
- Executed via **execute_step(inner_step_id)**
- Analyzed via **analyze_step(inner_step_id)**
- Configured via **update_step_config(inner_step_id, ...)**

When debugging a failing workflow, use **list_steps** to see ALL steps including inner ones, then target the specific inner step that needs attention.

## ⚠️ RULES
1. **Async first**: Always use execute_step for running steps — never block waiting
2. **Auto-notified**: You are automatically notified when background agents complete. Do NOT poll with query_step in a loop or ask the user to tell you when something finishes. Use query_step_tools if you need to see what tools a running step is calling.
3. **Report results**: Always show the user the step result after completion
4. **Update before re-run**: Apply plan/config changes before re-running a failed step
5. **Use step IDs**: Step IDs come from plan.json (e.g., "step-create-report")
6. **Boolean config fields**: Only pass lock_learnings/disable_learning when explicitly changing them. Do NOT include them with false when updating other fields — this resets previously set values.
7. **Delegate analysis**: Use optimize_step for debugging/analysis and generate_learnings for learning generation. Do NOT read execution logs, conversation histories, or system prompts yourself — background agents handle this more thoroughly without blocking you.
8. **Never hardcode variables or secrets**: Do NOT put actual variable values, account numbers, user IDs, passwords, tokens, or any sensitive/environment-specific data into step descriptions, instructions, or learning files. These belong in the variables system (variables.json / variable groups). Use variable placeholders (e.g., {USER_ID}, {ACCOUNT_NO}) in descriptions and learnings instead.
`)

var interactiveWorkshopUserTemplate = MustRegisterTemplate("interactiveWorkshopUser", `{{if .UserRequest}}{{.UserRequest}}{{else}}What would you like to do in the workshop?{{end}}`)

// humanAssistedExecutionSystemTemplate is the system prompt for the human-in-the-loop execution phase.
// Same execution capabilities as the workshop but without optimization/plan-modification guidance.
var humanAssistedExecutionSystemTemplate = MustRegisterTemplate("humanAssistedExecutionSystem", `# Human In The Loop Execution Agent

You are a workflow execution assistant. You help users run workflow steps interactively — choosing which steps to run, monitoring progress, and reporting results.

## 🤖 ROLE
- Execute workflow steps in the background and report results
- Help the user choose which steps to run and in what order
- Report step results clearly and concisely
- Debug execution failures and help the user understand what went wrong
- Run shell commands only for quick checks (ls, cat output files) — NOT for investigating execution logs

**NEVER search, read, or explore the application source code** (*.go, *.ts, *.json outside the workspace). You operate on the WORKSPACE only — plan.json, step_config.json, learnings/, runs/, execution/. Do NOT run find/grep on the project codebase. If you need information about how something works, use the tools (get_step_details, debug_step, get_workflow_config, etc.).

## ⚙️ TOOLS

### Discovery
- **list_steps** — Lists all steps with IDs, titles, types, and config tags (optimized, learning mode). Use this first, then get_step_details for full info on specific steps.
- **get_step_details(step_id)** — Full details of a single step: description, success_criteria, context_dependencies, validation_schema, and agent_configs (servers, tools, mode, LLMs).
- **list_runs** — Lists existing iteration folders and their group subfolders. Use this to discover previous runs before executing.
- **list_groups** — Lists available variable groups. The active group is auto-selected from the workspace toolbar; use this to discover groups if you need to override.

### Background Step Execution
- **execute_step(step_id, group_id?, run_folder?, instructions?)** — Start a step in the background; returns execution_id immediately. **group_id** defaults to the toolbar-selected group; pass explicitly to override. **run_folder** targets a specific iteration; if omitted, auto-resolves. **instructions** provides orchestrator context for inner steps. **Important: by default, skip_learning=true** — no learnings are generated after execution for faster iteration. To generate learnings, pass skip_learning=false explicitly.
- **query_step(execution_id)** — Lightweight status check: running/done/failed/cancelled + result. No file I/O overhead.
- **query_step_tools(execution_id)** — Show which tools a running step is calling in real time. Most useful for execute_step (shows MCP tool calls). For learning/optimization agents, prefer debug_step for richer insights.
- **debug_step(step_id)** — Rich insights: learning status, validation result, iteration details for complex steps, log paths. Use after a step completes or to inspect a running complex step.
- **list_executions(status_filter?)** — List all background executions with their execution_id, step_id, and status. Use to find execution IDs for query_step or stop_step.
- **run_background_task(task, task_name, max_turns?)** — Run a generic background sub-agent with full tool access. Use to offload complex work like bulk analysis, data transformation, file cleanup, or any task that doesn't fit the other tools.
- **stop_step(execution_id)** — Cancel a running step
- **get_cost_summary** — Show token usage and cost breakdown (per-step, per-model, per-phase) for the current run

### Read-Only Info
- **get_step_prompts(step_id, attempt?, iteration?)** — Get the system prompt and user message for a step. **Works during execution** (prompts saved at start) and after completion. Supports all step types.
- **get_workflow_config** — See full workflow config: MCP servers (selected + available with descriptions), skills, secrets (names only), LLM config (tiered + defaults with fallbacks)
- **get_llm_config** — Show per-step LLM overrides from step_config.json
- **get_variables** — Read variable definitions and group configurations

### Shell & Human
- **execute_shell_command** — Run shell commands for investigation
- **human_feedback** — Ask the user a question or request confirmation

## 🎓 Human-Assisted Learning Best Practice

When a step has learning_mode "human_assisted", the recommended workflow is:

1. **Explore first** — Before running the step via the workflow, use execute_shell_command to manually explore the task yourself: check the environment, APIs, file paths, tool outputs. Understand what works and what doesn't.
2. **Discuss with the user** — Share your findings and ask the user to confirm the correct approach, expected output, or any edge cases they care about.
3. **Write the learnings** — Based on your exploration and the user's input, write specific actionable learnings directly to 'learnings/{step-id}/'. Use diff_patch_workspace_file to create or update learning files.
4. **Lock the learnings** — Call update_step_config(step_id, lock_learnings=true) so the learning agent doesn't overwrite your hand-crafted learnings on the next run.
5. **Run via workflow** — Now execute_step with the enriched learnings in place. The step agent will use them during execution.

**Important distinction**: "human_tools:human_feedback" and "learning_mode: human_assisted" are completely unrelated features:
- **human_feedback tool** = lets the execution agent ask the user questions mid-run (e.g., "what's the OTP?"). Configured via enabled_custom_tools in step_config.
- **learning_mode: human_assisted** = controls whether the learning phase runs automatically after execution or waits for manual trigger via generate_learnings. This is about post-execution learning, NOT runtime interaction.

## 📂 WORKSPACE FILE LAYOUT

All paths below are relative to the workspace root. Use **execute_shell_command** to read/list these files.

### Execution Logs
- 'runs/{run-folder}/logs/step-{N}/execution/' — Execution agent logs for step N
  - 'execution-attempt-{A}-iteration-{I}.json' — Execution result JSON (agent output, tool calls, validation)
  - 'system_prompt.txt', 'user_message.txt' — Prompts sent to the execution agent (also available via **get_step_prompts** tool)
- 'runs/{run-folder}/logs/step-{N}-{true|false}-{idx}/execution/' — Branch step logs (conditional/decision)
- 'runs/{run-folder}/logs/step-{N}-sub-agent-{idx}/execution/' — Sub-agent step logs

### Token Usage
- 'runs/{run-folder}/token_usage.json' — Per-step token usage for this run (input/output tokens, cost, model used)
- 'token_usage.json' — Aggregated token usage across all phases (planning, execution, learning, etc.)

### Plan & Config
- 'planning/plan.json' — The current workflow plan
- 'planning/step_config.json' — Step-level configuration overrides

{{if eq .UseKnowledgebase "true"}}### Persistent Storage (Knowledgebase)
- **knowledgebase/**: Persistent folder at workspace root. Never deleted across runs.
- Steps can read from and write to this folder to share data across iterations.
{{end}}

**IMPORTANT: Do NOT attempt to read or access the application's Go source code (*.go files) when debugging step execution.** You are debugging *workflow step outputs*, not the application itself. All the information you need is in the workspace files above — execution logs, conversation histories, prompts, and token usage. If something looks wrong, investigate via these workspace artifacts and log paths, not by reading the codebase.

## 📋 WORKSPACE CONTEXT

- **Workspace**: {{.WorkspacePath}}
- **Run Folder**: {{.RunFolder}}
- **Progress**: {{if .ProgressSummary}}{{.ProgressSummary}}{{else}}No progress tracked yet{{end}}

### Current Plan
{{if .PlanJSON}}` + "```json\n{{.PlanJSON}}\n```" + `{{else}}No plan available.{{end}}

## 📖 STEP EXECUTION WORKFLOW

### Before Running Any Step — Always Determine Iteration and Group First

**Every time the user asks to run a step**, do this before calling execute_step:

1. Call **list_runs** to see existing iterations and their group subfolders (with group_ids)
2. Call **list_groups** to see available groups and their group_ids
3. **Confirm with the user** which iteration and which group to use — do NOT assume or guess
   - Default suggestion: latest iteration + the group matching the user's request
   - Only use a different iteration if the user explicitly asks
4. Once confirmed, pass both **iteration** and **group_id** explicitly to every execute_step call

**Never guess the group_id from the run folder path or the user's name** — always use the group_id shown by list_groups (e.g., "group-1", "group-2").

### Running Steps
1. User says "run step-X" (or "run all") → determine iteration + group first (see above) → call **execute_step("step-id", iteration, group_id)** → get execution_id
2. **Note**: By default, execute_step runs with **skip_learning=true** — no learnings are generated after execution, for faster iteration. If the user wants learnings generated, pass skip_learning=false explicitly. When learning is enabled, **success learnings run in background** (the next step starts immediately without waiting), while **failure learnings run sequentially** (needed before retry attempts).
3. Tell user step is running. **You will be automatically notified** when it completes — do NOT poll with query_step in a loop.
4. When the auto-notification arrives with the result — report it to the user clearly
   - ✅ If success: show the key output/result and ask what to do next
   - ❌ If failed: show the error and explain to the user what went wrong. Use **debug_step** for deeper analysis if needed.
5. Let the user decide the next step — they may want to re-run, skip to another step, or stop

### Running All Steps
When the user asks to "run all" or "run the workflow":
1. Call **list_steps** to get all steps in order
2. Execute them sequentially (you'll be auto-notified when each completes before starting the next)
3. For conditional/decision steps, the execution engine handles branching automatically
4. Report progress after each step completes
5. If a step fails, ask the user whether to continue with the next step or stop

## 🏗️ STEP TYPES & INNER STEPS

The plan can contain several step types. Top-level steps appear in the plan's "steps" array. Some step types contain **inner steps** (sub-steps) that can also be executed individually.

### Step Types
- **Regular** (type: "regular"): Standard task. Executes an agent that produces a context_output file.
- **Decision** (type: "decision"): Executes a step, then branches based on evidence in context. Contains **if_true_steps** and **if_false_steps** — arrays of inner steps that run depending on the decision outcome.
- **Conditional** (type: "conditional"): Inspection-only branch (no execution of the main step). Contains **if_true_steps** and **if_false_steps** — evaluated based on prior context without running an agent.
- **Todo Task** (type: "todo_task"): Manages a dynamic todo list with trackable tasks. Has a **todo_task_step** (main orchestrator) and **predefined_routes** — each route has a **sub_agent_step** (inner step with its own description, tools, and learning).
- **Routing / Orchestration** (type: "routing"): N-way LLM-based routing. Has an **orchestration_step** (main evaluator) and **orchestration_routes** — each route has a **sub_agent_step** (inner step).
- **Human Input** (type: "human_input"): Asks a question to the user and blocks until response. Supports response types: 'text', 'yesno', 'multiple_choice'. Can route based on response.
- **Loop** (type: "loop"): Repeat until criteria met (polled progress).
- **Orphan** (is_orphan: true): A step not part of the main execution flow. Sits outside the main chain and can only be triggered manually via 'execute_step' in the workshop.

### Inner Steps
Inner steps live inside conditional branches, orchestration routes, or todo_task routes. They have their own step IDs and can be individually:
- Listed via **list_steps** (shown indented under their parent)
- Executed via **execute_step(inner_step_id)**

## ⚠️ RULES
1. **Async first**: Always use execute_step for running steps — never block waiting
2. **Auto-notified**: You are automatically notified when background agents complete. Do NOT poll with query_step in a loop or ask the user to tell you when something finishes. Use query_step_tools if you need to see what tools a running step is calling.
3. **Report results**: Always show the user the step result after completion
4. **User decides**: Let the user choose what to run and when — don't force a sequence
5. **Use step IDs**: Step IDs come from plan.json (e.g., "step-create-report")
6. **Be helpful**: Explain what each step does when listing them, so the user can make informed choices
7. **Never hardcode variables or secrets**: Do NOT put actual variable values, account numbers, user IDs, passwords, tokens, or any sensitive/environment-specific data into step instructions or learning files. Use variable placeholders (e.g., {USER_ID}, {ACCOUNT_NO}) instead.
`)

// Execute implements OrchestratorAgent interface for the interactive workshop agent
func (agent *WorkflowInteractiveWorkshopAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}

	mcpAgentRef := baseAgent.Agent()
	iwm := agent.iwm
	workspacePath := templateVars["WorkspacePath"]

	// Logger (prefer mcpagent logger, fall back to controller logger)
	var logger loggerv2.Logger
	if mcpAgentRef != nil && mcpAgentRef.Logger != nil {
		logger = mcpAgentRef.Logger
	} else {
		logger = iwm.controller.GetLogger()
	}

	// Register plan modification tools
	if err := RegisterPlanModificationTools(
		mcpAgentRef,
		workspacePath,
		logger,
		iwm.controller.ReadWorkspaceFile,
		iwm.controller.WriteWorkspaceFile,
		iwm.controller.MoveWorkspaceFile,
		"workflow-builder",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register plan modification tools: %v", err))
	}

	// Register custom workshop tools (execute_step, query_step, stop_step, update_step_config)
	registerInteractiveWorkshopTools(iwm, mcpAgentRef, logger)

	// Update code execution registry for CLI providers (claude-code, gemini-cli)
	if agent.GetConfig().UseCodeExecutionMode {
		if err := mcpAgentRef.UpdateCodeExecutionRegistry(); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to update code execution registry with workshop tools: %v", err))
		} else {
			logger.Info("✅ Code execution registry updated with workshop tools")
		}
	}

	// Build system prompt and initial user message
	var systemPrompt, userMessage strings.Builder
	if err := interactiveWorkshopSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := interactiveWorkshopUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}

	sessionID := templateVars["SessionID"]
	workflowID := templateVars["WorkflowID"]

	// Emit start event
	eventBridge := iwm.controller.GetContextAwareBridge()
	if eventBridge != nil {
		startedEvent := &orchestrator_events.OrchestratorAgentStartEvent{
			BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
			AgentType:     "workflow-builder",
			AgentName:     "workflow-builder-agent",
			Objective:     "Workflow builder session",
			InputData:     templateVars,
		}
		eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
			Type:      orchestrator_events.OrchestratorAgentStart,
			Timestamp: time.Now(),
			Data:      startedEvent,
		})
	}

	currentResult := ""
	currentConversationHistory := conversationHistory

	// Free-flow loop — no cap; ends only when user approves or provides empty feedback
	for {
		inputProcessor := func(map[string]string) string { return userMessage.String() }

		result, updatedHistory, err := agent.ExecuteWithTemplateValidation(
			ctx, templateVars, inputProcessor,
			currentConversationHistory, struct{}{},
			systemPrompt.String(), true,
		)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedHistory

		// Ask user if done or what to do next
		requestID := fmt.Sprintf("workshop_continue_%d", time.Now().UnixNano())
		approved, feedback, err := iwm.controller.RequestHumanFeedback(
			ctx, requestID,
			"Done? Or tell me what to do next.",
			currentResult,
			sessionID,
			workflowID,
		)
		if err != nil {
			break
		}
		if approved || feedback == "" {
			break
		}
		// Continue with user feedback as next message
		var feedbackBuilder strings.Builder
		feedbackBuilder.WriteString(feedback)
		userMessage = feedbackBuilder
	}

	// Emit completion event
	if eventBridge != nil {
		completedEvent := &orchestrator_events.OrchestratorAgentEndEvent{
			BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
			AgentType:     "workflow-builder",
			AgentName:     "workflow-builder-agent",
			Objective:     "Workflow builder session",
			Result:        currentResult,
			Success:       true,
		}
		eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
			Type:      orchestrator_events.OrchestratorAgentEnd,
			Timestamp: time.Now(),
			Data:      completedEvent,
		})
	}

	return currentResult, currentConversationHistory, nil
}

// ============================================================================
// Custom Workshop Tools
// ============================================================================

// resolveWorkshopStepID resolves a user-provided step reference to an actual step ID from the plan.
// Accepts exact IDs, 1-based positions ("1", "step-1", "step1"), and falls back with suggestions.
// Requires plan to be loaded on the controller (call LoadPlanForWorkshop first).
func resolveWorkshopStepID(controller *StepBasedWorkflowOrchestrator, inputID string) (string, error) {
	plan := controller.approvedPlan
	if plan == nil || len(plan.Steps) == 0 {
		return "", fmt.Errorf("no plan loaded")
	}

	// 1. Exact match (top-level + inner steps)
	if info := findWorkshopStepByID(plan.Steps, inputID); info != nil {
		return inputID, nil
	}

	// 2. Positional match: extract number from "1", "step-1", "step1", "step 1"
	numStr := inputID
	for _, prefix := range []string{"step-", "step ", "step"} {
		if strings.HasPrefix(strings.ToLower(inputID), prefix) {
			numStr = inputID[len(prefix):]
			break
		}
	}
	var pos int
	if _, err := fmt.Sscanf(strings.TrimSpace(numStr), "%d", &pos); err == nil && pos >= 1 && pos <= len(plan.Steps) {
		return plan.Steps[pos-1].GetID(), nil
	}

	// 3. Not found — return valid IDs for helpful error message
	allSteps := collectAllSteps(plan.Steps)
	ids := make([]string, 0, len(allSteps))
	for _, info := range allSteps {
		label := fmt.Sprintf("%q", info.Step.GetID())
		if info.TopIndex > 0 {
			label = fmt.Sprintf("%q (step %d)", info.Step.GetID(), info.TopIndex)
		} else {
			label = fmt.Sprintf("%q (inner, parent=%s, branch=%s)", info.Step.GetID(), info.ParentID, info.BranchName)
		}
		ids = append(ids, label)
	}
	return "", fmt.Errorf("step %q not found in plan. Valid IDs: %s", inputID, strings.Join(ids, ", "))
}

// registerInteractiveWorkshopTools registers the custom workshop tools on the agent
func registerInteractiveWorkshopTools(iwm *InteractiveWorkshopManager, mcpAgent *mcpagent.Agent, logger loggerv2.Logger) {
	// Tool 0: list_steps — list all steps with IDs and titles
	if err := mcpAgent.RegisterCustomTool(
		"list_steps",
		"List all steps in the current plan with their IDs and titles. Call this to discover step IDs before using execute_step.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v", err), nil
			}
			plan := iwm.controller.approvedPlan
			if plan == nil || len(plan.Steps) == 0 {
				return "No steps found in plan.", nil
			}

			// Load step configs to show optimized + learning_mode
			stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
			stepConfigMap := make(map[string]*AgentConfigs, len(stepConfigs))
			for i := range stepConfigs {
				if stepConfigs[i].AgentConfigs != nil {
					stepConfigMap[stepConfigs[i].ID] = stepConfigs[i].AgentConfigs
				}
			}

			var sb strings.Builder
			allSteps := collectAllSteps(plan.Steps)
			topCount := len(plan.Steps)
			innerCount := len(allSteps) - topCount
			sb.WriteString(fmt.Sprintf("## Plan Steps (%d top-level", topCount))
			if innerCount > 0 {
				sb.WriteString(fmt.Sprintf(", %d inner", innerCount))
			}
			sb.WriteString(")\n\n")
			for _, info := range allSteps {
				step := info.Step
				stepID := step.GetID()

				// Build tags (optimized, learning mode, agent mode)
				var tags []string
				if cfg, ok := stepConfigMap[stepID]; ok {
					if cfg.Optimized != nil && *cfg.Optimized {
						tags = append(tags, "✓ optimized")
					}
					if cfg.LearningMode != "" {
						tags = append(tags, "learning:"+cfg.LearningMode)
					}
					if cfg.DisableLearning != nil && *cfg.DisableLearning {
						tags = append(tags, "learning:disabled")
					}
					// Agent execution mode
					if cfg.UseCodeExecutionMode != nil && *cfg.UseCodeExecutionMode {
						tags = append(tags, "mode:code-exec")
					} else if cfg.UseToolSearchMode != nil && *cfg.UseToolSearchMode {
						tags = append(tags, "mode:tool-search")
					} else {
						tags = append(tags, "mode:simple")
					}
				}
				tagStr := ""
				if len(tags) > 0 {
					tagStr = "  [" + strings.Join(tags, ", ") + "]"
				}

				if info.TopIndex > 0 {
					// Top-level step
					sb.WriteString(fmt.Sprintf("%d. **%s** [%s]%s\n   ID: `%s`\n\n", info.TopIndex, step.GetTitle(), step.StepType(), tagStr, stepID))
				} else {
					// Inner step — indented
					sb.WriteString(fmt.Sprintf("   ↳ **%s** [%s] (parent: `%s`, branch: %s)%s\n      ID: `%s`\n\n", step.GetTitle(), step.StepType(), info.ParentID, info.BranchName, tagStr, stepID))
				}
			}
			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_steps tool: %v", err))
	}

	// Tool: get_step_details — get full details of a single step
	if err := mcpAgent.RegisterCustomTool(
		"get_step_details",
		"Get full details of a single step: description, success_criteria, context_dependencies, context_output, validation_schema, and agent_configs (servers, tools, mode, learning settings, LLMs). Use after list_steps to drill into a specific step.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or positional reference (e.g., '1', 'step-1')",
				},
			},
			"required": []string{"step_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v", err), nil
			}
			resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}

			stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, resolvedID)
			if stepInfo == nil {
				return fmt.Sprintf("step %q not found in plan", stepID), nil
			}

			step := stepInfo.Step
			var sb strings.Builder

			// Header
			sb.WriteString(fmt.Sprintf("## %s [%s]\n", step.GetTitle(), step.StepType()))
			sb.WriteString(fmt.Sprintf("ID: `%s`\n\n", step.GetID()))

			// Description
			if desc := step.GetDescription(); desc != "" {
				sb.WriteString("### Description\n")
				sb.WriteString(desc)
				sb.WriteString("\n\n")
			}

			// Success criteria
			if sc := step.GetSuccessCriteria(); sc != "" {
				sb.WriteString("### Success Criteria\n")
				sb.WriteString(sc)
				sb.WriteString("\n\n")
			}

			// Context dependencies
			if deps := step.GetContextDependencies(); len(deps) > 0 {
				sb.WriteString("### Context Dependencies\n")
				for _, d := range deps {
					sb.WriteString(fmt.Sprintf("- %s\n", d))
				}
				sb.WriteString("\n")
			}

			// Context output
			if co := step.GetContextOutput().String(); co != "" {
				sb.WriteString("### Context Output\n")
				sb.WriteString(co)
				sb.WriteString("\n\n")
			}

			// Validation schema
			if vs := step.GetValidationSchema(); vs != nil {
				vsJSON, err := json.MarshalIndent(vs, "", "  ")
				if err == nil {
					sb.WriteString("### Validation Schema\n```json\n")
					sb.WriteString(string(vsJSON))
					sb.WriteString("\n```\n\n")
				}
			}

			// Step-type-specific fields (serialize the full step minus common fields)
			stepJSON, err := json.MarshalIndent(step, "", "  ")
			if err == nil {
				sb.WriteString("### Step Definition (full)\n```json\n")
				sb.WriteString(string(stepJSON))
				sb.WriteString("\n```\n\n")
			}

			// Agent configs from step_config.json
			stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
			var agentCfg *AgentConfigs
			for i := range stepConfigs {
				if stepConfigs[i].ID == resolvedID && stepConfigs[i].AgentConfigs != nil {
					agentCfg = stepConfigs[i].AgentConfigs
					break
				}
			}
			if agentCfg != nil {
				cfgJSON, err := json.MarshalIndent(agentCfg, "", "  ")
				if err == nil {
					sb.WriteString("### Agent Configs (from step_config.json)\n```json\n")
					sb.WriteString(string(cfgJSON))
					sb.WriteString("\n```\n")
				}
			} else {
				sb.WriteString("### Agent Configs\nNo step-level config overrides — using preset defaults.\n")
			}

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_step_details tool: %v", err))
	}

	// Tool: list_runs — discover existing iterations and run folders
	if err := mcpAgent.RegisterCustomTool(
		"list_runs",
		"List existing iteration/run folders in the workspace. Shows which iterations exist and what group subfolders they contain. Use this to discover previous runs before asking the user whether to continue an existing iteration or start a new one.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			runsPath := fmt.Sprintf("%s/runs", iwm.controller.GetWorkspacePath())
			iterations, err := iwm.controller.listRunFolders(ctx, runsPath)
			if err != nil || len(iterations) == 0 {
				return "No runs found. This workflow has not been executed yet. A new iteration-1 will be created on first execute_step.", nil
			}

			// Build folder-name → group_id lookup from manifest
			folderToGroupID := map[string]string{}
			if iwm.controller.variablesManifest != nil {
				for _, g := range iwm.controller.variablesManifest.Groups {
					folderName := g.GroupID
					if g.DisplayName != "" {
						if s := iwm.controller.sanitizeDisplayNameForFolder(g.DisplayName); s != "" {
							folderName = s
						}
					}
					folderToGroupID[folderName] = g.GroupID
				}
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Existing Runs (%d iterations)\n\n", len(iterations)))

			for _, iterFolder := range iterations {
				iterPath := fmt.Sprintf("%s/%s", runsPath, iterFolder)
				subFolders, err := iwm.controller.listRunFolders(ctx, iterPath)
				if err != nil || len(subFolders) == 0 {
					sb.WriteString(fmt.Sprintf("- **%s** (no group subfolders)\n", iterFolder))
				} else {
					var groupDescs []string
					for _, sf := range subFolders {
						if gid, ok := folderToGroupID[sf]; ok {
							groupDescs = append(groupDescs, fmt.Sprintf("%s (group_id: %s)", sf, gid))
						} else {
							groupDescs = append(groupDescs, sf)
						}
					}
					sb.WriteString(fmt.Sprintf("- **%s** (%d groups: %s)\n", iterFolder, len(subFolders), strings.Join(groupDescs, ", ")))
				}
			}

			maxIter := iwm.controller.findMaxIterationNumber(iterations)
			sb.WriteString(fmt.Sprintf("\n**Latest iteration**: iteration-%d\n", maxIter))
			sb.WriteString(fmt.Sprintf("**Next new iteration**: iteration-%d\n", maxIter+1))
			sb.WriteString("\nTo run on an existing iteration, pass `iteration` to `execute_step` (e.g., `iteration-2`).\n")
			sb.WriteString("To start a new iteration, pass the next iteration number (e.g., `iteration-3`).\n")
			sb.WriteString("The run folder is auto-calculated from iteration + group.")

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_runs tool: %v", err))
	}

	// Tool: list_groups — discover available variable groups (read-only)
	if err := mcpAgent.RegisterCustomTool(
		"list_groups",
		"List available variable groups for step execution. Each group has a group_id, display_name, variable values, and enabled status. The active group is auto-selected from the workspace toolbar; use this to discover groups if you need to override.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			manifest := iwm.controller.variablesManifest
			if manifest == nil || !manifest.HasGroups() {
				if manifest != nil && len(manifest.Variables) > 0 {
					values := make(map[string]string)
					for _, v := range manifest.Variables {
						values[v.Name] = v.Value
					}
					return fmt.Sprintf("No variable groups defined. Single set of variables:\n%v", values), nil
				}
				return "No variable groups defined. This workflow does not use variables.", nil
			}

			var sb strings.Builder
			groups := manifest.Groups
			enabledCount := 0
			for _, g := range groups {
				if g.Enabled {
					enabledCount++
				}
			}
			sb.WriteString(fmt.Sprintf("## Variable Groups (%d total, %d enabled)\n\n", len(groups), enabledCount))

			// Show active group from toolbar selection
			activeGroupID := ""
			if len(iwm.controller.enabledGroupIDs) > 0 {
				activeGroupID = iwm.controller.enabledGroupIDs[0]
			}

			for _, g := range groups {
				status := "enabled"
				if !g.Enabled {
					status = "disabled"
				}
				displayName := g.DisplayName
				if displayName == "" {
					displayName = g.GroupID
				}
				active := ""
				if g.GroupID == activeGroupID {
					active = " **[ACTIVE]**"
				}
				sb.WriteString(fmt.Sprintf("- **%s** (group_id: `%s`, %s)%s\n", displayName, g.GroupID, status, active))
				if len(g.Values) > 0 {
					for k, v := range g.Values {
						sb.WriteString(fmt.Sprintf("  - %s = %s\n", k, v))
					}
				}
			}
			if activeGroupID != "" {
				sb.WriteString(fmt.Sprintf("\nActive group (from toolbar): `%s` — used by default for execute_step.\n", activeGroupID))
			}
			sb.WriteString("\nTo override, pass `group_id` to `execute_step`.")
			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_groups tool: %v", err))
	}

	// Tool 1: execute_step — start step in background
	if err := mcpAgent.RegisterCustomTool(
		"execute_step",
		"Start a workflow step in the background. Returns an execution_id immediately. You will be automatically notified when it completes. By default, learning is skipped (skip_learning=true) for faster iteration. Set skip_learning=false to generate/update learnings after execution. When enabled, success learnings run in background (next step starts immediately), failure learnings run sequentially (needed for retry).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or positional reference (e.g., '1', 'step-1', 'step1')",
				},
				"group_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional variable group ID override (e.g., 'group-1'). If omitted, uses the group selected in the workspace toolbar. Use list_groups to see available groups.",
				},
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Iteration folder name (e.g., 'iteration-3'). Use list_runs to see available iterations. The full run folder is auto-calculated from iteration + group.",
				},
				"skip_learning": map[string]interface{}{
					"type":        "boolean",
					"description": "If true (default), skip the learning phase after execution for faster iteration. Set to false to generate/update learnings.",
				},
				"instructions": map[string]interface{}{
					"type":        "string",
					"description": "Optional orchestrator instructions for inner steps (sub-agents from todo_task/orchestration routes). Appended to the step description as '## Orchestrator Instructions'. Simulates what the parent orchestrator would provide when delegating. Ignored for top-level steps.",
				},
				"human_input": map[string]interface{}{
					"type":        "string",
					"description": "Optional human input/custom instructions for the step agent. Injected as high-priority '🚨 HUMAN FEEDBACK (CRITICAL)' context that takes precedence over other instructions. Use this to guide the agent's behavior, override defaults, or provide clarifications. Works for all step types (execution, todo_task, etc.).",
				},
				"tier": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"high", "medium", "low"},
					"description": "Optional LLM tier override for this execution. 'high' = Tier 1 (most capable), 'medium' = Tier 2, 'low' = Tier 3 (fastest/cheapest). Overrides the default maturity-based tier selection. Only works in tiered mode.",
				},
			},
			"required": []string{"step_id", "iteration"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			// Extract group_id, iteration, and other options
			groupIDRaw, _ := args["group_id"]
			iterationRaw, _ := args["iteration"]
			groupID, _ := groupIDRaw.(string)
			iteration, _ := iterationRaw.(string)

			// Fallback to session-level group from toolbar selection
			if groupID == "" && len(iwm.controller.enabledGroupIDs) > 0 {
				groupID = iwm.controller.enabledGroupIDs[0]
			}

			// Validate iteration is provided
			if iteration == "" {
				return "iteration is required (e.g., 'iteration-3'). Use list_runs to see available iterations.", nil
			}

			// Build run_folder from iteration + group folder name
			// Resolve group folder name from group_id (uses sanitized display name or group_id)
			groupFolderName := groupID
			if iwm.controller.variablesManifest != nil && groupID != "" {
				for _, g := range iwm.controller.variablesManifest.Groups {
					if g.GroupID == groupID || iwm.controller.sanitizeDisplayNameForFolder(g.DisplayName) == groupID {
						if g.DisplayName != "" {
							sanitized := iwm.controller.sanitizeDisplayNameForFolder(g.DisplayName)
							if sanitized != "" {
								groupFolderName = sanitized
							}
						}
						break
					}
				}
			}
			runFolder := fmt.Sprintf("%s/%s", iteration, groupFolderName)

			// skip_learning defaults to true for faster workshop iteration
			skipLearning := true
			if val, ok := args["skip_learning"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					skipLearning = b
				}
			}

			// Optional orchestrator instructions for inner steps
			instructions, _ := args["instructions"].(string)

			// Optional human input for any step type
			humanInput, _ := args["human_input"].(string)

			// Optional tier override (high=1, medium=2, low=3)
			tierValue := 0
			if tierStr, ok := args["tier"].(string); ok && tierStr != "" {
				switch tierStr {
				case "high":
					tierValue = 1
				case "medium":
					tierValue = 2
				case "low":
					tierValue = 3
				}
			}

			execOpts := &WorkshopExecuteOptions{
				GroupID:      groupID,
				RunFolder:    runFolder,
				SkipLearning: skipLearning,
				Instructions: instructions,
				HumanInput:   humanInput,
				Tier:         tierValue,
			}

			// Resolve flexible step ID (handles "1", "step-1", "step1" etc.)
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v. Cannot resolve step ID.", err), nil
			}
			resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}
			stepID = resolvedID

			execID := fmt.Sprintf("exec-%s-%05d", stepID, time.Now().UnixNano()%100000)
			execCtx, cancel := context.WithCancel(iwm.sessionCtx)

			// Inject correlation IDs so step execution events are tagged as sub-agent
			// events. ForceCorrelationIDKey survives child agent context overwrites
			// (child agents overwrite AgentSessionIDKey but not ForceCorrelationIDKey),
			// so all nested events share the same correlation_id as the
			// orchestrator_agent_start event, enabling frontend grouping.
			agentSessionID := fmt.Sprintf("workshop-step-%s-%d", stepID, time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         stepID,
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			go func() {
				// Inject tier override into context if specified (concurrent-safe via context, not shared field)
				if execOpts.Tier >= 1 && execOpts.Tier <= 3 {
					execCtx = context.WithValue(execCtx, WorkshopTierOverrideKey, execOpts.Tier)
				}

				// Emit orchestrator_agent_start so the frontend creates a grouping card
				eventBridge := iwm.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-execution",
						AgentName:     fmt.Sprintf("Step: %s", stepID),
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				result, err := iwm.controller.ExecuteStepForWorkshop(execCtx, stepID, execOpts)

				// Check if step is marked as optimized in step config
				isOptimized := false
				if configs, configErr := iwm.controller.ReadStepConfigs(execCtx); configErr == nil {
					for _, sc := range configs {
						if sc.ID == stepID && sc.AgentConfigs != nil && sc.AgentConfigs.Optimized != nil && *sc.AgentConfigs.Optimized {
							isOptimized = true
							break
						}
					}
				}

				// Emit orchestrator_agent_end to close the grouping card
				if eventBridge != nil {
					isCancelled := execCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-execution",
						AgentName:     fmt.Sprintf("Step: %s", stepID),
						Success:       err == nil,
					}
					if isOptimized {
						endEvent.InputData = map[string]string{"step_optimized": "true"}
					}
					if err != nil {
						if isCancelled {
							endEvent.Result = fmt.Sprintf("Cancelled: %v", err)
						} else {
							endEvent.Result = fmt.Sprintf("Failed: %v", err)
						}
					} else {
						endEvent.Result = result
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentEnd,
						Timestamp:     time.Now(),
						Data:          endEvent,
						CorrelationID: agentSessionID,
					})
				}

				exec.mu.Lock()
				defer exec.mu.Unlock()
				// Don't overwrite "cancelled" status — stop_step may have already set it
				if exec.Status == WorkshopStepCancelled {
					return
				}
				if err != nil {
					// Check if this is a context cancellation (user stop, session timeout, etc.)
					// — treat as cancelled, not failed. Only real failures (validation, execution errors)
					// should be reported to the agent for debugging.
					if execCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						exec.Status = WorkshopStepCancelled
						exec.Err = err
					} else {
						exec.Status = WorkshopStepFailed
						exec.Err = err
					}
				} else {
					exec.Status = WorkshopStepDone
					exec.Result = result
				}
			}()

			groupInfo := ""
			if groupID != "" {
				groupInfo = fmt.Sprintf(", group=%q", groupID)
			}
			learningInfo := "Learning: skipped (default for faster iteration). To generate learnings after execution, use generate_learnings(step_id). To run with learning enabled, use execute_step(step_id, skip_learning=false)."
			if !skipLearning {
				learningInfo = "Learning: enabled — success learnings run in background (won't block next step), failure learnings run sequentially (needed for retry)."
			}
			logger.Info(fmt.Sprintf("🚀 Workshop: step %q started in background, execution_id=%q%s, skip_learning=%v", stepID, execID, groupInfo, skipLearning))
			return fmt.Sprintf("Step %q started in background.\nexecution_id: %q\n%s\nYou will be automatically notified when it completes.", stepID, execID, learningInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register execute_step tool: %v", err))
	}

	// Tool 2: query_step — lightweight status check (no file I/O for running steps)
	if err := mcpAgent.RegisterCustomTool(
		"query_step",
		"Check the status of a background step execution. Returns status (running/done/failed/cancelled) and the result. Lightweight — use debug_step for detailed insights.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "The execution_id returned by execute_step",
				},
			},
			"required": []string{"execution_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			execIDRaw, ok := args["execution_id"]
			if !ok || execIDRaw == nil {
				return "execution_id is required", nil
			}
			execID, ok := execIDRaw.(string)
			if !ok || execID == "" {
				return "execution_id must be a non-empty string", nil
			}

			exec := iwm.stepRegistry.Get(execID)
			if exec == nil {
				return fmt.Sprintf("execution %q not found", execID), nil
			}

			exec.mu.RLock()
			defer exec.mu.RUnlock()

			switch exec.Status {
			case WorkshopStepRunning:
				return fmt.Sprintf("Step %q is still running. Use query_step again later or debug_step for live progress on complex steps.", exec.StepID), nil
			case WorkshopStepDone:
				return fmt.Sprintf("Step %q completed.\n\n%s\n\n**Next actions (do these now):**\n1. Review the result against the step's success criteria\n2. Read learnings: 'cat learnings/%s/*.md' — are they specific and actionable? Edit or delete noisy ones.\n3. Check learning metadata: 'cat learnings/%s/.learning_metadata.json' — if consecutive_successes >= 3, consider locking learnings.\n4. Note the highest-priority optimization from Post-Execution Step Review.\n5. If output looks wrong, investigate with debug_step(%q) or analyze_step(%q) and fix the root cause before re-running.", exec.StepID, exec.Result, exec.StepID, exec.StepID, exec.StepID, exec.StepID), nil
			case WorkshopStepFailed:
				return fmt.Sprintf("Step %q failed: %v\n\n**Next**: Investigate the failure. Call debug_step(%q) for detailed execution insights, then fix the root cause (description, validation, context deps) before re-running.", exec.StepID, exec.Err, exec.StepID), nil
			case WorkshopStepCancelled:
				return fmt.Sprintf("Step %q was cancelled.", exec.StepID), nil
			default:
				return fmt.Sprintf("Step %q has unknown status: %s", exec.StepID, exec.Status), nil
			}
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register query_step tool: %v", err))
	}

	// Tool 2a: query_step_tools — real-time tool call visibility for running steps
	if err := mcpAgent.RegisterCustomTool(
		"query_step_tools",
		"Show which tools a running step is currently calling in real time. By default returns a summary with truncated args/results. Pass tool_call_id to get full input/output for a specific call. Most useful for execute_step (shows MCP tool calls). For learning/optimization agents, use debug_step instead for richer insights.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "The execution_id returned by execute_step",
				},
				"tool_call_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: a specific tool_call_id from the summary to get full input/output details",
				},
			},
			"required": []string{"execution_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			execIDRaw, ok := args["execution_id"]
			if !ok || execIDRaw == nil {
				return "execution_id is required", nil
			}
			execID, ok := execIDRaw.(string)
			if !ok || execID == "" {
				return "execution_id must be a non-empty string", nil
			}

			// Optional: specific tool_call_id for detailed view
			toolCallID := ""
			if val, ok := args["tool_call_id"]; ok && val != nil {
				if s, ok := val.(string); ok {
					toolCallID = s
				}
			}

			exec := iwm.stepRegistry.Get(execID)
			if exec == nil {
				return fmt.Sprintf("execution %q not found", execID), nil
			}

			exec.mu.RLock()
			status := exec.Status
			stepID := exec.StepID
			agentSessID := exec.AgentSessionID
			exec.mu.RUnlock()

			if status != WorkshopStepRunning {
				return fmt.Sprintf("Step %q is not running (status: %s). Use query_step for the final result.", stepID, status), nil
			}

			if iwm.toolCallQueryFunc == nil {
				return fmt.Sprintf("Step %q is running but real-time tool call tracking is not available in this session.", stepID), nil
			}

			mainSessID := iwm.mainSessionID
			if mainSessID == "" {
				mainSessID = iwm.sessionID
			}

			summary := iwm.toolCallQueryFunc(mainSessID, agentSessID, toolCallID)

			// Detect execution type from ID prefix and add context
			isAnalysisAgent := strings.HasPrefix(execID, "learn-") || strings.HasPrefix(execID, "debug-")
			var hint string
			if isAnalysisAgent {
				hint = "\n\nNote: This is a learning/optimization agent — it only uses workspace tools (execute_shell_command, diff_patch_workspace_file). For richer insights, use debug_step(step_id) instead."
			}

			if summary == "" {
				return fmt.Sprintf("Step %q is running. No tool calls observed yet.%s", stepID, hint), nil
			}

			if toolCallID != "" {
				return fmt.Sprintf("Step %q — tool call detail:\n%s", stepID, summary), nil
			}

			return fmt.Sprintf("Step %q is running. Tool calls:\n%s%s", stepID, summary, hint), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register query_step_tools tool: %v", err))
	}

	// Tool 2b: debug_step — rich insights about a step's execution
	if err := mcpAgent.RegisterCustomTool(
		"debug_step",
		"Get detailed insights about a step's execution: learning status, validation result, iteration details for complex steps (todo_task/orchestration), and log paths. Use after a step completes or to inspect a running complex step's progress.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or positional reference (e.g., '1', 'step-1')",
				},
			},
			"required": []string{"step_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			// Resolve step ID
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v", err), nil
			}
			resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}

			stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, resolvedID)
			if stepInfo == nil {
				return fmt.Sprintf("step %q not found in plan", stepID), nil
			}

			runFolder := iwm.controller.selectedRunFolder
			if runFolder == "" {
				return "no run folder selected", nil
			}

			var result strings.Builder
			result.WriteString(fmt.Sprintf("## Debug: %s (%s)\n\n", stepInfo.Step.GetTitle(), resolvedID))

			// Section 1: Learning status
			learningsPath := getLearningFolderPathByStepID("", resolvedID, "", iwm.controller.isEvaluationMode)
			learningFiles, _ := iwm.controller.readStepLearningFiles(ctx, learningsPath)

			stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
			lockStatus := "unlocked"
			for _, sc := range stepConfigs {
				if sc.ID == resolvedID && sc.AgentConfigs != nil {
					if sc.AgentConfigs.LockLearnings != nil && *sc.AgentConfigs.LockLearnings {
						lockStatus = "locked"
					}
					if sc.AgentConfigs.DisableLearning != nil && *sc.AgentConfigs.DisableLearning {
						lockStatus = "disabled"
					}
					break
				}
			}

			result.WriteString("### Learnings\n")
			if len(learningFiles) > 0 {
				fileNames := make([]string, 0, len(learningFiles))
				for f := range learningFiles {
					fileNames = append(fileNames, f)
				}
				sort.Strings(fileNames)
				result.WriteString(fmt.Sprintf("Files: %d | Status: %s | Path: %s\n", len(learningFiles), lockStatus, learningsPath))
				for _, f := range fileNames {
					result.WriteString(fmt.Sprintf("  - %s\n", f))
				}
			} else {
				result.WriteString(fmt.Sprintf("No learnings yet | Status: %s | Path: %s\n", lockStatus, learningsPath))
			}
			result.WriteString("\n")

			// Section 2: Validation result
			stepNum := stepInfo.TopIndex
			if stepNum < 1 {
				stepNum = 1 // inner steps use step-1
			}
			validationLogDir := fmt.Sprintf("runs/%s/logs/step-%d", runFolder, stepNum)

			result.WriteString("### Validation\n")
			foundValidation := false
			for i := 5; i >= 2; i-- {
				vPath := fmt.Sprintf("%s/validation-%d.json", validationLogDir, i)
				if content, err := iwm.controller.ReadWorkspaceFile(ctx, vPath); err == nil {
					result.WriteString(content)
					result.WriteString("\n")
					foundValidation = true
					break
				}
			}
			if !foundValidation {
				if content, err := iwm.controller.ReadWorkspaceFile(ctx, fmt.Sprintf("%s/validation.json", validationLogDir)); err == nil {
					result.WriteString(content)
					result.WriteString("\n")
				} else {
					result.WriteString("No validation result found.\n")
				}
			}
			result.WriteString("\n")

			// Section 3: Complex step details (todo_task/orchestration)
			complexSummary := iwm.enrichQueryForComplexStep(ctx, resolvedID)
			if complexSummary != "" {
				result.WriteString("### Execution Details")
				result.WriteString(complexSummary)
				result.WriteString("\n")
			}

			// Section 4: Log paths for manual inspection
			result.WriteString("### Log Paths\n")
			result.WriteString(fmt.Sprintf("Execution logs: runs/%s/logs/step-%d/execution/\n", runFolder, stepNum))
			result.WriteString(fmt.Sprintf("Validation: %s/\n", validationLogDir))
			result.WriteString(fmt.Sprintf("Learnings: %s/\n", learningsPath))
			result.WriteString("Use execute_shell_command to read these files for details.\n")

			return result.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register debug_step tool: %v", err))
	}

	// Tool 2b: list_executions — list all tracked background executions
	if err := mcpAgent.RegisterCustomTool(
		"list_executions",
		"List all background executions (execute_step, generate_learnings, optimize_step). Shows execution_id, step_id, status (running/done/failed/cancelled), and type. Useful when you need to find execution IDs for query_step or stop_step.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"status_filter": map[string]interface{}{
					"type":        "string",
					"description": "Optional filter: 'running', 'done', 'failed', 'cancelled'. If omitted, returns all.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			statusFilter, _ := args["status_filter"].(string)

			allExecs := iwm.stepRegistry.List()
			if len(allExecs) == 0 {
				return "No background executions tracked in this session.", nil
			}

			// Sort by ID (contains timestamp) for chronological order
			sort.Slice(allExecs, func(i, j int) bool {
				return allExecs[i].ID < allExecs[j].ID
			})

			var sb strings.Builder
			count := 0
			for _, exec := range allExecs {
				exec.mu.RLock()
				status := string(exec.Status)
				execErr := exec.Err
				exec.mu.RUnlock()

				if statusFilter != "" && status != statusFilter {
					continue
				}

				count++
				sb.WriteString(fmt.Sprintf("- **%s** | step: %s | status: %s", exec.ID, exec.StepID, status))
				if status == "failed" && execErr != nil {
					sb.WriteString(fmt.Sprintf(" | error: %v", execErr))
				}
				sb.WriteString("\n")
			}

			if count == 0 {
				if statusFilter != "" {
					return fmt.Sprintf("No executions with status %q. Total tracked: %d.", statusFilter, len(allExecs)), nil
				}
				return "No background executions tracked.", nil
			}

			return fmt.Sprintf("**%d execution(s)**%s:\n%s", count, func() string {
				if statusFilter != "" {
					return fmt.Sprintf(" (filter: %s)", statusFilter)
				}
				return ""
			}(), sb.String()), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_executions tool: %v", err))
	}

	// Tool 3: stop_step — cancel a running step
	if err := mcpAgent.RegisterCustomTool(
		"stop_step",
		"Cancel a running background step execution.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "The execution_id returned by execute_step",
				},
			},
			"required": []string{"execution_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			execIDRaw, ok := args["execution_id"]
			if !ok || execIDRaw == nil {
				return "execution_id is required", nil
			}
			execID, ok := execIDRaw.(string)
			if !ok || execID == "" {
				return "execution_id must be a non-empty string", nil
			}

			exec := iwm.stepRegistry.Get(execID)
			if exec == nil {
				return fmt.Sprintf("execution %q not found", execID), nil
			}

			exec.cancel()
			exec.mu.Lock()
			exec.Status = WorkshopStepCancelled
			exec.mu.Unlock()

			logger.Info(fmt.Sprintf("🛑 Workshop: step %q (execution_id=%q) cancelled", exec.StepID, execID))
			return fmt.Sprintf("Step %q (execution_id=%q) has been cancelled.", exec.StepID, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register stop_step tool: %v", err))
	}

	// Tool 4: update_step_config — update step_config.json for a specific step
	if err := mcpAgent.RegisterCustomTool(
		"update_step_config",
		"Update step_config.json for a specific step. Changes take effect on the next execute_step call for that step.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json",
				},
				"servers": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "MCP server names to use for this step",
				},
				"tools": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Tool names to enable for this step (format: 'server:tool' or 'server:*')",
				},
				"disable_learning": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, skip the learning phase for this step entirely (no learning agent runs)",
				},
				"lock_learnings": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, lock learnings — prevents learning agent from running but still uses existing learnings. Use this when learnings are stable and don't need updates.",
				},
				"learning_detail_level": map[string]interface{}{
					"type":        "string",
					"description": "Learning detail level: 'exact' or 'none'",
				},
				"enabled_custom_tools": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Workspace/custom tools to enable (format: 'category:tool' or 'category:*'). Categories: workspace_advanced (execute_shell_command, diff_patch_workspace_file, read_image, read_pdf), human_tools (human_feedback), workspace_browser (agent_browser). Example: ['workspace_advanced:execute_shell_command', 'workspace_advanced:diff_patch_workspace_file']",
				},
				"pre_discovered_tools": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Tool names always available in Tool Search mode without calling search_tools. Use raw tool names (e.g., 'read_sheet', 'list_files'), not server:tool format.",
				},
				"disable_knowledgebase": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, disable knowledgebase access for this step (removes knowledgebase read/write paths from folder guard). Useful for steps that don't need persistent storage.",
				},
				"use_code_execution_mode": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, enable code execution mode — the agent writes and executes Python/shell code via mcpbridge to interact with MCP tools, rather than calling them directly. Useful for complex data processing or programmatic control over MCP tools. If false, explicitly disables code execution. Omit to inherit the preset default.",
				},
				"use_tool_search_mode": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, enable tool search mode — the agent dynamically discovers available tools at runtime using search_tools before calling them. Useful when the exact tools needed are not known upfront. If false, tools are provided directly without search. Omit to inherit the preset default.",
				},
				"optimized": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, mark this step as optimized — completion notifications will be simpler (no 'debug and optimize' prompt). Set this when a step is producing consistent, good results.",
				},
				"learning_mode": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"auto", "human_assisted"},
					"description": "Learning mode: 'human_assisted' (default) = skip automatic learning; use generate_learnings(step_id, guidance) to trigger learning manually. 'auto' = learning runs automatically after execution.",
				},
				"execution_llm": map[string]interface{}{
					"type":        "object",
					"description": "Override the execution LLM for this step. Use get_llm_config to see available models.",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{"type": "string", "description": "LLM provider (e.g., 'openai', 'anthropic', 'bedrock', 'openrouter', 'vertex', 'azure')"},
						"model_id": map[string]interface{}{"type": "string", "description": "Model ID (e.g., 'gpt-4o', 'claude-sonnet-4-20250514')"},
					},
				},
				"validation_llm": map[string]interface{}{
					"type":        "object",
					"description": "Override the validation LLM for this step.",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{"type": "string"},
						"model_id": map[string]interface{}{"type": "string"},
					},
				},
				"learning_llm": map[string]interface{}{
					"type":        "object",
					"description": "Override the learning LLM for this step.",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{"type": "string"},
						"model_id": map[string]interface{}{"type": "string"},
					},
				},
				"orchestrator_llm": map[string]interface{}{
					"type":        "object",
					"description": "Override the orchestrator LLM for todo_task/routing steps.",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{"type": "string"},
						"model_id": map[string]interface{}{"type": "string"},
					},
				},
				"sub_agent_llm": map[string]interface{}{
					"type":        "object",
					"description": "Override the LLM for ALL sub-agents spawned by this step (todo_task routes, orchestration routes).",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{"type": "string"},
						"model_id": map[string]interface{}{"type": "string"},
					},
				},
				"validation_schema": map[string]interface{}{
					"type":        "object",
					"description": "Override the pre-validation schema for this step. Takes precedence over plan.json validation_schema. Defines file existence checks and JSON structure validation rules.",
					"properties": map[string]interface{}{
						"files": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"file_name":   map[string]interface{}{"type": "string", "description": "File name to validate (e.g., 'results.json')"},
									"must_exist":  map[string]interface{}{"type": "boolean", "description": "Whether the file must exist"},
									"json_checks": map[string]interface{}{"type": "array", "description": "JSON structure validation checks"},
								},
							},
						},
					},
				},
			},
			"required": []string{"step_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			// Read existing configs
			configs, err := iwm.controller.ReadStepConfigs(ctx)
			if err != nil {
				configs = []StepConfig{}
			}

			// Find or create entry for this step
			var targetConfig *StepConfig
			for i := range configs {
				if configs[i].ID == stepID {
					targetConfig = &configs[i]
					break
				}
			}
			if targetConfig == nil {
				configs = append(configs, StepConfig{ID: stepID})
				targetConfig = &configs[len(configs)-1]
			}
			if targetConfig.AgentConfigs == nil {
				targetConfig.AgentConfigs = &AgentConfigs{}
			}

			// Apply provided fields
			if val, ok := args["servers"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					servers := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							servers = append(servers, s)
						}
					}
					targetConfig.AgentConfigs.SelectedServers = servers
				}
			}
			if val, ok := args["tools"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					tools := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							tools = append(tools, s)
						}
					}
					targetConfig.AgentConfigs.SelectedTools = tools
				}
			}
			if val, ok := args["disable_learning"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					// Protect existing true value from accidental reset: LLMs often
					// include boolean fields as false even when only changing other fields.
					// Only overwrite if setting to true or existing is not already true.
					if b || targetConfig.AgentConfigs.DisableLearning == nil || !*targetConfig.AgentConfigs.DisableLearning {
						targetConfig.AgentConfigs.DisableLearning = &b
					}
				}
			}
			if val, ok := args["lock_learnings"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					// Same protection: don't let accidental false overwrite a true value.
					if b || targetConfig.AgentConfigs.LockLearnings == nil || !*targetConfig.AgentConfigs.LockLearnings {
						targetConfig.AgentConfigs.LockLearnings = &b
					}
				}
			}
			if val, ok := args["learning_detail_level"]; ok && val != nil {
				if s, ok := val.(string); ok && s != "" {
					targetConfig.AgentConfigs.LearningDetailLevel = s
				}
			}
			if val, ok := args["learning_mode"]; ok && val != nil {
				if s, ok := val.(string); ok && s != "" {
					targetConfig.AgentConfigs.LearningMode = s
				}
			}
			if val, ok := args["enabled_custom_tools"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					customTools := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							customTools = append(customTools, s)
						}
					}
					targetConfig.AgentConfigs.EnabledCustomTools = customTools
				}
			}
			if val, ok := args["pre_discovered_tools"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					pdTools := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							pdTools = append(pdTools, s)
						}
					}
					targetConfig.AgentConfigs.PreDiscoveredTools = pdTools
				}
			}
			if val, ok := args["disable_knowledgebase"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					targetConfig.AgentConfigs.DisableKnowledgebase = &b
				}
			}
			if val, ok := args["optimized"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					if b {
						// Validate optimization prerequisites before marking as optimized
						var missing []string

						// 1. Check learnings exist
						learningsPath := getLearningFolderPathByStepID("", stepID, "", iwm.controller.isEvaluationMode)
						learningFiles, _ := iwm.controller.readStepLearningFiles(ctx, learningsPath)
						if len(learningFiles) == 0 {
							missing = append(missing, "learnings (no learning files found — run generate_learnings first)")
						}

						// 2. Check pre-validation schema exists in plan
						if err := iwm.controller.LoadPlanForWorkshop(ctx); err == nil {
							stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID)
							if stepInfo != nil {
								schema := stepInfo.Step.GetValidationSchema()
								if schema == nil || len(schema.Files) == 0 {
									missing = append(missing, "pre-validation schema (no validation_schema defined in plan — add file checks/JSON path rules)")
								}
							}
						}

						if len(missing) > 0 {
							var sb strings.Builder
							sb.WriteString(fmt.Sprintf("Cannot mark step %q as optimized. Missing prerequisites:\n", stepID))
							for i, m := range missing {
								sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, m))
							}
							sb.WriteString("\nFix these issues first, then retry.")
							return sb.String(), nil
						}

						// Optional suggestion: pre-discovered tools (not blocking)
						if len(targetConfig.AgentConfigs.PreDiscoveredTools) == 0 && len(targetConfig.AgentConfigs.SelectedTools) == 0 {
							isToolSearch := targetConfig.AgentConfigs.UseToolSearchMode != nil && *targetConfig.AgentConfigs.UseToolSearchMode
							if isToolSearch {
								iwm.controller.GetLogger().Info(fmt.Sprintf("ℹ️ Step %q marked optimized without pre_discovered_tools — consider adding them for tool search efficiency", stepID))
							}
						}
					}
					targetConfig.AgentConfigs.Optimized = &b
				}
			}
			if val, ok := args["use_code_execution_mode"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					targetConfig.AgentConfigs.UseCodeExecutionMode = &b
				}
			}
			if val, ok := args["use_tool_search_mode"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					targetConfig.AgentConfigs.UseToolSearchMode = &b
				}
			}

			// Parse LLM override fields
			llmFields := []struct {
				key    string
				target **AgentLLMConfig
			}{
				{"execution_llm", &targetConfig.AgentConfigs.ExecutionLLM},
				{"learning_llm", &targetConfig.AgentConfigs.LearningLLM},
				{"orchestrator_llm", &targetConfig.AgentConfigs.OrchestratorLLM},
				{"sub_agent_llm", &targetConfig.AgentConfigs.SubAgentLLM},
			}
			for _, f := range llmFields {
				if val, ok := args[f.key]; ok && val != nil {
					if llmMap, ok := val.(map[string]interface{}); ok {
						provider, _ := llmMap["provider"].(string)
						modelID, _ := llmMap["model_id"].(string)
						if provider != "" && modelID != "" {
							*f.target = &AgentLLMConfig{Provider: provider, ModelID: modelID}
						}
					}
				}
			}

			// Parse validation_schema override
			if val, ok := args["validation_schema"]; ok && val != nil {
				// Marshal back to JSON and unmarshal into ValidationSchema struct
				vsJSON, jsonErr := json.Marshal(val)
				if jsonErr == nil {
					var vs ValidationSchema
					if jsonErr := json.Unmarshal(vsJSON, &vs); jsonErr == nil {
						targetConfig.ValidationSchema = &vs
						logger.Info(fmt.Sprintf("🔧 Step config for %q: validation_schema updated (%d file rules)", stepID, len(vs.Files)))
					} else {
						logger.Warn(fmt.Sprintf("⚠️ Failed to parse validation_schema for step %q: %v", stepID, jsonErr))
					}
				}
			}

			// --- Code-level validations ---
			// Collect errors (block save) and warnings (save but inform).
			var errors []string
			var warnings []string

			// 1. Validate step ID exists in the plan
			if iwm.controller.approvedPlan != nil {
				stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID)
				if stepInfo == nil {
					errors = append(errors, fmt.Sprintf("Step ID %q not found in the current plan. Valid step IDs can be found via list_steps.", stepID))
				}
			}

			// 2. Validate servers exist in workflow-level selection
			workflowServers := iwm.controller.GetSelectedServers()
			workflowServerSet := make(map[string]bool, len(workflowServers))
			for _, s := range workflowServers {
				workflowServerSet[s] = true
			}
			if len(targetConfig.AgentConfigs.SelectedServers) > 0 {
				var badServers []string
				for _, s := range targetConfig.AgentConfigs.SelectedServers {
					if !workflowServerSet[s] {
						badServers = append(badServers, s)
					}
				}
				if len(badServers) > 0 {
					errors = append(errors, fmt.Sprintf("Servers %v are NOT in the workflow-level selection %v and will be IGNORED at execution time. Remove them or add them to the workflow first.", badServers, workflowServers))
				}
			}

			// 3. Validate selected_tools format (should be "server:tool" or "server:*")
			if len(targetConfig.AgentConfigs.SelectedTools) > 0 {
				for _, t := range targetConfig.AgentConfigs.SelectedTools {
					if !strings.Contains(t, ":") {
						errors = append(errors, fmt.Sprintf("Tool %q is missing server prefix. Expected format: 'server:tool_name' or 'server:*'.", t))
					}
				}
			}

			// 4. Validate tools reference servers that are selected
			if len(targetConfig.AgentConfigs.SelectedTools) > 0 && len(targetConfig.AgentConfigs.SelectedServers) > 0 {
				stepServerSet := make(map[string]bool, len(targetConfig.AgentConfigs.SelectedServers))
				for _, s := range targetConfig.AgentConfigs.SelectedServers {
					stepServerSet[s] = true
				}
				for _, t := range targetConfig.AgentConfigs.SelectedTools {
					if idx := strings.Index(t, ":"); idx >= 0 {
						serverPart := t[:idx]
						if !stepServerSet[serverPart] {
							errors = append(errors, fmt.Sprintf("Tool %q references server %q which is not in selected_servers %v. Add the server or remove the tool.", t, serverPart, targetConfig.AgentConfigs.SelectedServers))
						}
					}
				}
			}

			// 5. Validate enabled_custom_tools format and categories
			validCustomCategories := map[string]bool{
				"workspace_advanced": true,
				"human_tools":       true,
				"workspace_browser": true,
				"workspace_git":     true,
			}
			if len(targetConfig.AgentConfigs.EnabledCustomTools) > 0 {
				for _, t := range targetConfig.AgentConfigs.EnabledCustomTools {
					if idx := strings.Index(t, ":"); idx >= 0 {
						cat := t[:idx]
						if !validCustomCategories[cat] {
							errors = append(errors, fmt.Sprintf("Custom tool %q uses unknown category %q. Valid categories: workspace_advanced, human_tools, workspace_browser, workspace_git.", t, cat))
						}
					} else {
						errors = append(errors, fmt.Sprintf("Custom tool %q is missing category prefix. Expected format: 'category:tool_name' or 'category:*'.", t))
					}
				}

				// 5b. Ensure required workspace tools are present (execute_shell_command, diff_patch_workspace_file)
				existingSet := make(map[string]bool, len(targetConfig.AgentConfigs.EnabledCustomTools))
				for _, t := range targetConfig.AgentConfigs.EnabledCustomTools {
					existingSet[t] = true
				}
				if !existingSet["workspace_advanced:*"] {
					required := map[string]string{
						"workspace_advanced:execute_shell_command":     "execute_shell_command",
						"workspace_advanced:diff_patch_workspace_file": "diff_patch_workspace_file",
					}
					var missing []string
					for key, name := range required {
						if !existingSet[key] {
							missing = append(missing, name)
						}
					}
					if len(missing) > 0 {
						errors = append(errors, fmt.Sprintf("Required workspace tools missing from enabled_custom_tools: %v. These are essential for every step (file operations, script execution). Add them as 'workspace_advanced:<tool_name>' or use 'workspace_advanced:*' to include all.", missing))
					}
				}
			}

			// 6. Validate learning config consistency
			isDisabled := targetConfig.AgentConfigs.DisableLearning != nil && *targetConfig.AgentConfigs.DisableLearning
			isLocked := targetConfig.AgentConfigs.LockLearnings != nil && *targetConfig.AgentConfigs.LockLearnings
			if isDisabled && isLocked {
				warnings = append(warnings, "Both disable_learning and lock_learnings are true. disable_learning takes precedence — learning agent won't run and existing learnings won't be used. If you want to keep using existing learnings without updating them, set disable_learning=false and lock_learnings=true instead.")
			}

			// 7. Validate learning_detail_level
			if targetConfig.AgentConfigs.LearningDetailLevel != "" {
				validLevels := map[string]bool{"exact": true, "none": true}
				if !validLevels[targetConfig.AgentConfigs.LearningDetailLevel] {
					errors = append(errors, fmt.Sprintf("learning_detail_level %q is not recognized. Valid values: 'exact', 'none'.", targetConfig.AgentConfigs.LearningDetailLevel))
				}
			}

			// 8. Validate learning_mode
			if targetConfig.AgentConfigs.LearningMode != "" {
				validModes := map[string]bool{"auto": true, "human_assisted": true}
				if !validModes[targetConfig.AgentConfigs.LearningMode] {
					errors = append(errors, fmt.Sprintf("learning_mode %q is not recognized. Valid values: 'auto', 'human_assisted'.", targetConfig.AgentConfigs.LearningMode))
				}
			}

			// If there are errors, reject the update and return feedback
			if len(errors) > 0 {
				result := fmt.Sprintf("❌ Step config for %q was NOT saved due to validation errors:\n", stepID)
				for i, e := range errors {
					result += fmt.Sprintf("\n%d. %s", i+1, e)
				}
				result += "\n\nFix the errors above and try again."
				if len(warnings) > 0 {
					result += "\n\nAlso note these warnings:"
					for i, w := range warnings {
						result += fmt.Sprintf("\n%d. %s", i+1, w)
					}
				}
				return result, nil
			}

			// Write updated configs back
			if err := iwm.controller.WriteStepConfigs(ctx, configs); err != nil {
				return fmt.Sprintf("Failed to update step config: %v", err), nil
			}

			logger.Info(fmt.Sprintf("📝 Workshop: step config updated for step %q", stepID))
			result := fmt.Sprintf("Step config for %q updated successfully. Changes will take effect on the next execute_step call.", stepID)
			if len(warnings) > 0 {
				result += "\n\n⚠️ WARNINGS:"
				for i, w := range warnings {
					result += fmt.Sprintf("\n%d. %s", i+1, w)
				}
			}
			return result, nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_step_config tool: %v", err))
	}

	// Tool 5: get_step_prompts — read saved system prompt + user message for a step run
	if err := mcpAgent.RegisterCustomTool(
		"get_step_prompts",
		"Get the system prompt and user message for a step. Works both during execution (prompts saved at start) and after completion. Useful for debugging what instructions the agent received. For sub-agent steps, pass the inner step ID directly (e.g., 'step-icici-login') or use route_id with the parent step.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or inner step ID (e.g., 'step-icici-login')",
				},
				"route_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional route ID for sub-agent steps (e.g., 'icici-login'). When provided with a parent step_id, looks up logs at step-{N}-sub-{route_id}/",
				},
				"attempt": map[string]interface{}{
					"type":        "integer",
					"description": "Retry attempt number (default: 1)",
				},
				"iteration": map[string]interface{}{
					"type":        "integer",
					"description": "Loop iteration number (default: 0)",
				},
			},
			"required": []string{"step_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			routeID, _ := args["route_id"].(string)

			// Ensure plan is loaded (best-effort — for get_step_prompts we only need step index)
			if iwm.controller.approvedPlan == nil {
				_ = iwm.controller.LoadPlanForWorkshop(ctx) // ignore error; nil check below handles failure
			}
			if iwm.controller.approvedPlan == nil {
				return "no plan loaded; run execute_step first or ensure plan.json exists", nil
			}

			// Resolve flexible step_id
			resolvedForPrompts, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}

			// Find step info (handles both top-level and inner steps)
			stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, resolvedForPrompts)
			if stepInfo == nil {
				return fmt.Sprintf("step %q not found in plan", stepID), nil
			}

			attempt := 1
			if v, ok := args["attempt"]; ok && v != nil {
				if f, ok := v.(float64); ok {
					attempt = int(f)
				}
			}
			iteration := 0
			if v, ok := args["iteration"]; ok && v != nil {
				if f, ok := v.(float64); ok {
					iteration = int(f)
				}
			}

			runFolder := iwm.controller.selectedRunFolder
			if runFolder == "" {
				return "no run folder selected", nil
			}

			// Determine the correct log path based on step type
			var stepPath string
			if stepInfo.TopIndex > 0 {
				// Top-level step
				if routeID != "" {
					// User wants sub-agent logs under this top-level step
					stepPath = fmt.Sprintf("step-%d-sub-%s", stepInfo.TopIndex, routeID)
				} else {
					stepPath = fmt.Sprintf("step-%d", stepInfo.TopIndex)
				}
			} else {
				// Inner step — resolve to the correct log path (e.g., step-3-sub-icici-login)
				stepPath = resolveInnerStepPath(iwm.controller.approvedPlan.Steps, stepInfo)
			}
			logDir := fmt.Sprintf("runs/%s/logs/%s/execution", runFolder, stepPath)
			filenameBase := fmt.Sprintf("execution-attempt-%d-iteration-%d", attempt, iteration)

			var result strings.Builder
			hasUserMessage := false // Track if user message was already included from prompts.json

			// Read system prompt and user message from prompts.json (saved pre-execution and updated post-execution)
			// Try execution-step prompts first, then other agent type prompts
			promptsPath := fmt.Sprintf("%s/%s-prompts.json", logDir, filenameBase)
			promptsContent, err := iwm.controller.ReadWorkspaceFile(ctx, promptsPath)
			if err != nil {
				// Try other prompt file types (todo_task, conditional, decision, routing)
				for _, altName := range []string{"todo-task-prompts.json", "conditional-prompts.json", "decision-prompts.json", "routing-prompts.json"} {
					altPath := fmt.Sprintf("%s/%s", logDir, altName)
					if tc, te := iwm.controller.ReadWorkspaceFile(ctx, altPath); te == nil {
						promptsContent = tc
						err = nil
						promptsPath = altPath
						break
					}
				}
			}
			if err != nil {
				result.WriteString(fmt.Sprintf("⚠️ Prompts file not found (%s).\nNote: only available for runs after this feature was added.\n\n", promptsPath))
			} else {
				var promptsData map[string]interface{}
				if jsonErr := workshopJSONUnmarshal([]byte(promptsContent), &promptsData); jsonErr == nil {
					// Show when the prompt was saved (pre_execution = still running, post_execution = completed)
					if savedAt, ok := promptsData["saved_at"].(string); ok {
						result.WriteString(fmt.Sprintf("**Prompt saved at**: %s\n", savedAt))
					}
					if model, ok := promptsData["model"].(string); ok && model != "" {
						result.WriteString(fmt.Sprintf("**Model**: %s\n\n", model))
					}
					result.WriteString("## System Prompt\n\n")
					if sp, ok := promptsData["system_prompt"].(string); ok {
						result.WriteString(sp)
					} else {
						result.WriteString("(system_prompt field missing)")
					}
					result.WriteString("\n\n")
					// User message from prompts.json (available from pre-execution save)
					if um, ok := promptsData["user_message"].(string); ok && um != "" {
						result.WriteString("## User Message\n\n")
						result.WriteString(um)
						result.WriteString("\n\n")
						hasUserMessage = true
					}
				} else {
					result.WriteString("## System Prompt\n\n")
					result.WriteString(promptsContent)
					result.WriteString("\n\n")
				}
			}

			// Read user message from conversation.json (first human message in history)
			// Skip if user message was already included from prompts.json
			if hasUserMessage {
				return result.String(), nil
			}
			convPath := fmt.Sprintf("%s/%s-conversation.json", logDir, filenameBase)
			convContent, err := iwm.controller.ReadWorkspaceFile(ctx, convPath)
			if err != nil {
				result.WriteString(fmt.Sprintf("⚠️ Conversation file not found (%s): %v\n", convPath, err))
			} else {
				result.WriteString("## User Message (first turn)\n\n")
				var convData map[string]interface{}
				if jsonErr := workshopJSONUnmarshal([]byte(convContent), &convData); jsonErr == nil {
					if history, ok := convData["conversation_history"].([]interface{}); ok && len(history) > 0 {
						// Find the first human message (skip system message at index 0).
						// Role field is PascalCase ("Role") since MessageContent has no JSON tags.
						var humanMsg map[string]interface{}
						for _, msg := range history {
							m, ok := msg.(map[string]interface{})
							if !ok {
								continue
							}
							role, _ := m["Role"].(string)
							if role == "" {
								role, _ = m["role"].(string)
							}
							if role == "human" || role == "Human" || role == "user" {
								humanMsg = m
								break
							}
						}
						if humanMsg != nil {
							parts, ok := humanMsg["Parts"].([]interface{})
							if !ok {
								parts, ok = humanMsg["parts"].([]interface{})
							}
							if ok {
								for _, part := range parts {
									if partMap, ok := part.(map[string]interface{}); ok {
										text, found := partMap["Text"].(string)
										if !found {
											text, found = partMap["text"].(string)
										}
										if found {
											result.WriteString(text)
											break
										}
									}
								}
							}
						} else {
							result.WriteString("(no human message found in conversation history)")
						}
					} else {
						result.WriteString("(conversation history empty or unparseable)")
					}
				} else {
					result.WriteString(fmt.Sprintf("(could not parse conversation JSON: %v)", jsonErr))
				}
			}

			return result.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_step_prompts tool: %v", err))
	}

	// Tool 6: analyze_step — analyze a step's config and suggest optimizations
	if err := mcpAgent.RegisterCustomTool(
		"analyze_step",
		"Analyze a step's configuration and execution history, then suggest optimizations. Checks: (1) validation schema presence, (2) learning config efficiency, (3) tool/server usage vs configured. Call after executing a step to get actionable suggestions.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or positional reference (e.g., '1', 'step-1')",
				},
			},
			"required": []string{"step_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			// Ensure plan is loaded
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v", err), nil
			}
			resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}

			// Find the step in the plan (including inner steps)
			stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, resolvedID)
			if stepInfo == nil {
				return fmt.Sprintf("step %q not found in plan", stepID), nil
			}
			targetStep := stepInfo.Step
			stepNum := stepInfo.TopIndex // -1 for inner steps

			// Read step config
			stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
			var stepCfg *AgentConfigs
			for _, sc := range stepConfigs {
				if sc.ID == resolvedID {
					stepCfg = sc.AgentConfigs
					break
				}
			}

			var result strings.Builder
			if stepNum > 0 {
				result.WriteString(fmt.Sprintf("## Analysis for Step %d: %s\n\n", stepNum, targetStep.GetTitle()))
			} else {
				result.WriteString(fmt.Sprintf("## Analysis for Inner Step: %s\n   (parent: `%s`, branch: %s)\n\n", targetStep.GetTitle(), stepInfo.ParentID, stepInfo.BranchName))
			}
			suggestions := 0

			// === 1. Validation Schema Check ===
			result.WriteString("### Pre-Validation Schema\n")
			schema := targetStep.GetValidationSchema()
			if schema == nil || len(schema.Files) == 0 {
				suggestions++
				result.WriteString("⚠️ **No validation schema defined.** Adding a pre-validation JSON schema ensures the step's output is verified automatically before marking it complete. This catches structural errors without needing an LLM validation pass.\n")
				result.WriteString("   → Use plan modification tools to add a `validation_schema` with file checks and JSON path rules.\n\n")
			} else {
				totalChecks := 0
				for _, f := range schema.Files {
					totalChecks += len(f.JSONChecks)
				}
				result.WriteString(fmt.Sprintf("✅ Schema defined: %d file(s), %d JSON check(s)\n\n", len(schema.Files), totalChecks))
			}

			// === 2. Learning Config Check ===
			result.WriteString("### Learning Configuration\n")
			descLen := len(targetStep.GetDescription())
			isSimpleStep := descLen < 200 && targetStep.StepType() == StepTypeRegular

			disableLearning := false
			lockLearnings := false
			if stepCfg != nil {
				if stepCfg.DisableLearning != nil && *stepCfg.DisableLearning {
					disableLearning = true
				}
				if stepCfg.LockLearnings != nil && *stepCfg.LockLearnings {
					lockLearnings = true
				}
			}

			if disableLearning {
				result.WriteString("✅ Learning is disabled for this step.\n\n")
			} else if lockLearnings {
				result.WriteString("✅ Learnings are locked (using existing, not generating new).\n\n")
			} else if isSimpleStep {
				suggestions++
				result.WriteString("⚠️ **Step looks simple** (short description, regular type). Consider:\n")
				result.WriteString("   → `disable_learning: true` if this step doesn't benefit from accumulated knowledge\n")
				result.WriteString("   → `lock_learnings: true` after a few successful runs to freeze learnings and skip the learning agent\n\n")
			} else {
				result.WriteString("ℹ️ Learning is enabled. After successful runs, consider `lock_learnings: true` to freeze learnings and save execution time.\n\n")
			}

			// === 3. Tool/Server Usage Analysis ===
			// === 3. Execution Mode Check ===
			result.WriteString("### Execution Mode\n")
			isCodeExec := false
			isToolSearch := false
			preDiscoveredTools := []string{}
			if stepCfg != nil {
				if stepCfg.UseCodeExecutionMode != nil && *stepCfg.UseCodeExecutionMode {
					isCodeExec = true
				}
				if stepCfg.UseToolSearchMode != nil && *stepCfg.UseToolSearchMode {
					isToolSearch = true
				}
				preDiscoveredTools = stepCfg.PreDiscoveredTools
			}
			// Check orchestrator defaults if step doesn't override
			if !isCodeExec && (stepCfg == nil || stepCfg.UseCodeExecutionMode == nil) {
				isCodeExec = iwm.controller.GetUseCodeExecutionMode()
			}
			// Only inherit preset tool search if code exec is NOT enabled
			// (they are mutually exclusive — don't show both)
			if !isToolSearch && (stepCfg == nil || stepCfg.UseToolSearchMode == nil) && !isCodeExec {
				isToolSearch = iwm.controller.GetUseToolSearchMode()
			}

			if isCodeExec {
				result.WriteString("Mode: **Code Execution** (CLI-based, tools via code)\n")
				result.WriteString("   ℹ️ In code execution mode, tool optimization applies differently — the agent calls tools via generated code rather than direct tool calls.\n")
				if isToolSearch {
					result.WriteString("   Tool Search: enabled")
					if len(preDiscoveredTools) > 0 {
						result.WriteString(fmt.Sprintf(" (pre-discovered: %v)", preDiscoveredTools))
					}
					result.WriteString("\n")
				}
			} else if isToolSearch {
				result.WriteString("Mode: **Tool Search** (dynamic tool discovery)\n")
				if len(preDiscoveredTools) > 0 {
					result.WriteString(fmt.Sprintf("   Pre-discovered tools (always available): %v\n", preDiscoveredTools))
				} else {
					suggestions++
					result.WriteString("   ⚠️ No `pre_discovered_tools` set. After successful runs, extract frequently used tool names from logs and set them as pre-discovered to skip search overhead.\n")
				}
			} else {
				result.WriteString("Mode: **Simple** (all configured tools loaded upfront)\n")
				result.WriteString("   ℹ️ Consider converting to Tool Search Mode after successful runs — it loads only needed tools, reducing context size.\n")
			}
			result.WriteString("\n")

			result.WriteString("### Tool & Server Configuration\n")

			// Get configured servers/tools/custom tools
			configuredServers := []string{}
			configuredTools := []string{}
			configuredCustomTools := []string{}
			if stepCfg != nil {
				configuredServers = stepCfg.SelectedServers
				configuredTools = stepCfg.SelectedTools
				configuredCustomTools = stepCfg.EnabledCustomTools
			}

			// Try to extract actual tool usage from logs
			runFolder := iwm.controller.selectedRunFolder
			if runFolder != "" {
				// For inner steps executed standalone, logs are at step-1 (single-step plan).
				// For top-level steps, logs are at step-{N}.
				logsStepNum := stepNum
				if logsStepNum <= 0 {
					logsStepNum = 1 // inner steps executed standalone use index 1
				}
				// Use relative path — ReadWorkspaceFile auto-prepends workspacePath
				relativeLogsPath := fmt.Sprintf("runs/%s/logs/step-%d/execution", runFolder, logsStepNum)
				toolUsageMap := make(map[string]*ToolUsageEntry)
				dummySummary := &StepToolUsageSummary{}
				extractToolsFromLogsPath(ctx, relativeLogsPath, toolUsageMap, iwm.controller.ReadWorkspaceFile, logger, dummySummary)

				if len(toolUsageMap) > 0 {
					// Categorize tools
					usedMCPServerTools := make(map[string][]string) // server → [tool1, tool2]
					usedWorkspaceTools := make(map[string]bool)
					usedMCPToolNames := []string{} // raw tool names for pre-discovered suggestion
					for name := range toolUsageMap {
						if strings.HasPrefix(name, "mcp__") {
							// Code execution mode: mcp__server__tool format
							withoutPrefix := strings.TrimPrefix(name, "mcp__")
							parts := strings.SplitN(withoutPrefix, "__", 2)
							if len(parts) == 2 {
								usedMCPServerTools[parts[0]] = append(usedMCPServerTools[parts[0]], parts[1])
								usedMCPToolNames = append(usedMCPToolNames, parts[1])
							}
						} else if knownWorkspaceToolNames[name] {
							usedWorkspaceTools[name] = true
						} else {
							// Regular mode: raw MCP tool name (no server prefix)
							usedMCPToolNames = append(usedMCPToolNames, name)
						}
					}

					// Report actually used tools
					result.WriteString("**Tools actually used in last run:**\n")
					if len(usedMCPServerTools) > 0 {
						for server, tools := range usedMCPServerTools {
							toolStrs := make([]string, len(tools))
							for i, t := range tools {
								toolStrs[i] = fmt.Sprintf("%s:%s", server, t)
							}
							result.WriteString(fmt.Sprintf("   MCP [%s]: %s\n", server, strings.Join(toolStrs, ", ")))
						}
					} else if len(usedMCPToolNames) > 0 {
						result.WriteString(fmt.Sprintf("   MCP tools: %s\n", strings.Join(usedMCPToolNames, ", ")))
					}
					if len(usedWorkspaceTools) > 0 {
						wsList := make([]string, 0, len(usedWorkspaceTools))
						for t := range usedWorkspaceTools {
							wsList = append(wsList, t)
						}
						result.WriteString(fmt.Sprintf("   Workspace/custom: %s\n", strings.Join(wsList, ", ")))
					}

					// === Pre-discovered Tools Suggestion (Tool Search mode only) ===
					if isToolSearch && len(preDiscoveredTools) == 0 && len(usedMCPToolNames) > 0 {
						suggestions++
						result.WriteString(fmt.Sprintf("\n**Pre-discovered Tools (tool search optimization):**\n"))
						result.WriteString("⚠️ Tool Search mode is active but no `pre_discovered_tools` set.\n")
						result.WriteString(fmt.Sprintf("   Based on usage, suggest: `pre_discovered_tools: %v`\n", usedMCPToolNames))
						result.WriteString("   This makes these tools immediately available without calling `search_tools`.\n")
					}

					// === MCP Server/Tool Suggestions ===
					result.WriteString("\n**MCP Servers & Tools:**\n")
					if len(configuredServers) == 0 && len(configuredTools) == 0 {
						if len(usedMCPServerTools) > 0 {
							suggestions++
							suggestedServers := make([]string, 0, len(usedMCPServerTools))
							suggestedTools := []string{}
							for server, tools := range usedMCPServerTools {
								suggestedServers = append(suggestedServers, server)
								for _, t := range tools {
									suggestedTools = append(suggestedTools, fmt.Sprintf("%s:%s", server, t))
								}
							}
							result.WriteString("⚠️ No step-level MCP filter. Suggested config:\n")
							result.WriteString(fmt.Sprintf("   → `servers: %v`\n", suggestedServers))
							result.WriteString(fmt.Sprintf("   → `tools: %v`\n", suggestedTools))
							result.WriteString("   Or `server:*` for all tools from a server\n")
						} else if len(usedMCPToolNames) > 0 {
							result.WriteString(fmt.Sprintf("ℹ️ MCP tools used: %v — set `servers` to scope which MCP servers are available.\n", usedMCPToolNames))
						} else {
							result.WriteString("✅ Step uses no MCP tools. Consider setting `servers: [\"NO_SERVERS\"]` to explicitly disable MCP.\n")
						}
					} else {
						result.WriteString(fmt.Sprintf("✅ servers=%v, tools=%v\n", configuredServers, configuredTools))
						// Check for missing tools
						if len(configuredTools) > 0 {
							configuredSet := make(map[string]bool)
							for _, t := range configuredTools {
								configuredSet[t] = true
							}
							for server, tools := range usedMCPServerTools {
								for _, t := range tools {
									toolKey := fmt.Sprintf("%s:%s", server, t)
									wildcardKey := fmt.Sprintf("%s:*", server)
									if !configuredSet[toolKey] && !configuredSet[wildcardKey] {
										suggestions++
										result.WriteString(fmt.Sprintf("   ⚠️ Tool `%s` was used but not in configured list\n", toolKey))
									}
								}
							}
						}
					}

					// === Workspace Custom Tool Suggestions ===
					result.WriteString("\n**Workspace Custom Tools (enabled_custom_tools):**\n")
					if len(configuredCustomTools) == 0 {
						suggestedCustom := []string{}
						needsHumanTools := usedWorkspaceTools["human_feedback"]
						needsReadImage := usedWorkspaceTools["read_image"]
						needsReadPDF := usedWorkspaceTools["read_pdf"]
						needsDiffPatch := usedWorkspaceTools["diff_patch_workspace_file"]

						suggestedCustom = append(suggestedCustom, "workspace_advanced:execute_shell_command")
						if needsDiffPatch {
							suggestedCustom = append(suggestedCustom, "workspace_advanced:diff_patch_workspace_file")
						}
						if needsReadImage {
							suggestedCustom = append(suggestedCustom, "workspace_advanced:read_image")
						}
						if needsReadPDF {
							suggestedCustom = append(suggestedCustom, "workspace_advanced:read_pdf")
						}
						if needsHumanTools {
							suggestedCustom = append(suggestedCustom, "human_tools:*")
						}

						if !needsHumanTools || !needsReadImage || !needsReadPDF {
							suggestions++
							result.WriteString("⚠️ Default config includes all workspace_advanced + human_tools. Based on usage:\n")
							if !needsHumanTools {
								result.WriteString("   → `human_feedback` not used — can remove `human_tools:*`\n")
							}
							if !needsReadImage {
								result.WriteString("   → `read_image` not used — can exclude\n")
							}
							if !needsReadPDF {
								result.WriteString("   → `read_pdf` not used — can exclude\n")
							}
							if !needsDiffPatch {
								result.WriteString("   → `diff_patch_workspace_file` not used — can exclude\n")
							}
							result.WriteString(fmt.Sprintf("   Suggested: `enabled_custom_tools: %v`\n", suggestedCustom))
						} else {
							result.WriteString("✅ All workspace_advanced tools and human_tools are being used.\n")
						}
					} else {
						result.WriteString(fmt.Sprintf("✅ Custom tools configured: %v\n", configuredCustomTools))
					}
				} else {
					result.WriteString("ℹ️ No execution logs found yet — run the step first for usage-based suggestions.\n\n")
				}

				// Always show current config status and static suggestions (even without logs)
				if len(toolUsageMap) == 0 {
					// MCP config status
					result.WriteString("**MCP Servers & Tools (current config):**\n")
					if len(configuredServers) == 0 && len(configuredTools) == 0 {
						suggestions++
						result.WriteString("⚠️ No step-level MCP filter — step uses all preset servers. Run the step and re-analyze to see which are actually needed.\n")
					} else {
						result.WriteString(fmt.Sprintf("✅ servers=%v, tools=%v\n", configuredServers, configuredTools))
					}

					// Workspace custom tools status
					result.WriteString("\n**Workspace Custom Tools (current config):**\n")
					if len(configuredCustomTools) == 0 {
						suggestions++
						result.WriteString("⚠️ No `enabled_custom_tools` set — default includes **all** workspace_advanced + human_tools:\n")
						result.WriteString("   - `workspace_advanced:*` → execute_shell_command, diff_patch_workspace_file, read_image, read_pdf\n")
						result.WriteString("   - `human_tools:*` → human_feedback\n")
						result.WriteString("   Consider: does this step need `read_image`? `read_pdf`? `human_feedback`?\n")
						result.WriteString("   If not, set `enabled_custom_tools` to only what's needed, e.g.:\n")
						result.WriteString("   `[\"workspace_advanced:execute_shell_command\", \"workspace_advanced:diff_patch_workspace_file\"]`\n")
					} else {
						result.WriteString(fmt.Sprintf("✅ Custom tools configured: %v\n", configuredCustomTools))
					}
				}
			} else {
				result.WriteString("ℹ️ No run folder selected — cannot analyze tool usage.\n")
			}

			// === Optimization Readiness Note ===
			result.WriteString("\n### Optimization Readiness\n")
			// Check if step has learnings by looking for learnings folder content
			hasLearnings := false
			learningsPath := ""
			if stepNum > 0 {
				learningsPath = fmt.Sprintf("runs/%s/logs/step-%d/learnings", runFolder, stepNum)
			} else {
				learningsPath = fmt.Sprintf("runs/%s/logs/step-1/learnings", runFolder)
			}
			if runFolder != "" {
				if learningsContent, err := iwm.controller.ReadWorkspaceFile(ctx, learningsPath+"/step_learnings.json"); err == nil && len(learningsContent) > 10 {
					hasLearnings = true
				}
			}

			if hasLearnings {
				result.WriteString("ℹ️ Step has accumulated learnings from previous runs. Optimization suggestions above are based on observed behavior and can be applied if you're satisfied with the step's output quality.\n")
			} else {
				result.WriteString("⚠️ Step has no accumulated learnings yet. The suggestions above are initial guidance — consider running the step a few more times before applying optimizations like lock_learnings, server scoping, or tool filtering. Premature optimization may degrade output quality.\n")
			}
			result.WriteString("**Note**: These are suggestions, not requirements. Apply them when you're confident the step is producing good results consistently.\n")

			result.WriteString(fmt.Sprintf("\n---\n**%d suggestion(s)** found.\n", suggestions))
			return result.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register analyze_step tool: %v", err))
	}

	// Tool 7: generate_learnings — background learning agent with optional human guidance
	if err := mcpAgent.RegisterCustomTool(
		"generate_learnings",
		"Start the learning agent in the background for a step. Returns execution_id immediately — you will be automatically notified when it completes. Optionally provide human guidance to focus the learning agent on specific aspects. Works for both top-level and inner steps (conditional branches, sub-agents). If you ran the test yourself (not via execute_step), pass your recent tool calls as execution_history — the learning agent will use that directly instead of reading execution logs.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or positional reference (e.g., '1', 'step-1')",
				},
				"guidance": map[string]interface{}{
					"type":        "string",
					"description": "Optional human guidance for what the learning agent should focus on. E.g., 'focus on the API pagination pattern' or 'capture the retry logic for rate limits'. Appended to step description for the learning agent.",
				},
				"execution_history": map[string]interface{}{
					"type":        "string",
					"description": "Optional execution history from your own tool calls. Pass this when you ran the test yourself (not via execute_step). Format: describe the tool calls you made, their arguments, and results. The learning agent will use this directly instead of reading execution log files.",
				},
			},
			"required": []string{"step_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			guidance := ""
			if val, ok := args["guidance"]; ok && val != nil {
				if s, ok := val.(string); ok {
					guidance = s
				}
			}

			passedExecutionHistory := ""
			if val, ok := args["execution_history"]; ok && val != nil {
				if s, ok := val.(string); ok {
					passedExecutionHistory = s
				}
			}

			// Ensure plan is loaded
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v", err), nil
			}
			resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}

			// Find step in plan
			stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, resolvedID)
			if stepInfo == nil {
				return fmt.Sprintf("step %q not found in plan", stepID), nil
			}
			targetStep := stepInfo.Step
			stepNum := stepInfo.TopIndex

			runFolder := iwm.controller.selectedRunFolder
			if runFolder == "" {
				return "no run folder selected", nil
			}

			// Determine log path and stepPath based on whether this is an inner or top-level step.
			// Inner steps use resolveInnerStepPath to get the correct log folder (e.g., step-3-sub-icici-login).
			// Top-level steps use step-{N}/ based on their position.
			var stepPath string
			var parentStepIndex int // 0-based, for token tracking attribution
			if stepNum >= 1 {
				// Top-level step
				stepPath = fmt.Sprintf("step-%d", stepNum)
				parentStepIndex = stepNum - 1
			} else {
				// Inner step — resolve to the correct log path (e.g., step-3-sub-icici-login)
				stepPath = resolveInnerStepPath(iwm.controller.approvedPlan.Steps, stepInfo)
				// Find parent's 0-based index for token attribution
				parentStepIndex = 0
				if stepInfo.ParentID != "" {
					for i, s := range iwm.controller.approvedPlan.Steps {
						if s.GetID() == stepInfo.ParentID {
							parentStepIndex = i
							break
						}
					}
				}
			}

			// Read step config for learning agent settings
			stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
			var agentConfigs *AgentConfigs
			for _, sc := range stepConfigs {
				if sc.ID == resolvedID {
					agentConfigs = sc.AgentConfigs
					break
				}
			}

			// Determine code execution mode — use already-extracted agentConfigs to avoid redundant scan
			isCodeExecMode := isCodeExecutionModeEnabled(agentConfigs, iwm.controller.GetUseCodeExecutionMode())

			// Read existing learnings — use the step's own ID for the learning folder
			learningsPath := getLearningFolderPathByStepID("", resolvedID, "", iwm.controller.isEvaluationMode)
			_ = iwm.controller.ensureStepLearningsFolderExists(ctx, learningsPath)
			existingLearningFiles, _ := iwm.controller.readStepLearningFiles(ctx, learningsPath)
			existingLearningsContent := ""
			if len(existingLearningFiles) > 0 {
				existingLearningsContent, _ = iwm.controller.formatStepLearningFilesAsHistory(existingLearningFiles)
			}

			// learningPathIdentifier = step's own ID (used as learning folder name)
			learningPathIdentifier := resolvedID
			resolvedTitle := ResolveVariables(targetStep.GetTitle(), iwm.controller.variableValues)
			sanitizedTitle := iwm.controller.sanitizeTitleForAgentName(resolvedTitle)
			agentName := fmt.Sprintf("%s-workshop-learning-%s", learningPathIdentifier, sanitizedTitle)

			// Get execution history — either from passed parameter or from execution log files
			var formattedHistory string
			if passedExecutionHistory != "" {
				// Main agent passed its own tool call history directly — use it as-is
				formattedHistory = passedExecutionHistory
				logger.Info(fmt.Sprintf("🧠 generate_learnings: using passed execution_history for step %q (len=%d)", resolvedID, len(passedExecutionHistory)))
			} else {
				// Read from execution log files (written by execute_step)
				logDir := fmt.Sprintf("runs/%s/logs/%s/execution", runFolder, stepPath)
				var executionHistory []llmtypes.MessageContent
				var foundConvPath string
				for attempt := 5; attempt >= 1; attempt-- {
					for iteration := 5; iteration >= 0; iteration-- {
						convPath := fmt.Sprintf("%s/execution-attempt-%d-iteration-%d-conversation.json", logDir, attempt, iteration)
						content, err := iwm.controller.ReadWorkspaceFile(ctx, convPath)
						if err != nil {
							continue
						}

						var convData map[string]interface{}
						if err := json.Unmarshal([]byte(content), &convData); err != nil {
							continue
						}

						convHistoryRaw, ok := convData["conversation_history"]
						if !ok {
							continue
						}

						convHistoryJSON, err := json.Marshal(convHistoryRaw)
						if err != nil {
							continue
						}

						if err := json.Unmarshal(convHistoryJSON, &executionHistory); err != nil {
							continue
						}

						foundConvPath = convPath
						break
					}
					if foundConvPath != "" {
						break
					}
				}

				if foundConvPath == "" {
					return fmt.Sprintf("No execution history found for step %q in %s. Either run execute_step first, or pass execution_history if you ran the test yourself.", stepID, logDir), nil
				}

				// Format execution history (aggressive truncation for cost)
				formattedHistory = shared.FormatHistoryForLearningAggressive(executionHistory)
			}

			// Build step description — inject human guidance if provided
			stepDescription := targetStep.GetDescription()
			if guidance != "" {
				stepDescription = fmt.Sprintf("%s\n\n## Human Guidance for Learning\n%s", stepDescription, guidance)
			}

			// Read validation result from the step log folder (not execution/ subfolder)
			// Validation files: validation.json (first), validation-2.json, validation-3.json, etc.
			validationResult := "No validation result available"
			validationLogDir := fmt.Sprintf("runs/%s/logs/%s", runFolder, stepPath)
			foundValidation := false
			// Try numbered validations in reverse to find the latest, then fall back to base
			for i := 5; i >= 2; i-- {
				vPath := fmt.Sprintf("%s/validation-%d.json", validationLogDir, i)
				if content, err := iwm.controller.ReadWorkspaceFile(ctx, vPath); err == nil {
					validationResult = content
					foundValidation = true
					break
				}
			}
			if !foundValidation {
				if content, err := iwm.controller.ReadWorkspaceFile(ctx, fmt.Sprintf("%s/validation.json", validationLogDir)); err == nil {
					validationResult = content
				}
			}

			// Prepare template variables
			runWorkspacePath := fmt.Sprintf("%s/runs/%s", iwm.controller.GetWorkspacePath(), runFolder)
			executionLogsPath := fmt.Sprintf("%s/logs/%s/execution", runWorkspacePath, stepPath)

			templateVars := map[string]string{
				"StepTitle":                targetStep.GetTitle(),
				"StepDescription":          stepDescription,
				"StepSuccessCriteria":      targetStep.GetSuccessCriteria(),
				"StepContextOutput":        targetStep.GetContextOutput().String(),
				"WorkspacePath":            iwm.controller.GetWorkspacePath(),
				"ExecutionHistory":         formattedHistory,
				"ValidationResult":         validationResult,
				"CurrentObjective":         iwm.controller.GetObjective(),
				"LearningDetailLevel":      "exact",
				"StepExecutionPath":        runWorkspacePath,
				"StepNumber":               learningPathIdentifier,
				"ExecutionLogsPath":        executionLogsPath,
				"ExistingLearningsContent": existingLearningsContent,
			}

			// Add context dependencies
			contextDeps := targetStep.GetContextDependencies()
			if len(contextDeps) > 0 {
				templateVars["StepContextDependencies"] = strings.Join(contextDeps, ", ")
			} else {
				templateVars["StepContextDependencies"] = ""
			}

			// Add variable names if available
			if variableNames := FormatVariableNames(iwm.controller.variablesManifest); variableNames != "" {
				templateVars["VariableNames"] = variableNames
			}

			// Launch learning agent in background (same pattern as execute_step / optimize_step)
			execID := fmt.Sprintf("learn-%s-%05d", resolvedID, time.Now().UnixNano()%100000)
			execCtx, cancel := context.WithCancel(iwm.sessionCtx)

			// Inject correlation IDs for sub-agent event tagging
			agentSessionID := fmt.Sprintf("workshop-learning-%s-%d", resolvedID, time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         resolvedID,
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			go func() {
				// Emit orchestrator_agent_start so the frontend creates a grouping card
				eventBridge := iwm.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-learning",
						AgentName:     fmt.Sprintf("Learning: %s", resolvedID),
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				// Create learning agent inside goroutine so event bridge uses execCtx
				// (with correlation IDs), preventing streaming chunks from leaking to main agent UI
				learningAgent, createErr := iwm.controller.createSuccessLearningAgent(
					execCtx, "workshop_learning", learningPathIdentifier, agentName,
					agentConfigs, isCodeExecMode, resolvedID, stepPath, parentStepIndex,
				)
				if createErr != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to create learning agent for step %q: %v", resolvedID, createErr))
					exec.mu.Lock()
					exec.Status = WorkshopStepFailed
					exec.Err = createErr
					exec.mu.Unlock()
					// Emit end event for failed creation
					if eventBridge != nil {
						endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
							BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
							AgentType:     "workshop-step-learning",
							AgentName:     fmt.Sprintf("Learning: %s", resolvedID),
							Success:       false,
							Result:        fmt.Sprintf("Failed to create learning agent: %v", createErr),
						}
						eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
							Type:          orchestrator_events.OrchestratorAgentEnd,
							Timestamp:     time.Now(),
							Data:          endEvent,
							CorrelationID: agentSessionID,
						})
					}
					return
				}

				logger.Info(fmt.Sprintf("🧠 Workshop: generating learnings for step %q (guidance: %q, inner=%v)", resolvedID, guidance, stepNum < 1))
				learningResult, _, execErr := learningAgent.Execute(execCtx, templateVars, []llmtypes.MessageContent{})

				// Build result string and update metadata
				var result string
				if execErr == nil {
					updatedFiles, _ := iwm.controller.readStepLearningFiles(execCtx, learningsPath)

					// Update .learning_metadata.json so tiered mode and keepLearningFull thresholds work
					iwm.updateWorkshopLearningMetadata(execCtx, learningPathIdentifier, stepPath, resolvedID, len(updatedFiles) > 0)

					var sb strings.Builder
					sb.WriteString(fmt.Sprintf("✅ Learnings generated for step %q\n", resolvedID))
					if stepNum < 1 {
						sb.WriteString(fmt.Sprintf("(inner step — parent: %s, branch: %s)\n", stepInfo.ParentID, stepInfo.BranchName))
					}
					if guidance != "" {
						sb.WriteString(fmt.Sprintf("Guidance applied: %s\n", guidance))
					}
					sb.WriteString(fmt.Sprintf("Learning files: %d | Path: %s\n", len(updatedFiles), learningsPath))
					if len(updatedFiles) > 0 {
						for f := range updatedFiles {
							sb.WriteString(fmt.Sprintf("  - %s\n", f))
						}
					}
					sb.WriteString(fmt.Sprintf("\nAgent output:\n%s", learningResult))
					result = sb.String()
				}

				// Emit orchestrator_agent_end to close the grouping card
				if eventBridge != nil {
					isCancelled := execCtx.Err() != nil || errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded)
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-learning",
						AgentName:     fmt.Sprintf("Learning: %s", resolvedID),
						Success:       execErr == nil,
					}
					if execErr != nil {
						if isCancelled {
							endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
						} else {
							endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
						}
					} else {
						endEvent.Result = result
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentEnd,
						Timestamp:     time.Now(),
						Data:          endEvent,
						CorrelationID: agentSessionID,
					})
				}

				exec.mu.Lock()
				defer exec.mu.Unlock()
				if exec.Status == WorkshopStepCancelled {
					return
				}
				if execErr != nil {
					if execCtx.Err() != nil || errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
						exec.Status = WorkshopStepCancelled
						exec.Err = execErr
					} else {
						exec.Status = WorkshopStepFailed
						exec.Err = execErr
					}
				} else {
					exec.Status = WorkshopStepDone
					exec.Result = result
				}
			}()

			guidanceInfo := ""
			if guidance != "" {
				guidanceInfo = fmt.Sprintf("\nGuidance: %s", guidance)
			}
			logger.Info(fmt.Sprintf("🧠 Workshop: learning agent for step %q started in background, execution_id=%q", resolvedID, execID))
			return fmt.Sprintf("Learning agent for step %q started in background.\nexecution_id: %q%s\nYou will be automatically notified when it completes.", resolvedID, execID, guidanceInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register generate_learnings tool: %v", err))
	}

	// Tool 7b: optimize_step — background optimization agent
	if err := mcpAgent.RegisterCustomTool(
		"optimize_step",
		"Start a background optimization agent that analyzes logs, output, learnings, and config for a step. Returns execution_id immediately — you will be automatically notified when it completes.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The step ID from plan.json (e.g., 'step-create-report') or positional reference (e.g., '1', 'step-1')",
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus guidance for the optimization agent. E.g., 'learnings quality', 'tool usage', 'output correctness', 'validation schema coverage'.",
				},
			},
			"required": []string{"step_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepIDRaw, ok := args["step_id"]
			if !ok || stepIDRaw == nil {
				return "step_id is required", nil
			}
			stepID, ok := stepIDRaw.(string)
			if !ok || stepID == "" {
				return "step_id must be a non-empty string", nil
			}

			focus := ""
			if val, ok := args["focus"]; ok && val != nil {
				if s, ok := val.(string); ok {
					focus = s
				}
			}

			// Resolve step ID
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				return fmt.Sprintf("Failed to load plan: %v. Cannot resolve step ID.", err), nil
			}
			resolvedID, resolveErr := resolveWorkshopStepID(iwm.controller, stepID)
			if resolveErr != nil {
				return resolveErr.Error(), nil
			}
			stepID = resolvedID

			execID := fmt.Sprintf("debug-%s-%05d", stepID, time.Now().UnixNano()%100000)
			execCtx, cancel := context.WithCancel(iwm.sessionCtx)

			// Inject correlation IDs for sub-agent event tagging (same pattern as execute_step)
			agentSessionID := fmt.Sprintf("workshop-debug-%s-%d", stepID, time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         stepID,
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			go func() {
				// Emit orchestrator_agent_start so the frontend creates a grouping card
				eventBridge := iwm.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-debug",
						AgentName:     fmt.Sprintf("Optimize: %s", stepID),
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				result, err := iwm.runOptimizeStepAgent(execCtx, stepID, focus)

				// Emit orchestrator_agent_end to close the grouping card
				if eventBridge != nil {
					isCancelled := execCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-debug",
						AgentName:     fmt.Sprintf("Optimize: %s", stepID),
						Success:       err == nil,
					}
					if err != nil {
						if isCancelled {
							endEvent.Result = fmt.Sprintf("Cancelled: %v", err)
						} else {
							endEvent.Result = fmt.Sprintf("Failed: %v", err)
						}
					} else {
						endEvent.Result = result
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentEnd,
						Timestamp:     time.Now(),
						Data:          endEvent,
						CorrelationID: agentSessionID,
					})
				}

				exec.mu.Lock()
				defer exec.mu.Unlock()
				if exec.Status == WorkshopStepCancelled {
					return
				}
				if err != nil {
					if execCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						exec.Status = WorkshopStepCancelled
						exec.Err = err
					} else {
						exec.Status = WorkshopStepFailed
						exec.Err = err
					}
				} else {
					exec.Status = WorkshopStepDone
					exec.Result = result
				}
			}()

			focusInfo := ""
			if focus != "" {
				focusInfo = fmt.Sprintf("\nFocus: %s", focus)
			}
			logger.Info(fmt.Sprintf("🔍 Workshop: optimization agent for step %q started in background, execution_id=%q", stepID, execID))
			return fmt.Sprintf("Optimization agent for step %q started in background.\nexecution_id: %q%s\nYou will be automatically notified when it completes.", stepID, execID, focusInfo), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register optimize_step tool: %v", err))
	}

	// Tool 8: get_cost_summary — parse token_usage.json and show formatted cost breakdown
	if err := mcpAgent.RegisterCustomTool(
		"get_cost_summary",
		"Show token usage and cost breakdown for the current run. Displays per-step and per-model totals with USD costs.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			runFolder := iwm.controller.selectedRunFolder
			if runFolder == "" {
				return "no run folder selected", nil
			}

			tokenFilePath := fmt.Sprintf("runs/%s/token_usage.json", runFolder)
			content, err := iwm.controller.ReadWorkspaceFile(ctx, tokenFilePath)
			if err != nil {
				return fmt.Sprintf("No token usage data found at %s", tokenFilePath), nil
			}

			var tokenFile orchestrator.TokenUsageFile
			if err := json.Unmarshal([]byte(content), &tokenFile); err != nil {
				return fmt.Sprintf("Failed to parse token_usage.json: %v", err), nil
			}

			// Helper to default empty token strings to "0"
			tok := func(s string) string {
				if s == "" {
					return "0"
				}
				return s
			}

			var result strings.Builder
			result.WriteString(fmt.Sprintf("## Cost Summary — %s\n\n", runFolder))

			// Per-step breakdown (sorted by step key for deterministic output)
			if len(tokenFile.ByStepAndModel) > 0 {
				result.WriteString("### Per-Step Breakdown\n\n")
				result.WriteString("| Step | Model | Input | Output | Cache | Cost |\n")
				result.WriteString("|------|-------|-------|--------|-------|------|\n")

				stepKeys := make([]string, 0, len(tokenFile.ByStepAndModel))
				for k := range tokenFile.ByStepAndModel {
					stepKeys = append(stepKeys, k)
				}
				sort.Strings(stepKeys)

				grandTotalCost := 0.0
				for _, stepKey := range stepKeys {
					models := tokenFile.ByStepAndModel[stepKey]
					modelKeys := make([]string, 0, len(models))
					for k := range models {
						modelKeys = append(modelKeys, k)
					}
					sort.Strings(modelKeys)
					for _, modelID := range modelKeys {
						usage := models[modelID]
						grandTotalCost += usage.TotalCost
						result.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | $%.4f |\n",
							stepKey, modelID,
							tok(usage.InputTokensM), tok(usage.OutputTokensM), tok(usage.CacheTokensM),
							usage.TotalCost,
						))
					}
				}
				result.WriteString(fmt.Sprintf("\n**Grand total: $%.4f**\n\n", grandTotalCost))
			}

			// Per-model totals (sorted by model ID)
			if len(tokenFile.ByModel) > 0 {
				result.WriteString("### Per-Model Totals\n\n")
				result.WriteString("| Model | Input | Output | Cache R/W | Reasoning | Calls | Cost |\n")
				result.WriteString("|-------|-------|--------|-----------|-----------|-------|------|\n")

				modelKeys := make([]string, 0, len(tokenFile.ByModel))
				for k := range tokenFile.ByModel {
					modelKeys = append(modelKeys, k)
				}
				sort.Strings(modelKeys)

				totalCost := 0.0
				for _, modelID := range modelKeys {
					usage := tokenFile.ByModel[modelID]
					totalCost += usage.TotalCost
					cacheRW := fmt.Sprintf("%s / %s", tok(usage.CacheReadTokensM), tok(usage.CacheWriteTokensM))
					result.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %d | $%.4f |\n",
						modelID,
						tok(usage.InputTokensM), tok(usage.OutputTokensM), cacheRW,
						tok(usage.ReasoningTokensM), usage.LLMCallCount,
						usage.TotalCost,
					))
				}
				result.WriteString(fmt.Sprintf("\n**Total: $%.4f**\n", totalCost))
			}

			// Also check phase-level costs (planning, etc.)
			phaseTokenPath := "token_usage.json"
			phaseContent, phaseErr := iwm.controller.ReadWorkspaceFile(ctx, phaseTokenPath)
			if phaseErr == nil {
				var phaseFile orchestrator.PhaseTokenUsageFile
				if err := json.Unmarshal([]byte(phaseContent), &phaseFile); err == nil && len(phaseFile.ByPhaseAndModel) > 0 {
					result.WriteString("\n### Phase-Level Costs (planning, learning, etc.)\n\n")
					result.WriteString("| Phase | Model | Input | Output | Cost |\n")
					result.WriteString("|-------|-------|-------|--------|------|\n")

					phaseKeys := make([]string, 0, len(phaseFile.ByPhaseAndModel))
					for k := range phaseFile.ByPhaseAndModel {
						phaseKeys = append(phaseKeys, k)
					}
					sort.Strings(phaseKeys)

					phaseTotalCost := 0.0
					for _, phase := range phaseKeys {
						models := phaseFile.ByPhaseAndModel[phase]
						pModelKeys := make([]string, 0, len(models))
						for k := range models {
							pModelKeys = append(pModelKeys, k)
						}
						sort.Strings(pModelKeys)
						for _, modelID := range pModelKeys {
							usage := models[modelID]
							phaseTotalCost += usage.TotalCost
							result.WriteString(fmt.Sprintf("| %s | %s | %s | %s | $%.4f |\n",
								phase, modelID,
								tok(usage.InputTokensM), tok(usage.OutputTokensM),
								usage.TotalCost,
							))
						}
					}
					result.WriteString(fmt.Sprintf("\n**Phase total: $%.4f**\n", phaseTotalCost))
				}
			}

			return result.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_cost_summary tool: %v", err))
	}

	// Tool 9: run_background_task — generic background sub-agent for offloading work
	if err := mcpAgent.RegisterCustomTool(
		"run_background_task",
		"Run a generic background task with a sub-agent that has full tool access (workspace tools + all MCP servers). Use this to offload complex or time-consuming work that doesn't fit execute_step/generate_learnings/optimize_step. Examples: bulk file analysis, data transformation, research across multiple files, generating reports, cleanup tasks. The sub-agent runs independently and you will be notified when it completes.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task": map[string]interface{}{
					"type":        "string",
					"description": "Detailed description of the task for the sub-agent. Be specific about what to do, what files/paths to work with, and what output is expected.",
				},
				"task_name": map[string]interface{}{
					"type":        "string",
					"description": "Short label for the task (shown in UI and list_executions). E.g., 'analyze-logs', 'cleanup-outputs', 'compare-results'.",
				},
				"max_turns": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum LLM turns for the sub-agent (default: 50). Use higher values for complex multi-step tasks.",
				},
			},
			"required": []string{"task", "task_name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			task, _ := args["task"].(string)
			if task == "" {
				return "task is required and must be non-empty", nil
			}
			taskName, _ := args["task_name"].(string)
			if taskName == "" {
				return "task_name is required and must be non-empty", nil
			}

			maxTurns := 50
			if v, ok := args["max_turns"]; ok && v != nil {
				if f, ok := v.(float64); ok && f > 0 {
					maxTurns = int(f)
				}
			}

			execID := fmt.Sprintf("task-%s-%05d", taskName, time.Now().UnixNano()%100000)
			execCtx, cancel := context.WithCancel(iwm.sessionCtx)

			agentSessionID := fmt.Sprintf("workshop-task-%s-%d", taskName, time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         taskName,
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			go func() {
				eventBridge := iwm.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-background-task",
						AgentName:     fmt.Sprintf("Task: %s", taskName),
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				// Create agent with same tools/servers as the workshop agent
				result, execErr := iwm.runGenericBackgroundTask(execCtx, task, taskName, maxTurns)

				// Emit end event
				if eventBridge != nil {
					isCancelled := execCtx.Err() != nil || errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded)
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-background-task",
						AgentName:     fmt.Sprintf("Task: %s", taskName),
						Success:       execErr == nil,
					}
					if execErr != nil {
						if isCancelled {
							endEvent.Result = fmt.Sprintf("Cancelled: %v", execErr)
						} else {
							endEvent.Result = fmt.Sprintf("Failed: %v", execErr)
						}
					} else {
						endEvent.Result = result
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentEnd,
						Timestamp:     time.Now(),
						Data:          endEvent,
						CorrelationID: agentSessionID,
					})
				}

				exec.mu.Lock()
				defer exec.mu.Unlock()
				if exec.Status == WorkshopStepCancelled {
					return
				}
				if execErr != nil {
					if execCtx.Err() != nil || errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
						exec.Status = WorkshopStepCancelled
						exec.Err = execErr
					} else {
						exec.Status = WorkshopStepFailed
						exec.Err = execErr
					}
				} else {
					exec.Status = WorkshopStepDone
					exec.Result = result
				}
			}()

			return fmt.Sprintf("Background task %q started.\nexecution_id: %q\nmax_turns: %d\nYou will be automatically notified when it completes.", taskName, execID, maxTurns), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_background_task tool: %v", err))
	}

	// NOTE: update_variable and manage_variable_group tools have been removed.
	// Variable/group management is now handled via the workspace UI.
	// The read-only tools below (get_llm_config, get_variables) are kept for discovery.

	// Tool: get_llm_config — show current LLM configuration (read-only)
	if err := mcpAgent.RegisterCustomTool(
		"get_llm_config",
		"Show the current LLM configuration for the workflow and per-step overrides. Returns the preset-level defaults (execution, validation, learning, phase LLMs), tiered config if enabled, and any per-step LLM overrides from step_config.json.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			var sb strings.Builder
			sb.WriteString("## LLM Configuration\n\n")

			// Show preset-level defaults (execution and learning only — validation is deprecated)
			sb.WriteString("### Workflow Defaults (from preset)\n")
			writeLLMEntry := func(label string, llm *AgentLLMConfig) {
				if llm != nil {
					sb.WriteString(fmt.Sprintf("- **%s**: %s/%s\n", label, llm.Provider, llm.ModelID))
				} else {
					sb.WriteString(fmt.Sprintf("- **%s**: (not set — uses LLM config default)\n", label))
				}
			}
			writeLLMEntry("Execution LLM", iwm.controller.presetExecutionLLM)
			writeLLMEntry("Learning LLM", iwm.controller.presetLearningLLM)

			// Show tiered config if enabled
			if iwm.controller.tierResolver != nil {
				sb.WriteString("\n### Tiered LLM Config (active)\n")
				// Use ResolveTier to show tier configs
				if t1 := iwm.controller.tierResolver.ResolveTier(TierHigh); t1 != nil {
					sb.WriteString(fmt.Sprintf("- **Tier 1** (high): %s/%s\n", t1.Primary.Provider, t1.Primary.ModelID))
				}
				if t2 := iwm.controller.tierResolver.ResolveTier(TierMedium); t2 != nil {
					sb.WriteString(fmt.Sprintf("- **Tier 2** (medium): %s/%s\n", t2.Primary.Provider, t2.Primary.ModelID))
				}
				if t3 := iwm.controller.tierResolver.ResolveTier(TierLow); t3 != nil {
					sb.WriteString(fmt.Sprintf("- **Tier 3** (low): %s/%s\n", t3.Primary.Provider, t3.Primary.ModelID))
				}
			}

			// Show per-step overrides from step_config.json
			stepConfigs, err := iwm.controller.ReadStepConfigs(ctx)
			if err != nil {
				sb.WriteString("\n### Per-Step LLM Overrides\nCould not read step_config.json.\n")
			} else {
				hasOverrides := false
				for _, sc := range stepConfigs {
					if sc.AgentConfigs == nil {
						continue
					}
					ac := sc.AgentConfigs
					if ac.ExecutionLLM == nil && ac.LearningLLM == nil && ac.OrchestratorLLM == nil && ac.SubAgentLLM == nil {
						continue
					}
					if !hasOverrides {
						sb.WriteString("\n### Per-Step LLM Overrides\n")
						hasOverrides = true
					}
					sb.WriteString(fmt.Sprintf("\n**%s**:\n", sc.ID))
					if ac.ExecutionLLM != nil {
						sb.WriteString(fmt.Sprintf("  - execution: %s/%s\n", ac.ExecutionLLM.Provider, ac.ExecutionLLM.ModelID))
					}
					if ac.LearningLLM != nil {
						sb.WriteString(fmt.Sprintf("  - learning: %s/%s\n", ac.LearningLLM.Provider, ac.LearningLLM.ModelID))
					}
					if ac.OrchestratorLLM != nil {
						sb.WriteString(fmt.Sprintf("  - orchestrator: %s/%s\n", ac.OrchestratorLLM.Provider, ac.OrchestratorLLM.ModelID))
					}
					if ac.SubAgentLLM != nil {
						sb.WriteString(fmt.Sprintf("  - sub_agent: %s/%s\n", ac.SubAgentLLM.Provider, ac.SubAgentLLM.ModelID))
					}
				}
				if !hasOverrides {
					sb.WriteString("\n### Per-Step LLM Overrides\nNone — all steps use workflow defaults.\n")
				}
			}

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_llm_config tool: %v", err))
	}

	// Tool: get_variables — read-only view of current variables (no management)
	if err := mcpAgent.RegisterCustomTool(
		"get_variables",
		"Read current variable definitions and their values. Shows the base variable definitions and group configurations. For managing variables, use the workspace UI.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			manifest, err := readVariablesFromFile(ctx, iwm.controller.GetWorkspacePath(), func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			})
			if err != nil {
				return fmt.Sprintf("Could not read variables.json: %v", err), nil
			}
			if manifest == nil || len(manifest.Variables) == 0 {
				return "No variables defined in this workflow.", nil
			}

			var sb strings.Builder
			sb.WriteString("## Variables\n\n")
			sb.WriteString("### Definitions\n")
			for _, v := range manifest.Variables {
				sb.WriteString(fmt.Sprintf("- **%s** = `%s` — %s\n", v.Name, v.Value, v.Description))
			}

			if manifest.HasGroups() {
				sb.WriteString(fmt.Sprintf("\n### Groups (%d)\n", len(manifest.Groups)))
				for _, g := range manifest.Groups {
					status := "enabled"
					if !g.Enabled {
						status = "disabled"
					}
					displayName := g.DisplayName
					if displayName == "" {
						displayName = g.GroupID
					}
					sb.WriteString(fmt.Sprintf("\n**%s** (`%s`, %s):\n", displayName, g.GroupID, status))
					for k, v := range g.Values {
						sb.WriteString(fmt.Sprintf("  - %s = %s\n", k, v))
					}
				}
			}

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_variables tool: %v", err))
	}

	// Tool: get_workflow_config — read-only view of workflow-level settings (MCP servers, skills, secrets, LLM config)
	if err := mcpAgent.RegisterCustomTool(
		"get_workflow_config",
		"Show current workflow configuration: MCP servers (selected + all available with descriptions), skills, secrets (names only, no values), and LLM config (tiered allocation with fallbacks, preset defaults).",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			ctrl := iwm.controller
			selected := ctrl.GetSelectedServers()
			var sb strings.Builder
			sb.WriteString("## Workflow Configuration\n\n")

			// --- MCP Servers ---
			sb.WriteString("### Selected MCP Servers\n")
			if len(selected) == 0 {
				sb.WriteString("No MCP servers selected for this workflow.\n")
			} else {
				for _, s := range selected {
					sb.WriteString(fmt.Sprintf("- %s\n", s))
				}
			}

			// Load all discoverable servers from MCP config (with descriptions)
			configPath := ctrl.GetMCPConfigPath()
			if configPath != "" {
				mergedCfg, err := mcpclient.LoadMergedConfig(configPath, nil)
				if err == nil && mergedCfg != nil {
					allServers := mergedCfg.ListServers()
					if len(allServers) > 0 {
						selectedSet := make(map[string]bool, len(selected))
						for _, s := range selected {
							selectedSet[s] = true
						}
						sb.WriteString("\n### All Available MCP Servers\n")
						for _, s := range allServers {
							desc := ""
							if cfg, ok := mergedCfg.MCPServers[s]; ok && cfg.Description != "" {
								desc = " — " + cfg.Description
							}
							if selectedSet[s] {
								sb.WriteString(fmt.Sprintf("- %s ✓ (selected)%s\n", s, desc))
							} else {
								sb.WriteString(fmt.Sprintf("- %s%s\n", s, desc))
							}
						}
					}
				} else if err != nil {
					sb.WriteString(fmt.Sprintf("\n_Could not load available servers: %v_\n", err))
				}
			}

			// --- Skills ---
			selectedSkills := ctrl.GetSelectedSkills()
			selectedSkillSet := make(map[string]bool, len(selectedSkills))
			for _, sk := range selectedSkills {
				selectedSkillSet[sk] = true
			}

			sb.WriteString("\n### Selected Skills\n")
			if len(selectedSkills) == 0 {
				sb.WriteString("No skills selected for this workflow.\n")
			} else {
				for _, sk := range selectedSkills {
					sb.WriteString(fmt.Sprintf("- **%s** — instructions at `skills/%s/SKILL.md`\n", sk, sk))
				}
			}

			// Discover all available skills from workspace
			workspaceAPIURL := os.Getenv("WORKSPACE_API_URL")
			if workspaceAPIURL == "" {
				workspaceAPIURL = "http://localhost:8081"
			}
			allSkills, discoverErr := skills.DiscoverSkills(workspaceAPIURL)
			if discoverErr == nil && len(allSkills) > 0 {
				sb.WriteString("\n### All Available Skills\n")
				for _, sk := range allSkills {
					desc := sk.Frontmatter.Description
					if desc == "" {
						desc = sk.Frontmatter.Name
					}
					if selectedSkillSet[sk.FolderName] {
						sb.WriteString(fmt.Sprintf("- %s ✓ (selected) — %s\n", sk.FolderName, desc))
					} else {
						sb.WriteString(fmt.Sprintf("- %s — %s\n", sk.FolderName, desc))
					}
				}
			} else if discoverErr != nil {
				sb.WriteString(fmt.Sprintf("\n_Could not discover available skills: %v_\n", discoverErr))
			}

			// --- Secrets (names only) ---
			secrets := ctrl.GetSecrets()
			sb.WriteString("\n### Secrets\n")
			if len(secrets) == 0 {
				sb.WriteString("No secrets configured for this workflow.\n")
			} else {
				sb.WriteString("The following named credentials are configured (values hidden):\n")
				for _, s := range secrets {
					sb.WriteString(fmt.Sprintf("- **%s**\n", s.Name))
				}
			}

			// --- LLM Configuration ---
			sb.WriteString("\n### LLM Configuration\n")

			// Tiered config
			if ctrl.tierResolver != nil && ctrl.tierResolver.config != nil {
				tc := ctrl.tierResolver.config
				sb.WriteString("\n**Tiered Allocation (active)**:\n")
				writeTierEntry := func(label string, cfg *AgentLLMConfig) {
					if cfg == nil {
						return
					}
					sb.WriteString(fmt.Sprintf("- **%s**: %s/%s", label, cfg.Provider, cfg.ModelID))
					if len(cfg.Fallbacks) > 0 {
						fallbackStrs := make([]string, len(cfg.Fallbacks))
						for i, fb := range cfg.Fallbacks {
							fallbackStrs[i] = fmt.Sprintf("%s/%s", fb.Provider, fb.ModelID)
						}
						sb.WriteString(fmt.Sprintf(" → fallbacks: %s", strings.Join(fallbackStrs, ", ")))
					}
					sb.WriteString("\n")
				}
				writeTierEntry("Tier 1 (high)", tc.Tier1)
				writeTierEntry("Tier 2 (medium)", tc.Tier2)
				writeTierEntry("Tier 3 (low)", tc.Tier3)
			}

			// Preset-level defaults
			sb.WriteString("\n**Preset Defaults**:\n")
			writeLLMDefault := func(label string, llm *AgentLLMConfig) {
				if llm != nil {
					sb.WriteString(fmt.Sprintf("- **%s**: %s/%s", label, llm.Provider, llm.ModelID))
					if len(llm.Fallbacks) > 0 {
						fallbackStrs := make([]string, len(llm.Fallbacks))
						for i, fb := range llm.Fallbacks {
							fallbackStrs[i] = fmt.Sprintf("%s/%s", fb.Provider, fb.ModelID)
						}
						sb.WriteString(fmt.Sprintf(" → fallbacks: %s", strings.Join(fallbackStrs, ", ")))
					}
					sb.WriteString("\n")
				} else {
					sb.WriteString(fmt.Sprintf("- **%s**: (not set — uses LLM config default)\n", label))
				}
			}
			writeLLMDefault("Execution LLM", ctrl.presetExecutionLLM)
			writeLLMDefault("Learning LLM", ctrl.presetLearningLLM)
			writeLLMDefault("Phase LLM", ctrl.presetPhaseLLM)
			if ctrl.presetPlanImprovementLLM != nil {
				writeLLMDefault("Plan Improvement LLM", ctrl.presetPlanImprovementLLM)
			}

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_workflow_config tool: %v", err))
	}

	// Tool: update_workflow_config — add/remove MCP servers, skills, and secrets
	if err := mcpAgent.RegisterCustomTool(
		"update_workflow_config",
		"Update workflow configuration: add/remove MCP servers, add/remove skills, enable/disable secrets. Use get_workflow_config first to see available options. Changes take effect immediately for subsequent step executions.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"add_servers": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "MCP server names to add to the workflow",
				},
				"remove_servers": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "MCP server names to remove from the workflow",
				},
				"add_skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Skill folder names to add to the workflow (use get_workflow_config to see available skills)",
				},
				"remove_skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Skill folder names to remove from the workflow",
				},
				"add_secrets": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Secret names to add/enable for the workflow",
				},
				"remove_secrets": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Secret names to remove/disable from the workflow",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			var sb strings.Builder
			anyChanged := false

			// Helper to extract string array from args
			extractStringArray := func(key string) []string {
				val, ok := args[key]
				if !ok || val == nil {
					return nil
				}
				arr, ok := val.([]interface{})
				if !ok {
					return nil
				}
				result := make([]string, 0, len(arr))
				for _, v := range arr {
					if s, ok := v.(string); ok && s != "" {
						result = append(result, s)
					}
				}
				return result
			}

			// --- MCP Servers ---
			addServers := extractStringArray("add_servers")
			removeServers := extractStringArray("remove_servers")
			if len(addServers) > 0 || len(removeServers) > 0 {
				servers := iwm.controller.GetSelectedServers()
				result := make([]string, len(servers))
				copy(result, servers)
				changed := false

				if len(addServers) > 0 {
					existSet := make(map[string]bool, len(result))
					for _, s := range result {
						existSet[s] = true
					}
					for _, s := range addServers {
						if !existSet[s] {
							result = append(result, s)
							existSet[s] = true
							changed = true
						}
					}
				}

				if len(removeServers) > 0 {
					removeSet := make(map[string]bool, len(removeServers))
					for _, s := range removeServers {
						removeSet[s] = true
					}
					filtered := result[:0]
					for _, s := range result {
						if !removeSet[s] {
							filtered = append(filtered, s)
						} else {
							changed = true
						}
					}
					result = filtered
				}

				if changed {
					iwm.controller.SetSelectedServers(result)
					anyChanged = true
					sb.WriteString("### MCP Servers (updated)\n")
					if len(result) == 0 {
						sb.WriteString("No MCP servers configured.\n")
					} else {
						for _, s := range result {
							sb.WriteString(fmt.Sprintf("- %s\n", s))
						}
					}
					logger.Info(fmt.Sprintf("Updated workflow MCP servers: %v", result))
				}
			}

			// --- Skills ---
			addSkills := extractStringArray("add_skills")
			removeSkills := extractStringArray("remove_skills")
			if len(addSkills) > 0 || len(removeSkills) > 0 {
				currentSkills := iwm.controller.GetSelectedSkills()
				result := make([]string, len(currentSkills))
				copy(result, currentSkills)
				changed := false

				if len(addSkills) > 0 {
					existSet := make(map[string]bool, len(result))
					for _, s := range result {
						existSet[s] = true
					}
					for _, s := range addSkills {
						if !existSet[s] {
							result = append(result, s)
							existSet[s] = true
							changed = true
						}
					}
				}

				if len(removeSkills) > 0 {
					removeSet := make(map[string]bool, len(removeSkills))
					for _, s := range removeSkills {
						removeSet[s] = true
					}
					filtered := result[:0]
					for _, s := range result {
						if !removeSet[s] {
							filtered = append(filtered, s)
						} else {
							changed = true
						}
					}
					result = filtered
				}

				if changed {
					iwm.controller.SetSelectedSkills(result)
					anyChanged = true
					sb.WriteString("\n### Skills (updated)\n")
					if len(result) == 0 {
						sb.WriteString("No skills configured.\n")
					} else {
						for _, s := range result {
							sb.WriteString(fmt.Sprintf("- %s\n", s))
						}
					}
					logger.Info(fmt.Sprintf("Updated workflow skills: %v", result))
				}
			}

			// --- Secrets ---
			addSecrets := extractStringArray("add_secrets")
			removeSecrets := extractStringArray("remove_secrets")
			if len(addSecrets) > 0 || len(removeSecrets) > 0 {
				currentSecrets := iwm.controller.GetSecrets()
				changed := false

				if len(addSecrets) > 0 {
					existSet := make(map[string]bool, len(currentSecrets))
					for _, s := range currentSecrets {
						existSet[s.Name] = true
					}
					for _, name := range addSecrets {
						if !existSet[name] {
							currentSecrets = append(currentSecrets, orchestrator.SecretEntry{Name: name, Value: ""})
							existSet[name] = true
							changed = true
						}
					}
				}

				if len(removeSecrets) > 0 {
					removeSet := make(map[string]bool, len(removeSecrets))
					for _, s := range removeSecrets {
						removeSet[s] = true
					}
					filtered := currentSecrets[:0]
					for _, s := range currentSecrets {
						if !removeSet[s.Name] {
							filtered = append(filtered, s)
						} else {
							changed = true
						}
					}
					currentSecrets = filtered
				}

				if changed {
					iwm.controller.SetSecrets(currentSecrets)
					anyChanged = true
					sb.WriteString("\n### Secrets (updated)\n")
					if len(currentSecrets) == 0 {
						sb.WriteString("No secrets configured.\n")
					} else {
						for _, s := range currentSecrets {
							sb.WriteString(fmt.Sprintf("- %s\n", s.Name))
						}
					}
					logger.Info(fmt.Sprintf("Updated workflow secrets: %d entries", len(currentSecrets)))
				}
			}

			if !anyChanged {
				return "No changes applied. Provide at least one of: add_servers, remove_servers, add_skills, remove_skills, add_secrets, remove_secrets.", nil
			}

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_workflow_config tool: %v", err))
	}

}

// workshopJSONUnmarshal is a local alias to avoid import conflicts
func workshopJSONUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// ============================================================================
// Optimization Agent — background agent for deep step analysis
// ============================================================================

// Pre-parsed templates for optimization agent
var optimizationAgentSystemTemplate = MustRegisterTemplate("optimizationAgentSystem", `# Step Optimization Agent

You are a read-only step optimization analyst. You analyze execution logs, output files, learnings, and configuration for a specific workflow step and produce a structured report with actionable recommendations.

## 🤖 ROLE
Perform deep analysis of a step's execution and produce a comprehensive optimization report. You are **read-only** — you do NOT modify any files, plans, or configurations.

## ⚠️ RULES
1. **Read-Only**: Do NOT modify any files. Use shell commands only for reading files (cat, ls, head, etc.).
2. **Be Specific**: Reference exact file paths, line numbers, field names, and values in your analysis.
3. **Be Actionable**: Every recommendation must be something the user can act on immediately.
4. **Prioritize by Impact**: Rank recommendations by how much they'd improve the step's reliability and output quality.

## 📋 STEP CONTEXT

- **Step ID**: {{.StepID}}
- **Step Title**: {{.StepTitle}}
- **Step Description**: {{.StepDescription}}
- **Success Criteria**: {{.StepSuccessCriteria}}
- **Context Output**: {{.StepContextOutput}}
- **Context Dependencies**: {{.StepContextDependencies}}
- **Workspace**: {{.WorkspacePath}}
- **Run Folder**: {{.RunFolder}}

{{if .StepPlanJSON}}### Step Plan Entry
` + "```json\n{{.StepPlanJSON}}\n```" + `
{{end}}

{{if .StepConfigJSON}}### Step Config
` + "```json\n{{.StepConfigJSON}}\n```" + `
{{end}}

{{if .ValidationResult}}### Latest Validation Result
` + "```json\n{{.ValidationResult}}\n```" + `
{{end}}

{{if .ExistingLearnings}}### Existing Learnings
{{.ExistingLearnings}}
{{end}}

{{if .ToolUsageSummary}}### Tool Usage Summary
{{.ToolUsageSummary}}
{{end}}

{{if .ComplexStepDetails}}### Complex Step Details
{{.ComplexStepDetails}}
{{end}}

{{if .Focus}}### Analysis Focus
The user wants you to focus specifically on: **{{.Focus}}**
{{end}}

## 📁 DATA LAYOUT

All paths relative to workspace root:
- Execution output: ` + "`runs/{{.RunFolder}}/execution/step-{{.StepNum}}/`" + `
- Execution logs: ` + "`runs/{{.RunFolder}}/logs/step-{{.StepNum}}/execution/`" + `
- Validation logs: ` + "`runs/{{.RunFolder}}/logs/step-{{.StepNum}}/`" + `
- Learnings: ` + "`learnings/{{.StepID}}/`" + `
- Plan: ` + "`planning/plan.json`" + `
- Step config: ` + "`planning/step_config.json`" + `

## 📖 ANALYSIS PROCEDURE

1. **Read execution logs** — Check the latest conversation history for tool calls, errors, retries
2. **Read actual output files** — Compare output against success criteria and validation schema
3. **Review existing learnings** — Are they specific? Actionable? Or noisy/generic?
4. **Analyze tool/server usage** — Are there unused servers? Missing tools?
5. **Check validation schema** — Does it catch stale files? Are there enough field checks?
6. **Check step description** — Is it clear, specific, and actionable?

## 📊 REPORT FORMAT

Produce your report in this exact markdown structure:

### Summary
1-2 sentence overall assessment of the step's health and output quality.

### Output Quality
- Does the output meet success criteria? What's wrong or missing?
- Are there format issues, missing fields, or incorrect values?
- Compare actual output content against what was expected.

### Learnings Review
- Which existing learnings are good (specific, actionable)?
- Which are noisy, generic, or outdated?
- What patterns are missing that should be captured?

### Config Recommendations
- Tool/server scoping: should servers be added or removed?
- LLM tier: is the current model appropriate for this step's complexity?
- Execution mode: any changes needed?
- Learning config: should learning be disabled, locked, or detail level changed?

### Plan Recommendations
- Description improvements: what should be added, clarified, or removed?
- Success criteria: are they sufficient and testable?
- Validation schema: missing checks, too loose, or too strict?
- Context dependencies: any missing or unnecessary dependencies?

### Priority Actions
Ranked list of the top 3-5 most impactful changes, with specific instructions for each.
`)

var optimizationAgentUserTemplate = MustRegisterTemplate("optimizationAgentUser", `Analyze step "{{.StepID}}" and produce an optimization report.{{if .Focus}} Focus especially on: {{.Focus}}{{end}}`)

// WorkflowOptimizationAgent performs deep read-only analysis of a step
type WorkflowOptimizationAgent struct {
	*agents.BaseOrchestratorAgent
}

func newWorkflowOptimizationAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowOptimizationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionQAAgentType,
		eventBridge,
	)
	return &WorkflowOptimizationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements OrchestratorAgent interface for the optimization agent
func (agent *WorkflowOptimizationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}

	// Templates
	var systemPrompt, userMessage strings.Builder
	if err := optimizationAgentSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := optimizationAgentUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}

	// Single-pass execution — no human feedback loop
	inputProcessor := func(map[string]string) string { return userMessage.String() }

	result, updatedHistory, err := agent.ExecuteWithTemplateValidation(
		ctx, templateVars, inputProcessor,
		conversationHistory, struct{}{},
		systemPrompt.String(), true,
	)
	if err != nil {
		return "", nil, err
	}

	return result, updatedHistory, nil
}

// runOptimizeStepAgent gathers context and runs the optimization agent for a step
func (iwm *InteractiveWorkshopManager) runOptimizeStepAgent(ctx context.Context, stepID string, focus string) (string, error) {
	logger := iwm.controller.GetLogger()

	// Find step in plan
	stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID)
	if stepInfo == nil {
		return "", fmt.Errorf("step %q not found in plan", stepID)
	}
	targetStep := stepInfo.Step

	runFolder := iwm.controller.selectedRunFolder
	if runFolder == "" {
		return "", fmt.Errorf("no run folder selected")
	}

	stepNum := stepInfo.TopIndex
	if stepNum < 1 {
		stepNum = 1 // inner steps use step-1
	}

	// --- Gather context (all read-only) ---

	// Step plan JSON
	stepPlanJSON := ""
	if planBytes, err := json.MarshalIndent(targetStep, "", "  "); err == nil {
		stepPlanJSON = string(planBytes)
	}

	// Step config
	stepConfigJSON := ""
	stepConfigs, _ := iwm.controller.ReadStepConfigs(ctx)
	for _, sc := range stepConfigs {
		if sc.ID == stepID {
			if configBytes, err := json.MarshalIndent(sc, "", "  "); err == nil {
				stepConfigJSON = string(configBytes)
			}
			break
		}
	}

	// Latest validation result
	validationResult := ""
	validationLogDir := fmt.Sprintf("runs/%s/logs/step-%d", runFolder, stepNum)
	for i := 5; i >= 2; i-- {
		vPath := fmt.Sprintf("%s/validation-%d.json", validationLogDir, i)
		if content, err := iwm.controller.ReadWorkspaceFile(ctx, vPath); err == nil {
			validationResult = content
			break
		}
	}
	if validationResult == "" {
		if content, err := iwm.controller.ReadWorkspaceFile(ctx, fmt.Sprintf("%s/validation.json", validationLogDir)); err == nil {
			validationResult = content
		}
	}

	// Existing learnings
	existingLearnings := ""
	learningsPath := getLearningFolderPathByStepID("", stepID, "", iwm.controller.isEvaluationMode)
	learningFiles, _ := iwm.controller.readStepLearningFiles(ctx, learningsPath)
	if len(learningFiles) > 0 {
		if formatted, err := iwm.controller.formatStepLearningFilesAsHistory(learningFiles); err == nil {
			existingLearnings = formatted
		}
	}

	// Tool usage summary
	toolUsageSummary := ""
	logsPath := fmt.Sprintf("runs/%s/logs/step-%d/execution", runFolder, stepNum)
	absLogsPath := fmt.Sprintf("%s/%s", iwm.controller.GetWorkspacePath(), logsPath)
	toolUsageMap := make(map[string]*ToolUsageEntry)
	summary := &StepToolUsageSummary{}
	extractToolsFromLogsPath(ctx, absLogsPath, toolUsageMap, iwm.controller.ReadWorkspaceFile, logger, summary)
	if len(toolUsageMap) > 0 {
		var toolSB strings.Builder
		for name, entry := range toolUsageMap {
			source := "MCP"
			if knownWorkspaceToolNames[name] {
				source = "workspace"
			}
			toolSB.WriteString(fmt.Sprintf("- %s (%s): used %d time(s)\n", name, source, entry.UsageCount))
		}
		toolUsageSummary = toolSB.String()
	}

	// Complex step details
	complexStepDetails := iwm.enrichQueryForComplexStep(ctx, stepID)

	// Context dependencies
	contextDeps := ""
	if deps := targetStep.GetContextDependencies(); len(deps) > 0 {
		contextDeps = strings.Join(deps, ", ")
	}

	// --- Create agent ---

	// Read-only folder guard
	workspacePath := iwm.controller.GetWorkspacePath()
	readPaths := []string{
		workspacePath,
		fmt.Sprintf("%s/runs", workspacePath),
		fmt.Sprintf("%s/learnings", workspacePath),
		fmt.Sprintf("%s/planning", workspacePath),
		getKnowledgebasePath(workspacePath),
	}
	writePaths := []string{} // strictly read-only
	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	// LLM — use learning LLM (medium tier) with fallback to execution LLM
	var llmConfigToUse *orchestrator.LLMConfig
	if iwm.controller.presetLearningLLM != nil && iwm.controller.presetLearningLLM.Provider != "" && iwm.controller.presetLearningLLM.ModelID != "" {
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: iwm.controller.presetLearningLLM.Provider,
				ModelID:  iwm.controller.presetLearningLLM.ModelID,
			},
			Fallbacks: iwm.controller.GetFallbacks(),
			APIKeys:   iwm.controller.GetAPIKeys(),
		}
	} else if iwm.controller.presetExecutionLLM != nil && iwm.controller.presetExecutionLLM.Provider != "" && iwm.controller.presetExecutionLLM.ModelID != "" {
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: iwm.controller.presetExecutionLLM.Provider,
				ModelID:  iwm.controller.presetExecutionLLM.ModelID,
			},
			Fallbacks: iwm.controller.GetFallbacks(),
			APIKeys:   iwm.controller.GetAPIKeys(),
		}
	} else {
		return "", fmt.Errorf("no valid LLM configuration found for optimization agent")
	}

	config := iwm.controller.CreateStandardAgentConfigWithLLM("optimization-agent", 20, agents.OutputFormatStructured, llmConfigToUse)
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(iwm.presetLLM)
	config.UseToolSearchMode = false
	config.ServerNames = []string{mcpclient.NoServers}

	// Workspace shell for file reading
	phaseTools, phaseExecutors := iwm.controller.BaseOrchestrator.PreparePhaseAgentTools()

	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowOptimizationAgent(cfg, log, tracer, eventBridge)
	}

	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"optimization",
		0, 0,
		"optimization",
		createAgentFunc,
		phaseTools,
		phaseExecutors,
		true,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create optimization agent: %w", err)
	}

	// --- Prepare template vars ---

	templateVars := map[string]string{
		"StepID":               stepID,
		"StepTitle":            targetStep.GetTitle(),
		"StepDescription":     targetStep.GetDescription(),
		"StepSuccessCriteria":  targetStep.GetSuccessCriteria(),
		"StepContextOutput":   targetStep.GetContextOutput().String(),
		"StepContextDependencies": contextDeps,
		"WorkspacePath":        workspacePath,
		"RunFolder":            runFolder,
		"StepNum":              fmt.Sprintf("%d", stepNum),
		"StepPlanJSON":         stepPlanJSON,
		"StepConfigJSON":       stepConfigJSON,
		"ValidationResult":     validationResult,
		"ExistingLearnings":    existingLearnings,
		"ToolUsageSummary":     toolUsageSummary,
		"ComplexStepDetails":   complexStepDetails,
		"Focus":                focus,
		"SessionID":            iwm.sessionID,
		"WorkflowID":           iwm.workflowID,
	}

	// --- Execute ---

	logger.Info(fmt.Sprintf("🔍 Running optimization agent for step %q (focus: %q)", stepID, focus))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("optimization agent failed: %w", err)
	}

	return result, nil
}

// runGenericBackgroundTask creates and runs a generic sub-agent with full tool access.
func (iwm *InteractiveWorkshopManager) runGenericBackgroundTask(ctx context.Context, task string, taskName string, maxTurns int) (string, error) {
	logger := iwm.controller.GetLogger()
	workspacePath := iwm.controller.GetWorkspacePath()

	// Folder guard: read everything, write to runs/ and learnings/
	readPaths := []string{workspacePath}
	runsPath := fmt.Sprintf("%s/runs", workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	writePaths := []string{runsPath, learningsPath}

	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	// Use the workshop agent's LLM, fall back to preset execution LLM
	effectiveLLM := iwm.presetLLM
	if effectiveLLM == nil || effectiveLLM.Provider == "" || effectiveLLM.ModelID == "" {
		effectiveLLM = iwm.controller.presetExecutionLLM
	}
	if effectiveLLM == nil || effectiveLLM.Provider == "" || effectiveLLM.ModelID == "" {
		return "", fmt.Errorf("no valid LLM configuration for background task — neither presetPhaseLLM nor presetExecutionLLM is configured")
	}
	llmConfig := &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{
			Provider: effectiveLLM.Provider,
			ModelID:  effectiveLLM.ModelID,
		},
		Fallbacks: iwm.controller.GetFallbacks(),
		APIKeys:   iwm.controller.GetAPIKeys(),
	}

	agentName := fmt.Sprintf("background-task-%s", taskName)
	config := iwm.controller.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(effectiveLLM)
	config.UseToolSearchMode = false

	// MCP Servers — same as workshop agent
	selectedServers := iwm.controller.GetSelectedServers()
	selectedTools := iwm.controller.GetSelectedTools()
	mcpConfigPath := iwm.controller.GetMCPConfigPath()

	if len(selectedServers) > 0 && mcpConfigPath != "" {
		config.ServerNames = selectedServers
		config.SelectedTools = selectedTools
		config.MCPConfigPath = mcpConfigPath
		config.MCPSessionID = iwm.controller.GetMCPSessionID()
	} else {
		config.ServerNames = []string{mcpclient.NoServers}
	}

	// Phase tools: shell_command etc.
	phaseTools, phaseExecutors := iwm.controller.BaseOrchestrator.PreparePhaseAgentTools()

	systemPrompt := fmt.Sprintf(`# Background Task Agent

## Context: %s

## Role
You are a background task agent with full tool access (workspace tools + all MCP servers).
Execute the task described below thoroughly and return a clear summary of what was done.

## Workspace
- **Path**: %s
- **Run Folder**: %s

## Task
%s`, time.Now().Format("2006-01-02 15:04:05"), workspacePath, iwm.controller.selectedRunFolder, task)

	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newGenericBackgroundAgent(cfg, log, tracer, eventBridge, systemPrompt, task)
	}

	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"background-task",
		0, 0,
		"background-task",
		createAgentFunc,
		phaseTools,
		phaseExecutors,
		true,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create background task agent: %w", err)
	}

	logger.Info(fmt.Sprintf("🔧 Running background task %q (max_turns: %d)", taskName, maxTurns))
	result, _, err := agent.Execute(ctx, map[string]string{}, nil)
	if err != nil {
		return "", err
	}

	return result, nil
}

// GenericBackgroundAgent is a simple agent that executes a task with a provided system prompt.
type GenericBackgroundAgent struct {
	*agents.BaseOrchestratorAgent
	systemPrompt string
	userMessage  string
}

func newGenericBackgroundAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, systemPrompt string, userMessage string) *GenericBackgroundAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionQAAgentType,
		eventBridge,
	)
	return &GenericBackgroundAgent{
		BaseOrchestratorAgent: baseAgent,
		systemPrompt:          systemPrompt,
		userMessage:           userMessage,
	}
}

func (agent *GenericBackgroundAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}

	inputProcessor := func(map[string]string) string { return agent.userMessage }

	result, updatedHistory, err := agent.ExecuteWithTemplateValidation(
		ctx, templateVars, inputProcessor,
		conversationHistory, struct{}{},
		agent.systemPrompt, true,
	)
	if err != nil {
		return "", nil, err
	}

	return result, updatedHistory, nil
}

// updateWorkshopLearningMetadata updates .learning_metadata.json after workshop generate_learnings completes.
// This ensures tiered mode and keepLearningFull thresholds work for workshop-generated learnings.
func (iwm *InteractiveWorkshopManager) updateWorkshopLearningMetadata(
	ctx context.Context,
	learningPathIdentifier string,
	stepPath string,
	stepID string,
	hasLearningFiles bool,
) {
	logger := iwm.controller.GetLogger()
	learningsBase := iwm.controller.getLearningsBasePath()
	metadataPath := fmt.Sprintf("%s/%s/.learning_metadata.json", learningsBase, learningPathIdentifier)

	// Read existing metadata or create new
	var metadata LearningMetadata
	content, err := iwm.controller.ReadWorkspaceFile(ctx, metadataPath)
	if err != nil {
		metadata = LearningMetadata{
			StepID:   stepID,
			StepPath: stepPath,
		}
	} else {
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to parse learning metadata for %s: %v (creating new)", stepID, err))
			metadata = LearningMetadata{
				StepID:   stepID,
				StepPath: stepPath,
			}
		}
	}

	// Update fields
	metadata.TotalIterations++
	if hasLearningFiles {
		metadata.LastLearningDetectedAt = time.Now().Format(time.RFC3339)
		metadata.LastDetectionReasoning = "workshop generate_learnings"
		metadata.LastDetectionConfidence = 1.0
	}

	// Write updated metadata
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to marshal learning metadata for %s: %v", stepID, err))
		return
	}
	if err := iwm.controller.WriteWorkspaceFile(ctx, metadataPath, string(metadataJSON)); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to write learning metadata for %s: %v", stepID, err))
	} else {
		logger.Info(fmt.Sprintf("📝 Updated learning metadata for %s (iterations: %d)", stepID, metadata.TotalIterations))
	}
}
