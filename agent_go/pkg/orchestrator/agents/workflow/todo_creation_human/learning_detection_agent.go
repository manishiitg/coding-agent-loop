package todo_creation_human

import (
	"context"
	"fmt"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// LearningDetectionResponse represents the structured response from learning detection and consolidation analysis
type LearningDetectionResponse struct {
	HasNewLearning         bool    `json:"has_new_learning"`                // Whether new learning was detected
	Reasoning              string  `json:"reasoning"`                       // Detailed reasoning for detection
	Confidence             float64 `json:"confidence"`                      // 0.0 to 1.0 - confidence in detection
	ConsolidatedFilePath   string  `json:"consolidated_file_path"`          // Path to the consolidated learning file (if consolidation was performed)
	ConsolidationPerformed bool    `json:"consolidation_performed"`         // Whether consolidation was performed
	AnonymizationPerformed bool    `json:"anonymization_performed"`         // Whether anonymization was performed during consolidation
	AnonymizationDetails   string  `json:"anonymization_details,omitempty"` // Details about anonymization actions taken (sensitive values replaced, paths normalized, etc.)
}

// HumanControlledTodoPlannerLearningDetectionAgent detects if new learnings were generated
type HumanControlledTodoPlannerLearningDetectionAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerLearningDetectionAgent creates a new learning detection agent
func NewHumanControlledTodoPlannerLearningDetectionAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerLearningDetectionAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerLearningDetectionAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerLearningDetectionAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the learning detection agent and returns structured output
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*LearningDetectionResponse, []llmtypes.MessageContent, error) {
	// Extract variables from template (these contain the actual content, not file paths)
	previousLearningsContent := templateVars["PreviousLearningsContent"]
	currentLearningsContent := templateVars["CurrentLearningsContent"]
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	stepContextOutput := templateVars["StepContextOutput"]

	// Validate that we have content to compare (defensive check)
	previousTrimmed := strings.TrimSpace(previousLearningsContent)
	currentTrimmed := strings.TrimSpace(currentLearningsContent)

	// Check if consolidation is needed (if NewLearningFilePath is provided)
	newLearningFilePath := templateVars["NewLearningFilePath"]
	needsConsolidation := newLearningFilePath != "" && strings.TrimSpace(newLearningFilePath) != ""

	// If consolidation is needed, currentLearningsContent can be empty (agent will read consolidated file after consolidation)
	// If consolidation is NOT needed, currentLearningsContent must be provided
	if !needsConsolidation {
		// If current is empty and consolidation is not needed, return error (previous can be empty for first iteration, which is handled in detectNewLearningWithLLM)
		if currentTrimmed == "" {
			return nil, conversationHistory, fmt.Errorf("current learning content is empty - cannot perform learning detection")
		}

		// If both are empty and consolidation is not needed, return error (should not happen if called from detectNewLearningWithLLM)
		if previousTrimmed == "" && currentTrimmed == "" {
			return nil, conversationHistory, fmt.Errorf("both previous and current learning contents are empty - cannot perform learning detection")
		}
	}

	// Validate step context fields are not empty (critical for accurate detection)
	stepTitleTrimmed := strings.TrimSpace(stepTitle)
	stepDescriptionTrimmed := strings.TrimSpace(stepDescription)
	stepSuccessCriteriaTrimmed := strings.TrimSpace(stepSuccessCriteria)

	// Collect missing fields for error message
	var missingFields []string
	if stepTitleTrimmed == "" {
		missingFields = append(missingFields, "StepTitle")
	}
	if stepDescriptionTrimmed == "" {
		missingFields = append(missingFields, "StepDescription")
	}
	if stepSuccessCriteriaTrimmed == "" {
		missingFields = append(missingFields, "StepSuccessCriteria")
	}

	// If critical step context is missing, return error (detection cannot be accurate without task context)
	if len(missingFields) > 0 {
		return nil, conversationHistory, fmt.Errorf("step context fields are empty: %s - cannot perform accurate learning detection without task context", strings.Join(missingFields, ", "))
	}

	// Build schema for structured output (include consolidation fields if needed)
	schema := `{
		"type": "object",
		"properties": {
			"has_new_learning": {
				"type": "boolean",
				"description": "Whether SUBSTANTIAL new learning occurred that represents a MAJOR change. Return true ONLY if: (1) A TOTALLY NEW OPTIMAL PATH (⭐ OPTIMAL) was added, OR (2) A pattern became viable (0% → viable, or new pattern with high success), OR (3) MAJOR structural changes (fundamentally different workflows/approaches). Return false for: minor additions (single failure patterns, small code tweaks), incremental improvements, score updates, formatting changes, or minor refinements to existing patterns."
			},
			"reasoning": {
				"type": "string",
				"description": "Detailed reasoning explaining why new learning was or was not detected. Must evaluate both: (1) whether new content exists, and (2) whether it helps with the step's task execution. Reference specific differences, task relevance, and how the learning relates to the step's title, description, and success criteria."
			},
			"confidence": {
				"type": "number",
				"description": "Confidence level (0.0 to 1.0) in the detection decision. Higher values indicate more certainty.",
				"minimum": 0.0,
				"maximum": 1.0
			},
			"consolidated_file_path": {
				"type": "string",
				"description": "Path to the consolidated learning file (only set if consolidation was performed). Format: 'Updated: {path}' or just the path."
			},
			"consolidation_performed": {
				"type": "boolean",
				"description": "Whether consolidation was performed (true if NewLearningFilePath was provided and consolidation completed, false otherwise)."
			},
			"anonymization_performed": {
				"type": "boolean",
				"description": "Whether anonymization was performed during consolidation (true if sensitive values were replaced with {{VARIABLE_NAME}} placeholders, workspace paths were normalized, or patterns were normalized to {{VARS}} format). Always true if consolidation_performed is true, false otherwise."
			},
			"anonymization_details": {
				"type": "string",
				"description": "Optional details about anonymization actions taken during consolidation. Include what was anonymized: sensitive values replaced (credentials, API keys, tokens, passwords, account IDs), workspace paths normalized (hardcoded paths → {{WORKSPACE_PATH}} or relative paths), patterns normalized to {{VARS}} format. Only include if anonymization_performed is true."
			}
		},
		"required": ["has_new_learning", "reasoning", "confidence", "consolidation_performed", "anonymization_performed"]
	}`

	// Build system prompt with task context (use trimmed versions to ensure no whitespace-only content)
	usedTempLLM := templateVars["UsedTempLLM"]
	validationPassed := templateVars["ValidationPassed"]
	systemPrompt := hctplda.buildSystemPrompt(
		stepTitleTrimmed, stepDescriptionTrimmed, stepSuccessCriteriaTrimmed, stepContextDependencies, stepContextOutput,
		usedTempLLM, validationPassed,
		needsConsolidation, templateVars,
	)

	// Build user message with task context (use trimmed versions to ensure no whitespace-only content)
	userMessage := hctplda.buildUserMessage(
		previousTrimmed, currentTrimmed,
		stepTitleTrimmed, stepDescriptionTrimmed, stepSuccessCriteriaTrimmed, stepContextDependencies, stepContextOutput,
		needsConsolidation, templateVars,
	)

	// Create input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute with structured output
	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessor[LearningDetectionResponse](
		hctplda.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		schema,
		systemPrompt,
		true, // overwrite system prompt
	)

	if err != nil {
		return nil, updatedHistory, fmt.Errorf("learning detection failed: %w", err)
	}

	return &result, updatedHistory, nil
}

