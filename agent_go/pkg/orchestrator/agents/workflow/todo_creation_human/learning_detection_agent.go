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

// LearningDetectionResponse represents the structured response from learning detection analysis
type LearningDetectionResponse struct {
	HasNewLearning bool    `json:"has_new_learning"`
	Reasoning      string  `json:"reasoning"`
	Confidence     float64 `json:"confidence"` // 0.0 to 1.0
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
	taskObjective := templateVars["TaskObjective"]

	// Build schema for structured output
	schema := `{
		"type": "object",
		"properties": {
			"has_new_learning": {
				"type": "boolean",
				"description": "Whether genuinely new learning occurred AND if it helps with task execution. Return true only if: (1) new patterns, workflows, failure analysis, or knowledge were added, AND (2) these changes are relevant to the step's task (title, description, success criteria). Return false if only score updates, formatting changes, repetition, or if changes don't help with the actual task."
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
			}
		},
		"required": ["has_new_learning", "reasoning", "confidence"]
	}`

	// Build system prompt with task context
	systemPrompt := hctplda.buildSystemPrompt(stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput, taskObjective)

	// Build user message with task context
	userMessage := hctplda.buildUserMessage(previousLearningsContent, currentLearningsContent, stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput, taskObjective)

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
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) buildSystemPrompt(stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput, taskObjective string) string {
	return fmt.Sprintf(`# Learning Detection Agent

You are an expert learning detection agent specialized in analyzing learning content to determine if genuinely new knowledge was generated AND if it helps with task execution.

## 🎯 YOUR ROLE

Compare old and new learning content to detect if **genuinely new learning** occurred that **helps with the step's task execution**. Your goal is to distinguish between:
- **New learning that helps**: New patterns, workflows, failure analysis, or knowledge that is relevant to the step's task
- **No new learning or irrelevant learning**: Only score updates, formatting changes, repetition, or changes that don't help with the actual task

## 📋 DETECTION CRITERIA

### ✅ NEW LEARNING DETECTED (has_new_learning = true)

Return **true** ONLY if BOTH conditions are met:
1. **New content exists**: New patterns, workflows, failure analysis, or knowledge were added
2. **Task relevance**: The changes help with executing the step's task (relate to title, description, success criteria)

Examples of valid new learning:
- **New code patterns/workflows** relevant to the step's task
- **New failure patterns** with root causes that relate to the step's success criteria
- **New optimal path markers** (⭐ OPTIMAL) for approaches that help achieve the step's goal
- **New prerequisites/error recovery strategies** that help complete the step successfully
- **New output file formats** that match the step's context output requirements
- **Significant score improvements** for patterns that help with the step's task
- **New tool combinations** that are relevant to the step's description

### ❌ NOT NEW LEARNING (has_new_learning = false)

Return **false** if you see:
- **Minor score updates** (e.g., [Runs: 5 → 6] with same pattern)
- **Formatting changes** or reorganization without new content
- **Repetition of existing patterns** with no new insights
- **Refinements to existing code** without new approaches
- **Reordering of content** without adding new knowledge
- **Only metadata changes** (timestamps, run counts) without new patterns
- **Changes unrelated to the step's task** - Learning about different tools/tasks that don't help with this step
- **Generic learnings** that don't address the specific step's requirements

## 🔍 COMPARISON PROCESS

1. **Read both files carefully** - Understand the full context of each
2. **Understand the task** - Review the step's title, description, success criteria, and overall objective
3. **Identify core patterns** - Extract the main workflows, tools, and approaches from each learning file
4. **Compare semantically** - Don't just look for text differences, compare the actual knowledge
5. **Evaluate task relevance** - Determine if the changes help with the step's specific task
6. **Check for new insights** - Look for new failure analysis, new recovery strategies, new optimal paths
7. **Assess significance** - Minor updates vs. substantial new knowledge that helps with task execution

## 🎯 TASK CONTEXT

**Overall Objective**: %s

**Step Title**: %s
**Step Description**: %s
**Success Criteria**: %s
**Context Dependencies**: %s
**Expected Output**: %s

When evaluating learning changes, ask yourself:
- Does this learning help achieve the step's success criteria?
- Is this learning relevant to the step's description and title?
- Would this learning improve the execution of this specific step?
- Is this learning about the right tools/approaches for this step's task?

## 📊 CONFIDENCE LEVELS

- **0.9-1.0**: Very clear new learning that helps with task OR clearly no new learning/no task relevance
- **0.7-0.9**: Strong evidence one way or the other
- **0.5-0.7**: Some uncertainty, but leaning toward a decision
- **<0.5**: High uncertainty (should be rare)

## 📝 OUTPUT REQUIREMENTS

You MUST return structured JSON with:
- **has_new_learning**: boolean - true if new learning detected AND it helps with task execution, false otherwise
- **reasoning**: string - Detailed explanation that addresses BOTH: (1) whether new content exists, and (2) whether it helps with the step's task. Reference the step's title, description, and success criteria.
- **confidence**: float (0.0-1.0) - Your confidence in the detection

Focus on semantic differences AND task relevance, not just text changes.`, taskObjective, stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput)
}

// buildUserMessage builds the user message with old and new learning content and task context
// Parameters contain the actual combined content of all learning files, not file paths
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) buildUserMessage(previousLearningsContent, currentLearningsContent, stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput, taskObjective string) string {
	var builder strings.Builder

	builder.WriteString("Compare the following learning content to determine if new learning occurred that helps with task execution:\n\n")

	// Add task context first so agent understands what to evaluate against
	builder.WriteString("## TASK CONTEXT\n\n")
	builder.WriteString("**Overall Objective**: ")
	if taskObjective != "" {
		builder.WriteString(taskObjective)
	} else {
		builder.WriteString("(Not provided)")
	}
	builder.WriteString("\n\n")

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

	builder.WriteString("\n## ANALYSIS TASK\n")
	builder.WriteString("Analyze these learning contents and determine if:\n")
	builder.WriteString("1. Genuinely new learning occurred (new patterns, workflows, knowledge)\n")
	builder.WriteString("2. The learning helps with executing the step's task (relates to title, description, success criteria)\n\n")
	builder.WriteString("Return your analysis as structured JSON with has_new_learning, reasoning, and confidence.\n")
	builder.WriteString("Your reasoning must address BOTH whether new content exists AND whether it helps with the task.\n")

	return builder.String()
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for learning detection agent - use ExecuteStructured() instead")
}
