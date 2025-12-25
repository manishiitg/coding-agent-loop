# Decision Step Implementation

## Overview

**Decision Step** (`has_decision_step`) is a workflow step type that executes a single step, evaluates its output to determine true/false, and routes to different next steps based on the evaluation result. This is distinct from conditional steps (`has_condition`) which evaluate a question without executing anything.

**Status**: ✅ **FULLY IMPLEMENTED** (as of December 2025)

## Key Concepts

### Decision Step vs Conditional Step

| Feature | Conditional Step (`has_condition`) | Decision Step (`has_decision_step`) |
|---------|-----------------------------------|-------------------------------------|
| **Evaluation Source** | `condition_question` → ConditionalLLM | Execute step → Evaluate output |
| **Execution** | No execution (evaluation only) | Executes single `decision_step` |
| **Branch Execution** | `IfTrueSteps[]` / `IfFalseSteps[]` arrays | Single step execution only |
| **Routing** | Optional `next_step_id` (defaults to sequential) | **Required** `if_true_next_step_id` / `if_false_next_step_id` |
| **Use Case** | Decision point without execution | Execute something, then decide based on result |

### Example Use Case

```json
{
  "id": "check-deployment-status",
  "title": "Check Deployment Status",
  "has_decision_step": true,
  "decision_step": {
    "id": "query-deployment-api",
    "title": "Query Deployment API",
    "description": "Call deployment API to get current status",
    "success_criteria": "API returns status response",
    "context_output": "deployment_status.json"
  },
  "decision_evaluation_question": "Is the deployment healthy and all services running?",
  "if_true_next_step_id": "proceed-to-next-phase",
  "if_false_next_step_id": "rollback-deployment"
}
```

## Data Structure

### PlanStep Fields

**File**: `../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/planning_agent.go`

```go
// Decision step fields
HasDecisionStep              bool      `json:"has_decision_step,omitempty"`            // true if step executes a single step and routes based on result
DecisionStep                 *PlanStep `json:"decision_step,omitempty"`                 // The single step to execute
DecisionEvaluationQuestion   string    `json:"decision_evaluation_question,omitempty"` // Question to evaluate step output
IfTrueNextStepID            string    `json:"if_true_next_step_id,omitempty"`         // REQUIRED: Next step if evaluation is true
IfFalseNextStepID           string    `json:"if_false_next_step_id,omitempty"`        // REQUIRED: Next step if evaluation is false
DecisionResponse             *DecisionResponse `json:"decision_response,omitempty"`      // runtime: stores evaluation result
```

### TodoStep Fields

**File**: `../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_types.go`

Same structure as PlanStep, with `TodoStep` type for `DecisionStep` field.

## Implementation

### Execution Flow

```
1. Execute DecisionStep (single step)
   ↓
2. Get execution output/result
   ↓
3. Evaluate output using ConditionalLLM with DecisionEvaluationQuestion
   ↓
4. Route to IfTrueNextStepID or IfFalseNextStepID
```

### Core Implementation

**File**: `../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_decision.go`

**Function**: `executeDecisionStep()`

The implementation:
1. Validates required fields (decision_step, evaluation_question, routing IDs)
2. Executes the inner decision step using `executeSingleStep()`
3. Evaluates the execution output using `ConditionalLLM.Decide()`
4. Stores the decision result and reasoning
5. Emits appropriate events (step_started, decision_evaluated, step_finished)
6. Returns the decision result for routing by the main execution loop

### Step Conversion

**File**: `../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/planning_management.go`

**Function**: `convertPlanStepsToTodoSteps()`

Converts `PlanStep.DecisionStep` to `TodoStep.DecisionStep` during plan-to-todo conversion.

### Validation

**File**: `../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/planning_management.go`

**Function**: `validatePlan()`

Validates:
- ✅ `decision_step` is present and valid
- ✅ `decision_evaluation_question` is not empty
- ✅ `if_true_next_step_id` is not empty
- ✅ `if_false_next_step_id` is not empty
- ✅ Decision step cannot contain another decision step (nested decision steps not allowed)

## Planning Agent Integration

### Schema Updates

**File**: `../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/planning_agent.go`

Decision step fields are included in:
- `getUpdatePlanStepsSchema()` - For updating existing steps
- `getAddPlanStepsSchema()` - For adding new steps
- `ExecuteStructured()` - For creating new plans

### Planning Tools

The following tools support decision steps:
- `convert_step_to_decision_step` - Convert regular step to decision step
- `update_decision_step` - Update decision step properties
- `convert_decision_to_regular` - Convert decision step back to regular step

**Location**: `planning_agent.go` - `registerPlanModificationTools()`

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
- Shows inner step information (title, description)
- Displays evaluation question
- Renders true/false routing edges
- Shows context inputs/outputs from the inner step
- Supports status indicators and execution controls

**File**: `../frontend/src/components/workflow/hooks/usePlanToFlow.ts`

Converts decision steps to React Flow nodes and edges:
- Creates decision node type
- Generates true/false routing edges
- Handles nested step visualization

### Step Sidebar

**File**: `../frontend/src/components/workflow/canvas/StepSidebar.tsx`

Provides UI for editing decision steps:
- Toggle `has_decision_step`
- Edit inner `decision_step` (nested step editor)
- Edit `decision_evaluation_question`
- Edit `if_true_next_step_id` / `if_false_next_step_id`

## Progress Tracking

### Step Progress

**File**: `../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_progress.go`

Decision step execution is tracked with:
- Decision step execution completion
- Evaluation result (true/false)
- Next step routing
- Run number calculation based on evaluation counts

### Execution Logs

When `saveValidationResponses` is enabled, the following logs are saved:
- `execution/step-{X}-decision/decision-inner-step.json` - Inner step execution result
- `validation/step-{X}/decision-evaluation.json` - Decision evaluation result with reasoning

## Event Emission

### Events

**File**: `../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_decision.go`

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
- Step index: Parent step index
- Step path: `step-{X}` for parent, `step-{X}-decision` for inner step

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

5. **Nested Decision Steps**
   - Validation prevents decision step containing another decision step

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

- Existing plans without `has_decision_step` continue to work
- Decision steps are an optional feature
- No migration needed for existing plans

### Step Config Matching

Decision step's `decision_step` is matched by ID in `step_config.json`:
- Use `MatchStepConfigByID()` to find config for `decision_step.ID`
- Apply config to decision step execution

## Key Design Decisions

1. **Single Step Execution**: Decision steps execute only one step (the `decision_step`), unlike conditional steps which can execute multiple steps in branches.

2. **Required Routing**: Both `if_true_next_step_id` and `if_false_next_step_id` are required, ensuring explicit routing for all outcomes.

3. **No Nested Decision Steps**: Decision steps cannot contain other decision steps to avoid complexity.

4. **Separate Evaluation**: Uses `ConditionalLLM` for evaluation rather than a full agent, keeping the evaluation lightweight.

5. **Execution Logs**: Stores both inner step execution and evaluation results for debugging and analysis.

## Summary

Decision Step provides a way to execute a single step, evaluate its output, and route to different next steps based on the evaluation. It complements conditional steps by adding execution capability to the decision-making process. The feature is fully implemented across backend, frontend, and schemas, with comprehensive validation, logging, and event emission.
