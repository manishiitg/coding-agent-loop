# TodoStep Type Safety Refactor

## Problem
Currently, `TodoStep` is a union type with boolean flags (`HasOrchestrationStep`, `HasDecisionStep`, etc.) and optional fields. This creates confusion:
- `OrchestrationStep` is itself a `TodoStep`, making it unclear if you're dealing with the wrapper or inner step
- Functions need to check `step.OrchestrationStep != nil` to determine if it's a wrapper or inner step
- Config access is ambiguous: `step.AgentConfigs` vs `step.OrchestrationStep.AgentConfigs`
- We already have type-safe plan step structs (`RegularPlanStep`, `ConditionalPlanStep`, `DecisionPlanStep`, `OrchestrationPlanStep`) but convert them to `TodoStep` for execution, losing type safety

## Solution
**Use the existing `PlanStep` types directly for execution!** Just add the runtime fields (`ConditionResult`, `DecisionResponse`, `OrchestrationResponse`, `AgentConfigs`) to the plan step types. This eliminates the need for `TodoStep` conversion and maintains type safety throughout.

## Proposed Structure

### 1. Add Runtime Fields to Existing PlanStep Types

Instead of creating new types, just add the runtime fields to the existing plan step structs:

```go
// RegularPlanStep - add AgentConfigs
type RegularPlanStep struct {
    Type StepType `json:"type"`
    CommonStepFields
    HasLoop         bool   `json:"has_loop"`
    LoopCondition   string `json:"loop_condition,omitempty"`
    MaxIterations   int    `json:"max_iterations,omitempty"`
    LoopDescription string `json:"loop_description,omitempty"`
    AgentConfigs    *AgentConfigs `json:"agent_configs,omitempty"` // ADD THIS
}

// ConditionalPlanStep - add runtime fields and AgentConfigs
type ConditionalPlanStep struct {
    Type StepType `json:"type"`
    CommonStepFields
    ConditionQuestion string              `json:"condition_question,omitempty"`
    ConditionContext  string              `json:"condition_context,omitempty"`
    IfTrueSteps       []PlanStepInterface `json:"if_true_steps,omitempty"`
    IfFalseSteps      []PlanStepInterface `json:"if_false_steps,omitempty"`
    IfTrueNextStepID  string              `json:"if_true_next_step_id,omitempty"`
    IfFalseNextStepID string              `json:"if_false_next_step_id,omitempty"`
    ConditionResult   *bool               `json:"condition_result,omitempty"`   // ADD THIS (runtime)
    ConditionReason   string              `json:"condition_reason,omitempty"`    // ADD THIS (runtime)
    AgentConfigs      *AgentConfigs       `json:"agent_configs,omitempty"`      // ADD THIS
}

// DecisionPlanStep - add runtime fields and AgentConfigs
type DecisionPlanStep struct {
    Type                       StepType          `json:"type"`
    ID                         string            `json:"id"`
    Title                      string            `json:"title"`
    DecisionStep               PlanStepInterface `json:"decision_step,omitempty"`
    DecisionEvaluationQuestion string            `json:"decision_evaluation_question,omitempty"`
    IfTrueNextStepID           string            `json:"if_true_next_step_id,omitempty"`
    IfFalseNextStepID          string            `json:"if_false_next_step_id,omitempty"`
    DecisionResult             *bool             `json:"decision_result,omitempty"`   // Already exists
    DecisionReason             string            `json:"decision_reason,omitempty"`    // Already exists
    DecisionResponse           *DecisionResponse `json:"decision_response,omitempty"` // ADD THIS (runtime)
    AgentConfigs               *AgentConfigs     `json:"agent_configs,omitempty"`      // ADD THIS
}

// OrchestrationPlanStep - add runtime fields and AgentConfigs
type OrchestrationPlanStep struct {
    Type                StepType                 `json:"type"`
    ID                  string                   `json:"id"`
    Title               string                   `json:"title"`
    OrchestrationStep   PlanStepInterface        `json:"orchestration_step,omitempty"` // Already type-safe!
    OrchestrationRoutes []PlanOrchestrationRoute `json:"orchestration_routes,omitempty"`
    NextStepID          string                   `json:"next_step_id,omitempty"`
    OrchestrationResponse *OrchestrationResponse `json:"orchestration_response,omitempty"` // ADD THIS (runtime)
    AgentConfigs        *AgentConfigs            `json:"agent_configs,omitempty"`         // ADD THIS
}
```

### 3. Key Benefits

1. **Type Safety**: `OrchestrationStep` is already `PlanStepInterface` (type-safe!). No conversion needed.

2. **Clear Config Access**: 
   - Wrapper: `orchestrationStep.AgentConfigs`
   - Inner step: `orchestrationStep.OrchestrationStep.(*RegularPlanStep).AgentConfigs` (type assertion)

3. **No Ambiguity**: Type switch makes it clear what you're dealing with:
   ```go
   switch step := step.(type) {
   case *OrchestrationPlanStep:
       // step.OrchestrationStep is guaranteed to be PlanStepInterface
       if innerStep, ok := step.OrchestrationStep.(*RegularPlanStep); ok {
           config := innerStep.AgentConfigs // Type-safe!
       }
       wrapperConfig := step.AgentConfigs // Wrapper config
   case *RegularPlanStep:
       config := step.AgentConfigs // Simple step
   }
   ```

4. **Compile-time Safety**: Can't accidentally access `OrchestrationStep` on a `RegularPlanStep`.

5. **No Conversion Needed**: Use plan steps directly for execution - no `convertTypedStepToTodoStep` needed!

### 4. Migration Strategy

1. **Phase 1**: Add runtime fields (`AgentConfigs`, `ConditionResult`, `DecisionResponse`, `OrchestrationResponse`) to existing `PlanStep` types
2. **Phase 2**: Update `convertTypedStepToTodoStep` to populate runtime fields on plan steps instead of creating `TodoStep`
3. **Phase 3**: Update execution functions to accept `PlanStepInterface` instead of `TodoStep`
4. **Phase 4**: Remove `TodoStep` union type (or keep for backward compatibility with events)

### 5. Example Usage

```go
// Before (confusing):
func executeStep(step TodoStep) {
    if step.HasOrchestrationStep && step.OrchestrationStep != nil {
        config := step.OrchestrationStep.AgentConfigs // Is this the wrapper or inner?
    }
}

// After (clear):
func executeStep(step PlanStepInterface) {
    switch s := step.(type) {
    case *OrchestrationPlanStep:
        // Clear: s.OrchestrationStep is PlanStepInterface (type-safe!)
        if innerStep, ok := s.OrchestrationStep.(*RegularPlanStep); ok {
            innerConfig := innerStep.AgentConfigs // Inner step config
        }
        wrapperConfig := s.AgentConfigs // Wrapper config
    case *RegularPlanStep:
        config := s.AgentConfigs // Simple step
    }
}
```

### 6. Why This Is Better

- **Reuses existing types**: No need to create duplicate structs
- **Maintains type safety**: `OrchestrationStep` is already `PlanStepInterface`
- **Simpler migration**: Just add fields, don't create new types
- **Less code duplication**: One set of types for planning and execution

## Implementation Notes

- Keep JSON marshaling/unmarshaling working for backward compatibility
- Use type field for JSON discrimination (like plan steps)
- Update all functions that accept `TodoStep` to use `TodoStepInterface`
- Update event structures to use `TodoStepInterface` (may need wrapper for JSON)

