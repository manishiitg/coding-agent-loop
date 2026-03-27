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

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/agent/prompt"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/instructions"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/shared"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	"mcp-agent-builder-go/agent_go/pkg/skills"

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

	// Count iterations and collect sub-agent paths + tier usage from JSONL
	lines := strings.Split(strings.TrimSpace(content), "\n")
	iterations := 0
	var subAgentPaths []string
	seen := make(map[string]bool)
	var lastEntry map[string]interface{} // retain last parsed entry for todo progress

	// Track tier usage per route/generic agent for optimization analysis
	type tierUsageEntry struct {
		RouteID   string
		RouteName string
		TodoID    string
		TodoTitle string
		Tier      int
		TierLabel string
		IsGeneric bool
	}
	var tierUsageLog []tierUsageEntry

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

		// Extract tier usage data for todo_task steps
		if stepType == StepTypeTodoTask {
			nextAction, _ := response["next_action"].(string)
			if nextAction == "delegate" {
				tierNum := 0
				if t, ok := response["preferred_tier"].(float64); ok {
					tierNum = int(t)
				}
				tierLabel, _ := response["preferred_tier_label"].(string)
				routeID, _ := response["selected_route_id"].(string)
				routeName, _ := response["selected_route_name"].(string)
				todoID, _ := response["todo_id_to_execute"].(string)
				todoTitle, _ := response["todo_title"].(string)
				isGeneric, _ := response["use_generic_agent"].(bool)

				tierUsageLog = append(tierUsageLog, tierUsageEntry{
					RouteID:   routeID,
					RouteName: routeName,
					TodoID:    todoID,
					TodoTitle: todoTitle,
					Tier:      tierNum,
					TierLabel: tierLabel,
					IsGeneric: isGeneric,
				})
			}
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

	// Append tier usage summary for todo_task steps
	if len(tierUsageLog) > 0 {
		summary.WriteString("\n\nTier Usage per Sub-Agent:\n")
		summary.WriteString("| Route/Agent | Todo | Tier | Label |\n")
		summary.WriteString("| :--- | :--- | :--- | :--- |\n")
		for _, tu := range tierUsageLog {
			agentName := tu.RouteName
			if agentName == "" && tu.IsGeneric {
				agentName = "generic-agent"
			} else if agentName == "" {
				agentName = tu.RouteID
			}
			tierStr := "auto"
			if tu.Tier > 0 {
				tierStr = fmt.Sprintf("%d", tu.Tier)
			}
			tierLabel := tu.TierLabel
			if tierLabel == "" {
				tierLabel = "auto-selected"
			}
			summary.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", agentName, tu.TodoTitle, tierStr, tierLabel))
		}
	}

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

// CancelAll cancels all running executions and returns the list of cancelled execution IDs.
func (r *WorkshopStepRegistry) CancelAll() []string {
	r.mu.RLock()
	// Collect running executions
	var toCancel []*WorkshopStepExecution
	for _, exec := range r.executions {
		exec.mu.RLock()
		isRunning := exec.Status == WorkshopStepRunning
		exec.mu.RUnlock()
		if isRunning {
			toCancel = append(toCancel, exec)
		}
	}
	r.mu.RUnlock()

	var cancelledIDs []string
	for _, exec := range toCancel {
		if exec.cancel != nil {
			exec.cancel()
		}
		exec.mu.Lock()
		exec.Status = WorkshopStepCancelled
		exec.mu.Unlock()
		cancelledIDs = append(cancelledIDs, exec.ID+" (step: "+exec.StepID+")")
	}
	return cancelledIDs
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

// WorkshopExecutionNotifier is called when workshop step/background executions start and complete.
// Implemented by the server layer to register executions in bgAgentRegistry so that
// HasRunningAgents() returns true and the frontend keeps polling for events.
type WorkshopExecutionNotifier interface {
	OnExecutionStart(execID, name string)
	OnExecutionComplete(execID, name, result string, err error)
	OnExecutionTerminated(execID, name string) // explicit cancellation via stop_step/stop_all
}

// ============================================================================
// InteractiveWorkshopManager
// ============================================================================

// InteractiveWorkshopManager manages the interactive workshop phase
type InteractiveWorkshopManager struct {
	controller           *StepBasedWorkflowOrchestrator
	presetLLM            *AgentLLMConfig
	sessionID            string
	workflowID           string
	stepRegistry         *WorkshopStepRegistry
	sessionCtx           context.Context                             // long-lived ctx for background goroutines
	toolCallQueryFunc    ToolCallQueryFunc                           // optional: query live tool calls for running steps
	mainSessionID        string                                      // event store session ID for tool call queries
	schedulerWorkspacePath string                                    // workspace path for schedule management
	schedulerFuncs       *SchedulerCallbacks                         // schedule CRUD callbacks from server.go
	skillFuncs           *SkillCallbacks                             // skill import/delete callbacks from server.go
	listAvailableSecrets func(ctx context.Context) ([]string, error) // list all available secret names
	executionNotifier    WorkshopExecutionNotifier                   // optional: notifies server when executions start/complete
}

// persistWorkflowConfigToManifest writes the current in-memory workflow config
// (servers, skills, secrets) back to workflow.json so changes survive session end.
func (iwm *InteractiveWorkshopManager) persistWorkflowConfigToManifest(ctx context.Context, logger loggerv2.Logger) {
	wsPath := iwm.controller.GetWorkspacePath()
	if wsPath == "" {
		return
	}
	manifestPath := "workflow.json"

	// Read existing manifest
	content, err := iwm.controller.ReadWorkspaceFile(ctx, manifestPath)
	if err != nil {
		logger.Info("No workflow.json found — skipping manifest persist")
		return
	}

	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		logger.Warn(fmt.Sprintf("Failed to parse workflow.json: %v", err))
		return
	}

	// Get or create capabilities object
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		caps = make(map[string]interface{})
	}

	// Update from current controller state
	caps["selected_servers"] = iwm.controller.GetSelectedServers()
	caps["selected_skills"] = iwm.controller.GetSelectedSkills()

	// Update secrets (names only, never values)
	secretNames := make([]string, 0)
	for _, s := range iwm.controller.GetSecrets() {
		if s.Name != "" {
			secretNames = append(secretNames, s.Name)
		}
	}
	caps["selected_global_secret_names"] = secretNames

	caps["use_code_execution_mode"] = iwm.controller.GetUseCodeExecutionMode()
	caps["use_tool_search_mode"] = iwm.controller.GetUseToolSearchMode()

	manifest["capabilities"] = caps
	manifest["updated_at"] = time.Now().UTC().Format(time.RFC3339)

	// Write back
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to marshal workflow.json: %v", err))
		return
	}

	if err := iwm.controller.WriteWorkspaceFile(ctx, manifestPath, string(updated)); err != nil {
		logger.Warn(fmt.Sprintf("Failed to write workflow.json: %v", err))
		return
	}

	logger.Info("✅ Persisted workflow config changes to workflow.json")
}

// refreshVariablesManifest reloads the variables manifest from file into the controller.
// Call this before using iwm.controller.variablesManifest to avoid stale data.
func (iwm *InteractiveWorkshopManager) refreshVariablesManifest(ctx context.Context) {
	manifest, err := readVariablesFromFile(ctx, iwm.controller.GetWorkspacePath(), func(ctx context.Context, path string) (string, error) {
		return iwm.controller.ReadWorkspaceFile(ctx, path)
	})
	if err == nil && manifest != nil {
		iwm.controller.variablesManifest = manifest
	}
}

// workshopSubAgentNotifier implements SubAgentNotifier by registering todo task
// sub-agents into the workshop stepRegistry so query_step can find them.
type workshopSubAgentNotifier struct {
	registry *WorkshopStepRegistry
}

func (n *workshopSubAgentNotifier) OnSubAgentStart(agentID, name string) {
	exec := &WorkshopStepExecution{
		ID:     agentID,
		StepID: name,
		Status: WorkshopStepRunning,
	}
	n.registry.Register(exec)
}

