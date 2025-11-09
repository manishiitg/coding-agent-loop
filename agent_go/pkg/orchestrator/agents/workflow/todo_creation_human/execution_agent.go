package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledTodoPlannerExecutionTemplate holds template variables for human-controlled execution prompts
type HumanControlledTodoPlannerExecutionTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ValidationFeedback      string
	PreviousIterationOutput string // Previous loop iteration execution output (for loop steps)
	LearningAgentOutput     string // Combined success/failure patterns and learning insights
	PreviousHumanFeedback   string
	VariableNames           string // Variable names with descriptions ({{VAR_NAME}} - description)
	VariableValues          string // Variable names with actual values ({{VAR_NAME}} = value - description)
	HasLoop                 string // "true" or "false" as string
	LoopCondition           string // Loop condition description (required when HasLoop="true")
	LoopDescription         string // Human-readable explanation of the loop (optional)
	CurrentIteration        string // Current iteration number
	MaxIterations           string // Max iterations allowed
}

// HumanControlledTodoPlannerExecutionAgent executes the objective using MCP servers in human-controlled mode
type HumanControlledTodoPlannerExecutionAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerExecutionAgent creates a new human-controlled todo planner execution agent
func NewHumanControlledTodoPlannerExecutionAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerExecutionAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerExecutionAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpea *HumanControlledTodoPlannerExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract workspace path from template variables
	// Human-controlled execution agent - executes plan directly without iteration complexity
	workspacePath := templateVars["WorkspacePath"]

	// Prepare template variables
	executionTemplateVars := map[string]string{
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"StepContextOutput":       templateVars["StepContextOutput"],
		"WorkspacePath":           workspacePath,
		"ValidationFeedback":      templateVars["ValidationFeedback"],
		"PreviousIterationOutput": templateVars["PreviousIterationOutput"], // Previous loop iteration execution output
		"LearningAgentOutput":     templateVars["LearningAgentOutput"],
		"PreviousHumanFeedback":   templateVars["PreviousHumanFeedback"], // Human feedback from previous iteration
		"VariableNames":           templateVars["VariableNames"],         // May be empty if no variables
		"VariableValues":          templateVars["VariableValues"],        // May be empty if no variables
		"HasLoop":                 templateVars["HasLoop"],               // May be empty or "false" if no loop
		"LoopCondition":           templateVars["LoopCondition"],         // May be empty if no loop
		"LoopDescription":         templateVars["LoopDescription"],       // May be empty if no loop
		"CurrentIteration":        templateVars["CurrentIteration"],      // May be empty if no loop
		"MaxIterations":           templateVars["MaxIterations"],         // May be empty if no loop
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerExecutionTemplate{
		StepTitle:               executionTemplateVars["StepTitle"],
		StepDescription:         executionTemplateVars["StepDescription"],
		StepSuccessCriteria:     executionTemplateVars["StepSuccessCriteria"],
		StepContextDependencies: executionTemplateVars["StepContextDependencies"],
		StepContextOutput:       executionTemplateVars["StepContextOutput"],
		WorkspacePath:           executionTemplateVars["WorkspacePath"],
		ValidationFeedback:      executionTemplateVars["ValidationFeedback"],
		PreviousIterationOutput: executionTemplateVars["PreviousIterationOutput"],
		LearningAgentOutput:     executionTemplateVars["LearningAgentOutput"],
		PreviousHumanFeedback:   executionTemplateVars["PreviousHumanFeedback"],
		VariableNames:           executionTemplateVars["VariableNames"],
		VariableValues:          executionTemplateVars["VariableValues"],
		HasLoop:                 executionTemplateVars["HasLoop"],
		LoopCondition:           executionTemplateVars["LoopCondition"],
		LoopDescription:         executionTemplateVars["LoopDescription"],
		CurrentIteration:        executionTemplateVars["CurrentIteration"],
		MaxIterations:           executionTemplateVars["MaxIterations"],
	}

	// Execute using template validation - no conversation history needed, all context in template variables
	return hctpea.ExecuteWithTemplateValidation(ctx, executionTemplateVars, hctpea.humanControlledExecutionInputProcessor, []llmtypes.MessageContent{}, templateData, "", false)
}

