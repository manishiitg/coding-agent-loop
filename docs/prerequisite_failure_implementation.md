# Prerequisite Failure Detection - Implementation Guide

## 📋 Overview

**Status**: ✅ **COMPLETED**

Prerequisite failure detection allows the execution agent to proactively detect missing prerequisites during step execution and immediately navigate back to the prerequisite step. Users enable prerequisite detection for specific steps and configure **prerequisite rules** - each rule specifies one step dependency and one description of when to detect prerequisite failures.

**Key Design**: The execution agent has access to a special tool `detect_prerequisite_failure` that, when called, immediately stops execution and triggers navigation to the prerequisite step. This is a proactive, tool-based approach rather than post-validation detection.

**Key Benefits:**
- Immediate detection during execution (no need to wait for validation)
- LLM-driven decision making (execution agent decides when prerequisites are missing)
- Single tool handles all prerequisite scenarios via `depends_on_step_id` parameter
- Context cancellation ensures execution stops immediately when tool is called

---

## 📁 Key Files & Locations

| Component | File | Key Functions/Types |
|-----------|------|---------------------|
| **Tool Creation** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_execution.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_execution.go) | `createPrerequisiteDetectionTool()`, `formatPrerequisiteRulesForExecutionAgent()`, `PrerequisiteFailureError` |
| **Tool Registration** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_agent_factory.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_agent_factory.go) | `createExecutionOnlyAgent()` |
| **Execution Loop** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_execution.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_execution.go) | `executeSingleStep()` - channel-based error handling |
| **System Prompt** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/execution_only_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/execution_only_agent.go) | `executionOnlySystemPromptProcessor()` - includes prerequisite rules info |
| **Data Model** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_execution.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_execution.go) | `PrerequisiteInfo`, `PrerequisiteRuleInfo`, `gatherPrerequisiteInfo()` |
| **Frontend Config** | [`frontend/src/components/workflow/canvas/PrerequisiteConfigPanel.tsx`](../frontend/src/components/workflow/canvas/PrerequisiteConfigPanel.tsx) | UI for configuring prerequisite rules |
| **Frontend Visualization** | [`frontend/src/components/workflow/hooks/usePlanToFlow.ts`](../frontend/src/components/workflow/hooks/usePlanToFlow.ts) | `createPrerequisiteEdges()` - creates prerequisite edges in React Flow |

---

## 🔄 How It Works

### 1. Configuration

User enables prerequisite detection and configures rules in the UI:

```json
{
  "steps": [{
    "id": "step-2",
    "agent_configs": {
      "enable_prerequisite_detection": true,
      "prerequisite_rules": [
        {
          "depends_on_step": "step-0",
          "description": "If login session is missing or expired, go back to step 0"
        }
      ]
    }
  }]
}
```

### 2. Tool Registration

During step execution, if prerequisite detection is enabled:

1. **Gather Prerequisite Info**: `gatherPrerequisiteInfo()` collects prerequisite rules and dependency step information
2. **Create Cancellable Context**: Execution context is wrapped with `context.WithCancel()` to allow immediate cancellation
3. **Create Error Channel**: Buffered channel (`chan *PrerequisiteFailureError`) is created to receive errors from tool
4. **Register Tool**: `detect_prerequisite_failure` tool is registered with the execution agent:
   - Tool executor validates `depends_on_step_id` against configured rules
   - Tool executor validates step index, navigation distance, and prerequisites
   - On success, tool sends error to channel and cancels execution context
5. **Update System Prompt**: Prerequisite rules are formatted and added to execution agent's system prompt via `formatPrerequisiteRulesForExecutionAgent()`

### 3. Tool Execution Flow

```mermaid
sequenceDiagram
    participant LLM as Execution Agent (LLM)
    participant Tool as detect_prerequisite_failure
    participant Channel as Error Channel
    participant Context as Execution Context
    participant Orchestrator as Execution Loop
    
    LLM->>Tool: Call tool with depends_on_step_id + reason
    Tool->>Tool: Validate step ID, index, distance
    Tool->>Channel: Send PrerequisiteFailureError
    Tool->>Context: Cancel context
    Context-->>LLM: Execution stopped
    Orchestrator->>Channel: Check for prerequisite error
    Channel-->>Orchestrator: PrerequisiteFailureError
    Orchestrator->>Orchestrator: Trigger navigation
```

### 4. Immediate Cancellation

When the tool is called:

1. **Validation**: Tool validates:
   - `depends_on_step_id` exists in prerequisite rules
   - Step index exists in plan
   - Target step is before current step
   - Navigation distance ≤ 10 steps

2. **Error Creation**: Creates `PrerequisiteFailureError` with:
   - `DependsOnStepID`: The step ID to navigate to
   - `StepIndex`: 0-based step index
   - `Reason`: User-provided reason

3. **Channel Send**: Sends error to channel (non-blocking)

4. **Context Cancellation**: Calls `cancelFunc()` to immediately stop agent execution

5. **Return**: Returns empty string (execution already stopped)

### 5. Error Handling in Execution Loop

After `Execute()` returns (due to context cancellation):

