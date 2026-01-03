# Conditional Agent Implementation

## Overview

The Conditional Agent (`HumanControlledTodoPlannerConditionalAgent`) evaluates true/false decisions for workflow branching. It uses structured JSON output and tool-based verification to make accurate conditional decisions.

## Key Implementation Details

### 1. Agent Creation and Factory Pattern

**Location**: [`controller_agent_factory.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_agent_factory.go) - `createConditionalAgent()`

The conditional agent uses the same factory pattern as the validation agent for consistency:

```go
agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
    ctx,
    config,
    phase,        // "conditional_evaluation"
    step,         // Step index
    iteration,    // Iteration number
    func(cfg, logger, tracer, eventBridge) agents.OrchestratorAgent {
        return NewHumanControlledTodoPlannerConditionalAgent(cfg, logger, tracer, eventBridge)
    },
    toolsToRegister,   // Workspace tools (filtered by step config)
    executorsToUse,    // Tool executors
    false,             // Don't overwrite system prompt
)
```

**Key Features**:
- Uses `CreateAndSetupStandardAgentWithConfig` for proper initialization
- Automatically connects context-aware event bridge
- Registers workspace tools and MCP tools based on step configuration
- Supports step-specific LLM configuration (`conditional_llm` in step config)
- Supports code execution mode (inherited from step config or orchestrator default)

### 2. Context-Aware Event Bridge Connection

**Location**: [`controller.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller.go) - Default agent creation, [`controller_agent_factory.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_agent_factory.go) - Factory method

The conditional agent's event bridge is automatically connected via the factory pattern:

- **Event Bridge**: Context-aware bridge (`ContextAwareEventBridge`) is set during agent creation
- **Orchestrator Context**: Set during agent creation in `getConditionalAgentForStep()` → `createConditionalAgent()` (factory pattern)
- **Context Phase**: Set to "conditional_evaluation" phase for conditional evaluation
- **Context Restoration**: After evaluation, context is restored to "execution" phase for subsequent steps

**Event Emission**:
- `OrchestratorAgentStart` event: Automatically emitted via `ExecuteStructuredWithInputProcessorViaTool`
- `OrchestratorAgentEnd` event: **Suppressed** for conditional agents (to avoid "Conditional LLM Completed" log spam)
- Structured response JSON: Emitted in `result` field of events (when applicable)

### 3. Condition Context (conditionContext)

**Location**: [`controller_conditional.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_conditional.go) - `executeConditionalStep()`

The `conditionContext` parameter contains **only** the output from the **last previous execution agent** (in-memory, not from files):

```go
// Build conditionContext - ONLY the last previous execution agent output (from in-memory results)
contextBuilder := strings.Builder{}

// Add context from the LAST previous execution agent output ONLY
if len(previousExecutionResults) > 0 {
    // Get the last (most recent) execution result
    lastExecutionResult := ""
    for i := len(previousExecutionResults) - 1; i >= 0; i-- {
        if previousExecutionResults[i] != "" {
            lastExecutionResult = previousExecutionResults[i]
            break
        }
    }

    if lastExecutionResult != "" {
        contextBuilder.WriteString("Previous Step Execution Output:\n")
        contextBuilder.WriteString(fmt.Sprintf("%s\n", lastExecutionResult))
    }
}

conditionContext := contextBuilder.String()
```

**Key Rules**:
- Contains **only** the last previous step's execution output
- Passed **in-memory** (not read from files)
- Does **not** include static context, learnings, or other metadata
- Empty if no previous execution results exist

### 4. Learning History (Separate Parameter)

**Location**: [`controller_conditional.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_conditional.go) - `executeConditionalStep()`

Learning history is passed as a **separate parameter** (`learningHistory`) via `LoadStepLearningHistory()`:

```go
// Read learnings separately (passed as separate learningHistory variable, not in conditionContext)
learningHistory, _ := hcpo.LoadStepLearningHistory(ctx, step.GetID(), stepIndex, conditionalStepPath, "conditional")
```

**Key Rules**:
- Separate from `conditionContext`
- Loaded via `LoadStepLearningHistory()` helper method
- Included in system prompt under "📚 LEARNINGS (Historical)"
- Used for guidance, not as current state
- Agent must verify conditions using tools, not rely on learnings

### 5. Tool Access and Configuration

**Location**: [`controller_agent_factory.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_agent_factory.go) - `createConditionalAgent()`

The conditional agent receives workspace tools and MCP tools based on step configuration:

```go
// Filter workspace tools based on step config if specified
var toolsToRegister []llmtypes.Tool
var executorsToUse map[string]interface{}
if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
    unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
        stepConfig.EnabledCustomToolCategories,
        stepConfig.EnabledCustomTools,
    )
    toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
        hcpo.WorkspaceTools,
        hcpo.WorkspaceToolExecutors,
        unifiedEnabledTools,
    )
} else {
    // Use all workspace tools if no filtering specified
    toolsToRegister = hcpo.WorkspaceTools
    executorsToUse = hcpo.WorkspaceToolExecutors
}
```

**Key Features**:
- Supports step-specific tool filtering (`EnabledCustomToolCategories`, `EnabledCustomTools`)
- Falls back to all workspace tools if no filtering specified
- MCP servers and tools are selected based on step config (`SelectedServers`, `SelectedTools`)
- Code execution mode support (inherited from step config or orchestrator)

### 6. Structured Response JSON in Events

**Location**: [`base_orchestrator_agent.go`](../agent_go/pkg/orchestrator/agents/base_orchestrator_agent.go) - `ExecuteStructuredWithInputProcessorViaTool()`

For structured responses (conditional and validation agents), the JSON is emitted in the `result` field:

