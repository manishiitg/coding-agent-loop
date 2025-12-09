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
func (hctpca *HumanControlledTodoPlannerConditionalAgent) Decide(ctx context.Context, conditionContext, question string, stepIndex, iteration int, isCodeExecutionMode bool, learningHistory string) (*ConditionalResponse, error) {
	// Verify event bridge is set (factory pattern ensures it's set, but check for safety)
	// The factory pattern handles event bridge connection, so this is just a safety check
	// Note: Event bridge is set by the factory pattern, so this check is mostly for debugging
	eventBridge := hctpca.GetEventBridge()
	if eventBridge == nil {
		// Factory pattern should have set this - this is unexpected
		// Logging is handled by the factory pattern, so we can skip here
	}

	// Build template variables
	templateVars := map[string]string{
		"ConditionContext": conditionContext,
		"Question":         question,
		"StepIndex":        fmt.Sprintf("%d", stepIndex),
		"Iteration":        fmt.Sprintf("%d", iteration),
		"LearningHistory":  learningHistory,
	}

	// Build system prompt
	var systemPrompt string
	var overwriteSystemPrompt bool

	if isCodeExecutionMode {
		// Code execution mode: overwrite base prompt and include code execution instructions
		codeExecutionInstructions := prompt.GetCodeExecutionInstructions()

		systemPrompt = fmt.Sprintf(`# Conditional Decision Agent

You are an expert decision-making agent specialized in evaluating workflow conditions. Your role is to make accurate true/false decisions by systematically verifying current state using available tools.

## 🔍 DECISION-MAKING FRAMEWORK

### Step 1: Understand the Condition
**Analyze the question:** What specific state or condition needs to be verified?

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
`, codeExecutionInstructions, learningHistory)
		overwriteSystemPrompt = true // Overwrite base prompt in code execution mode
	} else {
		// Non-code execution mode: append to base prompt (keeps MCP tools available)
		systemPrompt = fmt.Sprintf(`# Conditional Decision Agent

You are an expert decision-making agent specialized in evaluating workflow conditions. Your role is to make accurate true/false decisions by systematically verifying current state using available tools.

## 🔍 DECISION-MAKING FRAMEWORK

### Step 1: Understand the Condition
**Analyze the question:** What specific state or condition needs to be verified?

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
`, learningHistory)
		overwriteSystemPrompt = false // Append to base prompt (keeps MCP tools)
	}

	// Build user message input processor
	inputProcessor := func(vars map[string]string) string {
		return fmt.Sprintf(`## 📝 DECISION TASK

**Reference Context** (historical data - verify with tools):
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
- Any cross-verification performed`, vars["ConditionContext"], vars["Question"])
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

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use Decide() instead
func (hctpca *HumanControlledTodoPlannerConditionalAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for conditional agent - use Decide() instead")
}
