package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/agent/prompt"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerExecutionOnlyTemplate holds template variables for execution-only agent prompts
type HumanControlledTodoPlannerExecutionOnlyTemplate struct {
	StepTitle                  string
	StepDescription            string
	StepSuccessCriteria        string
	StepContextDependencies    string
	StepContextOutput          string
	WorkspacePath              string
	IsCodeExecutionMode        string // "true" or "false" - indicates if code execution mode is enabled
	ValidationFeedback         string
	HumanFeedback              string // Human guidance provided after validation failure (highest priority)
	PreviousIterationOutput    string // Previous loop iteration execution output (for loop steps)
	VariableNames              string // Variable names with descriptions ({{VAR_NAME}} - description)
	VariableValues             string // Variable names with actual values ({{VAR_NAME}} = value - description)
	HasLoop                    string // "true" or "false" as string
	LoopCondition              string // Loop condition description (required when HasLoop="true")
	LoopDescription            string // Human-readable explanation of the loop (optional)
	CurrentIteration           string // Current iteration number
	MaxIterations              string // Max iterations allowed
	LearningHistory            string // Formatted learning conversation history (REQUIRED for execution-only mode)
	StepNumber                 string // Step identifier (e.g., "step-8" or "step-3-if-true-0")
	StepExecutionPath          string // Full execution folder path (e.g., "execution/step-8")
	DecisionReasoning          string // Context from decision step that routed to this step (empty if not routed from decision)
	DecisionEvaluationQuestion string // Evaluation question for decision inner steps (used to format output for LLM evaluation)
	PreviousStepsSummary       string // Summary of previous completed steps (titles, descriptions, outputs)
}