func (n *workshopSubAgentNotifier) OnSubAgentComplete(agentID, name, result string, err error) {
	exec := n.registry.Get(agentID)
	if exec == nil {
		return
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if err != nil {
		exec.Status = WorkshopStepFailed
		exec.Err = err
	} else {
		exec.Status = WorkshopStepDone
		exec.Result = result
	}
}

// NewInteractiveWorkshopManager creates a new InteractiveWorkshopManager
func NewInteractiveWorkshopManager(
	controller *StepBasedWorkflowOrchestrator,
	presetLLM *AgentLLMConfig,
	sessionID string,
	workflowID string,
) *InteractiveWorkshopManager {
	registry := newWorkshopStepRegistry()
	// Wire sub-agent notifier so todo task sub-agents appear in stepRegistry
	// and are queryable via query_step
	controller.SetSubAgentNotifier(&workshopSubAgentNotifier{registry: registry})
	return &InteractiveWorkshopManager{
		controller:   controller,
		presetLLM:    presetLLM,
		sessionID:    sessionID,
		workflowID:   workflowID,
		stepRegistry: registry,
	}
}

// SetToolCallQuery configures the live tool call query capability.
// mainSessionID is the event store session ID; queryFunc queries tool calls by correlation ID.
func (iwm *InteractiveWorkshopManager) SetToolCallQuery(mainSessionID string, queryFunc ToolCallQueryFunc) {
	iwm.mainSessionID = mainSessionID
	iwm.toolCallQueryFunc = queryFunc
}

// GetToolsForWorkshopMode returns the list of tool names that should be available
// for the given workshop mode. This is used with Agent.SetToolAllowList() to dynamically
// restrict tools per-turn as the user switches modes from the frontend.
//
// Tools are grouped into categories:
//   - System tools: always included (shell, workspace, human feedback, virtual tools)
//   - Workshop execution tools: execute_step, query_step, stop, list, run_in_background
//   - Step config tools: update_step_config, analyze_step, generate_learnings, optimize_step
//   - Plan modification tools: add/update/delete steps, branches, routes
//   - Variable/config tools: update_variable, groups, workflow config
//   - Schedule tools: list/create/update/delete schedules
//   - Skill tools: list/search/install/uninstall skills
//   - Eval tools: validate_evaluation_plan, run_full_evaluation
//   - Report tools: validate_report_plan, run_full_report
func GetToolsForWorkshopMode(mode string) []string {
	// System tools — always available regardless of mode.
	// Includes workspace, shell, virtual tools, and human feedback.
	system := []string{
		// Workspace basic tools
		"list_workspace_files", "read_workspace_file", "update_workspace_file",
		"delete_workspace_file", "move_workspace_file",
		// Workspace advanced tools
		"execute_shell_command", "diff_patch_workspace_file",
		// Human tools
		"human_feedback",
		// Browser (if registered)
		"agent_browser",
		// mcpagent virtual tools (get_api_spec, get_prompt, get_resource)
		"get_api_spec", "get_prompt", "get_resource",
		// Code execution virtual tools
		"write_code", "discover_code_files",
	}

	// Read-only info tools — safe in all modes
	readOnly := []string{
		"get_step_prompts", "get_workflow_config", "get_llm_config", "get_cost_summary",
	}

	// Workshop execution tools
	execution := []string{
		"execute_step", "query_step", "stop_step", "stop_all_executions",
		"list_executions", "run_in_background",
	}

	// Step config & analysis tools
	stepConfig := []string{
		"update_step_config", "analyze_step", "generate_learnings", "optimize_step",
	}

	// Plan modification tools
	planMod := []string{
		"add_regular_step", "add_decision_step", "add_routing_step",
		"add_human_input_step", "add_todo_task_step", "add_todo_task_route",
		"update_regular_step", "update_decision_step", "update_routing_step",
		"update_human_input_step", "update_todo_task_step", "update_todo_task_route",
		"delete_todo_task_route", "delete_plan_steps",
		"update_success_criteria", "update_validation_schema",
		"publish_workflow_version", "restore_workflow_version",
	}

	// Variable & config tools
	variableConfig := []string{
		"update_variable", "add_group", "update_group", "delete_group",
		"update_workflow_config",
	}

	// Schedule tools
	schedule := []string{
		"create_schedule", "update_schedule",
		"delete_schedule", "trigger_schedule", "get_schedule_runs",
	}

	// Skill tools
	skills := []string{
		"list_skills", "search_skills", "install_skill", "uninstall_skill", "import_skill",
	}

	// Eval tools
	eval := []string{
		"validate_evaluation_plan", "run_full_evaluation",
	}

	// Report tools
	report := []string{
		"validate_report_plan", "run_full_report",
	}

	var tools []string
	tools = append(tools, system...)
	tools = append(tools, readOnly...)

	switch mode {
	case "builder":
		// BUILD: design workflow, create/modify steps, test execution, configure
		tools = append(tools, execution...)
		tools = append(tools, planMod...)
		tools = append(tools, variableConfig...)
		tools = append(tools, schedule...)
		tools = append(tools, skills...)
		// analyze_step & update_step_config for testing, but no optimize_step/generate_learnings
		tools = append(tools, "update_step_config", "analyze_step")

	case "optimizer":
		// OPTIMIZE: run steps, analyze, optimize, update config — no structural plan changes
		tools = append(tools, execution...)
		tools = append(tools, stepConfig...)
		// Allow validation schema updates during optimization
		tools = append(tools, "update_success_criteria", "update_validation_schema")
		tools = append(tools, "debug_step")
		tools = append(tools, "run_full_workflow")

	case "debugger":
		// DEBUG: read-only analysis of past runs — no execution, no plan changes
		// get_step_prompts already included via readOnly (all modes)
		tools = append(tools, "analyze_step", "debug_step")
		tools = append(tools, "query_step", "list_executions")

	case "runner":
		// RUN: execute optimized steps and report — no plan changes, no optimization, no config changes
		tools = append(tools, execution...)
		tools = append(tools, "run_full_workflow")

	case "eval":
		// EVAL: build and run evaluations — no plan or step changes
		tools = append(tools, eval...)
		tools = append(tools, "query_step", "list_executions")

	case "output":
		// OUTPUT/REPORT: design and run the final report artifact
		tools = append(tools, report...)

	default:
		// Unknown mode — allow everything (no restriction)
		tools = append(tools, execution...)
		tools = append(tools, stepConfig...)
		tools = append(tools, planMod...)
		tools = append(tools, variableConfig...)
		tools = append(tools, schedule...)
		tools = append(tools, skills...)
		tools = append(tools, eval...)
		tools = append(tools, report...)
		tools = append(tools, "debug_step")
	}

	return tools
}

// detectWorkshopMode determines the current workshop mode based on step optimization state.
// Returns the mode ("builder", "optimizer", "runner") and a comma-separated list of unoptimized step IDs.
func detectWorkshopMode(plan *PlanningResponse, stepConfigs []StepConfig) (string, string) {
	if plan == nil || len(plan.Steps) == 0 {
		return "builder", ""
	}

	// Build a set of optimized step IDs from step configs
	optimizedSet := make(map[string]bool)
	for _, sc := range stepConfigs {
		if sc.AgentConfigs != nil && sc.AgentConfigs.Optimized != nil && *sc.AgentConfigs.Optimized {
			optimizedSet[sc.ID] = true
		}
	}

	// Count optimized vs total steps, collect unoptimized step IDs
	totalSteps := len(plan.Steps)
	optimizedCount := 0
	var unoptimized []string
	for _, step := range plan.Steps {
		if optimizedSet[step.GetID()] {
			optimizedCount++
		} else {
			unoptimized = append(unoptimized, step.GetID())
		}
	}

	unoptimizedList := strings.Join(unoptimized, ", ")

	if optimizedCount == 0 {
		return "builder", unoptimizedList
	} else if optimizedCount >= totalSteps {
		return "runner", ""
	}
	return "optimizer", unoptimizedList
}

// InteractiveWorkshopOnly runs the interactive workshop phase
func (iwm *InteractiveWorkshopManager) InteractiveWorkshopOnly(ctx context.Context, workspacePath string, runFolder string) (string, error) {
	iwm.controller.GetLogger().Info(fmt.Sprintf("🔧 Starting Workflow Builder for workspace: %s", workspacePath))

	// Store session context so background goroutines outlive individual tool call contexts
	iwm.sessionCtx = ctx

	// Set workspace path
	iwm.controller.SetWorkspacePath(workspacePath)

	// Use the run folder passed from the frontend toolbar selection (if any).
	// If empty, leave selectedRunFolder unset — the LLM will run 'ls runs/' and
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

	// Auto-detect workshop mode based on step optimization state
	workshopMode, unoptimizedSteps := detectWorkshopMode(iwm.controller.approvedPlan, stepConfigs)
	iwm.controller.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Auto-detected mode: %s (unoptimized: %v)", workshopMode, unoptimizedSteps))

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
		"WorkspacePath":       workspacePath,
		"RunFolder":           iwm.controller.selectedRunFolder,
		"PlanJSON":            planContent,
		"StepConfigSummary":   stepConfigSummary,
		"IsCodeExecutionMode": fmt.Sprintf("%v", agent.GetConfig().UseCodeExecutionMode),
		"UseToolSearchMode":   fmt.Sprintf("%v", agent.GetConfig().UseToolSearchMode),
		"WorkshopMode":        workshopMode,
		"UnoptimizedSteps":    unoptimizedSteps,
		"ProgressSummary":     progressSummary,
		"UserRequest":         userGoal,
		"SessionID":           iwm.sessionID,
		"WorkflowID":          iwm.workflowID,
		"UseKnowledgebase":    useKB,
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
		"Chats", // Allow reading chat history for context
	}
	// Write to full workspace — the workshop agent and its background agents need to write
	// to learnings, knowledgebase, execution, memory/, and other workspace files.
	// Plan tools also write to planning/ via workspace API (bypass guard).
	writePaths := []string{
		workspacePath,
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
	// Tool search and code execution are mutually exclusive — don't show both
	config.UseToolSearchMode = !config.UseCodeExecutionMode

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

You are the intelligent orchestrator of an automated workflow system. Workflow steps are executed by smaller, cheaper LLM agents that follow instructions narrowly. Your role — running on a more capable model — is to design the workflow, run and monitor steps, diagnose failures, and encode what you learn into step instructions and learnings so the execution agents can reliably succeed. Think of yourself as the senior engineer; the step agents are junior engineers who need clear, specific guidance.

## CURRENT MODE: {{if eq .WorkshopMode "builder"}}BUILD{{else if eq .WorkshopMode "optimizer"}}OPTIMIZE{{else if eq .WorkshopMode "debugger"}}DEBUG{{else if eq .WorkshopMode "eval"}}EVAL{{else if eq .WorkshopMode "output"}}OUTPUT{{else}}RUN{{end}}
{{if eq .WorkshopMode "builder"}}
**BUILD MODE** — Focus on designing and building the workflow. Get steps to work correctly.
- **Do NOT create steps until the plan is fully clear.** The user may be exploring, testing ideas, or not yet ready to commit to a plan. First discuss the approach, ask clarifying questions, and confirm the plan with the user. Only create steps after the user explicitly confirms the plan or asks you to create/add steps.
- When the user describes what they want, respond with a proposed plan (step breakdown, types, context flow) and ask for confirmation before creating any steps in plan.json
- If the user is just asking questions, brainstorming, or exploring possibilities, engage in discussion — do NOT jump to creating steps
- Add, remove, reorder, and configure steps freely once the plan is confirmed
- Test steps to verify they produce correct output — use execute_step(step_id) with default skip_learning=true for fast iteration
- Set up servers, tools, and context dependencies
- Do NOT worry about optimization yet — the workflow structure may still change
- Do NOT mark steps as optimized or run optimize_step — premature optimization wastes effort on steps that may be restructured
- Generate learnings only when a step is working correctly and the user explicitly asks for it
{{else if eq .WorkshopMode "optimizer"}}
**OPTIMIZE MODE** — The workflow structure is set. Your job is to make every step reliable and efficient.
{{if .UnoptimizedSteps}}- **Steps not yet optimized**: {{.UnoptimizedSteps}}{{end}}

**IMPORTANT: Optimize ONE step at a time.** Do NOT batch multiple steps. Focus entirely on a single step — run it, review, fix, verify, mark optimized — then ask the user which step to work on next. This gives the user control over the order and lets them review each step's optimization before moving on.

**Optimization workflow (one step at a time):**
1. **Ask which step** — If the user hasn't specified a step, show the unoptimized steps list and ask which one to optimize next.
2. **Run the step** — execute_step(step_id, skip_learning=false) so the step learns from its execution. Wait for auto-notification.
3. **Review tool usage** — When the step completes, check the execution result:
   - How many tool calls did it make? Are there unnecessary or redundant calls?
   - Did it search for tools that should already be configured? (wasted turns)
   - Did it call the wrong server/tool names? (stale learnings)
   - Could the same result be achieved with fewer tool calls?
4. **Review and fix learnings** — Read learnings: cat learnings/{step-id}/SKILL.md
   - Do they reference the correct server/tool names matching the step config?
   - Are they guiding the agent to use the minimum number of tool calls?
   - Are they specific enough to prevent exploration/guessing?
   - Fix with diff_patch_workspace_file. Lock after editing: update_step_config(step_id, lock_learnings=true)
5. **Review and fix description** — Is the step description precise enough?
   - Does it tell the agent exactly WHAT to do and HOW?
   - Could the agent misinterpret it and waste turns exploring?
   - Update via plan modification tools if needed.
6. **Ensure validation schema exists** — Check if the step has a validation_schema. If not, add one with update_validation_schema.
7. **Re-run and verify** — execute_step(step_id) again. Check that:
   - No wasted tool calls (minimum necessary calls only)
   - Learnings guided the agent correctly
   - Output passes validation
8. **Mark optimized** — When the step has **at least 3 successful runs** (check successful_runs in step_config) and runs cleanly: update_step_config(step_id, optimized=true)
   - Setting optimized=true also locks learnings automatically (they always move together)
   - **Cost impact**: optimized steps automatically use **lower-cost LLM tiers** at runtime for execution agents — execution agents drop to Tier 3 (Low). Todo-task orchestrators still stay on Tier 1 (High) unless explicitly overridden.
   - The system auto-sets optimized=true after 3 successful validations, but you can also set it manually after verifying quality
   - If an optimized step starts failing later: update_step_config(step_id, optimized=false) — reverts to higher tiers and unlocks learnings for rework
9. **Report and ask** — Tell the user the step is optimized and ask which step to work on next.

**For todo_task steps (sub-agent steps):**
Todo task steps contain inner sub-agents (routes). Optimize each sub-agent individually BEFORE optimizing the parent:
1. **Run each sub-agent separately** — execute_step(sub_agent_step_id) to test it in isolation
2. **Optimize each sub-agent** — follow the workflow above for each sub-agent one at a time
3. **Mark each sub-agent optimized** independently
4. **Then optimize the parent todo_task** — ensure the orchestrator description has clear instructions for routing to sub-agents. The orchestrator should NOT duplicate sub-agent logic — it should just dispatch tasks to the right route. Do NOT modify tasks.md directly — tasks.md is auto-generated by the orchestrator agent at runtime. Only update the step description and learnings.
5. **Optimize tier selection** — After running the full todo_task, review optimize_step output for tier analysis. Add a TIER RECOMMENDATIONS section to the orchestration SKILL.md with per-route tier assignments (1=High for complex, 2=Medium for routine, 3=Low for simple). The orchestrator reads this at runtime to pick the right LLM tier for each sub-agent.
6. **Run the full todo_task** — execute_step(parent_step_id) to verify the orchestrator + all sub-agents work together end-to-end

**Code execution mode — prefer it for most steps:**
Steps that make multiple MCP tool calls should use **code execution mode** (update_step_config(step_id, use_code_execution_mode=true)). In code exec mode, the agent writes a single Python script that calls MCP tools via HTTP, which is:
- **More reliable** — one script with error handling instead of multiple independent tool calls
- **Faster** — fewer LLM turns (one script vs. multiple tool-call-then-parse cycles)
- **More deterministic** — the same script runs the same way every time
- **Easier to optimize** — learnings can include the exact working script (saved in learnings/{step-id}/code/)

When reviewing a step, check: if it makes 2+ MCP tool calls, switch it to code exec mode. The learning should then include the full working Python script so the agent can reuse it directly on future runs.

**Exception — browser automation steps:** Do NOT use code execution mode for steps that use browser tools (browser_snapshot, browser_click, browser_navigate, agent_browser, Playwright). Browser automation requires the LLM to react to page state between each action (read snapshot → decide next click → read result). A Python script cannot do this — it needs the LLM in the loop for every browser interaction.

**Goal**: Each step should execute with the **fewest possible tool calls and LLM turns** — no exploration, no wrong server names, no retries. Prefer code execution mode with a single comprehensive Python script. The learnings and description should be precise enough that the agent gets it right on the first try.

If structural changes are needed (add/remove steps), ask the user to switch to Build mode.
{{else if eq .WorkshopMode "debugger"}}
**DEBUG MODE** — Investigate existing runs without re-executing. Analyze what happened and why.

**What to do in debug mode:**
- Use **optimize_step(step_id)** to analyze an existing run — it reads execution logs, system prompts, conversation history, tool usage, and learnings, then returns a detailed analysis with fix suggestions.
- Use **debug_step(step_id)** for deeper analysis if available.
- Read execution output files directly: cat runs/{run_folder}/execution/{step}/output.json
- Read learnings: cat learnings/{step-id}/SKILL.md
- Compare step config against learnings — check for stale server/tool names.
- Check validation logs: cat runs/{run_folder}/logs/{step}/*.json

**DO NOT run steps in debug mode.** Debug mode is for analysis only. If you need to re-run, ask the user to switch to Optimize or Build mode.
{{else if eq .WorkshopMode "eval"}}
**EVAL MODE** — Build and run evaluation plans to measure workflow quality.

**Evaluation workflow:**
1. Edit `+"`evaluation/evaluation_plan.json`"+` directly using shell/file tools.
2. Keep each eval step focused on one execution concern with a clear `+"`id`"+`, `+"`title`"+`, `+"`description`"+`, and `+"`success_criteria`"+`.
3. After editing, run **validate_evaluation_plan** to confirm the JSON parses and the eval step schema is acceptable.
4. Use **pre_validation** on eval steps when the generated artifacts need concrete file checks before scoring.
5. Use **run_full_evaluation(target_run_folder)** to score the current eval plan against a specific execution run.
6. Review the evaluation report: `+"`cat evaluation/runs/{run_folder}/evaluation_report.json`"+`. Low scores (< 5) usually mean the step output is weak or the eval criteria need tightening.
7. Iterate by refining `+"`evaluation/evaluation_plan.json`"+` or switching to Build/Optimize mode if the execution workflow itself needs changes.

**Evaluation files:**
- Plan: evaluation/evaluation_plan.json
- Reports: evaluation/runs/{runFolder}/evaluation_report.json
- Learnings: evaluation/learnings/{stepID}/

Do NOT modify execution steps or plan.json in eval mode — focus only on evaluation design and scoring. Switch to Build mode for workflow changes.
{{else if eq .WorkshopMode "output"}}
**REPORT MODE** — Design the final workflow report artifact that is generated automatically after a workflow group run completes.

**Report workflow:**
1. Edit `+"`planning/output_plan.json`"+` directly using shell/file tools.
2. Keep the file in the single-step report-plan shape — one `+"`step`"+` object, not a `+"`steps`"+` array.
3. After editing, run **validate_report_plan** to confirm the JSON is valid and the report step shape is acceptable.
4. Use **pre_validation** on the report step when the final markdown must satisfy concrete file checks.
5. Keep the report focused on human review: what happened, what succeeded, what failed, what was produced, and what should be reviewed later.
6. If the user wants lightweight visuals in the markdown report, ask for fenced `+"`chart`"+` blocks with JSON data using supported types `+"`bar`"+` or `+"`line`"+`.
7. Use **run_full_report(target_run_folder)** to manually regenerate the report for an existing completed group run after you update the report definition.

**Report files:**
- Plan: `+"`planning/output_plan.json`"+`
- Generated artifact per group run: `+"`runs/{iteration}/{group}/final_output.md`"+` (or the configured markdown filename)

Do NOT modify execution steps or evaluation steps in output mode unless the user explicitly asks to switch contexts. Focus on the final markdown artifact definition only.
{{else}}
**RUN MODE** — All steps are optimized. Execute and report results.
- Run steps with execute_step(step_id) using default skip_learning=true — learnings are already locked
- Report results concisely
- If a step fails or produces incorrect output, reset its optimized flag (update_step_config(step_id, optimized=false)) and investigate
- Do NOT make structural changes to the plan in this mode
- If issues require optimization, tell the user you are switching to optimize mode
{{end}}

## CURRENT STATE

- **Workspace**: {{.WorkspacePath}} (`+"`/app/workspace-docs/{{.WorkspacePath}}/`"+`)
- **Run Folder**: {{.RunFolder}}
- **Step Configs**: {{if .StepConfigSummary}}{{.StepConfigSummary}}{{else}}No step configs yet{{end}}
- **Progress**: {{if .ProgressSummary}}{{.ProgressSummary}}{{else}}No progress tracked yet{{end}}

{{if eq .WorkshopMode "output"}}
### Current Report Plan
Use `+"`cat planning/output_plan.json`"+` to inspect the current report definition.
{{else}}
{{if and .StepSummary (ne .WorkshopMode "output")}}### Plan Steps
{{.StepSummary}}
{{end}}
{{if .PlanJSON}}`+"```json\n{{.PlanJSON}}\n```"+`{{else}}Do NOT dump the full `+"`planning/plan.json`"+` by default. Read it precisely with targeted `+"`jq`"+` queries. The structure is: root `+"`steps[]`"+` for top-level steps, with nested step containers in `+"`if_true_steps`"+`, `+"`if_false_steps`"+`, `+"`decision_step`"+`, `+"`todo_task_step`"+`, `+"`predefined_routes[].sub_agent_step`"+`, `+"`orchestration_step`"+`, and `+"`orchestration_routes[].sub_agent_step`"+`.

Use `+"`execute_shell_command`"+` with focused queries like:
- **Top-level overview only**: `+"`jq '[.steps[] | {id, title, type}]' planning/plan.json`"+`
- **Single step by `+"`step_id`"+` anywhere in the plan**: `+"`jq --arg sid \"step-id\" '.. | objects | select(.id? == $sid)' planning/plan.json`"+`
- **Only the fields you need from one step**: `+"`jq --arg sid \"step-id\" '.. | objects | select(.id? == $sid) | {id, title, type, description, success_criteria, context_output}' planning/plan.json`"+`
- **Inspect only route structure for a todo/orchestration step**: `+"`jq --arg sid \"step-id\" '.. | objects | select(.id? == $sid) | {id, type, predefined_routes, orchestration_routes}' planning/plan.json`"+`

Use `+"`cat planning/plan.json`"+` only when you genuinely need the entire file.{{end}}
{{end}}

{{if eq .WorkshopMode "builder"}}
## PLAN DESIGN — From Requirements to Steps

When a user describes what they want to automate, follow this process to design the plan. **Present the plan to the user and get explicit confirmation before creating any steps.** The user may be exploring or testing ideas — do not assume they are ready to commit to a workflow structure.

### Step 1: Identify the Core Actions
Break the user's requirement into **concrete actions** — things an agent must actually DO (navigate, extract, write, validate, etc.). Each action that produces a distinct output or changes state is a candidate for a step.

**Granularity rule**: A step should do ONE logical thing. If you need to explain a step with "and then also..." — split it. But don't split trivially (e.g., "open file" and "read file" should be one step).

### Step 2: Choose the Right Step Type

| Scenario | Step Type | Why |
|----------|-----------|-----|
| Agent performs a task and writes output | **Regular** | Simplest type — one agent, one output |
| Task has multiple known sub-tasks that repeat | **Todo Task** (sub-workflow/pipeline) with sub-agents | Each sub-task gets its own learning, validation, and tools |
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
### Step 4: When to Use Todo Task (Sub-Workflow / Pipeline) with Sub-Agents

**Note:** Users may refer to todo_task steps as "sub-workflows" or "pipelines", and to the routes/sub-agent steps within them as "sub-agents". These are the same concept.

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

### Step Types Reference

- **Regular** (type: "regular"): Standard task. Executes an agent that produces a context_output file.
- **Decision** (type: "decision"): Executes a step, then branches based on evidence in context. Contains **if_true_steps** and **if_false_steps**.
- **Conditional** (type: "conditional"): Inspection-only branch (no execution). Contains **if_true_steps** and **if_false_steps** — evaluated based on prior context.
- **Todo Task / Sub-Workflow** (type: "todo_task"): Manages a dynamic todo list. Has a **todo_task_step** (orchestrator) and **predefined_routes** — each route has a **sub_agent_step** (inner step with its own description, tools, and learning).
- **Routing / Orchestration** (type: "routing"): N-way LLM-based routing. Has an **orchestration_step** and **orchestration_routes** — each route has a **sub_agent_step**.
- **Human Input** (type: "human_input"): Asks a question to the user and blocks until response. Supports: 'text', 'yesno', 'multiple_choice'. Can route based on response.
- **Loop** (type: "loop"): Repeat until criteria met (polled progress).
- **Orphan** (is_orphan: true): Not part of the main execution flow — only triggered manually via execute_step in the workshop. Use orphan steps as **reusable utility agents** for the builder: data checks, environment validation, one-off investigations, or any task you want to run on-demand without adding it to the main workflow sequence.

### Inner Steps
Inner steps live inside conditional branches, orchestration routes, or todo_task routes. They have their own step IDs and can be individually executed, analyzed, and configured via **execute_step**, **analyze_step**, **update_step_config** using the inner step ID.
{{end}}

## RUNNING STEPS

### Iterations & Groups
**Iterations** are just output folders (e.g., iteration-0). The system defaults to **iteration-0** — do NOT pass iteration unless the user explicitly requests a different one. Every execute_step re-reads the **latest** plan.json — no caching or snapshotting.

**Groups**: Before running a step, read `+"`cat variables.json`"+` to find available group_ids. Call execute_step with the correct **group_id**. Never guess the group_id — always read variables.json.

### Execution Procedure
1. User says "run step-X" → determine group → call **execute_step("step-id", group_id=group_id)** → get execution_id
2. By default, execute_step runs with **skip_learning=true** for faster iteration. Pass skip_learning=false to generate learnings.
3. **Human input steps**: Pass **human_input** parameter with the appropriate answer from your conversation context. This prevents blocking for manual UI input.
4. Tell user step is running. Move on to other work or wait for the auto-notification.
5. When the notification arrives — respond based on the current mode:
{{if eq .WorkshopMode "builder"}}   - ✅ If success: briefly tell user the result. Confirm it works and ask what to do next.
   - ❌ If failed: report the error clearly. Investigate the root cause, fix the step description or config, then re-run.
{{else if eq .WorkshopMode "optimizer"}}   - ✅ If success AND step is not yet optimized: briefly tell user the result, then call **optimize_step(step_id)** to review in background. When done, apply fixes and mark optimized.
   - ✅ If success AND step is already optimized: briefly report success and move to next unoptimized step.
   - ❌ If failed: reset optimized flag (update_step_config(step_id, optimized=false)), call optimize_step(step_id), apply fixes and re-run.
{{else}}   - ✅ If success: briefly report the result. Move to the next step.
   - ❌ If failed: report the error. Reset the step optimized flag and investigate.
{{end}}
6. **ALWAYS follow up** after execution. Never fire-and-forget.

### Auto-Notification System
All background agents (execute_step, optimize_step, generate_learnings) **automatically notify you** when they complete:
- Notifications arrive as messages prefixed with **[AUTO-NOTIFICATION]** — they are **system-generated, NOT from the user**. Do not treat them as user requests.
- **Do NOT poll** with query_step in a loop or ask the user when something finishes — the system handles this.
- **Notifications may be delayed** — they can arrive after you've moved on or the user has changed the plan. Always check whether a notification is still relevant to the **current** context before acting on it.
- Use **query_step** for a live status check — it shows which tools the step is calling in real time.

### Stopping Tasks
When the user asks you to "stop", "cancel", or "abort" running tasks, you MUST call **stop_all_executions()** or **stop_step(execution_id)**. Simply responding with text does NOT stop anything — tasks run independently in the background.

## DEBUGGING

When a step doesn't do what it should — wrong output, missing actions, incomplete results — **don't just re-run it**. You have a smarter model — use it to investigate.

**When a step is stuck or repeatedly failing**, run the task yourself using the same tools the step agent would use, figure out what works, then update the step's learnings and instructions with the correct approach.

**Investigation workflow:**
1. Call **optimize_step(step_id)** — runs a background agent that reads conversation history, prompts, validation results, learnings, and tool usage. Returns detailed analysis with fix suggestions.
2. While it runs, continue other work. You'll be auto-notified when done.
3. Review suggestions and **apply the fixes immediately**.

**Root cause → Fix mapping:**
- **Agent didn't attempt the task** → Step description is unclear. Rewrite it.
- **Agent used wrong approach** → Description missing constraints. Add HOW instructions.
- **Agent missed fields/data** → Update validation_schema and clarify output structure.
- **Agent couldn't find data from previous steps** → Fix context_dependencies chain.
- **Validation rejected correct output** → Schema or success_criteria too strict. Update them.
- **Agent wasted turns on irrelevant tool calls** → Description too vague. Tighten it.

**The fix should be one of:** update step description (most common), update validation_schema, update success_criteria, fix context dependencies, or edit/delete learnings.

**CRITICAL: Act, don't just analyze.** When optimize_step identifies an issue, immediately fix it. Do NOT list recommendations for the user — you ARE the builder, make the change. After fixing, re-run to verify.

{{if eq .WorkshopMode "optimizer"}}
## OPTIMIZATION GUIDELINES

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
Learnings are stored as SKILL.md files in the workspace at 'learnings/{step-id}/SKILL.md'. Each learning file MUST use YAML frontmatter format:
`+"```"+`
---
name: <step title>
description: "<1-2 sentence summary of what this skill teaches — optimal approach and key pitfalls>"
disable-model-invocation: true
user-invocable: false
---
(learning content here)
`+"```"+`
You can read, edit, and delete them using **execute_shell_command** and **diff_patch_workspace_file**:
- **Read learnings**: 'cat learnings/{step-id}/SKILL.md' to read the learning file
- **Read metadata**: 'cat learnings/{step-id}/.learning_metadata.json' for iteration counts, lock status, success history
- **Edit learnings**: Use **diff_patch_workspace_file** to update SKILL.md. If learnings are locked, edits are used directly by the execution agent. If unlocked, the learning agent may overwrite on next run — suggest locking after manual edits.
- **Delete learnings**: 'rm learnings/{step-id}/SKILL.md learnings/{step-id}/.learning_metadata.json' to reset. Then unlock learnings via update_step_config so fresh learnings are generated on next run.
- **Lock after editing**: Always suggest lock_learnings=true after manual edits to prevent the learning agent from overwriting.
- **Legacy migration**: If you find '*_learning.md' files (old format) instead of SKILL.md, migrate their content into a new SKILL.md with proper frontmatter and delete the legacy files.

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
- **Review learnings after every successful run** — call 'cat learnings/{step-id}/SKILL.md' to read the current learning file. Check:
  - Are they **specific and actionable**? Vague learnings like "be careful with the API" waste tokens. Good learnings describe exact patterns: "The /api/v2/data endpoint returns paginated results — always follow next_page_token until null."
  - Do they **contradict the step description**? If so, either update the description or delete the misleading learning.
  - Do they **match the current step config**? Cross-check learnings against the step's configured servers, tools, and description. Learnings may reference server names, tool names, or patterns from a previous config that no longer apply (e.g., learning says "use server gws" but the step now uses "google_sheets", or learning references a tool that's been removed). Stale references cause the execution agent to search for non-existent servers/tools, wasting turns and causing failures. Fix by updating the learning file with the correct names.
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
4. **No wasted tool calls** — Review the execution: the agent should not have wasted turns on failed tool searches, wrong server names, retried API calls, or unnecessary exploration. If the agent spent turns searching for tools that don't exist, reading files that aren't there, or trying approaches that the learnings should have prevented — the step is NOT optimized yet. Fix the learnings or description first, re-run to confirm clean execution, then mark optimized.
5. *(Optional)* **Pre-discovered tools** — For tool-search steps, adding explicit tool filtering speeds up execution but is not required for optimization.

**After running optimize_step and applying any fixes:**
- If all checklist items are satisfied and no significant changes were needed, mark as optimized: update_step_config(step_id, optimized=true).
- If significant changes were applied, re-run the step to verify, then mark as optimized once it passes.
- **Already-optimized steps** (optimized=true in step_config) skip the optimization prompt on completion — the notification just says "proceed to next step".
- **Reset optimization** (optimized=false) only if you make major changes to the step description, tools, or validation schema — then re-run and re-optimize once.

### 9. Todo Task (Sub-Workflow / Pipeline) — The Preferred Multi-Step Pattern
**Default to todo_task** when a step involves multiple distinct sub-tasks. Users may call this a "sub-workflow" or "pipeline" — it's the most powerful step type, giving each sub-task (sub-agent) independent learnings, tools, skills, and debugging.

**When to use todo_task (prefer this over a single large regular step):**
- The step has **3+ distinct actions** (e.g., "login, extract data, generate report") — each becomes a sub-agent
- Sub-tasks need **different tools/skills/servers** (e.g., browser for login, code-exec for processing)
- Sub-tasks should **learn independently** — a login pattern shouldn't be mixed with data extraction learnings
- You want **parallel execution** — todo_task supports running sub-agents in parallel
- You need **granular debugging** — each sub-agent can be individually re-run and optimized

**When NOT to use todo_task:**
- Simple steps with a **single focused task** (one tool call, one output file) — use regular step
- The task is **dynamic/unpredictable** — depends entirely on runtime context that can't be anticipated
- The task is **trivial** — a one-line action that doesn't benefit from learning

**Sub-agent design:**
- Break known, predictable tasks into **predefined sub-agents** (routes) rather than leaving them as inline orchestrator instructions
- Each sub-agent has its own **learning files**, **server/tool scoping**, **skills (via enabled_skills in step_config)**, and **validation schemas**
- Sub-agents can be **individually debugged, re-run, and optimized** via the workshop tools
- The orchestrator stays lean — it manages task flow, while sub-agents handle execution details

**Design principle:** If you find yourself writing a step description with "First do X, then do Y, then do Z", convert it to a todo_task with sub-agents for X, Y, and Z. Each sub-agent gets its own learnings, tools, and optimization lifecycle.

**Rule of thumb:** When planning a new workflow, start by identifying the distinct tasks, then group related tasks into todo_task steps with sub-agents. Only use regular steps for truly simple, single-purpose tasks.
{{end}}

{{if eq .WorkshopMode "eval"}}
## EVALUATION

Evaluation plans test execution quality. Each eval step checks one execution step's output.

**Workflow:**
1. Edit `+"`evaluation/evaluation_plan.json`"+` directly with shell/file tools
2. Run **validate_evaluation_plan** after editing
3. Run **run_full_evaluation(target_run_folder)** to score against an execution run
4. Review the report — low scores (< 5) need tighter criteria or better step descriptions
5. Iterate: fix execution steps or refine the eval plan, then re-run

Do NOT modify execution steps or plan.json in eval mode. Switch to Build mode for workflow changes.
{{end}}

## TOOLS REFERENCE

{{if eq .IsCodeExecutionMode "true"}}**Code execution mode:** You do NOT have direct tool-call access. Bridge-native tools: `+"`execute_shell_command`"+`, `+"`diff_patch_workspace_file`"+`, `+"`agent_browser`"+`, `+"`get_api_spec`"+`. All other workflow tools (execute_step, query_step, plan modification, etc.) are available via the workflow API path — use `+"`get_api_spec(server_name=\"workflow\", tool_name=\"...\")`"+` to get their schemas. Do **not** hardcode raw HTTP requests.
{{end}}

{{if or (eq .WorkshopMode "builder") (eq .WorkshopMode "optimizer") (eq .WorkshopMode "runner")}}
### Step Execution
- **execute_step(step_id, iteration, group_id?, instructions?, human_input?)** — Start a step in background; returns execution_id. iteration defaults to iteration-0. skip_learning=true by default. Pass skip_learning=false to generate learnings. Pass human_input for human input steps.
- **query_step(execution_id, tool_call_id?)** — Status check + live tool calls
{{if ne .WorkshopMode "runner"}}- **debug_step(step_id, iteration, group_id)** — Rich insights: learning status, validation result, log paths{{end}}
- **list_executions(status_filter?)** — List all background executions
- **stop_step(execution_id)** / **stop_all_executions()** — Cancel running steps
- **run_in_background(name, instruction)** — Spawn independent background agent with same tools
{{if or (eq .WorkshopMode "optimizer") (eq .WorkshopMode "runner")}}- **run_full_workflow(iteration?, execution_strategy?, group_id?)** — Execute the complete workflow (all steps) for a single variable group in background. Specify iteration to reuse an existing run folder, or omit to create a new one. Defaults to fresh run skipping human input. Returns execution_id.{{end}}
{{end}}

{{if eq .WorkshopMode "debugger"}}
### Step Query (Read-Only)
- **query_step(execution_id, tool_call_id?)** — Status check + live tool calls
- **list_executions(status_filter?)** — List all background executions
{{end}}

{{if or (eq .WorkshopMode "eval") (eq .WorkshopMode "output")}}
### Step Query (Read-Only)
- **query_step(execution_id, tool_call_id?)** — Status check + live tool calls
- **list_executions(status_filter?)** — List all background executions
{{end}}

{{if or (eq .WorkshopMode "builder") (eq .WorkshopMode "optimizer")}}
### Step Config & Analysis
- **update_step_config(step_id, ...)** — Update servers, tools, skills, learning settings, execution mode, LLMs, optimized flag
- **analyze_step(step_id)** — Config and execution history analysis
{{if eq .WorkshopMode "optimizer"}}- **generate_learnings(step_id, guidance?, execution_history?)** — Start learning agent in background
- **optimize_step(step_id, focus?, forced?)** — Start optimization agent in background{{end}}
- **get_cost_summary** — Token usage and cost breakdown
{{end}}

{{if eq .WorkshopMode "debugger"}}
### Step Analysis (Read-Only)
- **analyze_step(step_id)** — Config and execution history analysis
- **debug_step(step_id, iteration, group_id)** — Rich insights: learning status, validation result, log paths
- **get_cost_summary** — Token usage and cost breakdown
{{end}}

### Read-Only Info
- **get_step_prompts(step_id, attempt?, iteration?)** — System prompt and user message for a step
- **get_workflow_config** — Full workflow config: MCP servers, skills, secrets, LLM config
- **get_llm_config** — Per-step LLM overrides

{{if eq .WorkshopMode "builder"}}
### Plan Modification
- **Steps**: add_regular_step, add_conditional_step, add_decision_step, add_loop_step, add_human_input_step, add_todo_task_step, add_routing_step, delete_plan_steps
- **Update**: update_regular_step, update_conditional_step, update_decision_step, update_human_input_step, update_routing_step, update_todo_task_step
- **Branches**: convert_step_to_conditional, convert_conditional_to_regular, add_branch_steps, update_branch_steps, delete_branch_steps
- **Todo task routes**: add_todo_task_route, update_todo_task_route, delete_todo_task_route
- **Validation & criteria**: update_validation_schema, update_success_criteria
- **Versioning**: publish_workflow_version(label), restore_workflow_version(version)
  To inspect available versions before restoring, use **execute_shell_command** with relative paths like `+"`ls versions/`"+` and `+"`cat versions/v3/version_meta.json`"+`.

### Variables & Config
- **update_variable(action, name?, value?, description?)** — Add, update, or delete a variable
- **add_group / update_group / delete_group** — Manage variable groups
- **update_workflow_config(add_servers?, remove_servers?, add_skills?, remove_skills?, add_secrets?, remove_secrets?)** — Update workflow MCP servers, skills, or secrets

### Schedule Management
- **create_schedule / update_schedule / delete_schedule / trigger_schedule / get_schedule_runs**
- To view existing schedules, read `+"`workflow.json`"+` via `+"`execute_shell_command`"+` — schedules are under the `+"`schedules`"+` key.
- Schedule management is only available in **builder mode**. If the user asks about schedules in another mode, tell them to switch to builder mode.
- Schedules support two execution modes: `+"`mode=\"workflow\"`"+` (direct orchestrator, default) and `+"`mode=\"workshop\"`"+` (LLM-driven via workshop builder with per-step notifications).
- Workshop mode takes `+"`messages`"+` (predefined message queue sent one-by-one) and `+"`workshop_mode`"+` (`+"`runner`"+` or `+"`optimizer`"+`).
- **Optimizer schedule best practices**: When creating a schedule with `+"`workshop_mode=\"optimizer\"`"+`, craft the message to optimize steps **one by one** after each step completes. Example message: "Run the full workflow using run_full_workflow. After each step completes, if it succeeded and is not yet optimized, call optimize_step and apply fixes. If a step fails, retry it once after fixing — if it fails again, skip it and move to the next step. Do NOT retry the same step more than 2 times to avoid infinite loops."
- **Infinite loop prevention**: Scheduled optimizer runs are unattended — they MUST have built-in stop conditions. The message should instruct the agent to: (1) skip already-optimized steps, (2) limit retries per step to 2 attempts max, (3) move on to the next step after repeated failures instead of looping, (4) stop after all steps have been attempted once.

### Skills
- **list_skills / search_skills(query) / install_skill(source) / uninstall_skill / import_skill** — Manage skills
{{end}}

{{if eq .WorkshopMode "optimizer"}}
### Validation (for optimization)
- **update_validation_schema** — Add or update a step's validation schema
- **update_success_criteria** — Update step success criteria
{{end}}

{{if eq .WorkshopMode "eval"}}
### Evaluation
- **validate_evaluation_plan** — Validate the evaluation plan JSON
- **run_full_evaluation(target_run_folder)** — Score the eval plan against an execution run
{{end}}

{{if eq .WorkshopMode "output"}}
### Report
- **validate_report_plan** — Validate the report plan JSON
- **run_full_report(target_run_folder)** — Regenerate the report for a completed group run
{{end}}

### Shell & Discovery
- **execute_shell_command** — Run shell commands. Quick lookups: `+"`jq '[.steps[] | {id, title, type}]' planning/plan.json`"+`, `+"`jq --arg sid \"step-id\" '.. | objects | select(.id? == $sid) | {id, title, type, description, success_criteria, context_output}' planning/plan.json`"+`, `+"`cat planning/step_config.json`"+`, `+"`ls runs/`"+`, `+"`cat variables/variables.json`"+`
- **human_feedback** — Ask the user a question during a run

{{if or (eq .WorkshopMode "optimizer") (eq .WorkshopMode "debugger")}}
### Human-Assisted Learning
**human_feedback tool** (runtime interaction) and **learning_mode: human_assisted** (post-execution learning) are unrelated features.

When a step has learning_mode "human_assisted":
1. Explore the task yourself first using execute_shell_command
2. Discuss findings with the user
3. Write SKILL.md to 'learnings/{step-id}/SKILL.md' with YAML frontmatter:
   `+"```"+`
   ---
   name: <step title>
   description: "<summary>"
   disable-model-invocation: true
   user-invocable: false
   ---
   (learning content)
   `+"```"+`
4. Lock learnings: update_step_config(step_id, lock_learnings=true)
5. Run the step via execute_step
{{end}}

## FILE LAYOUT

**Shell working directory**: `+"`/app/workspace-docs/{{.WorkspacePath}}/`"+` — use RELATIVE paths in all shell commands (e.g., `+"`cat planning/plan.json`"+`).
All paths below are relative to this root.

| Path | Contents |
|------|----------|
| planning/plan.json | Workflow plan |
| planning/step_config.json | Step-level config overrides |
| planning/output_plan.json | Report/output plan (output mode) |
| runs/{iteration}/{group}/ | Execution outputs per run |
| runs/{run}/logs/step-{N}/execution/ | Execution logs, prompts, tool calls |
| runs/{run}/token_usage.json | Per-step token usage |
| token_usage.json | Aggregated token usage |
| learnings/{step-id}/SKILL.md | Learning file (keyed by step ID, not position) |
| learnings/{step-id}/code/*.py | Code examples for code-exec steps |
| learnings/{step-id}/.learning_metadata.json | Iteration counts, success history |
| evaluation/evaluation_plan.json | Eval plan definitions |
| evaluation/runs/{run}/evaluation_report.json | Eval results |
| builder/session-{id}-conversation.json | Previous builder chat sessions |
| knowledgebase/ | Persistent data shared across all runs |

**Cleanup**: Delete old builder conversation files when >3 exist (`+"`ls -t builder/session-*.json`"+`, keep latest).

## CONSTRAINTS
1. **Use step IDs**: Step IDs come from plan.json (e.g., "step-create-report"), not positional numbers.
2. **Boolean config fields**: Only pass lock_learnings/disable_learning when explicitly changing them. Do NOT include them with false when updating other fields — this resets previously set values.
3. **Never hardcode variables or secrets**: Use variable placeholders (e.g., {USER_ID}) in descriptions and learnings. Actual values belong in variables.json / variable groups.
4. **Never read application source code**: Do NOT search or read *.go, *.ts, or *.json files outside the workspace. You operate on workspace files only.
`)

var interactiveWorkshopUserTemplate = MustRegisterTemplate("interactiveWorkshopUser", `{{if .UserRequest}}{{.UserRequest}}{{else}}What would you like to do in the workshop?{{end}}`)

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

	// Variable management tools (update_variable, group CRUD)
	// are registered inside registerInteractiveWorkshopTools for both full and HAE modes.

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

	// Append code execution or tool search instructions from mcpagent library
	// These include the {{TOOL_STRUCTURE}} placeholder (replaced by SetSystemPrompt with actual tool index)
	if agent.GetConfig().UseCodeExecutionMode {
		codeExecInstructions := prompt.GetCodeExecutionInstructions(workspacePath)
		if codeExecInstructions != "" {
			systemPrompt.WriteString("\n\n")
			systemPrompt.WriteString(codeExecInstructions)
			logger.Info("Added code execution instructions with tool structure to workshop agent")
		}
	} else if agent.GetConfig().UseToolSearchMode {
		toolSearchInstructions := prompt.GetToolSearchInstructions()
		if toolSearchInstructions != "" {
			systemPrompt.WriteString("\n\n")
			systemPrompt.WriteString(toolSearchInstructions)
			logger.Info("Added tool search instructions to workshop agent")
		}
	}

	// Append browser instructions if browser tools are available in this workflow
	browserCfg := iwm.controller.resolveBrowserConfig(iwm.controller.GetSelectedServers(), iwm.controller.GetSelectedSkills())
	if browserPromptStr := instructions.BuildBrowserInstructions(browserCfg); browserPromptStr != "" {
		systemPrompt.WriteString("\n\n")
		systemPrompt.WriteString(browserPromptStr)
		logger.Info(fmt.Sprintf("🌐 Added browser instructions to workflow builder system prompt (playwright=%v, camofox=%v, agent-browser=%v)",
			browserCfg.HasPlaywright, browserCfg.HasCamofox, browserCfg.HasAgentBrowser))
	}

	// Append GWS instructions if gws server is enabled
	for _, s := range iwm.controller.GetSelectedServers() {
		if s == "gws" {
			systemPrompt.WriteString("\n\n")
			systemPrompt.WriteString(instructions.GetGWSQuickStartInstructions())
			logger.Info("📧 Added GWS quick-start instructions to workflow builder system prompt")
			break
		}
	}

	// NOTE: Secrets are injected by the server-level handler (server.go) via AppendSystemPrompt
	// after the agent is created. Do NOT inject here — it causes duplication in the prompt.

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
		label := fmt.Sprintf("%q (step %d)", info.Step.GetID(), info.TopIndex)
		if info.TopIndex <= 0 {
			label = fmt.Sprintf("%q (inner, parent=%s, branch=%s)", info.Step.GetID(), info.ParentID, info.BranchName)
		}
		ids = append(ids, label)
	}
	return "", fmt.Errorf("step %q not found in plan. Valid IDs: %s", inputID, strings.Join(ids, ", "))
}

var workshopVersionedConfigFiles = []string{
	"planning/plan.json",
	"planning/step_config.json",
	"planning/workflow_layout.json",
	"planning/step_override.json",
	"planning/output_plan.json",
	"variables/variables.json",
	"evaluation/evaluation_plan.json",
}

var workshopVersionedFolderRoots = []string{
	"learnings",
	"evaluation/learnings",
}

func resolveWorkshopWorkspacePath(controller *StepBasedWorkflowOrchestrator, path string) string {
	workspacePath := controller.GetWorkspacePath()
	if workspacePath == "" || path == "" {
		return path
	}
	if path == workspacePath || strings.HasPrefix(path, workspacePath+"/") {
		return path
	}
	return workspacePath + "/" + path
}

func flattenWorkshopWorkspaceFiles(files []virtualtools.WorkspaceFile) []virtualtools.WorkspaceFile {
	var result []virtualtools.WorkspaceFile
	for _, file := range files {
		result = append(result, file)
		if len(file.Children) > 0 {
			result = append(result, flattenWorkshopWorkspaceFiles(file.Children)...)
		}
	}
	return result
}

func listWorkshopWorkspaceTree(ctx context.Context, controller *StepBasedWorkflowOrchestrator, dirPath string, maxDepth int) ([]virtualtools.WorkspaceFile, error) {
	listExecutorInterface, exists := controller.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		return nil, fmt.Errorf("list_workspace_files executor not found")
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return nil, fmt.Errorf("list_workspace_files executor has wrong type")
	}

	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, controller.GetContextAwareBridge())
	listJSON, err := listExecutor(ctx, map[string]interface{}{
		"folder":    resolveWorkshopWorkspacePath(controller, dirPath),
		"max_depth": maxDepth,
	})
	if err != nil {
		return nil, err
	}

	if strings.Contains(listJSON, "exists but contains no files") {
		return []virtualtools.WorkspaceFile{}, nil
	}

	var filesList []virtualtools.WorkspaceFile
	if err := json.Unmarshal([]byte(listJSON), &filesList); err != nil {
		var apiResp struct {
			Data []virtualtools.WorkspaceFile `json:"data"`
		}
		if err2 := json.Unmarshal([]byte(listJSON), &apiResp); err2 != nil {
			return nil, err
		}
		filesList = apiResp.Data
	}

	resolvedPath := resolveWorkshopWorkspacePath(controller, dirPath)
	if len(filesList) == 1 && filesList[0].Type == "folder" && filesList[0].FilePath == resolvedPath && len(filesList[0].Children) > 0 {
		filesList = filesList[0].Children
	}

	return flattenWorkshopWorkspaceFiles(filesList), nil
}

// registerInteractiveWorkshopTools registers the custom workshop tools on the agent.
func registerInteractiveWorkshopTools(iwm *InteractiveWorkshopManager, mcpAgent *mcpagent.Agent, logger loggerv2.Logger) {
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
					"description": "Optional variable group ID override (e.g., 'group-1'). If omitted, uses the group selected in the workspace toolbar. Read variables.json to see available groups.",
				},
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Iteration folder name. Defaults to 'iteration-0' (workshop scratch iteration) if omitted. Use a different iteration only when the user explicitly requests it.",
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

			// Extract group_id, iteration, and other options
			groupIDRaw, _ := args["group_id"]
			iterationRaw, _ := args["iteration"]
			groupID, _ := groupIDRaw.(string)
			iteration, _ := iterationRaw.(string)

			// Fallback to session-level group from toolbar selection
			if groupID == "" && len(iwm.controller.enabledGroupIDs) > 0 {
				groupID = iwm.controller.enabledGroupIDs[0]
			}

			// Validate a group is available — cannot run steps without one
			if groupID == "" {
				iwm.refreshVariablesManifest(ctx)
				if iwm.controller.variablesManifest == nil || len(iwm.controller.variablesManifest.Groups) == 0 {
					return "No variable groups exist. Create a group first using add_group before running steps.", nil
				}
				// Auto-select the first available group
				groupID = iwm.controller.variablesManifest.Groups[0].GroupID
			}

			// Default to iteration-0 (workshop scratch iteration) if not provided
			if iteration == "" {
				iteration = "iteration-0"
			}

			// Build run_folder from iteration + group folder name
			// Refresh manifest from file to avoid stale group data
			iwm.refreshVariablesManifest(ctx)
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

				// Resolve step title and type for the wrapper event (use plan step if available)
				stepDisplayName := stepID
				stepType := ""
				if iwm.controller.approvedPlan != nil {
					if stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID); stepInfo != nil {
						stepDisplayName = stepInfo.Step.GetTitle()
						stepType = string(stepInfo.Step.StepType())
					}
				}

				// Notify server layer so bgAgentRegistry tracks this execution (keeps frontend polling alive)
				if iwm.executionNotifier != nil {
					iwm.executionNotifier.OnExecutionStart(execID, stepDisplayName)
				}

				// Emit orchestrator_agent_start so the frontend creates a grouping card
				eventBridge := iwm.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-execution",
						AgentName:     fmt.Sprintf("Step: %s", stepDisplayName),
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

				// Update status BEFORE emitting event so query_step sees the final state
				exec.mu.Lock()
				alreadyCancelled := exec.Status == WorkshopStepCancelled
				// Don't overwrite "cancelled" status — stop_step may have already set it
				if !alreadyCancelled {
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
				}
				exec.mu.Unlock()

				// Always emit orchestrator_agent_end to close the grouping card (even for cancelled steps)
				if eventBridge != nil {
					isCancelled := execCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-execution",
						AgentName:     fmt.Sprintf("Step: %s", stepDisplayName),
						Success:       err == nil,
						InputData:     map[string]string{},
					}
					if execOpts != nil && execOpts.RunFolder != "" {
						endEvent.InputData["run_folder"] = execOpts.RunFolder
					}
					if isOptimized {
						endEvent.InputData["step_optimized"] = "true"
					}
					// Include workshop mode so frontend can tailor notification messages
					if configs, configErr := iwm.controller.ReadStepConfigs(execCtx); configErr == nil {
						wMode, _ := detectWorkshopMode(iwm.controller.approvedPlan, configs)
						endEvent.InputData["workshop_mode"] = wMode
					}
					// Include step type so frontend can skip notifications for human_input steps
					if stepType != "" {
						endEvent.InputData["step_type"] = stepType
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

				// Notify server layer so bgAgentRegistry marks this execution as done.
				// Skip if already cancelled (stop_step already sent OnExecutionTerminated).
				if iwm.executionNotifier != nil && !alreadyCancelled {
					iwm.executionNotifier.OnExecutionComplete(execID, stepDisplayName, result, err)
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

	// Tool: run_in_background — spawn independent background agent (not tied to a workflow step)
	if err := mcpAgent.RegisterCustomTool(
		"run_in_background",
		"Spawn an independent background agent to run a task with the same tools as the workflow. Returns an execution_id immediately. You will be notified when it completes. Use this to offload context-heavy work or run tasks in parallel.\n\nagent_type controls the agent model:\n- \"executor\" (default): single-pass execution agent — best for focused, well-defined tasks\n- \"orchestrator\": todo task orchestrator with call_generic_agent — best for complex multi-step tasks that benefit from task management and sub-agent delegation. Sub-agent completions also auto-notify you.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Short descriptive name (e.g., 'Research APIs', 'Validate data')",
				},
				"instruction": map[string]interface{}{
					"type":        "string",
					"description": "Comprehensive instructions for the background agent. This is the agent's task — be specific about what it should do, inputs, expected outputs.",
				},
				"agent_type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"executor", "orchestrator"},
					"description": "executor (default): single-pass agent. orchestrator: todo task orchestrator with sub-agent delegation.",
				},
			},
			"required": []string{"name", "instruction"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			nameRaw, ok := args["name"]
			if !ok || nameRaw == nil {
				return "name is required", nil
			}
			name, ok := nameRaw.(string)
			if !ok || name == "" {
				return "name must be a non-empty string", nil
			}

			instructionRaw, ok := args["instruction"]
			if !ok || instructionRaw == nil {
				return "instruction is required", nil
			}
			instruction, ok := instructionRaw.(string)
			if !ok || instruction == "" {
				return "instruction must be a non-empty string", nil
			}

			agentType := "executor"
			if v, ok := args["agent_type"].(string); ok && v != "" {
				agentType = v
			}

			// Create slug from name for execution ID
			nameSlug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
			// Trim to reasonable length
			if len(nameSlug) > 30 {
				nameSlug = nameSlug[:30]
			}

			execID := fmt.Sprintf("bg-%s-%05d", nameSlug, time.Now().UnixNano()%100000)
			execCtx, cancel := context.WithCancel(iwm.sessionCtx)

			// Inject correlation IDs for sub-agent event tagging (same pattern as execute_step)
			agentSessionID := fmt.Sprintf("workshop-bg-%s-%d", nameSlug, time.Now().UnixNano())
			execCtx = context.WithValue(execCtx, orchestrator_events.AgentSessionIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.ForceCorrelationIDKey, agentSessionID)
			execCtx = context.WithValue(execCtx, orchestrator_events.IsSubAgentContextKey, true)

			exec := &WorkshopStepExecution{
				ID:             execID,
				StepID:         name, // Use name as the "step" identifier for display
				AgentSessionID: agentSessionID,
				Status:         WorkshopStepRunning,
				cancel:         cancel,
			}
			iwm.stepRegistry.Register(exec)

			// Notify server layer so bgAgentRegistry tracks this execution (keeps frontend polling alive)
			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(execID, name)
			}

			go func() {
				// Emit orchestrator_agent_start so the frontend creates a grouping card
				eventBridge := iwm.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-background-task",
						AgentName:     fmt.Sprintf("Background: %s", name),
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				var result string
				var err error
				if agentType == "orchestrator" {
					result, err = iwm.runBackgroundTodoTaskAgent(execCtx, name, instruction)
				} else {
					result, err = iwm.runBackgroundTaskAgent(execCtx, name, instruction)
				}

				// Update status BEFORE emitting event so query_step sees the final state
				exec.mu.Lock()
				alreadyCancelled := exec.Status == WorkshopStepCancelled
				if !alreadyCancelled {
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
				}
				exec.mu.Unlock()

				// Always emit orchestrator_agent_end to close the grouping card (even for cancelled steps)
				if eventBridge != nil {
					isCancelled := execCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || alreadyCancelled
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-background-task",
						AgentName:     fmt.Sprintf("Background: %s", name),
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

				// Notify server layer so bgAgentRegistry marks this execution as done.
				// Skip if already cancelled (stop_step already sent OnExecutionTerminated).
				if iwm.executionNotifier != nil && !alreadyCancelled {
					iwm.executionNotifier.OnExecutionComplete(execID, name, result, err)
				}
			}()

			logger.Info(fmt.Sprintf("🚀 Workshop: background task %q started (type=%s), execution_id=%q", name, agentType, execID))
			return fmt.Sprintf("Background task %q started (type=%s).\nexecution_id: %q\nYou will be automatically notified when it completes.", name, agentType, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register run_in_background tool: %v", err))
	}

	// Tool 2: query_step — unified status + real-time tool call visibility
	// When running: shows status + live tool calls (auto-enriched)
	// When done/failed/cancelled: shows result
	if err := mcpAgent.RegisterCustomTool(
		"query_step",
		"Check the status of a background step execution. When running, also shows which tools the step is currently calling in real time. When done, shows the result. Pass tool_call_id to get full input/output for a specific tool call. Use debug_step for file-based insights (learnings, validation, logs).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id": map[string]interface{}{
					"type":        "string",
					"description": "The execution_id returned by execute_step",
				},
				"tool_call_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: a specific tool_call_id from a previous query_step summary to get full input/output details for that call",
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
			result := exec.Result
			execErr := exec.Err
			agentSessID := exec.AgentSessionID
			exec.mu.RUnlock()

			switch status {
			case WorkshopStepRunning:
				// Auto-enrich with live tool calls when running
				var toolCallInfo string
				if iwm.toolCallQueryFunc != nil {
					mainSessID := iwm.mainSessionID
					if mainSessID == "" {
						mainSessID = iwm.sessionID
					}
					summary := iwm.toolCallQueryFunc(mainSessID, agentSessID, toolCallID)
					if toolCallID != "" && summary != "" {
						return fmt.Sprintf("Step %q — tool call detail:\n%s", stepID, summary), nil
					}
					if summary != "" {
						toolCallInfo = fmt.Sprintf("\n\n**Live tool calls:**\n%s", summary)
					}
				}

				// Detect execution type from ID prefix and add context
				isAnalysisAgent := strings.HasPrefix(execID, "learn-") || strings.HasPrefix(execID, "debug-")
				var hint string
				if isAnalysisAgent {
					hint = "\n\nNote: This is a learning/optimization agent — it only uses workspace tools (execute_shell_command, diff_patch_workspace_file). For richer insights, use debug_step(step_id) instead."
				}

				if toolCallInfo == "" {
					return fmt.Sprintf("Step %q is still running. No tool calls observed yet.%s", stepID, hint), nil
				}
				return fmt.Sprintf("Step %q is still running.%s%s", stepID, toolCallInfo, hint), nil

			case WorkshopStepDone:
				// Background tasks get a generic completion response (no step-specific hints)
				if strings.HasPrefix(execID, "bg-") {
					return fmt.Sprintf("Background task %q completed.\n\n%s", stepID, result), nil
				}
				return fmt.Sprintf("Step %q completed.\n\n%s\n\n**Next actions (do these now):**\n1. Review the result against the step's success criteria\n2. Read learnings: 'cat learnings/%s/SKILL.md' — are they specific and actionable? Edit or delete noisy ones.\n3. Check learning metadata: 'cat learnings/%s/.learning_metadata.json' — if consecutive_successes >= 3, consider locking learnings.\n4. Note the highest-priority optimization from Post-Execution Step Review.\n5. If output looks wrong, investigate with debug_step(%q) or analyze_step(%q) and fix the root cause before re-running.", stepID, result, stepID, stepID, stepID, stepID), nil
			case WorkshopStepFailed:
				if strings.HasPrefix(execID, "bg-") {
					return fmt.Sprintf("Background task %q failed: %v", stepID, execErr), nil
				}
				return fmt.Sprintf("Step %q failed: %v\n\n**Next**: Investigate the failure. Call debug_step(%q) for detailed execution insights, then fix the root cause (description, validation, context deps) before re-running.", stepID, execErr, stepID), nil
			case WorkshopStepCancelled:
				return fmt.Sprintf("Step %q was cancelled.", stepID), nil
			default:
				return fmt.Sprintf("Step %q has unknown status: %s", stepID, status), nil
			}
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register query_step tool: %v", err))
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
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Iteration folder name. Defaults to 'iteration-0' if omitted.",
				},
				"group_id": map[string]interface{}{
					"type":        "string",
					"description": "Variable group ID (e.g., 'group-1'). Read variables.json to see available groups.",
				},
			},
			"required": []string{"step_id", "group_id"},
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

			// Extract iteration and group_id
			iteration, _ := args["iteration"].(string)
			groupID, _ := args["group_id"].(string)

			// Default to iteration-0 (workshop scratch iteration)
			if iteration == "" {
				iteration = "iteration-0"
			}
			if groupID == "" {
				return "group_id is required (e.g., 'group-1'). Read variables.json to see available groups.", nil
			}

			// Refresh manifest from file to avoid stale group data
			iwm.refreshVariablesManifest(ctx)
			// Resolve group folder name and build run folder
			groupFolderName := groupID
			if iwm.controller.variablesManifest != nil {
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
			iwm.controller.SetSelectedRunFolder(runFolder)

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

			// Notify server layer so bgAgentRegistry marks this as terminated and frontend updates
			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionTerminated(execID, exec.StepID)
			}

			logger.Info(fmt.Sprintf("🛑 Workshop: step %q (execution_id=%q) cancelled", exec.StepID, execID))
			return fmt.Sprintf("Step %q (execution_id=%q) has been cancelled.", exec.StepID, execID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register stop_step tool: %v", err))
	}

	// Tool 3b: stop_all_executions — cancel all running background executions at once
	if err := mcpAgent.RegisterCustomTool(
		"stop_all_executions",
		"Cancel ALL running background executions (steps, learnings, optimizations, background agents). Use this when the user asks to stop, cancel, or abort everything.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			cancelledIDs := iwm.stepRegistry.CancelAll()
			if len(cancelledIDs) == 0 {
				return "No running executions found to cancel.", nil
			}
			// Notify server layer for each cancelled execution
			if iwm.executionNotifier != nil {
				for _, id := range cancelledIDs {
					exec := iwm.stepRegistry.Get(id)
					name := id
					if exec != nil {
						name = exec.StepID
					}
					iwm.executionNotifier.OnExecutionTerminated(id, name)
				}
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Cancelled %d running execution(s):\n", len(cancelledIDs)))
			for _, id := range cancelledIDs {
				sb.WriteString(fmt.Sprintf("- %s\n", id))
			}
			logger.Info(fmt.Sprintf("🛑 Workshop: cancelled all %d running executions", len(cancelledIDs)))
			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register stop_all_executions tool: %v", err))
	}

	// === Builder tools: config, optimization, learning ===

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
				"enabled_skills": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Skill folder names to enable for this step (overrides workflow-level skills). Use list_skills or get_workflow_config to see available skills. Set to empty array to use workflow defaults.",
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
						// Sync: lock_learnings and optimized move together
						targetConfig.AgentConfigs.Optimized = &b
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
			if val, ok := args["enabled_skills"]; ok && val != nil {
				if arr, ok := val.([]interface{}); ok {
					enabledSkills := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							enabledSkills = append(enabledSkills, s)
						}
					}
					targetConfig.AgentConfigs.EnabledSkills = enabledSkills
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
					// Sync: optimized and lock_learnings move together
					targetConfig.AgentConfigs.LockLearnings = &b
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
					errors = append(errors, fmt.Sprintf("Step ID %q not found in the current plan. Valid step IDs can be found in planning/plan.json.", stepID))
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
				"human_tools":        true,
				"workspace_browser":  true,
				"workspace_git":      true,
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

	// === Tools: analyze, learn, optimize, background tasks ===

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
				"iteration": map[string]interface{}{
					"type":        "string",
					"description": "Iteration folder name. Defaults to 'iteration-0' if omitted.",
				},
				"group_id": map[string]interface{}{
					"type":        "string",
					"description": "The variable group ID (e.g., 'group1').",
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
			"required": []string{"step_id", "group_id"},
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

			iteration, _ := args["iteration"].(string)
			// Default to iteration-0 (workshop scratch iteration)
			if iteration == "" {
				iteration = "iteration-0"
			}
			groupID, _ := args["group_id"].(string)
			if groupID == "" {
				return "group_id is required (e.g., 'group1')", nil
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

			// Resolve group folder name from group_id
			iwm.refreshVariablesManifest(ctx)
			groupFolderName := groupID
			if iwm.controller.variablesManifest != nil {
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
			iwm.controller.SetSelectedRunFolder(runFolder)

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

			// Notify server layer so bgAgentRegistry tracks this execution (keeps frontend polling alive)
			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(execID, fmt.Sprintf("Learning: %s", resolvedID))
			}

			go func() {
				// Resolve step title for display
				learningDisplayName := resolvedID
				if iwm.controller.approvedPlan != nil {
					if stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, resolvedID); stepInfo != nil {
						learningDisplayName = stepInfo.Step.GetTitle()
					}
				}

				// Emit orchestrator_agent_start so the frontend creates a grouping card
				eventBridge := iwm.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-learning",
						AgentName:     fmt.Sprintf("Learning: %s", learningDisplayName),
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
							AgentName:     fmt.Sprintf("Learning: %s", learningDisplayName),
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
					if iwm.executionNotifier != nil {
						iwm.executionNotifier.OnExecutionComplete(execID, fmt.Sprintf("Learning: %s", learningDisplayName), "", createErr)
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

				// Update status BEFORE emitting event so query_step sees the final state
				exec.mu.Lock()
				alreadyCancelled := exec.Status == WorkshopStepCancelled
				if !alreadyCancelled {
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
				}
				exec.mu.Unlock()

				// Always emit orchestrator_agent_end to close the grouping card (even for cancelled steps)
				if eventBridge != nil {
					isCancelled := execCtx.Err() != nil || errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded)
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-learning",
						AgentName:     fmt.Sprintf("Learning: %s", learningDisplayName),
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

				// Notify server layer so bgAgentRegistry marks this execution as done.
				if iwm.executionNotifier != nil && !alreadyCancelled {
					iwm.executionNotifier.OnExecutionComplete(execID, fmt.Sprintf("Learning: %s", learningDisplayName), result, execErr)
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
		"Start a background optimization agent that analyzes logs, output, learnings, and config for a step. Returns execution_id immediately — you will be automatically notified when it completes. By default, if a step is already optimized, this tool returns early unless forced=true.",
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
				"forced": map[string]interface{}{
					"type":        "boolean",
					"description": "Optional. Default false. If true, run optimize_step even when step_config already marks the step as optimized.",
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
			forced := false
			if val, ok := args["forced"]; ok && val != nil {
				if b, ok := val.(bool); ok {
					forced = b
				} else {
					return "forced must be a boolean", nil
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

			// Default guard: if the step is already optimized, skip re-optimization unless explicitly forced.
			if !forced {
				stepConfigs, cfgErr := iwm.controller.ReadStepConfigs(ctx)
				if cfgErr != nil {
					logger.Warn(fmt.Sprintf("⚠️ optimize_step: failed to read step configs for optimized check: %v (continuing)", cfgErr))
				} else {
					for _, sc := range stepConfigs {
						if sc.ID != stepID || sc.AgentConfigs == nil || sc.AgentConfigs.Optimized == nil || !*sc.AgentConfigs.Optimized {
							continue
						}
						return fmt.Sprintf(
							"Step %q is already optimized (optimized=true in planning/step_config.json). Skipping optimize_step by default. To run optimization analysis again, call optimize_step with forced=true.",
							stepID,
						), nil
					}
				}
			}

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

			// Notify server layer so bgAgentRegistry tracks this execution (keeps frontend polling alive)
			if iwm.executionNotifier != nil {
				iwm.executionNotifier.OnExecutionStart(execID, fmt.Sprintf("Optimize: %s", stepID))
			}

			go func() {
				// Resolve step title for display
				debugDisplayName := stepID
				if iwm.controller.approvedPlan != nil {
					if stepInfo := findWorkshopStepByID(iwm.controller.approvedPlan.Steps, stepID); stepInfo != nil {
						debugDisplayName = stepInfo.Step.GetTitle()
					}
				}

				// Emit orchestrator_agent_start so the frontend creates a grouping card
				eventBridge := iwm.controller.GetContextAwareBridge()
				if eventBridge != nil {
					startEvent := &orchestrator_events.OrchestratorAgentStartEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-debug",
						AgentName:     fmt.Sprintf("Optimize: %s", debugDisplayName),
					}
					eventBridge.HandleEvent(execCtx, &baseevents.AgentEvent{
						Type:          orchestrator_events.OrchestratorAgentStart,
						Timestamp:     time.Now(),
						Data:          startEvent,
						CorrelationID: agentSessionID,
					})
				}

				result, err := iwm.runOptimizeStepAgent(execCtx, stepID, focus)

				// Update status BEFORE emitting event so query_step sees the final state
				exec.mu.Lock()
				alreadyCancelled := exec.Status == WorkshopStepCancelled
				if !alreadyCancelled {
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
				}
				exec.mu.Unlock()

				// Always emit orchestrator_agent_end to close the grouping card (even for cancelled steps)
				if eventBridge != nil {
					isCancelled := execCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || alreadyCancelled
					endEvent := &orchestrator_events.OrchestratorAgentEndEvent{
						BaseEventData: baseevents.BaseEventData{Timestamp: time.Now(), Component: "orchestrator"},
						AgentType:     "workshop-step-debug",
						AgentName:     fmt.Sprintf("Optimize: %s", debugDisplayName),
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

				// Notify server layer so bgAgentRegistry marks this execution as done.
				if iwm.executionNotifier != nil && !alreadyCancelled {
					iwm.executionNotifier.OnExecutionComplete(execID, fmt.Sprintf("Optimize: %s", debugDisplayName), result, err)
				}
			}()

			focusInfo := ""
			if focus != "" {
				focusInfo = fmt.Sprintf("\nFocus: %s", focus)
			}
			logger.Info(fmt.Sprintf("🔍 Workshop: optimization agent for step %q started in background, execution_id=%q, forced=%v", stepID, execID, forced))
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

	// === Tools: background tasks, LLM config, workflow config ===

	// Tool: get_llm_config — show current LLM configuration (read-only)
	if err := mcpAgent.RegisterCustomTool(
		"get_llm_config",
		"Show the current LLM configuration for the workflow: tiered config (tier 1/2/3 with fallbacks), phase LLM, and any per-step LLM overrides from step_config.json.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			var sb strings.Builder
			sb.WriteString("## LLM Configuration\n\n")

			// Show tiered config if enabled
			if iwm.controller.tierResolver != nil && iwm.controller.tierResolver.config != nil {
				tc := iwm.controller.tierResolver.config
				sb.WriteString("\n### Tiered LLM Config (active)\n")
				writeTierWithFallbacks := func(label string, cfg *AgentLLMConfig) {
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
				writeTierWithFallbacks("Tier 1 (high)", tc.Tier1)
				writeTierWithFallbacks("Tier 2 (medium)", tc.Tier2)
				writeTierWithFallbacks("Tier 3 (low)", tc.Tier3)
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

	// Tool: update_variable — add, update, or delete variables
	updateVariableSchema := getUpdateVariableSchema()
	updateVariableParams, parseErr := parseSchemaForToolParameters(updateVariableSchema)
	if parseErr != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to parse update_variable schema: %v", parseErr))
	} else if err := mcpAgent.RegisterCustomTool(
		"update_variable",
		"Update, add, or delete variables in variables.json. Provide action (required: 'update', 'add', or 'delete'), existing_variable_name (required for update/delete), and fields to update (name, value, description). The variables.json file is updated immediately.",
		updateVariableParams,
		createUpdateVariableExecutor(iwm.controller.GetWorkspacePath(), logger,
			func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			},
			func(ctx context.Context, path string, content string) error {
				return iwm.controller.WriteWorkspaceFile(ctx, path, content)
			}),
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_variable tool: %v", err))
	}

	// Tool: add_group — create a new variable group
	if err := mcpAgent.RegisterCustomTool(
		"add_group",
		"Create a new variable group. Optionally provide a display_name and initial values. The new group will have all defined variables with empty values by default.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"display_name": map[string]interface{}{
					"type":        "string",
					"description": "User-friendly name for the group (e.g., 'Production', 'Staging'). If not provided, defaults to the group_id.",
				},
				"values": map[string]interface{}{
					"type":        "object",
					"description": "Optional initial variable values as key-value pairs (e.g., {\"API_URL\": \"https://prod.example.com\"}). Variables not specified will have empty values.",
					"additionalProperties": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			readFile := func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			}
			writeFile := func(ctx context.Context, path string, content string) error {
				return iwm.controller.WriteWorkspaceFile(ctx, path, content)
			}
			workspacePath := iwm.controller.GetWorkspacePath()

			manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
			if err != nil {
				// Create new manifest if none exists
				manifest = &VariablesManifest{
					Variables:      []Variable{},
					Groups:         []VariableGroup{},
					ExtractionDate: time.Now().Format(time.RFC3339),
				}
			}

			newGroup := manifest.AddGroup()

			// Set display name if provided
			if displayName, ok := args["display_name"].(string); ok && displayName != "" {
				newGroup.DisplayName = displayName
			}

			// Set initial values if provided
			if values, ok := args["values"].(map[string]interface{}); ok {
				for k, v := range values {
					if strVal, ok := v.(string); ok {
						newGroup.Values[k] = strVal
					}
				}
			}

			if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
				return "", fmt.Errorf("failed to write variables: %w", err)
			}

			displayName := newGroup.DisplayName
			if displayName == "" {
				displayName = newGroup.GroupID
			}
			return fmt.Sprintf("Created new group: %s (group_id: %s) with %d variables", displayName, newGroup.GroupID, len(newGroup.Values)), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register add_group tool: %v", err))
	}

	// Tool: update_group — update an existing variable group's display_name, values, or enabled status
	if err := mcpAgent.RegisterCustomTool(
		"update_group",
		"Update a variable group. Provide group_id (required) and fields to change: display_name, values (key-value map), enabled (true/false). Only provided fields are updated.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"group_id": map[string]interface{}{
					"type":        "string",
					"description": "The group_id of the group to update (e.g., 'group-1')",
				},
				"display_name": map[string]interface{}{
					"type":        "string",
					"description": "New display name for the group",
				},
				"values": map[string]interface{}{
					"type":        "object",
					"description": "Variable values to set or update as key-value pairs. Only specified variables are updated; others remain unchanged.",
					"additionalProperties": map[string]interface{}{
						"type": "string",
					},
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable or disable the group for execution",
				},
			},
			"required": []interface{}{"group_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			groupID, ok := args["group_id"].(string)
			if !ok || groupID == "" {
				return "", fmt.Errorf("group_id is required")
			}

			readFile := func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			}
			writeFile := func(ctx context.Context, path string, content string) error {
				return iwm.controller.WriteWorkspaceFile(ctx, path, content)
			}
			workspacePath := iwm.controller.GetWorkspacePath()

			manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
			if err != nil {
				return "", fmt.Errorf("failed to read variables: %w", err)
			}

			// Find the group
			groupIdx := -1
			for i := range manifest.Groups {
				if manifest.Groups[i].GroupID == groupID {
					groupIdx = i
					break
				}
			}
			if groupIdx == -1 {
				return "", fmt.Errorf("group %s not found", groupID)
			}

			changes := []string{}

			// Update display_name
			if displayName, ok := args["display_name"].(string); ok {
				manifest.Groups[groupIdx].DisplayName = displayName
				changes = append(changes, fmt.Sprintf("display_name=%s", displayName))
			}

			// Update enabled
			if enabled, ok := args["enabled"].(bool); ok {
				manifest.Groups[groupIdx].Enabled = enabled
				changes = append(changes, fmt.Sprintf("enabled=%v", enabled))
			}

			// Update values (merge, don't replace)
			if values, ok := args["values"].(map[string]interface{}); ok {
				if manifest.Groups[groupIdx].Values == nil {
					manifest.Groups[groupIdx].Values = make(map[string]string)
				}
				for k, v := range values {
					if strVal, ok := v.(string); ok {
						manifest.Groups[groupIdx].Values[k] = strVal
						changes = append(changes, fmt.Sprintf("%s=%s", k, strVal))
					}
				}
			}

			if len(changes) == 0 {
				return "No changes specified", nil
			}

			if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
				return "", fmt.Errorf("failed to write variables: %w", err)
			}

			return fmt.Sprintf("Updated group %s: %s", groupID, strings.Join(changes, ", ")), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_group tool: %v", err))
	}

	// Tool: delete_group — remove a variable group
	if err := mcpAgent.RegisterCustomTool(
		"delete_group",
		"Delete a variable group by group_id. Cannot delete the last remaining group.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"group_id": map[string]interface{}{
					"type":        "string",
					"description": "The group_id of the group to delete (e.g., 'group-2')",
				},
			},
			"required": []interface{}{"group_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			groupID, ok := args["group_id"].(string)
			if !ok || groupID == "" {
				return "", fmt.Errorf("group_id is required")
			}

			readFile := func(ctx context.Context, path string) (string, error) {
				return iwm.controller.ReadWorkspaceFile(ctx, path)
			}
			writeFile := func(ctx context.Context, path string, content string) error {
				return iwm.controller.WriteWorkspaceFile(ctx, path, content)
			}
			workspacePath := iwm.controller.GetWorkspacePath()

			manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
			if err != nil {
				return "", fmt.Errorf("failed to read variables: %w", err)
			}

			if len(manifest.Groups) <= 1 {
				return "", fmt.Errorf("cannot delete the last remaining group")
			}

			if !manifest.DeleteGroup(groupID) {
				return "", fmt.Errorf("group %s not found", groupID)
			}

			if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
				return "", fmt.Errorf("failed to write variables: %w", err)
			}

			return fmt.Sprintf("Deleted group %s. Remaining groups: %d", groupID, len(manifest.Groups)), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register delete_group tool: %v", err))
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
				sb.WriteString("**Selected** (values hidden):\n")
				for _, s := range secrets {
					sb.WriteString(fmt.Sprintf("- **%s**\n", s.Name))
				}
			}

			// Show available secrets that can be added
			if iwm.listAvailableSecrets != nil {
				allSecretNames, listErr := iwm.listAvailableSecrets(ctx)
				if listErr == nil && len(allSecretNames) > 0 {
					selectedSet := make(map[string]bool, len(secrets))
					for _, s := range secrets {
						selectedSet[s.Name] = true
					}
					var available []string
					for _, name := range allSecretNames {
						if !selectedSet[name] {
							available = append(available, name)
						}
					}
					if len(available) > 0 {
						sb.WriteString("\n**Available to add** (use update_workflow_config with add_secrets):\n")
						for _, name := range available {
							sb.WriteString(fmt.Sprintf("- %s\n", name))
						}
					}
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
			// Only show preset learning LLM when tiered mode is not active (tiered mode overrides it)
			writeLLMDefault("Phase LLM", ctrl.presetPhaseLLM)

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_workflow_config tool: %v", err))
	}

	// === Tool: update_workflow_config ===
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
				"update_tier_fallbacks": map[string]interface{}{
					"type":        "object",
					"description": "Update fallback LLMs for tiered allocation. Keys: 'tier_1', 'tier_2', 'tier_3'. Value: array of {provider, model_id} objects. Use get_workflow_config or get_llm_config to see current config.",
					"properties": map[string]interface{}{
						"tier_1": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"provider": map[string]interface{}{"type": "string"}, "model_id": map[string]interface{}{"type": "string"}}, "required": []string{"provider", "model_id"}},
						},
						"tier_2": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"provider": map[string]interface{}{"type": "string"}, "model_id": map[string]interface{}{"type": "string"}}, "required": []string{"provider", "model_id"}},
						},
						"tier_3": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"provider": map[string]interface{}{"type": "string"}, "model_id": map[string]interface{}{"type": "string"}}, "required": []string{"provider", "model_id"}},
						},
					},
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

			// --- Tier Fallbacks ---
			if tierFallbacksRaw, ok := args["update_tier_fallbacks"]; ok && tierFallbacksRaw != nil {
				if iwm.controller.tierResolver == nil || iwm.controller.tierResolver.config == nil {
					sb.WriteString("\n### Tier Fallbacks\n⚠️ Tiered allocation is not enabled. Cannot update fallbacks.\n")
				} else if tierMap, ok := tierFallbacksRaw.(map[string]interface{}); ok {
					tc := iwm.controller.tierResolver.config
					parseFallbacks := func(raw interface{}) []AgentLLMFallback {
						arr, ok := raw.([]interface{})
						if !ok {
							return nil
						}
						var fbs []AgentLLMFallback
						for _, item := range arr {
							m, ok := item.(map[string]interface{})
							if !ok {
								continue
							}
							provider, _ := m["provider"].(string)
							modelID, _ := m["model_id"].(string)
							if provider != "" && modelID != "" {
								fbs = append(fbs, AgentLLMFallback{Provider: provider, ModelID: modelID})
							}
						}
						return fbs
					}

					tierChanged := false
					for _, entry := range []struct {
						key  string
						tier **AgentLLMConfig
						name string
					}{
						{"tier_1", &tc.Tier1, "Tier 1 (high)"},
						{"tier_2", &tc.Tier2, "Tier 2 (medium)"},
						{"tier_3", &tc.Tier3, "Tier 3 (low)"},
					} {
						if raw, exists := tierMap[entry.key]; exists {
							fbs := parseFallbacks(raw)
							if *entry.tier == nil {
								sb.WriteString(fmt.Sprintf("⚠️ %s has no primary LLM configured, skipping fallback update.\n", entry.name))
								continue
							}
							(*entry.tier).Fallbacks = fbs
							tierChanged = true
							sb.WriteString(fmt.Sprintf("- **%s**: %s/%s", entry.name, (*entry.tier).Provider, (*entry.tier).ModelID))
							if len(fbs) > 0 {
								fbStrs := make([]string, len(fbs))
								for i, fb := range fbs {
									fbStrs[i] = fmt.Sprintf("%s/%s", fb.Provider, fb.ModelID)
								}
								sb.WriteString(fmt.Sprintf(" → fallbacks: %s", strings.Join(fbStrs, ", ")))
							} else {
								sb.WriteString(" → fallbacks: (cleared)")
							}
							sb.WriteString("\n")
						}
					}
					if tierChanged {
						anyChanged = true
						sb.WriteString("\n### Tier Fallbacks (updated)\n")
						logger.Info("Updated tier fallback LLMs")
					}
				}
			}

			if !anyChanged {
				return "No changes applied. Provide at least one of: add_servers, remove_servers, add_skills, remove_skills, add_secrets, remove_secrets, update_tier_fallbacks.", nil
			}

			// Persist config changes to workflow.json manifest (file-backed)
			iwm.persistWorkflowConfigToManifest(ctx, logger)

			return sb.String(), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_workflow_config tool: %v", err))
	}

	// Tool: publish_workflow_version — snapshot the current workflow config and learnings.
	if err := mcpAgent.RegisterCustomTool(
		"publish_workflow_version",
		"Create a numbered snapshot of the current workflow state. Saves planning/config files plus learnings and evaluation learnings under versions/vN/. Use this before risky edits so you can restore later.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"label": map[string]interface{}{
					"type":        "string",
					"description": "Required version label describing this snapshot (for example: 'stable before refactor' or 'added bank validation').",
				},
			},
			"required": []string{"label"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			label, _ := args["label"].(string)
			label = strings.TrimSpace(label)
			if label == "" {
				return "label is required", nil
			}

			versions, err := iwm.controller.ListWorkspaceFiles(ctx, "versions")
			nextVersion := 1
			if err == nil {
				for _, name := range versions {
					var versionNum int
					if _, scanErr := fmt.Sscanf(name, "v%d", &versionNum); scanErr == nil && versionNum >= nextVersion {
						nextVersion = versionNum + 1
					}
				}
			}

			versionFolder := fmt.Sprintf("versions/v%d", nextVersion)
			var filesSnapshot []string

			for _, relPath := range workshopVersionedConfigFiles {
				exists, err := iwm.controller.CheckWorkspaceFileExists(ctx, relPath)
				if err != nil || !exists {
					continue
				}

				content, err := iwm.controller.ReadWorkspaceFile(ctx, relPath)
				if err != nil {
					logger.Warn(fmt.Sprintf("⚠️ publish_workflow_version: failed to read %s: %v", relPath, err))
					continue
				}
				if err := iwm.controller.WriteWorkspaceFile(ctx, versionFolder+"/"+relPath, content); err != nil {
					return "", fmt.Errorf("failed to write snapshot file %s: %w", relPath, err)
				}
				filesSnapshot = append(filesSnapshot, relPath)
			}

			for _, folderRoot := range workshopVersionedFolderRoots {
				items, err := listWorkshopWorkspaceTree(ctx, iwm.controller, folderRoot, 100)
				if err != nil {
					logger.Warn(fmt.Sprintf("⚠️ publish_workflow_version: failed to list %s: %v", folderRoot, err))
					continue
				}
				for _, item := range items {
					if item.Type == "folder" {
						continue
					}
					relPath := strings.TrimPrefix(item.FilePath, iwm.controller.GetWorkspacePath()+"/")
					if relPath == "" || relPath == item.FilePath {
						continue
					}
					content, err := iwm.controller.ReadWorkspaceFile(ctx, relPath)
					if err != nil {
						logger.Warn(fmt.Sprintf("⚠️ publish_workflow_version: failed to read %s: %v", relPath, err))
						continue
					}
					if err := iwm.controller.WriteWorkspaceFile(ctx, versionFolder+"/"+relPath, content); err != nil {
						return "", fmt.Errorf("failed to write snapshot file %s: %w", relPath, err)
					}
					filesSnapshot = append(filesSnapshot, relPath)
				}
			}

			if len(filesSnapshot) == 0 {
				return "No workflow config or learning files were found to version.", nil
			}

			meta := map[string]interface{}{
				"version":         nextVersion,
				"label":           label,
				"created_at":      time.Now().UTC().Format(time.RFC3339),
				"files_snapshot":  filesSnapshot,
				"managed_files":   workshopVersionedConfigFiles,
				"managed_folders": workshopVersionedFolderRoots,
			}
			metaJSON, _ := json.MarshalIndent(meta, "", "  ")
			if err := iwm.controller.WriteWorkspaceFile(ctx, versionFolder+"/version_meta.json", string(metaJSON)); err != nil {
				return "", fmt.Errorf("failed to write version metadata: %w", err)
			}

			return fmt.Sprintf("Published workflow version v%d (%s) with %d files. Restore later with restore_workflow_version(version=%d).", nextVersion, label, len(filesSnapshot), nextVersion), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register publish_workflow_version tool: %v", err))
	}

	// Tool: restore_workflow_version — restore a previous snapshot into the live workspace.
	if err := mcpAgent.RegisterCustomTool(
		"restore_workflow_version",
		"Restore a previously published workflow version from versions/vN/. This overwrites the current planning/config files and restores learnings from that snapshot.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"version": map[string]interface{}{
					"type":        "integer",
					"description": "Version number to restore (for example: 1 restores versions/v1).",
				},
			},
			"required": []string{"version"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			versionRaw, ok := args["version"].(float64)
			if !ok {
				return "version is required", nil
			}
			versionNum := int(versionRaw)
			if versionNum < 1 {
				return "version must be >= 1", nil
			}

			versionFolder := fmt.Sprintf("versions/v%d", versionNum)
			metaContent, err := iwm.controller.ReadWorkspaceFile(ctx, versionFolder+"/version_meta.json")
			if err != nil {
				return fmt.Sprintf("Version v%d not found.", versionNum), nil
			}

			var meta map[string]interface{}
			if err := json.Unmarshal([]byte(metaContent), &meta); err != nil {
				return "", fmt.Errorf("failed to parse version metadata: %w", err)
			}

			rawSnapshot, ok := meta["files_snapshot"].([]interface{})
			if !ok || len(rawSnapshot) == 0 {
				return fmt.Sprintf("Version v%d has no files to restore.", versionNum), nil
			}

			var snapshotPaths []string
			snapshotSet := make(map[string]struct{}, len(rawSnapshot))
			for _, item := range rawSnapshot {
				relPath, ok := item.(string)
				if !ok || relPath == "" {
					continue
				}
				snapshotPaths = append(snapshotPaths, relPath)
				snapshotSet[relPath] = struct{}{}
			}

			toStringSlice := func(value interface{}) []string {
				items, ok := value.([]interface{})
				if !ok {
					return nil
				}
				out := make([]string, 0, len(items))
				for _, item := range items {
					if s, ok := item.(string); ok && s != "" {
						out = append(out, s)
					}
				}
				return out
			}

			managedFiles := toStringSlice(meta["managed_files"])
			managedFolders := toStringSlice(meta["managed_folders"])

			for _, folderRoot := range managedFolders {
				fullFolderPath := resolveWorkshopWorkspacePath(iwm.controller, folderRoot)
				if err := iwm.controller.CleanupDirectory(ctx, fullFolderPath, folderRoot); err != nil {
					return "", fmt.Errorf("failed to clear %s before restore: %w", folderRoot, err)
				}
			}

			for _, relPath := range managedFiles {
				if _, exists := snapshotSet[relPath]; exists {
					continue
				}
				exists, err := iwm.controller.CheckWorkspaceFileExists(ctx, relPath)
				if err != nil || !exists {
					continue
				}
				if err := iwm.controller.DeleteWorkspaceFile(ctx, resolveWorkshopWorkspacePath(iwm.controller, relPath)); err != nil {
					return "", fmt.Errorf("failed to remove %s before restore: %w", relPath, err)
				}
			}

			filesRestored := 0
			for _, relPath := range snapshotPaths {
				content, err := iwm.controller.ReadWorkspaceFile(ctx, versionFolder+"/"+relPath)
				if err != nil {
					logger.Warn(fmt.Sprintf("⚠️ restore_workflow_version: failed to read %s from v%d: %v", relPath, versionNum, err))
					continue
				}
				if err := iwm.controller.WriteWorkspaceFile(ctx, relPath, content); err != nil {
					return "", fmt.Errorf("failed to restore %s: %w", relPath, err)
				}
				filesRestored++
			}

			label, _ := meta["label"].(string)
			if err := iwm.controller.LoadPlanForWorkshop(ctx); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ restore_workflow_version: restored files but failed to reload plan: %v", err))
			}

			if label != "" {
				return fmt.Sprintf("Restored workflow version v%d (%s). %d files restored.", versionNum, label, filesRestored), nil
			}
			return fmt.Sprintf("Restored workflow version v%d. %d files restored.", versionNum, filesRestored), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register restore_workflow_version tool: %v", err))
	}

	// === Schedule management tools ===

	// Tool: create_schedule — Create a new cron schedule
	if err := mcpAgent.RegisterCustomTool(
		"create_schedule",
		"Create a new cron schedule for this workflow. The schedule will automatically run the workflow at the specified times. Use mode='workshop' with messages to drive execution via the LLM (with per-step notifications). For optimizer schedules (workshop_mode='optimizer'), the message MUST instruct the agent to optimize steps one-by-one after each completion, skip already-optimized steps, limit retries to 2 per step, and move on after repeated failures to prevent infinite loops.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Display name for the schedule (e.g., 'Daily morning run').",
				},
				"cron_expression": map[string]interface{}{
					"type":        "string",
					"description": "5-field cron expression (minute hour day-of-month month day-of-week). Examples: '0 9 * * *' (daily 9 AM), '*/30 * * * *' (every 30 min), '0 0 * * 1' (weekly Monday midnight).",
				},
				"timezone": map[string]interface{}{
					"type":        "string",
					"description": "IANA timezone (e.g., 'America/New_York', 'Asia/Kolkata'). Defaults to 'UTC'.",
				},
				"group_ids": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Variable group IDs to run (e.g., 'group-1', 'group-2'). Read variables.json to see available groups. Empty = run all groups.",
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"description": "Execution mode: 'workflow' (default, direct orchestrator) or 'workshop' (LLM-driven via workshop builder with per-step notifications).",
					"enum":        []string{"workflow", "workshop"},
				},
				"messages": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Required when mode='workshop'. Predefined message queue sent one-by-one to the LLM. Messages should reference tools with full parameters. Example: ['Run the full workflow using run_full_workflow(group_id=\"group-1\", iteration=\"iteration-0\")']. Read variables.json for available group IDs.",
				},
				"workshop_mode": map[string]interface{}{
					"type":        "string",
					"description": "Workshop builder mode to use when mode='workshop'. Defaults to 'runner'. Use 'optimizer' to run with optimization (generate learnings, analyze steps).",
					"enum":        []string{"runner", "optimizer"},
				},
			},
			"required": []string{"name", "cron_expression"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			if iwm.schedulerWorkspacePath == "" {
				return "No workspace path associated with this workflow session.", nil
			}
			name, _ := args["name"].(string)
			cronExpr, _ := args["cron_expression"].(string)
			timezone, _ := args["timezone"].(string)
			var groupIDs []string
			if raw, ok := args["group_ids"]; ok && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							groupIDs = append(groupIDs, s)
						}
					}
				}
			}
			mode, _ := args["mode"].(string)
			var messages []string
			if raw, ok := args["messages"]; ok && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							messages = append(messages, s)
						}
					}
				}
			}
			workshopMode, _ := args["workshop_mode"].(string)
			if name == "" {
				return "name is required.", nil
			}
			if cronExpr == "" {
				return "cron_expression is required.", nil
			}
			// Validate: workshop mode requires messages
			if mode == "workshop" && len(messages) == 0 {
				return "messages is required when mode='workshop'. Provide at least one message, e.g. ['Run the full workflow using run_full_workflow(group_id=\"group-1\")'].", nil
			}
			return iwm.schedulerFuncs.CreateSchedule(ctx, iwm.schedulerWorkspacePath, name, cronExpr, timezone, groupIDs, mode, messages, workshopMode)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register create_schedule tool: %v", err))
	}

	// Tool: update_schedule — Update a schedule
	if err := mcpAgent.RegisterCustomTool(
		"update_schedule",
		"Update an existing schedule. Only provided fields are changed; omitted fields keep their current values.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule ID to update (from list_schedules).",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "New display name.",
				},
				"cron_expression": map[string]interface{}{
					"type":        "string",
					"description": "New 5-field cron expression.",
				},
				"timezone": map[string]interface{}{
					"type":        "string",
					"description": "New IANA timezone.",
				},
				"group_ids": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "New variable group IDs (e.g., 'group-1', 'group-2'). Read variables.json to see available groups. Pass empty array to clear (run all groups).",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable or disable the schedule.",
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"description": "Execution mode: 'workflow' (default, direct orchestrator) or 'workshop' (LLM-driven via workshop builder).",
					"enum":        []string{"workflow", "workshop"},
				},
				"messages": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Replaces existing messages. Messages should reference tools with full parameters, e.g. ['Run the full workflow using run_full_workflow(group_id=\"group-1\")'].",
				},
				"workshop_mode": map[string]interface{}{
					"type":        "string",
					"description": "Workshop builder mode: 'runner' (default) or 'optimizer'.",
					"enum":        []string{"runner", "optimizer"},
				},
			},
			"required": []string{"job_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			name, _ := args["name"].(string)
			cronExpr, _ := args["cron_expression"].(string)
			timezone, _ := args["timezone"].(string)
			var groupIDs []string
			setGroupIDs := false
			if raw, ok := args["group_ids"]; ok && raw != nil {
				setGroupIDs = true
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							groupIDs = append(groupIDs, s)
						}
					}
				}
			}
			var enabled *bool
			if raw, ok := args["enabled"]; ok && raw != nil {
				if b, ok := raw.(bool); ok {
					enabled = &b
				}
			}
			mode, _ := args["mode"].(string)
			var messages []string
			if raw, ok := args["messages"]; ok && raw != nil {
				if arr, ok := raw.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							messages = append(messages, s)
						}
					}
				}
			}
			workshopMode, _ := args["workshop_mode"].(string)
			return iwm.schedulerFuncs.UpdateSchedule(ctx, jobID, name, cronExpr, timezone, groupIDs, setGroupIDs, enabled, mode, messages, workshopMode)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register update_schedule tool: %v", err))
	}

	// Tool: delete_schedule — Delete a schedule
	if err := mcpAgent.RegisterCustomTool(
		"delete_schedule",
		"Permanently delete a schedule. This cannot be undone.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule ID to delete (from list_schedules).",
				},
			},
			"required": []string{"job_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			if err := iwm.schedulerFuncs.DeleteSchedule(ctx, jobID); err != nil {
				return "", err
			}
			return fmt.Sprintf("Schedule `%s` deleted.", jobID), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register delete_schedule tool: %v", err))
	}

	// Tool: trigger_schedule — Manually trigger a schedule run
	if err := mcpAgent.RegisterCustomTool(
		"trigger_schedule",
		"Manually trigger a schedule to run immediately, outside its normal cron timing.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule ID to trigger (from list_schedules).",
				},
			},
			"required": []string{"job_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			return iwm.schedulerFuncs.TriggerSchedule(ctx, jobID)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register trigger_schedule tool: %v", err))
	}

	// Tool: get_schedule_runs — Get run history for a schedule
	if err := mcpAgent.RegisterCustomTool(
		"get_schedule_runs",
		"View the execution history for a specific schedule, including status, duration, and errors.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "The schedule ID to get runs for (from list_schedules).",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of runs to return. Defaults to 10.",
				},
			},
			"required": []string{"job_id"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.schedulerFuncs == nil {
				return "Schedule management not available in this session.", nil
			}
			jobID, _ := args["job_id"].(string)
			if jobID == "" {
				return "job_id is required.", nil
			}
			limit := 10
			if raw, ok := args["limit"]; ok && raw != nil {
				if f, ok := raw.(float64); ok {
					limit = int(f)
				}
			}
			return iwm.schedulerFuncs.GetScheduleRuns(ctx, jobID, limit)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register get_schedule_runs tool: %v", err))
	}

	// === Skill management tools ===

	// Tool: list_skills — List all available skills in the workspace
	if err := mcpAgent.RegisterCustomTool(
		"list_skills",
		"List all available skills in the workspace. Shows both selected skills (used by this workflow) and all discovered skills.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil {
				return "Skill management not available in this session.", nil
			}
			return iwm.skillFuncs.ListSkills(ctx)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register list_skills tool: %v", err))
	}

	// Tool: import_skill — Import a skill from GitHub
	if err := mcpAgent.RegisterCustomTool(
		"import_skill",
		"Import a skill from GitHub into the workspace. The skill will be downloaded and available for use in workflows. Use list_skills first to see what's already available.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"github_url": map[string]interface{}{
					"type":        "string",
					"description": "GitHub URL of the skill to import. Can be a repository URL or a path to a specific skill folder (e.g., 'https://github.com/org/repo/tree/main/skills/my-skill').",
				},
				"token": map[string]interface{}{
					"type":        "string",
					"description": "Optional GitHub personal access token for private repositories.",
				},
			},
			"required": []string{"github_url"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil {
				return "Skill management not available in this session.", nil
			}
			githubURL, _ := args["github_url"].(string)
			if githubURL == "" {
				return "github_url is required.", nil
			}
			token, _ := args["token"].(string)
			return iwm.skillFuncs.ImportSkill(ctx, githubURL, token)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register import_skill tool: %v", err))
	}

	// Tool: uninstall_skill — Uninstall a skill from the workspace
	if err := mcpAgent.RegisterCustomTool(
		"uninstall_skill",
		"Uninstall a skill from the workspace. Removes skill files and version tracking. Use list_skills first to see available skills and their folder names.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"folder_name": map[string]interface{}{
					"type":        "string",
					"description": "The folder name of the skill to uninstall (from list_skills).",
				},
			},
			"required": []string{"folder_name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil {
				return "Skill management not available in this session.", nil
			}
			folderName, _ := args["folder_name"].(string)
			if folderName == "" {
				return "folder_name is required.", nil
			}
			if err := iwm.skillFuncs.DeleteSkill(ctx, folderName); err != nil {
				return fmt.Sprintf("Failed to uninstall skill %q: %v", folderName, err), nil
			}
			return fmt.Sprintf("Successfully uninstalled skill %q from workspace.", folderName), nil
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register uninstall_skill tool: %v", err))
	}

	// Tool: search_skills — Search the skills registry
	if err := mcpAgent.RegisterCustomTool(
		"search_skills",
		"Search for skills in the public skills registry. Returns matching skills with install commands. Use install_skill to install a result.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query (e.g., 'social media', 'browser automation', 'data visualization').",
				},
			},
			"required": []string{"query"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil || iwm.skillFuncs.SearchSkills == nil {
				return "Skill search not available. The skills CLI (npx) may not be installed.", nil
			}
			query, _ := args["query"].(string)
			if query == "" {
				return "query is required.", nil
			}
			return iwm.skillFuncs.SearchSkills(ctx, query)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register search_skills tool: %v", err))
	}

	// Tool: install_skill — Install a skill via the skills CLI
	if err := mcpAgent.RegisterCustomTool(
		"install_skill",
		"Install a skill from the public skills registry using owner/repo@skill-name format. Use search_skills first to find available skills.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source": map[string]interface{}{
					"type":        "string",
					"description": "Skill source in owner/repo@skill-name format (e.g., 'anthropics/skills@skill-creator', 'vercel-labs/agent-browser@agent-browser').",
				},
			},
			"required": []string{"source"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if iwm.skillFuncs == nil || iwm.skillFuncs.InstallSkill == nil {
				return "Skill installation not available. The skills CLI (npx) may not be installed.", nil
			}
			source, _ := args["source"].(string)
			if source == "" {
				return "source is required (e.g., 'owner/repo@skill-name').", nil
			}
			return iwm.skillFuncs.InstallSkill(ctx, source)
		},
		"workflow",
	); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Failed to register install_skill tool: %v", err))
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

