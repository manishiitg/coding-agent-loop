package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/cmd/server/services"
	"mcp-agent-builder-go/agent_go/pkg/workspace"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetDelegationToolCategory returns the category name for delegation tools
// Using "human" category makes the tool directly available (not a code tool) in:
// - Code execution mode: human category tools are always directly callable
// - Tool search mode: human category tools are immediately available without searching
func GetDelegationToolCategory() string {
	return "human"
}

// Context keys for delegation tool execution
type delegationContextKey string

const (
	// ExecuteDelegatedTaskKey is the context key for the delegation execution function
	ExecuteDelegatedTaskKey delegationContextKey = "execute_delegated_task"
	// DelegationDepthKey is the context key for tracking delegation depth
	DelegationDepthKey delegationContextKey = "delegation_depth"
	// MaxDelegationDepth is the maximum allowed delegation depth to prevent infinite recursion
	MaxDelegationDepth = 3
	// WorkspaceClientKey is the context key for the workspace client (plan file I/O)
	WorkspaceClientKey delegationContextKey = "workspace_client"
	// DelegationTierConfigKey is the context key for the delegation tier configuration
	DelegationTierConfigKey delegationContextKey = "delegation_tier_config"
	// ReasoningLevelKey is the context key for the reasoning level of a delegation
	ReasoningLevelKey delegationContextKey = "reasoning_level"
	// PlanEventEmitterKey is the context key for emitting workspace file events when plans are saved
	PlanEventEmitterKey delegationContextKey = "plan_event_emitter"
	// PlanFolderKey is the context key for the plan-specific output folder (e.g. "Plans/{planID}")
	PlanFolderKey delegationContextKey = "plan_folder"
	// CapabilitiesContextKey is the context key for available capabilities (MCP servers, skills, etc.)
	CapabilitiesContextKey delegationContextKey = "capabilities_context"
	// ToolModeKey is the context key for the tool mode of a delegated task ("simple", "code_execution", "tool_search")
	ToolModeKey delegationContextKey = "tool_mode"
	// AgentTemplateKey is the context key for the sub-agent template folder name
	AgentTemplateKey delegationContextKey = "agent_template"
	// PlanTrackerKey is the context key for tracking whether a plan has been created in this session
	PlanTrackerKey delegationContextKey = "plan_tracker"
	// PlanSessionStateKey is the context key for the session-level plan phase state
	PlanSessionStateKey delegationContextKey = "plan_session_state"
	// SessionEventEmitterKey is the context key for the session event emitter (used by confirm_plan_execution)
	SessionEventEmitterKey delegationContextKey = "session_event_emitter"
	// PlanFileFolderPath is the workspace folder for delegation plan files
	PlanFileFolderPath = "Plans"
)

// PlanEventEmitter is the interface for emitting workspace file events when plans are saved
type PlanEventEmitter interface {
	EmitFileEvent(filepath string)
}

// SessionEventEmitter is the interface for emitting arbitrary events to the session event store
type SessionEventEmitter interface {
	EmitBlockingHumanFeedback(requestID, question, context string, yesNoOnly bool, yesLabel, noLabel string, options ...string)
}

// PlanSessionState tracks session-level plan state for multi-agent mode.
// It replaces PlanTracker and adds phase tracking (planning vs execution).
// Shared across tool calls within a session to enforce one-plan-per-conversation.
type PlanSessionState struct {
	mu         sync.Mutex
	Phase      string // "planning" or "execution"
	PlanID     string
	PlanFolder string
}

// NewPlanSessionState creates a new plan session state for a session
func NewPlanSessionState() *PlanSessionState {
	return &PlanSessionState{Phase: "planning"}
}

// TryCreate atomically checks if a plan already exists. If not, records the new plan
// and returns true. During execution phase, allows creating a new plan for follow-up tasks
// (resets state back to planning). If a plan already exists during planning phase,
// returns false with existing plan info.
func (ps *PlanSessionState) TryCreate(planID, planFolder string) (existingPlanID, existingPlanFolder string, ok bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.PlanID != "" {
		if ps.Phase == "execution" {
			// Allow follow-up: reset and create new plan
			ps.PlanID = planID
			ps.PlanFolder = planFolder
			ps.Phase = "planning"
			return "", "", true
		}
		return ps.PlanID, ps.PlanFolder, false
	}
	ps.PlanID = planID
	ps.PlanFolder = planFolder
	return "", "", true
}

// GetPhase returns the current phase (thread-safe)
func (ps *PlanSessionState) GetPhase() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.Phase
}

// SetPhase sets the current phase (thread-safe)
func (ps *PlanSessionState) SetPhase(phase string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.Phase = phase
}

// PlanTracker is an alias for backward compatibility
type PlanTracker = PlanSessionState

// NewPlanTracker creates a new plan tracker (backward-compatible alias)
func NewPlanTracker() *PlanTracker {
	return NewPlanSessionState()
}

// DelegationTierConfig holds provider/model for each reasoning tier
type DelegationTierConfig struct {
	High   *TierModel `json:"high,omitempty"`
	Medium *TierModel `json:"medium,omitempty"`
	Low    *TierModel `json:"low,omitempty"`
}

// TierModel represents a specific provider+model for a tier
type TierModel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
}

