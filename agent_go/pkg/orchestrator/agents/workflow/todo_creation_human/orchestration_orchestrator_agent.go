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

// HumanControlledTodoPlannerOrchestrationOrchestratorAgent executes the main orchestration step
// This agent focuses on orchestration and delegation, not direct execution
type HumanControlledTodoPlannerOrchestrationOrchestratorAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerOrchestrationOrchestratorAgent creates a new orchestration orchestrator agent
func NewHumanControlledTodoPlannerOrchestrationOrchestratorAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerOrchestrationOrchestratorAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.OrchestrationAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerOrchestrationOrchestratorAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// OrchestrationOrchestratorTemplate holds template variables for orchestration orchestrator agent prompts
type OrchestrationOrchestratorTemplate struct {
	StepTitle                       string
	StepDescription                 string
	StepSuccessCriteria             string
	StepContextDependencies         string
	StepContextOutput               string
	WorkspacePath                   string
	IsCodeExecutionMode             string
	VariableNames                   string
	VariableValues                  string
	StepNumber                      string
	StepExecutionPath               string
	PreviousStepsSummary            string
	OrchestrationRoutes             string // Description of available sub-agents
	OrchestrationEvaluationQuestion string // Question used to evaluate output
}

// Execute implements the OrchestratorAgent interface
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := hctpooa.orchestrationOrchestratorSystemPromptProcessor(templateVars)
	userMessage := hctpooa.orchestrationOrchestratorUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use ExecuteWithTemplateValidation with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return hctpooa.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
}

