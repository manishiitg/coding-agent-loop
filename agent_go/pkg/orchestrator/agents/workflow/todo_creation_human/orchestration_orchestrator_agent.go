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
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	IsCodeExecutionMode     string
	VariableNames           string
	VariableValues          string
	StepNumber              string
	StepExecutionPath       string
	PreviousStepsSummary    string
	OrchestrationRoutes     string // Description of available sub-agents
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is kept for backward compatibility but ExecuteStructured should be used instead
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

// ExecuteStructured executes the orchestration orchestrator agent and returns structured OrchestrationResponse
// This includes routing decisions (which sub-agent to use) and success criteria evaluation
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*OrchestrationResponse, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := hctpooa.orchestrationOrchestratorSystemPromptProcessorStructured(templateVars)
	userMessage := hctpooa.orchestrationOrchestratorUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Build schema for structured output
	schema := `{
		"type": "object",
		"properties": {
			"selected_route_id": {
				"type": "string",
				"description": "ID of the route (sub-agent) to execute. Required if success_criteria_met is false, empty string if success_criteria_met is true."
			},
			"reasoning": {
				"type": "string",
				"description": "Detailed reasoning explaining route selection and success evaluation process"
			},
			"success_criteria_met": {
				"type": "boolean",
				"description": "Whether the orchestration step's success criteria is met"
			},
			"success_reasoning": {
				"type": "string",
				"description": "Detailed reasoning for success criteria evaluation. Required if success_criteria_met is true."
			},
			"success_criteria_verified_by_validation": {
				"type": "boolean",
				"description": "Whether validation confirmed that success criteria is met. This is set to false initially (validation hasn't run yet). Will be updated after validation if needed."
			},
			"instructions_to_sub_agent": {
				"type": "string",
				"description": "Detailed instructions to pass to the selected sub-agent. REQUIRED if selected_route_id is provided (not empty). These instructions REPLACE the sub-agent's step description and must provide detailed, step-by-step guidance on EXACTLY what the sub-agent should do. Include: specific actions to take, approach to follow, important context, expected behavior, and any critical requirements. Make instructions comprehensive, actionable, and unambiguous - the sub-agent should know exactly what to do without ambiguity."
			},
			"success_criteria_for_sub_agent": {
				"type": "string",
				"description": "Measurable and verifiable success criteria to pass to the selected sub-agent. REQUIRED if selected_route_id is provided (not empty). These criteria REPLACE the sub-agent's original success criteria and must be MEASURABLE and VERIFIABLE. Must be file-verifiable (reference specific file names, not paths), quantifiable (specific numbers, states, or conditions), and testable (can be objectively verified). Examples: 'File X contains exactly 5 entries', 'File Y exists with status field set to \"completed\"', 'Output file Z has validation errors count of 0'."
			},
			"context_dependencies_for_sub_agent": {
				"type": "string",
				"description": "Context dependencies to pass to the selected sub-agent. OPTIONAL if selected_route_id is provided. These dependencies REPLACE the sub-agent's original context dependencies and specify which files the sub-agent should read as input. Format: comma-separated list of relative file paths (e.g., \"step-1/output.json, step-2/credentials.json\")."
			},
			"context_output_for_sub_agent": {
				"type": "string",
				"description": "Context output file name to pass to the selected sub-agent. OPTIONAL if selected_route_id is provided. This REPLACES the sub-agent's original context output and specifies the output file name the sub-agent should create (e.g., \"step_3_output.json\"). The file will be created in the sub-agent's step folder."
			}
		},
		"required": ["selected_route_id", "reasoning", "success_criteria_met", "success_reasoning", "success_criteria_verified_by_validation"]
	}`

	// Define tool name and description for structured output via tool calls
	// This single tool handles three scenarios:
	// 1. call_sub_agent: When calling a sub-agent (provide selected_route_id, instructions_to_sub_agent, success_criteria_for_sub_agent)
	// 2. completed_success_criteria: When success criteria is met (provide success_criteria_met: true, success_reasoning)
	// 3. success_criteria_validated_after_validation: When validation confirms success (provide success_criteria_met: true, success_criteria_verified_by_validation: true)
	toolName := "submit_orchestration_result"
	toolDescription := `Submit the orchestration result. This tool handles three scenarios:
1. **call_sub_agent**: When calling a sub-agent - provide selected_route_id (required), instructions_to_sub_agent (required), success_criteria_for_sub_agent (required), context_dependencies_for_sub_agent (optional), context_output_for_sub_agent (optional), reasoning, success_criteria_met: false
2. **completed_success_criteria**: When success criteria is met - provide success_criteria_met: true, success_reasoning (required), selected_route_id: "", success_criteria_verified_by_validation: false
3. **success_criteria_validated_after_validation**: When validation confirms success - provide success_criteria_met: true, success_criteria_verified_by_validation: true, success_reasoning, selected_route_id: ""`

	// Use ExecuteStructuredWithInputProcessorViaTool
	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[OrchestrationResponse](
		hctpooa.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		schema,
		systemPrompt,
		true, // Overwrite system prompt
		toolName,
		toolDescription,
	)

	if err != nil {
		return nil, nil, fmt.Errorf("orchestration orchestrator structured execution failed: %w", err)
	}

	return &result, updatedHistory, nil
}