// buildSystemPrompt builds the system prompt for learning detection and consolidation with task context
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) buildSystemPrompt(stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput, usedTempLLM, validationPassed string, needsConsolidation bool, templateVars map[string]string) string {
	// Build tempLLM context section if tempLLM was used and validation passed
	tempLLMContext := ""
	if usedTempLLM != "" && validationPassed == "true" {
		tempLLMContext = fmt.Sprintf(`
## 🔑 CRITICAL CONTEXT: TEMPLLM SUCCESS INDICATOR

**IMPORTANT**: This step was executed successfully using %s AND validation passed (success criteria met).

**This is a STRONG SIGNAL that existing learnings are already sufficient**:
- If we can complete the step successfully with a tempLLM (which typically has less context/capability than the primary LLM) AND validation passes, it means:
  - The existing learnings contain enough information to execute this step correctly
  - The step's execution patterns are well-documented and reproducible
  - No major new learning is likely needed - the current learnings are adequate

**When evaluating learning changes in this context**:
- **Bias toward "no new learning"**: If the step succeeded with tempLLM + validation passed, existing learnings are likely sufficient
- **Only detect new learning if**: There are genuinely substantial new patterns, workflows, or knowledge that significantly improve execution beyond what tempLLM success already demonstrates
- **Minor changes should be considered "no new learning"**: Score updates, formatting, minor refinements are NOT new learning when tempLLM success already proves learnings are adequate

**Reasoning requirement**: If you determine "no new learning", explicitly mention in your reasoning that "tempLLM success with validation passed indicates existing learnings are sufficient".`, usedTempLLM)
	}

	// Build consolidation section if needed
	consolidationSection := ""
	if needsConsolidation {
		workspacePath := templateVars["WorkspacePath"]
		stepNumber := templateVars["StepNumber"]
		writePath := workspacePath + "/learnings/" + stepNumber
		newLearningFilePath := templateVars["NewLearningFilePath"]
		learningDetailLevel := templateVars["LearningDetailLevel"]
		if learningDetailLevel == "" {
			learningDetailLevel = "exact"
		}
		isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"

		executionModeCleanup := ""
		if isCodeExecutionMode {
			executionModeCleanup = `   - **Delete scripts/ folder** (code execution mode): Since this step uses code execution mode, delete the entire scripts/ subfolder if it exists - it's not used in code execution mode
     * Use delete_workspace_file tool to delete the scripts/ folder and all its contents`
		} else {
			executionModeCleanup = `   - **Delete code/ folder** (simple mode): Since this step uses simple mode, delete the entire code/ subfolder if it exists - it's not used in simple mode
     * Use delete_workspace_file tool to delete the code/ folder and all its contents`
		}

		consolidationSection = fmt.Sprintf(`
## 🔗 CONSOLIDATION PHASE (MUST BE PERFORMED FIRST)

**CRITICAL**: Before performing detection, you MUST first consolidate the new learning file with existing learnings.

### Consolidation Process:

**1. Read New Learning File**
   - Read the new learning content from: %s
   - This contains raw patterns extracted from the latest execution

**2. Read Existing Learning Files**
   - Read all existing learning files from: %s/
   - Look for ALL *_learning.md files (may be multiple - consolidate into single file)
   - Also check code/ subfolder for ALL *.go files (code execution mode - may be multiple)
   - Also check scripts/ subfolder for ALL *.py and *.sh files (simple mode - may be multiple)
   - Exclude metadata files (.learning_metadata.json)

**3. Pattern Matching & Merging**
   - **Pattern Matching Logic**: Same tool/function names = same pattern (normalize to {{VARS}} for comparison)
   - **Anonymization & Normalization** (CRITICAL):
     * **Replace sensitive values** with {{VARIABLE_NAME}} placeholders (credentials, API keys, tokens, passwords, account IDs, etc.)
     * **Workspace Path Normalization**: Replace hardcoded workspace paths in tool arguments with {{WORKSPACE_PATH}} variable or relative paths
       - **Wrong**: "filepath": "Workflow/HDFC Personal Accounts/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json"
       - **Correct**: "filepath": "{{WORKSPACE_PATH}}/runs/iteration-11/group-1/execution/step-1/step_1_credentials.json" OR "filepath": "step-1/step_1_credentials.json"
       - **Apply to**: All file paths in tool arguments (filepath, path, input_path, output_path, etc.)
     * **Pattern Normalization**: Normalize patterns to {{VARS}} format for comparison (e.g., "account_12345" → "{{ACCOUNT_ID}}")
     * **Why**: Hardcoded paths and sensitive values break reusability and security across different workspace locations
   - **Merge Duplicate Patterns**: Keep the version with highest score (highest [Runs + Success%%])
   - **Update Existing Patterns**: 
     * If pattern worked: Increment Runs, recalculate Success %%
     * If pattern failed: Keep Runs same, recalculate Success %% (add 1 to total attempts)
   - **Append New Patterns**: Add as new entries with initial scores

**4. Score Updates**
   - **Score Format**: [Runs: X | Success: Y%%]
   - **Runs (X)**: Count of successful completions (only increment when pattern succeeds)
   - **Success Rate (Y%%)**: (Runs / Total Attempts) × 100

**5. Optimal Path Identification**
   - Compare all patterns' [Runs + Success%%] scores
   - Mark pattern with highest score as ⭐ OPTIMAL
   - Deprecate patterns with <50%% success as ⚠️ UNRELIABLE

**6. Write Consolidated File**
   - Write all consolidated learnings to: %s/%s_learning.md
   - Preserve all valuable patterns, update scores, maintain format consistency

**7. Cleanup (MANDATORY)**
   - Delete the temp file: %s
   - Delete all multiple/duplicate learning files that were consolidated
%s
   - Use delete_workspace_file tool to remove all files that were consolidated

**After consolidation, read the consolidated file and use it for detection comparison.**

`, newLearningFilePath, writePath, writePath, stepTitle, newLearningFilePath, executionModeCleanup)
	}

	return fmt.Sprintf(`# Learning Detection & Consolidation Agent

You are an expert learning agent specialized in consolidating learning content AND detecting if genuinely new knowledge was generated that helps with task execution.%s

## 🎯 YOUR ROLE

**TWO-PHASE PROCESS**:
1. **CONSOLIDATION PHASE** (if NewLearningFilePath is provided): Merge new learning content with existing learnings, update scores, identify optimal paths
2. **DETECTION PHASE**: Compare consolidated result with previous learnings to detect if **genuinely new learning** occurred that **helps with the step's task execution**

Your goal is to distinguish between:
- **SUBSTANTIAL new learning**: Totally new optimal paths (⭐ OPTIMAL), patterns becoming viable (0% → viable), or major structural changes (fundamentally different approaches)
- **Minor changes (NOT new learning)**: Single failure patterns, minor code tweaks, small score updates, formatting changes, incremental improvements, or small additions to existing patterns%s%s

**CRITICAL**: Be STRICT. Only detect MAJOR, SUBSTANTIAL changes. Small additions or minor improvements should return false.

## 📋 DETECTION CRITERIA (STRICT - SUBSTANTIAL CHANGES ONLY)

### ✅ NEW LEARNING DETECTED (has_new_learning = true)

Return **true** ONLY if you detect SUBSTANTIAL, MAJOR changes that represent fundamentally new approaches:

**CRITERIA: Must meet ONE of these MAJOR change types:**

1. **TOTALLY NEW OPTIMAL PATH** (⭐ OPTIMAL):
   - A completely new pattern/workflow marked as ⭐ OPTIMAL
   - Represents a fundamentally different approach to the task
   - Has high success rate or is the best-performing pattern

2. **PATTERN BECOMES VIABLE** (Major Status Change):
   - Pattern went from 0% success (⚠️ UNRELIABLE) → viable (50%+ success)
   - Pattern went from unreliable → ⭐ OPTIMAL
   - Represents a new working method for the task

3. **MAJOR STRUCTURAL CHANGES** (Fundamentally Different Approach):
   - Complete workflow redesign (e.g., different tool chain, different execution method)
   - Major output format restructuring that changes how the task is accomplished
   - New execution paradigm (e.g., switching from simple mode to code execution mode successfully)

**IMPORTANT**: These must be SUBSTANTIAL changes. Small additions, minor refinements, or incremental improvements do NOT count.

### ❌ NOT NEW LEARNING (has_new_learning = false)

Return **false** for ANY of these (these are TOO MINOR):

- **Single failure pattern additions** - Adding one failure pattern is NOT substantial
- **Minor code tweaks** - Small code changes, removing debug statements, minor refactoring
- **Minor score updates** - [Runs: 5 → 6] with same pattern, small success rate bumps
- **Formatting/reorganization** - Reordering content, formatting changes without new approaches
- **Incremental improvements** - Small refinements to existing patterns without new approach
- **Adding details to existing patterns** - Expanding existing patterns with more examples/details
- **Metadata updates** - Timestamps, run counts, minor documentation updates
- **Repetition** - Repeating existing patterns with no new insights
- **Minor JSON structure changes** - Small field additions/changes that don't change the fundamental approach
- **Single tool call additions** - Adding one tool call to an existing pattern
- **Changes unrelated to task** - Learning about different tools/tasks

**KEY PRINCIPLE**: If the change is small enough that it doesn't represent a fundamentally different way to accomplish the task, it's NOT new learning.

## 🔍 COMPARISON PROCESS (STRICT EVALUATION)

1. **Read both files carefully** - Understand the full context of each
2. **Understand the task** - Review the step's title, description, success criteria, and overall objective
3. **Identify core patterns** - Extract the main workflows, tools, and approaches from each learning file
4. **Compare fundamentally** - Don't just look for text differences. Ask: "Is this a FUNDAMENTALLY different approach?"
5. **Check for MAJOR changes** - Look for:
   - New ⭐ OPTIMAL patterns (totally new optimal paths)
   - Patterns becoming viable (0% → viable, unreliable → optimal)
   - Major structural changes (different workflows, different execution methods)
6. **Reject minor changes** - Single failure patterns, minor code tweaks, small score updates, formatting changes
7. **Assess SUBSTANTIALITY** - Only detect if the change represents a MAJOR, SUBSTANTIAL difference in how the task can be accomplished

## 🎯 TASK CONTEXT

**Step Title**: %s
**Step Description**: %s
**Success Criteria**: %s
**Context Dependencies**: %s
**Expected Output**: %s

When evaluating learning changes, ask yourself (STRICT CRITERIA):
- **Is this a TOTALLY NEW OPTIMAL PATH?** (⭐ OPTIMAL marker for a new approach)
- **Did a pattern become VIABLE?** (0% → viable, unreliable → optimal)
- **Is this a MAJOR STRUCTURAL CHANGE?** (Fundamentally different workflow/approach)
- **Is this change SUBSTANTIAL enough?** (Not just a small addition or minor tweak)
- **Would this represent a fundamentally different way to accomplish the task?** (Not just an incremental improvement)

**If the answer to ALL of the above is NO, return has_new_learning = false**

## 📊 CONFIDENCE LEVELS

- **0.9-1.0**: Very clear new learning that helps with task OR clearly no new learning/no task relevance
- **0.7-0.9**: Strong evidence one way or the other
- **0.5-0.7**: Some uncertainty, but leaning toward a decision
- **<0.5**: High uncertainty (should be rare)

## 📝 OUTPUT REQUIREMENTS

You MUST return structured JSON with:
- **has_new_learning**: boolean - true ONLY if SUBSTANTIAL, MAJOR change detected (new optimal path, pattern becomes viable, or major structural change). False for minor additions, incremental improvements, or small tweaks.
- **reasoning**: string - Detailed explanation that clearly identifies which MAJOR change type was detected (new optimal path, pattern becomes viable, or major structural change). If no substantial change, explicitly state why the changes are too minor (e.g., "only a single failure pattern was added", "only minor code tweaks", "only score updates"). Reference the step's title, description, and success criteria.
- **confidence**: float (0.0-1.0) - Your confidence in the detection
- **consolidated_file_path**: string - Path to the consolidated learning file (only set if consolidation was performed). Format: "Updated: {path}" or just the path.
- **consolidation_performed**: boolean - true if consolidation was performed (NewLearningFilePath was provided), false otherwise
- **anonymization_performed**: boolean - true if anonymization was performed during consolidation (sensitive values replaced, paths normalized, patterns normalized), false otherwise. Always true if consolidation_performed is true.
- **anonymization_details**: string (optional) - Details about anonymization actions taken. Include: what sensitive values were replaced (credentials, API keys, tokens, passwords, account IDs), workspace paths normalized, patterns normalized to {{VARS}} format. Only include if anonymization_performed is true.

**Anonymization Requirements** (if consolidation was performed):
- **MUST** set anonymization_performed to true
- **SHOULD** include anonymization_details describing what was anonymized
- Examples of anonymization_details: "Replaced 3 API keys with {{API_KEY}} placeholders, normalized 5 workspace paths to {{WORKSPACE_PATH}}, normalized account IDs to {{ACCOUNT_ID}} format"

Focus on semantic differences AND task relevance, not just text changes.%s`, consolidationSection, stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput, tempLLMContext)
}