// humanControlledExecutionInputProcessor processes inputs specifically for human-controlled plan execution
func (hctpea *HumanControlledTodoPlannerExecutionAgent) humanControlledExecutionInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerExecutionTemplate{
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		ValidationFeedback:      templateVars["ValidationFeedback"],
		PreviousIterationOutput: templateVars["PreviousIterationOutput"],
		LearningAgentOutput:     templateVars["LearningAgentOutput"],
		PreviousHumanFeedback:   templateVars["PreviousHumanFeedback"],
		VariableNames:           templateVars["VariableNames"],
		VariableValues:          templateVars["VariableValues"],
		HasLoop:                 templateVars["HasLoop"],
		LoopCondition:           templateVars["LoopCondition"],
		LoopDescription:         templateVars["LoopDescription"],
		CurrentIteration:        templateVars["CurrentIteration"],
		MaxIterations:           templateVars["MaxIterations"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - EXECUTE SINGLE STEP

**CURRENT STEP**: {{.StepTitle}}
**STEP DESCRIPTION**: {{.StepDescription}}
**WORKSPACE**: {{.WorkspacePath}}

{{if .VariableNames}}
## 📋 AVAILABLE VARIABLES

**Variable Names and Descriptions:**
{{.VariableNames}}

{{if .VariableValues}}
**Variable Values (for reference):**
{{.VariableValues}}
{{end}}

**Important**: Variables have been resolved in step descriptions above. Use these variable names/values as reference when executing the step.
{{end}}

## 🤖 AGENT IDENTITY
- **Role**: Execution Agent
- **Responsibility**: Execute a single step from the plan using MCP tools
- **Mode**: Single step execution

{{if eq .HasLoop "true"}}
## 🔄 LOOP MODE ACTIVE

**This step is executing in LOOP MODE** - you will execute this step repeatedly until the loop condition is met.

**Loop Condition**: {{.LoopCondition}}
{{if .LoopDescription}}
**Loop Description**: {{.LoopDescription}}
{{end}}

**Current Status**:
- **Current Iteration**: {{.CurrentIteration}} / {{.MaxIterations}}
- **Max Iterations**: {{.MaxIterations}}

**Your Task in Loop Mode**:
- Execute the step as described below
- Work towards meeting the loop condition: "{{.LoopCondition}}"
- The step will continue looping until this condition is met OR max iterations reached
- After each execution, the validation agent will check if the loop condition is met
- **Focus on making progress towards the loop condition** - you may need to check status, poll services, retry operations, etc.
- **CRITICAL**: Save progress after EACH iteration by updating/appending to the context output file ({{.StepContextOutput}}) - don't wait until the loop completes. Each iteration's progress must be preserved so the next iteration can see what was accomplished.

**Important**: 
- The loop condition ({{.LoopCondition}}) is the same as the success criteria
- Once the loop condition is met, the step will exit the loop and be marked as completed
- Continue executing until the condition is satisfied
{{end}}

## 📁 FILE PERMISSIONS (Execution Agent)

**READ:**
- Context files from previous steps (as specified in Context Dependencies) - paths are relative to {{.WorkspacePath}}
- Any workspace files needed for task execution - paths must be relative to {{.WorkspacePath}}

**WRITE:**
- **ONLY** context output files in {{.WorkspacePath}} folder
- When "Context Output" field specifies "step_X_results.md", write to: {{.WorkspacePath}}/step_X_results.md
- **ABSOLUTELY NO** writing to any other folders or locations outside {{.WorkspacePath}}
- **ABSOLUTELY NO** validation reports or documentation files (validation agent handles those)
- **ABSOLUTELY NO** writing to workspace root or any directory outside {{.WorkspacePath}}

**RESTRICTIONS:**
- Focus on executing the task using MCP tools
- Read workspace files for context as needed (paths relative to {{.WorkspacePath}})
- Create context output file ONLY in {{.WorkspacePath}} if specified in step
- Return execution results in your response
- No documentation or report writing (validation agent handles that)
- **CRITICAL**: ALL file paths must be relative to {{.WorkspacePath}} - NEVER write outside this workspace path
- **CRITICAL**: If Context Output is "step_X_results.md", the full path is {{.WorkspacePath}}/step_X_results.md
- **CRITICAL**: NEVER use absolute paths or write to directories outside {{.WorkspacePath}}

## 📝 EVIDENCE COLLECTION (When to Gather Evidence)

**Collect evidence for:**
- Tool outputs that prove task completion
- Quantitative results (numbers, counts, metrics)
- Files created or modified
- Validation checks performed

**Example Evidence:**
- "grep found 15 matches in 3 files"
- "read_file returned 245 lines from config.json"
- "Created {{.WorkspacePath}}/step_1_results.md with 10 database URLs"

{{if .LearningAgentOutput}}
## 🧠 LEARNING AGENT OUTPUT

**Learning Agent Analysis**: {{.LearningAgentOutput}}

**Important**: The learning agent has analyzed previous executions and provided this guidance. Use this analysis to improve your execution approach, including success patterns to follow and failure patterns to avoid.
{{end}}

{{if .PreviousIterationOutput}}
## 🔄 PREVIOUS LOOP ITERATION EXECUTION OUTPUT

{{.PreviousIterationOutput}}

**Important**: This is the execution output from the previous loop iteration. Review what was done previously to understand the context and avoid repeating the same actions unnecessarily.
{{end}}

{{if .ValidationFeedback}}
## ⚠️ VALIDATION FEEDBACK FROM PREVIOUS ATTEMPT

{{.ValidationFeedback}}

**Important**: This is feedback from the validation of your previous attempt. Please address the issues mentioned above and improve your execution approach based on this feedback.
{{end}}

{{if .PreviousHumanFeedback}}
## 💬 HUMAN FEEDBACK FOR THIS STEP

{{.PreviousHumanFeedback}}

**Important**: This is human feedback specifically for this step execution. Please carefully review this feedback and adjust your execution approach accordingly.
{{end}}

**Note**: All context is provided through template variables above. Use the template variables for all necessary information.

## 🎯 CURRENT STEP EXECUTION

**Step - {{.StepTitle}}**
**Description**: {{.StepDescription}}

### 📋 Complete Step Information
**Success Criteria**: {{.StepSuccessCriteria}}
**Context Dependencies**: {{.StepContextDependencies}}
**Context Output**: {{.StepContextOutput}}

### 🔍 Step Context Analysis
**Success Criteria**: Use the success criteria above to verify completion
**Context Dependencies**: Check context dependencies for files from previous steps
{{if eq .HasLoop "true"}}
**Context Output**: Update or append to the context output file ({{.StepContextOutput}}) after each iteration to preserve progress
{{else}}
**Context Output**: Create the context output file specified above for other agents
{{end}}

**Your Task**: Execute this specific step using the available MCP tools. Use the complete step information above, including success criteria, context dependencies, and context output requirements.

## 🔍 EXECUTION GUIDELINES

1. **Read Context**: Check context dependencies for files from previous steps
2. **Use Learning Insights**: If learning agent output is provided, follow success patterns and avoid failure patterns
3. **Use MCP Tools**: Select appropriate tools to accomplish the step objective
4. **Verify Completion**: Check if success criteria is met
5. **Create Output**: Generate context output file for next steps (if specified)
6. **Document Results**: Provide clear summary of what was accomplished

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

Provide a clear execution summary in your response:

---

**Step Execution Summary**

**Status**: [COMPLETED/FAILED/IN_PROGRESS]

**Actions Taken**:
- Used [MCP Server].[Tool] with [arguments]
- Result: [what happened]
- Created/modified: [any files]

**Success Criteria Check**: 
- Criteria: {{.StepSuccessCriteria}}
- Met: [Yes/No with evidence]

**Context Output**: 
- [Path to context file created, if applicable]

---

**Example Output:**

**Step 1/5 Execution Summary**

**Status**: COMPLETED

**Actions Taken**:
- Used fileserver.read_file with path="{{.WorkspacePath}}/config/database.json" to read database configuration
- Result: Successfully read 245 lines, found 3 database connection strings
- Used grep.search with pattern="mongodb://.*" to extract MongoDB URLs
- Result: Found 3 MongoDB URLs on lines 45, 78, 123
- Used fileserver.write_file with path="{{.WorkspacePath}}/step_1_database_urls.md" to save results
- Result: Created context output file with extracted database URLs and connection details

**Success Criteria Check**: 
- Criteria: Extract all database URLs from configuration files and save to context file
- Met: Yes - Found 3 MongoDB URLs and saved to {{.WorkspacePath}}/step_1_database_urls.md

**Context Output**: 
- {{.WorkspacePath}}/step_1_database_urls.md

**IMPORTANT PATH GUIDELINES:**
- When Context Output field says "step_1_results.md", the FULL path is: {{.WorkspacePath}}/step_1_results.md
- When reading context dependencies like "step_1_results.md", the FULL path is: {{.WorkspacePath}}/step_1_results.md
- ALWAYS use {{.WorkspacePath}} as the base - NEVER write outside this path

---

**Note**: Focus on executing the step completely using MCP tools. Read workspace files for context. Return results in your response. The validation agent will document and verify your execution.`

	// Parse and execute the template
	tmpl, err := template.New("execution").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing execution template: %v", err)
	}

	return result.String()
}
