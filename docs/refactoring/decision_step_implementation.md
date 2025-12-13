# Decision Step Implementation

## Overview

**Decision Step** (`has_decision_step`) is a new workflow step type that executes a single step, evaluates its output to determine true/false, and routes to different next steps based on the evaluation result. This is distinct from conditional steps (`has_condition`) which evaluate a question without executing anything.

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

### PlanStep Fields (planning_agent.go)

```go
// Decision step fields (NEW)
HasDecisionStep              bool     `json:"has_decision_step,omitempty"`              // true if step executes a single step and routes based on result
DecisionStep                 PlanStep `json:"decision_step,omitempty"`                   // The single step to execute
DecisionEvaluationQuestion   string   `json:"decision_evaluation_question,omitempty"`   // Question to evaluate step output
IfTrueNextStepID            string   `json:"if_true_next_step_id,omitempty"`           // REQUIRED: Next step if evaluation is true
IfFalseNextStepID           string   `json:"if_false_next_step_id,omitempty"`          // REQUIRED: Next step if evaluation is false
DecisionResult               *bool    `json:"decision_result,omitempty"`                 // runtime: stores evaluation result
DecisionReason               string   `json:"decision_reason,omitempty"`                 // runtime: stores evaluation reasoning
```

### TodoStep Fields (controller_types.go)

Same structure as PlanStep, but with `TodoStep` type for `DecisionStep` field.

## Execution Flow

### High-Level Flow

```
1. Execute DecisionStep (single step)
   ↓
2. Get execution output/result
   ↓
3. Evaluate output using ConditionalLLM with DecisionEvaluationQuestion
   ↓
4. Route to IfTrueNextStepID or IfFalseNextStepID
```

### Implementation Location

**File**: `controller_execution.go`

**Function**: `executeDecisionStep()` (new function, similar to `executeConditionalStep()`)

### Execution Steps

1. **Execute Decision Step**
   - Call `executeSingleStep()` for the `decision_step`
   - Store execution result in memory
   - Track execution in step-specific folder: `execution/step-{parentIndex}-decision/`

2. **Evaluate Output**
   - Build evaluation context from execution result
   - Call `ConditionalLLM.Decide()` with:
     - `context`: Execution output
     - `question`: `DecisionEvaluationQuestion`
     - `stepIndex`: Parent step index
   - Store result in `DecisionResult` and `DecisionReason`

3. **Route to Next Step**
   - If `DecisionResult == true` → Use `IfTrueNextStepID`
   - If `DecisionResult == false` → Use `IfFalseNextStepID`
   - Find target step by ID and jump to it
   - Handle "end" special case to terminate workflow

### Code Structure

```go
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeDecisionStep(
    ctx context.Context,
    step TodoStep,
    stepIndex int,
    depth int,
    previousExecutionResults []string,
    previousContextFiles []string,
    progress *StepProgress,
    iteration int,
    execCtx *ExecutionContext,
) error {
    // 1. Execute decision_step
    executionResult, _, err := hcpo.executeSingleStep(...)
    
    // 2. Evaluate output
    conditionalAgent := hcpo.getConditionalAgentForStep(...)
    evaluationContext := buildEvaluationContext(executionResult)
    conditionalResponse, err := conditionalAgent.Decide(
        ctx,
        evaluationContext,
        step.DecisionEvaluationQuestion,
        stepIndex,
        0,
        isCodeExecutionMode,
        learningHistory,
    )
    
    // 3. Store result
    step.DecisionResult = &conditionalResponse.Result
    step.DecisionReason = conditionalResponse.Reason
    
    // 4. Route to next step (handled in main execution loop)
    // Return next_step_id for routing
}
```

## Step Conversion

### Location

**File**: `planning_management.go`

**Function**: `convertPlanStepsToTodoSteps()` - Add decision step handling

### Conversion Logic

```go
// In convertPlanStepsToTodoSteps()
if step.HasDecisionStep {
    // Convert decision_step from PlanStep to TodoStep
    decisionTodoStep, err := convertSingleStep(step.DecisionStep, stepConfigs)
    if err != nil {
        return nil, err
    }
    
    todoStep.DecisionStep = decisionTodoStep
    todoStep.HasDecisionStep = true
    todoStep.DecisionEvaluationQuestion = step.DecisionEvaluationQuestion
    todoStep.IfTrueNextStepID = step.IfTrueNextStepID
    todoStep.IfFalseNextStepID = step.IfFalseNextStepID
}
```

## Validation

### Required Fields

When `has_decision_step == true`:
- ✅ `decision_step` (required) - Must be a valid PlanStep
- ✅ `decision_evaluation_question` (required) - Question for evaluation
- ✅ `if_true_next_step_id` (required) - Next step ID or "end"
- ✅ `if_false_next_step_id` (required) - Next step ID or "end"

### Validation Location

**File**: `planning_management.go`