## ROLE
Perform deep analysis of a step's execution and produce a comprehensive optimization report. You are **read-only** — you do NOT modify any files, plans, or configurations.

## RULES
1. **Read-Only**: Do NOT modify any files. Use shell commands only for reading files (cat, ls, head, etc.).
2. **Be Specific**: Reference exact file paths, line numbers, field names, and values in your analysis.
3. **Be Actionable**: Every recommendation must be something the user can act on immediately.
4. **Prioritize by Impact**: Rank recommendations by how much they'd improve the step's reliability and output quality.

## STEP CONTEXT

- **Step ID**: {{.StepID}}
- **Workspace**: {{.WorkspacePath}}
- **Run Folder**: {{.RunFolder}}

{{if .StepPlanJSON}}### Step Definition (from plan.json)
`+"```json\n{{.StepPlanJSON}}\n```"+`
{{end}}

{{if .StepConfigJSON}}### Step Config
`+"```json\n{{.StepConfigJSON}}\n```"+`
{{end}}

{{if .ValidationResult}}### Latest Validation Result
`+"```json\n{{.ValidationResult}}\n```"+`
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

## DATA LAYOUT

All paths relative to workspace root:
- Execution output: `+"`runs/{{.RunFolder}}/execution/step-{{.StepNum}}/`"+`
- Execution logs: `+"`runs/{{.RunFolder}}/logs/step-{{.StepNum}}/execution/`"+`
- Validation logs: `+"`runs/{{.RunFolder}}/logs/step-{{.StepNum}}/`"+`
- Learnings: `+"`learnings/{{.StepID}}/`"+`
- Plan: `+"`planning/plan.json`"+`
- Step config: `+"`planning/step_config.json`"+`