// CapabilitiesContext describes the available tools, servers, and skills for the planner
type CapabilitiesContext struct {
	EnabledServers    []string                  `json:"enabled_servers,omitempty"`
	SelectedTools     []string                  `json:"selected_tools,omitempty"` // "server:tool" format
	Skills            []SkillSummary            `json:"skills,omitempty"`
	SubAgentTemplates []SubAgentTemplateSummary `json:"subagent_templates,omitempty"`
	HasWorkspace      bool                      `json:"has_workspace"`
	HasBrowser        bool                      `json:"has_browser"`
}

// SkillSummary holds minimal info about a skill for the planner prompt
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	FolderName  string `json:"folder_name"`
}

// SubAgentTemplateSummary holds minimal info about a sub-agent template for the planner prompt
type SubAgentTemplateSummary struct {
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	FolderName            string   `json:"folder_name"`
	DefaultReasoningLevel string   `json:"default_reasoning_level,omitempty"`
	DefaultToolMode       string   `json:"default_tool_mode,omitempty"`
	Skills                []string `json:"skills,omitempty"`  // Skill folder names to auto-activate
	Servers               []string `json:"servers,omitempty"` // MCP server names to enable
}

// ExecuteDelegatedTaskFunc is the function signature for executing delegated tasks
// Injected via context by the server
type ExecuteDelegatedTaskFunc func(ctx context.Context, instruction string) (string, error)

// CreateDelegationTools creates all delegation virtual tools
func CreateDelegationTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// delegate tool - Execute a sub-agent to handle a task
	delegateTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "delegate",
			Description: "Delegate a task to a sub-agent for execution. Provide comprehensive, self-contained instructions.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"instruction": map[string]interface{}{
						"type":        "string",
						"description": "Comprehensive, self-contained instructions for the sub-agent. Include all necessary context, requirements, and expected outcomes.",
					},
					"reasoning_level": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"high", "medium", "low"},
						"description": "Optional reasoning tier for this task. 'high' for complex planning/architecture, 'medium' for standard implementation, 'low' for simple tasks like formatting/tests. If not specified, uses the parent agent's model.",
					},
					"plan_folder": map[string]interface{}{
						"type":        "string",
						"description": "Optional plan folder path (e.g. 'Plans/{plan_id}'). When set, the worker's write access is restricted to this folder only. Always pass this when executing tasks from a plan.",
					},
					"tool_mode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"simple", "code_execution", "tool_search"},
						"description": "Tool access mode for the worker. 'simple' (default): worker gets all tools directly and calls them normally — use this for most tasks including writing scripts, file editing, shell commands. 'code_execution': worker writes Go code that calls MCP tools programmatically — use ONLY for batch MCP tool operations (e.g., fetching data from 50 APIs in a loop, processing MCP tool results with complex logic). NOT for writing Python/Bash scripts or general coding. 'tool_search': worker discovers tools on-demand via search — use ONLY when 30+ MCP tools are available and the worker needs to find the right ones.",
					},
					"agent_template": map[string]interface{}{
						"type":        "string",
						"description": "Sub-agent template folder name from subagents/. Loads specialized instructions, default reasoning level, default tool mode, and auto-activates the template's configured skills and MCP servers for the sub-agent.",
					},
				},
				"required": []string{"instruction"},
			}),
		},
	}
	tools = append(tools, delegateTool)

	// create_delegation_plan - Delegate planning to a sub-agent that writes plan.md
	createPlanTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "create_delegation_plan",
			Description: "Delegate planning to a sub-agent that analyzes the objective, creates a strategy, and writes a plan.md todo list to workspace. The plan file appears in Plans/{plan_name}/ for tracking. After the plan is created, read plan.md and delegate tasks from it using the delegate tool.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan_name": map[string]interface{}{
						"type":        "string",
						"description": "Short kebab-case name for the plan folder (e.g. 'user-auth', 'api-refactor', 'bug-fix-login'). This becomes the folder name under Plans/. Keep it concise (2-4 words, kebab-case).",
					},
					"objective": map[string]interface{}{
						"type":        "string",
						"description": "What needs to be accomplished. The planner sub-agent will analyze this and create a task breakdown.",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Optional additional context for the planner: constraints, file paths, tech stack, or any information that helps create a better plan.",
					},
					"reasoning_level": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"high", "medium", "low"},
						"description": "Optional reasoning tier for the planner sub-agent. 'high' for complex multi-step projects, 'medium' for standard planning, 'low' for simple task breakdowns. If not specified, uses the parent agent's model.",
					},
				},
				"required": []string{"plan_name", "objective"},
			}),
		},
	}
	tools = append(tools, createPlanTool)

	// confirm_plan_execution - Blocking approval tool for plan execution
	confirmPlanTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "confirm_plan_execution",
			Description: "Present the plan to the user for approval. This tool BLOCKS until the user responds. If approved, switches to Execution Mode. If rejected, returns the user's feedback so you can adjust the plan. Call this after creating a plan and summarizing it.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan_summary": map[string]interface{}{
						"type":        "string",
						"description": "Brief summary of the plan to show the user (key phases, tasks, models used)",
					},
				},
				"required": []string{"plan_summary"},
			}),
		},
	}
	tools = append(tools, confirmPlanTool)

	return tools
}

// CreateDelegationToolExecutors creates the execution functions for delegation tools
// Note: These executors require context injection from the server to work
func CreateDelegationToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["delegate"] = handleDelegate
	executors["create_delegation_plan"] = handleCreateDelegationPlan
	executors["confirm_plan_execution"] = handleConfirmPlanExecution

	return executors
}

