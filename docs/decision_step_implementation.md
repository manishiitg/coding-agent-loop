# Decision Step Implementation

## Overview

**Decision Step** (`type: "decision"`) is a workflow step type that executes a step, evaluates its output to determine true/false, and routes to different next steps based on the evaluation result. This is distinct from conditional steps which evaluate a question without executing anything.

**Status**: ✅ **FULLY IMPLEMENTED** (as of January 2026 - updated to flattened structure)

## Key Concepts

### Decision Step vs Conditional Step

| Feature | Conditional Step (`type: "conditional"`) | Decision Step (`type: "decision"`) |
|---------|-----------------------------------|-------------------------------------|
| **Evaluation Source** | `condition_question` → `ConditionalAgent.Decide()` | Execute step → `ConditionalAgent.EvaluateDecision()` |
| **Execution** | No execution (evaluation only) | Executes the decision step itself |
| **Branch Execution** | `IfTrueSteps[]` / `IfFalseSteps[]` arrays | Single step execution only |
| **Routing** | Optional `next_step_id` (defaults to sequential) | **Required** `if_true_next_step_id` / `if_false_next_step_id` |
| **Use Case** | Decision point without execution | Execute something, then decide based on result |
| **Evaluation Input** | `conditionContext` (previous step output) | `executionOutput` (step execution result) |
| **Learning History** | Loaded for conditional step ID | Loaded for decision step ID |

### Example Use Case

```json
{
  "type": "decision",
  "id": "check-deployment-status",
  "title": "Check Deployment Status",
  "description": "Call deployment API to get current status",
  "success_criteria": "API returns status response",
  "context_output": "deployment_status.json",
  "decision_evaluation_question": "Is the deployment healthy and all services running?",
  "if_true_next_step_id": "proceed-to-next-phase",
  "if_false_next_step_id": "rollback-deployment"
}
```

## Data Structure

### DecisionPlanStep (Flattened)

**File**: `../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go`

The decision step now uses a flattened structure with embedded `CommonStepFields`:

```go
type DecisionPlanStep struct {
    Type StepType `json:"type"` // Always "decision"

    // Embedded CommonStepFields
    CommonStepFields           // ID, Title, Description, SuccessCriteria, ContextDependencies, ContextOutput, etc.

    // Decision-specific fields
    DecisionEvaluationQuestion string            `json:"decision_evaluation_question,omitempty"` // Question to evaluate step output
    IfTrueNextStepID           string            `json:"if_true_next_step_id,omitempty"`         // REQUIRED: Next step if evaluation is true
    IfFalseNextStepID          string            `json:"if_false_next_step_id,omitempty"`        // REQUIRED: Next step if evaluation is false
    DecisionResponse           *DecisionResponse `json:"-"`                                       // runtime: stores evaluation result
    AgentConfigs               *AgentConfigs     `json:"agent_configs,omitempty"`                // Step-specific agent configuration
}
```

### Backward Compatibility

The system supports automatic migration from the legacy nested format:

**Legacy Format (auto-migrated on load):**
```json
{
  "type": "decision",
  "id": "wrapper-id",
  "title": "Decision Wrapper",
  "decision_step": {
    "type": "regular",
    "id": "inner-step-id",
    "title": "Inner Step",
    "description": "...",
    "success_criteria": "..."
  },
  "decision_evaluation_question": "...",
  "if_true_next_step_id": "...",
  "if_false_next_step_id": "..."
}
```

When the legacy format is detected during JSON unmarshal, fields from `decision_step` are automatically copied to the parent level.

## Implementation

### Execution Flow

The decision step has a two-phase execution model:

```
1. Execute Decision Step (the step itself)
   - Uses executeSingleStep() with isDecisionInnerStep=true
   - Step has its own Description, SuccessCriteria, etc.
   ↓
2. Get execution output/result
   ↓
3. Load learning history for decision step (via LoadStepLearningHistory())
   ↓
4. Evaluate output using ConditionalAgent.EvaluateDecision()
   - Uses execution output directly (not conditionContext)
   - Includes learning history, variables, code execution mode
   ↓
5. Auto-unlock learnings if result is false
   ↓
6. Route to IfTrueNextStepID or IfFalseNextStepID
```

### Core Implementation

**File**: [`controller_decision.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_decision.go)

**Function**: `executeDecisionStep()`

The implementation:
1. Validates required fields (description, success_criteria, evaluation_question, routing IDs)
2. Executes the decision step itself using `executeSingleStep()` with `isDecisionInnerStep=true` flag
3. Loads learning history for the step via `LoadStepLearningHistory()`
4. Determines code execution mode (step config > orchestrator default)
5. Evaluates the execution output using `ConditionalAgent.EvaluateDecision()` (not `Decide()`)
6. Auto-unlocks learnings for the step if decision result is false
7. Stores the decision result and reasoning
8. Emits appropriate events (step_started, decision_evaluated, step_finished)
9. Returns the decision result for routing by the main execution loop

**Key Details**:
- Uses `ConditionalAgent.EvaluateDecision()` method (different from `Decide()` used by conditional steps)
- `EvaluateDecision()` takes `executionOutput` directly (not `conditionContext`)
- Learning history is loaded using the step's own ID
- Variables (names and values) are passed to the evaluation agent

### Validation

**File**: [`planning_management.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_management.go)

