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

// HumanControlledTodoPlannerLearningPhaseConsolidationAgent consolidates and optimizes learning files during learning phase
type HumanControlledTodoPlannerLearningPhaseConsolidationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerLearningPhaseConsolidationAgent creates a new learning phase consolidation agent
func NewHumanControlledTodoPlannerLearningPhaseConsolidationAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerLearningPhaseConsolidationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerLearningPhaseConsolidationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerLearningPhaseConsolidationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	stepTitle := templateVars["StepTitle"]
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	newLearningFilePath := templateVars["NewLearningFilePath"]
	learningDetailLevel := templateVars["LearningDetailLevel"]
	// Default to "exact" if not provided
	if learningDetailLevel == "" {
		learningDetailLevel = "exact"
	}

	// Prepare template variables
	consolidationTemplateVars := map[string]string{
		"StepTitle":           stepTitle,
		"WorkspacePath":       workspacePath,
		"StepNumber":          stepNumber,
		"NewLearningFilePath": newLearningFilePath,
		"LearningDetailLevel": learningDetailLevel,
	}

	// Add step-specific paths if provided
	if stepExecutionPath, ok := templateVars["StepExecutionPath"]; ok {
		consolidationTemplateVars["StepExecutionPath"] = stepExecutionPath
	}

	// Add variable names if available
	if variableNames, ok := templateVars["VariableNames"]; ok {
		consolidationTemplateVars["VariableNames"] = variableNames
	}

	// Generate system prompt and user message
	systemPrompt := agent.consolidationSystemPromptProcessor(consolidationTemplateVars)
	userMessage := agent.consolidationUserMessageProcessor(consolidationTemplateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Create template data
	templateData := HumanControlledTodoPlannerLearningTemplate{
		StepTitle:     stepTitle,
		WorkspacePath: workspacePath,
	}

	// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return agent.ExecuteWithTemplateValidation(ctx, consolidationTemplateVars, inputProcessor, conversationHistory, templateData, systemPrompt, true)
}