// handleDelegate executes a delegated task via sub-agent
func handleDelegate(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract instruction argument
	instruction, ok := args["instruction"].(string)
	if !ok || instruction == "" {
		return "", fmt.Errorf("instruction is required")
	}

	// Extract optional reasoning_level, plan_folder, and tool_mode
	reasoningLevel, _ := args["reasoning_level"].(string)
	if reasoningLevel != "" && reasoningLevel != "high" && reasoningLevel != "medium" && reasoningLevel != "low" {
		reasoningLevel = "" // Ignore invalid values
	}
	planFolder, _ := args["plan_folder"].(string)
	toolMode, _ := args["tool_mode"].(string)
	if toolMode != "" && toolMode != "simple" && toolMode != "code_execution" && toolMode != "tool_search" {
		toolMode = "" // Ignore invalid values
	}
	agentTemplate, _ := args["agent_template"].(string)

	// Check delegation depth to prevent infinite recursion
	currentDepth := 0
	if depth, ok := ctx.Value(DelegationDepthKey).(int); ok {
		currentDepth = depth
	}

	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached - cannot delegate further to prevent infinite recursion", MaxDelegationDepth)
	}

	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecuteDelegatedTaskKey).(ExecuteDelegatedTaskFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("delegation execution function not available - delegation mode may not be enabled")
	}

	log.Printf("[DELEGATION] Executing delegated task at depth %d: %s", currentDepth+1, truncateString(instruction, 100))

	startTime := time.Now()

	// Increment depth in context for the sub-agent and pass reasoning level + plan folder
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
	if reasoningLevel != "" {
		subCtx = context.WithValue(subCtx, ReasoningLevelKey, reasoningLevel)
	}
	if planFolder != "" {
		subCtx = context.WithValue(subCtx, PlanFolderKey, planFolder)
	}
	if toolMode != "" {
		subCtx = context.WithValue(subCtx, ToolModeKey, toolMode)
	}
	if agentTemplate != "" {
		subCtx = context.WithValue(subCtx, AgentTemplateKey, agentTemplate)
	}

	// Execute the delegated task
	result, err := executeFunc(subCtx, instruction)

	executionTime := time.Since(startTime)

	// Build result
	delegationResult := map[string]interface{}{
		"success":        err == nil,
		"result":         result,
		"execution_time": executionTime.String(),
		"completed_at":   time.Now().Format(time.RFC3339),
		"depth":          currentDepth + 1,
	}

	if err != nil {
		delegationResult["error"] = err.Error()
		log.Printf("[DELEGATION] Delegated task failed at depth %d: %v", currentDepth+1, err)
	} else {
		log.Printf("[DELEGATION] Delegated task completed at depth %d in %s", currentDepth+1, executionTime)
	}

	resultJSON, _ := json.MarshalIndent(delegationResult, "", "  ")
	return string(resultJSON), nil
}