**Function**: `validateDecisionStepTyped()`

Validates:
- ✅ `description` is not empty
- ✅ `success_criteria` is not empty
- ✅ `decision_evaluation_question` is not empty
- ✅ `if_true_next_step_id` is not empty
- ✅ `if_false_next_step_id` is not empty

## Evaluation Method: EvaluateDecision vs Decide

**File**: [`conditional_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/conditional_agent.go)

Decision steps use `EvaluateDecision()` method, which differs from `Decide()` used by conditional steps:

| Method | Used By | Input | Output | Tool Submission |
|--------|---------|-------|--------|-----------------|
| `Decide()` | Conditional steps | `conditionContext` (previous step output), `question` | `ConditionalResponse` with `result` and `reason` | Direct JSON response |
| `EvaluateDecision()` | Decision steps | `executionOutput` (inner step result), `question` | `DecisionResponse` with `result` and `reasoning` | Via `submit_decision_result` tool |

**Key Differences**:
- `EvaluateDecision()` uses `ExecuteStructuredWithInputProcessorViaTool()` with `submit_decision_result` tool
- `Decide()` uses `ExecuteStructuredWithInputProcessor()` with direct JSON response
- `EvaluateDecision()` takes execution output directly, not condition context
- Both methods support code execution mode and learning history

## Planning Agent Integration

### Schema Updates

**File**: [`planning_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go)

Decision step fields are included in:
- `getAddDecisionStepSchema()` - For adding new decision steps
- `getUpdateDecisionStepSchema()` - For updating existing decision steps

### Planning Tools

The following tools support decision steps:
- `add_decision_step` - Add a new decision step
- `update_decision_step` - Update decision step properties

**Location**: `planning_agent.go` - tool registration

## Frontend Integration

### TypeScript Types

**Files**:
- `../frontend/src/generated/events.ts`
- `../frontend/src/generated/events-bridge.ts`
- `../frontend/src/utils/stepConfigMatching.ts`

All include decision step fields in `PlanStep` and `TodoStep` interfaces.

### Canvas Rendering

**File**: `../frontend/src/components/workflow/nodes/DecisionNode.tsx`

Dedicated `DecisionNode` component that:
- Displays the decision step with diamond shape
- Shows step information (title, description)
- Displays evaluation question
- Renders true/false routing edges
- Shows context inputs/outputs
- Supports status indicators and execution controls

**File**: `../frontend/src/components/workflow/hooks/usePlanToFlow.ts`

Converts decision steps to React Flow nodes and edges:
- Creates decision node type
- Generates true/false routing edges
- Handles nested step visualization

### Step Sidebar

**File**: `../frontend/src/components/workflow/canvas/StepSidebar.tsx`

Provides UI for editing decision steps:
- Edit step fields (description, success_criteria, etc.)
- Edit `decision_evaluation_question`
- Edit `if_true_next_step_id` / `if_false_next_step_id`

## Progress Tracking

### Step Progress

**File**: `../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_progress.go`

Decision step execution is tracked with:
- Decision step execution completion
- Evaluation result (true/false)
- Next step routing
- Run number calculation based on evaluation counts

### Execution Logs

When `saveValidationResponses` is enabled, the following logs are saved:
- `execution/step-{X}-decision/decision-execution.json` - Step execution result (via `getExecutionFolderPathForLogs()`)
- `validation/step-{X}/decision-evaluation.json` - Decision evaluation result with reasoning

**File Paths**:
- Step execution: Uses `getExecutionFolderPathForLogs()` helper
- Decision evaluation: Uses `getValidationFolderPath()` helper

## Learning History and Code Execution Mode

### Learning History Loading

**File**: [`controller_decision.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_decision.go)

Learning history is loaded for decision evaluation using the step's ID:

```go
// Read learnings for the decision step (learnings are stored under the step's ID)
learningHistory, _ := hcpo.LoadStepLearningHistory(ctx, step.GetID(), stepIndex, decisionStepPath, "decision")
```

**Key Rules**:
- Uses step's own ID for learning folder identification
- Loaded via `LoadStepLearningHistory()` helper method
- Passed separately to `EvaluateDecision()` (not included in execution output)

### Code Execution Mode

Code execution mode is determined with priority: step config > orchestrator default

```go
var isCodeExecutionMode bool
stepConfigs := getAgentConfigs(step)
if stepConfigs != nil && stepConfigs.UseCodeExecutionMode != nil {
    isCodeExecutionMode = *stepConfigs.UseCodeExecutionMode
} else {
    isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
}
```

**Inheritance**:
- Uses step's own agent config if set
- Falls back to orchestrator default if not set
- Code execution mode is passed to `EvaluateDecision()` for evaluation agent

### Auto-Unlock Learnings

**File**: [`controller_decision.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_decision.go)

