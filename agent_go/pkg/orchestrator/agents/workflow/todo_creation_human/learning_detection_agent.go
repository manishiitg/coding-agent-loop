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

	// Current learnings content must be provided (extraction agent already consolidated)
	if currentTrimmed == "" {
		return nil, conversationHistory, fmt.Errorf("current learning content is empty - cannot perform learning detection")
	}

	// If both are empty, return error (should not happen if called from detectNewLearningWithLLM)
	if previousTrimmed == "" && currentTrimmed == "" {
		return nil, conversationHistory, fmt.Errorf("both previous and current learning contents are empty - cannot perform learning detection")
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
				"description": "Path to the consolidated learning file (always empty - consolidation is handled by extraction agent)."
			},
			"consolidation_performed": {
				"type": "boolean",
				"description": "Always false - consolidation is handled by extraction agent, not detection agent."
			},
			"anonymization_performed": {
				"type": "boolean",
				"description": "Always false - anonymization is handled by extraction agent during consolidation."
			},
			"anonymization_details": {
				"type": "string",
				"description": "Always empty - anonymization is handled by extraction agent."
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
	)

	// Build user message with task context (use trimmed versions to ensure no whitespace-only content)
	userMessage := hctplda.buildUserMessage(
		previousTrimmed, currentTrimmed,
		stepTitleTrimmed, stepDescriptionTrimmed, stepSuccessCriteriaTrimmed, stepContextDependencies, stepContextOutput,
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

// buildSystemPrompt builds the system prompt for learning detection with task context
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) buildSystemPrompt(stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput, usedTempLLM, validationPassed string) string {
	// Build tempLLM context section if tempLLM was used and validation passed
	tempLLMContext := ""
	if usedTempLLM != "" && validationPassed == "true" {
		tempLLMContext = fmt.Sprintf(`
## CRITICAL: TEMPLLM SUCCESS
Step executed successfully using %s AND validation passed. This indicates existing learnings are sufficient.

**Impact**: Bias toward "no new learning". Only detect if genuinely substantial new patterns/workflows that significantly improve beyond tempLLM success. Minor changes (score updates, formatting, tweaks) are NOT new learning.

**Reasoning requirement**: If "no new learning", explicitly mention "tempLLM success with validation passed indicates existing learnings are sufficient" and explain why changes are too minor.`, usedTempLLM)
	}

	// Consolidation is now handled by extraction agents - detection agent only compares before/after

	return fmt.Sprintf(`# Learning Detection Agent

Expert agent for detecting substantial new knowledge that helps task execution.

## PROCESS
**DETECTION ONLY**: Compare previous learnings (before extraction agent ran) vs current learnings (after extraction agent consolidated) to detect genuinely new learning.

**PRINCIPLES**: Focus on MCP tool patterns (not workspace operations), strict detection (MAJOR changes only - be conservative)

**Goal**: Distinguish SUBSTANTIAL learning (new ⭐ OPTIMAL paths, patterns becoming viable 0%%→50%%+, major structural changes) from MINOR changes (single failures, code tweaks, score updates, formatting, incremental improvements)

%s

**RULE**: Be STRICT. Only detect MAJOR changes. When in doubt, return false.

## DETECTION CRITERIA

**Return true ONLY if**: (1) Fundamentally different approach, (2) Substantial impact on execution, (3) Helps with task execution. Must meet ONE:
- **New ⭐ OPTIMAL path**: Completely new optimal pattern/workflow
- **Pattern becomes viable**: 0%% → 50%%+ success, or unreliable → ⭐ OPTIMAL
- **Major structural change**: Different MCP tool chain, execution method, or paradigm shift

**Return false for**: Single failures, code tweaks, score updates, formatting, incremental improvements, metadata updates, workspace tool operations, changes unrelated to task. **When in doubt, return false.**

## COMPARISON PROCESS
1. **Context**: Read both files, understand task (title, description, success criteria)
2. **Extract**: Identify MCP tool patterns (ignore workspace tools), map tool chains
3. **Compare**: Is this fundamentally different? Compare MCP tool chains and execution methods
4. **Classify**: Check for new ⭐ OPTIMAL, patterns becoming viable, major structural changes. Reject minor changes.
5. **Assess**: Is change substantial? Does it help task execution? Significant impact?
6. **Decide**: Apply strict criteria, when in doubt return false, document reasoning

## TASK CONTEXT
**Step Title**: %s
**Step Description**: %s (CURRENT - SOURCE OF TRUTH, remove learnings that don't match)
**Success Criteria**: %s
**Context Dependencies**: %s
**Expected Output**: %s

**Evaluation Checklist** (answer in reasoning): (1) New ⭐ OPTIMAL path? (2) Pattern became viable? (3) Major structural change? (4) Substantial enough? (5) Fundamentally different approach? (6) Helps task execution?

**Decision**: If ALL NO → false. If ANY YES + substantial → true. When in doubt → false.

## OUTPUT REQUIREMENTS

**Required fields**:
- **has_new_learning** (boolean): true ONLY if substantial major change (new ⭐ OPTIMAL, pattern viable, major structural). false for minor changes or when in doubt.
- **reasoning** (string): Summary, answer evaluation checklist, specify change type if detected, task relevance, specific examples, conclusion. If no change, state why too minor. 3-5 sentences min.
- **confidence** (float 0.0-1.0): 0.9-1.0=very clear, 0.7-0.9=strong evidence, 0.5-0.7=some uncertainty, <0.5=high uncertainty (return false)
- **consolidation_performed** (boolean): true if consolidation done, false otherwise
- **anonymization_performed** (boolean): true if anonymization done (always true if consolidation_performed true), false otherwise

**Optional fields**:
- **consolidated_file_path** (string): Set if consolidation_performed true. Format: "Updated: {path}" or path
- **anonymization_details** (string): Set if anonymization_performed true. Describe what was anonymized (sensitive values, paths, patterns)

**Reasoning quality**: Be specific (patterns/tools), comprehensive (all checklist questions), clear, task-focused, honest (state uncertainty).

Focus on semantic differences AND task relevance, not just text changes.

`, stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput, tempLLMContext)
}

