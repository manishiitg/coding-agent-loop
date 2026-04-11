# Todo Task Orchestrator Step Type

## Overview

The `TodoTaskPlanStep` is a new orchestrator step type that combines:
- **Main orchestrator agent** with full tool access (workspace + MCP) - can do work directly
- Predefined sub-agents (with learning and prevalidation)
- A generic execution sub-agent (no learning or prevalidation)
- Todo list management tools for task tracking
- Dynamic task creation and progress tracking

## Context Management

The main orchestrator agent manages the overall context and can:
1. **Do work directly** - Use its tools for quick tasks, verifications, or context gathering
2. **Delegate to sub-agents** - Sub-agents have **smaller context** (only the instructions provided)

This design allows:
- Main agent: Maintains full conversation context, coordinates work, handles complex decisions
- Sub-agents: Focused execution with clean context, efficient for well-defined tasks

## Key Differences from OrchestrationPlanStep

| Feature | OrchestrationPlanStep | TodoTaskPlanStep |
|---------|----------------------|------------------|
| Progress Tracking | Success criteria only | Todo list with tasks |
| Sub-agents | All predefined in routes | Predefined + Generic |
| Learning | All sub-agents have learning | Only predefined sub-agents |
| Routing | LLM selects route | LLM creates tasks + assigns agents |
| Tools | Standard workspace tools | + Todo management tools |

## Plan JSON Format

As of 2026-04-02, `TodoTaskPlanStep` uses a **flat format** — all fields (description, success_criteria, context_dependencies, context_output, validation_schema) are directly on the step, identical to how `RegularPlanStep` works. There is only **one ID** per step.

```json
{
  "type": "todo_task",
  "id": "my-orchestrator",
  "title": "My Orchestrator",
  "description": "Orchestrate the process by delegating to sub-agents in order...",
  "success_criteria": "All routes completed, output files created.",
  "context_dependencies": ["previous_step_output.json"],
  "context_output": "final_status.json",
  "validation_schema": { "files": [{ "file_name": "final_status.json", "must_exist": true }] },
  "has_loop": false,
  "loop_condition": "",
  "predefined_routes": [
    {
      "route_id": "fetch-data",
      "route_name": "Fetch Data",
      "condition": "To fetch data from API",
      "sub_agent_step": {
        "type": "regular",
        "id": "fetch-data",
        "title": "Fetch Data from API",
        "description": "...",
        "success_criteria": "...",
        "context_output": "data.json"
      }
    }
  ],
  "next_step_id": "end"
}
```

### Step Config

Step config uses the **single step ID** (`my-orchestrator` in the example above). There is no inner ID.

```json
{
  "steps": [
    {
      "id": "my-orchestrator",
      "agent_configs": {
        "execution_max_turns": 100,
        "execution_llm": { "provider": "vertex", "model_id": "gemini-2.5-pro" }
      }
    }
  ]
}
```

### Legacy Format (Backwards Compatible)

The old nested `todo_task_step` format is still supported for reading. On unmarshal, fields are automatically promoted from the inner step to the top level. On marshal, the flat format is always written.

```json
// OLD FORMAT (deprecated, still parsed)
{
  "type": "todo_task",
  "id": "my-task",
  "title": "My Task",
  "todo_task_step": {
    "type": "regular",
    "id": "my-orchestrator",
    "description": "...",
    "success_criteria": "..."
  },
  "predefined_routes": [...],
  "next_step_id": "end"
}
```

### Migrating Old Plans

**Automatic (no action needed):** If the backend loads an old-format plan.json, `UnmarshalJSON` automatically promotes `todo_task_step` fields to the top level. The next time the plan is saved (e.g. via workshop edit, plan update tool, or any write path), it will be written in the new flat format. No manual migration is required for plans that are actively used.

**Manual migration** for plans stored externally (backups, other machines, shared repos), use this Python script:

