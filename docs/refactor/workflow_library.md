# Workflow Library Extraction

This document describes the extraction of the workflow orchestration system into a reusable `pkg/workflow` package.

## 1. Overview

### Goals
- Create a reusable workflow engine that can be used in external projects
- Provide a clean, well-documented API for workflow orchestration
- Enable standalone usage of the TodoTask orchestration pattern

### Benefits
- **Standalone usage**: External projects can use the workflow library without the full orchestrator
- **Cleaner architecture**: Clear separation between workflow logic and orchestrator internals
- **Testability**: Isolated components are easier to test
- **Flexibility**: Callback-based design allows custom integrations

### Phased Approach
- **Phase 1 (Current)**: Extract TodoTask agent into `pkg/workflow/todotask`
- **Phase 2 (Future)**: Extract full workflow engine with all step types

---

## 2. Phase 1: TodoTask Extraction

### Why TodoTask First?
- **Self-contained**: Has clear inputs/outputs and can run independently
- **High value**: Most complex step type, useful standalone
- **Good test**: Validates the extraction pattern before bigger extractions

### Target Structure

```
pkg/workflow/
└── todotask/
    ├── types.go          # Config, Response, RouteConfig, interfaces
    ├── templates.go      # System + user prompt templates (exported)
    ├── tools.go          # Tool definitions (call_sub_agent, call_generic_agent)
    ├── executors.go      # Tool executors with callbacks
    └── orchestrator.go   # Orchestrator - main execution loop
```

### Source Files

| Current Location | Extract To | Content |
|-----------------|------------|---------|
| `planning_agent.go:866-920` | `types.go` | TodoTaskPlanStep, TodoTaskResponse |
| `todo_task_orchestrator_agent.go` | `templates.go` | System/user prompt templates |
| `cmd/server/virtual-tools/sub_agent_tools.go` | `tools.go`, `executors.go` | Tool definitions and handlers |
| `controller_todo_task.go` | `orchestrator.go` | Execution loop logic |

---

## 3. Phase 2: Full Workflow Engine (Future)

### Target Structure

```
pkg/workflow/                    # Full workflow library
├── workflow.go                  # Engine type, Execute()
├── plan.go                      # Step types
├── executor/
│   ├── regular.go
│   ├── conditional.go
│   ├── decision.go
│   ├── orchestration.go
│   ├── human_input.go
│   └── todo_task.go            # Will import pkg/workflow/todotask
└── todotask/                    # Already extracted in Phase 1
    ├── types.go
    ├── templates.go
    ├── tools.go
    ├── executors.go
    └── orchestrator.go
```

### Components to Extract
- **Engine**: Core workflow execution engine
- **Plan**: Step definitions and parsing
- **Executors**: Step-type-specific execution logic
- **Events**: Event system for observability

---

## 4. API Reference

### Phase 1 Types

#### Config

```go
// Config configures the TodoTask orchestrator
type Config struct {
    // Step context
    StepTitle           string
    StepDescription     string
    StepSuccessCriteria string

    // Paths
    WorkspacePath       string
    StepExecutionPath   string

    // Routes
    PredefinedRoutes    []RouteConfig
    EnableGenericAgent  bool

    // Execution
    MaxIterations       int  // Default: 10
}
```

#### RouteConfig

```go
// RouteConfig defines a sub-agent route
type RouteConfig struct {
    RouteID     string
    RouteName   string
    Description string
}
```

#### Response

```go
// Response represents the orchestration result
type Response struct {
    AllTasksComplete  bool
    CompletionReason  string
    ProgressSummary   string
    Error             error
}
```

#### Callback Types

```go
// SubAgentExecutor is called when delegating to a predefined sub-agent
type SubAgentExecutor func(ctx context.Context, params SubAgentParams) (string, error)

type SubAgentParams struct {
    RouteID           string
    TodoID            string
    Instructions      string
    SuccessCriteria   string
}

// GenericExecutor is called when delegating to the generic agent
type GenericExecutor func(ctx context.Context, params GenericParams) (string, error)

type GenericParams struct {
    TodoID          string
    Instructions    string
    SuccessCriteria string
}
```

#### Interfaces

```go
// WorkspaceClient abstracts file operations
type WorkspaceClient interface {
    ReadFile(ctx context.Context, path string) (string, error)
    WriteFile(ctx context.Context, path string, content string) error
}

// LLMProvider abstracts LLM calls
type LLMProvider interface {
    Chat(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error)
}
```

### Orchestrator

```go
// Orchestrator runs the TodoTask orchestration loop
type Orchestrator struct { ... }

// New creates a TodoTask orchestrator
func New(config Config, opts ...Option) (*Orchestrator, error)

// Execute runs the orchestration loop until completion
func (o *Orchestrator) Execute(ctx context.Context) (*Response, error)
```

### Options

