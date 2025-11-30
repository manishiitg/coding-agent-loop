package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/agent/prompt"
	"mcpagent/observability"
)

// HumanControlledTodoPlannerExecutionTemplate holds template variables for human-controlled execution prompts
type HumanControlledTodoPlannerExecutionTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	LearningsPath           string // Learnings folder path for reading learning files and scripts/code
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
	// Generate system prompt and user message separately
	systemPrompt := hctpea.executionSystemPromptProcessor(templateVars)
	userMessage := hctpea.executionUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use ExecuteWithTemplateValidation with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return hctpea.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
}

// executionSystemPromptProcessor generates the system prompt for execution agent
func (hctpea *HumanControlledTodoPlannerExecutionAgent) executionSystemPromptProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	learningsPath := templateVars["LearningsPath"]
	hasLoop := templateVars["HasLoop"] == "true"
	stepContextOutput := templateVars["StepContextOutput"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"

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
	templateStr := `# Execution Agent

## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Execution Agent
- **Responsibility**: Execute a single step from the plan using MCP tools
{{if .IsCodeExecutionMode}}
## ⚡ CODE EXECUTION MODE ACTIVE

**You are operating in CODE EXECUTION MODE** - instead of making direct MCP tool calls, you will write and execute Go code.

{{.CodeExecutionInstructions}}

### **Learning from Code Patterns:**
- Look for Go code examples in {{.LearningsPath}}/code/ folder (.go files)
- These contain working code patterns that successfully executed similar steps
- Adapt these patterns to match your current step requirements
- Reference best code examples ranked by effectiveness
{{end}}
- **Mode**: Single step execution

## 📁 FILE PERMISSIONS

**READ (ORDER MATTERS):**
1. **FIRST**: Learning files/scripts from {{.LearningsPath}}/ (auto-discover by name matching - see EXECUTION GUIDELINES)
2. **SECOND**: Context files from previous steps ({{.WorkspacePath}}/step_X_results.md)
3. **THIRD**: Workspace files as needed (paths relative to {{.WorkspacePath}})

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

2. **SECOND - Auto-Discover Learning Files and Code Patterns** (GUIDANCE - NOT STRICT RULES):
   - **After understanding current step**, discover relevant learning files and code patterns:
     1. **List all learning files**: Use list_workspace_files to discover all files in {{.LearningsPath}}/ (max_depth: 1)
     2. **Match files by name similarity**: 
        - Look for files whose names contain keywords from the step title/description
        - Files typically named: *{keyword}_learning.md, general_learnings.md, or similar patterns
        - Match based on step title words, not exact matches (e.g., "Deploy Application" matches "Deploy_application_learning.md", "deployment_learning.md", etc.)
{{if .IsCodeExecutionMode}}
     3. **List all Go code patterns**: Use list_workspace_files to discover all Go code files in {{.LearningsPath}}/code/ (max_depth: 1)
     4. **Match code patterns by name similarity**:
        - Look for Go files whose names contain keywords from the step title/description
        - Files typically named: *{keyword}_code.go, *{keyword}_code_v1.go, etc.
        - Match based on step title words (e.g., "Deploy Application" matches "Deploy_application_code.go", "deployment_code.go", etc.)
        - These contain working Go code patterns ranked by effectiveness
     5. **Read discovered files**: Read ALL relevant learning files and Go code patterns using read_workspace_file tool
     6. **Dynamic discovery**: If you encounter problems during execution, list and read additional learning files/code patterns that might be relevant based on the problem context
{{else}}
     3. **List all scripts**: Use list_workspace_files to discover all Python scripts in {{.LearningsPath}}/scripts/ (max_depth: 1)
     4. **Match scripts by name similarity**:
        - Look for scripts whose names contain keywords from the step title/description
        - Scripts typically named: *{keyword}_script.py or similar patterns
        - Match based on step title words (e.g., "Deploy Application" matches "Deploy_application_script.py", "deployment_script.py", etc.)
     5. **Read discovered files**: Read ALL relevant learning files and scripts using read_workspace_file tool
     6. **Dynamic discovery**: If you encounter problems during execution, list and read additional learning files/scripts that might be relevant based on the problem context
{{end}}
   - **PURPOSE**: These files contain patterns from previous executions - use them as GUIDANCE, not strict rules
   - **Discovery strategy**: Use name-based matching (keywords, partial matches) rather than exact matches - be flexible in finding relevant files

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
     - **Adapt Python scripts from learnings** to match current step needs:
       - **Preserve parameter structure**: If scripts use argparse or environment variables, keep that structure
       - **Pass actual variable values**: Use variable values from step context when executing scripts
       - **Modify script logic as needed**: Adapt the script's functionality to match current step, but don't hardcode values that should be parameters
{{end}}
   - **If step description is similar to learnings**: You can follow learnings more closely
   - **If step description differs significantly**: Prioritize step description, use learnings only as general guidance

5. **Execute the Step**:
{{if .IsCodeExecutionMode}}
   - **Use Virtual Tools**: Use discover_code_files to see available Go packages and functions
   - **Write Go Code**: Use write_code to write and execute Go code that:
     - Imports generated tool packages (e.g., aws_tools, workspace_tools)
     - Calls tool functions with proper types and arguments
     - Uses workspace_tools for all file operations
     - Implements the logic needed to accomplish the step
   - **Reference Code Patterns**: Use Go code examples from {{.LearningsPath}}/code/ as guidance, but adapt them to match current step requirements
{{else}}
   - **Use MCP Tools**: Select appropriate tools to accomplish the CURRENT step objective (as described in step description), using learnings as guidance
{{end}}

6. **Adapt Discovered Code/Scripts**:
{{if .IsCodeExecutionMode}}
   - Adapt Go code patterns from {{.LearningsPath}}/code/ to match current step requirements - modify them as needed rather than using exact copies
   - Use best code patterns ranked by effectiveness as starting points
{{else}}
   - **Execute Python Scripts with Parameters**: When using Python scripts from {{.LearningsPath}}/scripts/:
     - **Check script structure**: Read the script to understand how it accepts variables:
       - **If script uses argparse**: Script accepts command-line arguments (e.g., --account-id, --region)
       - **If script uses os.getenv()**: Script reads from environment variables
       - **If script has hardcoded values**: Adapt the script to use parameters (preferred) or modify values as needed
     - **Pass variables as parameters**:
       - **For argparse scripts**: Execute with command-line arguments using actual variable values
         - Example: python script.py --account-id "123456789012" --region "us-east-1"
         - Use the variable values provided in the step context (from VariableValues)
       - **For environment variable scripts**: Set environment variables before execution
         - Example: AWS_ACCOUNT_ID="123456789012" AWS_REGION="us-east-1" python script.py
         - Export or set environment variables with actual values from VariableValues
     - **Adapt scripts as needed**: Modify scripts to match current step requirements, but preserve parameter structure
     - **DO NOT hardcode values**: Always use parameters/arguments when scripts support them - never modify scripts to hardcode values that should be parameters
{{end}}

8. **Verify Completion**: Check if success criteria (from CURRENT step description) is met

9. **Create Output**: Generate context output file for next steps (if specified)

10. **Document Results**: Provide clear summary of what was accomplished
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
	tmpl, err := template.New("executionSystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"WorkspacePath":             workspacePath,
		"LearningsPath":             learningsPath,
		"IsCodeExecutionMode":       isCodeExecutionMode,
		"CodeExecutionInstructions": codeExecutionInstructions,
		"HasLoop":                   hasLoop,
		"StepContextOutput":         stepContextOutput,
		"CurrentDate":               currentDate,
		"CurrentTime":               currentTime,
	})
	if err != nil {
		return fmt.Sprintf("Error executing execution system prompt template: %v", err)
	}

	return result.String()
}

// executionUserMessageProcessor generates the user message for execution agent
func (hctpea *HumanControlledTodoPlannerExecutionAgent) executionUserMessageProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerExecutionTemplate{
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		LearningsPath:           templateVars["LearningsPath"],
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
	}

	// Define the user message template
	templateStr := `## 🎯 PRIMARY TASK - EXECUTE SINGLE STEP

**⚠️ CRITICAL FIRST STEP**: Before executing, you MUST auto-discover and read ALL relevant learning files{{if eq .IsCodeExecutionMode "true"}} and Go code patterns{{else}} and scripts{{end}} from {{.LearningsPath}}/ folder. See EXECUTION GUIDELINES section in system prompt for detailed instructions.

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

**CRITICAL - Python Script Execution with Variables**:
- **When executing Python scripts**: Scripts may use variables as parameters (argparse) or environment variables
- **For argparse scripts**: Pass variable values as command-line arguments (e.g., --account-id "123456789012" --region "us-east-1")
- **For environment variable scripts**: Set environment variables before execution (e.g., AWS_ACCOUNT_ID="123456789012" python script.py)
- **Use actual variable values**: Use the values from VariableValues above when executing scripts
- **DO NOT hardcode values**: Always use parameters/arguments when scripts support them
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
**Context Dependencies**: After reading learnings (step 1 below), check context dependencies for files from previous steps (read from {{.WorkspacePath}} folder)
{{if eq .HasLoop "true"}}
**Context Output**: Update or append to the context output file ({{.WorkspacePath}}/{{.StepContextOutput}}) after each iteration to preserve progress
{{else}}
**Context Output**: Create the context output file ({{.WorkspacePath}}/{{.StepContextOutput}}) specified above for other agents
{{end}}

**Your Task**: 
1. **FIRST**: Understand the CURRENT step description, success criteria, and requirements (this is your PRIMARY source of truth)
2. **SECOND**: Auto-discover and read relevant learning files{{if eq .IsCodeExecutionMode "true"}} and Go code patterns{{else}} and scripts{{end}} from {{.LearningsPath}}/ folder (see EXECUTION GUIDELINES in system prompt) - use these as GUIDANCE, not strict rules
3. **THIRD**: Read context dependencies from previous steps (if any)
4. **FOURTH**: Execute this specific step:
   {{if eq .IsCodeExecutionMode "true"}}
   - **Use Virtual Tools**: First use discover_code_files to see available Go packages and functions
   - **Write Go Code**: Use write_code to write and execute Go code that accomplishes the step
   - **Reference Code Patterns**: Use Go code examples from learnings as guidance, but adapt them to match current step requirements
   {{else}}
   - **Use MCP Tools**: Select appropriate tools to accomplish the step
   {{end}}
   - **PRIORITY**: Follow the CURRENT step description above
   - **GUIDANCE**: Use learnings to inform your approach, but adapt them to match current step requirements
   - **IF STEP DESCRIPTION DIFFERS FROM LEARNINGS**: Follow the step description, adapt learnings as needed
   - **Python Script Execution**: When executing Python scripts:
     - Check if scripts use argparse (command-line arguments) or environment variables
     - Pass variable values as parameters/arguments using actual values from VariableValues above
     - DO NOT hardcode values in scripts - always use parameters when scripts support them
   - Use the complete step information above, including success criteria, context dependencies, and context output requirements.`

	// Parse and execute the template
	tmpl, err := template.New("executionUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution user message template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing execution user message template: %v", err)
	}

	return result.String()
}