```python
import json, os, sys

def migrate_step(step):
    """Flatten todo_task_step into parent step."""
    if step.get('type') != 'todo_task':
        return step
    inner = step.get('todo_task_step')
    if inner is None:
        return step  # already flat
    for field in ['description', 'success_criteria', 'context_dependencies',
                  'context_output', 'validation_schema', 'has_loop', 'loop_condition']:
        if not step.get(field) and field in inner:
            step[field] = inner[field]
    del step['todo_task_step']
    step.setdefault('has_loop', False)
    step.setdefault('loop_condition', '')
    # Recurse into nested todo_task sub-agents in routes
    for route in step.get('predefined_routes', []):
        if route.get('sub_agent_step'):
            route['sub_agent_step'] = migrate_step(route['sub_agent_step'])
    return step

# Usage: python migrate_plan.py path/to/plan.json
path = sys.argv[1]
with open(path) as f:
    data = json.load(f)
changed = False
for i, step in enumerate(data.get('steps', [])):
    if step.get('type') == 'todo_task' and 'todo_task_step' in step:
        data['steps'][i] = migrate_step(step)
        changed = True
if changed:
    with open(path, 'w') as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
        f.write('\n')
    print(f'Migrated {path}')
else:
    print(f'{path} already in flat format')
```

**Step config cleanup:** After migrating plan.json, also check `step_config.json` for entries referencing the old inner step ID (e.g. `my-orchestrator` from the example above). These configs are now orphaned since the inner step no longer exists. Either remove them or merge their settings into the outer step's config entry.

**Learnings folders:** If learnings were accumulated under the old inner step ID (e.g. `learnings/my-orchestrator/`), they will still be found — the orchestrator resolves learnings by the step ID used at execution time, which is now the outer ID. Old learnings under the inner ID won't be loaded automatically. To preserve them, rename the folder to match the outer step ID:

```bash
# Example: rename old inner-ID learnings folder to match the outer step ID
mv workspace-docs/Workflow/MyWorkflow/learnings/my-orchestrator/ \
   workspace-docs/Workflow/MyWorkflow/learnings/my-task/
```

## Architecture

```
TodoTaskPlanStep (single step, single ID)
├── TodoTaskOrchestratorAgent (main brain - FULL CONTEXT)
│   ├── Tools: workspace + todo management + MCP (full access)
│   ├── Can do work directly (read files, call APIs, etc.)
│   ├── Creates and tracks todo tasks
│   ├── Delegates focused tasks to sub-agents
│   └── Outputs: TodoTaskResponse (next action + progress)
│
├── Predefined Sub-Agents (SMALLER CONTEXT - only instructions)
│   ├── Route 1 → SpecificSubAgentStep
│   │   ├── Full tool access (workspace + MCP)
│   │   ├── Has learning (automatic on completion)
│   │   └── Has prevalidation schema
│   ├── Route 2 → SpecificSubAgentStep
│   └── ...
│
└── Generic Execution (SMALLER CONTEXT - only instructions)
    ├── Uses standard execution_only_agent pattern
    ├── Full tool access (workspace + MCP) - same as predefined
    ├── No learning (DisableLearning=true)
    └── No prevalidation (DisableValidation=true)
```

### Tool Access Matrix

| Agent | Workspace | Todo Tools | MCP | Learning | Prevalidation |
|-------|-----------|------------|-----|----------|---------------|
| TodoTaskOrchestratorAgent | Yes | All | Yes | - | - |
| Predefined Sub-Agent | Yes | update/complete | Yes | Yes (Auto) | Yes |
| Generic Execution (via executeSingleStep) | Yes | update/complete | Yes | No | No |

## Backend Implementation

### Files Created

1. **`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/todo_task_orchestrator_agent.go`**
   - Main orchestrator agent for todo task steps
   - System and user prompt templates with emoji formatting
   - Structured output via `submit_todo_task_result` tool
   - `TodoTaskResponse` struct for agent decisions

