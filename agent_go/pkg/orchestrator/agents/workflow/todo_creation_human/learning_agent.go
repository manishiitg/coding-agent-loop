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
	// Default to "exact" if not provided
	if learningDetailLevel == "" {
		learningDetailLevel = "exact"
	}

	// Prepare template variables
	learningTemplateVars := map[string]string{
		"StepTitle":               stepTitle,
		"StepDescription":         stepDescription,
		"StepSuccessCriteria":     stepSuccessCriteria,
		"StepContextDependencies": stepContextDependencies,
		"StepContextOutput":       stepContextOutput,
		"WorkspacePath":           workspacePath,
		"ExecutionHistory":        executionHistory,
		"ValidationResult":        validationResult,
		"VariableNames":           variableNames,
		"LearningDetailLevel":     learningDetailLevel,
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

**What to Capture**:
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

**What to EXCLUDE**:
- ❌ Workspace management tools (read_workspace_file, write_workspace_file, etc.)
- ❌ Internal infrastructure tools
- ❌ Tools not from MCP servers

**Variable Replacement**:
` + func() string {
		if learningDetailLevel == "exact" {
			return `- **Tool arguments**: Replace actual values with {{VARIABLE_NAME}} placeholders when they match known variables
- **Python scripts**: Refactor to accept variables as parameters (argparse/env vars), NOT placeholders in code
- **Data references**: Use step references like "output from Step 1" or "{{STEP_1_OUTPUT}}"`
		}
		return `- **Descriptions**: Replace actual values with {{VARIABLE_NAME}} when referencing them
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
	}() + `. Handle Existing Files** - **MANDATORY**:

**Consolidation Behavior** (when multiple files detected):
- **Detection**: Multiple files if path contains commas or ends with "/" or "\"
- **Process**:
  1. If comma-separated: Split by comma and read each file
  2. If folder path: Use 'list_workspace_files' to find all *_learning.md files, then read each
  3. Read all files and extract patterns from each
  4. **Consolidate Logic**:
     - Compare patterns across all files
     - **Keep Latest**: Patterns from most recent files (by modification time or content freshness)
     - **Keep Best**: Patterns with highest success rates [Runs: X | Success: Y%]
     - **Merge Duplicates**: When same pattern appears in multiple files, combine run counts and recalculate success rate
     - **Remove Outdated**: Patterns with low success (<50%) that have better alternatives
     - **Preserve Unique**: Keep all unique valuable patterns from all files
  5. **Output**: Write consolidated content to single file at target path
  6. **Cleanup (MANDATORY)**: After successfully writing consolidated file, delete ALL old files that were consolidated (use delete_workspace_file tool for each old file)
- **Priority**: Latest patterns → Best success rates → Most runs → Unique patterns
- **Note**: Consolidation is SECONDARY - PRIMARY goal is always to learn from current execution

**Read Existing Files**:
   ` + func() string {
		if existingPath, ok := templateVars["ExistingLearningFilePath"]; ok && existingPath != "" {
			// Check if path contains multiple files (comma-separated) or is a folder
			hasMultipleFiles := strings.Contains(existingPath, ",") || strings.HasSuffix(existingPath, "/") || strings.HasSuffix(existingPath, "\\")

			if hasMultipleFiles {
				return `- **MULTIPLE EXISTING LEARNINGS FOUND**: ` + existingPath + `
   - **SECONDARY**: Consolidate all files using consolidation process from system prompt
   - **PRIMARY**: Then merge new learnings from current execution with consolidated existing learnings`
			} else {
				return `- **EXISTING LEARNINGS FOUND**: Read ` + existingPath + `
   - Merge new learnings with existing (preserve all previous content)
   - Update existing patterns if latest run differs from file
   - Update pattern scores: [Runs: X | Success: Y%]`
			}
		}
		return `- **NO EXISTING LEARNINGS**: No learning file exists for this step - DO NOT try to read or search for existing learnings
   - **Action**: Create new file at ` + writePath + `/{StepTitle}_learning.md with all current learnings`
	}() + `

**` + func() string {
		if learningDetailLevel == "exact" {
			return `8`
		}
		return `7`
	}() + `. Write Merged Content** - Priority: ` + func() string {
		if learningDetailLevel == "exact" {
			return `Execution Workflow → Data Flow → Decision Logic → Error Recovery → Failures`
		}
		return `Success tools/scripts → Guidelines → Failures`
	}() + `

## 📊 PATTERN SCORING & OPTIMAL PATH TRACKING

**Score Format**: [Runs: X | Success: Y%] ✅
- **Runs (X)**: Count of successful completions (only increment when pattern succeeds)
- **Total Attempts**: Runs + Failures (count of times pattern was tried)
- **Success Rate (Y%)**: (Runs / Total Attempts) × 100
- **Pattern Matching Logic**:
  * **Same Pattern**: Same tool/function names = same pattern (normalize to {{VARS}} for comparison)
  * **Different Pattern**: Different tool/function names = different pattern
  * **Normalization**: When comparing, normalize both patterns to {{VARS}} format first
- **Score Update Rules** (Most recent run is considered the best):
  * **When pattern works**: Increment Runs by 1, recalculate Success rate
    - Example: [Runs: 3 | Success: 75%] (3/4) → [Runs: 4 | Success: 80%] (4/5)
  * **When pattern fails**: Keep Runs same, recalculate Success rate (add 1 to total attempts)
    - Example: [Runs: 3 | Success: 75%] (3/4) → [Runs: 3 | Success: 60%] (3/5)
  * **New patterns that succeeded**: [Runs: 1 | Success: 100%]
  * **New patterns that failed**: [Runs: 0 | Success: 0%]
- Higher Runs + Success % = more reliable

**🏆 OPTIMAL PATH IDENTIFICATION (Critical for Long-Term Learning):**
- **Track Multiple Approaches**: If different tool sequences achieve the same goal, document ALL of them
- **Compare & Rank**: After multiple runs, identify which approach has highest [Runs + Success%] combination
- **Mark Optimal Path**: Add "⭐ OPTIMAL" tag to the approach with best [Runs + Success%] combination
  - **If tie**: Prefer pattern with higher Success % over higher Runs
  - **Must have**: Success % ≥ 50% (otherwise mark as ⚠️ UNRELIABLE)
- **Update Optimal Path Immediately**: When current optimal pattern fails, immediately check if another pattern should become optimal
  - Compare all patterns' [Runs + Success%] scores
  - Mark the pattern with highest score as ⭐ OPTIMAL
- **Deprecate Inferior Paths**: Mark approaches with <50% success as "⚠️ UNRELIABLE - prefer optimal path"
- **Evolution Over Time**: As more runs complete, the optimal path becomes clearer
  - Run 1-3: Multiple approaches may have similar scores
  - Run 4-10: Clear winner emerges based on consistent success
  - Run 10+: Optimal path is well-established, alternatives are documented but de-prioritized

**Example Optimal Path Tracking:**

⭐ OPTIMAL PATH [Runs: 15 | Success: 93%] - RECOMMENDED
  Step 1 → Step 2 → Step 3 (3-step approach)
  
Alternative Path A [Runs: 8 | Success: 75%]
  Step 1 → Step 2a → Step 2b → Step 3 (4-step approach)
  Note: More steps, slightly less reliable
  
⚠️ UNRELIABLE Path B [Runs: 5 | Success: 40%]
  Step 1 → Step X → Step 3 (skips validation)
  Note: Fails often due to missing validation - avoid

` + func() string {
		if learningDetailLevel == "exact" {
			return `
**WORKFLOW SCORING (Exact Mode):**
- **Track entire workflow success**, not just individual tools
- When workflow completes successfully: increment workflow Runs
- When any step fails: note which step failed, don't increment workflow Runs
- **Compare workflow variations**: If Step 2 has alternatives (2a vs 2b), track each path separately
- **Converge to optimal**: Over time, the best workflow path should have highest score`
		}
		return ``
	}() + `

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
  arguments: {COMPLETE JSON - copy exact arguments from execution history}
  prerequisites: What must be true before this step (or "None" for first step)
  outputs: What this step produces (data, state change, file, etc.)
  on_error: Specific recovery action if this step fails

**Step 2**: server.tool_name [Runs: X | Success: Y%] ✅
  arguments: {COMPLETE JSON - use {{VARIABLE}} for dynamic values}
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

**Failure Patterns**:
- Which MCP tools failed
- Which Python scripts failed
- What approaches didn't work
- Root causes of failures

**Extract**:
- Tool names (format: server_name.tool_name)
- Full Python script content (save to ` + scriptsPath + `/)
- **Output file formats** - Structure/format of JSON files and other output files created (especially files referenced in success criteria). Extract exact JSON structure, field names, data types.
- Refactor scripts to accept variables as parameters (not placeholders)
- Focus on MCP server tools (exclude workspace management tools)`
	}() + `

## 📤 REQUIRED OUTPUT

**CRITICAL**: After writing learning file, output ONLY the file path:
Updated: ` + writePath + "/" + templateVars["StepTitle"] + "_learning.md" + `

**DO NOT provide**: summaries, analysis reports, long explanations, lists of patterns

**Key Requirements**:
- ALWAYS read existing file first, merge new learnings (never overwrite)
- ALWAYS save working Python scripts to ` + scriptsPath + `/` + `
- Document learnings ONLY in ` + writePath + `/ folder
` + func() string {
		if learningDetailLevel == "exact" {
			return "- **PRESERVE WORKFLOW ORDER** - Steps must be in execution sequence\n" +
				"- **TRACK DATA FLOW** - Show how outputs connect to inputs\n" +
				"- **INCLUDE DECISION LOGIC** - Document conditional branches\n" +
				"- **DOCUMENT ERROR RECOVERY** - What to do when steps fail"
		}
		return "- Keep file content SHORT and precise (each entry 1-2 lines max)"
	}() + `
- Update pattern scores when reading existing patterns
- Analyze BOTH success and failure patterns
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
		if templateVars["VariableNames"] != "" {
			return `## 🔑 AVAILABLE VARIABLES