// orchestrationOrchestratorSystemPromptProcessor generates the system prompt for orchestration orchestrator agent
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) orchestrationOrchestratorSystemPromptProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"
	stepNumber := templateVars["StepNumber"]
	stepExecutionPath := templateVars["StepExecutionPath"]
	previousStepsSummary := templateVars["PreviousStepsSummary"]
	orchestrationRoutes := templateVars["OrchestrationRoutes"]
	orchestrationEvaluationQuestion := templateVars["OrchestrationEvaluationQuestion"]

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Get code execution instructions (reuse from builder.go)
	codeExecutionInstructions := ""
	if isCodeExecutionMode {
		codeExecutionInstructions = prompt.GetCodeExecutionInstructions()
	}

	// Get variable names and values for system prompt
	variableNames := templateVars["VariableNames"]
	variableValues := templateVars["VariableValues"]

	// Define the system prompt template
	templateStr := `# Orchestration Orchestrator Agent

## 📅 Current Session
**Date**: {{.CurrentDate}} | **Time**: {{.CurrentTime}}

## 🤖 Agent Identity
- **Role**: Orchestration Orchestrator Agent  
- **Responsibility**: Coordinate and delegate work to specialized sub-agents  
- **Mode**: Orchestration and delegation (NOT direct execution)

**CRITICAL**: Your role is to **ORCHESTRATE and DELEGATE**, not to execute the actual work yourself. You analyze the situation, prepare information, and provide structured output that will be evaluated to select the appropriate sub-agent.

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

## 🎯 PRIMARY: Orchestration Role (SOURCE OF TRUTH)

**YOUR ROLE**: You are an **ORCHESTRATOR**, not an executor. Your job is to:
1. **Analyze** the current situation and context
2. **Prepare** information needed for delegation decisions
3. **Provide structured output** that will be evaluated to select the appropriate sub-agent
4. **DO NOT execute the actual work** - that's what sub-agents do

{{if .PreviousStepsSummary}}
## 📋 Previous Steps Context
{{.PreviousStepsSummary}}
{{end}}

## 🎯 Available Sub-Agents

{{.OrchestrationRoutes}}

**Understanding Sub-Agents**:
- Each sub-agent is a specialized agent that can handle specific situations
- Your output will be evaluated using this question: **{{.OrchestrationEvaluationQuestion}}**
- Based on your output, the system will select the most appropriate sub-agent
- Sub-agents will execute the actual work - you just prepare the context

## 📁 File Permissions
**READ**: 
- **Execution folder** ("execution/") - To read previous step results and context dependencies
**WRITE**: 
- **🚨 CRITICAL**: Only your current step folder: {{.StepExecutionPath}}/ (which is {{.WorkspacePath}}/{{.StepNumber}}/)
- **Your step identifier**: {{.StepNumber}} - ALWAYS use this exact step number when writing files
- Cannot write to other steps' folders or validation reports
- Path validation is enforced at the code level - invalid paths will be rejected

## 🎯 Orchestration Approach

**ALWAYS START WITH ORCHESTRATION REQUIREMENTS (Primary):**

1. **Understand the Situation** (WHAT needs to be orchestrated)
   - Read step description carefully - what situation needs to be handled?
   - Understand success criteria - when is orchestration complete?
   - Check context dependencies - what inputs do you have?
   - Review conversation history - what has been discussed and attempted in previous iterations?

2. **Analyze for Delegation** (HOW to prepare for sub-agent selection)
   - Analyze the current state and situation
   - Identify what information is needed for sub-agent selection
   - Prepare structured output that clearly describes the situation
   - Consider which sub-agent conditions might match the current state
   {{if .IsCodeExecutionMode}}
   - If needed, use tools to gather additional information (read files, check status, etc.)
   {{end}}

3. **Provide Structured Output** (FOR SUB-AGENT SELECTION)
   - Create clear, structured output that describes the current situation
   - Include relevant details that will help evaluate which sub-agent to select
   - Format output to be easily evaluated against sub-agent conditions
   - Save output to context file{{if eq .IsCodeExecutionMode "true"}} (if using code execution mode){{end}}

**KEY PRINCIPLE:**
- **Your role** = Orchestrate and prepare information for delegation
- **Sub-agents' role** = Execute the actual work
- **Your output** = Information that will be evaluated to select the right sub-agent

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
{{end}}

## 📤 Output Format
**Status**: [ORCHESTRATION_COMPLETE/ORCHESTRATION_IN_PROGRESS/NEEDS_SUB_AGENT]  
**Analysis**: Clear description of the current situation  
**Context**: Information relevant for sub-agent selection  
**Evidence**: Specific details that support the analysis  
**Context Output**: File path if created

**REMEMBER:**
- You are orchestrating, not executing
- Your output will be evaluated to select a sub-agent
- Sub-agents will do the actual work
- Provide clear, structured information for delegation decisions`

	// Parse and execute the template
	tmpl, err := template.New("orchestrationOrchestratorSystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing orchestration orchestrator system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"WorkspacePath":                   workspacePath,
		"IsCodeExecutionMode":             isCodeExecutionMode,
		"CodeExecutionInstructions":       codeExecutionInstructions,
		"StepNumber":                      stepNumber,
		"StepExecutionPath":               stepExecutionPath,
		"PreviousStepsSummary":            previousStepsSummary,
		"OrchestrationRoutes":             orchestrationRoutes,
		"OrchestrationEvaluationQuestion": orchestrationEvaluationQuestion,
		"VariableNames":                   variableNames,
		"VariableValues":                  variableValues,
		"CurrentDate":                     currentDate,
		"CurrentTime":                     currentTime,
	})
	if err != nil {
		return fmt.Sprintf("Error executing orchestration orchestrator system prompt template: %v", err)
	}

	return result.String()
}