// buildUserMessage builds the user message with old and new learning content and task context
// Parameters contain the actual combined content of all learning files, not file paths
// Note: Consolidation is now handled by extraction agents, so this only performs detection comparison
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) buildUserMessage(previousLearningsContent, currentLearningsContent, stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput string) string {
	var builder strings.Builder

	builder.WriteString("Compare the following learning content to determine if new learning occurred that helps with task execution:\n\n")

	// Task context is already in system prompt, just reference it here
	builder.WriteString("## TASK CONTEXT\n\n")
	builder.WriteString("**Use the task context provided in the system prompt** (Step Title, Description, Success Criteria, Context Dependencies, Expected Output) for plan alignment and detection evaluation.\n\n")

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

	builder.WriteString("\n## 📊 ANALYSIS TASK\n\n")
	builder.WriteString("**Your Task**: Determine if SUBSTANTIAL, MAJOR changes occurred that represent new learning.\n\n")
	builder.WriteString("### Instructions:\n")
	builder.WriteString("1. **Follow the detection criteria** in the system prompt (strict evaluation)\n")
	builder.WriteString("2. **Follow the comparison process** step-by-step (systematic evaluation)\n")
	builder.WriteString("3. **Answer the evaluation checklist** questions explicitly\n")
	builder.WriteString("4. **Compare the learning contents** above (previous vs current)\n")
	builder.WriteString("5. **Return structured JSON** with has_new_learning, reasoning, and confidence\n\n")
	builder.WriteString("### Reasoning Requirements:\n")
	builder.WriteString("- **If new learning detected**: Clearly identify which MAJOR change type was detected:\n")
	builder.WriteString("  - New optimal path (⭐ OPTIMAL)\n")
	builder.WriteString("  - Pattern becomes viable (0%% → viable, unreliable → optimal)\n")
	builder.WriteString("  - Major structural change (fundamentally different approach)\n")
	builder.WriteString("- **If no new learning**: Explicitly state why changes are too minor to count as new learning\n")
	builder.WriteString("- **Reference task context**: Explain how the change relates (or doesn't relate) to the step's objective\n")
	builder.WriteString("- **Be specific**: Reference exact patterns, tools, and differences\n\n")
	builder.WriteString("**Remember**: Be STRICT and CONSERVATIVE. When in doubt, return false.\n")

	return builder.String()
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for learning detection agent - use ExecuteStructured() instead")
}