// buildPlannerPrompt creates the system instruction for the planner sub-agent
// It includes capabilities context so the planner knows what tools/servers/skills are available
func buildPlannerPrompt(caps *CapabilitiesContext, planFolder string) string {
	var sb strings.Builder

	sb.WriteString(`You are a Planner. Your job is to analyze an objective and produce a structured plan as a markdown file.

## Your Task
1. Use your tools to **research and understand** the problem — read files, query data, explore the workspace, read skill instructions
2. Break the objective into concrete, actionable tasks based on what you learned
3. Write the plan as a markdown file to the output path specified below

**IMPORTANT: You are ONLY planning, NOT executing.** Do not perform any of the plan steps yourself. Your only output is the plan.md file. Use tools solely for research and discovery to write a better plan.

## Output File
Write your plan to: ` + "`" + planFolder + `/plan.md` + "`" + `

## Plan Format
Write the plan as a structured markdown document with these sections:

` + "```" + `markdown
# Plan: {Short descriptive title}

## Goal
{Clear 1-2 sentence statement of what we're trying to achieve}

## Constraints
- {Known constraint 1 — e.g., environment, technology stack, data source}
- {Known constraint 2 — e.g., file locations, API endpoints, dependencies}
- {Known constraint 3 — e.g., business rules, limitations}

## Key Knowledge
- {Important finding from research — e.g., data schema, file structure}
- {Hypothesis or approach rationale}
- {Critical detail workers need to know}

## Tasks

### Phase 1 (parallel)
- [ ] **task-1**: {Task title}
  - {Detailed self-contained description. Include all context a worker needs.}
  - Reasoning level: {high|medium|low}

- [ ] **task-2**: {Task title}
  - {Detailed description — independent of task-1, can run simultaneously}
  - Reasoning level: {medium}

### Phase 2 (parallel, after Phase 1)
- [ ] **task-3**: {Task title}
  - {Detailed description}
  - Depends on: task-1
  - Reasoning level: {medium}

- [ ] **task-4**: {Task title}
  - {Detailed description — independent of task-3, can run simultaneously}
  - Depends on: task-2
  - Reasoning level: {low}

### Phase 3
- [ ] **task-5**: {Task title}
  - {Detailed description}
  - Depends on: task-3, task-4
  - Reasoning level: {low}

## Notes
{Workers append results, findings, and issues here as tasks complete}
` + "```" + `

## Guidelines
- Break the objective into 3-10 concrete, actionable tasks
- Each task description must be **self-contained** — a worker with no prior context will execute it
- Include specific details: file paths, function names, requirements, expected outcomes
- **Maximize parallelism**: Group independent tasks into phases. Tasks within a phase run simultaneously, so they MUST NOT depend on each other. The manager will delegate all tasks in a phase at once.
- Use "### Phase N (parallel)" headings to group tasks. Add "(after Phase M)" when a phase depends on a previous one.
- Add "Depends on: task-X, task-Y" on individual tasks to show which earlier tasks must complete first
- Identify tasks that are truly independent (e.g., frontend + backend, different modules, tests + docs) and put them in the same phase
- The **Constraints** section should capture environment details, technology stack, and known limitations
- The **Key Knowledge** section should include insights from your research (file paths, schemas, patterns found)
- Reasoning level recommendations:
  - **high**: Complex architecture, system design, tricky algorithms, planning
  - **medium**: Standard implementation, building features, writing code
  - **low**: Simple tasks like formatting, config changes, running tests, documentation
- The plan should be general-purpose — not limited to coding tasks
`)

	// Append capabilities section if available
	if caps != nil {
		sb.WriteString("\n## Available Capabilities\n")
		sb.WriteString("The following capabilities are available to workers executing tasks:\n\n")

		if caps.HasWorkspace {
			sb.WriteString("- **Workspace**: File read/write access via workspace tools\n")
		}
		if caps.HasBrowser {
			sb.WriteString("- **Browser**: Web browsing and automation capabilities\n")
		}

		if len(caps.EnabledServers) > 0 {
			sb.WriteString("\n### MCP Servers\n")
			sb.WriteString("Workers have access to these MCP servers (tool servers):\n")
			for _, server := range caps.EnabledServers {
				sb.WriteString(fmt.Sprintf("- `%s`\n", server))
			}
		}

		if len(caps.SelectedTools) > 0 {
			sb.WriteString("\n### Selected Tools\n")
			sb.WriteString("Specifically enabled tools (server:tool format):\n")
			for _, tool := range caps.SelectedTools {
				sb.WriteString(fmt.Sprintf("- `%s`\n", tool))
			}
		}

		if len(caps.Skills) > 0 {
			sb.WriteString("\n### Skills (HIGH PRIORITY)\n")
			sb.WriteString("The following skills are activated and contain detailed instructions, methodologies, and templates that workers MUST follow.\n")
			sb.WriteString("Skills are stored at the workspace root under skills/<name>/SKILL.md.\n\n")
			sb.WriteString("**Before writing the plan, you MUST read each skill file** using execute_shell_command(command: \"cat skills/<name>/SKILL.md\", working_directory: \".\") to understand the methodology, steps, and requirements.\n\n")
			sb.WriteString("Available skills:\n")
			for _, skill := range caps.Skills {
				sb.WriteString(fmt.Sprintf("- **%s** (`skills/%s/SKILL.md`): %s\n", skill.Name, skill.FolderName, skill.Description))
			}
			sb.WriteString("\n**IMPORTANT**: When writing task descriptions in the plan, reference the relevant skill by name and path. Tell workers to read and follow the skill instructions. Skills contain detailed step-by-step guidance that is critical for task quality.\n")
		}

		if len(caps.SubAgentTemplates) > 0 {
			sb.WriteString("\n### Sub-Agent Templates\n")
			sb.WriteString("Reusable sub-agent profiles are available. Use the `agent_template` parameter on `delegate` to load a template's specialized instructions and defaults.\n\n")
			sb.WriteString("Available templates:\n")
			for _, tmpl := range caps.SubAgentTemplates {
				line := fmt.Sprintf("- **%s** (`subagents/%s/`): %s", tmpl.Name, tmpl.FolderName, tmpl.Description)
				if tmpl.DefaultReasoningLevel != "" {
					line += fmt.Sprintf(" [reasoning: %s]", tmpl.DefaultReasoningLevel)
				}
				if tmpl.DefaultToolMode != "" {
					line += fmt.Sprintf(" [tool_mode: %s]", tmpl.DefaultToolMode)
				}
				sb.WriteString(line + "\n")
			}
			sb.WriteString("\nWhen a template matches a task, pass `agent_template: \"<folder_name>\"` in the delegate call. The template's instructions, default reasoning level, tool mode, skills, and MCP servers are automatically applied.\n")
		}

		sb.WriteString("\nConsider these capabilities when designing tasks — reference specific servers, tools, skills, or sub-agent templates where relevant.\n")
	}

	return sb.String()
}