## ANALYSIS PROCEDURE

1. **Read execution logs** — Check the latest conversation history for tool calls, errors, retries
2. **Read actual output files** — Compare output against success criteria and validation schema
3. **Review existing learnings** — Are they specific? Actionable? Or noisy/generic?
4. **Analyze tool/server usage** — Are there unused servers? Missing tools?
5. **Check validation schema** — Does it catch stale files? Are there enough field checks?
6. **Check step description** — Is it clear, specific, and actionable?

## REPORT FORMAT

Produce your report in this exact markdown structure:

### Summary
1-2 sentence overall assessment of the step's health and output quality.

### Output Quality
- Does the output meet success criteria? What's wrong or missing?
- Are there format issues, missing fields, or incorrect values?
- Compare actual output content against what was expected.

### Hardcoded Values Check
Scan the step description (from plan.json) AND learnings (SKILL.md) for hardcoded values that should use variable placeholders instead:
- **Paths**: Absolute workspace paths (e.g., `+"`/Users/...`"+`, `+"`/home/...`"+`, `+"`C:\\...`"+`) — should use `+"`"+`{{"{{WORKSPACE_PATH}}"}}`+"`"+` or relative paths
- **Secrets/credentials**: API keys, tokens, passwords, auth headers — should use secret variables from variables.json
- **User-specific values**: Account IDs, usernames, emails, phone numbers, URLs with specific domains — should use variable placeholders (e.g., `+"`{USER_ID}`"+`, `+"`{EMAIL}`"+`)
- **Environment-specific values**: Hardcoded ports, hostnames, database names — should be parameterized
For each hardcoded value found, recommend the specific variable placeholder to use and where to define it.

