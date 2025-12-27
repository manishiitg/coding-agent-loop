package todo_creation_human

import (
	"context"
	"fmt"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/agent/prompt"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ConditionalResponse represents a true/false response with reasoning
type ConditionalResponse struct {
	Result bool   `json:"result"`
	Reason string `json:"reason"`
}

// GetResult returns the boolean result
func (cr *ConditionalResponse) GetResult() bool {
	return cr.Result
}

// DecisionResponse represents the structured response from decision evaluation
type DecisionResponse struct {
	Result    bool   `json:"result"`    // The decision result (true or false)
	Reasoning string `json:"reasoning"` // Detailed reasoning for the decision
}

// HumanControlledTodoPlannerConditionalAgent evaluates conditional decisions for step branching
type HumanControlledTodoPlannerConditionalAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerConditionalAgent creates a new conditional agent
func NewHumanControlledTodoPlannerConditionalAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerConditionalAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.ConditionalAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerConditionalAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Decide makes a true/false decision based on context and question
// Returns ConditionalResponse for backward compatibility with conditional steps
// variableNames: Variable names with descriptions ({{VAR_NAME}} - description)
// variableValues: Variable names with actual values ({{VAR_NAME}} = value - description)
func (hctpca *HumanControlledTodoPlannerConditionalAgent) Decide(ctx context.Context, conditionContext, question, description string, stepIndex, iteration int, isCodeExecutionMode bool, learningHistory string, variableNames, variableValues string) (*ConditionalResponse, error) {
	// Verify event bridge is set (factory pattern ensures it's set, but check for safety)
	// The factory pattern handles event bridge connection, so this is just a safety check
	// Note: Event bridge is set by the factory pattern, so this check is mostly for debugging

	// Build template variables
	templateVars := map[string]string{
		"ConditionContext": conditionContext,
		"Question":         question,
		"Description":      description,
		"StepIndex":        fmt.Sprintf("%d", stepIndex),
		"Iteration":        fmt.Sprintf("%d", iteration),
		"LearningHistory":  learningHistory,
		"VariableNames":    variableNames,
		"VariableValues":   variableValues,
	}

	// Build system prompt
	var systemPrompt string
	var overwriteSystemPrompt bool

	if isCodeExecutionMode {
		// Code execution mode: overwrite base prompt and include code execution instructions
		codeExecutionInstructions := prompt.GetCodeExecutionInstructions()

		// Build variable section if available
		variableSection := ""
		if variableNames != "" {
			variableSection = fmt.Sprintf(`
## 🔑 Available Variables
%s`, variableNames)
			if variableValues != "" {
				variableSection += fmt.Sprintf(`

**Current Values**: %s`, variableValues)
			}
			variableSection += `

**Variable Handling**:
- **Step descriptions already have variables resolved** - you'll see actual values in StepDescription, etc.
- **For new tool calls or code**: Use actual values directly from the resolved step description
- **Don't hardcode values** - reference them from the step context
`
		}

		systemPrompt = fmt.Sprintf(`# Conditional Decision Agent

You are an expert decision-making agent specialized in evaluating workflow conditions. Your role is to make accurate true/false decisions by systematically verifying current state using available tools.
%s
## 🔍 DECISION-MAKING FRAMEWORK

### Step 1: Understand the Condition
**Analyze the question:** What specific state or condition needs to be verified?

**Step Description**: %s

### Step 2: Review Reference Context (Optional Guidance)
- **Condition Context**: Previous step execution output (historical reference only)
- **Step Learnings**: Patterns from previous executions (guidance, not current state)

**CRITICAL**: Context is historical reference data. Never rely on context alone - always verify current state.

### Step 3: Systematic Verification Using Tools
**REQUIRED**: Use MCP tools to verify actual current state. Do not guess or assume.

**Verification Strategy:**
- Identify what information you need to answer the question
- Use appropriate MCP tools to gather current, factual data
- Cross-reference multiple sources when possible
- Document exactly what tools you used and their results

### Step 4: Decision Criteria
**TRUE** if: The verified current state meets the condition requirements
**FALSE** if: The verified current state does not meet the condition requirements

**Confidence Requirement**: Only decide when you have verified evidence from tools.

### Step 5: Structured Reasoning
**Decision Process:**
1. **Question Analysis**: What does the condition ask?
2. **Information Needed**: What must be verified?
3. **Tool Selection**: Which MCP tools will provide the needed information?
4. **Evidence Gathering**: Execute tools and collect results
5. **Verification**: Cross-check results for consistency
6. **Decision**: Based on verified evidence, make true/false determination

## 📋 OUTPUT REQUIREMENTS

**Format**: Return only valid JSON with this exact structure:
{"result": true/false, "reason": "Clear, evidence-based explanation"}

**Reasoning Guidelines:**
- Reference specific tools used and their results
- Explain how evidence led to your decision
- If uncertain, use tools to gather more information before deciding
- Be specific about what was verified and how

## ⚠️ EDGE CASES & ERROR HANDLING

- **Insufficient Information**: If tools don't provide clear evidence, gather more data
- **Tool Failures**: If a tool fails, try alternative approaches
- **Ambiguous Results**: When results are unclear, investigate further
- **No Context Available**: Rely entirely on tool verification (this is normal)

%s

## 📚 LEARNING CONTEXT (Reference Only)
%s

**Learning Usage Guidelines:**
- Use learnings to understand what typically needs verification
- Reference learnings for investigation strategies, not as current state
- Verify that learning patterns still apply to current situation using tools
`, variableSection, description, codeExecutionInstructions, learningHistory)
		overwriteSystemPrompt = true // Overwrite base prompt in code execution mode
	} else {
		// Non-code execution mode: append to base prompt (keeps MCP tools available)
		// Build variable section if available
		variableSection := ""
		if variableNames != "" {
			variableSection = fmt.Sprintf(`
## 🔑 Available Variables
%s`, variableNames)
			if variableValues != "" {
				variableSection += fmt.Sprintf(`

**Current Values**: %s`, variableValues)
			}
			variableSection += `

**Variable Handling**:
- **Step descriptions already have variables resolved** - you'll see actual values in StepDescription, etc.
- **For new tool calls or code**: Use actual values directly from the resolved step description
- **Don't hardcode values** - reference them from the step context
`
		}

		systemPrompt = fmt.Sprintf(`# Conditional Decision Agent

You are an expert decision-making agent specialized in evaluating workflow conditions. Your role is to make accurate true/false decisions by systematically verifying current state using available tools.
%s
## 🔍 DECISION-MAKING FRAMEWORK

### Step 1: Understand the Condition
**Analyze the question:** What specific state or condition needs to be verified?

**Step Description**: %s

### Step 2: Review Reference Context (Optional Guidance)
- **Condition Context**: Previous step execution output (historical reference only)
- **Step Learnings**: Patterns from previous executions (guidance, not current state)

**CRITICAL**: Context is historical reference data. Never rely on context alone - always verify current state.

### Step 3: Systematic Verification Using Tools
**REQUIRED**: Use MCP tools to verify actual current state. Do not guess or assume.

**Verification Strategy:**
- Identify what information you need to answer the question
- Use appropriate MCP tools to gather current, factual data
- Cross-reference multiple sources when possible
- Document exactly what tools you used and their results

### Step 4: Decision Criteria
**TRUE** if: The verified current state meets the condition requirements
**FALSE** if: The verified current state does not meet the condition requirements

**Confidence Requirement**: Only decide when you have verified evidence from tools.

### Step 5: Structured Reasoning
**Decision Process:**
1. **Question Analysis**: What does the condition ask?
2. **Information Needed**: What must be verified?
3. **Tool Selection**: Which MCP tools will provide the needed information?
4. **Evidence Gathering**: Execute tools and collect results
5. **Verification**: Cross-check results for consistency
6. **Decision**: Based on verified evidence, make true/false determination

## 📋 OUTPUT REQUIREMENTS

**Format**: Return only valid JSON with this exact structure:
{"result": true/false, "reason": "Clear, evidence-based explanation"}

**Reasoning Guidelines:**
- Reference specific tools used and their results
- Explain how evidence led to your decision
- If uncertain, use tools to gather more information before deciding
- Be specific about what was verified and how

## ⚠️ EDGE CASES & ERROR HANDLING

- **Insufficient Information**: If tools don't provide clear evidence, gather more data
- **Tool Failures**: If a tool fails, try alternative approaches
- **Ambiguous Results**: When results are unclear, investigate further
- **No Context Available**: Rely entirely on tool verification (this is normal)

## 📚 LEARNING CONTEXT (Reference Only)
%s

**Learning Usage Guidelines:**
- Use learnings to understand what typically needs verification
- Reference learnings for investigation strategies, not as current state
- Verify that learning patterns still apply to current situation using tools
`, variableSection, description, learningHistory)
		overwriteSystemPrompt = false // Append to base prompt (keeps MCP tools)
	}

	// Build user message input processor
	inputProcessor := func(vars map[string]string) string {
		descriptionSection := ""
		if vars["Description"] != "" {
			descriptionSection = fmt.Sprintf("\n**Step Description**:\n%s\n", vars["Description"])
		}
		return fmt.Sprintf(`## 📝 DECISION TASK
%s**Reference Context** (historical data - verify with tools):
%s

**Condition to Evaluate**:
%s

## 🔧 VERIFICATION REQUIRED

**MANDATORY**: You must use MCP tools to verify the current state before making your decision.

**Do not rely on reference context alone** - it is historical data that may not reflect current reality.

**Use tools to:**
- Read current files and check actual state
- Query systems and verify conditions
- Cross-reference multiple data sources
- Confirm the condition meets the specified criteria

## 📤 OUTPUT FORMAT

Return ONLY valid JSON: {"result": true/false, "reason": "detailed explanation of verification process and evidence"}

**Reason must include:**
- Tools used for verification
- What each tool revealed
- How evidence supports your decision
- Any cross-verification performed`, descriptionSection, vars["ConditionContext"], vars["Question"])
	}

	// Build schema
	schema := `{
  "type": "object",
  "properties": {
    "result": {"type": "boolean", "description": "The decision result (true or false)"},
    "reason": {"type": "string", "description": "Clear reasoning for the decision, referencing relevant context"}
  },
  "required": ["result", "reason"],
  "additionalProperties": false
}`

	// Use ExecuteStructuredWithInputProcessor for text conversion approach
	// This approach handles JSON in markdown code blocks better than the tool-based approach
	result, _, err := agents.ExecuteStructuredWithInputProcessor[ConditionalResponse](
		hctpca.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		[]llmtypes.MessageContent{},
		schema,
		systemPrompt,
		overwriteSystemPrompt, // Overwrite in code exec mode, append in non-code exec mode
	)

	if err != nil {
		return nil, fmt.Errorf("conditional decision failed: %w", err)
	}

	return &result, nil
}

// EvaluateDecision makes a structured decision evaluation for decision steps
// Returns DecisionResponse with rich structured output similar to validation agent
// variableNames: Variable names with descriptions ({{VAR_NAME}} - description)
// variableValues: Variable names with actual values ({{VAR_NAME}} = value - description)
func (hctpca *HumanControlledTodoPlannerConditionalAgent) EvaluateDecision(ctx context.Context, executionOutput, question string, stepIndex, iteration int, isCodeExecutionMode bool, learningHistory string, variableNames, variableValues string) (*DecisionResponse, error) {
	// Build template variables
	templateVars := map[string]string{
		"ExecutionOutput": executionOutput,
		"Question":        question,
		"StepIndex":       fmt.Sprintf("%d", stepIndex),
		"Iteration":       fmt.Sprintf("%d", iteration),
		"LearningHistory": learningHistory,
		"VariableNames":   variableNames,
		"VariableValues":  variableValues,
	}

	// Build system prompt for decision evaluation
	var systemPrompt string

	// Build variable section if available
	variableSection := ""
	if variableNames != "" {
		variableSection = fmt.Sprintf(`
## 🔑 Available Variables
%s`, variableNames)
		if variableValues != "" {
			variableSection += fmt.Sprintf(`

**Current Values**: %s`, variableValues)
		}
		variableSection += `

**Variable Handling**:
- **Step descriptions already have variables resolved** - you'll see actual values in StepDescription, etc.
- **For new tool calls or code**: Use actual values directly from the resolved step description
- **Don't hardcode values** - reference them from the step context
`
	}

	if isCodeExecutionMode {
		codeExecutionInstructions := prompt.GetCodeExecutionInstructions()
		systemPrompt = fmt.Sprintf(`# Decision Evaluation Agent

You are an expert decision evaluation agent specialized in analyzing execution outputs and making structured decisions. Your role is to evaluate decision step execution results and provide comprehensive structured analysis.
%s
## 🔍 DECISION EVALUATION FRAMEWORK

### Step 1: Analyze Execution Output
**Review the execution output:** What was the result of the decision step's execution?

### Step 2: Understand the Evaluation Question
**Analyze the question:** What specific condition or criteria needs to be evaluated?

### Step 3: Systematic Evaluation
**Evaluation Strategy:**
- Identify key information in the execution output
- Cross-reference with the evaluation question
- Determine if the execution output meets the evaluation criteria

### Step 4: Structured Decision
**Decision Criteria:**
- **TRUE** if: Execution output clearly meets the evaluation criteria
- **FALSE** if: Execution output does not meet the evaluation criteria

## 📋 OUTPUT REQUIREMENTS

**USE THE 'submit_decision_result' TOOL TO SUBMIT YOUR DECISION ANALYSIS**

You MUST call the 'submit_decision_result' tool with your structured decision response. Do NOT return JSON directly in your response - use the tool instead.

The tool accepts a structured object with:
- result: boolean - Whether the evaluation question is answered as true or false
- reasoning: string - Detailed reasoning explaining how the execution output relates to the evaluation question

**Example JSON structure:**
`+"```json"+`
{
  "result": true,
  "reasoning": "The execution output shows that the deployment was successful. All services are running and health checks passed."
}
`+"```"+`

**CRITICAL**: You MUST call the 'submit_decision_result' tool with your decision analysis. The tool will be available to you - use it to submit your structured decision response.

%s

## 📚 LEARNING CONTEXT (Reference Only)
%s

**Learning Usage Guidelines:**
- Use learnings to understand typical evaluation patterns
- Reference learnings for decision-making strategies, not as current state
- Verify that learning patterns apply to current execution output
`, codeExecutionInstructions, learningHistory)
	} else {
		systemPrompt = fmt.Sprintf(`# Decision Evaluation Agent

You are an expert decision evaluation agent specialized in analyzing execution outputs and making structured decisions. Your role is to evaluate decision step execution results and provide comprehensive structured analysis.
%s
## 🔍 DECISION EVALUATION FRAMEWORK

### Step 1: Analyze Execution Output
**Review the execution output:** What was the result of the decision step's execution?

### Step 2: Understand the Evaluation Question
**Analyze the question:** What specific condition or criteria needs to be evaluated?

### Step 3: Systematic Evaluation
**Evaluation Strategy:**
- Identify key information in the execution output
- Cross-reference with the evaluation question
- Determine if the execution output meets the evaluation criteria

### Step 4: Structured Decision
**Decision Criteria:**
- **TRUE** if: Execution output clearly meets the evaluation criteria
- **FALSE** if: Execution output does not meet the evaluation criteria

## 📋 OUTPUT REQUIREMENTS

**USE THE 'submit_decision_result' TOOL TO SUBMIT YOUR DECISION ANALYSIS**

You MUST call the 'submit_decision_result' tool with your structured decision response. Do NOT return JSON directly in your response - use the tool instead.

The tool accepts a structured object with:
- result: boolean - Whether the evaluation question is answered as true or false
- reasoning: string - Detailed reasoning explaining how the execution output relates to the evaluation question

**Example JSON structure:**
`+"```json"+`
{
  "result": true,
  "reasoning": "The execution output shows that the deployment was successful. All services are running and health checks passed."
}
`+"```"+`

**CRITICAL**: You MUST call the 'submit_decision_result' tool with your decision analysis. The tool will be available to you - use it to submit your structured decision response.

## 📚 LEARNING CONTEXT (Reference Only)
%s

**Learning Usage Guidelines:**
- Use learnings to understand typical evaluation patterns
- Reference learnings for decision-making strategies, not as current state
- Verify that learning patterns apply to current execution output
`, variableSection, learningHistory)
	}

	// Build user message input processor
	inputProcessor := func(vars map[string]string) string {
		return fmt.Sprintf(`## 📝 DECISION EVALUATION TASK

**Execution Output** (from decision step execution):
%s

**Evaluation Question**:
%s

## 🔧 EVALUATION REQUIRED

Analyze the execution output and determine if it answers the evaluation question as true or false.

**Evaluation Process:**
1. Review the execution output carefully
2. Understand what the evaluation question is asking
3. Determine if the execution output indicates the answer is true or false

## 📤 OUTPUT FORMAT

**USE THE 'submit_decision_result' TOOL** to submit your structured decision analysis.

Your decision should include:
- result: true or false based on the evaluation question
- reasoning: Detailed explanation of how execution output relates to the question`, vars["ExecutionOutput"], vars["Question"])
	}

	// Build schema for structured output
	schema := `{
		"type": "object",
		"properties": {
			"result": {
				"type": "boolean",
				"description": "The decision result - true if execution output indicates the evaluation question is true, false otherwise"
			},
			"reasoning": {
				"type": "string",
				"description": "Detailed reasoning explaining how the execution output relates to the evaluation question and supports the decision"
			}
		},
		"required": ["result", "reasoning"]
	}`

	// Define tool name and description for structured output via tool calls
	toolName := "submit_decision_result"
	toolDescription := "Submit the decision evaluation result. This tool should be called with the structured decision response containing the result and reasoning."

	// Use ExecuteStructuredWithInputProcessorViaTool similar to validation agent
	result, _, err := agents.ExecuteStructuredWithInputProcessorViaTool[DecisionResponse](
		hctpca.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		[]llmtypes.MessageContent{},
		schema,
		systemPrompt,
		isCodeExecutionMode, // Overwrite in code exec mode, append otherwise
		toolName,
		toolDescription,
	)

	if err != nil {
		return nil, fmt.Errorf("decision evaluation failed: %w", err)
	}

	return &result, nil
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use Decide() instead
func (hctpca *HumanControlledTodoPlannerConditionalAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for conditional agent - use Decide() instead")
}