// handleCreateDelegationPlan spawns a planner sub-agent that writes plan.md to workspace
func handleCreateDelegationPlan(ctx context.Context, args map[string]interface{}) (string, error) {
	planName, _ := args["plan_name"].(string)
	if planName == "" {
		return "", fmt.Errorf("plan_name is required")
	}
	objective, _ := args["objective"].(string)
	if objective == "" {
		return "", fmt.Errorf("objective is required")
	}
	additionalContext, _ := args["context"].(string)
	reasoningLevel, _ := args["reasoning_level"].(string)
	if reasoningLevel != "" && reasoningLevel != "high" && reasoningLevel != "medium" && reasoningLevel != "low" {
		reasoningLevel = "" // Ignore invalid values
	}

	// Check delegation depth
	currentDepth := 0
	if depth, ok := ctx.Value(DelegationDepthKey).(int); ok {
		currentDepth = depth
	}
	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached", MaxDelegationDepth)
	}

	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecuteDelegatedTaskKey).(ExecuteDelegatedTaskFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("delegation execution function not available")
	}

	// Sanitize plan name to create plan ID (kebab-case, no special chars)
	planID := sanitizePlanName(planName)
	planFolder := fmt.Sprintf("%s/%s", PlanFileFolderPath, planID)

	// Enforce one plan per conversation — if a plan was already created, reject with guidance
	// Check both PlanSessionStateKey (new) and PlanTrackerKey (backward compat)
	var tracker *PlanSessionState
	if ps, ok := ctx.Value(PlanSessionStateKey).(*PlanSessionState); ok && ps != nil {
		tracker = ps
	} else if pt, ok := ctx.Value(PlanTrackerKey).(*PlanTracker); ok && pt != nil {
		tracker = pt
	}
	if tracker != nil {
		if existingID, existingFolder, ok := tracker.TryCreate(planID, planFolder); !ok {
			return "", fmt.Errorf(
				"a plan already exists in this conversation (plan_id: %s, folder: %s). "+
					"Do not create a second plan. Read the existing plan.md and delegate tasks from it. "+
					"If the plan needs changes, delegate a sub-agent to update it",
				existingID, existingFolder,
			)
		}
	}

	// Create the plan folder eagerly via workspace API so the sidebar shows it immediately
	if wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client); ok && wsClient != nil {
		if err := wsClient.CreateFolder(ctx, planFolder); err != nil {
			log.Printf("[DELEGATION PLAN] Warning: could not pre-create plan folder %s: %v", planFolder, err)
			// Non-fatal — the planner will create it when writing plan.md
		} else {
			log.Printf("[DELEGATION PLAN] Pre-created plan folder: %s", planFolder)
		}
	}

	// Emit file event early so workspace sidebar can focus on the folder
	if emitter, ok := ctx.Value(PlanEventEmitterKey).(PlanEventEmitter); ok && emitter != nil {
		emitter.EmitFileEvent(planFolder)
	}

	// Extract capabilities context
	var caps *CapabilitiesContext
	if c, ok := ctx.Value(CapabilitiesContextKey).(*CapabilitiesContext); ok {
		caps = c
	}

	// Build the planner instruction
	plannerInstruction := buildPlannerPrompt(caps, planFolder)
	plannerInstruction += fmt.Sprintf("\n\n## Objective\n%s", objective)

	if additionalContext != "" {
		plannerInstruction += fmt.Sprintf("\n\n## Additional Context\n%s", additionalContext)
	}

	log.Printf("[DELEGATION PLAN] Spawning planner sub-agent for: %s (plan_id: %s)", truncateString(objective, 80), planID)

	// Spawn planner sub-agent with optional reasoning level from LLM
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
	if reasoningLevel != "" {
		subCtx = context.WithValue(subCtx, ReasoningLevelKey, reasoningLevel)
	}
	subCtx = context.WithValue(subCtx, PlanFolderKey, planFolder)

	startTime := time.Now()
	plannerResult, err := executeFunc(subCtx, plannerInstruction)
	planDuration := time.Since(startTime)

	if err != nil {
		return "", fmt.Errorf("planner sub-agent failed: %w", err)
	}

	log.Printf("[DELEGATION PLAN] Planner completed in %s for plan %s", planDuration, planID)

	// Emit file event for plan.md so the sidebar highlights it
	planFilePath := fmt.Sprintf("%s/plan.md", planFolder)
	if emitter, ok := ctx.Value(PlanEventEmitterKey).(PlanEventEmitter); ok && emitter != nil {
		emitter.EmitFileEvent(planFilePath)
		log.Printf("[DELEGATION PLAN] Emitted file event for plan: %s", planFilePath)
	}

	// Read plan.md content via workspace API (handles per-user path resolution correctly)
	// This avoids the mount namespace issue where shell cat can't find per-user files
	var planContent string
	if wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client); ok && wsClient != nil {
		readResult, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
			Filepath: planFilePath,
		})
		if err != nil {
			log.Printf("[DELEGATION PLAN] Warning: could not read plan.md content: %v", err)
		} else {
			// Extract just the content field from the JSON response
			var readData map[string]interface{}
			if json.Unmarshal([]byte(readResult), &readData) == nil {
				if content, ok := readData["content"].(string); ok {
					planContent = content
				}
			}
		}
	}

	// Return summary to the manager with plan content included
	result := map[string]interface{}{
		"plan_id":       planID,
		"plan_folder":   planFolder,
		"plan_file":     planFilePath,
		"objective":     objective,
		"planning_time": planDuration.String(),
		"result":        plannerResult,
		"message":       fmt.Sprintf("Plan created at %s. Use delegate to execute tasks. Update checkboxes in plan.md as tasks complete.", planFilePath),
	}
	if planContent != "" {
		result["plan_content"] = planContent
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// --- Helper functions ---

// sanitizePlanName sanitizes a plan name from LLM input into a safe folder name.
// Converts to lowercase kebab-case, strips special characters.
// No random suffix — the LLM-provided name is descriptive enough and TryCreate
// prevents duplicates within a session. This allows resuming chats to reuse
// existing plan folders.
func sanitizePlanName(name string) string {
	// Lowercase and split into words
	words := strings.Fields(strings.ToLower(name))
	var parts []string
	for _, w := range words {
		// Strip non-alphanumeric (keep hyphens between words)
		cleaned := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			return -1
		}, w)
		// Split on hyphens to get individual parts, then rejoin
		for _, part := range strings.Split(cleaned, "-") {
			if part != "" {
				parts = append(parts, part)
			}
		}
	}
	if len(parts) == 0 {
		parts = []string{"plan"}
	}
	// Cap at 5 parts to keep folder names reasonable
	if len(parts) > 5 {
		parts = parts[:5]
	}
	return strings.Join(parts, "-")
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GetDelegationInstructions returns system prompt instructions for delegation mode
func GetDelegationInstructions() string {
	return `
## Delegation — Sub-Agent Tools

You have access to sub-agent delegation tools. You are a fully capable agent — you can do any task yourself using your tools. Delegation is an **additional capability** that lets you run work in parallel or offload tasks to focused sub-agents.

### When to Delegate vs Do It Yourself

**Do it yourself** when:
- The task is simple or quick (answering questions, small edits, running a command)
- You need to explore or understand something before deciding what to do
- The user is asking for your opinion, analysis, or explanation
- It's a single focused task that doesn't benefit from parallelism

**Delegate** when:
- You can split work into independent tasks that run **in parallel** (this is the main benefit)
- The task is large and you want a sub-agent to focus on one piece while you handle another
- You want to offload a well-defined subtask so you can move on to other work

**Use ` + "`create_delegation_plan`" + `** when:
- The project is complex with many interdependent steps
- The user needs visibility into progress (plan.md appears in workspace Plans/ folder)
- You want a structured breakdown before executing

### Delegation Tools

**` + "`delegate`" + `** — Spawn a sub-agent for a task:
- Provide comprehensive, self-contained instructions (sub-agents have no shared memory)
- Call multiple times in one turn for **parallel execution** — this is the key advantage
- Optional ` + "`reasoning_level`" + `: "high" (architecture, complex logic), "medium" (standard implementation), "low" (formatting, tests, config)
- Optional ` + "`plan_folder`" + `: restricts worker writes to this folder (always pass when executing plan tasks)
- Optional ` + "`agent_template`" + `: sub-agent template folder name (e.g. "code-review"). Loads specialized instructions and defaults from subagents/<name>/SUBAGENT.md

**` + "`create_delegation_plan`" + `** — Spawn a planner sub-agent that writes plan.md:
- Planner researches the objective and creates a phased task breakdown
- Returns plan_id, plan_folder, and plan_content directly
- Then execute tasks phase by phase using ` + "`delegate`" + `

### Tool Mode (optional, for ` + "`delegate`" + `):

- **"simple"** (default): Worker gets all tools directly. Best for most tasks, including **writing Python/Bash scripts** — use workspace shell tools (` + "`execute_shell_command`" + `) to create and run scripts.
- **"code_execution"**: Worker writes Go code to call tools programmatically. Best for **data analysis with MCP tools** — e.g., fetching data from MCP servers, transforming responses, batch operations, loops over tool results. NOT recommended for simple script writing.
- **"tool_search"**: Worker discovers tools on-demand via search. Best when many MCP servers are available (20+).

**Guideline**: For writing Python scripts, shell scripts, or any file-based work, always prefer **"simple"** mode with workspace shell tools. Use **"code_execution"** only when you need to programmatically orchestrate multiple MCP tool calls and analyze their responses.

### Executing a Plan

` + "```" + `
1. create_delegation_plan(plan_name: "user-auth", objective: "Build user auth system", context: "...")
   → Returns plan_id, plan_folder, plan_content
2. Execute phase by phase:
   - Phase 1: delegate ALL tasks simultaneously (multiple calls in one turn)
   - Re-read plan.md to collect worker learnings (Key Knowledge, Notes)
   - Phase 2: delegate all tasks, relaying learnings from Phase 1
   - Continue until done
3. Summarize results to user
` + "```" + `

### Plan Management Rules
- **One plan per conversation**: Never create a second plan. Update the existing plan.md instead.
- **Re-read plan.md after each phase**: Workers write Key Knowledge and Notes into plan.md. Collect their discoveries before the next phase.
- **Relay learnings**: Include relevant Key Knowledge in the next delegate instruction. Workers start fresh with no shared memory.
- **Pass ` + "`plan_folder`" + `**: Always pass it when executing plan tasks to restrict worker writes.
- **Self-contained instructions**: Each delegate call must include ALL context the worker needs.
- **Verify results**: You are the quality gate — check results before reporting to the user.

### Limitations
- Sub-agents cannot delegate further (max depth enforced).
- Sub-agents start fresh — no shared context or conversation history.
`
}