2. **`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_todo_task.go`**
   - Execution logic for `executeTodoTaskStep()`
   - Handles todo.json loading/saving
   - Routes to predefined or generic agents (uses standard `executeSingleStep` with `execution_only_agent`)
   - Manages task state transitions
   - Uses factory pattern for proper event bridge connection

3. **`agent_go/cmd/server/virtual-tools/todo_tools.go`**
   - `create_todo` - Create a new task
   - `update_todo` - Update task status/notes
   - `complete_todo` - Mark task as done with result
   - `list_todos` - View all tasks and status
   - `get_todo` - Get details of a specific task

### Files Modified

1. **`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go`**
   - `TodoTaskPlanStep` struct embeds `CommonStepFields` directly (same pattern as `RegularPlanStep`)
   - `MarshalJSON` writes flat format, `UnmarshalJSON` reads both flat and legacy nested format
   - Tool schemas (`getAddTodoTaskStepSchema`, `getUpdateTodoTaskStepSchema`) use flat fields
   - Partial update merge handles both new flat fields and legacy `todo_task_step` map

2. **`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_management.go`**
   - `populateRuntimeFields()` matches config by the single step ID, populates `AgentConfigs` and `ValidationSchema` directly
   - Route sub-agent steps are populated recursively

3. **`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/step_config.go`**
   - Added TodoTaskPlanStep and HumanInputPlanStep to type switch

4. **`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_execution.go`**
   - Added routing for TodoTaskPlanStep in main execution loop
   - Added HumanInputPlanStep to `getAgentConfigs()`

5. **`agent_go/pkg/orchestrator/base_orchestrator_tools.go`**
   - Registered todo_tools category

6. **`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go`**
   - Added `createTodoTaskOrchestratorAgent()` factory method
   - Uses standard factory pattern for proper event bridge connection
   - Mirrors `createOrchestrationOrchestratorAgent()` pattern

## Frontend Implementation

### Files Created

1. **`frontend/src/components/workflow/nodes/TodoTaskNode.tsx`**
   - React component for rendering todo_task steps in workflow canvas
   - Purple color scheme with ListTodo icon
   - Shows predefined routes count and generic agent indicator
   - Layout-aware handle positioning (LR/TB modes)
   - Status handling: pending, running, executing, evaluating, orchestrating, completed, failed

### Files Modified

1. **`frontend/src/utils/stepConfigMatching.ts`**
   - Added `TodoTaskPlanStep` interface
   - Added `isTodoTaskStep()` type guard
   - Updated `PlanStep` union type

2. **`frontend/src/components/workflow/hooks/usePlanToFlow.ts`**
   - Added `TodoTaskNodeData` interface with `evaluating` status
   - Added NODE_DIMENSIONS for todo_task
   - Added node conversion logic in `stepToNode()`
   - Added edge routing for todo_task (similar to orchestration)
   - Creates sub-agent nodes for predefined routes

3. **`frontend/src/components/workflow/nodes/index.ts`**
   - Registered TodoTaskNode in nodeTypes

4. **`frontend/src/components/workflow/nodes/RoutingNode.tsx`**
   - Added layout-aware handle positioning
   - Output handles on Right (LR) or Bottom (TB)
   - Added `running` and `failed` status support

5. **`frontend/src/components/workflow/nodes/StepNode.tsx`**
   - Added layout-aware handle positioning for sub-agents
   - Sub-agent "top" handle positioned on Left (LR) or Top (TB)
   - Sub-agents show Bot icon instead of step number

6. **`frontend/src/stores/useLLMStore.ts`**
   - Fixed `testAPIKey` return type to include `correctedOptions`

7. **`frontend/src/components/AzureSection.tsx`**
   - Fixed type casting for `correctedOptions.endpoint`

## Todo File Structure

**Location**: `{step_execution_path}/todos.json`

