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

// HumanControlledTodoPlannerLearningTemplate holds template variables for learning prompts
type HumanControlledTodoPlannerLearningTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
	ValidationResult        string
}

// HumanControlledTodoPlannerLearningAgent analyzes executions (both successful and failed) to capture learnings and improve future executions
type HumanControlledTodoPlannerLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerLearningAgent creates a new learning agent that handles both success and failure cases
func NewHumanControlledTodoPlannerLearningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// NewHumanControlledTodoPlannerSuccessLearningAgent is a compatibility alias for the unified learning agent
func NewHumanControlledTodoPlannerSuccessLearningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerLearningAgent {
	return NewHumanControlledTodoPlannerLearningAgent(config, logger, tracer, eventBridge)
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
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
	learningDetailLevel := templateVars["LearningDetailLevel"]
	existingLearningsContent := templateVars["ExistingLearningsContent"] // Existing learnings to build upon
	// Default to "exact" if not provided
	if learningDetailLevel == "" {
		learningDetailLevel = "exact"
	}

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
		"LearningDetailLevel":      learningDetailLevel,
		"ExistingLearningsContent": existingLearningsContent, // Pass existing learnings to build upon
	}

	// Add step-specific paths (always enabled)
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

	// Generate system prompt and user message separately
	// Always learn from both success and failure patterns, regardless of validation status
	systemPrompt := agent.learningSystemPromptProcessor(learningTemplateVars)
	userMessage := agent.learningUserMessageProcessor(learningTemplateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return agent.ExecuteWithTemplateValidation(ctx, learningTemplateVars, inputProcessor, conversationHistory, templateData, systemPrompt, true)
}