// handleConfirmPlanExecution handles the confirm_plan_execution tool — blocks until user approves or rejects
func handleConfirmPlanExecution(ctx context.Context, args map[string]interface{}) (string, error) {
	// Try PlanSessionStateKey first, fall back to PlanTrackerKey
	var planState *PlanSessionState
	if ps, ok := ctx.Value(PlanSessionStateKey).(*PlanSessionState); ok && ps != nil {
		planState = ps
	} else if pt, ok := ctx.Value(PlanTrackerKey).(*PlanTracker); ok && pt != nil {
		planState = pt
	}
	if planState == nil {
		return "", fmt.Errorf("no plan session state available")
	}

	planState.mu.Lock()
	if planState.PlanID == "" {
		planState.mu.Unlock()
		return "", fmt.Errorf("no plan has been created yet. Use create_delegation_plan first")
	}
	currentPlanID := planState.PlanID
	currentPlanFolder := planState.PlanFolder
	planState.mu.Unlock()

	planSummary, _ := args["plan_summary"].(string)
	if planSummary == "" {
		planSummary = "Plan is ready for review."
	}

	// Use HumanFeedbackStore to block until user responds
	uniqueID := fmt.Sprintf("plan-approval-%s", currentPlanID)
	feedbackStore := GetHumanFeedbackStore()

	message := fmt.Sprintf("**%s** — Plan is ready. Approve to start execution, or type feedback in the chat.", currentPlanFolder)

	// Read plan.md content to show in the approval UI
	planContent := ""
	if wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client); ok && wsClient != nil {
		planFilePath := currentPlanFolder + "/plan.md"
		if resultJSON, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{Filepath: planFilePath}); err == nil {
			// Parse the JSON response to extract raw content
			var result map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(resultJSON), &result); jsonErr == nil {
				if content, ok := result["content"].(string); ok {
					planContent = content
				}
			}
		} else {
			log.Printf("[PLAN APPROVAL] Could not read plan.md from %s: %v", planFilePath, err)
		}
	}

	// Emit workspace_file_operation to highlight plan.md in the workspace sidebar
	if planEmitter, ok := ctx.Value(PlanEventEmitterKey).(PlanEventEmitter); ok && planEmitter != nil {
		planEmitter.EmitFileEvent(currentPlanFolder + "/plan.md")
	}

	// Emit blocking_human_feedback event — approve-only button, plan content as context
	if emitter, ok := ctx.Value(SessionEventEmitterKey).(SessionEventEmitter); ok && emitter != nil {
		emitter.EmitBlockingHumanFeedback(uniqueID, message, planContent, true, "Approve & Execute", "")
	}

	// Create blocking request — yes/no mode but with empty noLabel so frontend hides reject button
	if err := feedbackStore.CreateRequestWithSlack(ctx, uniqueID, message, "", &services.ButtonOptions{
		YesNoOnly: true,
		YesLabel:  "Approve & Execute",
		NoLabel:   "",
	}); err != nil {
		return "", fmt.Errorf("failed to create approval request: %w", err)
	}

	// Block until user responds (10 minute timeout for plan review)
	response, err := feedbackStore.WaitForResponse(uniqueID, 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("approval timed out or failed: %w", err)
	}

	// Check if approved
	isApproved := response == "yes" || response == "Approve & Execute" ||
		strings.EqualFold(response, "yes") || strings.EqualFold(response, "approve")

	if isApproved {
		planState.SetPhase("execution")
		return fmt.Sprintf(`APPROVED — You are now in Execution Mode.
Plan: %s (folder: %s)

INSTRUCTIONS (follow these exactly):
1. Read plan.md from %s/plan.md
2. Execute one phase at a time — call delegate for ALL tasks in a phase simultaneously (multiple delegate calls in one turn)
3. After each phase, re-read plan.md to collect worker learnings
4. Relay learnings to next phase workers
5. When ALL phases are done, summarize results to the user

RULES:
- NEVER call confirm_plan_execution again — the plan is already approved
- NEVER show the plan to the user again — they already approved it
- NEVER do work yourself — always delegate
- Pass plan_folder to every delegate call
- Each delegate call must have self-contained instructions (workers have no shared memory)`, currentPlanID, currentPlanFolder, currentPlanFolder), nil
	}

	// User rejected — return their feedback
	return fmt.Sprintf("User requested changes: %s\nPlease address the feedback and update the plan, then call confirm_plan_execution again.", response), nil
}