When decision result is `false`, learnings are automatically unlocked for the step:

```go
if !decisionResponse.Result {
    // Auto-unlock learnings for step so it can learn from the failure
    hcpo.unlockStepLearningsInConfig(ctx, step.GetID())
}
```

**Rationale**: Allows the step to learn from failures when decision evaluation returns false.

## Event Emission

### Events

**File**: [`controller_decision.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_decision.go)

Emits the following events:
- `step_started` - When decision step starts
- `orchestrator_agent_start` - When decision step execution starts
- `orchestrator_agent_end` - When decision step execution completes
- `decision_evaluated` - When evaluation completes (includes result and reasoning)
- `step_finished` - When decision step completes (after routing)

### Event Context

Orchestrator context includes:
- Phase: `"execution"` for decision step execution
- Phase: `"decision_evaluation"` for evaluation
- Step index: Step index
- Step path: `step-{X}` for the step, `step-{X}-decision` for execution path

## Schema Definitions

Decision step fields are defined in:
- `../schemas/unified-events-complete.schema.json`
- `../schemas/polling-event.schema.json`
- `../agent_go/schemas/unified-events-complete.schema.json`
- `../agent_go/schemas/polling-event.schema.json`

## Testing Considerations

### Test Cases

1. **Basic Decision Step**
   - Execute decision step
   - Evaluate output (true case)
   - Route to if_true_next_step_id

2. **False Evaluation**
   - Execute decision step
   - Evaluate output (false case)
   - Route to if_false_next_step_id

3. **End Workflow**
   - Route to "end" terminates workflow

4. **Missing Fields**
   - Validation catches missing required fields

5. **Legacy Format Migration**
   - Load old plan.json with nested `decision_step` → auto-migrates and works

## Implementation Status

### Backend (Go) - ✅ Complete

- ✅ Add fields to `PlanStep` struct
- ✅ Add fields to `TodoStep` struct
- ✅ Update step conversion logic
- ✅ Add `executeDecisionStep()` function
- ✅ Update main execution loop to handle decision steps
- ✅ Add validation for decision steps
- ✅ Update planning agent schemas
- ✅ Update planning agent prompts
- ✅ Add planning tools
- ✅ Add event emission
- ✅ Add progress tracking
- ✅ Add execution logging

### Frontend (TypeScript/React) - ✅ Complete

- ✅ Update TypeScript types
- ✅ Add frontend canvas rendering (DecisionNode)
- ✅ Add frontend step sidebar editing
- ✅ Add React Flow integration
- ✅ Add event handling

### Schema - ✅ Complete

- ✅ Update unified events schema
- ✅ Update polling event schema

## Migration Notes

### Backward Compatibility

- Existing plans with legacy nested `decision_step` format are automatically migrated on load
- When a plan with the old format is loaded, fields from the nested `decision_step` are copied to the parent level
- The migration is seamless - no manual intervention required
- A warning is logged when legacy format is detected and migrated

### Step Config Matching

Decision step is matched by ID in `step_config.json`:
- Use `MatchStepConfigByID()` to find config for the decision step's ID
- Apply config to decision step execution

## Key Design Decisions

1. **Flattened Structure**: Decision steps use a flattened structure with embedded `CommonStepFields`, providing a single ID for the step (unlike the previous nested structure).

2. **Two-Phase Execution**: Decision steps have two execution phases:
   - Execution Phase: The step itself executes using its Description, SuccessCriteria, etc.
   - Evaluation Phase: `ConditionalAgent.EvaluateDecision()` evaluates the output against `DecisionEvaluationQuestion`

3. **Required Routing**: Both `if_true_next_step_id` and `if_false_next_step_id` are required, ensuring explicit routing for all outcomes.

4. **Full Agent Evaluation**: Uses `ConditionalAgent.EvaluateDecision()` (full agent with workspace tools), not a lightweight LLM call.

5. **Execution Logs**: Stores both step execution and evaluation results for debugging and analysis.

6. **Auto-Unlock on Failure**: Automatically unlocks learnings for the step when decision result is false, enabling learning from failures.

7. **Code Execution Mode Support**: Supports code execution mode for both step execution and evaluation.

8. **Variable Support**: Passes variable names and values to evaluation agent for context-aware decisions.

9. **Backward Compatibility**: Legacy nested `decision_step` format is automatically migrated on load.

## Summary

Decision Step provides a way to execute a step, evaluate its output, and route to different next steps based on the evaluation. It complements conditional steps by adding execution capability to the decision-making process. The feature uses a flattened structure with embedded CommonStepFields, providing a single ID for learning storage and step configuration. The feature is fully implemented across backend, frontend, and schemas, with comprehensive validation, logging, and event emission.
