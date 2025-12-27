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
	userMessage := hctpooa.orchestrationOrchestratorUserMessageProcessor(templateVars, conversationHistory)

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
				"description": "ID of the route (sub-agent) to execute from the available orchestration_routes. REQUIRED if success_criteria_met is false (you MUST always delegate to a sub-agent when success criteria is not met). Empty string only if success_criteria_met is true. If an \"end\" route exists in orchestration_routes, you can select it (route_id: \"end\") to immediately terminate the entire workflow when you determine the objective is complete."
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
- **Responsibility**: Coordinate and delegate work to sub-agents based on the current situation
- **Mode**: Orchestration - evaluate situation and delegate to appropriate sub-agents

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

**Available Sub-Agents (Routes):**
%s

**Your Task**: 
1. **EVALUATE the situation** - Use your tools to understand the current state and analyze what needs to be done
   - **Use your tools actively**: Read files, check status, gather information needed to understand the situation
   - **Analyze context**: Review context dependencies, previous steps, and current state
2. **EVALUATE against success criteria** - Use your tools to test and verify if the success criteria is met
3. **If success criteria is met**: Report success (set success_criteria_met: true, selected_route_id: "")
4. **If success criteria is NOT met**: 
   - **Always delegate to a sub-agent** - Select the appropriate sub-agent route based on the current situation
   - **End workflow** if an "end" route exists and you determine the objective is complete (set selected_route_id: "end")

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

#### Step 3: Evaluate Success Criteria and Delegate

**Evaluation and Delegation Strategy:**
1. **Evaluate success criteria using your tools**
   - **USE YOUR TOOLS** to verify the current state against the success criteria:
     - Read output files to check if they exist and contain expected content
     - Use workspace tools to validate file contents, check file existence, count entries, verify states, etc.
     - Use MCP tools to query external systems if success criteria involves external state
     - Request human feedback if you need clarification on ambiguous success criteria
   - **Don't guess**: Use tools to gather concrete evidence before evaluating success
   - Analyze the current state against the success criteria based on tool-gathered information

2. **Decision Point:**
   - **If success criteria is met** (based on tool verification): Set success_criteria_met: true, selected_route_id: "" (validation will be called by the system separately)
   - **If success criteria is NOT met**: 
     - **Always delegate to a sub-agent** - Select the appropriate sub-agent route based on the current situation
     - **End workflow** if an "end" route exists and you determine the objective is complete (set selected_route_id: "end")

#### Step 4: Route Selection Framework

**🚨 CRITICAL DECISION: Which sub-agent route should be selected?**

**Available Sub-Agents (Routes):**
%s

**Route Selection Strategy:**
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

## 📋 OUTPUT REQUIREMENTS

**USE THE 'submit_orchestration_result' TOOL TO SUBMIT YOUR ORCHESTRATION ANALYSIS**

You MUST call the 'submit_orchestration_result' tool with your structured orchestration response. Do NOT return JSON directly in your response - use the tool instead.

**Two Scenarios for Tool Usage:**

1. **Scenario: success_criteria_met** (When success criteria IS met):
   - Set success_criteria_met: true
   - Set success_reasoning with detailed explanation of why success criteria is met (REQUIRED)
   - Set selected_route_id: "" (empty string - no sub-agent needed)
   - Set instructions_to_sub_agent: "" and success_criteria_for_sub_agent: "" (empty strings)
   - Validation will be called by the system separately

2. **Scenario: delegate_to_sub_agent** (When success criteria is NOT met - you MUST delegate to a sub-agent):
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
- selected_route_id: string - ID of the route (sub-agent) to execute. REQUIRED if success_criteria_met is false (you MUST delegate to a sub-agent). Empty string only if success_criteria_met is true. Can also be "end" to terminate workflow.
- success_criteria_met: boolean - Whether the success criteria is met
- success_reasoning: string - Detailed reasoning for success criteria evaluation. REQUIRED. Explain: (1) why success criteria is/isn't met, (2) what the current state is, (3) what needs to be done (if not met)
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
- **Knowledgebase folder** ("execution/knowledgebase/") - Files that are NEVER deleted during cleanup
  - **What this means**: When execution folders are cleaned (on re-execution, fresh start, or resume), files in knowledgebase/ remain untouched
  - **Location**: %s/knowledgebase/your-file.json
  - **Use for**: Templates, reference data, configurations, cached data that multiple steps need
**WRITE**: 
- **🚨 CRITICAL**: Only your current step folder: %s/ (which is %s/%s/)
- **Your step identifier**: %s - ALWAYS use this exact step number when writing files
- **Knowledgebase folder** ("execution/knowledgebase/") - For files that should NEVER be deleted
  - **⚠️ IMPORTANT**: Files in step folders (step-1/, step-2/, etc.) get DELETED when:
    - Step is re-executed
    - Workflow starts from beginning
    - Step is resumed from a later point
  - **Files in knowledgebase/ are SAFE** - they are never deleted by cleanup operations
  - **Path**: %s/knowledgebase/your-file.json
  - **When to use**: Store files that should survive cleanup and be available to all steps
- Cannot write to other steps' folders or validation reports
- Path validation is enforced at the code level - invalid paths will be rejected

## 🗑️ Understanding File Cleanup Behavior

**What gets DELETED during cleanup:**
- ✅ All files in execution/step-{N}/ folders (when step is re-executed or workflow restarts)
- ✅ All files in execution/step-{N}-{branch}/ folders
- ✅ All files in execution/step-{N}-decision/ folders

**What STAYS SAFE (never deleted):**
- ✅ All files in execution/knowledgebase/ folder
- ✅ Files in learnings/ folder
- ✅ Files in planning/ folder