```go
// LLM Configuration (required)
WithLLM(llm LLMProvider)                      // LLM for orchestrator decisions

// Tool Configuration
WithTools(tools []Tool, executors map[string]ToolExecutor)
WithWorkspaceClient(client WorkspaceClient)   // For file operations

// Sub-Agent Callbacks (required for delegation)
WithSubAgentExecutor(fn SubAgentExecutor)     // Executes predefined routes
WithGenericAgentExecutor(fn GenericExecutor)  // Executes generic agent

// Observability
WithEventListener(listener EventListener)
WithLogger(logger Logger)
```

---

## 5. Migration Guide

### How StepBasedWorkflowOrchestrator Will Use pkg/workflow/todotask

The existing `controller_todo_task.go` will be updated to use the extracted package:

```go
// Before: All logic inline in controller_todo_task.go
func (hcpo *StepBasedWorkflowOrchestrator) executeTodoTaskStep(...) {
    // 1000+ lines of orchestration logic
}

// After: Uses pkg/workflow/todotask
import "mcp-agent-builder-go/agent_go/pkg/workflow/todotask"

func (hcpo *StepBasedWorkflowOrchestrator) executeTodoTaskStep(...) {
    config := todotask.Config{
        StepTitle:           step.GetTitle(),
        StepDescription:     step.GetDescription(),
        StepSuccessCriteria: step.GetSuccessCriteria(),
        WorkspacePath:       hcpo.GetWorkspacePath(),
        StepExecutionPath:   stepExecutionPath,
        PredefinedRoutes:    convertRoutes(step.PredefinedRoutes),
        EnableGenericAgent:  step.EnableGenericAgent,
        MaxIterations:       maxIterations,
    }

    orchestrator, err := todotask.New(config,
        todotask.WithLLM(hcpo.getLLMProvider()),
        todotask.WithWorkspaceClient(hcpo.getWorkspaceClient()),
        todotask.WithSubAgentExecutor(hcpo.createSubAgentExecutor()),
        todotask.WithGenericAgentExecutor(hcpo.createGenericExecutor()),
    )
    if err != nil {
        return false, "", err
    }

    result, err := orchestrator.Execute(ctx)
    // ... handle result
}
```

### Backward Compatibility

The extraction is designed to be non-breaking:
1. The internal orchestrator continues to work as before
2. The new package provides the same functionality with a cleaner API
3. Migration can be done incrementally

---

## 6. Examples

### Standalone TodoTask Usage

```go
package main

import (
    "context"
    "log"

    "mcp-agent-builder-go/agent_go/pkg/workflow/todotask"
)

func main() {
    ctx := context.Background()

    // Define sub-agent executor (how to execute predefined routes)
    subAgentExec := func(ctx context.Context, p todotask.SubAgentParams) (string, error) {
        log.Printf("Executing route %s for todo %s", p.RouteID, p.TodoID)
        // Your sub-agent implementation here
        return "Task completed successfully", nil
    }

    // Create TodoTask orchestrator
    config := todotask.Config{
        StepTitle:           "Process Customer Data",
        StepDescription:     "Process all customer records",
        StepSuccessCriteria: "All records processed and saved",
        WorkspacePath:       "/workspace/myproject",
        StepExecutionPath:   "/workspace/myproject/execution/step-1",
        PredefinedRoutes: []todotask.RouteConfig{
            {RouteID: "validator", RouteName: "Data Validator", Description: "Validates data format"},
            {RouteID: "processor", RouteName: "Data Processor", Description: "Processes records"},
        },
        EnableGenericAgent: true,
        MaxIterations:      10,
    }

    orchestrator, err := todotask.New(config,
        todotask.WithLLM(myLLM),
        todotask.WithWorkspaceClient(myWorkspaceClient),
        todotask.WithSubAgentExecutor(subAgentExec),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Execute - orchestrator manages tasks.md and delegates to sub-agents
    result, err := orchestrator.Execute(ctx)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Completed: %s", result.CompletionReason)
}
```

The orchestrator will:
1. Create/manage `tasks.md` in the step execution path
2. Use LLM to decide which tasks to execute and which route to use
3. Call the appropriate callback (sub-agent or generic) for each task
4. Track progress until all tasks complete or success criteria met

---

## 7. Design Decisions

### Package Path
`pkg/workflow/todotask` (not `pkg/todotask`) anticipates the full workflow library structure.

### Callback-Based Architecture
The callback pattern (SubAgentExecutor, GenericExecutor) keeps the package decoupled from orchestrator internals. This allows:
- Custom sub-agent implementations
- Integration with different execution environments
- Testing with mock executors

### Interface Abstractions
- `LLMProvider`: Allows using any LLM (Anthropic, OpenAI, local)
- `WorkspaceClient`: Decouples from specific workspace implementations
- `EventListener`: Optional observability integration

### Template Exports
Prompt templates are exported for customization while providing sensible defaults.

---

## 8. Timeline

| Phase | Component | Estimated Effort |
|-------|-----------|-----------------|
| 1.1 | types.go | Low |
| 1.2 | templates.go | Low |
| 1.3 | tools.go | Low |
| 1.4 | executors.go | Medium |
| 1.5 | orchestrator.go | Medium |
| 1.6 | Example | Low |
| 2.0 | Full Workflow Engine | Future |