### Learnings Review
- Which existing learnings are good (specific, actionable)?
- Which are noisy, generic, or outdated?
- What patterns are missing that should be captured?

### Config Recommendations
- Tool/server scoping: should servers be added or removed?
- LLM tier: is the current model appropriate for this step's complexity?
- Execution mode: any changes needed?
- Learning config: should learning be disabled, locked, or detail level changed?
- **Human feedback tool**: Check if `+"`human_feedback`"+` was used in execution logs. If it was NOT used, recommend removing `+"`human_tools:*`"+` from `+"`enabled_custom_tools`"+` — unused human tools add noise and slow down execution. If it WAS used, confirm it's needed and check whether the interaction could be automated instead.

### Plan Recommendations
- Description improvements: what should be added, clarified, or removed?
- Success criteria: are they sufficient and testable?
- Validation schema: missing checks, too loose, or too strict?
- Context dependencies: any missing or unnecessary dependencies?

{{if .ComplexStepDetails}}
### Tier Selection Analysis (Todo Task Steps)
If this is a todo_task step with sub-agents, analyze tier usage from the routing log:
- **Per-route tier analysis**: For each route/sub-agent, was the tier appropriate for the task complexity?
  - Tier 1 (High) is for complex, novel, or critical tasks that need strong reasoning
  - Tier 2 (Medium) is for routine, well-defined tasks with clear patterns
  - Tier 3 (Low) is for simple, repetitive tasks (formatting, validation, file ops)