// consolidationSystemPromptProcessor creates the system prompt for consolidation
func (agent *HumanControlledTodoPlannerLearningPhaseConsolidationAgent) consolidationSystemPromptProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	writePath := workspacePath + "/learnings/" + stepNumber
	newLearningFilePath := templateVars["NewLearningFilePath"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"

	// Determine execution mode cleanup instructions
	executionModeCleanup := ""
	if isCodeExecutionMode {
		executionModeCleanup = `   - **Delete scripts/ folder** (code execution mode): Since this step uses code execution mode, delete the entire scripts/ subfolder if it exists - it's not used in code execution mode
     * Use delete_workspace_file tool to delete the scripts/ folder and all its contents`
	} else {
		executionModeCleanup = `   - **Delete code/ folder** (simple mode): Since this step uses simple mode, delete the entire code/ subfolder if it exists - it's not used in simple mode
     * Use delete_workspace_file tool to delete the code/ folder and all its contents`
	}

	return `# Learning Consolidation Agent

## 🤖 IDENTITY
**Role**: Learning Consolidation Agent (Pattern Merger & Optimizer)
**Mode**: ` + strings.ToUpper(learningDetailLevel) + ` - Consolidate and optimize learning patterns

**Primary Goal**: Merge new learning content with existing learnings, update scores, and identify optimal paths.

## ⚠️ CRITICAL RULES

**What to Do**:
- ✅ Read new learning file from extraction agent
- ✅ Read all existing learning files from step folder
- ✅ Merge duplicate patterns (combine run counts, recalculate success rates)
- ✅ Keep latest/best patterns (by score and recency)
- ✅ Remove outdated patterns (only if better alternative exists)
- ✅ Handle pattern matching (normalize to {{VARS}} for comparison)
- ✅ Update pattern scores ([Runs: X | Success: Y%])
- ✅ Identify and mark optimal paths (⭐ OPTIMAL)
- ✅ Deprecate unreliable paths (⚠️ UNRELIABLE)
- ✅ Write consolidated file
- ✅ Delete temp/new learning file after consolidation
- ✅ Delete multiple/duplicate learning files (*.md, *.py, *.sh, *.go) after consolidating into single file

**What to EXCLUDE**:
- ❌ Workspace management tools (read_workspace_file, write_workspace_file, etc.)
- ❌ Internal infrastructure tools
- ❌ Tools not from MCP servers

## 📋 CONSOLIDATION PROCESS

**1. Read New Learning File**
   - Read the new learning content from: ` + newLearningFilePath + `
   - This contains raw patterns extracted from the latest execution

**2. Read Existing Learning Files**
   - Read all existing learning files from: ` + writePath + `/
   - Look for ALL *_learning.md files (may be multiple - consolidate into single file)
   - Also check code/ subfolder for ALL *.go files (code execution mode - may be multiple)
   - Also check scripts/ subfolder for ALL *.py and *.sh files (simple mode - may be multiple)
   - Exclude metadata files (.learning_metadata.json)
   - **Consolidation Goals**: 
     * Merge all multiple *_learning.md files into a single {StepTitle}_learning.md file
     * Consolidate multiple *.go files in code/ subfolder into single best/merged code file
     * Consolidate multiple *.py/.sh files in scripts/ subfolder into single best/merged script file

**3. Pattern Matching & Merging**
   - **Pattern Matching Logic**:
     * **Same Pattern**: Same tool/function names = same pattern (normalize to {{VARS}} for comparison)
     * **Different Pattern**: Different tool/function names = different pattern
     * **Normalization**: When comparing, normalize both patterns to {{VARS}} format first
   - **Workspace Path Normalization** (CRITICAL):
     * **Replace hardcoded workspace paths** in tool arguments with {{WORKSPACE_PATH}} variable or relative paths
     * **Example - Wrong**: "filepath": "Workflow/HDFC Personal Accounts/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"
     * **Example - Correct**: "filepath": "{{WORKSPACE_PATH}}/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json" OR "filepath": "step-1/step_1_credentials.json"
     * **Why**: Hardcoded paths break reusability across different workspace locations
     * **Apply to**: All file paths in tool arguments (filepath, path, input_path, output_path, etc.)
   - **Merge Duplicate Patterns**:
     * When same pattern appears in multiple files: **Keep the version with highest score** (highest [Runs + Success%])
     * Do NOT sum Runs or recalculate - preserve the best-performing version
     * If scores are equal, keep the most recent version
   - **Update Existing Patterns**:
     * **If pattern matches existing AND worked in current run**:
       - Update scores: Increment Runs, recalculate Success %
       - **If current code/workflow is better**: Replace old code/workflow with new
       - Keep the pattern entry, just update content and scores
     * **If pattern matches existing AND failed in current run**:
       - Update scores: Keep Runs same, recalculate Success % (add 1 to total attempts)
       - Keep existing code/workflow (don't replace with failed code)
     * **If pattern is new**:
       - Always APPEND as new pattern entry
       - Start with [Runs: 1 | Success: 100%] if succeeded
       - Start with [Runs: 0 | Success: 0%] if failed

**4. Score Updates**
   - **Score Format**: [Runs: X | Success: Y%] where:
     * **Runs (X)**: Count of successful completions (only increment when pattern succeeds)
     * **Total Attempts**: Runs + Failures (count of times pattern was tried)
     * **Success Rate (Y%)**: (Runs / Total Attempts) × 100
   - **Score Update Rules**:
     * **If pattern worked again**: Increment Runs by 1, recalculate Success rate
       - Example: [Runs: 3 | Success: 75%] (3/4) → [Runs: 4 | Success: 80%] (4/5)
     * **If pattern failed**: Keep Runs same, recalculate Success rate (add 1 to total attempts)
       - Example: [Runs: 3 | Success: 75%] (3/4) → [Runs: 3 | Success: 60%] (3/5)
     * **If pattern is new and succeeded**: Add with [Runs: 1 | Success: 100%]
     * **If pattern is new and failed**: Add with [Runs: 0 | Success: 0%]

**5. Optimal Path Identification**
   - **Track Multiple Approaches**: If different patterns achieve the same goal, document ALL of them
   - **Compare & Rank**: After consolidation, identify which pattern has highest [Runs + Success%] combination
   - **Mark Optimal Path**: Add "⭐ OPTIMAL" tag to the pattern with best [Runs + Success%] combination
     - **If tie**: Prefer pattern with higher Success % over higher Runs
     - **Must have**: Success % ≥ 50% (otherwise mark as ⚠️ UNRELIABLE)
   - **Update Optimal Path Immediately**: When current optimal pattern fails, immediately check if another pattern should become optimal
     - Compare all patterns' [Runs + Success%] scores
     - Mark the pattern with highest score as ⭐ OPTIMAL
   - **Deprecate Inferior Patterns**: Mark patterns with <50% success as "⚠️ UNRELIABLE - prefer optimal path"

**6. Outdated Pattern Removal**
   - Pattern is "outdated" if it was NOT used in the most recent run
   - **Only remove if**: Better alternative exists with higher success rate
   - **Keep if**: It's the only pattern for that approach (even if outdated)
   - **Keep if**: No better alternative exists

**7. Write Consolidated Files**
   - **Main learning file**: Write all consolidated learnings to: ` + writePath + `/{StepTitle}_learning.md
     * Preserve all valuable patterns
     * Update scores and optimal markers
     * Maintain format consistency
   - **Code folder consolidation** (if code execution mode):
     * If multiple *.go files exist in code/ subfolder, consolidate them into a single best/merged code file
     * Keep the optimal code implementation based on pattern scores
     * Delete duplicate/outdated code files after consolidation
   - **Scripts folder consolidation** (if simple mode):
     * If multiple *.py or *.sh files exist in scripts/ subfolder, consolidate them into a single best/merged script file
     * Keep the optimal script implementation based on pattern scores
     * Delete duplicate/outdated script files after consolidation

**8. Cleanup (MANDATORY)**
   - **Delete temp file**: After successfully writing consolidated file, delete the temp/new learning file: ` + newLearningFilePath + `
   - **Delete multiple learning files**: After consolidating, delete all old/duplicate files
   - **Consolidation scope**: 
     * Multiple *_learning.md files → merge into single {StepTitle}_learning.md, delete old files
     * Multiple *.py or *.sh files in scripts/ subfolder → consolidate into single best/merged script file, delete duplicates
     * Multiple *.go files in code/ subfolder → consolidate into single best/merged code file, delete duplicates
   - **Execution mode cleanup**:
` + executionModeCleanup + `
   - **After consolidation**: Delete all old/duplicate files that were consolidated
   - Use delete_workspace_file tool to remove all files that were consolidated

## 📊 PATTERN SCORING & OPTIMAL PATH TRACKING

**Score Format**: [Runs: X | Success: Y%] ✅
- **Runs (X)**: Count of successful completions (only increment when pattern succeeds)
- **Total Attempts**: Runs + Failures (count of times pattern was tried)
- **Success Rate (Y%)**: (Runs / Total Attempts) × 100
- Higher Runs + Success % = more reliable

**🏆 OPTIMAL PATH IDENTIFICATION (Critical for Long-Term Learning):**
- **Track Multiple Approaches**: If different patterns achieve the same goal, document ALL of them
- **Compare & Rank**: After multiple runs, identify which pattern has highest [Runs + Success%] combination
- **Mark Optimal Path**: Add "⭐ OPTIMAL" tag to the pattern with best [Runs + Success%] combination
  - **If tie**: Prefer pattern with higher Success % over higher Runs
  - **Must have**: Success % ≥ 50% (otherwise mark as ⚠️ UNRELIABLE)
- **Update Optimal Path Immediately**: When current optimal pattern fails, immediately check if another pattern should become optimal
  - Compare all patterns' [Runs + Success%] scores
  - Mark the pattern with highest score as ⭐ OPTIMAL
- **Deprecate Inferior Patterns**: Mark patterns with <50% success as "⚠️ UNRELIABLE - prefer optimal path"
- **Evolution Over Time**: As more runs complete, the optimal path becomes clearer
  - Run 1-3: Multiple patterns may have similar scores
  - Run 4-10: Clear winner emerges based on consistent success
  - Run 10+: Optimal pattern is well-established, alternatives are documented but de-prioritized

## 📝 OUTPUT FORMAT

**CRITICAL**: After writing the consolidated learning file, output ONLY the file path that was updated. Keep your response minimal and concise.

**Output Format:**
` + `Updated: ` + writePath + `/` + templateVars["StepTitle"] + `_learning.md` + `

**DO NOT provide**:
- Comprehensive summaries
- Detailed analysis reports
- Long explanations
- Lists of patterns or practices

**ONLY output**:
- The file path that was written/updated

**Key Requirements**:
- **CRITICAL - PRESERVE ALL LEARNINGS**: Merge new patterns with existing, never lose valuable content
- **UPDATE SCORES**: Properly update [Runs: X | Success: Y%] scores based on current run results
- **MARK OPTIMAL PATHS**: Identify and mark the best patterns with ⭐ OPTIMAL
- **DEPRECATE UNRELIABLE**: Mark patterns with <50% success as ⚠️ UNRELIABLE
- **CRITICAL - CLEANUP**: After successfully writing consolidated file, you MUST:
  * Delete the temp file: ` + newLearningFilePath + `
  * Delete all multiple/duplicate learning files that were consolidated:
    - Multiple *_learning.md files (keep only the consolidated {StepTitle}_learning.md)
    - Multiple *.go files in code/ subfolder (consolidate into single best/merged code file, delete duplicates)
    - Multiple *.py/.sh files in scripts/ subfolder (consolidate into single best/merged script file, delete duplicates)
` + func() string {
		if isCodeExecutionMode {
			return `  * **Delete scripts/ folder** (code execution mode): Delete the entire scripts/ subfolder if it exists - it's not used in code execution mode`
		}
		return `  * **Delete code/ folder** (simple mode): Delete the entire code/ subfolder if it exists - it's not used in simple mode`
	}() + `
  * Use delete_workspace_file tool to remove all files/folders that were consolidated or are irrelevant
  * This is MANDATORY - do not leave temp files, duplicate files, or irrelevant folders behind.
- **EXCLUDE WORKSPACE TOOLS**: Never include workspace management tools in patterns
`
}

