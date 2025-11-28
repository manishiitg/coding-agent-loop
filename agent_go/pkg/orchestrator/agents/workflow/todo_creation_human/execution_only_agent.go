package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"llm-providers/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/mcpagent/prompt"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// HumanControlledTodoPlannerExecutionOnlyTemplate holds template variables for execution-only agent prompts
type HumanControlledTodoPlannerExecutionOnlyTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	IsCodeExecutionMode     string // "true" or "false" - indicates if code execution mode is enabled
	ValidationFeedback      string
	PreviousIterationOutput string // Previous loop iteration execution output (for loop steps)
	VariableNames           string // Variable names with descriptions ({{VAR_NAME}} - description)
	VariableValues          string // Variable names with actual values ({{VAR_NAME}} = value - description)
	HasLoop                 string // "true" or "false" as string
	LoopCondition           string // Loop condition description (required when HasLoop="true")
	LoopDescription         string // Human-readable explanation of the loop (optional)
	CurrentIteration        string // Current iteration number
	MaxIterations           string // Max iterations allowed
	LearningHistory         string // Formatted learning conversation history (REQUIRED for execution-only mode)
}

// HumanControlledTodoPlannerExecutionOnlyAgent executes steps using pre-discovered learning context
// This agent does NOT discover learnings - it receives learning history from LearningReadingAgent
type HumanControlledTodoPlannerExecutionOnlyAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerExecutionOnlyAgent creates a new execution-only agent
func NewHumanControlledTodoPlannerExecutionOnlyAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerExecutionOnlyAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionAgentType, // Reuse execution agent type for consistency
		eventBridge,
	)

	return &HumanControlledTodoPlannerExecutionOnlyAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpeoa *HumanControlledTodoPlannerExecutionOnlyAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := hctpeoa.executionOnlySystemPromptProcessor(templateVars)
	userMessage := hctpeoa.executionOnlyUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use ExecuteWithTemplateValidation with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return hctpeoa.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
}

