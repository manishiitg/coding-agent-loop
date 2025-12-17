# Routing Step Implementation

## Overview

**Routing Step** is a new step type that acts as an orchestrator with multiple sub-agents. The main routing step executes with full MCP tools, evaluates its output to select a sub-agent based on conditions, and iteratively executes sub-agents until success criteria is met.

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
1. Execute main routing step → Get output
2. Evaluate: Success criteria met?
   - **YES** → Route to `NextStepID` (exit routing step)
   - **NO** → Continue to route selection
3. Select route based on conditions → Execute ONE sub-agent
4. Sub-agent completes → Return output to main routing step
5. Loop back: Main routing step re-executes with sub-agent output in context
6. Repeat until success criteria met

## Data Structures

### RoutingRoute
```go
type RoutingRoute struct {
    RouteID       string   // Unique ID for route
    RouteName     string   // Human-readable name
    Condition     string   // Condition description (e.g., "If error is authentication-related")
    SubAgentStep  TodoStep // Sub-agent step to execute (private)
    ContextToPass string   // Optional: context to pass to sub-agent
}
```

### RoutingResponse
```go
type RoutingResponse struct {
    SelectedRouteID    string // Which route was selected
    Reasoning          string // Why this route was chosen
    SuccessCriteriaMet bool   // Whether main step's success criteria is met
    SuccessReasoning   string // Reasoning for success evaluation
}
```

### RoutingStepProgress
```go
type RoutingStepProgress struct {
    MainStepExecuted   bool   // Whether main routing step executed
    SelectedRouteID    string // Current selected route
    SubAgentCompleted  bool   // Whether sub-agent completed
    SuccessCriteriaMet bool   // Whether success criteria was met
    IterationCount     int    // How many times main step re-executed
    SubAgentOutput     string // Last sub-agent output (for context)
}
```

## Implementation Steps

1. ✅ **Data Structures** - Add routing types to `controller_types.go`
2. ⏳ **Routing Evaluation Agent** - Create agent to evaluate routing step output (similar to conditional agent)
3. ⏳ **Execution Function** - Create `executeRoutingStep` in `controller_routing.go`
4. ⏳ **Main Loop Integration** - Add routing step handling in `controller_execution.go`
5. ⏳ **Progress Tracking** - Add routing progress functions in `controller_progress.go`
6. ⏳ **Events** - Add routing step events (routing_started, route_selected, sub_agent_completed, routing_finished)

## Key Differences from Decision Steps

| Feature | Decision Step | Routing Step |
|---------|--------------|--------------|
| Choices | 2 (true/false) | N (multiple routes) |
| Sub-steps | None | Yes (sub-agents) |
| Loop | No | Yes (until success) |
| Context passing | One-way | Bidirectional (main ↔ sub-agent) |
| Success evaluation | N/A | Evaluated each iteration |

## Example Use Case

**Step: "Handle API Error"**
- Main routing step: "Analyze error and determine type"
- Routes:
  - `auth-error`: Fix authentication issues
  - `network-error`: Fix network connectivity
  - `data-error`: Fix data validation
- Flow: Main step analyzes → Selects route → Sub-agent fixes → Main step re-evaluates → Repeat until success