- **Over-provisioned routes**: Which routes used Tier 1 but could succeed at Tier 2 or 3? (cost savings)
- **Under-provisioned routes**: Which routes failed or struggled at a lower tier and should use a higher tier?
- **Tier recommendations**: For each route, recommend a specific tier with reasoning. Format as:
  - Route: {route_name} → Recommended Tier: {1/2/3} — Reason: {why}
- **SKILL.md tier section**: Recommend adding a TIER RECOMMENDATIONS section to the orchestration SKILL.md that the orchestrator can read at runtime, e.g.:
`+"```"+`
## TIER RECOMMENDATIONS
- route: {route_id} | tier: {1/2/3} | reason: {brief justification}
- route: generic | tier: {1/2/3} | reason: {brief justification}
`+"```"+`
{{end}}

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

	// LLM — use phase LLM (same model as the workshop agent)
	var llmConfigToUse *orchestrator.LLMConfig
	if iwm.controller.presetPhaseLLM != nil && iwm.controller.presetPhaseLLM.Provider != "" && iwm.controller.presetPhaseLLM.ModelID != "" {
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: iwm.controller.presetPhaseLLM.Provider,
				ModelID:  iwm.controller.presetPhaseLLM.ModelID,
			},
			Fallbacks: iwm.controller.GetFallbacks(),
			APIKeys:   iwm.controller.GetAPIKeys(),
		}
	} else {
		return "", fmt.Errorf("no valid LLM configuration found for optimization agent: phase LLM is not configured")
	}

	config := iwm.controller.CreateStandardAgentConfigWithLLM("optimization-agent", 50, agents.OutputFormatStructured, llmConfigToUse)
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
		"StepID":                  stepID,
		"StepTitle":               targetStep.GetTitle(),
		"StepDescription":         targetStep.GetDescription(),
		"StepSuccessCriteria":     targetStep.GetSuccessCriteria(),
		"StepContextOutput":       targetStep.GetContextOutput().String(),
		"StepContextDependencies": contextDeps,
		"WorkspacePath":           workspacePath,
		"RunFolder":               runFolder,
		"StepNum":                 fmt.Sprintf("%d", stepNum),
		"StepPlanJSON":            stepPlanJSON,
		"StepConfigJSON":          stepConfigJSON,
		"ValidationResult":        validationResult,
		"ExistingLearnings":       existingLearnings,
		"ToolUsageSummary":        toolUsageSummary,
		"ComplexStepDetails":      complexStepDetails,
		"Focus":                   focus,
		"SessionID":               iwm.sessionID,
		"WorkflowID":              iwm.workflowID,
	}

	// --- Execute ---

	logger.Info(fmt.Sprintf("🔍 Running optimization agent for step %q (focus: %q)", stepID, focus))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("optimization agent failed: %w", err)
	}

	return result, nil
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