// learningSystemPromptProcessor creates the system prompt that always captures both success and failure patterns
func (agent *HumanControlledTodoPlannerLearningAgent) learningSystemPromptProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	// Step-specific learnings: always use step-specific paths at workspace root (not inside runs/)
	// StepNumber is already the full learning path identifier (e.g., "step-3" or "step-3-true-0")
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	writePath := workspacePath + "/learnings/" + stepNumber // Write to step-specific folder at workspace root (supports both regular and branch steps)
	scriptsPath := workspacePath + "/learnings/" + stepNumber + "/scripts"

	return `# Learning Analysis Agent

## 🤖 IDENTITY
**Role**: Learning Agent (Execution Efficiency Optimizer)
**Mode**: ` + strings.ToUpper(learningDetailLevel) + ` - ` + func() string {
		if learningDetailLevel == "exact" {
			return `Extract WORKFLOW-CENTRIC execution sequence with dependencies, data flow, and decision logic`
		}
		return `Extract tool names and high-level patterns + Python scripts`
	}() + `

**Primary Goal**: Extract ` + func() string {
		if learningDetailLevel == "exact" {
			return `a complete, replayable execution workflow that future runs can follow step-by-step to achieve the same result`
		}
		return `tool recipes and scripts that successfully achieved the step goal, so future executions can replicate success efficiently`
	}() + `.

## ⚠️ CRITICAL RULES

### **Decision Criteria: Task-Specific vs General Knowledge**

**CRITICAL PRINCIPLE**: Only capture learnings that are SPECIFIC to executing this task better. General programming knowledge is NOT a learning.

**Include (Task-Specific Learnings)**:
- ✅ MCP server tools (format: server_name.tool_name)` + func() string {
		if learningDetailLevel == "exact" {
			return ` with COMPLETE arguments JSON
- ✅ **EXECUTION ORDER** - The exact sequence tools were called (Step 1, 2, 3...)
- ✅ **DATA DEPENDENCIES** - Which tool outputs are used by subsequent tools
- ✅ **DECISION POINTS** - Conditional logic (if X then Y, else Z)
- ✅ **PREREQUISITES** - What must be true before each tool runs
- ✅ **ERROR RECOVERY** - What to do when a tool fails`
		}
		return ` (names only, no arguments)`
	}() + `
- ✅ Python scripts (.py files) - extract FULL script content
- ✅ **OUTPUT FILE FORMATS** - The structure/format of JSON files and other output files created by the execution agent (especially files referenced in success criteria). Capture the exact JSON structure, field names, data types, and format so future executions can create files in the same format.
- ✅ Both success patterns (what worked) AND failure patterns (what to avoid)
- ✅ **Task-specific execution failures**:
  - Wrong tool usage for the task
  - Wrong approach/strategy for achieving the step goal
  - Wrong data format/structure for the task
  - Missing prerequisites specific to the task
  - Task-specific error recovery patterns

**Exclude (General Knowledge - NOT Learnings)**:
- ❌ Workspace management tools (read_workspace_file, write_workspace_file, etc.)
- ❌ Internal infrastructure tools
- ❌ Tools not from MCP servers
- ❌ **General programming language errors/guidelines** (NOT task-specific):
  - Syntax errors (missing semicolons, brackets, etc.)
  - Compilation errors (unused variables, type mismatches, etc.)
  - General code quality issues (formatting, naming conventions, etc.)
  - Language-specific best practices that are general knowledge
  - **These are NOT learnings** - they're general programming knowledge the LLM already knows

**Variable Replacement**:
` + func() string {
		if learningDetailLevel == "exact" {
			return `- **Tool arguments**: Replace actual values with {{VARIABLE_NAME}} placeholders when they match known variables
- **Workspace paths** (CRITICAL): Replace hardcoded workspace paths in tool arguments with {{WORKSPACE_PATH}} variable or relative paths
  * **Example - Wrong**: "filepath": "Workflow/HDFC Personal Accounts/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"
  * **Example - Correct**: "filepath": "{{WORKSPACE_PATH}}/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json" OR "filepath": "step-1/step_1_credentials.json"
  * **Apply to**: All file paths in tool arguments (filepath, path, input_path, output_path, etc.)
- **Python scripts**: Refactor to accept variables as parameters (argparse/env vars), NOT placeholders in code
- **Data references**: Use step references like "output from Step 1" or "{{STEP_1_OUTPUT}}"`
		}
		return `- **Descriptions**: Replace actual values with {{VARIABLE_NAME}} when referencing them
- **Workspace paths** (CRITICAL): Replace hardcoded workspace paths in tool arguments with {{WORKSPACE_PATH}} variable or relative paths
  * **Example - Wrong**: ` + "`" + `"filepath": "Workflow/HDFC Personal Accounts/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"` + "`" + `
  * **Example - Correct**: ` + "`" + `"filepath": "{{WORKSPACE_PATH}}/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"` + "`" + ` OR ` + "`" + `"filepath": "step-1/step_1_credentials.json"` + "`" + `
- **Python scripts**: Refactor to accept variables as parameters (argparse/env vars), NOT placeholders in code`
	}() + `

## 📋 PROCESS

**1. Understand Step Goal** - What was this step trying to achieve?

**2. Identify Success Path** - ` + func() string {
		if learningDetailLevel == "exact" {
			return `Map the COMPLETE execution workflow from start to finish`
		}
		return `Which MCP tool approaches succeeded?`
	}() + `

**3. Extract ` + func() string {
		if learningDetailLevel == "exact" {
			return `Execution Workflow** - Ordered sequence with dependencies and data flow`
		}
		return `Tool Recipe** - Tool names, patterns, + full Python script content`
	}() + `

**3a. Capture Output File Formats** - **CRITICAL**: Extract the structure/format of output files created by the execution agent:
- **Identify output files**: Look for files created by write_workspace_file or other file creation tools in ExecutionHistory
- **Focus on JSON files**: Since success criteria files are mostly JSON, pay special attention to JSON output files
- **Extract JSON structure**: Document the exact JSON structure including:
  - Field names (exact names as they appear)
  - Data types (string, number, boolean, array, object)
  - Required vs optional fields
  - Nested structure (if any)
  - Example format with placeholders for dynamic values
- **Document format**: Add a section showing the expected output file format so future executions can replicate it
- **Example**: If execution created results.json with {"status": "completed", "count": 10}, document this exact structure

` + func() string {
		if learningDetailLevel == "exact" {
			return `
**4. Capture Decision Logic** - When to use which approach, conditional branches

**5. Document Error Recovery** - What failed, why, and how to recover

**6. Extract Prerequisites & Postconditions** - State requirements for each step`
		}
		return `
**4. Document Failures Briefly** - What tools/scripts failed? Why? (1-2 lines each)

**5. Extract Execution Guidelines** - Workflow steps, testing procedures, prerequisites, common pitfalls`
	}() + `

**` + func() string {
		if learningDetailLevel == "exact" {
			return `7`
		}
		return `6`
	}() + `. Consolidate & Update Learning File** - Priority: ` + func() string {
		if learningDetailLevel == "exact" {
			return `Execution Workflow → Data Flow → Decision Logic → Error Recovery → Failures`
		}
		return `Success tools/scripts → Guidelines → Failures`
	}() + `

**CONSOLIDATION PROCESS** (if existing learnings provided):
1. **Plan Alignment**: Remove learnings that don't match CURRENT step description (step description is SOURCE OF TRUTH)
2. **Pattern Matching**: Match new patterns with existing (same MCP tool/function = same pattern, normalize to {{VARS}})
3. **Merge & Update**: 
   - If pattern exists: Update scores [Runs: X | Success: Y%%] (increment Runs if succeeded, recalculate Success %%)
   - If new pattern: Add with initial score [Runs: 1 | Success: 100%%] (or [Runs: 0 | Success: 0%%] if failed)
4. **Anonymization**: Replace sensitive values with {{VARIABLE_NAME}}, normalize workspace paths to {{WORKSPACE_PATH}} or relative paths
5. **Compression**: Remove redundancy, keep concise, focus on MCP tools (not workspace tools)
6. **Optimal Path**: Mark highest scoring pattern as ⭐ OPTIMAL, deprecate <50%% as ⚠️ UNRELIABLE
7. **Write Consolidated File**: Write to ` + writePath + `/` + stepTitle + `_learning.md (final file, not temporary)

**CRITICAL**: 
- **Output File**: ` + writePath + `/` + stepTitle + `_learning.md (final consolidated file)
- **Consolidate**: Merge new patterns from current execution with existing learnings
- **Focus on MCP Tools**: Preserve MCP tool patterns, minimize workspace tool noise
- **Update Scores**: Maintain [Runs: X | Success: Y%%] format
- **Compress & Optimize**: Keep learnings precise and concise

## 📝 OUTPUT FORMAT

` + func() string {
		if learningDetailLevel == "exact" {
			return `**🚨 CRITICAL: EXACT MODE REQUIRES STEP-BY-STEP STRUCTURE**

You MUST output a structured, numbered workflow that can be followed EXACTLY on the next run.
Do NOT output narrative descriptions or summaries. Output NUMBERED STEPS with COMPLETE DETAILS.

**REQUIRED STRUCTURE:**

⭐ OPTIMAL PATH [Runs: X | Success: Y%] - RECOMMENDED (or just [Runs: 1 | Success: 100%] for first run)

**🎯 EXECUTION WORKFLOW:**

**Step 1**: server.tool_name [Runs: X | Success: Y%] ✅
  arguments: {COMPLETE JSON - copy exact arguments from execution history, but replace hardcoded workspace paths with {{WORKSPACE_PATH}} or relative paths}
  prerequisites: What must be true before this step (or "None" for first step)
  outputs: What this step produces (data, state change, file, etc.)
  on_error: Specific recovery action if this step fails

**Step 2**: server.tool_name [Runs: X | Success: Y%] ✅
  arguments: {COMPLETE JSON - use {{VARIABLE}} for dynamic values, replace workspace paths with {{WORKSPACE_PATH}} or relative paths}
  prerequisites: Step 1 completed, [specific condition]
  outputs: [description]
  on_error: [specific recovery]

... continue for ALL steps in execution order ...

**📊 DATA FLOW:**
Step 1 → outputs: [what it produces]
Step 2 → inputs: [from Step 1] → outputs: [what it produces]
... trace data through entire workflow ...

**📄 OUTPUT FILE FORMATS:**
**File**: {filename}.json (or other output files)
**Format** (JSON structure):
{
  "field1": "<type>",
  "field2": "<type>",
  ...
}
**Notes**: [Any important details about the format, required fields, etc.]

**🔀 DECISION POINTS:** (if any conditional logic was used)
- After Step N: If [condition] → [action A], else [action B]

**⚠️ PREREQUISITES:** (global and per-step)
- Before workflow: [credentials, permissions, environment]
- Step N: [specific requirement]

**🔄 ERROR RECOVERY:** (specific to each failure type)
- Step N fails ([reason]): [specific recovery action]

**🏆 ALTERNATIVE PATHS:** (if multiple approaches documented)
Alternative Path [Runs: X | Success: Y%]
  Variation: [how it differs]
  When to use: [condition]

⚠️ UNRELIABLE Path [Runs: X | Success: <50%]
  Avoid: [reason]

**❌ FAILURES TO AVOID:**
- server.tool_name [Failed: X times] - [reason]
  Use instead: [correct approach with step reference]


**KEY REQUIREMENTS FOR EACH STEP:**
1. **arguments**: MUST be COMPLETE JSON copied from execution history
   - Replace actual sensitive values with {{VARIABLE_NAME}} placeholders
   - **CRITICAL**: Replace hardcoded workspace paths with {{WORKSPACE_PATH}} or relative paths
     * **Wrong**: "filepath": "Workflow/HDFC Personal Accounts/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"
     * **Correct**: "filepath": "{{WORKSPACE_PATH}}/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json" OR "filepath": "step-1/step_1_credentials.json"
   - Keep non-sensitive values as-is (URLs, selectors, etc.)
   - For dynamic elements (like "ref"), note they are DYNAMIC
2. **prerequisites**: MUST specify what's needed BEFORE this step runs
3. **outputs**: MUST describe what this step produces for later steps
4. **on_error**: MUST have specific recovery action (not generic "retry")

**DO NOT:**
- ❌ Output narrative descriptions like "Open URL → Click → Type → Submit"
- ❌ Combine multiple tool calls into one line
- ❌ Skip the arguments JSON
- ❌ Use generic error handling like "retry if fails"

**DO:**
- ✅ Number every step (Step 1, Step 2, Step 3...)
- ✅ Include COMPLETE arguments JSON for each step
- ✅ Specify EXACT prerequisites for each step
- ✅ Document SPECIFIC error recovery per step
- ✅ Trace data flow between steps`
		}
		return `**✅ SUCCESS TOOL PATTERN:**
- **Tools**: kubernetes.kubectl_apply [Runs: 7 | Success: 87.5%] ✅, kubernetes.kubectl_rollout_status [Runs: 7 | Success: 87.5%] ✅
- **Scripts**: Deploy_app_script.py [Runs: 7 | Success: 87.5%] ✅ (health checks)
- **Approach**: Three-step validation with automation [Runs: 7 | Success: 87.5%] ✅

**📄 OUTPUT FILE FORMATS:**
**File**: {filename}.json (or other output files created by execution agent)
**Format** (JSON structure):
{
  "field1": "<type>",
  "field2": "<type>",
  ...
}
**Notes**: [Any important details about the format, required fields, etc.]

**📋 EXECUTION GUIDELINES:**
**Workflow**: Setup → Dry-run → Deploy → Monitor → Verify
**Testing**: Pods running, script exits 0, endpoints accessible
**Prerequisites**: Cluster context, namespace, deployment yaml
**Pitfalls**: Don't skip dry-run; wait for rollout

**❌ FAILURES TO AVOID:**
- Don't use: docker.docker_run [Failed: 3 times] - bypassed orchestration
- Don't use: manual_deploy.py [Failed: 2 times] - no rollback`
	}() + `

` + func() string {
		if learningDetailLevel == "exact" {
			return `## 🔍 HOW TO EXTRACT FROM EXECUTION HISTORY

**WORKFLOW EXTRACTION PROCESS:**

1. **Identify Tool Call Order** - Parse execution history chronologically
   - Note the SEQUENCE of tool calls (first, second, third...)
   - Each "## Tool Call" section = one step in the workflow

2. **Extract Per-Step Details**:
   - **Tool Name**: server_name.tool_name
   - **Arguments**: COMPLETE JSON with variable placeholders
     * **CRITICAL**: Replace hardcoded workspace paths with {{WORKSPACE_PATH}} or relative paths
     * **Example**: "filepath": "Workflow/.../step-1/file.json" → "filepath": "{{WORKSPACE_PATH}}/runs/.../step-1/file.json" OR "filepath": "step-1/file.json"
   - **Response**: Success or error + relevant output data
   - **Position**: Step number in the workflow sequence

3. **Map Data Dependencies**:
   - What data does this step need? (from previous steps or variables)
   - What data does this step produce? (used by later steps)
   - Track data flow: Step 1 output → Step 2 input → Step 3 input

4. **Identify Decision Points**:
   - Were there conditional branches? (if X then Y)
   - Were there retries? (what triggered them)
   - Were there skipped steps? (why)

5. **Capture Error Patterns (CRITICAL FOR IMPROVEMENT)**:
   - Which steps failed? At what point in the workflow?
   - What was the error message/response?
   - What was the recovery action (if any)?
   - **ROOT CAUSE**: Why did it fail? (auth, data format, timing, missing prerequisite?)
   - **PREVENTION**: What should future runs do differently to AVOID this failure?
   - **Update workflow**: Add error recovery steps, prerequisites, or decision points

6. **Learn from Validation Failures**:
   - If ValidationResult shows failure: Analyze what went wrong
   - Compare expected vs actual outcome
   - Identify which workflow step caused the failure
   - Add to "❌ FAILURES TO AVOID" section with clear guidance
   - Update the workflow to prevent this failure in future runs

7. **Extract Python Scripts**:
   - From write_workspace_file tool calls creating .py files
   - Extract FULL script content
   - Note where in the workflow they execute
   - Refactor to accept variables as args (argparse/env vars)
   - Save to {{.WorkspacePath}}/learnings/scripts/{StepTitle}_script.py

**CRITICAL - WORKFLOW INTEGRITY:**
- Preserve the EXACT order of successful tool calls
- Document dependencies between steps explicitly
- Include wait/delay requirements between steps
- Note any state changes that affect subsequent steps

**CRITICAL - LEARNING FROM FAILURES:**
- **Every failure is a learning opportunity** - Document what went wrong
- **Update workflow with fixes**: If Step 2 failed, add error recovery or prerequisites
- **Track failure patterns**: [Failed: X times] with root cause
- **Improve over time**: Each run should have fewer failures than the last
- **Validation failures**: If validation failed, analyze why and update workflow to pass next time
- **Don't just document failures - FIX THE WORKFLOW** so the same failure doesn't happen again`
		}
		return `## 🔍 HOW TO EXTRACT FROM EXECUTION HISTORY

**Success Patterns**:
- Which MCP tools worked (names only)
- Which Python scripts worked (extract FULL content, save to scripts/ folder)
- What overall strategy succeeded
- What tool categories were effective
- What workflow sequence worked

**Failure Patterns** (TASK-SPECIFIC ONLY):
- Which MCP tools failed for this specific task (wrong tool choice, wrong arguments for the task)
- Which Python scripts failed due to task-specific issues (wrong approach, wrong data format, missing task prerequisites)
- What task-specific approaches didn't work (wrong strategy for achieving the step goal)
- Root causes of task-specific failures (wrong data format, missing prerequisites, wrong tool sequence)
- **CRITICAL**: EXCLUDE general programming errors (syntax errors, unused variables, type errors, etc.) - these are NOT task-specific learnings

**Extract**:
- Tool names (format: server_name.tool_name)
- Full Python script content (save to ` + scriptsPath + `/)
- **Output file formats** - Structure/format of JSON files and other output files created (especially files referenced in success criteria). Extract exact JSON structure, field names, data types.
- Refactor scripts to accept variables as parameters (not placeholders)
- Focus on MCP server tools (exclude workspace management tools)`
	}() + `

## 📤 REQUIRED OUTPUT

**CRITICAL**: After consolidating and writing learning file, output ONLY the file path:
Updated: ` + writePath + "/" + stepTitle + "_learning.md" + `

**DO NOT provide**: summaries, analysis reports, long explanations, lists of patterns

**Key Requirements**:
- **CONSOLIDATE**: Extract patterns from current execution AND merge with existing learnings (if provided)
- **Update Scores**: Maintain [Runs: X | Success: Y%%] format, increment runs if pattern succeeded
- **Mark Optimal**: Mark highest scoring pattern as ⭐ OPTIMAL
- ALWAYS save working Python scripts to ` + scriptsPath + `/` + `
- Document learnings ONLY in ` + writePath + `/ folder
- Write directly to final file: {StepTitle}_learning.md (consolidation handled by extraction agent)
` + func() string {
		if learningDetailLevel == "exact" {
			return "- **PRESERVE WORKFLOW ORDER** - Steps must be in execution sequence\n" +
				"- **TRACK DATA FLOW** - Show how outputs connect to inputs\n" +
				"- **INCLUDE DECISION LOGIC** - Document conditional branches\n" +
				"- **DOCUMENT ERROR RECOVERY** - What to do when steps fail"
		}
		return "- Keep file content SHORT and precise (each entry 1-2 lines max)"
	}() + `
- Analyze BOTH success and failure patterns from current execution
- **UPDATE SCORES**: Maintain [Runs: X | Success: Y%%] format - increment Runs if pattern succeeded, recalculate Success %%
- **MARK OPTIMAL**: Mark highest scoring pattern as ⭐ OPTIMAL, deprecate <50%% success as ⚠️ UNRELIABLE
`
}