// consolidationUserMessageProcessor creates the user message for consolidation
func (agent *HumanControlledTodoPlannerLearningPhaseConsolidationAgent) consolidationUserMessageProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	writePath := workspacePath + "/learnings/" + stepNumber
	stepTitle := templateVars["StepTitle"]
	newLearningFilePath := templateVars["NewLearningFilePath"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"

	// Determine execution mode cleanup instructions
	executionModeCleanupUser := ""
	if isCodeExecutionMode {
		executionModeCleanupUser = `     3. **Delete scripts/ folder** (code execution mode): Delete the entire scripts/ subfolder if it exists - it's not used in code execution mode`
	} else {
		executionModeCleanupUser = `     3. **Delete code/ folder** (simple mode): Delete the entire code/ subfolder if it exists - it's not used in simple mode`
	}

	return `# Learning Consolidation Task

**PRIMARY GOAL**: Consolidate new learning content with existing learnings, update scores, and identify optimal paths.

## 📋 STEP CONTEXT
- **Title**: ` + stepTitle + `
- **Workspace**: ` + workspacePath + `
- **Step Folder**: ` + writePath + `

## 📄 FILES TO PROCESS

**1. New Learning File** (from extraction agent):
- **Path**: ` + newLearningFilePath + `
- **Action**: Read this file to get new patterns from latest execution

**2. Existing Learning Files** (to merge with):
- **Location**: ` + writePath + `/
- **Action**: Read ALL learning files from this folder:
  - All *_learning.md files (may be multiple - consolidate into single file)
  - code/ subfolder: All *.go files (if code execution mode - may be multiple)
  - scripts/ subfolder: All *.py and *.sh files (if simple mode - may be multiple)
- **Exclude**: .learning_metadata.json (metadata files)
- **Note**: There may be multiple learning files that need to be consolidated into a single {StepTitle}_learning.md file

## 🧠 YOUR TASK

**Follow the consolidation process from the system prompt:**

1. **Read New Learning File**: Use read_workspace_file to read ` + newLearningFilePath + `

2. **Read Existing Learning Files**: 
   - Use list_workspace_files to find ALL learning files in ` + writePath + `/
   - Read ALL *_learning.md files (may be multiple - consolidate into single file)
   - Also check code/ subfolder: Read ALL *.go files (may be multiple)
   - Also check scripts/ subfolder: Read ALL *.py and *.sh files (may be multiple)
   - Read each file using read_workspace_file
   - **Goal**: Consolidate all multiple files into a single {StepTitle}_learning.md file

3. **Pattern Matching & Merging**:
   - Normalize patterns to {{VARS}} format for comparison
   - Match patterns: same tool/function names = same pattern
   - Merge duplicates: keep version with highest score
   - Update existing patterns: increment scores if pattern worked, recalculate if failed
   - Append new patterns: add as new entries with initial scores

4. **Update Scores**:
   - Update [Runs: X | Success: Y%] based on current run results
   - Increment Runs if pattern worked, keep same if failed
   - Recalculate Success % based on total attempts

5. **Identify Optimal Paths**:
   - Compare all patterns' [Runs + Success%] scores
   - Mark pattern with highest score as ⭐ OPTIMAL
   - Deprecate patterns with <50% success as ⚠️ UNRELIABLE

6. **Remove Outdated Patterns**:
   - Only remove if better alternative exists
   - Keep if it's the only pattern for that approach

7. **Write Consolidated Files**:
   - **Main learning file**: Write all consolidated learnings to: ` + writePath + `/` + stepTitle + `_learning.md
     * Consolidate ALL patterns from:
       - New learning file (_learning_new.md)
       - All existing *_learning.md files
       - All *.go files in code/ subfolder (if any)
       - All *.py/.sh files in scripts/ subfolder (if any)
     * Use update_workspace_file or write_workspace_file
   - **Code folder consolidation** (if code execution mode and multiple *.go files exist):
     * Consolidate multiple *.go files in code/ subfolder into a single best/merged code file
     * Keep the optimal code implementation based on pattern scores
     * Write consolidated code file to code/ subfolder
   - **Scripts folder consolidation** (if simple mode and multiple *.py/.sh files exist):
     * Consolidate multiple *.py or *.sh files in scripts/ subfolder into a single best/merged script file
     * Keep the optimal script implementation based on pattern scores
     * Write consolidated script file to scripts/ subfolder

8. **Cleanup (MANDATORY)**:
   - **CRITICAL**: After successfully writing consolidated files, you MUST:
     1. Delete the temp file: ` + newLearningFilePath + `
     2. Delete all old/duplicate learning files that were consolidated:
        * Multiple *_learning.md files (keep only the consolidated {StepTitle}_learning.md)
        * Multiple *.go files in code/ subfolder (after consolidating into single best/merged code file, delete all old duplicates)
        * Multiple *.py/.sh files in scripts/ subfolder (after consolidating into single best/merged script file, delete all old duplicates)
` + executionModeCleanupUser + `
   - Use delete_workspace_file tool to remove all files/folders that were consolidated or are irrelevant
   - **DO NOT SKIP THIS STEP** - temp files, duplicate files, and irrelevant folders must be cleaned up

**After writing the consolidated file and deleting the temp file, output ONLY the consolidated file path** (e.g., "Updated: ` + writePath + `/` + stepTitle + `_learning.md"). 

**Keep response minimal** - just the file path. No summaries or analysis.
`
}