**When to use each location:**

| File Type | Store In | Reason |
|-----------|----------|--------|
| Step output (context_output) | execution/step-{N}/ | Step-specific, will be recreated |
| Email template (used by multiple steps) | execution/knowledgebase/ | Shared resource, should persist |
| API configuration | execution/knowledgebase/ | Needed across runs |
| Step execution results | execution/step-{N}/ | Step-specific, temporary |
| Reference data (lookup tables) | execution/knowledgebase/ | Reusable across runs |
| Cached API responses | execution/knowledgebase/ | Avoid re-fetching |

## 💾 Progress Storage

**Save important information to %s/progress.md for agent restart recovery**

**What to Save:**
- Current state and situation analysis
- Routes considered and which one was selected (with reasoning)
- Success criteria evaluation status
- Previous iterations and what was learned
- Next steps based on current state

**Best Practices:**
- Read progress.md first if it exists to understand previous state
- Update after each major decision or iteration
- Use markdown format for readability

**REMEMBER:**
- Your role is to evaluate the situation and delegate to appropriate sub-agents
- Your structured output determines which sub-agent route to select
- Provide clear reasoning for your routing decisions`,
		currentDate, currentTime,
		orchestrationRoutes,
		workspacePath, workspacePath, workspacePath,
		orchestrationRoutes,
		stepExecutionPath, workspacePath, stepNumber, stepNumber,
		// Knowledgebase path placeholders (2 placeholders for READ and WRITE sections)
		workspacePath, workspacePath,
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

	// Insert code execution instructions if code execution mode is enabled
	if isCodeExecutionMode && codeExecutionInstructions != "" {
		// Insert after "Your Task" section, before Variables section
		insertPoint := "   - **Delegate to a sub-agent** ONLY if the task is complex, long-running, or requires specialized capabilities (see Decision Framework below)\n\n"
		codeExecutionSection := fmt.Sprintf("## ⚡ Code Execution Mode Active\n%s\n\n", codeExecutionInstructions)
		result = strings.Replace(result, insertPoint, insertPoint+codeExecutionSection, 1)
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
func (hctpooa *HumanControlledTodoPlannerOrchestrationOrchestratorAgent) orchestrationOrchestratorUserMessageProcessor(templateVars map[string]string, conversationHistory []llmtypes.MessageContent) string {
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
	// NOTE: Previous steps summary is in system prompt to avoid duplication
	templateStr := `## 🎯 Step Goal: {{.StepTitle}}

**STEP DESCRIPTION**: {{.StepDescription}}  
**SUCCESS CRITERIA**: {{.StepSuccessCriteria}}

**WORKSPACE**: {{.WorkspacePath}}  
**STEP NUMBER**: {{.StepNumber}} (write all output files to {{.StepExecutionPath}}/)

**🚨 CRITICAL: Your role is ORCHESTRATION - delegate work to sub-agents.**
- Use your tools actively to read files, check status, gather information - evaluate the situation
- Provide structured output indicating which sub-agent route to select

**EVALUATE the situation and delegate to appropriate sub-agents.**

## 📋 Step Details
**Context Dependencies**: {{.StepContextDependencies}}  
**Context Output**: {{.StepContextOutput}}`

	// Parse and execute the template
	tmpl, err := template.New("orchestrationOrchestratorUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing orchestration orchestrator user message template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing orchestration orchestrator user message template: %v", err)
	}

	userMsg := result.String()

	// Check for validation feedback in conversation history
	hasValidationFeedback := false
	var validationMessages []string
	for _, msg := range conversationHistory {
		// Check messages from assistant or human roles
		if msg.Role == llmtypes.ChatMessageTypeAI || msg.Role == llmtypes.ChatMessageTypeHuman {
			content := ""
			// Extract text from message parts
			for _, part := range msg.Parts {
				if textPart, ok := part.(llmtypes.TextContent); ok {
					content += textPart.Text + " "
				}
			}

			// Check for validation feedback indicators
			contentLower := strings.ToLower(content)
			if strings.Contains(contentLower, "validation agent completed") ||
				strings.Contains(contentLower, "validation failed") ||
				strings.Contains(contentLower, "validation result") ||
				strings.Contains(contentLower, "validation error") {
				hasValidationFeedback = true
				validationMessages = append(validationMessages, strings.TrimSpace(content))
			}
		}
	}

	// Add validation feedback section if found
	if hasValidationFeedback {
		validationSection := "\n\n## ⚠️ VALIDATION FEEDBACK\n\n"
		validationSection += "**IMPORTANT**: Validation has failed. Review the validation feedback below and address the issues.\n\n"
		validationSection += "**Validation Feedback from Previous Attempt:**\n"
		for i, msg := range validationMessages {
			if i < 3 { // Limit to last 3 validation messages
				validationSection += fmt.Sprintf("- %s\n", strings.TrimSpace(msg))
			}
		}
		validationSection += "\n**Action Required:**\n"
		validationSection += "1. **Review validation feedback** - Understand what went wrong and what needs to be fixed\n"
		validationSection += "2. **Use your tools to address the issues** - Fix the specific problems identified by validation\n"
		validationSection += "3. **Re-execute the step description** - Incorporate the validation feedback into your work\n"
		validationSection += "4. **Re-evaluate success criteria** - After fixing issues, verify if success criteria is now met\n\n"

		// Insert validation section before "EXECUTE" instruction
		userMsg = strings.Replace(userMsg, "**EXECUTE the step description using your tools.**", validationSection+"**EXECUTE the step description using your tools.**", 1)
	}

	return userMsg
}