```go
// Structured output: marshal to JSON for result field and map for structuredResponse field
resultBytes, marshalErr := json.Marshal(result.StructuredResult)
if marshalErr == nil {
    // Set Result field to the JSON string of the structured response
    resultStr = string(resultBytes)
    
    // Also unmarshal to map for StructuredResponse field
    var responseMap map[string]interface{}
    if unmarshalErr := json.Unmarshal(resultBytes, &responseMap); unmarshalErr == nil {
        structuredResponse = responseMap
    }
}
```

**Event Structure**:
- `result`: JSON string of structured response (e.g., `{"result": true, "reason": "..."}`)
- `structured_response`: Map representation of the same JSON (for programmatic access)

### 7. Prompt Engineering

**Location**: [`conditional_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/conditional_agent.go) - `Decide()` method

The conditional agent's prompts emphasize **tool-based verification**:

**System Prompt Key Points**:
- **CRITICAL**: Context is historical reference data - never rely on context alone
- **REQUIRED**: Use MCP tools to verify actual current state
- **Verification Strategy**: Identify needed information → Use tools → Cross-reference → Document results
- **Decision Criteria**: Only decide when verified evidence from tools is available

**User Message Key Points**:
- **MANDATORY**: Must use MCP tools to verify current state before decision
- **Do not rely on reference context alone** - it is historical data
- **Use tools to**: Read files, query systems, cross-reference, confirm criteria

**Output Format**:
```json
{
  "result": true/false,
  "reason": "detailed explanation of verification process and evidence"
}
```

**Reason Requirements**:
- Tools used for verification
- What each tool revealed
- How evidence supports decision
- Any cross-verification performed

### 8. Event Suppression

**Location**: [`base_orchestrator_agent.go`](../agent_go/pkg/orchestrator/agents/base_orchestrator_agent.go) - `emitAgentEndEventWithStructuredResponse()`

The `OrchestratorAgentEnd` event is suppressed for conditional agents to avoid log spam:

```go
// Skip emitting OrchestratorAgentEnd event for conditional agents
if boa.agentType == ConditionalAgentType {
    boa.logger.Debug(fmt.Sprintf("ℹ️ Skipping OrchestratorAgentEnd event for conditional agent type: %s", boa.agentType))
    return
}
```

**Rationale**: Conditional evaluations are frequent and the "Conditional LLM Completed" log message is not needed. The structured response JSON in the `result` field provides the necessary information.

### 9. Agent Type and Identification

**Location**: [`base_agent.go`](../agent_go/pkg/orchestrator/agents/base_agent.go) - Agent type constants

```go
const ConditionalAgentType AgentType = "conditional"
```

**Usage**:
- Agent type: `"conditional"`
- Frontend display: "Conditional LLM" (in `OrchestratorAgentStartEvent.tsx` and `OrchestratorAgentEndEvent.tsx`)
- Icon: 🔀 (branch icon)
- Color: indigo

### 10. Code Execution Mode Support

**Location**: [`conditional_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/conditional_agent.go) - `Decide()` method

The conditional agent supports code execution mode:

- **Code Execution Mode**: Overwrites base system prompt with code execution instructions
- **Non-Code Execution Mode**: Appends to base system prompt (keeps MCP tools available)
- **Determination**: Inherited from step config (`UseCodeExecutionMode`) or orchestrator default

**Code Execution Instructions**: Includes guidance on using code execution tools to verify conditions.

## Usage Flow

1. **Step Execution**: `executeConditionalStep()` is called for conditional steps (in `controller_conditional.go`)
2. **Context Building**: `conditionContext` is built from last previous execution output (in-memory)
3. **Learning Loading**: Learning history is loaded via `LoadStepLearningHistory()` (separate parameter)
4. **Code Execution Mode**: Determined from step config or orchestrator default
5. **Agent Creation**: Conditional agent is created via `getConditionalAgentForStep()` (step-specific or default)
6. **Context Setup**: Orchestrator context is set to "conditional_evaluation" phase (done in factory)
7. **Decision**: `conditionalAgent.Decide()` is called with context, question, and learnings
8. **Tool Verification**: Agent uses MCP tools (or code execution) to verify current state
9. **Structured Output**: Agent returns `ConditionalResponse` with `result` (bool) and `reason` (string)
10. **Branch Selection**: Based on `result`, either `if_true_steps` or `if_false_steps` are executed
11. **Nested Support**: Branch steps can include nested conditional steps (max depth: 2)

## Key Design Principles

1. **Tool-Based Verification**: Always verify conditions using tools, not just context
2. **In-Memory Context**: Pass execution results in-memory, not via file I/O
3. **Last Step Only**: `conditionContext` contains only the last previous step's output
4. **Separate Learnings**: Learning history is separate from condition context
5. **Factory Pattern**: Consistent agent creation pattern (same as validation agent)
6. **Event Suppression**: Suppress verbose end events for conditional agents
7. **Structured JSON**: Emit structured response JSON in `result` field for programmatic access
8. **Step-Specific Config**: Support step-specific LLM, tools, and code execution mode

## Related Files

- [`conditional_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/conditional_agent.go): Conditional agent implementation (`Decide()` method)
- [`controller_conditional.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_conditional.go): Conditional step execution logic (`executeConditionalStep()`)
- [`controller_agent_factory.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_agent_factory.go): Factory method for conditional agent creation (`createConditionalAgent()`)
- [`controller.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller.go): Step-specific agent retrieval (`getConditionalAgentForStep()`)
- [`controller_learning_helpers.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_learning_helpers.go): Learning history loading (`LoadStepLearningHistory()`)
- [`base_orchestrator_agent.go`](../agent_go/pkg/orchestrator/agents/base_orchestrator_agent.go): Structured response event emission
- [`base_agent.go`](../agent_go/pkg/orchestrator/agents/base_agent.go): Agent type constants