// GetPlanningModeInstructions returns system prompt for the planning phase of multi-agent mode
func GetPlanningModeInstructions() string {
	return `
## Multi-Agent Planning Mode

You are an **Orchestrator**. You plan and coordinate — you NEVER execute work yourself.

Your role: Research → Plan → Get Approval → Delegate execution to sub-agents.

**CRITICAL: You must NEVER directly create, edit, or write files. You must NEVER run shell commands to perform work (build, install, test, etc.). ALL work must be delegated to sub-agents via ` + "`delegate`" + `. You may only use workspace tools (read files, list files) and shell commands for RESEARCH purposes — reading, listing, and understanding the codebase.**

### Your Workflow

**Step 1: Understand & Clarify**
- Read the user's request carefully
- Use ` + "`human_feedback`" + ` to ask clarifying questions BEFORE doing anything else:
  - What is the expected outcome? Any preferences on approach?
  - Are there constraints, priorities, or specific requirements?
  - Which parts are most important or time-sensitive?
- Do NOT skip this step — a good plan requires clear requirements from the human

**Step 2: Research & Analyze**
- Use ` + "`execute_shell_command`" + ` for research: ` + "`cat`" + `, ` + "`ls`" + `, ` + "`grep`" + `, ` + "`find`" + ` to read files and explore the workspace
- Gather context needed for planning
- Use ` + "`human_feedback`" + ` again if you discover ambiguities during research

**Step 3: Create Plan**
- Use ` + "`create_delegation_plan`" + ` with a clear objective and all context you gathered
- The planner sub-agent will research further and write a structured plan.md

**Step 4: Get Approval**
- Call ` + "`confirm_plan_execution`" + ` with a plan summary (key phases, tasks, reasoning levels)
- This BLOCKS until the user clicks "Approve & Execute" or "Request Changes"
- If approved → phase switches to Execution Mode automatically
- If rejected → read the feedback, adjust the plan, and call ` + "`confirm_plan_execution`" + ` again
- Do NOT call ` + "`delegate`" + ` until plan is approved

### Resuming an Existing Plan
If the user references an existing plan file (e.g., via @ mention of a Plans/ folder or plan.md file):
1. **Do NOT create a new plan** — read the existing plan.md using ` + "`execute_shell_command`" + `
2. Review the plan content and task statuses (checked = done, unchecked = pending)
3. Call ` + "`confirm_plan_execution`" + ` to get approval to resume execution
4. Once approved, continue executing from where the plan left off — only delegate unchecked tasks
5. The plan_folder is the folder containing the plan.md (e.g., "Plans/my-plan")

### When NOT to Plan
- Simple questions, explanations, or opinions — answer directly (no plan needed)

### Available Tools
- ` + "`create_delegation_plan`" + ` — Create a structured plan with phased task breakdown
- ` + "`delegate`" + ` — Spawn a sub-agent (available but use only after plan approval)
- ` + "`human_feedback`" + ` — Ask the user questions during research/planning
- ` + "`confirm_plan_execution`" + ` — Present plan for user approval, switches to Execution Mode
- ` + "`execute_shell_command`" + ` — For research only (` + "`cat`" + `, ` + "`ls`" + `, ` + "`grep`" + `, ` + "`find`" + `). NEVER use for executing work.

### Plan Rules
- Plan is saved to Plans/{plan_id}/plan.md
- **Always ask the human** before creating the plan — use ` + "`human_feedback`" + ` to confirm requirements
- NEVER execute plan tasks yourself — always delegate
- After execution completes and you report results, you can create a new plan for follow-up requests

### IMPORTANT: After Plan Approval
- Once ` + "`confirm_plan_execution`" + ` returns "APPROVED", you are in Execution Mode
- **DO NOT** call ` + "`confirm_plan_execution`" + ` again — the plan is already approved
- **DO NOT** show the plan to the user again — they already approved it
- Immediately start delegating tasks phase by phase using ` + "`delegate`" + `
`
}