```json
{
  "step_id": "step-3",
  "objective": "Process all customer data",
  "todos": [
    {
      "id": "todo_1",
      "title": "Fetch customer list from API",
      "description": "Call /api/customers endpoint",
      "priority": "high",
      "status": "completed",
      "assigned_agent": "api_fetcher",
      "result": "Fetched 150 customers to customers.json",
      "created_at": "2024-01-25T10:00:00Z",
      "updated_at": "2024-01-25T10:05:00Z"
    },
    {
      "id": "todo_2",
      "title": "Transform customer data",
      "description": "Convert to required format",
      "priority": "medium",
      "status": "in_progress",
      "assigned_agent": "generic",
      "created_at": "2024-01-25T10:05:00Z",
      "updated_at": "2024-01-25T10:05:00Z"
    }
  ],
  "summary": {
    "total": 5,
    "completed": 1,
    "in_progress": 1,
    "open": 3
  }
}
```

## Execution Flow

```
┌─────────────────────────────────────────────────────────────┐
│                    executeTodoTaskStep                      │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
              ┌─────────────────────────────┐
              │  Load/Init todos.json       │
              └─────────────────────────────┘
                            │
                            ▼
         ┌──────────────────────────────────────┐
         │  TodoTaskOrchestratorAgent           │
         │  - Reviews current todos             │
         │  - Creates/updates tasks             │
         │  - Selects agent for next task       │
         │  - Returns TodoTaskResponse          │
         └──────────────────────────────────────┘
                            │
         ┌──────────────────┼──────────────────┐
         │                  │                  │
    all_complete?    delegate_predefined?   delegate_generic?
         │                  │                  │
         ▼                  ▼                  ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ Run Validation  │ │ Execute via     │ │ Execute via     │
│ + Learning      │ │ executeSingleStep│ │ GenericAgent   │
│ → Return        │ │ (has learning)  │ │ (NO learning)  │
└─────────────────┘ └─────────────────┘ └─────────────────┘
                            │                  │
                            └────────┬─────────┘
                                     │
                                     ▼
                         ┌─────────────────────┐
                         │ Update todos.json   │
                         │ → Loop back         │
                         └─────────────────────┘
```

## LLM Configuration

The TodoTaskOrchestratorAgent uses the **execution LLM** (same as the regular OrchestrationPlanStep), not the validation LLM.

### LLM Selection Implementation

All orchestrator and control agents (TodoTaskOrchestrator, OrchestrationOrchestrator, ConditionalAgent) use the shared `selectExecutionLLM` helper function. This keeps execution model selection consistent with the main workflow runtime.

For conditional agents, there's an additional priority check for `ConditionalLLM` before falling back to `selectExecutionLLM`.

### LLM Selection Priority

**Standard agents (TodoTaskOrchestrator, OrchestrationOrchestrator, GenericAgent):**
1. **Step-specific execution LLM** (`agent_configs.execution_llm`)
2. **Preset default execution LLM** (`presetExecutionLLM`)
3. **Orchestrator default LLM** (fallback)

**Conditional agents:**
1. **Step-specific conditional LLM** (`agent_configs.conditional_llm`)
2. **Step-specific execution LLM** (`agent_configs.execution_llm`)
3. **Preset default execution LLM** (`presetExecutionLLM`)
4. **Orchestrator default LLM** (fallback)

This is consistent with how the regular orchestration orchestrator selects its LLM.

### Agent LLM Matrix

| Agent | LLM Type | Priority |
|-------|----------|----------|
| TodoTaskOrchestratorAgent | Execution LLM | step > preset > default |
| Predefined Sub-Agent | Execution LLM | step > preset > default |
| Generic Execution Agent | Execution LLM | step > preset > default |

## TodoTaskResponse Schema

```json
{
  "next_action": "delegate|complete|continue",
  "selected_route_id": "route_id (for predefined)",
  "use_generic_agent": true|false,
  "todo_id_to_execute": "todo_1",
  "instructions_to_sub_agent": "Detailed instructions...",
  "success_criteria_for_sub_agent": "Measurable criteria...",
  "all_tasks_complete": true|false,
  "progress_summary": "2 of 5 tasks completed",
  "completion_reason": "All tasks done and verified"
}
```