// executionOnlySystemPromptProcessor generates the system prompt for execution-only agent
func (hctpeoa *HumanControlledTodoPlannerExecutionOnlyAgent) executionOnlySystemPromptProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	hasLoop := templateVars["HasLoop"] == "true"
	stepContextOutput := templateVars["StepContextOutput"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"
	learningHistory := templateVars["LearningHistory"]

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Get code execution instructions (reuse from builder.go)
	codeExecutionInstructions := ""
	if isCodeExecutionMode {
		// Get the reusable instructions - keep {{TOOL_STRUCTURE}} placeholder
		// agent.go will automatically replace it with actual tool structure when SetSystemPrompt is called
		codeExecutionInstructions = prompt.GetCodeExecutionInstructions()
	}

	// Define the system prompt template
	templateStr := `# Execution-Only Agent

## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Execution-Only Agent
- **Responsibility**: Execute a single step from the plan using MCP tools
- **Mode**: Single step execution (learning discovery already completed)
{{if .IsCodeExecutionMode}}
## ⚡ CODE EXECUTION MODE ACTIVE

**You are operating in CODE EXECUTION MODE** - instead of making direct MCP tool calls, you will write and execute Go code.

{{.CodeExecutionInstructions}}
{{end}}

## 📚 LEARNING CONTEXT (Pre-Discovered)

**Learning discovery has been completed by the Learning Reading Agent. Use the following learning context:**

{{.LearningHistory}}

**Important**: The learning files and{{if .IsCodeExecutionMode}} Go code patterns{{else}} scripts{{end}} have already been discovered and read. Use the insights above to inform your execution approach.

## 📁 FILE PERMISSIONS

**READ (ORDER MATTERS):**
1. **FIRST**: Context files from previous steps ({{.WorkspacePath}}/step_X_results.md)
2. **SECOND**: Workspace files as needed (paths relative to {{.WorkspacePath}})
3. **NOTE**: Learning files have already been read - use the learning context above

**WRITE:**
- **ONLY** context output files in {{.WorkspacePath}}/ (e.g., {{.WorkspacePath}}/step_X_results.md)
- **NO** writing outside {{.WorkspacePath}} or to workspace root
- **NO** validation reports (validation agent handles those)

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

## 🔍 EXECUTION GUIDELINES

**⚠️ CRITICAL PRIORITY ORDER: CURRENT STEP DESCRIPTION ALWAYS TAKES PRECEDENCE ⚠️**

**The current step description is the PRIMARY source of truth. Learnings are GUIDANCE only - adapt them to match the current step requirements.**

1. **FIRST - Understand Current Step Requirements** (MANDATORY):
   - **Read and understand the CURRENT step description, success criteria, and context dependencies**
   - **This is your PRIMARY source of truth** - what needs to be accomplished RIGHT NOW
   - **If step description differs from learnings, FOLLOW THE STEP DESCRIPTION**
   - Identify what tools/scripts might be needed based on the current step requirements

2. **SECOND - Review Pre-Discovered Learning Context** (GUIDANCE - NOT STRICT RULES):
   - **Learning files have already been discovered and read** - review the learning context provided above
   - **Use the learning insights** as guidance for your execution approach
   - **Adapt learnings to match current step requirements** - don't use exact copies if step description differs
   - **PURPOSE**: These patterns from previous executions are GUIDANCE, not strict rules
   - **If step description is similar to learnings**: You can follow learnings more closely
   - **If step description differs significantly**: Prioritize step description, use learnings only as general guidance

3. **Read Context**: Check context dependencies for files from previous steps (read from {{.WorkspacePath}} folder)

4. **Adapt Learning Insights to Current Step** (GUIDANCE - ADAPT TO MATCH STEP DESCRIPTION):
   - **CRITICAL**: If current step description differs from learnings, FOLLOW THE STEP DESCRIPTION
   - **Use learnings as starting point**, but adapt them to match current step requirements:
     - Adapt success patterns from learnings to match current step description
     - Avoid failure patterns mentioned in learnings (still relevant)
{{if .IsCodeExecutionMode}}
     - **Modify Go code patterns** from learnings to match current step requirements (don't use exact copies if step description differs)
     - Adapt Go code examples from learnings to match current step needs (modify imports, function calls, logic as needed)
     - Reference best code patterns ranked by effectiveness from learning files
{{else}}
     - **Modify tool calls and arguments** from learnings to match current step requirements (don't use exact copies if step description differs)
     - Adapt Python scripts from learnings to match current step needs (modify as needed)
{{end}}

5. **Execute the Step**:
{{if .IsCodeExecutionMode}}
   - **Use Virtual Tools**: Use discover_code_files to see available Go packages and functions
   - **Write Go Code**: Use write_code to write and execute Go code that:
     - Imports generated tool packages (e.g., aws_tools, workspace_tools)
     - Calls tool functions with proper types and arguments
     - Uses workspace_tools for all file operations
     - Implements the logic needed to accomplish the step
   - **Reference Code Patterns**: Use Go code examples from learning context above as guidance, but adapt them to match current step requirements
{{else}}
   - **Use MCP Tools**: Select appropriate tools to accomplish the CURRENT step objective (as described in step description), using learnings as guidance
{{end}}

6. **Adapt Discovered Code/Scripts**:
{{if .IsCodeExecutionMode}}
   - Adapt Go code patterns from learning context to match current step requirements - modify them as needed rather than using exact copies
   - Use best code patterns ranked by effectiveness as starting points
{{else}}
   - Adapt Python scripts from learning context to match current step requirements - modify them as needed rather than using exact copies
{{end}}

7. **Verify Completion**: Check if success criteria (from CURRENT step description) is met

8. **Create Output**: Generate context output file for next steps (if specified)

9. **Document Results**: Provide clear summary of what was accomplished
{{if .HasLoop}}
7. **Save Progress After Each Iteration**: Update or append to the context output file ({{.WorkspacePath}}/{{.StepContextOutput}}) after each iteration to preserve progress
{{end}}

## 📤 Output Format

Provide a clear execution summary in your response with:
- **Status**: [COMPLETED/FAILED/IN_PROGRESS]
- **Actions Taken**: List tools used and results
- **Success Criteria Check**: Whether criteria was met with evidence
- **Context Output**: Path to any context file created (if applicable)

Return results in your response. The validation agent will document and verify your execution.`

	// Parse and execute the template
	tmpl, err := template.New("executionOnlySystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution-only system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"WorkspacePath":             workspacePath,
		"IsCodeExecutionMode":       isCodeExecutionMode,
		"CodeExecutionInstructions": codeExecutionInstructions,
		"HasLoop":                   hasLoop,
		"StepContextOutput":         stepContextOutput,
		"CurrentDate":               currentDate,
		"CurrentTime":               currentTime,
		"LearningHistory":           learningHistory,
	})
	if err != nil {
		return fmt.Sprintf("Error executing execution-only system prompt template: %v", err)
	}

	return result.String()
}

