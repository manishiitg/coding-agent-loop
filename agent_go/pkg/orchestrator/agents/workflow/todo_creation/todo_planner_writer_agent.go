package todo_creation

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

	"github.com/tmc/langchaingo/llms"
)

// TodoPlannerWriterTemplate holds template variables for todo writing prompts
type TodoPlannerWriterTemplate struct {
	Objective        string
	PlanResult       string
	ExecutionResult  string
	ValidationResult string
	CritiqueResult   string
	WorkspacePath    string
	TotalIterations  string
}

// TodoPlannerWriterAgent creates optimal todo list based on execution experience
type TodoPlannerWriterAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoPlannerWriterAgent creates a new todo planner writer agent
func NewTodoPlannerWriterAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoPlannerWriterAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerWriterAgentType,
		eventBridge,
	)

	return &TodoPlannerWriterAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (tpwa *TodoPlannerWriterAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract variables from template variables
	// Writer is strategic - synthesizes from all iterations
	objective := templateVars["Objective"]
	planResult := templateVars["PlanResult"]
	executionResult := templateVars["ExecutionResult"]
	validationResult := templateVars["ValidationResult"]
	critiqueResult := templateVars["CritiqueResult"]
	workspacePath := templateVars["WorkspacePath"]
	totalIterations := templateVars["TotalIterations"]

	// Prepare template variables
	writerTemplateVars := map[string]string{
		"Objective":        objective,
		"PlanResult":       planResult,
		"ExecutionResult":  executionResult,
		"ValidationResult": validationResult,
		"CritiqueResult":   critiqueResult,
		"WorkspacePath":    workspacePath,
		"TotalIterations":  totalIterations,
	}

	// Execute using input processor
	return tpwa.ExecuteWithInputProcessor(ctx, writerTemplateVars, tpwa.writerInputProcessor, conversationHistory)
}