// ============================================================================
// Background Task Agent — standalone agent for run_in_background tool
// ============================================================================

var backgroundTaskAgentSystemTemplate = MustRegisterTemplate("backgroundTaskAgentSystem", `# Background Task Agent

You are a background agent spawned by the workflow builder to perform a specific task. You have access to the same workspace tools as the workflow execution agents.

**Workspace folder:** {{.WorkspacePath}}

## Instructions
Complete the task described in the user message below. Be thorough and specific in your output.
When you finish, summarize what you did and any important findings.

{{.SkillPrompt}}
{{.SecretPrompt}}
{{.BrowserPrompt}}
{{.GWSPrompt}}
`)

var backgroundTaskAgentUserTemplate = MustRegisterTemplate("backgroundTaskAgentUser", `{{.Instruction}}`)

// WorkflowBackgroundTaskAgent is a standalone agent spawned by run_in_background
type WorkflowBackgroundTaskAgent struct {
	*agents.BaseOrchestratorAgent
}

func newWorkflowBackgroundTaskAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowBackgroundTaskAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		"workshop-background-task", // Must match the manual start/end events in the goroutine for frontend dedup
		eventBridge,
	)
	return &WorkflowBackgroundTaskAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements OrchestratorAgent interface for the background task agent
func (agent *WorkflowBackgroundTaskAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	baseAgent := agent.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil || baseAgent.Agent() == nil {
		return "", nil, fmt.Errorf("agent not initialized")
	}

	// Templates
	var systemPrompt, userMessage strings.Builder
	if err := backgroundTaskAgentSystemTemplate.Execute(&systemPrompt, templateVars); err != nil {
		return "", nil, err
	}
	if err := backgroundTaskAgentUserTemplate.Execute(&userMessage, templateVars); err != nil {
		return "", nil, err
	}

	// Single-pass execution
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

// runBackgroundTodoTaskAgent runs a todo task orchestrator as a background agent.
// Unlike runBackgroundTaskAgent (single-pass), this supports multi-step task management
// and sub-agent delegation via call_generic_agent. Sub-agent completions auto-notify
// the main workshop agent via the subAgentNotifier already set on the controller.
func (iwm *InteractiveWorkshopManager) runBackgroundTodoTaskAgent(ctx context.Context, name, instruction string) (string, error) {
	stepID := fmt.Sprintf("bg-todo-%s-%d", strings.ToLower(strings.ReplaceAll(name, " ", "-")), time.Now().UnixNano()%100000)

	// Build a minimal TodoTaskPlanStep from the instruction
	innerStep := &RegularPlanStep{
		Type: StepTypeRegular,
		CommonStepFields: CommonStepFields{
			ID:              stepID + "-inner",
			Title:           name,
			Description:     instruction,
			SuccessCriteria: fmt.Sprintf("Complete all tasks described in the instruction for: %s", name),
		},
	}
	todoStep := &TodoTaskPlanStep{
		Type:               StepTypeTodoTask,
		ID:                 stepID,
		Title:              name,
		TodoTaskStep:       innerStep,
		PredefinedRoutes:   nil, // generic agent only
		EnableGenericAgent: true,
		NextStepID:         "end",
	}

	execCtx := &ExecutionContext{
		SkipHumanInput:     true,
		FastExecuteMode:    false,
		FastExecuteEndStep: -1,
		RunSingleStepOnly:  false,
		SingleStepTarget:   -1,
		IsEvaluationMode:   false,
	}

	_, _, err := iwm.controller.executeTodoTaskStep(
		ctx,
		todoStep,
		0,
		&StepProgress{},
		[]string{},
		[]string{},
		0,
		execCtx,
		[]PlanStepInterface{todoStep},
		stepID,
		nil,
	)
	if err != nil {
		return fmt.Sprintf("Background todo task %q failed: %v", name, err), err
	}
	return fmt.Sprintf("Background todo task %q completed.", name), nil
}

// runBackgroundTaskAgent creates and runs a standalone background agent
func (iwm *InteractiveWorkshopManager) runBackgroundTaskAgent(ctx context.Context, name string, instruction string) (string, error) {
	logger := iwm.controller.GetLogger()

	// --- Folder guard: same as workshop agent ---
	workspacePath := iwm.controller.GetWorkspacePath()
	knowledgebasePath := getKnowledgebasePath(workspacePath)
	readPaths := []string{
		workspacePath,
		fmt.Sprintf("%s/runs", workspacePath),
		fmt.Sprintf("%s/learnings", workspacePath),
		fmt.Sprintf("%s/planning", workspacePath),
		knowledgebasePath,
		"Chats",
	}
	writePaths := []string{
		workspacePath,
	}
	iwm.controller.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	// --- LLM: use phase LLM (same tier as planning/analysis agents) ---
	var llmConfigToUse *orchestrator.LLMConfig
	if iwm.controller.presetPhaseLLM != nil && iwm.controller.presetPhaseLLM.Provider != "" && iwm.controller.presetPhaseLLM.ModelID != "" {
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: iwm.controller.presetPhaseLLM.Provider,
				ModelID:  iwm.controller.presetPhaseLLM.ModelID,
			},
			Fallbacks: iwm.controller.GetFallbacks(),
			APIKeys:   iwm.controller.GetAPIKeys(),
		}
	} else if iwm.presetLLM != nil && iwm.presetLLM.Provider != "" && iwm.presetLLM.ModelID != "" {
		// Fallback to workshop builder LLM
		llmConfigToUse = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: iwm.presetLLM.Provider,
				ModelID:  iwm.presetLLM.ModelID,
			},
			Fallbacks: iwm.controller.GetFallbacks(),
			APIKeys:   iwm.controller.GetAPIKeys(),
		}
	} else {
		return "", fmt.Errorf("no valid LLM configuration found for background task agent")
	}

	// --- Agent config ---
	config := iwm.controller.CreateStandardAgentConfigWithLLM(fmt.Sprintf("Background: %s", name), 80, agents.OutputFormatStructured, llmConfigToUse)
	isCodeExecMode := iwm.controller.GetUseCodeExecutionMode()
	config.UseCodeExecutionMode = isCodeExecMode
	config.UseToolSearchMode = iwm.controller.GetUseToolSearchMode()
	config.EnableParallelToolExecution = true

	// --- Tools: same as default execution agent (all workspace tools) ---
	toolsToRegister, executorsToUse := iwm.controller.prepareCustomTools(nil) // nil = default tools

	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, log loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return newWorkflowBackgroundTaskAgent(cfg, log, tracer, eventBridge)
	}

	// PushContext before setup so the shared bridge context is preserved for concurrent agents.
	// setupStandardAgent calls SetOrchestratorContext which overwrites the bridge — without
	// push/pop this corrupts the main agent's metadata when the bg task runs in a goroutine.
	if cab, ok := iwm.controller.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PushContext("background-task", 0, "background-task", fmt.Sprintf("Background: %s", name))
	}

	agent, err := iwm.controller.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"background-task",
		0, 0,
		"background-task",
		createAgentFunc,
		toolsToRegister,
		executorsToUse,
		true, // overwriteSystemPrompt — we provide our own
	)

	// Immediately restore bridge context — bg task events use ForceCorrelationIDKey from ctx,
	// not the bridge's current context, so restoring here is safe.
	if cab, ok := iwm.controller.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PopContext()
	}

	if err != nil {
		return "", fmt.Errorf("failed to create background task agent: %w", err)
	}

	// --- Post-setup: add skill/secret/browser prompts ---
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return "", fmt.Errorf("base agent is nil after creation")
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return "", fmt.Errorf("mcp agent is nil after creation")
	}

	// Build supplementary prompts
	skillPrompt := ""
	effectiveSkills := GetEffectiveSkills(nil, iwm.controller.BaseOrchestrator)
	if len(effectiveSkills) > 0 {
		skillPrompt = BuildWorkflowSkillPrompt(ctx, effectiveSkills, iwm.controller.BaseOrchestrator, GetPromptDocsRoot())
	}

	secretPrompt := ""
	effectiveSecrets := GetEffectiveSecrets(iwm.controller.BaseOrchestrator)
	if len(effectiveSecrets) > 0 {
		secretPrompt = BuildWorkflowSecretPrompt(effectiveSecrets)
	}

	bgBrowserCfg := iwm.controller.resolveBrowserConfig(config.ServerNames, effectiveSkills)
	browserPrompt := instructions.BuildBrowserInstructions(bgBrowserCfg)

	// GWS instructions
	gwsPrompt := ""
	for _, s := range config.ServerNames {
		if s == "gws" {
			gwsPrompt = instructions.GetGWSQuickStartInstructions()
			break
		}
	}

	// Apply post-setup configuration (folder guard + registry for code execution mode)
	if err := iwm.controller.applyPostSetupToAgent(agent, "background-task-agent", isCodeExecMode); err != nil {
		logger.Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for background-task-agent: %v", err))
	}

	// --- Template vars ---
	templateVars := map[string]string{
		"WorkspacePath": workspacePath,
		"Instruction":   instruction,
		"SkillPrompt":   skillPrompt,
		"SecretPrompt":  secretPrompt,
		"BrowserPrompt": browserPrompt,
		"GWSPrompt":     gwsPrompt,
	}

	// --- Execute ---
	logger.Info(fmt.Sprintf("🚀 Running background task agent: %q", name))
	result, _, err := agent.Execute(ctx, templateVars, nil)
	if err != nil {
		return "", fmt.Errorf("background task agent failed: %w", err)
	}

	return result, nil
}
