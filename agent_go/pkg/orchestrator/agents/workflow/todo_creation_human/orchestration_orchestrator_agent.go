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
// NOTE: This is a minimal implementation that delegates to ExecuteStructured.
// ExecuteStructured should be used directly for orchestration steps.
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Delegate to ExecuteStructured and convert the result to string format
	response, updatedHistory, err := hctpooa.ExecuteStructured(ctx, templateVars, conversationHistory)
	if err != nil {
		return "", nil, err
	}

	// Convert structured response to string format for backward compatibility
	result := fmt.Sprintf("Success Criteria Met: %t\nSelected Route: %s",
		response.SuccessCriteriaMet, response.SelectedRouteID)
	if response.SuccessCriteriaMet && response.SuccessReasoning != "" {
		result += fmt.Sprintf("\nSuccess Reasoning: %s", response.SuccessReasoning)
	}

	return result, updatedHistory, nil
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
				"description": "ID of the route (sub-agent) to execute from the available orchestration_routes. REQUIRED if success_criteria_met is false AND you need to delegate to a sub-agent. Empty string if: (1) success_criteria_met is true, OR (2) you are doing the work yourself (even if success_criteria_met is false). Only provide a route_id if the task is complex/long-running and requires delegation to a specialized sub-agent. If an \"end\" route exists in orchestration_routes, you can select it (route_id: \"end\") to immediately terminate the entire workflow when you determine the objective is complete."
			},
			"success_criteria_met": {
				"type": "boolean",
				"description": "Whether the orchestration step's success criteria is met"
			},
			"success_reasoning": {
				"type": "string",
				"description": "Detailed reasoning for success criteria evaluation. Required if success_criteria_met is true."
			},
			"instructions_to_sub_agent": {
				"type": "string",
				"description": "VERY DETAILED and PRECISE instructions to pass to the selected sub-agent. REQUIRED if selected_route_id is provided (not empty). Must be extremely specific - include exact actions, specific file names, precise steps, exact commands. Leave no ambiguity - the sub-agent must know EXACTLY what to do without any guessing. Include: specific actions to take, exact approach to follow, important context, expected behavior, exact file paths/names, precise requirements, any edge cases. Format: Use numbered steps, clear bullet points, explicit commands. Examples of good instructions: '1. Read file step-1/credentials.json. 2. Extract api_key field. 3. Create step-2/api_config.json with structure: {\"key\": \"<extracted_key>\"}. 4. Validate JSON before writing.' Examples of bad instructions: 'Process credentials' or 'Create config' (too vague). Make instructions comprehensive, actionable, unambiguous, and PRECISE."
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
		"required": ["success_criteria_met", "success_reasoning"]
	}`

	// Define tool name and description for structured output via tool calls
	// This single tool handles two scenarios:
	// 1. call_sub_agent: When calling a sub-agent (provide selected_route_id, instructions_to_sub_agent, success_criteria_for_sub_agent)
	// 2. completed_success_criteria: When success criteria is met (provide success_criteria_met: true, success_reasoning)
	toolName := "submit_orchestration_result"
	toolDescription := `Submit the orchestration result. This tool handles two scenarios:
