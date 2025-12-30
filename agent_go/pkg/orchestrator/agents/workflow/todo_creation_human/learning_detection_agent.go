package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// LearningDetectionResponse represents the structured response from learning detection analysis
type LearningDetectionResponse struct {
	HasNewLearning bool    `json:"has_new_learning"` // Whether new learning was detected
	Reasoning      string  `json:"reasoning"`        // Detailed reasoning for detection
	Confidence     float64 `json:"confidence"`       // 0.0 to 1.0 - confidence in detection
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

	// Current learnings content must be provided (already consolidated)
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

	// Build schema for structured output (detection only)
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
			}
		},
		"required": ["has_new_learning", "reasoning", "confidence"]
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
	templateStr := `# Learning Detection Agent

## 🤖 Role
Expert agent tasked with detecting *substantial* new knowledge that significantly improves task execution.

## 🚨 STRICT DETECTION CRITERIA
Return **true** ONLY if there is a MAJOR change:
1. **New ⭐ OPTIMAL Path**: A completely new, highly efficient pattern or workflow.
2. **Viability Shift**: A pattern moved from unreliable/failure (0%) to viable/success (50%+).
3. **Paradigm Shift**: A fundamentally different MCP tool chain or execution method.

Return **false** for:
- ❌ Minor code tweaks or formatting.
- ❌ Incremental score updates or failure logs.
- ❌ Workspace tool noise (reads/writes).
- ❌ **Uncertainty**: When in doubt, return false.

{{if .TempLLMContext}}
## ⚠️ SUFFICIENCY BIAS
The step succeeded using {{.UsedTempLLM}} with validation passed. This suggests existing learnings are likely sufficient.
**Impact**: Bias toward "no new learning". Only approve genuinely major structural improvements.{{end}}

## 📋 TASK CONTEXT
- **Title**: {{.Title}}
- **Goal**: {{.Description}}
- **Success Criteria**: {{.SuccessCriteria}}

## 📤 OUTPUT REQUIREMENTS
1. **has_new_learning** (bool): Strict boolean based on criteria.
2. **reasoning** (string): Explain specific differences and task relevance. Address the checklist: New optimal? Viability shift? Structural shift?
3. **confidence** (float): 0.0 to 1.0.

*Be conservative. We want signal, not noise.*`

	tmpl, err := template.New("learningDetectionSystemPrompt").Parse(templateStr)
	if err != nil {
		return "Error parsing learning detection system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, map[string]interface{}{
		"Title":           stepTitle,
		"Description":     stepDescription,
		"SuccessCriteria": stepSuccessCriteria,
		"UsedTempLLM":     usedTempLLM,
		"TempLLMContext":  usedTempLLM != "" && validationPassed == "true",
	}); err != nil {
		return "Error executing learning detection system prompt template: " + err.Error()
	}

	return result.String()
}

// buildUserMessage builds the user message with old and new learning content and task context
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) buildUserMessage(previousLearningsContent, currentLearningsContent, stepTitle, stepDescription, stepSuccessCriteria, stepContextDependencies, stepContextOutput string) string {
	templateStr := `### 📊 Learning Comparison Task

Compare the previous and current learnings below against the **Strict Detection Criteria** in the system prompt.

#### ⬅️ PREVIOUS LEARNINGS
{{if .PreviousContent}}` + "```markdown" + `
{{.PreviousContent}}
` + "```" + `{{else}}*No previous learnings exist.*{{end}}

#### ➡️ CURRENT LEARNINGS
{{if .CurrentContent}}` + "```markdown" + `
{{.CurrentContent}}
` + "```" + `{{else}}*No current learnings exist to compare.*{{end}}

### 🧠 Evaluation Instructions
1. **Strictness**: Return true ONLY for MAJOR additions (New Optimal Path, Viability Shift, or Paradigm Shift).
2. **Context**: Ensure changes are relevant to the step's success criteria.
3. **Reasoning**: Explicitly address the checklist (Optimal? Viable? Structural? Substantial?).`

	tmpl, err := template.New("learningDetectionUserMessage").Parse(templateStr)
	if err != nil {
		return "Error parsing learning detection user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, map[string]interface{}{
		"PreviousContent": previousLearningsContent,
		"CurrentContent":  currentLearningsContent,
	}); err != nil {
		return "Error executing learning detection user message template: " + err.Error()
	}

	return result.String()
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hctplda *HumanControlledTodoPlannerLearningDetectionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for learning detection agent - use ExecuteStructured() instead")
}