## Layout Support

The frontend nodes support both horizontal (LR) and vertical (TB) layout modes:

### Horizontal Mode (LR)
- Input handles on the Left side
- Output handles (routes) on the Right side
- Sub-agents positioned to the right of parent

### Vertical Mode (TB)
- Input handles on the Top
- Output handles (routes) on the Bottom
- Sub-agents positioned below parent

Nodes dynamically adjust handle positions by reading `layoutDirection` from the workflow store.

## Events

The todo task step emits the following events for tracking and UI updates:

### TodoTaskRouteSelectedEvent
Emitted when the orchestrator makes a routing decision:
- `step_index`, `step_path`, `step_id`, `step_title`: Step identification
- `iteration`: Current iteration number (1-based)
- `next_action`: "delegate", "complete", or "continue"
- `selected_route_id`, `selected_route_name`: Selected predefined route (if any)
- `use_generic_agent`: True if generic agent was selected
- `todo_id_to_execute`, `todo_title`: The todo item being worked on
- `instructions_to_sub_agent`: Instructions given to the sub-agent
- `selection_reasoning`: Why this route was selected
- `all_tasks_complete`: Whether all tasks are complete
- `progress_summary`: Summary of overall progress

### TodoTaskStepCompletedEvent
Emitted when the entire todo task step completes:
- `step_index`, `step_path`, `step_id`, `step_title`: Step identification
- `total_iterations`: How many iterations the step took
- `total_todos_count`: Total number of todo items
- `completed_count`: Number of completed todo items
- `completion_reason`: Why the step completed
- `next_step_id`: The next step to execute

### Todo Item Events
The controller emits events when todo items change (detected by comparing before/after state):
- `TodoTaskItemCreated`: When a new todo item is created
  - `step_index`, `step_path`, `step_id`: Step identification
  - `todo_id`, `title`, `description`, `priority`: Todo item details
  - `created_by`: Always "orchestrator"
- `TodoTaskItemUpdated`: When a todo item status changes (but not to completed)
  - `todo_id`, `title`: Todo identification
  - `old_status`, `new_status`: Status transition
  - `notes`: Any notes added
  - `updated_by`: Always "orchestrator"
- `TodoTaskItemCompleted`: When a todo item is marked as completed
  - `todo_id`, `title`: Todo identification
  - `result`: Completion result summary
  - `completed_by`: Always "orchestrator"

### Execution Logs UI

The `ExecutionLogsPopup` component displays todo task logs in a similar format to orchestration logs:

**Todo Task Logs Section** (`stepLogs.todo_task`):
- Groups logs by iteration number
- Timeline visualization with purple accent color
- Displays for each routing decision:
  - Next action (delegate/complete/continue)
  - Selected agent (predefined route or generic agent)
  - Todo item being executed
  - Selection reasoning
  - Instructions and success criteria for sub-agent
  - Progress summary
- "View Raw JSON" collapsible for debugging

**Archived Todo Task Logs**:
- Displayed in the archived logs section
- Shows route selections and completion status

**Type Definitions** (`api-types.ts`):
- `TodoTaskLog` interface for routing log entries
- Added `todo_task?: TodoTaskLog[]` to `StepExecutionLogs`
- Added `todo_task?: TodoTaskLog[]` to `ArchivedLogEntry`

## Visual Indicators

### TodoTaskNode
- Purple badge with ListTodo icon
- Shows predefined routes count
- Shows "Generic agent enabled" indicator
- Context inputs/outputs from step fields directly

### Sub-Agent Nodes (in StepNode)
- Bot icon instead of step number
- Dashed cyan border
- Cannot run independently (controlled by parent)
- Receives connections from parent orchestrator/todo_task