1. **call_sub_agent**: When calling a sub-agent - provide selected_route_id (required), instructions_to_sub_agent (required), success_criteria_for_sub_agent (required), context_dependencies_for_sub_agent (optional), context_output_for_sub_agent (optional), success_criteria_met: false
2. **completed_success_criteria**: When success criteria is met - provide success_criteria_met: true, success_reasoning (required), selected_route_id: ""`

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
- **Responsibility**: Execute step objectives directly when possible, or coordinate sub-agents for complex tasks
- **Mode**: Execution-first orchestration - do the work yourself when simple, delegate when complex

## 🛠️ AVAILABLE TOOLS

**You have access to MCP tools and workspace tools to help you:**
- **MCP Tools**: Tools from configured MCP servers (e.g., file operations, API calls, database queries, etc.)
- **Workspace Tools**: Custom tools for workspace operations (file reading, writing, validation, etc.)
- **Human Tools**: Tools to request human feedback when needed

**USE YOUR TOOLS ACTIVELY:**
- **Read files** to understand current state and context dependencies
- **Check status** of previous steps and outputs
- **Gather information** needed to evaluate success criteria
- **Analyze data** to determine which route (sub-agent) to select
- **Verify conditions** before making routing decisions
- **Request human feedback** if you need clarification or approval

**CRITICAL**: Don't guess or assume - use your tools to gather concrete information before making routing decisions or evaluating success criteria.

## 🎯 YOUR MISSION

**Step Goal**: %s
**Step Description**: %s
**Success Criteria**: %s

**Available Sub-Agents (Routes):**
%s

**Your Task**: 
1. **EXECUTE the step description** - Use your tools to perform the work described in the step description
   - **DO THE WORK YOURSELF** if the task is simple, straightforward, or quick (e.g., read a file, create a simple config, validate data, make a simple API call)
   - **Use your tools actively**: Read files, write files, call APIs, execute code, validate conditions - whatever is needed to complete the task
2. **EVALUATE against success criteria** - Use your tools to test and verify if the success criteria is met
3. **If success criteria is met**: Report success (set success_criteria_met: true, selected_route_id: "")
4. **If success criteria is NOT met**: 
   - **Continue working yourself** if the task is still simple/straightforward and you can complete it with your tools
   - **Delegate to a sub-agent** ONLY if the task is complex, long-running, or requires specialized capabilities that you don't have

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
- **USE YOUR TOOLS**: Actively use MCP tools and workspace tools to read files, check status, gather information
  - Read context dependency files to understand inputs
  - Check previous step outputs to understand current state
  - Use workspace tools to examine file contents, check existence, validate data
  - Use MCP tools to query external systems, APIs, or databases if needed
- **Review conversation history**: What has been discussed and attempted in previous iterations?
- **Check for sub-agent completion**: If a sub-agent has completed work (you'll see "Sub-agent completed" in conversation history), review their output to understand what was accomplished
- **Identify current state**: What is the current situation that needs orchestration?

#### Step 3: Execute the Task and Evaluate Success Criteria
**Success Criteria**: %s

**Execution and Evaluation Strategy:**
1. **FIRST: Try to complete the work yourself using your tools**
   - **Simple tasks you should do yourself**: Reading files, writing simple configs, validating data, making simple API calls, creating basic files, checking conditions, transforming simple data
   - **Use your tools actively**: Read files, write files, call APIs, execute code, validate conditions - perform the actual work described in the step description
   - **Work incrementally**: Do the work, then check if success criteria is met

2. **THEN: Evaluate success criteria using your tools**
   - **USE YOUR TOOLS** to verify the current state against the success criteria:
     - Read output files to check if they exist and contain expected content
     - Use workspace tools to validate file contents, check file existence, count entries, verify states, etc.
     - Use MCP tools to query external systems if success criteria involves external state
     - Request human feedback if you need clarification on ambiguous success criteria
   - **Don't guess**: Use tools to gather concrete evidence before evaluating success
   - Analyze the current state against the success criteria based on tool-gathered information

3. **Decision Point:**
   - **If success criteria is met** (based on tool verification): Set success_criteria_met: true, selected_route_id: "" (validation will be called by the system separately)
   - **If success criteria is NOT met**: 
     - **Continue working yourself** if the task is still simple and you can complete it with your tools
     - **Delegate to sub-agent** ONLY if the task is complex, long-running, or requires specialized capabilities
     - **End workflow** if an "end" route exists in orchestration_routes and you determine the objective is complete (set selected_route_id: "end")

#### Step 4: Decision Framework - Do It Yourself vs Delegate

**🚨 CRITICAL DECISION: Should you do the work yourself or delegate to a sub-agent?**

**DO THE WORK YOURSELF if:**
- ✅ The task is **simple and straightforward** (e.g., read a file, create a config, validate data, make a simple API call)
- ✅ The task can be completed **quickly** with your available tools
- ✅ The task doesn't require **specialized knowledge or complex multi-step processes**
- ✅ You have **all the tools and information** needed to complete it
- ✅ The work is **not long-running** or resource-intensive

**DELEGATE TO SUB-AGENT if:**
- ❌ The task is **complex** and requires multiple coordinated steps
- ❌ The task is **long-running** and would consume too many turns/resources
- ❌ The task requires **specialized capabilities** that a sub-agent is designed for
- ❌ The task matches a **specific sub-agent's condition** and that sub-agent is better suited
- ❌ You've tried to do it yourself but the task is too complex or requires specialized knowledge

**Available Sub-Agents (Routes):**
%s

**Route Selection Strategy (ONLY if delegating):**
- **USE YOUR TOOLS** to gather information needed to match the situation to routes:
  - Read files to check current state, status, or conditions
  - Use workspace tools to examine data, check file contents, validate conditions
  - Use MCP tools to query external systems if route conditions depend on external state
  - Request human feedback if route conditions are ambiguous or unclear
- Compare the current situation (gathered via tools) against each route's condition
- **Don't guess**: Use tools to verify conditions before selecting a route
- Select the route (sub-agent) whose condition best matches the current situation (based on tool-gathered evidence)
- The selected sub-agent will execute work to help achieve the success criteria
- If multiple routes match, select the most specific/relevant one based on tool-verified conditions
- If no route matches exactly, select the closest match based on tool-gathered information

**CRITICAL: Provide VERY DETAILED and PRECISE Instructions, Success Criteria, and Context to Sub-Agent**
- When selecting a route (selected_route_id is not empty), you MUST provide:
  - **instructions_to_sub_agent**: VERY DETAILED and PRECISE step-by-step instructions on EXACTLY what the sub-agent should do for this execution
    - **🚨 CRITICAL**: Provide VERY DETAILED and PRECISE step-by-step instructions on EXACTLY what the sub-agent should do
    - **Be extremely specific**: Include exact actions, specific file names, precise steps, exact commands or operations
    - **Leave no ambiguity**: The sub-agent should know EXACTLY what to do without any guessing or interpretation
    - **Include**: Specific actions to take, exact approach to follow, important context, expected behavior, exact file paths/names, precise requirements, any edge cases to handle
    - **Format**: Use numbered steps, clear bullet points, and explicit commands
    - **Examples of good instructions**: "1. Read file 'step-1/credentials.json' from the execution folder. 2. Extract the 'api_key' field. 3. Create a new file 'step-2/api_config.json' with structure: {\"key\": \"<extracted_key>\", \"endpoint\": \"https://api.example.com\"}. 4. Validate the JSON structure before writing."
    - **Examples of bad instructions**: "Process the credentials" or "Create the config file" (too vague, no specifics)
    - Make instructions comprehensive, actionable, unambiguous, and PRECISE - the sub-agent should have zero doubt about what to do
  - **success_criteria_for_sub_agent**: These criteria REPLACE the sub-agent's original success criteria. Must be MEASURABLE and VERIFIABLE. Must be file-verifiable (reference specific file names, not paths), quantifiable (specific numbers, states, or conditions), and testable (can be objectively verified). Examples: 'File X contains exactly 5 entries', 'File Y exists with status field set to \"completed\"', 'Output file Z has validation errors count of 0'.
  - **context_dependencies_for_sub_agent** (OPTIONAL): These dependencies REPLACE the sub-agent's original context dependencies. Specify which files the sub-agent should read as input. Format: comma-separated list of relative file paths (e.g., "step-1/output.json, step-2/credentials.json"). If not provided, the sub-agent will use its original context dependencies.
  - **context_output_for_sub_agent** (OPTIONAL): This REPLACES the sub-agent's original context output file name. Specify the output file name the sub-agent should create (e.g., "step_3_output.json"). The file will be created in the sub-agent's step folder. If not provided, the sub-agent will use its original context output.
- The sub-agent will use your instructions, success criteria, and context settings instead of its original step configuration

### Phase 2: Handling Validation Feedback (if validation feedback is in conversation history)

**IMPORTANT**: Validation runs separately after you complete. If you see validation feedback in the conversation history, it means validation failed and you are being restarted from the beginning.

**When you see validation feedback in conversation history:**
1. **Review validation feedback** - Look for messages containing "Validation agent completed"
   - Review the validation result and any feedback provided (errors, issues, missing elements)
   - Understand what went wrong and what needs to be fixed

2. **Use validation feedback to improve your work**:
   - Review validation feedback carefully (errors, missing elements, quality issues)
   - Use your tools to address the issues identified by validation
   - Re-execute the step description incorporating the validation feedback
   - Make sure to fix the specific issues mentioned in the validation feedback

3. **Re-evaluate success criteria** - After addressing validation feedback, check if success criteria is now met
   - **If success criteria is met**: Set success_criteria_met: true (validation will run again)
   - **If success criteria is NOT met**: Set success_criteria_met: false and select a route (sub-agent) to help complete the work

## 📋 OUTPUT REQUIREMENTS

**USE THE 'submit_orchestration_result' TOOL TO SUBMIT YOUR ORCHESTRATION ANALYSIS**

You MUST call the 'submit_orchestration_result' tool with your structured orchestration response. Do NOT return JSON directly in your response - use the tool instead.

**Three Scenarios for Tool Usage:**

1. **Scenario: work_completed_successfully** (When you completed the work yourself and success criteria IS met):
   - Set success_criteria_met: true
   - Set success_reasoning with detailed explanation of what work you did and why success criteria is met (REQUIRED)
   - Set selected_route_id: "" (empty string - no sub-agent needed)
   - Set instructions_to_sub_agent: "" and success_criteria_for_sub_agent: "" (empty strings)
   - Validation will be called by the system separately

2. **Scenario: continue_working_myself** (When success criteria is NOT met but you're continuing to work yourself):
   - Set success_criteria_met: false
   - Set selected_route_id: "" (empty string - you're doing the work yourself, no sub-agent needed)
   - Set success_reasoning with explanation of what work you've done so far and what remains (REQUIRED)
   - Set instructions_to_sub_agent: "" and success_criteria_for_sub_agent: "" (empty strings)
   - **Note**: You will be called again in the next iteration to continue working. Use your tools to make progress toward meeting the success criteria.

3. **Scenario: delegate_to_sub_agent** (When success criteria is NOT met and you need to delegate to a sub-agent):
   - Set success_criteria_met: false
   - Set selected_route_id to the route ID of the sub-agent to call (REQUIRED)
   - Set instructions_to_sub_agent with VERY DETAILED and PRECISE step-by-step instructions for EXACTLY what the sub-agent should do (REQUIRED)
     - **🚨 CRITICAL**: Be extremely specific - include exact actions, specific file names, precise steps, exact commands
     - **No ambiguity**: The sub-agent must know EXACTLY what to do without any guessing
     - **Include**: Specific actions, exact approach, important context, expected behavior, exact file paths/names, precise requirements
     - **Format**: Use numbered steps, clear bullet points, explicit commands
     - Make instructions comprehensive, actionable, unambiguous, and PRECISE
   - Set success_criteria_for_sub_agent with MEASURABLE and VERIFIABLE success criteria (REQUIRED). Must be file-verifiable, quantifiable, and testable.
   - Set context_dependencies_for_sub_agent (OPTIONAL): Comma-separated list of input files the sub-agent should read (e.g., "step-1/output.json, step-2/credentials.json")
   - Set context_output_for_sub_agent (OPTIONAL): Output file name the sub-agent should create (e.g., "step_3_output.json")

The tool accepts a structured object with:
- selected_route_id: string - ID of the route (sub-agent) to execute. REQUIRED only if you're delegating to a sub-agent (success_criteria_met is false AND task is complex/long-running). Empty string if: (1) success_criteria_met is true, OR (2) you're doing the work yourself (even if success_criteria_met is false)
- success_criteria_met: boolean - Whether the success criteria is met
- success_reasoning: string - Detailed reasoning for success criteria evaluation. REQUIRED. Explain: (1) what work you did (if you did it yourself), (2) why success criteria is/isn't met, (3) what remains (if not met)
- instructions_to_sub_agent: string - VERY DETAILED and PRECISE instructions to pass to the selected sub-agent (REQUIRED if selected_route_id is provided). Provide step-by-step instructions on EXACTLY what the sub-agent should do for this execution.
  - **🚨 CRITICAL**: Must be extremely specific - include exact actions, specific file names, precise steps, exact commands
  - **No ambiguity**: The sub-agent must know EXACTLY what to do without any guessing or interpretation
  - **Must include**: Specific actions to take, exact approach to follow, important context, expected behavior, exact file paths/names, precise requirements, any edge cases
  - **Format**: Use numbered steps, clear bullet points, explicit commands
  - **Examples of good instructions**: "1. Read file 'step-1/credentials.json'. 2. Extract 'api_key' field. 3. Create 'step-2/api_config.json' with structure: {\"key\": \"<extracted_key>\"}. 4. Validate JSON before writing."
  - **Examples of bad instructions**: "Process credentials" or "Create config" (too vague)
  - Make instructions comprehensive, actionable, unambiguous, and PRECISE
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
- **DO THE WORK YOURSELF** for simple, straightforward tasks - use your tools actively to complete the step description
- **DELEGATE TO SUB-AGENTS** only for complex, long-running, or specialized tasks
- You are both an executor AND an orchestrator - execute when you can, delegate when you must
- Your structured output determines whether you continue working yourself or delegate to a sub-agent
- Provide clear reasoning for your decisions and what work you've done`,
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