// executionOnlyUserMessageProcessor generates the user message for execution-only agent
func (hctpeoa *HumanControlledTodoPlannerExecutionOnlyAgent) executionOnlyUserMessageProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerExecutionOnlyTemplate{
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		IsCodeExecutionMode:     templateVars["IsCodeExecutionMode"],
		ValidationFeedback:      templateVars["ValidationFeedback"],
		PreviousIterationOutput: templateVars["PreviousIterationOutput"],
		VariableNames:           templateVars["VariableNames"],
		VariableValues:          templateVars["VariableValues"],
		HasLoop:                 templateVars["HasLoop"],
		LoopCondition:           templateVars["LoopCondition"],
		LoopDescription:         templateVars["LoopDescription"],
		CurrentIteration:        templateVars["CurrentIteration"],
		MaxIterations:           templateVars["MaxIterations"],
		LearningHistory:         templateVars["LearningHistory"],
	}

	// Define the user message template
	templateStr := `## 🎯 PRIMARY TASK - EXECUTE SINGLE STEP

**✅ LEARNING DISCOVERY COMPLETE**: Learning files{{if eq .IsCodeExecutionMode "true"}} and Go code patterns{{else}} and scripts{{end}} have already been discovered and read. Review the learning context in the system prompt above.

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
- **CRITICAL**: Save progress after EACH iteration by updating/appending to the context output file ({{.WorkspacePath}}/{{.StepContextOutput}}) - don't wait until the loop completes. Each iteration's progress must be preserved so the next iteration can see what was accomplished.

**Important**: 
- The loop condition ({{.LoopCondition}}) is the same as the success criteria
- Once the loop condition is met, the step will exit the loop and be marked as completed
- Continue executing until the condition is satisfied
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

## 🎯 CURRENT STEP EXECUTION

**Step - {{.StepTitle}}**
**Description**: {{.StepDescription}}

### 📋 Complete Step Information
**Success Criteria**: {{.StepSuccessCriteria}}
**Context Dependencies**: {{.StepContextDependencies}}
**Context Output**: {{.StepContextOutput}}

### 🔍 Step Context Analysis
**Success Criteria**: Use the success criteria above to verify completion
**Context Dependencies**: Check context dependencies for files from previous steps (read from {{.WorkspacePath}} folder)
{{if eq .HasLoop "true"}}
**Context Output**: Update or append to the context output file ({{.WorkspacePath}}/{{.StepContextOutput}}) after each iteration to preserve progress
{{else}}
**Context Output**: Create the context output file ({{.WorkspacePath}}/{{.StepContextOutput}}) specified above for other agents
{{end}}

**Your Task**: 
1. **FIRST**: Understand the CURRENT step description, success criteria, and requirements (this is your PRIMARY source of truth)
2. **SECOND**: Review the pre-discovered learning context in the system prompt above - use these as GUIDANCE, not strict rules
3. **THIRD**: Read context dependencies from previous steps (if any)
4. **FOURTH**: Execute this specific step:
   {{if eq .IsCodeExecutionMode "true"}}
   - **Use Virtual Tools**: First use discover_code_files to see available Go packages and functions
   - **Write Go Code**: Use write_code to write and execute Go code that accomplishes the step
   - **Reference Code Patterns**: Use Go code examples from learning context as guidance, but adapt them to match current step requirements
   {{else}}
   - **Use MCP Tools**: Select appropriate tools to accomplish the step
   {{end}}
   - **PRIORITY**: Follow the CURRENT step description above
   - **GUIDANCE**: Use learnings to inform your approach, but adapt them to match current step requirements
   - **IF STEP DESCRIPTION DIFFERS FROM LEARNINGS**: Follow the step description, adapt learnings as needed
   - Use the complete step information above, including success criteria, context dependencies, and context output requirements.`

	// Parse and execute the template
	tmpl, err := template.New("executionOnlyUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution-only user message template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing execution-only user message template: %v", err)
	}

	return result.String()
}