```go
// Check for prerequisite failure (from tool call via channel)
var prereqErr *PrerequisiteFailureError
select {
case prereqErr = <-prereqErrChan:
    // Prerequisite failure detected - tool called and context was cancelled
default:
    // No prerequisite failure - check for other errors
}
```

If prerequisite error found:
1. Validate target step (index, distance, branch constraints)
2. Clean up progress from target step onward via `cleanupProgressFromStep()`
3. Emit `PrerequisiteNavigationEvent`
4. Return navigation error to restart from target step

---

## 🏗️ Architecture

### Tool Registration

**File**: [`controller_agent_factory.go:354-385`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_agent_factory.go#L354)

```go
if prerequisiteInfo != nil && len(prerequisiteInfo.PrerequisiteRules) > 0 {
    toolExecutor := hcpo.createPrerequisiteDetectionTool(
        prerequisiteInfo, allSteps, currentStepIndex, 
        cancelFunc, prereqErrChan)
    
    toolParams := map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "depends_on_step_id": map[string]interface{}{
                "type": "string",
                "description": "Step ID from one of the prerequisite rules",
            },
            "reason": map[string]interface{}{
                "type": "string",
                "description": "Brief explanation of why the prerequisite failure was detected",
            },
        },
        "required": []string{"depends_on_step_id", "reason"},
    }
    
    toolDescription := "Detect a prerequisite failure and navigate back to a prerequisite step. Call this tool when you detect that a prerequisite condition (as described in the prerequisite rules) is met during execution. Execution will stop and automatically navigate back to the specified prerequisite step."
    
    mcpAgent.RegisterCustomTool(
        "detect_prerequisite_failure",
        toolDescription,
        toolParams,
        toolExecutor,
        "structured_output", // Always available even in code execution mode
    )
}
```

### Tool Executor

**File**: [`controller_execution.go:386-464`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_execution.go#L386)

The tool executor:
1. Validates `depends_on_step_id` is in configured prerequisite rules
2. Validates step index exists and is before current step
3. Validates navigation distance ≤ 10 steps
4. Creates `PrerequisiteFailureError`
5. Sends error to channel
6. Cancels execution context
7. Returns (execution already stopped)

### System Prompt Integration

**File**: [`execution_only_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/execution_only_agent.go)

Prerequisite rules are formatted via `formatPrerequisiteRulesForExecutionAgent()` and included in the system prompt template:

```go
{{if .PrerequisiteRulesInfo}}{{.PrerequisiteRulesInfo}}{{end}}
```

The formatted text includes:
- Available prerequisite rules with step IDs and descriptions
- Instructions on when to call `detect_prerequisite_failure`
- How to use the tool (parameters, behavior)

---

## 🧩 Code Examples

### Tool Executor Implementation

```go
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPrerequisiteDetectionTool(
    prerequisiteInfo *PrerequisiteInfo, 
    allSteps []TodoStep, 
    currentStepIndex int, 
    cancelFunc context.CancelFunc, 
    prereqErrChan chan<- *PrerequisiteFailureError,
) func(ctx context.Context, args map[string]interface{}) (string, error) {
    // ... validation maps setup ...
    
    return func(ctx context.Context, args map[string]interface{}) (string, error) {
        // Extract and validate parameters
        dependsOnStepID := args["depends_on_step_id"].(string)
        reason := args["reason"].(string)
        
        // Validate against prerequisite rules
        // ... validation logic ...
        
        // Create error
        prereqErr := &PrerequisiteFailureError{
            DependsOnStepID: dependsOnStepID,
            StepIndex:       stepIndex,
            Reason:          reason,
        }
        
        // Send to channel
        select {
        case prereqErrChan <- prereqErr:
        default:
        }
        
        // Cancel execution immediately
        if cancelFunc != nil {
            cancelFunc()
        }
        
        return "", nil
    }
}
```

### Execution Loop Integration

```go
// Create cancellable context
executionCtx, cancelExecution := context.WithCancel(ctx)
defer cancelExecution()

// Create error channel
prereqErrChan := make(chan *PrerequisiteFailureError, 1)

// Create agent with tool registration
executionAgent, err := hcpo.createExecutionOnlyAgent(
    executionCtx, "execution_only", stepPath, executionAgentName, 
    step.AgentConfigs, isRetryAfterValidationFailure, retryAttempt, 
    prerequisiteInfoForExecution, allSteps, stepIndex, 
    cancelExecution, prereqErrChan,
)

// Execute agent
executionResult, executionConversationHistory, err := executionAgent.Execute(
    executionCtx, templateVars, []llmtypes.MessageContent{},
)

// Check for prerequisite failure
var prereqErr *PrerequisiteFailureError
select {
case prereqErr = <-prereqErrChan:
    // Handle navigation
    targetStepIndex := prereqErr.StepIndex
    // ... navigation logic ...
default:
    // No prerequisite failure
}
```

---

## ⚙️ Configuration

### Step Configuration

| Field | Type | Default | Purpose |
|-------|------|---------|---------|
| `enable_prerequisite_detection` | `boolean` | `false` | Enable prerequisite detection for this step |
| `prerequisite_rules` | `PrerequisiteRule[]` | `[]` | Array of prerequisite rules |

### Prerequisite Rule Structure

| Field | Type | Required | Purpose |
|-------|------|----------|---------|
| `depends_on_step` | `string` | Yes | Step ID this rule depends on (e.g., `"step-0"`) |
| `description` | `string` | Yes | Natural language description of when to detect prerequisite failure |

### Tool Parameters

| Parameter | Type | Required | Purpose |
|-----------|------|----------|---------|
| `depends_on_step_id` | `string` | Yes | Step ID from prerequisite rules to navigate to |
| `reason` | `string` | Yes | Brief explanation of why prerequisite failure was detected |

---

## 🛠️ Common Issues & Solutions

| Issue | Cause | Solution |
|-------|-------|----------|
| Tool not available in execution agent | Prerequisite detection not enabled or no rules configured | Check `enable_prerequisite_detection` and `prerequisite_rules` in step config |
| Execution doesn't stop immediately | Context cancellation not working | Verify `cancelFunc` is passed correctly to tool executor |
| Prerequisite error not detected | Channel not checked after Execute() | Ensure `select` statement checks channel after `Execute()` returns |
| Invalid step ID error | Step ID not in prerequisite rules | Verify `depends_on_step_id` matches a configured rule's `depends_on_step` |
| Navigation distance exceeded | Target step too far back | Maximum 10 steps - check step indices |

---

## 🔍 For LLMs: Quick Reference

### Constraints

✅ **Allowed:**
- Calling `detect_prerequisite_failure` when prerequisite condition is met
- Using any configured `depends_on_step_id` from prerequisite rules
- Providing clear reason for prerequisite failure

❌ **Forbidden:**
- Calling tool with step IDs not in prerequisite rules
- Calling tool for execution failures (not prerequisite failures)
- Navigating more than 10 steps back

### Tool Usage Pattern

```go
// When prerequisite condition is detected:
detect_prerequisite_failure({
    "depends_on_step_id": "step-0",  // Must match a configured rule
    "reason": "Login session expired" // Brief explanation
})
```

### System Prompt Format

The execution agent receives prerequisite rules in this format:

```
## 🔄 Prerequisite Detection

**Prerequisite detection is enabled for this step.** If you detect that a prerequisite condition described below is met during execution, you can call the `detect_prerequisite_failure` tool to navigate back to the prerequisite step.

### Available Prerequisite Rules:

**Rule 1:**
- **Step ID**: `step-0` (Step 1: Login to Website)
- **Condition**: If login session is missing or expired, go back to step 0

### How to Use:
1. Call `detect_prerequisite_failure` with:
   - `depends_on_step_id`: The step ID from the matching rule
   - `reason`: A brief explanation
2. Execution will stop and automatically navigate back
```

---

## 📊 Data Model

### PrerequisiteFailureError

```go
type PrerequisiteFailureError struct {
    DependsOnStepID string // Step ID to navigate back to
    StepIndex       int    // 0-based step index (computed from step ID)
    Reason          string // Reason for prerequisite failure
}
```

### PrerequisiteInfo

```go
type PrerequisiteInfo struct {
    CurrentStepID               string                 `json:"current_step_id"`
    CurrentStepIndex            int                    `json:"current_step_index"`
    EnablePrerequisiteDetection bool                   `json:"enable_prerequisite_detection"`
    PrerequisiteRules           []PrerequisiteRuleInfo `json:"prerequisite_rules"`
}

type PrerequisiteRuleInfo struct {
    DependsOnStep      string             `json:"depends_on_step"`
    Description        string             `json:"description"`
    DependencyStepInfo DependencyStepInfo `json:"dependency_step_info"`
}
```

---

## 🎯 Safety Limits

| Limit | Value | Purpose |
|-------|-------|---------|
| **Max Navigation Distance** | 10 steps | Prevents excessive backtracking |
| **Channel Buffer Size** | 1 | Single error per execution |
| **Validation** | Multiple checks | Step ID, index, distance, branch constraints |

---

## ✅ Implementation Status

1. ✅ **Data Model**: `PrerequisiteRule`, `PrerequisiteInfo`, `PrerequisiteFailureError`
2. ✅ **Tool Creation**: `createPrerequisiteDetectionTool()` with context cancellation
3. ✅ **Tool Registration**: Registered in `createExecutionOnlyAgent()` with `structured_output` category
4. ✅ **System Prompt**: `formatPrerequisiteRulesForExecutionAgent()` adds rules to execution agent prompt
5. ✅ **Execution Loop**: Channel-based error detection in `executeSingleStep()`
6. ✅ **Navigation Logic**: Progress cleanup and event emission
7. ✅ **Frontend UI**: `PrerequisiteConfigPanel` for rule configuration
8. ✅ **Frontend Visualization**: Prerequisite edges in React Flow

---

## 📖 Related Documentation

- [Workflow Execution Guide](../workflow_execution.md) - Overall workflow execution flow
- [Agent Factory Guide](../agent_factory.md) - Agent creation and tool registration
- [Event System](../events.md) - Event types and handling