// orchestrationOrchestratorSystemPromptProcessor generates the system prompt for orchestration orchestrator agent
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) orchestrationOrchestratorSystemPromptProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"
	stepNumber := templateVars["StepNumber"]
	stepExecutionPath := templateVars["StepExecutionPath"]
	previousStepsSummary := templateVars["PreviousStepsSummary"]
	orchestrationRoutes := templateVars["OrchestrationRoutes"]

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
- Based on your analysis of the situation and success criteria, select the most appropriate sub-agent
- Sub-agents will execute the actual work - you analyze the situation and select the right route

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
		"WorkspacePath":             workspacePath,
		"IsCodeExecutionMode":       isCodeExecutionMode,
		"CodeExecutionInstructions": codeExecutionInstructions,
		"StepNumber":                stepNumber,
		"StepExecutionPath":         stepExecutionPath,
		"PreviousStepsSummary":      previousStepsSummary,
		"OrchestrationRoutes":       orchestrationRoutes,
		"VariableNames":             variableNames,
		"VariableValues":            variableValues,
		"CurrentDate":               currentDate,
		"CurrentTime":               currentTime,
	})
	if err != nil {
		return fmt.Sprintf("Error executing orchestration orchestrator system prompt template: %v", err)
	}

	return result.String()
}