// buildUserMessage builds the user message with old and new learning content and task context
// Parameters contain the actual combined content of all learning files, not file paths
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) buildUserMessage(previousLearningsContent, currentLearningsContent, stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput string, needsConsolidation bool, templateVars map[string]string) string {
	var builder strings.Builder

	if needsConsolidation {
		workspacePath := templateVars["WorkspacePath"]
		stepNumber := templateVars["StepNumber"]
		writePath := workspacePath + "/learnings/" + stepNumber
		newLearningFilePath := templateVars["NewLearningFilePath"]

		builder.WriteString("# Learning Consolidation & Detection Task\n\n")
		builder.WriteString("**PRIMARY GOAL**: First consolidate new learning content with existing learnings, then detect if new learning occurred.\n\n")

		builder.WriteString("## 📋 STEP CONTEXT\n")
		builder.WriteString(fmt.Sprintf("- **Title**: %s\n", stepTitle))
		builder.WriteString(fmt.Sprintf("- **Workspace**: %s\n", workspacePath))
		builder.WriteString(fmt.Sprintf("- **Step Folder**: %s\n\n", writePath))

		builder.WriteString("## 🔗 CONSOLIDATION PHASE (MUST BE PERFORMED FIRST)\n\n")
		builder.WriteString("**1. Read New Learning File**\n")
		builder.WriteString(fmt.Sprintf("- **Path**: %s\n", newLearningFilePath))
		builder.WriteString("- **Action**: Read this file to get new patterns from latest execution\n\n")

		builder.WriteString("**2. Read Existing Learning Files**\n")
		builder.WriteString(fmt.Sprintf("- **Location**: %s/\n", writePath))
		builder.WriteString("- **Action**: Read ALL learning files from this folder:\n")
		builder.WriteString("  - All *_learning.md files (may be multiple - consolidate into single file)\n")
		if templateVars["IsCodeExecutionMode"] == "true" {
			builder.WriteString("  - code/ subfolder: All *.go files (may be multiple)\n")
		} else {
			builder.WriteString("  - scripts/ subfolder: All *.py and *.sh files (may be multiple)\n")
		}
		builder.WriteString("- **Exclude**: .learning_metadata.json (metadata files)\n\n")

		builder.WriteString("**3. Consolidation Process**\n")
		builder.WriteString("- **Anonymization** (CRITICAL):\n")
		builder.WriteString("  * Replace sensitive values (credentials, API keys, tokens, passwords, account IDs) with {{VARIABLE_NAME}} placeholders\n")
		builder.WriteString("  * Replace hardcoded workspace paths with {{WORKSPACE_PATH}} or relative paths\n")
		builder.WriteString("  * Normalize patterns to {{VARS}} format for comparison\n")
		builder.WriteString("- Merge duplicate patterns (keep version with highest score)\n")
		builder.WriteString("- Update scores: Increment Runs if pattern worked, recalculate Success % if failed\n")
		builder.WriteString("- Mark optimal paths (⭐ OPTIMAL) and deprecate unreliable ones (⚠️ UNRELIABLE)\n")
		builder.WriteString(fmt.Sprintf("- Write consolidated file to: %s/%s_learning.md\n", writePath, stepTitle))
		builder.WriteString(fmt.Sprintf("- Delete temp file: %s\n", newLearningFilePath))
		builder.WriteString("- Delete all multiple/duplicate learning files that were consolidated\n\n")

		builder.WriteString("**4. After Consolidation**\n")
		builder.WriteString("- Read the consolidated file you just created\n")
		builder.WriteString("- Use it as the CURRENT learnings content for detection comparison\n\n")
	} else {
		builder.WriteString("Compare the following learning content to determine if new learning occurred that helps with task execution:\n\n")
	}

	// Add task context first so agent understands what to evaluate against
	builder.WriteString("## TASK CONTEXT\n\n")
	builder.WriteString("**Step Title**: ")
	if stepTitle != "" {
		builder.WriteString(stepTitle)
	} else {
		builder.WriteString("(Not provided)")
	}
	builder.WriteString("\n\n")

	builder.WriteString("**Step Description**: ")
	if stepDescription != "" {
		builder.WriteString(stepDescription)
	} else {
		builder.WriteString("(Not provided)")
	}
	builder.WriteString("\n\n")

	builder.WriteString("**Success Criteria**: ")
	if stepSuccessCriteria != "" {
		builder.WriteString(stepSuccessCriteria)
	} else {
		builder.WriteString("(Not provided)")
	}
	builder.WriteString("\n\n")

	if stepContextDependencies != "" {
		builder.WriteString("**Context Dependencies**: ")
		builder.WriteString(stepContextDependencies)
		builder.WriteString("\n\n")
	}

	if stepContextOutput != "" {
		builder.WriteString("**Expected Output**: ")
		builder.WriteString(stepContextOutput)
		builder.WriteString("\n\n")
	}

	builder.WriteString("## PREVIOUS LEARNINGS CONTENT\n")
	if previousLearningsContent == "" || strings.TrimSpace(previousLearningsContent) == "" {
		builder.WriteString("(No previous learnings - this is the first iteration)\n")
	} else {
		builder.WriteString("```\n")
		builder.WriteString(previousLearningsContent)
		builder.WriteString("\n```\n")
	}

	builder.WriteString("\n## CURRENT LEARNINGS CONTENT\n")
	if currentLearningsContent == "" || strings.TrimSpace(currentLearningsContent) == "" {
		builder.WriteString("(No current learnings - cannot compare)\n")
	} else {
		builder.WriteString("```\n")
		builder.WriteString(currentLearningsContent)
		builder.WriteString("\n```\n")
	}

	builder.WriteString("\n## ANALYSIS TASK (STRICT EVALUATION)\n")
	builder.WriteString("Analyze these learning contents and determine if SUBSTANTIAL, MAJOR changes occurred:\n")
	builder.WriteString("1. **TOTALLY NEW OPTIMAL PATH** (⭐ OPTIMAL marker for a completely new approach)\n")
	builder.WriteString("2. **PATTERN BECOMES VIABLE** (0% → viable, unreliable → optimal)\n")
	builder.WriteString("3. **MAJOR STRUCTURAL CHANGE** (Fundamentally different workflow/approach)\n\n")
	builder.WriteString("**REJECT** if you see: single failure patterns, minor code tweaks, small score updates, formatting changes, incremental improvements, or small additions.\n\n")
	builder.WriteString("Return your analysis as structured JSON with has_new_learning, reasoning, and confidence.\n")
	builder.WriteString("Your reasoning must clearly identify which MAJOR change type was detected, OR explicitly state why changes are too minor to count as new learning.\n")

	return builder.String()
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for learning detection agent - use ExecuteStructured() instead")
}