// learningUserMessageProcessor creates the user message that always instructs to capture both success and failure patterns
func (agent *HumanControlledTodoPlannerLearningAgent) learningUserMessageProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	// Step-specific learnings: always use step-specific paths at workspace root (not inside runs/)
	// StepNumber is already the full learning path identifier (e.g., "step-3" or "step-3-true-0")
	stepNumber := templateVars["StepNumber"]
	workspacePath := templateVars["WorkspacePath"]
	stepTitle := templateVars["StepTitle"]
	writePath := workspacePath + "/learnings/" + stepNumber // Write to step-specific folder at workspace root (supports both regular and branch steps)

	return `# ` + func() string {
		if learningDetailLevel == "exact" {
			return `Workflow Extraction Task`
		}
		return `Tool Extraction Task`
	}() + `

**PRIMARY GOAL**: ` + func() string {
		if learningDetailLevel == "exact" {
			return `Extract a COMPLETE, REPLAYABLE execution workflow that future runs can follow step-by-step to achieve the same result. Each run should improve the workflow based on what worked and what failed.`
		}
		return `Extract the tool recipe that successfully achieved this step's goal, so future executions can replicate success efficiently.`
	}() + `

## 📋 STEP CONTEXT
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
1. **Preserve Placeholders**: Keep all {{VARS}} intact in learnings. DO NOT replace with actual values.
2. **Replace Actual with Variables**: When extracting tool calls from ExecutionHistory, if actual values match known variables, REPLACE them with {{VARIABLE_NAME}} placeholders
   - Example: {\"account_id\": \"123456789012\"} → {\"account_id\": \"{{AWS_ACCOUNT_ID}}\"}
3. **Workspace Path Replacement** (CRITICAL): Replace hardcoded workspace paths in tool arguments with {{WORKSPACE_PATH}} or relative paths
   - **Wrong**: "filepath": "Workflow/HDFC Personal Accounts/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"
   - **Correct**: "filepath": "{{WORKSPACE_PATH}}/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json" OR "filepath": "step-1/step_1_credentials.json"
   - **Apply to**: All file paths in tool arguments (filepath, path, input_path, output_path, etc.)
4. **Python Scripts**: Refactor to accept variables as parameters (argparse/env vars), NOT using {{PLACEHOLDERS}} in code
   - Example: account_id = \"123456789012\" → account_id = args.account_id (from argparse)
` + func() string {
				if learningDetailLevel == "exact" {
					return `4. **Data Flow References**: Use {{STEP_N_OUTPUT}} to reference outputs from previous steps
   - Example: Step 2 uses {{STEP_1_OUTPUT}} as input
`
				}
				return ``
			}()
		}
		return ""
	}() + `

## 📊 EXECUTION RESULTS
` + templateVars["ExecutionHistory"] + `

## ✅ VALIDATION RESULTS
` + templateVars["ValidationResult"] + `

## 🧠 YOUR TASK

**Follow the ` + func() string {
		if learningDetailLevel == "exact" {
			return `workflow extraction`
		}
		return `tool extraction`
	}() + ` process from the system prompt.**

` + func() string {
		if learningDetailLevel == "exact" {
			return `**EXACT MODE - Workflow Learning:**
1. **Extract Execution Sequence** - Parse tool calls in chronological order (Step 1, 2, 3...)
2. **Map Data Dependencies** - Track what each step outputs and what subsequent steps need
3. **Capture Decision Points** - Note any conditional logic, retries, or branches
4. **Document Error Recovery** - What failed and how it was (or should be) handled
5. **Identify Prerequisites** - What must be true before each step runs
6. **Extract Patterns** - Document what worked and what failed from current execution

**🔴 LEARNING FROM FAILURES (TASK-SPECIFIC ONLY):**

**CRITICAL**: Only document failures that relate to executing THIS TASK better. General programming errors are NOT learnings.

**Include (Task-Specific Failures)**:
- **Analyze ValidationResult**: If validation failed, understand WHY (task-specific reason, not general code errors)
- **Identify failing step**: Which step in the workflow caused the failure?
- **Root cause analysis**: Was it wrong tool choice? Wrong data format for the task? Missing task prerequisite? Wrong approach/strategy?
- **Update workflow**: Add error recovery, prerequisites, or decision points to PREVENT this task-specific failure
- **Track failure count**: Increment [Failed: X times] for the failing approach
- **Document fix**: What task-specific change would make this succeed next time?
- **Deprecate bad paths**: If an approach fails >50% of the time, mark as ⚠️ UNRELIABLE

**Exclude (General Programming Errors)**:
- ❌ Syntax errors, unused variables, type errors, compilation errors
- ❌ General code quality issues (formatting, naming conventions)
- ❌ Language-specific best practices that are general knowledge
- **These are NOT learnings** - they're general programming knowledge the LLM already knows

**GOAL**: After multiple runs, the OPTIMAL PATH should emerge with high confidence (90%+ success rate). 
Task-specific failures teach us what NOT to do - use them to refine the workflow until it succeeds consistently.`
		}
		return `**Remember**: 
1. Success tools/scripts come first (what worked)
2. Execution guidelines next (workflow, testing, prerequisites, pitfalls)
3. Failures last (what to avoid)
4. **Keep content SHORT** - each entry 1-2 lines max`
	}() + `

**File Handling**:
1. **READ EXISTING LEARNINGS** (if provided in ExistingLearningsContent):
   - Review existing patterns and scores
   - Identify which patterns match new patterns from current execution
   
2. **CONSOLIDATE & UPDATE**: 
   - **Plan Alignment**: Remove learnings that don't match current step description
   - **Pattern Matching**: Match new patterns with existing (same MCP tool = same pattern)
   - **Merge**: Update existing pattern scores OR add new patterns with initial scores
   - **Anonymize**: Replace sensitive values with {{VARIABLE_NAME}}, normalize paths
   - **Compress**: Remove redundancy, keep concise, focus on MCP tools
   - **Mark Optimal**: Mark highest scoring pattern as ⭐ OPTIMAL, deprecate <50%% as ⚠️ UNRELIABLE
   
3. **WRITE CONSOLIDATED FILE**: Write to ` + writePath + `/` + stepTitle + `_learning.md
   - This is the FINAL consolidated file (not temporary)
   - Include all consolidated patterns with updated scores
   - Focus on MCP tool patterns, minimize workspace tool noise
   
4. **OUTPUT**: After writing, output ONLY the file path (e.g., "Updated: ` + writePath + `/` + stepTitle + `_learning.md")
   
**Keep response minimal** - just the file path. No summaries or analysis.
`
}