// writerInputProcessor processes inputs specifically for todo list creation
func (tpwa *TodoPlannerWriterAgent) writerInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoPlannerWriterTemplate{
		Objective:        templateVars["Objective"],
		PlanResult:       templateVars["PlanResult"],
		ExecutionResult:  templateVars["ExecutionResult"],
		ValidationResult: templateVars["ValidationResult"],
		CritiqueResult:   templateVars["CritiqueResult"],
		WorkspacePath:    templateVars["WorkspacePath"],
		TotalIterations:  templateVars["TotalIterations"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - SYNTHESIZE FINAL TODO LIST FROM ALL ITERATIONS

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}
**TOTAL ITERATIONS COMPLETED**: {{.TotalIterations}}

**CORE TASK**: Read ALL {{.TotalIterations}} iterations and create FINAL, production-ready todo list using proven methods.

## 🎯 Understanding Your Role

**IMPORTANT - Final Synthesis**:
- 🔄 **{{.TotalIterations}} Iterations Completed** = Long discovery process testing different methods
- 📝 **Planning Files** = Read {{.WorkspacePath}}/todo_creation/planning/plan.md (all {{.TotalIterations}} iterations)
- 🧪 **Execution Files** = Read {{.WorkspacePath}}/todo_creation/execution/execution_results.md (all {{.TotalIterations}} iterations)
- ✅ **Validation Files** = Read {{.WorkspacePath}}/todo_creation/validation/ (all validations)
- 📄 **YOUR OUTPUT** = Synthesize best methods from ALL {{.TotalIterations}} iterations

**Synthesis Process**:
- Read ALL "## Iteration X" sections from plan.md
- Read ALL "## Iteration X" sections from execution_results.md
- Compare what was tried across iterations
- Identify BEST methods that emerged (highest success rate)
- Create production todo using ONLY proven methods

## 📋 Todo Creation Guidelines
- **Read ALL Iterations**: Don't just use latest - analyze entire history
- **Compare Approaches**: Iteration 1 tried API (80% success), Iteration 3 tried scraping (95% success)
- **Select Best**: Choose methods with highest success + reliability
- **Synthesize**: Combine best discoveries from multiple iterations
- **Make Reproducible**: Use exact MCP tools/arguments that worked
- **Evidence-Based**: Only include methods that were validated
- **Save Todo List**: Write to {{.WorkspacePath}}/todo_creation/todo.md

**⚠️ IMPORTANT**: Only create/modify files within {{.WorkspacePath}}/todo_creation/ folder structure.

` + memory.GetWorkflowMemoryRequirements() + `

## 📁 Read ALL Iteration History First

**CRITICAL**: You MUST read ALL "## Iteration X" sections from these files:

### Planning History (All Approaches Tried)
- {{.WorkspacePath}}/todo_creation/planning/plan.md
  - Read EVERY "## Iteration X" section (not just latest!)
  - See ALL approaches that were planned
  - Understand evolution of planning strategy

### Execution History (What Worked/Failed)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md
  - Read EVERY "## Iteration X" section (not just latest!)
  - Compare success rates across iterations
  - Identify which approaches/tools worked best
  - Learn from failures and rejected methods
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md
  - Track all completed work
  - Extract proven MCP tools and arguments

### Validation History (What Was Verified)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md
  - Read EVERY "## Iteration X" validation
  - See evidence quality across iterations
  - Only use validated methods in final todo

### Evidence Files
- {{.WorkspacePath}}/todo_creation/execution/evidence/
  - Review critical evidence from all iterations

## 🔍 Extract Best Methods from ALL Iterations
Parse ALL iterations to extract proven methods:
- **MCP Servers**: Which servers worked best (aws, gitlab, github, filesystem)
- **Tool Names**: Exact tools with highest success rates (aws_cli_query, gitlab_get_project)
- **Arguments**: Exact parameters that were successful
- **Patterns**: Approaches that worked consistently across iterations
- **Success Rates**: Compare approaches across iterations (Iteration 1: 80%, Iteration 3: 95%)

**Comparison Process**:
1. List all approaches tried across iterations
2. Calculate success rate for each approach
3. Identify highest performing methods
4. Select BEST methods for final todo list

## 📤 Output Format

# Production Todo List - Synthesized from All Iterations

## Project Overview
- **Objective**: {{.Objective}}
- **Created**: [Current date]
- **Total Iterations**: [Number of iterations run]
- **Best Methods**: [Synthesized from iteration history]
- **Total Steps in Todo**: [Final count]

## 📊 Iteration Analysis Summary

### Methods Tried Across Iterations
[Read from plan.md and execution_results.md]

**Iteration 1**: [Approach tried, success rate]
- Method: [Description]
- Result: [Success/Failure]
- Key Learning: [What we discovered]

**Iteration 2**: [Approach tried, success rate]
- Method: [Description]
- Result: [Success/Failure]
- Key Learning: [What we discovered]

[Continue for all iterations...]

### 🏆 Best Methods Discovered
[Compare ALL iterations, identify winners]

**Method 1**: [Highest success rate method]
- **Source**: Iteration [X]
- **Success Rate**: [%]
- **MCP Tools**: [Proven tools/arguments]
- **Why Best**: [Evidence]

**Method 2**: [Second best method]
- **Source**: Iteration [X]
- **Success Rate**: [%]
- **MCP Tools**: [Proven tools/arguments]
- **Why Best**: [Evidence]

### Methods Rejected
[What was tried but didn't work well]
- **Method**: [Name]
- **Iteration**: [X]
- **Failure Rate**: [%]
- **Why Rejected**: [Evidence]

## Prerequisites
[What needs to be prepared before starting]

## Execution Plan
### Phase 1: [Phase Name]

#### Step 1.1: [Step Name]
- **Description**: [What needs to be done]
- **MCP Server**: [Server to use - e.g., aws, gitlab, github]
- **MCP Tool**: [Tool name - e.g., aws_cli_query]
- **Tool Arguments**: [Exact arguments needed]
- **Commands**: [Exact commands to run]
- **Success Criteria**: [Measurable outcome]
- **Verification**: [How to verify completion]
- **Expected Output**: [What will be produced]
- **Estimated Effort**: [Time estimate]
- **Dependencies**: [Prerequisites]

#### Step 1.2: [Step Name]
- **Description**: [What needs to be done]
- **MCP Server**: [Server to use]
- **MCP Tool**: [Tool name]
- **Tool Arguments**: [Arguments needed]
- **Commands**: [Commands to run]
- **Success Criteria**: [Measurable outcome]
- **Verification**: [Verification method]
- **Expected Output**: [Output produced]
- **Estimated Effort**: [Time estimate]
- **Dependencies**: [Prerequisites]

[Continue for all steps...]

### 📝 Task-Specific Fields (Add when relevant)

**Core Fields** (always include): Description, MCP Server, MCP Tool, Tool Arguments, Commands, Success Criteria, Verification, Expected Output, Effort, Dependencies

**Additional Fields** (add only if relevant to task type):
- **Web scraping**: URL, Selectors, Data Extraction
- **File operations**: File Paths, Formats, Permissions
- **API calls**: Endpoints, Authentication, Rate Limits
- **Data processing**: Input/Output Format, Validation Rules
- **Database**: Connection Details, Queries, Backup Strategy

**Example - API Integration:**
- **Endpoints**: https://api.example.com/users
- **Authentication**: Bearer token in Authorization header
- **Rate Limits**: 100 requests/minute
- **Error Handling**: Retry with exponential backoff

## 📊 Todo Summary
- **Total Steps**: [Number of steps in todo list]
- **Estimated Duration**: [Overall time estimate]
- **Key MCP Servers**: [List of servers needed]
- **Critical Dependencies**: [Major blockers]

## ✅ Verification Checklist
- [ ] Each step has measurable success criteria
- [ ] Each step has clear verification method
- [ ] Commands are exact and reproducible
- [ ] Dependencies are clearly defined
- [ ] MCP tools and arguments are documented

## ⚠️ Risk Management
[High-risk areas and mitigation strategies]

Focus on creating a COMPLETE, reproducible todo list with proven MCP methods from execution experience.`

	// Parse and execute the template
	tmpl, err := template.New("writer").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing writer template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing writer template: %v", err)
	}

	return result.String()
}