// GetExecutionModeInstructions returns system prompt for the execution phase of multi-agent mode
func GetExecutionModeInstructions() string {
	return `
## Multi-Agent Execution Mode

You are an **Orchestrator**. The plan has been approved. Delegate all work to sub-agents — you NEVER execute work yourself.

**CRITICAL: You must NEVER directly create, edit, or write files. You must NEVER run shell commands to perform work (build, install, test, etc.). ALL work must be delegated to sub-agents via ` + "`delegate`" + `. You may only use workspace tools and shell commands to READ plan.md and verify results.**

### Your Workflow
1. Read plan.md from the Plans/ folder to get the full task breakdown
2. Execute one phase at a time — call ` + "`delegate`" + ` for ALL tasks in a phase simultaneously
3. After each phase completes, re-read plan.md to collect worker learnings (Key Knowledge, Notes)
4. Relay relevant learnings to the next phase's workers via their delegate instructions
5. Summarize results to the user when all phases are done

### Available Tools
- ` + "`delegate`" + ` — Spawn a sub-agent for a task (this is your PRIMARY tool)
  - ` + "`reasoning_level`" + `: "high" (architecture, complex), "medium" (standard), "low" (formatting, config)
  - ` + "`plan_folder`" + `: always pass to restrict worker writes
  - ` + "`tool_mode`" + `: "simple" (default) for most tasks including writing scripts, coding, file work. "code_execution" ONLY for batch MCP tool operations (loops over API calls). "tool_search" ONLY when 30+ MCP tools available
  - ` + "`agent_template`" + `: sub-agent template folder name — loads specialized instructions and defaults from subagents/<name>/SUBAGENT.md
- ` + "`human_feedback`" + ` — Ask the user questions if you are unsure about something
- ` + "`execute_shell_command`" + ` — Read plan.md (` + "`cat Plans/{plan_id}/plan.md`" + `) to check progress and collect learnings
- ` + "`create_delegation_plan`" + ` — Available but typically not needed (plan already exists)

### Execution Rules
- **NEVER do work yourself** — always delegate via ` + "`delegate`" + `
- Execute one phase at a time — all tasks within a phase run in parallel
- Call ` + "`delegate`" + ` multiple times in one turn for parallel execution
- Pass ` + "`plan_folder`" + ` to every delegate call
- Self-contained instructions: each worker starts fresh with no shared memory
- Re-read plan.md after each phase to collect discoveries
- You are the quality gate — review worker output and verify results before reporting to user

### Follow-Up Tasks
- After all phases are done and you've reported results, the user may ask follow-up questions
- For follow-ups that need new work: call ` + "`create_delegation_plan`" + ` to create a new plan — this automatically resets back to Planning Mode
- For simple follow-up questions: answer directly without a plan

### Limitations
- Sub-agents cannot delegate further (max depth enforced)
- Sub-agents start fresh — no shared context or conversation history
`
}