These variables may appear in the plan as {{VARIABLE_NAME}} placeholders:
` + templateVars["VariableNames"] + `

**CRITICAL VARIABLE HANDLING**:
1. **Preserve Placeholders**: Keep all {{VARS}} intact in learnings. DO NOT replace with actual values.
2. **Replace Actual with Variables**: When extracting tool calls from ExecutionHistory, if actual values match known variables, REPLACE them with {{VARIABLE_NAME}} placeholders
   - Example: {\"account_id\": \"123456789012\"} → {\"account_id\": \"{{AWS_ACCOUNT_ID}}\"}
3. **Python Scripts**: Refactor to accept variables as parameters (argparse/env vars), NOT using {{PLACEHOLDERS}} in code
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
6. **Update Workflow Scores** - Track success rate of the entire workflow, not just individual tools

**🏆 OPTIMAL PATH EVOLUTION (Key for Long-Term Success):**
- **First few runs**: Document all approaches that work, even if different
- **After 5+ runs**: Compare approaches, identify which has highest success rate
- **Mark the winner**: Tag the best approach as "⭐ OPTIMAL" 
- **Keep alternatives**: Document other approaches but note they're less reliable
- **Continuous improvement**: Each run refines scores and clarifies the optimal path

**MERGING WITH EXISTING LEARNINGS:**
- If workflow exists: Compare step-by-step, update scores, add new decision points
- If step order changed: Document BOTH patterns, compare their success rates
- If new errors discovered: Add to error recovery section
- If new prerequisites found: Add to prerequisites section
- If current run used different approach: Compare with existing, update optimal path if better

**🔴 LEARNING FROM FAILURES (Critical):**
- **Analyze ValidationResult**: If validation failed, understand WHY
- **Identify failing step**: Which step in the workflow caused the failure?
- **Root cause analysis**: Was it auth? Data format? Timing? Missing prerequisite?
- **Update workflow**: Add error recovery, prerequisites, or decision points to PREVENT this failure
- **Track failure count**: Increment [Failed: X times] for the failing approach
- **Document fix**: What change would make this succeed next time?
- **Deprecate bad paths**: If an approach fails >50% of the time, mark as ⚠️ UNRELIABLE

**GOAL**: After multiple runs, the OPTIMAL PATH should emerge with high confidence (90%+ success rate). 
Failures teach us what NOT to do - use them to refine the workflow until it succeeds consistently.`
		}
		return `**Remember**: 
1. Success tools/scripts come first (what worked)
2. Execution guidelines next (workflow, testing, prerequisites, pitfalls)
3. Failures last (what to avoid)
4. **Keep content SHORT** - each entry 1-2 lines max`
	}() + `

**File Handling**:
` + func() string {
		if existingPath, ok := templateVars["ExistingLearningFilePath"]; ok && existingPath != "" {
			// Check if path contains multiple files (comma-separated) or is a folder
			hasMultipleFiles := strings.Contains(existingPath, ",") || strings.HasSuffix(existingPath, "/") || strings.HasSuffix(existingPath, "\\")

			if hasMultipleFiles {
				return `1. **PRIMARY GOAL FIRST**: Extract learnings from current execution (ExecutionHistory and ValidationResult above)
   
2. **SECONDARY - CONSOLIDATE EXISTING FILES**: ` + existingPath + `
   - Follow consolidation process from system prompt (detect files, read all, consolidate patterns)
   - **Consolidation Rule**: When same pattern appears in multiple files, **keep the version with highest score** (highest [Runs + Success%])
   - **Note**: This is secondary - don't spend excessive time on consolidation
   
3. **PRIMARY - MERGE WITH NEW LEARNINGS**: ` + func() string {
					if learningDetailLevel == "exact" {
						return `Refine consolidated workflow based on latest run (PRIMARY FOCUS)`
					}
					return `Merge consolidated learnings with new patterns from latest run (PRIMARY FOCUS)`
				}() + `
   - **Pattern Matching**: Normalize both patterns to {{VARS}} format, then compare tool/function names
   - **Same pattern** (same tool/function names): Update existing pattern (scores + code/workflow if better)
   - **Different pattern**: Append as new pattern entry
   
4. **UPDATE EXISTING PATTERNS**: 
   - **If pattern worked**: Update scores (increment Runs, recalculate Success %), replace code/workflow if current is better
   - **If pattern failed**: Update scores (keep Runs same, recalculate Success %), keep existing code/workflow
   
5. **OUTDATED PATTERNS**: Remove patterns not used in current run ONLY if better alternative exists
   
6. **WRITE CONSOLIDATED FILE**: Write all consolidated learnings (from all files + new from current execution) to: ` + writePath + `/` + templateVars["StepTitle"] + `_learning.md
7. **CLEANUP (MANDATORY)**: After successfully writing consolidated file, delete ALL old files that were consolidated (use delete_workspace_file tool for each old file)`
			} else {
				return `1. **READ FIRST**: Use read_workspace_file tool to read: ` + existingPath + `
2. **PATTERN MATCHING**: Compare current run patterns with existing patterns:
   - Normalize both to {{VARS}} format before comparing
   - Same tool/function names = same pattern (update existing)
   - Different tool/function names = different pattern (append new)
3. **UPDATE EXISTING PATTERNS**:
   - **If pattern matches AND worked**: Update scores (increment Runs, recalculate Success %), replace code/workflow if current is better
   - **If pattern matches AND failed**: Update scores (keep Runs same, recalculate Success %), keep existing code/workflow
   - **If pattern is new**: Append as new pattern entry with [Runs: 1 | Success: 100%] if succeeded, or [Runs: 0 | Success: 0%] if failed
4. **OUTDATED PATTERNS**: Remove patterns not used in current run ONLY if better alternative exists
5. **PRESERVE ALL LEARNINGS**: Keep all previous learnings, only update scores and code/workflow for matching patterns`
			}
		}
		return `1. **NO EXISTING LEARNINGS**: No learning file exists for this step - DO NOT try to read or search for existing learnings
2. **CREATE NEW FILE**: Create new learning file at ` + writePath + `/` + templateVars["StepTitle"] + `_learning.md with all current learnings
3. **NEW PATTERNS**: All new patterns start with [Runs: 1 | Success: 100%] if succeeded, or [Runs: 0 | Success: 0%] if failed`
	}() + `
4. **UPDATE OPTIMAL PATH**: After updating scores, immediately check if optimal path should change (if current optimal failed, find new optimal)
5. **WRITE**: Merged content (existing + new learnings with updated scores)` + func() string {
		if existingPath, ok := templateVars["ExistingLearningFilePath"]; ok && existingPath != "" {
			hasMultipleFiles := strings.Contains(existingPath, ",") || strings.HasSuffix(existingPath, "/") || strings.HasSuffix(existingPath, "\\")
			if hasMultipleFiles {
				return `
6. **CLEANUP**: Delete all old consolidated files after successful write`
			}
		}
		return ""
	}() + `

**After writing, output ONLY the file path** (e.g., \"Updated: ` + writePath + `/` + templateVars["StepTitle"] + `_learning.md\"). 

**Keep response minimal** - just the file path. No summaries or analysis.
`
}