**🚨 CRITICAL: Your role is EXECUTION AND ORCHESTRATION.**
- **FIRST**: Try to complete the work yourself using your tools - do simple tasks directly
- **THEN**: Delegate to sub-agents ONLY if the task is complex, long-running, or requires specialized capabilities
- Use your tools actively to read files, write files, call APIs, execute code - perform the actual work
- Provide structured output indicating whether you completed the work yourself or need to delegate

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

**EXECUTE the step description using your tools. If the task is simple, do it yourself. If complex/long-running, delegate to the most appropriate sub-agent.**

{{if eq .IsCodeExecutionMode "true"}}
## 📝 Code Execution Example

**Task**: Execute the step description using your tools. Do the work yourself if simple, or delegate to a sub-agent if complex.

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
    
    // 3. Execute the task and create output
    // Write output files to current step folder (as specified in step description)
    outputPath := filepath.Join(basePath, "{{.StepNumber}}/output.json")
    result := workspace_tools.UpdateWorkspaceFile(workspace_tools.UpdateWorkspaceFileParams{
        Filepath: outputPath,
        Content:  "...", // Actual work output (not just analysis)
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

**STEP 0: 🧠 UNDERSTAND & EXECUTE**
- Understand the step description and what needs to be done
- Identify if the task is simple (do it yourself) or complex (delegate)
- Execute the work using your tools

**ALWAYS (Execution Requirements are PRIMARY):**
1. ✓ **Understand step description** ← What work needs to be done?
2. ✓ **Know success criteria** ← When is the work complete?
3. ✓ Read context dependencies from {{.WorkspacePath}}
4. ✓ **EXECUTE the step description** - Use your tools to do the actual work:
   - Read files, write files, call APIs, execute code, validate data
   - Do simple tasks yourself directly
   - Only delegate if task is complex/long-running
5. ✓ **Evaluate success criteria** - Use your tools to verify if success criteria is met
6. ✓ **Create context output file** at {{.StepExecutionPath}}/{{.StepContextOutput}}
   - **🚨 CRITICAL**: Always write to {{.StepNumber}} folder - use filepath.Join(basePath, "{{.StepNumber}}", filename)
7. ✓ **Provide structured output** - Indicate whether you completed the work yourself or need to delegate

**REMEMBER:**
- **DO THE WORK YOURSELF** for simple tasks - use your tools actively
- **DELEGATE TO SUB-AGENTS** only for complex, long-running, or specialized tasks
- You are both an executor AND an orchestrator - execute when you can, delegate when you must`

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