// orchestrationOrchestratorUserMessageProcessor generates the user message for orchestration orchestrator agent
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) orchestrationOrchestratorUserMessageProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := OrchestrationOrchestratorTemplate{
		StepTitle:                       templateVars["StepTitle"],
		StepDescription:                 templateVars["StepDescription"],
		StepSuccessCriteria:             templateVars["StepSuccessCriteria"],
		StepContextDependencies:         templateVars["StepContextDependencies"],
		StepContextOutput:               templateVars["StepContextOutput"],
		WorkspacePath:                   templateVars["WorkspacePath"],
		IsCodeExecutionMode:             templateVars["IsCodeExecutionMode"],
		VariableNames:                   templateVars["VariableNames"],
		VariableValues:                  templateVars["VariableValues"],
		StepNumber:                      templateVars["StepNumber"],
		StepExecutionPath:               templateVars["StepExecutionPath"],
		PreviousStepsSummary:            templateVars["PreviousStepsSummary"],
		OrchestrationRoutes:             templateVars["OrchestrationRoutes"],
		OrchestrationEvaluationQuestion: templateVars["OrchestrationEvaluationQuestion"],
	}

	// Define the user message template
	templateStr := `## 🎯 Orchestrate Step: {{.StepTitle}}

**STEP DESCRIPTION**: {{.StepDescription}}  
**WORKSPACE**: {{.WorkspacePath}}  
**STEP NUMBER**: {{.StepNumber}} (write all output files to {{.StepExecutionPath}}/)

**🚨 CRITICAL: Your role is ORCHESTRATION, not execution.**
- Analyze the situation and prepare information for sub-agent selection
- DO NOT execute the actual work - that's what sub-agents do
- Provide structured output that will be evaluated to select the appropriate sub-agent

{{if .PreviousStepsSummary}}
{{.PreviousStepsSummary}}
{{end}}

{{if .VariableNames}}## 📋 Variables
{{.VariableNames}}
{{if .VariableValues}}
**Values**: {{.VariableValues}}
{{end}}
{{end}}

## 🎯 Available Sub-Agents

{{.OrchestrationRoutes}}

**Evaluation Question**: {{.OrchestrationEvaluationQuestion}}

Your output will be evaluated using this question to select the most appropriate sub-agent.

{{if eq .IsCodeExecutionMode "true"}}
## 📝 Code Execution Example

**Task**: Analyze the situation and prepare information for sub-agent selection.

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
    
    // 3. Analyze the situation and prepare output
    // Write orchestration output to current step folder
    outputPath := filepath.Join(basePath, "{{.StepNumber}}/orchestration_analysis.json")
    result := workspace_tools.UpdateWorkspaceFile(workspace_tools.UpdateWorkspaceFileParams{
        Filepath: outputPath,
        Content:  "...", // Structured analysis for sub-agent selection
    })
}

**Key Points**:
- Tool call: Pass ONLY base workspace path (NOT full file paths)
- Go code: Use relative paths like "{{.StepNumber}}/file.json" (ALWAYS use {{.StepNumber}}, never hardcode step numbers)
- Path construction: Always use filepath.Join(basePath, "{{.StepNumber}}", filename)
- Context dependencies: Relative paths from base execution folder (e.g., "step-1/file.json" for previous steps)
- **🚨 CRITICAL**: When writing output files, ALWAYS use "{{.StepNumber}}" - never guess or hardcode step numbers!
{{end}}

## 📋 Step Details
**Success Criteria**: {{.StepSuccessCriteria}}  
**Context Dependencies**: {{.StepContextDependencies}}  
**Context Output**: {{.StepContextOutput}}

## ✅ Orchestration Checklist

**STEP 0: 🧠 ANALYSIS & PLAN**
- Analyze the current situation
- Identify what information is needed for sub-agent selection
- Plan how to structure your output for evaluation

**ALWAYS (Orchestration Requirements are PRIMARY):**
1. ✓ **Understand step description** ← What situation needs orchestration?
2. ✓ **Know success criteria** ← When is orchestration complete?
3. ✓ Read context dependencies from {{.WorkspacePath}}
4. ✓ **Analyze the situation** - What is the current state?
5. ✓ **Prepare structured output** - Information for sub-agent selection
6. ✓ **Format for evaluation** - Make it easy to match against sub-agent conditions
7. ✓ Create context output file at {{.StepExecutionPath}}/{{.StepContextOutput}}
   - **🚨 CRITICAL**: Always write to {{.StepNumber}} folder - use filepath.Join(basePath, "{{.StepNumber}}", filename)

**REMEMBER:**
- You are an ORCHESTRATOR - coordinate and delegate
- Sub-agents will execute the actual work
- Your output helps select the right sub-agent`

	// Parse and execute the template
	tmpl, err := template.New("orchestrationOrchestratorUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing orchestration orchestrator user message template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing orchestration orchestrator user message template: %v", err)
	}

	return result.String()
}
