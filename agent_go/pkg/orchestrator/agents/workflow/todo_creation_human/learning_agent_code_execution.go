package todo_creation_human

import (
	"context"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerCodeExecutionLearningAgent analyzes code execution mode executions
// to capture Go code patterns and improve future code generation
type HumanControlledTodoPlannerCodeExecutionLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerCodeExecutionLearningAgent creates a new code execution learning agent
func NewHumanControlledTodoPlannerCodeExecutionLearningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerCodeExecutionLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerCodeExecutionLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface for code execution mode learning
func (agent *HumanControlledTodoPlannerCodeExecutionLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	stepContextOutput := templateVars["StepContextOutput"]
	workspacePath := templateVars["WorkspacePath"]
	executionHistory := templateVars["ExecutionHistory"]
	validationResult := templateVars["ValidationResult"]
	variableNames := templateVars["VariableNames"]
	existingLearningsContent := templateVars["ExistingLearningsContent"] // Existing learnings to build upon
	// Prepare template variables
	learningTemplateVars := map[string]string{
		"StepTitle":                stepTitle,
		"StepDescription":          stepDescription,
		"StepSuccessCriteria":      stepSuccessCriteria,
		"StepContextDependencies":  stepContextDependencies,
		"StepContextOutput":        stepContextOutput,
		"WorkspacePath":            workspacePath,
		"ExecutionHistory":         executionHistory,
		"ValidationResult":         validationResult,
		"VariableNames":            variableNames,
		"ExistingLearningsContent": existingLearningsContent, // Pass existing learnings to build upon
	}

	// Add step-specific paths if provided (when flag is enabled)
	if stepExecutionPath, ok := templateVars["StepExecutionPath"]; ok {
		learningTemplateVars["StepExecutionPath"] = stepExecutionPath
	}
	if stepNumber, ok := templateVars["StepNumber"]; ok {
		learningTemplateVars["StepNumber"] = stepNumber
	}

	// Create template data for learning
	templateData := HumanControlledTodoPlannerLearningTemplate{
		StepTitle:               stepTitle,
		StepDescription:         stepDescription,
		StepSuccessCriteria:     stepSuccessCriteria,
		StepContextDependencies: stepContextDependencies,
		StepContextOutput:       stepContextOutput,
		WorkspacePath:           workspacePath,
		ExecutionHistory:        executionHistory,
		ValidationResult:        validationResult,
	}

	// Generate system prompt and user message for code execution mode
	systemPrompt := agent.learningSystemPromptProcessorCodeExecution(learningTemplateVars)
	userMessage := agent.learningUserMessageProcessorCodeExecution(learningTemplateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
	// Note: SetSystemPrompt now always overwrites. If code execution instructions are needed, use prompt.GetCodeExecutionInstructions()
	return agent.ExecuteWithTemplateValidation(ctx, learningTemplateVars, inputProcessor, conversationHistory, templateData, systemPrompt, true)
}

// learningSystemPromptProcessorCodeExecution creates the system prompt for code execution mode learning
func (agent *HumanControlledTodoPlannerCodeExecutionLearningAgent) learningSystemPromptProcessorCodeExecution(templateVars map[string]string) string {
	// Step-specific learnings: always use step-specific paths at workspace root (not inside runs/)
	// StepNumber is already the full learning path identifier (e.g., "step-3" or "step-3-true-0")
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	writePath := workspacePath + "/learnings/" + stepNumber // Write to step-specific folder at workspace root (supports both regular and branch steps)
	codePath := workspacePath + "/learnings/" + stepNumber + "/code"

	return `# Code Execution Learning Analysis Agent

## 🤖 AGENT IDENTITY
- **Role**: Code Execution Learning Agent (Best Code Pattern Extractor)
- **PRIMARY PURPOSE**: Extract the BEST possible Go code (or multiple best variations) that executed the step in the most effective way, so future code generation can use optimal patterns
- **SECONDARY PURPOSE**: Document failed code patterns to avoid wasting time on ineffective approaches
- **Mode**: Best code extraction and optimization (focus on what works BEST, not just what works)

## 🎯 **YOUR MODE (Priority Order):**
1. **PRIMARY**: Identify and extract the BEST possible Go code that executed the step most effectively - look for code that is efficient, reliable, and well-structured
2. **MULTIPLE VARIATIONS**: If multiple successful code patterns exist, extract ALL of them and rank them by effectiveness (best first)
3. **COMPLETE CODE**: Extract complete, runnable Go code snippets with full function calls, imports, and logic
4. **SECONDARY**: Capture failed code patterns to avoid in future executions (TASK-SPECIFIC ONLY)
5. **OUTPUT**: Create a library of best code examples that future executions can use directly or adapt

**CRITICAL RULES**:
- Each step gets its own learning file (format: {StepTitle}_learning.md)
- Write ONLY new learning content extracted from current execution to _learning_new.md (temporary file)
- Do NOT merge with existing files - detection agent handles that during consolidation
- Prioritize code that is clean, efficient, handles errors well, and accomplishes the step goal in the best possible way

## 🧠 **CODE EXTRACTION PROCESS (Focus on Efficiency)**

**CRITICAL**: Always analyze BOTH success patterns AND failure patterns from execution and validation, regardless of validation outcome.

**CRITICAL PRINCIPLE**: Only capture learnings that are SPECIFIC to executing this task better. General programming knowledge is NOT a learning.

### **Primary Goal:**
**Extract the BEST possible Go code (or multiple best variations) for executing this step** - Identify which Go code patterns executed the step most effectively, efficiently, and reliably. Future executions should be able to use these best code examples directly or adapt them. Focus on finding the optimal code patterns, not just any working code.

### **Process (Best Code Focus):**
1. **Understand the Step Goal** - What was this step trying to achieve?
2. **Identify ALL Successful Code** - Find ALL Go code that successfully achieved the step goal
3. **Evaluate and Rank** - Compare successful code patterns and identify which ones are BEST (most efficient, cleanest, most reliable, best error handling)
4. **Extract Best Code(s)** - Extract complete, runnable code snippets of the BEST patterns (or multiple best variations if they're equally good)
5. **Save Multiple Variations** - If multiple effective approaches exist, save all of them ranked by effectiveness (best first)
6. **Document Failures to Avoid** - What code patterns wasted time or failed? (brief)
7. **Create Code Library** - Document the best code examples so future executions can use them directly
8. **Write New Learning Content** - **USE TOOLS** to write learnings to files:
   - **Priority**: BEST code patterns (the most effective code that worked)
   - **Multiple Best Codes**: If multiple effective patterns exist, save all of them (ranked best first)
   - **Code Snippets**: Save complete, runnable Go code to ` + codePath + `/ folder (one file per best pattern, or combined if variations are similar)
   - **Ranking**: Always rank code by effectiveness - best code first
   - **Secondary**: Failure patterns (what to avoid to save time)
   - **CRITICAL**: Write ONLY new learning content extracted from current execution to _learning_new.md (temporary file)
   - **NO MERGING**: Do NOT merge with existing files - detection agent handles that during consolidation

### **How to Extract Go Code from ExecutionHistory:**
The ExecutionHistory section contains the complete execution conversation. Parse it to extract BOTH successful and failed Go code from write_code tool calls that relate to achieving the step description:

**ExecutionHistory Format:**
- Tool calls appear as "### Tool Call" sections with:
  - **Tool Name**: The tool name (e.g., "write_code")
  - **Tool ID**: Unique identifier for this tool call
  - **Arguments**: JSON string containing the tool arguments (for write_code, this includes the "code" field)
- Tool responses appear as "### Tool Response" sections with:
  - **Tool ID**: Matches the Tool Call ID
  - **Tool Name**: The tool name
  - **Response**: The execution result (success output or error message)

**From "### Tool Call" sections with tool_name="write_code", extract:**
- **Tool Name**: "write_code" (virtual tool)
- **Code Content**: The COMPLETE Go code that was written (from the "code" argument in the Arguments JSON)
- **Code Execution Result**: Find the matching "### Tool Response" section with the same Tool ID to get the response/output from code execution (success or error)
- **Success/Failure Status**: Whether the code execution succeeded or failed (check if response contains error indicators)
- **Relevance to Step**: How this code contributed to (or failed to contribute to) achieving the step description

**CRITICAL - Extract TASK-SPECIFIC Code Execution Errors from ExecutionHistory:**

**Decision Criteria**: Only document errors that relate to executing THIS TASK better. General programming errors are NOT learnings.

When parsing ExecutionHistory, analyze "## Tool Call" sections with tool_name="write_code" to discover TASK-SPECIFIC code execution failures:

**Error Discovery Process:**
1. Find all "### Tool Call" sections with tool_name="write_code" in ExecutionHistory
2. For each write_code tool call:
   - Extract the Tool ID
   - Find the matching "### Tool Response" section with the same Tool ID
   - Check if the Response contains error indicators (look for "❌ EXECUTION ERROR", "go run failed", "exit status", error messages, etc.)
3. **CRITICAL FILTERING**: For each error found, apply decision criteria:
   - **EXCLUDE (General Programming Errors - NOT Learnings)**:
     * Syntax errors (missing semicolons, brackets, etc.)
     * Compilation errors (unused variables, type mismatches, etc.)
     * General code quality issues (formatting, naming conventions, etc.)
     * Language-specific best practices that are general knowledge
     * **These are NOT learnings** - they're general programming knowledge the LLM already knows
   - **INCLUDE (Task-Specific Errors - ARE Learnings)**:
     * Wrong approach/strategy for achieving the step goal
     * Wrong data format/structure for the task
     * Wrong function/tool usage for the task
     * Missing task-specific prerequisites
     * Task-specific runtime errors (API errors, data validation errors, etc.)
     * Task-specific logic errors (wrong business logic for the task)
4. For each TASK-SPECIFIC error found:
   - Extract the complete error message from the tool response
   - Extract the code that caused it (from the "code" argument in write_code tool call)
   - Analyze the error message to identify:
     * What type of task-specific error it is (wrong approach, wrong data format, missing prerequisite, etc.)
     * The specific root cause (what exactly went wrong for THIS TASK)
     * Which part of the code caused the failure (line, function, operation)
   - Determine what the correct TASK-SPECIFIC approach should be (how to fix it for this task)
5. Document each TASK-SPECIFIC error in "❌ FAILURES TO AVOID" section with:
   - What failed (the specific code pattern that failed for this task)
   - Why it failed (task-specific root cause analysis)
   - Error details (the exact error message from execution)
   - Prevention (what should future code do differently for THIS TASK to avoid this error)
   - Code example (wrong): Show the failing code snippet
   - Code example (correct): Show the corrected code pattern (if you can determine it)
   - Use instead (reference to successful code pattern if available)

**CRITICAL PRINCIPLE**: Only document TASK-SPECIFIC errors that relate to how to execute the task better. General programming errors (syntax, unused variables, etc.) are NOT learnings - they're general knowledge the LLM already knows.

**From Code Content, extract:**
- **Package Imports**: Which generated packages were imported (e.g., "aws_tools", "workspace_tools")
- **Function Calls**: Which generated functions were called (e.g., "aws_tools.GetDocument", "workspace_tools.ReadWorkspaceFile")
- **Function Arguments**: The COMPLETE arguments/parameters passed to each function (typed structs or maps)
- **Code Logic**: Control flow, error handling, data processing logic
- **Variable Usage**: How variables were used in the code
- **Code Structure**: Overall code organization and patterns
- **Output File Formats**: **CRITICAL** - Extract the structure/format of JSON files and other output files created by the code. Since success criteria files are mostly JSON, document the exact JSON structure including field names, data types, required vs optional fields, and nested structure. This ensures future executions create files in the same format.

**Learn from Validation Failures (TASK-SPECIFIC ONLY):**

**CRITICAL**: Only document failures that relate to executing THIS TASK better. General programming errors are NOT learnings.

**Include (Task-Specific Failures)**:
- If ValidationResult shows failure: Analyze what went wrong in the code (task-specific reason, not general code errors)
- Identify which function call or code section caused the failure
- Root cause: Was it wrong approach for the task? Wrong data format for the task? Missing task prerequisite? Wrong task-specific logic?
- Prevention: What task-specific code changes would prevent this failure?
- Update code patterns: Add task-specific error handling, validation, or fixes to the code pattern

**Exclude (General Programming Errors)**:
- ❌ Syntax errors, unused variables, type errors, compilation errors
- ❌ General code quality issues (formatting, naming conventions)
- ❌ Language-specific best practices that are general knowledge
- **These are NOT learnings** - they're general programming knowledge the LLM already knows


### **Decision Criteria: Task-Specific vs General Knowledge**

**CRITICAL PRINCIPLE**: Only capture learnings that are SPECIFIC to executing this task better. General programming knowledge is NOT a learning.

**Include (Task-Specific Learnings)**:
- ✅ Complete Go code snippets that successfully achieved the step description
- ✅ Function calls to generated tool packages (e.g., aws_tools, workspace_tools, custom_tools)
- ✅ Code logic, error handling, and data processing patterns
- ✅ **Task-specific execution failures**:
  - Wrong approach/strategy for achieving the step goal
  - Wrong data format/structure for the task
  - Wrong function/tool usage for the task
  - Missing task-specific prerequisites
  - Task-specific runtime errors (API errors, data validation errors, etc.)
  - Task-specific logic errors (wrong business logic for the task)

**Exclude (General Knowledge - NOT Learnings)**:
- ❌ Internal workspace management code unless it's part of the success pattern
- ❌ Workspace management tool calls (e.g., read_workspace_file, write_workspace_file) unless they're part of the code being learned
- ❌ **General programming language errors/guidelines** (NOT task-specific):
  - Syntax errors (missing semicolons, brackets, etc.)
  - Compilation errors (unused variables like "declared and not used: userId", type mismatches, etc.)
  - General code quality issues (formatting, naming conventions, etc.)
  - Language-specific best practices that are general knowledge
  - **These are NOT learnings** - they're general programming knowledge the LLM already knows

**CRITICAL (Priority Order)**: 
1. **Find ALL successful code**: Identify ALL Go code that successfully achieved the step goal
2. **Evaluate and rank by quality**: Compare successful code patterns and identify which are BEST based on:
   - Efficiency (fastest, most direct approach)
   - Code quality (clean, readable, well-structured)
   - Error handling (proper error checking and handling)
   - Reliability (consistent success rate)
   - Completeness (handles edge cases well)
3. **Extract BEST code(s)**: Extract complete, runnable Go code of the BEST patterns (or multiple best variations if equally effective)
4. **REPLACE ACTUAL VALUES WITH VARIABLES**: Before saving code, check if any hardcoded values match known variables. Replace them with variable placeholders (e.g., accountID := "{{AWS_ACCOUNT_ID}}", region := "{{AWS_REGION}}"). This makes code reusable across different environments.
5. **REPLACE WORKSPACE PATHS** (CRITICAL): In code examples and tool arguments documented in learning files, replace hardcoded workspace paths with {{WORKSPACE_PATH}} or relative paths
   - **Wrong**: "filepath": "Workflow/HDFC Personal Accounts/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"
   - **Correct**: "filepath": "{{WORKSPACE_PATH}}/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json" OR "filepath": "step-1/step_1_credentials.json"
   - **In Go code**: Use os.Args[1] for workspace path and filepath.Join() for relative paths (never hardcode full paths)
6. **Save best code**: Save complete, runnable code to ` + codePath + `/ folder:
   - If one best pattern: Save to {StepTitle}_code.go
   - If multiple best patterns: Save to {StepTitle}_code_v1.go, {StepTitle}_code_v2.go, etc. (ranked best first)
6. **Add effectiveness notes**: 1-2 sentences explaining WHY this code is best (e.g., "Most efficient approach", "Best error handling", "Handles edge cases")
7. **Focus on generated tool function calls**: Prioritize code that calls generated tool functions (aws_tools, workspace_tools, etc.)
8. **Rank in learning file**: Always list best code first, then other successful variations
9. **Reference saved code**: In learning file, reference the saved code path(s) with ranking

### **Failure Documentation (Critical for Improvement):**
**CRITICAL**: Every failure is a learning opportunity. Document what went wrong and how to prevent it.

**Enhanced Failure Format for Code Execution Errors:**
When documenting errors discovered from ExecutionHistory, use this format:
- **What failed**: Code pattern + error type (discovered from error message)
- **Why it failed**: Root cause (analyzed from the error message)
- **Error details**: Exact error message from "go run failed" output
- **Prevention**: Specific code fix (determined by analyzing the error)
- **Code example (wrong)**: Show the failing code snippet from ExecutionHistory
- **Code example (correct)**: Show the corrected code pattern (if you can determine it from context)
- **Use instead**: Reference to successful code pattern or general guidance

**Important:** Don't assume what errors exist. Discover them from the actual ExecutionHistory. Analyze each error message to understand what went wrong and document it clearly for future executions to avoid.

**Learn from Validation Failures:**
- If ValidationResult shows failure: Analyze what went wrong in the code
- Compare expected vs actual outcome
- Identify which part of the code caused the failure
- Add to "❌ FAILURES TO AVOID" section with clear guidance
- Update code patterns to prevent this failure in future runs

**Purpose**: Help future executions skip known failures and use the working code recipe immediately. Each failure should improve the code patterns.

### **Learning Documentation Focus:**
**Priority**: Document BEST code examples first (ranked by effectiveness), then other successful variations, then failures to avoid. Format optimized for future execution efficiency.

**Example (Best Code First with Optimal Path):**
**✅ BEST CODE PATTERNS (Ranked by Effectiveness):**

1. ⭐ **OPTIMAL**: Complete AWS + Workspace Integration [Runs: 15 | Success: 93%] ✅ - RECOMMENDED
   **Why Optimal**: Most efficient single-code approach, excellent error handling, handles all edge cases
   **Code saved to**: code/{StepTitle}_code.go (complete runnable code)
   **Key features**: Combined AWS and workspace operations, comprehensive error handling, variable replacement
   **Output File Format**: [Document the JSON structure/format of output files created by this code]
   
2. **ALTERNATIVE**: Separate AWS and Workspace Calls [Runs: 8 | Success: 75%] ✅
   **Why Alternative**: More modular but slightly less efficient
   **Code saved to**: code/{StepTitle}_code_v2.go (complete runnable code)
   **Key features**: Separate functions, easier to debug, good for complex workflows
   **Output File Format**: [Document the JSON structure/format of output files created by this code]

⚠️ **UNRELIABLE**: Direct HTTP calls without error handling [Runs: 5 | Success: 40%]
   **Why Unreliable**: No error handling caused panics, low success rate
   **Avoid**: Use optimal pattern (#1) instead

**Brief Context**: Optimal pattern (#1) combines operations efficiently with robust error handling. Alternative (#2) is more modular but requires multiple code executions. Unreliable pattern should be avoided.

**❌ FAILURES TO AVOID:**
- Code Pattern: Direct HTTP calls without error handling [Failed: 3 times]
  Root cause: No error handling caused panics
  Prevention: Always use generated function calls with error handling
  Use instead: Optimal pattern (#1 above)
- Code Pattern: Missing environment variable setup [Failed: 2 times]
  Root cause: MCP_SERVER_NAME not set before calling AWS tools
  Prevention: Set environment variables before function calls
  Use instead: Optimal pattern (#1 above)

**Documentation Guidelines:**
- **Priority 1**: Success code recipe (code patterns that achieved the goal)
- **Priority 2**: Success code snippets (save working code to code/ folder and reference them)
- **Priority 3**: Output file formats (JSON structure/format of files created by execution agent - especially files referenced in success criteria)
- **Priority 4**: Failures to avoid (save time in future executions)
- **Keep it actionable**: Future executions should be able to replicate success using code patterns
- **Save BEST Go code**: When Go code worked, save the BEST code to ` + codePath + `/{StepTitle}_code.go (or multiple files if multiple best variations exist)
- **CRITICAL - Capture Output File Formats**: Document the exact JSON structure/format of output files created by the execution agent. Since success criteria files are mostly JSON, extract the complete JSON structure including field names, data types, required vs optional fields, and nested structure. This ensures future executions create files in the same format that will pass validation.
- **Rank by effectiveness**: Always rank code by how well it executes the step - best code first
- **Multiple best codes**: If multiple effective patterns exist, save all of them (ranked best first) - future executions can choose the most appropriate
- **CRITICAL - Variable Replacement**: Always replace actual values in code with variable placeholders when they match known variables. This ensures code is reusable across different environments, accounts, and configurations.
- **CRITICAL - Workspace Path Replacement**: In learning file documentation, replace hardcoded workspace paths in tool arguments with {{WORKSPACE_PATH}} or relative paths. In Go code examples, use os.Args[1] and filepath.Join() for paths.
- **ONLY save best code**: Only save code that is truly effective - don't save mediocre or inefficient code just because it worked
- **Keep descriptions concise and focused on efficiency**
- **CRITICAL - File Content Must Be Short**: Write learning files that are brief, precise, and to the point. Avoid verbose explanations, long paragraphs, or unnecessary details. Each code pattern entry should be 1-2 lines maximum. Focus on actionable information only.
- **NO SCORING**: Do not add [Runs: X | Success: Y%] scores - detection agent handles this during consolidation
- **NO OPTIMAL MARKERS**: Do not mark ⭐ OPTIMAL paths - detection agent handles this during consolidation

### **Available Tools:**
You have access to all MCP tools to examine workspace files and gather additional context.

## 📝 **REQUIRED OUTPUT FORMAT**

**CRITICAL**: After writing the learning file, output ONLY the file path that was updated. Keep your response minimal and concise.

**Output Format:**
` + `Updated: ` + writePath + `/_learning_new.md` + `

**DO NOT provide:**
- Comprehensive summaries
- Detailed analysis reports
- Long explanations
- Lists of patterns or practices

**ONLY output:**
- The file path that was written/updated

**Key Requirements:**
- **EXTRACTION ONLY**: Extract patterns from current execution, do NOT merge with existing files
- **ALWAYS analyze BOTH success patterns AND failure patterns** from execution and validation
- **ALWAYS save working Go code** to ` + codePath + `/ folder before writing the learning file
- Document learnings ONLY in ` + writePath + `/ folder
- Focus on capturing the BEST code that worked (ranked by effectiveness), then other successful variations, then what to avoid
- Save complete, runnable Go code of BEST patterns to learnings/{step_id}/code/ folder and reference it in the learning file
- Extract and save the BEST code snippets (or multiple best variations) with full function calls
- Rank code by effectiveness: best code first, then alternatives, then failures to avoid
- Document only meaningful best code patterns - don't save mediocre code just because it worked
- **WRITE TO TEMP FILE**: Write extracted patterns to _learning_new.md (temporary file for detection agent)
- **NO MERGING**: Do NOT read existing files or merge patterns - detection agent handles that during consolidation
- **EXCLUDE WORKSPACE TOOLS**: Never include workspace management tools in success/failure patterns unless part of the code being learned

`
}

// learningUserMessageProcessorCodeExecution creates the user message for code execution mode learning
func (agent *HumanControlledTodoPlannerCodeExecutionLearningAgent) learningUserMessageProcessorCodeExecution(templateVars map[string]string) string {
	// Step-specific learnings: always use step-specific paths at workspace root (not inside runs/)
	// StepNumber is already the full learning path identifier (e.g., "step-3" or "step-3-true-0")
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	writePath := workspacePath + "/learnings/" + stepNumber // Write to step-specific folder at workspace root (supports both regular and branch steps)
	codePath := workspacePath + "/learnings/" + stepNumber + "/code"

	return `# Go Code Pattern Extraction Task (Focus on Execution Efficiency)

**PRIMARY GOAL**: Extract the BEST possible Go code (or multiple best variations) that executed this step most effectively, so future code generation can use optimal patterns.

**Focus on finding the BEST code - most efficient, cleanest, most reliable patterns. Extract complete, runnable code snippets. Priority: Best code first, then other successful variations, then failures to avoid.**

## 📋 **STEP CONTEXT**
- **Title**: ` + templateVars["StepTitle"] + `
- **Description**: ` + templateVars["StepDescription"] + `
- **Success Criteria**: ` + templateVars["StepSuccessCriteria"] + `
- **Context Dependencies**: ` + templateVars["StepContextDependencies"] + `
- **Expected Output**: ` + templateVars["StepContextOutput"] + `
- **Workspace**: ` + templateVars["WorkspacePath"] + `

` + func() string {
		existingLearnings := templateVars["ExistingLearningsContent"]
		if existingLearnings != "" && strings.TrimSpace(existingLearnings) != "" {
			return `## 📚 EXISTING LEARNINGS

These are existing learnings from previous executions. If the current execution is similar, improve upon these patterns rather than duplicating them.

` + existingLearnings + `

---
`
		}
		return ""
	}() + func() string {
		if templateVars["VariableNames"] != "" {
			return `## 🔑 AVAILABLE VARIABLES

These variables may appear in the plan as {{VARIABLE_NAME}} placeholders:
` + templateVars["VariableNames"] + `

**CRITICAL VARIABLE HANDLING**:
1. **Preserve Existing Placeholders**: When analyzing learnings, preserve ALL {{VARS}} exactly as written. DO NOT replace them with actual values. Keep variable placeholders like {{AWS_ACCOUNT_ID}} intact.

2. **Replace Actual Values with Variables**: When extracting Go code from ExecutionHistory, if you find actual values in code that match known variables, REPLACE those actual values with the corresponding variable placeholder:
   - Example: If code has accountID := "123456789012" and {{AWS_ACCOUNT_ID}} is a known variable, replace it with accountID := "{{AWS_ACCOUNT_ID}}"
   - Example: If code has region := "us-east-1" and {{AWS_REGION}} is a known variable, replace it with region := "{{AWS_REGION}}"
   - This makes code recipes reusable across different environments and accounts

3. **Replace Workspace Paths** (CRITICAL): In learning file documentation and tool arguments, replace hardcoded workspace paths with {{WORKSPACE_PATH}} or relative paths:
   - **Wrong**: "filepath": "Workflow/HDFC Personal Accounts/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"
   - **Correct**: "filepath": "{{WORKSPACE_PATH}}/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json" OR "filepath": "step-1/step_1_credentials.json"
   - **In Go code**: Use os.Args[1] for workspace path and filepath.Join(basePath, "step-1/file.json") for relative paths (never hardcode full paths)

4. **Check All Code Values**: Before documenting code, systematically check each hardcoded value against the list of known variables above. If a match is found, use the variable placeholder instead of the actual value.

5. **Code Snippets**: When saving Go code snippets, also replace hardcoded values that match known variables with variable placeholders (e.g., accountID := "{{AWS_ACCOUNT_ID}}" instead of accountID := "123456789012").
`
		}
		return ""
	}() + `
## 📊 **EXECUTION RESULTS**
` + templateVars["ExecutionHistory"] + `

## ✅ **VALIDATION RESULTS**
` + templateVars["ValidationResult"] + `

## 🧠 **YOUR TASK - EXTRACT CODE RECIPE FOR EFFICIENCY**

**Follow the efficiency-focused code extraction process described in the system prompt.**

**Remember**: 
1. Success code recipe comes first (what worked) - rank by effectiveness
2. **Replace actual values with variables**: When extracting code, check if values match known variables and replace them with variable placeholders (e.g., {{AWS_ACCOUNT_ID}} instead of "123456789012")
3. **Learn from Failures**: Analyze validation failures, identify root causes, document what to avoid
4. Failures to avoid come second (save time)
5. Keep it actionable for future executions
6. **Write short, precise content**: Each entry should be 1-2 lines maximum. No verbose explanations.
7. **NO SCORING**: Do not add [Runs: X | Success: Y%] scores - detection agent handles this during consolidation
8. **NO OPTIMAL MARKERS**: Do not mark ⭐ OPTIMAL paths - detection agent handles this during consolidation

**🔴 LEARNING FROM FAILURES (TASK-SPECIFIC ONLY):**

**CRITICAL**: Only document failures that relate to executing THIS TASK better. General programming errors are NOT learnings.

**Include (Task-Specific Failures)**:
- **Analyze ValidationResult**: If validation failed, understand WHY (task-specific reason, not general code errors)
- **Identify failing code**: Which part of the code caused the failure?
- **Root cause analysis**: Was it wrong approach for the task? Wrong data format for the task? Missing task prerequisite? Wrong task-specific logic?
- **Document failures**: What task-specific code patterns failed and why
- **Document fix**: What task-specific change would make this code succeed next time?

**Exclude (General Programming Errors)**:
- ❌ Syntax errors, unused variables, type errors, compilation errors
- ❌ General code quality issues (formatting, naming conventions)
- ❌ Language-specific best practices that are general knowledge
- **These are NOT learnings** - they're general programming knowledge the LLM already knows

**File to create:**
` + `- ` + writePath + `/_learning_new.md (temporary file for detection agent)

**CRITICAL FILE HANDLING INSTRUCTIONS:**
1. **EXTRACTION ONLY**: Extract learnings from current execution (ExecutionHistory and ValidationResult above)
   - Do NOT read existing learning files
   - Do NOT merge with existing patterns
   - Do NOT update scores or optimal paths
   
2. **WRITE NEW FILE**: Write extracted patterns to: ` + writePath + `/_learning_new.md
   - This is a temporary file that the detection agent will process during consolidation
   - Include all patterns extracted from current execution (success + failure)
   - Use same format as final learning file, but without scores or optimal markers
   
3. **SAVE WORKING CODE**: If Go code worked, save the complete code to ` + codePath + `/` + templateVars["StepTitle"] + `_code.go using write_workspace_file tool, then reference it in the learning file
   
4. **EXCLUDE WORKSPACE TOOLS**: Never include workspace management tools (read_workspace_file, write_workspace_file, etc.) in success/failure patterns unless part of the code being learned

**After writing the file, output ONLY the file path** (e.g., "Updated: ` + writePath + `/_learning_new.md"). 

**Keep response minimal** - just the file path. No summaries or analysis.
`
}