**Function**: `validatePlan()` - Add decision step validation

```go
if step.HasDecisionStep {
    if step.DecisionStep.ID == "" {
        return fmt.Errorf("decision step missing ID")
    }
    if step.DecisionEvaluationQuestion == "" {
        return fmt.Errorf("decision step missing evaluation question")
    }
    if step.IfTrueNextStepID == "" {
        return fmt.Errorf("decision step missing if_true_next_step_id")
    }
    if step.IfFalseNextStepID == "" {
        return fmt.Errorf("decision step missing if_false_next_step_id")
    }
}
```

## Planning Agent Integration

### Schema Updates

**File**: `planning_agent.go`

**Functions**:
- `getUpdatePlanStepsSchema()` - Add decision step fields
- `getAddPlanStepsSchema()` - Add decision step fields
- `ExecuteStructured()` - Add decision step to CREATE schema

### Planning Prompt Updates

**File**: `planning_agent.go`

**Function**: `planningSystemPromptProcessorForCreate()`

Add section:
```markdown
### Decision Steps (has_decision_step=true)

**When to use**: When you need to execute a step and route based on its output.

**Configuration**:
- Set has_decision_step = true
- Set decision_step: The single step to execute
- Set decision_evaluation_question: Question to evaluate step output
- Set if_true_next_step_id: REQUIRED - Next step if evaluation is true
- Set if_false_next_step_id: REQUIRED - Next step if evaluation is false

**Example**: Execute deployment check, then route to success or rollback based on result.
```

## Planning Tools

### New Tools (Optional)

If needed, add tools similar to conditional step tools:
- `convert_step_to_decision_step` - Convert regular step to decision step
- `update_decision_step` - Update decision step properties
- `convert_decision_to_regular` - Convert back to regular step

**Location**: `planning_agent.go` - `registerPlanModificationTools()`

## Frontend Integration

### TypeScript Types

**File**: `frontend/src/services/api-types.ts`

Add to `PlanStep` interface:
```typescript
has_decision_step?: boolean;
decision_step?: PlanStep;
decision_evaluation_question?: string;
if_true_next_step_id?: string;
if_false_next_step_id?: string;
decision_result?: boolean;
decision_reason?: string;
```

### Canvas Rendering

**File**: `frontend/src/components/workflow/hooks/usePlanToFlow.ts`

Add decision step node type:
- Create `DecisionNode` component (similar to `ConditionalNode`)
- Render decision step with execution step preview
- Show evaluation question
- Render routing edges (true/false branches)

### Step Sidebar

**File**: `frontend/src/components/workflow/canvas/StepSidebar.tsx`

Add decision step editing:
- Toggle `has_decision_step`
- Edit `decision_step` (nested step editor)
- Edit `decision_evaluation_question`
- Edit `if_true_next_step_id` / `if_false_next_step_id`

## Progress Tracking

### Step Progress

**File**: `controller_progress.go`

Decision step execution should be tracked:
- Decision step execution completion
- Evaluation result (true/false)
- Next step routing

### Branch Progress (Not Applicable)

Decision steps do NOT use `BranchStepProgress` (unlike conditional steps). They execute a single step and route directly.

## Event Emission

### Events

- `step_started` - When decision step starts
- `orchestrator_agent_start` - When decision step execution starts
- `orchestrator_agent_end` - When decision step execution completes
- `orchestrator_agent_start` - When evaluation starts (conditional agent)
- `orchestrator_agent_end` - When evaluation completes
- `step_completed` - When decision step completes (after routing)

### Event Context

Set orchestrator context:
- Phase: `"execution"` for decision step execution
- Phase: `"decision_evaluation"` for evaluation
- Step index: Parent step index

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
   - Decision step can contain decision step in decision_step (if needed)

## Migration Notes

### Backward Compatibility

- Existing plans without `has_decision_step` continue to work
- Decision steps are optional feature
- No migration needed for existing plans

### Step Config Matching

Decision step's `decision_step` is matched by ID in `step_config.json`:
- Use `MatchStepConfigByID()` to find config for `decision_step.ID`
- Apply config to decision step execution

## Implementation Checklist

- [ ] Add fields to `PlanStep` struct
- [ ] Add fields to `TodoStep` struct
- [ ] Update step conversion logic
- [ ] Add `executeDecisionStep()` function
- [ ] Update main execution loop to handle decision steps
- [ ] Add validation for decision steps
- [ ] Update planning agent schemas
- [ ] Update planning agent prompts
- [ ] Add planning tools (if needed)
- [ ] Update frontend TypeScript types
- [ ] Add frontend canvas rendering
- [ ] Add frontend step sidebar editing
- [ ] Add event emission
- [ ] Add progress tracking
- [ ] Write tests

## Summary

Decision Step provides a way to execute a single step, evaluate its output, and route to different next steps based on the evaluation. It complements conditional steps by adding execution capability to the decision-making process.