// orchestrationOrchestratorSystemPromptProcessorStructured generates the system prompt for structured orchestration orchestrator agent
// This includes evaluation logic for success criteria and route selection
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) orchestrationOrchestratorSystemPromptProcessorStructured(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"
	stepNumber := templateVars["StepNumber"]
	stepExecutionPath := templateVars["StepExecutionPath"]
	previousStepsSummary := templateVars["PreviousStepsSummary"]
	orchestrationRoutes := templateVars["OrchestrationRoutes"]
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]

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

	// Check if this is a re-evaluation after validation (validation response in conversation history)
	// This will be handled by checking conversation history in the prompt

	// Define the system prompt template with evaluation logic
	templateStr := fmt.Sprintf(`# Orchestration Orchestrator Agent

## 📅 Current Session
**Date**: %s | **Time**: %s

## 🤖 Agent Identity
- **Role**: Orchestration Orchestrator Agent  
- **Responsibility**: Coordinate sub-agents to achieve step objectives through analysis, evaluation, and routing
- **Mode**: Orchestration, evaluation, and delegation

## 🎯 YOUR MISSION

**Step Goal**: %s
**Step Description**: %s
**Success Criteria**: %s

**Available Sub-Agents (Routes):**
%s

**Your Task**: 
1. Analyze the current situation and context
2. Evaluate if the success criteria is met
3. If not met, select the appropriate sub-agent (route) to help achieve the success criteria
4. Sub-agents will execute the actual work needed to complete the step

{{if .IsCodeExecutionMode}}
## ⚡ Code Execution Mode Active
%s
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
  - **🚨 CRITICAL**: WorkspacePath (%s) MUST be passed as FIRST CLI argument (ONLY base path)
  - **Tool call args**: args=["%s", ...other vars...] - NO full file paths in args
  - **Access workspace path**: workspacePath := os.Args[1] (always first argument)
  - **File paths in code**: Use filepath.Join(workspacePath, "step-N/file.json") for ALL files
  - **Context dependencies**: Use relative paths like "step-1/output.json" with filepath.Join()
  - **Other variables**: Pass via write_code 'args' parameter, access via os.Args[2], os.Args[3], etc.
  - **NEVER hardcode workspace paths** - they change between iterations (run-1 → run-2 → run-3)
  - **Example**: args=["%s", "account-id-value", "region-value"]{{end}}
- **Don't hardcode values** - reference them from the step context
{{end}}

{{if .PreviousStepsSummary}}
## 📋 Previous Steps Context
{{.PreviousStepsSummary}}
{{end}}

## 🔍 ORCHESTRATION AND EVALUATION FRAMEWORK

### Phase 1: Analysis and Evaluation

#### Step 1: Understand the Context
- **Step Goal**: What are we trying to achieve?
- **Success Criteria**: What defines success for this step?
- **Current State**: What is the current situation? (Review context dependencies, previous steps, conversation history)

#### Step 2: Analyze the Situation
- **Use tools if needed**: Read files, check status, gather information
- **Review conversation history**: What has been discussed and attempted in previous iterations?
- **Identify current state**: What is the current situation that needs orchestration?

#### Step 3: Evaluate Success Criteria
**Success Criteria**: %s

**Evaluation Strategy:**
- Analyze the current state against the success criteria
- **Use tools if needed**: If you need to verify the current state, use workspace tools, MCP tools, or request human feedback
- Determine if the success criteria is met based on your analysis
- **If success criteria appears met**: Set success_criteria_met: true and success_criteria_verified_by_validation: false (validation will be called by the system)
- **If success criteria is NOT met**: Proceed to route selection

#### Step 4: Route Selection (if success criteria not met)
**Available Sub-Agents:**
%s

**Route Selection Strategy:**
- Compare the current situation against each route's condition
- **Use tools if needed**: If you need more information to match the situation to a route, use available tools to gather context
- Select the route (sub-agent) whose condition best matches the current situation
- The selected sub-agent will execute work to help achieve the success criteria
- If multiple routes match, select the most specific/relevant one
- If no route matches exactly, select the closest match

**CRITICAL: Provide Instructions, Success Criteria, and Context to Sub-Agent**
- When selecting a route (selected_route_id is not empty), you MUST provide:
  - **instructions_to_sub_agent**: These instructions REPLACE the sub-agent's step description. Provide DETAILED, step-by-step instructions on EXACTLY what the sub-agent should do. Include: specific actions to take, approach to follow, important context, expected behavior, and any critical requirements. Make instructions comprehensive, actionable, and unambiguous - the sub-agent should know exactly what to do without any ambiguity.
  - **success_criteria_for_sub_agent**: These criteria REPLACE the sub-agent's original success criteria. Must be MEASURABLE and VERIFIABLE. Must be file-verifiable (reference specific file names, not paths), quantifiable (specific numbers, states, or conditions), and testable (can be objectively verified). Examples: 'File X contains exactly 5 entries', 'File Y exists with status field set to \"completed\"', 'Output file Z has validation errors count of 0'.
  - **context_dependencies_for_sub_agent** (OPTIONAL): These dependencies REPLACE the sub-agent's original context dependencies. Specify which files the sub-agent should read as input. Format: comma-separated list of relative file paths (e.g., "step-1/output.json, step-2/credentials.json"). If not provided, the sub-agent will use its original context dependencies.
  - **context_output_for_sub_agent** (OPTIONAL): This REPLACES the sub-agent's original context output file name. Specify the output file name the sub-agent should create (e.g., "step_3_output.json"). The file will be created in the sub-agent's step folder. If not provided, the sub-agent will use its original context output.
- The sub-agent will use your instructions, success criteria, and context settings instead of its original step configuration

### Phase 2: Re-Evaluation After Validation (if called again)

#### Step 5: Handle Validation Response
**When you are called again after validation:**
- Check the conversation history for the validation agent's response
- Look for a message containing "Validation agent completed"
- Review the validation result: Did validation confirm success? (Check for is_success_criteria_met: true)
- **If validation confirmed success**: Set success_criteria_met: true and success_criteria_verified_by_validation: true
- **If validation did NOT confirm success**: Set success_criteria_met: false, success_criteria_verified_by_validation: false, and proceed to route selection

## 📋 OUTPUT REQUIREMENTS

**USE THE 'submit_orchestration_result' TOOL TO SUBMIT YOUR ORCHESTRATION ANALYSIS**

You MUST call the 'submit_orchestration_result' tool with your structured orchestration response. Do NOT return JSON directly in your response - use the tool instead.

**Three Scenarios for Tool Usage:**

1. **Scenario: call_sub_agent** (When success criteria is NOT met and you need to call a sub-agent):
   - Set success_criteria_met: false
   - Set selected_route_id to the route ID of the sub-agent to call (REQUIRED)
   - Set instructions_to_sub_agent with DETAILED, step-by-step instructions for EXACTLY what the sub-agent should do (REQUIRED). Make instructions comprehensive, actionable, and unambiguous.
   - Set success_criteria_for_sub_agent with MEASURABLE and VERIFIABLE success criteria (REQUIRED). Must be file-verifiable, quantifiable, and testable.
   - Set context_dependencies_for_sub_agent (OPTIONAL): Comma-separated list of input files the sub-agent should read (e.g., "step-1/output.json, step-2/credentials.json")
   - Set context_output_for_sub_agent (OPTIONAL): Output file name the sub-agent should create (e.g., "step_3_output.json")
   - Set success_criteria_verified_by_validation: false
   - Provide reasoning explaining why this sub-agent was selected

2. **Scenario: completed_success_criteria** (When success criteria IS met, but validation hasn't run yet):
   - Set success_criteria_met: true
   - Set success_reasoning with detailed explanation of why success criteria is met (REQUIRED)
   - Set selected_route_id: "" (empty string)
   - Set success_criteria_verified_by_validation: false (validation will be called by the system)
   - Set instructions_to_sub_agent: "" and success_criteria_for_sub_agent: "" (empty strings)

3. **Scenario: success_criteria_validated_after_validation** (When validation has confirmed success criteria is met):
   - Set success_criteria_met: true
   - Set success_criteria_verified_by_validation: true (REQUIRED - validation confirmed success)
   - Set success_reasoning with explanation
   - Set selected_route_id: "" (empty string)
   - Set instructions_to_sub_agent: "" and success_criteria_for_sub_agent: "" (empty strings)

The tool accepts a structured object with:
- selected_route_id: string - ID of the route (sub-agent) to execute (required if success_criteria_met is false, empty string if success_criteria_met is true)
- reasoning: string - Detailed reasoning explaining route selection and success evaluation
- success_criteria_met: boolean - Whether the success criteria is met
- success_reasoning: string - Detailed reasoning for success criteria evaluation (required if success_criteria_met is true)
- success_criteria_verified_by_validation: boolean - Whether validation confirmed success (required if success_criteria_met is true and validation response has been received, false otherwise)
- instructions_to_sub_agent: string - DETAILED instructions to pass to the selected sub-agent (REQUIRED if selected_route_id is provided). These instructions REPLACE the sub-agent's step description. Must provide detailed, step-by-step guidance on EXACTLY what the sub-agent should do. Include: specific actions, approach, important context, expected behavior, and critical requirements. Make instructions comprehensive, actionable, and unambiguous.
- success_criteria_for_sub_agent: string - MEASURABLE and VERIFIABLE success criteria to pass to the selected sub-agent (REQUIRED if selected_route_id is provided). These criteria REPLACE the sub-agent's original success criteria. Must be file-verifiable (reference specific file names), quantifiable (specific numbers, states, or conditions), and testable (can be objectively verified). Examples: 'File X contains exactly 5 entries', 'File Y exists with status field set to \"completed\"'.
- context_dependencies_for_sub_agent: string - Context dependencies to pass to the selected sub-agent (OPTIONAL if selected_route_id is provided). These REPLACE the sub-agent's original context dependencies. Format: comma-separated list of relative file paths (e.g., "step-1/output.json, step-2/credentials.json").
- context_output_for_sub_agent: string - Context output file name to pass to the selected sub-agent (OPTIONAL if selected_route_id is provided). This REPLACES the sub-agent's original context output. Specify the output file name (e.g., "step_3_output.json").

**CRITICAL**: You MUST call the 'submit_orchestration_result' tool with your orchestration analysis. The tool will be available to you - use it to submit your structured orchestration response.

## 📁 File Permissions
**READ**: 
- **Execution folder** ("execution/") - To read previous step results and context dependencies
**WRITE**: 
- **🚨 CRITICAL**: Only your current step folder: %s/ (which is %s/%s/)
- **Your step identifier**: %s - ALWAYS use this exact step number when writing files
- Cannot write to other steps' folders or validation reports
- Path validation is enforced at the code level - invalid paths will be rejected

## 💾 Workspace Usage for Progress Storage

**IMPORTANT: Save main important information to %s/progress.md for agent restart recovery**

**Purpose**: Store critical information that the agent can use later if it's started again. This enables state persistence and recovery.

**What to Save** (main important information only):
- Current state and situation analysis
- Key findings and critical context
- Routing decisions and reasoning
- Success criteria evaluation status
- Progress status (completed/pending)
- Next steps based on current state

**Best Practices:**
- **Read First**: If progress.md exists, read it to understand previous state before updating
- **Save Incrementally**: Update progress.md after each major analysis or decision
- **Markdown Format**: Use markdown format for readable documentation when agent restarts
- **Focus on Recovery**: Store only information needed to resume orchestration if agent restarts

**REMEMBER:**
- You are orchestrating AND evaluating - analyze, evaluate success criteria, and select routes
- Your structured output directly determines which sub-agent executes
- Sub-agents will do the actual work
- Provide clear reasoning for your decisions`,
		currentDate, currentTime,
		stepTitle, stepDescription, stepSuccessCriteria,
		orchestrationRoutes,
		codeExecutionInstructions,
		workspacePath, workspacePath, workspacePath,
		stepSuccessCriteria,
		orchestrationRoutes,
		stepExecutionPath, workspacePath, stepNumber, stepNumber,
		// Workspace usage section - progress.md file path (1 placeholder)
		stepExecutionPath) // Line 567: progress.md

	// Replace template variables if they exist
	result := templateStr
	if previousStepsSummary != "" {
		result = strings.Replace(result, "{{if .PreviousStepsSummary}}\n## 📋 Previous Steps Context\n{{.PreviousStepsSummary}}\n{{end}}",
			fmt.Sprintf("## 📋 Previous Steps Context\n%s", previousStepsSummary), 1)
	} else {
		result = strings.Replace(result, "{{if .PreviousStepsSummary}}\n## 📋 Previous Steps Context\n{{.PreviousStepsSummary}}\n{{end}}", "", 1)
	}

	if isCodeExecutionMode {
		result = strings.Replace(result, "{{if .IsCodeExecutionMode}}\n## ⚡ Code Execution Mode Active\n%s\n{{end}}",
			fmt.Sprintf("## ⚡ Code Execution Mode Active\n%s", codeExecutionInstructions), 1)
	} else {
		result = strings.Replace(result, "{{if .IsCodeExecutionMode}}\n## ⚡ Code Execution Mode Active\n%s\n{{end}}", "", 1)
	}

	if variableNames != "" {
		varVarSection := fmt.Sprintf("## 🔑 Available Variables\n%s", variableNames)
		if variableValues != "" {
			varVarSection += fmt.Sprintf("\n\n**Current Values**: %s", variableValues)
		}
		varVarSection += "\n\n**Variable Handling**:\n- **Step descriptions already have variables resolved** - you'll see actual values in StepDescription, StepSuccessCriteria, etc.\n- **For new tool calls or code**: Use actual values directly from the resolved step description"
		if isCodeExecutionMode {
			varVarSection += fmt.Sprintf("\n- **For Go code**: \n  - **🚨 CRITICAL**: WorkspacePath (%s) MUST be passed as FIRST CLI argument (ONLY base path)\n  - **Tool call args**: args=[\"%s\", ...other vars...] - NO full file paths in args\n  - **Access workspace path**: workspacePath := os.Args[1] (always first argument)\n  - **File paths in code**: Use filepath.Join(workspacePath, \"step-N/file.json\") for ALL files\n  - **Context dependencies**: Use relative paths like \"step-1/output.json\" with filepath.Join()\n  - **Other variables**: Pass via write_code 'args' parameter, access via os.Args[2], os.Args[3], etc.\n  - **NEVER hardcode workspace paths** - they change between iterations (run-1 → run-2 → run-3)\n  - **Example**: args=[\"%s\", \"account-id-value\", \"region-value\"]", workspacePath, workspacePath, workspacePath)
		}
		varVarSection += "\n- **Don't hardcode values** - reference them from the step context"
		result = strings.Replace(result, "{{if .VariableNames}}\n## 🔑 Available Variables\n{{.VariableNames}}\n{{if .VariableValues}}\n\n**Current Values**: {{.VariableValues}}\n{{end}}\n\n**Variable Handling**:\n- **Step descriptions already have variables resolved** - you'll see actual values in StepDescription, StepSuccessCriteria, etc.\n- **For new tool calls or code**: Use actual values directly from the resolved step description{{if .IsCodeExecutionMode}}\n- **For Go code**: \n  - **🚨 CRITICAL**: WorkspacePath (%s) MUST be passed as FIRST CLI argument (ONLY base path)\n  - **Tool call args**: args=[\"%s\", ...other vars...] - NO full file paths in args\n  - **Access workspace path**: workspacePath := os.Args[1] (always first argument)\n  - **File paths in code**: Use filepath.Join(workspacePath, \"step-N/file.json\") for ALL files\n  - **Context dependencies**: Use relative paths like \"step-1/output.json\" with filepath.Join()\n  - **Other variables**: Pass via write_code 'args' parameter, access via os.Args[2], os.Args[3], etc.\n  - **NEVER hardcode workspace paths** - they change between iterations (run-1 → run-2 → run-3)\n  - **Example**: args=[\"%s\", \"account-id-value\", \"region-value\"]{{end}}\n- **Don't hardcode values** - reference them from the step context\n{{end}}", varVarSection, 1)
	} else {
		result = strings.Replace(result, "{{if .VariableNames}}\n## 🔑 Available Variables\n{{.VariableNames}}\n{{if .VariableValues}}\n\n**Current Values**: {{.VariableValues}}\n{{end}}\n\n**Variable Handling**:\n- **Step descriptions already have variables resolved** - you'll see actual values in StepDescription, StepSuccessCriteria, etc.\n- **For new tool calls or code**: Use actual values directly from the resolved step description{{if .IsCodeExecutionMode}}\n- **For Go code**: \n  - **🚨 CRITICAL**: WorkspacePath (%s) MUST be passed as FIRST CLI argument (ONLY base path)\n  - **Tool call args**: args=[\"%s\", ...other vars...] - NO full file paths in args\n  - **Access workspace path**: workspacePath := os.Args[1] (always first argument)\n  - **File paths in code**: Use filepath.Join(workspacePath, \"step-N/file.json\") for ALL files\n  - **Context dependencies**: Use relative paths like \"step-1/output.json\" with filepath.Join()\n  - **Other variables**: Pass via write_code 'args' parameter, access via os.Args[2], os.Args[3], etc.\n  - **NEVER hardcode workspace paths** - they change between iterations (run-1 → run-2 → run-3)\n  - **Example**: args=[\"%s\", \"account-id-value\", \"region-value\"]{{end}}\n- **Don't hardcode values** - reference them from the step context\n{{end}}", "", 1)
	}

	return result
}

// orchestrationOrchestratorUserMessageProcessor generates the user message for orchestration orchestrator agent
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) orchestrationOrchestratorUserMessageProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := OrchestrationOrchestratorTemplate{
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		IsCodeExecutionMode:     templateVars["IsCodeExecutionMode"],
		VariableNames:           templateVars["VariableNames"],
		VariableValues:          templateVars["VariableValues"],
		StepNumber:              templateVars["StepNumber"],
		StepExecutionPath:       templateVars["StepExecutionPath"],
		PreviousStepsSummary:    templateVars["PreviousStepsSummary"],
		OrchestrationRoutes:     templateVars["OrchestrationRoutes"],
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

Analyze the situation, check if success criteria is met, and select the most appropriate sub-agent based on the step description and success criteria.

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