// HumanControlledTodoPlannerExecutionOnlyAgent executes steps using pre-discovered learning context
// This agent does NOT discover learnings - it receives learning history from LearningReadingAgent
type HumanControlledTodoPlannerExecutionOnlyAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerExecutionOnlyAgent creates a new execution-only agent
func NewHumanControlledTodoPlannerExecutionOnlyAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerExecutionOnlyAgent {
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
	stepNumber := templateVars["StepNumber"]               // e.g., "step-8" or "step-3-if-true-0"
	stepExecutionPath := templateVars["StepExecutionPath"] // e.g., "execution/step-8"
	previousStepsSummary := templateVars["PreviousStepsSummary"]

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

	// Get variable names and values for system prompt
	variableNames := templateVars["VariableNames"]
	variableValues := templateVars["VariableValues"]
	prerequisiteRulesInfo := templateVars["PrerequisiteRulesInfo"]
	decisionEvaluationQuestion := templateVars["DecisionEvaluationQuestion"]
	validationSchema := templateVars["ValidationSchema"] // Validation schema JSON string

	// Define the system prompt template
	templateStr := `# Execution-Only Agent

## 📅 Current Session
**Date**: {{.CurrentDate}} | **Time**: {{.CurrentTime}}

## 🤖 Agent Identity
- **Role**: Execution-Only Agent  
- **Responsibility**: Execute a single plan step using MCP tools or Go code  
- **Mode**: Pre-discovered learning context available (read-only)
{{if .IsCodeExecutionMode}}

## ⚡ Code Execution Mode Active
{{.CodeExecutionInstructions}}
{{end}}
{{if .VariableNames}}

## 🔑 Available Variables
{{.VariableNames}}
{{if .VariableValues}}

**Current Values**: {{.VariableValues}}
{{end}}

**Variable Handling**:
- **Step descriptions already have variables resolved** - you'll see actual values in StepDescription, StepSuccessCriteria, etc.
- **For new tool calls or code**: Use actual values directly from the resolved step description{{if .IsCodeExecutionMode}}
- **For Go code**: 
  - **🚨 CRITICAL**: WorkspacePath ({{.WorkspacePath}}) MUST be passed as FIRST CLI argument (ONLY base path)
  - **Tool call args**: args=["{{.WorkspacePath}}", ...other vars...] - NO full file paths in args
  - **Access workspace path**: workspacePath := os.Args[1] (always first argument)
  - **File paths in code**: Use filepath.Join(workspacePath, "step-N/file.json") for ALL files
  - **Context dependencies**: Use relative paths like "step-1/output.json" with filepath.Join()
  - **Other variables**: Pass via write_code 'args' parameter, access via os.Args[2], os.Args[3], etc.
  - **NEVER hardcode workspace paths** - they change between iterations (run-1 → run-2 → run-3)
  - **Example**: args=["{{.WorkspacePath}}", "account-id-value", "region-value"]{{end}}
- **Don't hardcode values** - reference them from the step context
{{end}}

## 🎯 PRIMARY: Step Requirements (SOURCE OF TRUTH)
The step description, success criteria, and context dependencies define WHAT you must accomplish.
**Always prioritize step requirements over learnings.**

{{if .PreviousStepsSummary}}
## 📋 Previous Steps Context
{{.PreviousStepsSummary}}
{{end}}
{{if .PrerequisiteRulesInfo}}
{{.PrerequisiteRulesInfo}}
{{end}}
## 📚 SECONDARY: Learning Context (BEST PRACTICE GUIDANCE)
{{.LearningHistory}}

**HOW TO USE LEARNINGS:**
- Learnings are **guidance for HOW to accomplish the step**, not WHAT to accomplish
- **Step description is PRIMARY** - learnings help you execute it better
- If learnings conflict with step requirements → **follow step requirements**
- If learnings are outdated or don't apply → **ignore them and solve the step directly**

**LEARNING APPLICATION:**
- **EXECUTION WORKFLOW exists**: Use as a proven approach to accomplish the step
  - Adapt tool calls and arguments to match current step requirements
  - Follow the sequence as a guideline, but verify each step applies
  - Use error recovery strategies when encountering similar issues
- **Only tool patterns exist**: Use as hints for which tools work well
- **Learnings don't match step**: Ignore learnings, solve step directly using available tools

**ACCESSING LEARNINGS FILES DIRECTLY:**
- **Pre-loaded context above** provides a summary of relevant learnings
- **If you get stuck or need more detail**: You can read learnings files directly from the learnings folder
  - Use "read_workspace_file" or "list_workspace_files" to explore learnings
  - Step-specific learnings: "learnings/step-{N}/*.md"{{if .IsCodeExecutionMode}} and "learnings/step-{N}/code/*.go"{{end}}
  - General learnings: "learnings/*.md"{{if .IsCodeExecutionMode}} and "learnings/code/*.go"{{end}}
  - Read files when you need: more detailed workflows{{if .IsCodeExecutionMode}}, code examples{{end}}, troubleshooting steps, or when pre-loaded context is insufficient

## 📁 File Permissions
**READ**: 
- **Learnings folder** ("learnings/") - You have full read access to all learning files
  - Step-specific: "learnings/step-{N}/*.md"{{if .IsCodeExecutionMode}} and "learnings/step-{N}/code/*.go"{{end}}
  - General: "learnings/*.md"{{if .IsCodeExecutionMode}} and "learnings/code/*.go"{{end}}
  - **Use this when stuck**: Read learnings files directly for detailed workflows{{if .IsCodeExecutionMode}}, code examples{{end}}, or troubleshooting
- **Execution folder** ("execution/") - To read previous step results and context dependencies
**WRITE**: 
- **🚨 CRITICAL**: Only your current step folder: {{.StepExecutionPath}}/ (which is {{.WorkspacePath}}/{{.StepNumber}}/)
- **Your step identifier**: {{.StepNumber}} - ALWAYS use this exact step number when writing files
- Cannot write to other steps' folders, learnings folder, or validation reports
- Path validation is enforced at the code level - invalid paths will be rejected

## 🎯 Execution Approach

**ALWAYS START WITH STEP REQUIREMENTS (Primary):**

1. **Understand the Step** (WHAT you must accomplish)
   - Read step description carefully - this is your PRIMARY goal
   - Understand success criteria - this defines when you're DONE
   - Check context dependencies - what inputs do you have?

2. **Apply Learnings as Best Practice** (HOW to accomplish it)
   - If EXECUTION WORKFLOW exists: Use as a proven approach
     - Adapt the workflow steps to match current step requirements
     - Use similar tool calls and arguments where applicable
     - Apply error recovery strategies for known failure modes
   - If only tool patterns exist: Use as hints for which tools work
   - If learnings don't apply: Ignore them and solve directly
   - **If stuck or need more detail**: Read learnings files directly from "learnings/" folder
     - Use "list_workspace_files" to see available learning files
     - Use "read_workspace_file" to read specific learning files for detailed guidance

3. **Execute & Verify**
   - Read context dependencies from {{.WorkspacePath}}
   - Execute using MCP tools{{if .IsCodeExecutionMode}} or Go code{{end}}
   - **If encountering issues**: Read relevant learnings files for troubleshooting steps
   - Verify success criteria met (collect evidence)
   - Create context output file{{if .HasLoop}} (update/append after each iteration){{end}}

**KEY PRINCIPLE:**
- **Step requirements** = WHAT to accomplish (mandatory)
- **Learnings** = HOW to accomplish it efficiently (optional guidance)
- If learnings conflict with step → **step wins**
- If learnings are outdated → **ignore and solve directly**

{{if .IsCodeExecutionMode}}## 💻 Code Execution Rules

**🚨 CRITICAL - WorkspacePath is ALWAYS os.Args[1]**

**Two-Step Process (Tool Call vs. Go Code):**

1. **Tool Call (write_code)**: You MUST pass ONLY the base workspace path as the first argument
   - ✅ **Correct**: args=["{{.WorkspacePath}}", "other_var1", "other_var2"]
   - ❌ **Wrong**: args=["{{.WorkspacePath}}/step-1/file.json", ...] (passing full file paths)
   - ❌ **Wrong**: args=["other_var1"] (missing workspace path)

2. **Go Code Content**: You MUST read the workspace path from os.Args[1] and use relative paths
   - ✅ **Correct**: workspacePath := os.Args[1], then filepath.Join(workspacePath, "step-1/file.json")
   - ❌ **Wrong**: filepath := "workspace/runs/run-1/execution/step-1" (hardcoded path)
   - ❌ **Wrong**: filepath := os.Args[2] where Args[2] is a full path (defeats purpose)

**Why This Matters:**
- Workspace paths change between iterations (run-1 → run-2 → run-3)
- Passing ONLY the base path makes code reusable
- Use relative paths for all file operations: filepath.Join(basePath, "step-1/file.json")

**Path Handling (CRITICAL):**
- **Base Path**: os.Args[1] is the base execution workspace (e.g., "Workflow/runs/iteration-11/execution")
- **Context Dependencies**: Use relative paths like "step-1/step_1_output.json" (NOT full paths)
- **File Construction**: filepath.Join(basePath, relativePath) for all file operations
- **Example**: 
  - Base: os.Args[1] → "Workflow/runs/iteration-11/execution"
  - Relative: "step-1/credentials.json"
  - Full: filepath.Join(basePath, "step-1/credentials.json")

**Variable Handling:**
- **Pass**: All variables via args parameter: args=["{{.WorkspacePath}}", "value1", "value2"]
- **Access**: Read from os.Args[1] (workspace path), os.Args[2], os.Args[3], etc. (os.Args[0] is program name)
- **NO Hardcoding**: Never hardcode variable values OR workspace paths inside the Go code string
- **NO Full Paths in Args**: Never pass full file paths as CLI arguments - use relative paths in code

**Packages & Operations:**
- **Packages**: Import generated tool packages (aws_tools, workspace_tools, etc.)
- **File Ops**: Always use workspace_tools for file operations with filepath.Join(basePath, relativePath)
- **Path Construction**: Always use filepath.Join() to construct paths from base + relative

**BEFORE GENERATING GO CODE - CRITICAL CHECKLIST:**
1. **🚨 WorkspacePath as FIRST CLI argument**: ALWAYS pass ONLY base WorkspacePath as os.Args[1] - NEVER pass full file paths
2. **🚨 Use Relative Paths**: ALL file paths in code must use filepath.Join(basePath, "step-N/file.json") - NEVER hardcode full paths
3. **Check FAILURES TO AVOID section** in learning context above - review ALL documented error patterns from previous executions
4. **Avoid documented error patterns**: For each error documented in learnings, ensure your code doesn't repeat the same mistake
5. **Use correct patterns** from successful code examples in learnings (but replace hardcoded paths with filepath.Join())
6. **If learnings show specific errors**: Make sure your code doesn't repeat them - follow the prevention guidance provided
7. **Verify Go syntax**: Ensure your code uses proper Go syntax and functions
8. **Path construction**: ALWAYS use filepath.Join(basePath, relativePath) for ALL file operations
9. **Parse tool responses correctly**: Follow the patterns shown in successful code examples for handling tool responses

**SUCCESS CRITERIA ASSERTION:**
Your Go code MUST verify success criteria programmatically. Don't just execute - assert each criterion:

Example patterns:
- File exists: if strings.HasPrefix(result, "Error:") → fmt.Println("❌ FAIL") + os.Exit(1)
- Count matches: if count != expected → fmt.Printf("❌ FAIL: Expected %%d, got %%d", expected, count) + os.Exit(1)
- Pattern found: if !strings.Contains(data, pattern) → fmt.Printf("❌ FAIL: Pattern not found") + os.Exit(1)

Print "✅ PASS: [criterion]" for each success, "❌ FAIL: [reason]" + os.Exit(1) for failures.
{{end}}
{{if .ValidationSchema}}

## ✅ Expected File Structure (Validation Schema)

**CRITICAL**: Your output files MUST match the validation schema structure below. Pre-validation will check these requirements automatically.

**Validation Schema** (JSON):
{{printf "%s" .ValidationSchema}}

**IMPORTANT**:
- Create files with the exact structure specified in the validation schema
- Ensure all required fields exist at the specified JSON paths
- Match the expected data types (string, array, number, etc.)
- Files must be created in your step execution folder: {{.StepExecutionPath}}/
- The validation schema defines the exact file names and JSON structure expected
- The validation schema paths (like $.plan_introduction.objective) tell you the exact nested structure required
{{end}}

## 📤 Output Format
**Status**: [COMPLETED/FAILED/IN_PROGRESS]  
**Actions**: Tools used + quantitative results  
**Evidence**: Specific outputs proving completion (e.g., "grep found 15 matches")  
**Context Output**: File path if created
**Workflow Deviations**: Note any deviations from learned workflow (if applicable)

{{if .DecisionEvaluationQuestion}}
## 🤖 IMPORTANT: LLM Evaluation Formatting

**Your output will be evaluated by an LLM** to determine: {{.DecisionEvaluationQuestion}}

**CRITICAL**: Format your output to make it easy for the LLM to answer this question. Include:

1. **Clear Status**: Explicitly state whether the step succeeded or failed
2. **Relevant Evidence**: Include specific information that directly relates to the evaluation question
   - If the question asks about file format → show file extension, headers, structure
   - If the question asks about verification → show verification results, checks performed
   - If the question asks about completion → show completion indicators, final state
3. **Quantitative Results**: Include numbers, counts, or measurable outcomes when relevant
4. **Key Findings**: Highlight the most important information that answers the evaluation question
5. **Structured Information**: Organize output clearly with sections if needed

**Example**: If the evaluation question is "Does the file have .txt extension and contain expected headers?"
- ✅ **Good Output**: "Status: COMPLETED. Downloaded file: data.txt (extension: .txt). File headers verified: Line 1 contains 'Date', Line 2 contains 'Narration', Line 3 contains 'Closing Balance'. All expected headers found."
- ❌ **Bad Output**: "File downloaded successfully." (missing extension and header information)

**Remember**: The LLM evaluating your output only sees your text response - make it comprehensive and clear!
{{end}}

Validation agent will verify your work - focus on execution and evidence.`

	// Parse and execute the template
	tmpl, err := template.New("executionOnlySystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution-only system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"WorkspacePath":              workspacePath,
		"IsCodeExecutionMode":        isCodeExecutionMode,
		"CodeExecutionInstructions":  codeExecutionInstructions,
		"HasLoop":                    hasLoop,
		"StepContextOutput":          stepContextOutput,
		"CurrentDate":                currentDate,
		"CurrentTime":                currentTime,
		"LearningHistory":            learningHistory,
		"VariableNames":              variableNames,
		"VariableValues":             variableValues,
		"StepNumber":                 stepNumber,
		"StepExecutionPath":          stepExecutionPath,
		"PreviousStepsSummary":       previousStepsSummary,
		"PrerequisiteRulesInfo":      prerequisiteRulesInfo,
		"DecisionEvaluationQuestion": decisionEvaluationQuestion,
		"ValidationSchema":           validationSchema, // Validation schema JSON string
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
		HumanFeedback:           templateVars["HumanFeedback"],
		PreviousIterationOutput: templateVars["PreviousIterationOutput"],
		VariableNames:           templateVars["VariableNames"],
		VariableValues:          templateVars["VariableValues"],
		HasLoop:                 templateVars["HasLoop"],
		LoopCondition:           templateVars["LoopCondition"],
		LoopDescription:         templateVars["LoopDescription"],
		CurrentIteration:        templateVars["CurrentIteration"],
		MaxIterations:           templateVars["MaxIterations"],
		LearningHistory:         templateVars["LearningHistory"],
		StepNumber:              templateVars["StepNumber"],
		StepExecutionPath:       templateVars["StepExecutionPath"],
		DecisionReasoning:       templateVars["DecisionReasoning"],
		PreviousStepsSummary:    templateVars["PreviousStepsSummary"],
	}

	// Define the user message template
	templateStr := `## 🎯 Execute Step: {{.StepTitle}}

**STEP DESCRIPTION**: {{.StepDescription}}  
**WORKSPACE**: {{.WorkspacePath}}  
**STEP NUMBER**: {{.StepNumber}} (write all output files to {{.StepExecutionPath}}/)

{{if .PreviousStepsSummary}}
{{.PreviousStepsSummary}}
{{end}}
{{if .VariableNames}}## 📋 Variables
{{.VariableNames}}
{{if .VariableValues}}
**Values**: {{.VariableValues}}
{{end}}
{{if eq .IsCodeExecutionMode "true"}}
## 📝 Code Execution Example

**Task**: Read a context dependency file and write output to current step folder.

**Tool Call Format**:
write_code(
  code="...",
  args=["{{.WorkspacePath}}", "userId123"]
)

**Go Code Content Pattern**:
package main
import (
    "os"
    "path/filepath"
    "workspace_tools"
)

func main() {
    // 1. Read base workspace path (ALWAYS first argument)
    basePath := os.Args[1]      // e.g., "Workflow/runs/iteration-11/execution"
    userId := os.Args[2]        // Additional variables
    
    // 2. Use relative paths with filepath.Join()
    // Read context dependency (previous step output)
    inputPath := filepath.Join(basePath, "step-1/credentials.json")
    inputData := workspace_tools.ReadWorkspaceFile(workspace_tools.ReadWorkspaceFileParams{
        Filepath: inputPath,
    })
    
    // Write to current step folder (ALWAYS use {{.StepNumber}} - never hardcode step numbers)
    outputPath := filepath.Join(basePath, "{{.StepNumber}}/analysis.json")
    result := workspace_tools.UpdateWorkspaceFile(workspace_tools.UpdateWorkspaceFileParams{
        Filepath: outputPath,
        Content:  "...",
    })
}

**Key Points**:
- Tool call: Pass ONLY base workspace path (NOT full file paths)
- Go code: Use relative paths like "{{.StepNumber}}/file.json" (ALWAYS use {{.StepNumber}}, never hardcode step numbers)
- Path construction: Always use filepath.Join(basePath, "{{.StepNumber}}", filename)
- Context dependencies: Relative paths from base execution folder (e.g., "step-1/file.json" for previous steps)
- **🚨 CRITICAL**: When writing output files, ALWAYS use "{{.StepNumber}}" - never guess or hardcode step numbers!
{{end}}{{end}}
{{if eq .HasLoop "true"}}
## 🔄 Loop Mode Active
**Loop Condition**: {{.LoopCondition}}  
{{if .LoopDescription}}**Loop Description**: {{.LoopDescription}}  
{{end}}**Iteration**: {{.CurrentIteration}} / {{.MaxIterations}}

**Task**: Execute step repeatedly until loop condition met. **Save progress after EACH iteration** to {{.StepExecutionPath}}/{{.StepContextOutput}} (update/append, don't overwrite).  
**🚨 CRITICAL**: Always write to {{.StepNumber}} folder - never use a different step number!
{{end}}
{{if .PreviousIterationOutput}}
## 🔄 Previous Iteration Output
{{.PreviousIterationOutput}}

Review what was done previously to avoid unnecessary repetition.
{{end}}
{{if .ValidationFeedback}}
## ⚠️ Validation Feedback
{{.ValidationFeedback}}

Address the issues above and improve your approach.
{{end}}
{{if .HumanFeedback}}
## 👤 **HUMAN GUIDANCE - HIGHEST PRIORITY**

{{.HumanFeedback}}

**⚠️ CRITICAL: This human guidance takes precedence over all other instructions (validation feedback, learnings, step descriptions).**
{{end}}
{{if .DecisionReasoning}}
## 🎯 **IMPORTANT: Decision Context - READ CAREFULLY**

{{.DecisionReasoning}}

**🚨 CRITICAL: This decision context is IMPORTANT and MUST be considered when executing this step.**

**How to use this context:**
- **READ AND UNDERSTAND** why this step is being executed (what condition was evaluated)
- **USE the decision reasoning** to inform your approach and decision-making throughout execution
- **CONSIDER the decision result and reasoning** when determining how to accomplish the step
- The reasoning explains what was evaluated in the previous decision step and why routing led here
- **The execution output from the decision step** provides context about what was done before the decision was made
- **This context directly impacts** how you should approach and execute this step
{{end}}

## 📋 Step Details
**Success Criteria**: {{.StepSuccessCriteria}}  
**Context Dependencies**: {{.StepContextDependencies}}  
**Context Output**: {{.StepContextOutput}}

## ✅ Execution Checklist

**STEP 0: 🧠 ANALYSIS & PLAN**
- Briefly analyze the step requirements.
- Identify which variables need to be passed to write_code.
- Confirm you will use os.Args[1] for the workspace path.

**ALWAYS (Step Requirements are PRIMARY):**
1. ✓ **Understand step description** ← THIS IS YOUR GOAL
2. ✓ **Know success criteria** ← THIS DEFINES DONE
3. ✓ Read context dependencies from {{.WorkspacePath}}

**THEN Apply Learnings (Best Practice Guidance):**
4. ✓ Check if learnings apply to this step
   - If WORKFLOW exists: Use as proven approach, adapt to current step
   - If only patterns exist: Use as hints for which tools work
   - If learnings don't match: Ignore and solve directly
5. ✓ Execute using MCP tools{{if eq .IsCodeExecutionMode "true"}} or Go code{{end}}:
{{if eq .IsCodeExecutionMode "true"}}   - discover_code_files (see available packages)
   - write_code (🚨 ALWAYS pass WorkspacePath as FIRST arg: args=["{{.WorkspacePath}}", ...other vars...], access via os.Args[1], os.Args[2], etc.)
{{else}}   - Use appropriate MCP tools to accomplish step
{{end}}6. ✓ **Verify success criteria met** (collect evidence)
7. ✓ Create context output file at {{.StepExecutionPath}}/{{.StepContextOutput}}{{if eq .HasLoop "true"}} (update/append after each iteration){{end}}
   - **🚨 CRITICAL**: Always write to {{.StepNumber}} folder - use filepath.Join(basePath, "{{.StepNumber}}", filename)

**REMEMBER:**
- Step description = WHAT to do (mandatory)
- Learnings = HOW to do it efficiently (optional guidance)
- If learnings conflict with step → **step wins**`

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
