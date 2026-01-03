# Orchestration Step Implementation

## Overview

**Status**: ✅ **FULLY IMPLEMENTED** (as Orchestration Step)

**Orchestration Step** (also referred to as "Routing Step" in planning docs) is a step type that acts as an orchestrator with multiple sub-agents. The main orchestration step executes with full MCP tools, evaluates its output to select a sub-agent based on conditions, and iteratively executes sub-agents until success criteria is met.

**Note**: The implementation uses "Orchestration" naming (`OrchestrationPlanStep`, `executeOrchestrationStep()`, etc.) rather than "Routing" naming.

## Key Concepts

### Main Routing Step
- **Purpose**: Execute with full MCP tools and evaluate output to determine routing
- **Access**: Full MCP tools (like normal step)
- **Output**: Structured evaluation to select route and check success criteria

### Sub-Agents
- **Purpose**: Execute specific tasks based on routing conditions
- **Scope**: Private to routing step (not accessible by other workflow steps)
- **Validation**: Disabled (execution + learning only)
- **Context**: Receives context from main routing step about what to do

### Execution Flow

**File**: [`controller_orchestration.go:52-1249`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_orchestration.go#L52)

```
1. Validate orchestration step (orchestration_step, routes, next_step_id)
2. Determine max iterations (step config > orchestrator default)
3. Main Loop (up to max iterations):
   a. Execute main orchestration step using OrchestrationOrchestratorAgent
      - Agent combines execution AND evaluation in one step
      - Returns OrchestrationResponse with:
        * success_criteria_met (bool)
        * selected_route_id (string)
        * instructions_to_sub_agent (string)
        * success_criteria_for_sub_agent (string)
   b. If success criteria met:
      - Call validation agent
      - If validation passes → Exit loop, route to NextStepID
   c. If success criteria NOT met:
      - Validate selected_route_id exists
      - Handle special routes ("end", "learning")
      - Execute selected sub-agent with dynamic instructions
      - Sub-agent output added to context for next iteration
      - Loop back to step 3a
4. If max iterations reached without success → Return failure
```

**Key Points**:
- Uses `OrchestrationOrchestratorAgent` which combines execution and evaluation (no separate evaluation agent)
- Sub-agents receive dynamic instructions from orchestrator (not static step description)
- Sub-agents use path format: `step-{N}-sub-agent-{index}`
- Max iterations configurable per step (default: orchestrator's max turns)

## Data Structures

**File**: [`controller_types.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_types.go)

### OrchestrationRoute
```go
type OrchestrationRoute struct {
    RouteID       string            `json:"route_id"`                  // Unique ID for this route
    RouteName     string            `json:"route_name"`                // Human-readable name
    Condition     string            `json:"condition"`                 // Condition description (e.g., "If error is authentication-related")
    SubAgentStep  PlanStepInterface `json:"sub_agent_step"`            // The sub-agent step to execute (private, not in main workflow)
    ContextToPass string            `json:"context_to_pass,omitempty"` // Optional: specific context to pass to sub-agent
}
```

### OrchestrationResponse
```go
type OrchestrationResponse struct {
    SelectedRouteID                string `json:"selected_route_id"`                            // Which route was selected (can be "end" to terminate workflow, empty to continue working, or a route ID)
    SuccessCriteriaMet             bool   `json:"success_criteria_met"`                         // Whether main orchestrator's success criteria is met
    SuccessReasoning               string `json:"success_reasoning,omitempty"`                  // Reasoning for success criteria evaluation
    InstructionsToSubAgent         string `json:"instructions_to_sub_agent,omitempty"`          // Instructions to pass to the selected sub-agent (replaces step description, required if selected_route_id is provided)
    SuccessCriteriaForSubAgent     string `json:"success_criteria_for_sub_agent,omitempty"`     // Success criteria to pass to the selected sub-agent (replaces step success criteria, required if selected_route_id is provided)
    ContextDependenciesForSubAgent string `json:"context_dependencies_for_sub_agent,omitempty"` // Context dependencies to pass to the selected sub-agent (replaces step context dependencies, optional)
    ContextOutputForSubAgent       string `json:"context_output_for_sub_agent,omitempty"`       // Context output file name to pass to the selected sub-agent (replaces step context output, optional)
}
```

### OrchestrationPlanStep
**File**: [`planning_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/planning_agent.go)

```go
type OrchestrationPlanStep struct {
    Type                  StepType                 `json:"type"`                           // Always "orchestration"
    ID                    string                   `json:"id"`                             // Stable step ID
    Title                 string                   `json:"title"`                          // Display title for the orchestration step wrapper
    OrchestrationStep     PlanStepInterface        `json:"orchestration_step,omitempty"`   // The main orchestrator step to execute
    OrchestrationRoutes   []PlanOrchestrationRoute `json:"orchestration_routes,omitempty"` // Array of possible routes with conditions
    NextStepID            string                   `json:"next_step_id,omitempty"`         // ID of step after orchestration completes (or "end")
    OrchestrationResponse *OrchestrationResponse   `json:"-"`                              // runtime: stores selected route and success evaluation
}
```

**Note**: Progress tracking is handled via `StepProgress.CompletedStepIndices` - orchestration steps don't have separate progress structure.

## Implementation Status

### Backend (Go) - ✅ Complete

1. ✅ **Data Structures** - `OrchestrationRoute`, `OrchestrationResponse`, `OrchestrationPlanStep` in `controller_types.go` and `planning_agent.go`
2. ✅ **Orchestration Agent** - `OrchestrationOrchestratorAgent` in [`orchestration_orchestrator_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/orchestration_orchestrator_agent.go) - combines execution and evaluation
3. ✅ **Execution Function** - `executeOrchestrationStep()` in [`controller_orchestration.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_orchestration.go)
4. ✅ **Main Loop Integration** - Orchestration step handling in `controller_execution.go`
5. ✅ **Progress Tracking** - Uses `StepProgress.CompletedStepIndices` for completion tracking
6. ✅ **Learning Agent** - `OrchestrationLearningAgent` in [`orchestration_learning_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/orchestration_learning_agent.go)
7. ✅ **Events** - Orchestration step events (step_started, route_selected, sub_agent_completed, step_finished)

### Frontend (TypeScript/React) - ✅ Complete

- ✅ TypeScript types for orchestration steps
- ✅ Canvas rendering for orchestration steps and sub-agents
- ✅ Step sidebar editing for orchestration steps
- ✅ React Flow integration with sub-agent nodes

## Key Differences from Decision Steps

| Feature | Decision Step | Orchestration Step |
|---------|--------------|-------------------|
| Choices | 2 (true/false) | N (multiple routes) |
| Sub-steps | None | Yes (sub-agents) |
| Loop | No | Yes (until success criteria met) |
| Context passing | One-way | Bidirectional (main ↔ sub-agent) |
| Success evaluation | N/A | Evaluated each iteration |
| Evaluation Agent | Separate `ConditionalAgent.EvaluateDecision()` | Built into `OrchestrationOrchestratorAgent` |
| Max Iterations | N/A | Configurable (default: 5) |
| Sub-agent Instructions | N/A | Dynamic instructions from orchestrator |

## Key Files & Locations

| Component | File | Key Functions |
|-----------|------|---------------|
| **Execution Function** | [`controller_orchestration.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_orchestration.go) | `executeOrchestrationStep()` |
| **Orchestrator Agent** | [`orchestration_orchestrator_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/orchestration_orchestrator_agent.go) | `OrchestrationOrchestratorAgent.ExecuteStructured()` |
| **Learning Agent** | [`orchestration_learning_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/orchestration_learning_agent.go) | `OrchestrationLearningAgent` |
| **Data Types** | [`controller_types.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_types.go) | `OrchestrationRoute`, `OrchestrationResponse` |
| **Plan Types** | [`planning_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/planning_agent.go) | `OrchestrationPlanStep`, `PlanOrchestrationRoute` |

## Example Use Case

**Step: "Handle API Error"**
- Main orchestration step: "Analyze error and determine type"
- Routes:
  - `auth-error`: Fix authentication issues
  - `network-error`: Fix network connectivity
  - `data-error`: Fix data validation
- Flow: Main step analyzes → Selects route → Sub-agent fixes → Main step re-evaluates → Repeat until success

**Implementation**:
- Uses `has_orchestration_step: true` in step config
- `orchestration_step`: Main step with description and success criteria
- `orchestration_routes`: Array of routes with conditions and sub-agent steps
- `next_step_id`: Step to route to when orchestration completes
