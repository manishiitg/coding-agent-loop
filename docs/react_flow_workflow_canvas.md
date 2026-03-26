# React Flow Workflow Canvas

The Workflow Canvas is a React Flow-based visual editor that serves as the command center for building and monitoring complex agentic workflows.

---

## ✅ Core Implementation

The canvas manages a directed acyclic graph (DAG) of **Steps**, each representing a discrete task or decision point.

### 1. Node Types (Current: 13)

The system uses a variety of specialized nodes, each with its own UI and configuration:

| Node Type | Purpose | Implementation |
| :--- | :--- | :--- |
| `start` | The entry point of the workflow. | `StartNode` |
| `end` | Termination points for success or failure. | `EndNode` |
| `step` | A standard agent task (Plan + Execution). | `StepNode` |
| `conditional` | Binary branching (True/False) based on a condition. | `ConditionalNode` |
| `decision` | Multi-path branching based on LLM reasoning. | `DecisionNode` |
| `routing` | Topology-aware step that handles complex logic flow. | `RoutingStepNode` |
| `orchestrator` | Legacy node, now aliased to `RoutingNode`. | `RoutingNode` |
| `todo_task` | A simplified task node with status tracking. | `TodoTaskNode` |
| `human_input` | Pauses execution for user feedback/input. | `HumanInputNode` |
| `loop` | Repeats a sequence of nodes until a condition is met. | `LoopNode` |
| `variables` | Configures global variables and constants. | `VariablesNode` |
| `evaluation` | Validates the output of a phase or step. | `EvaluationNode` |
| `execution-settings` | Configures LLM params and tool constraints. | `ExecutionSettingsNode` |

### 2. Workflow Versioning System

A critical recent addition is the **Version Publishing** system, which allows users to snapshot the entire workflow configuration.

- **Storage**: Versions are snapshotted in `versions/v{N}/` within the workspace.
- **Snapshot scope**: Includes planning/config files plus workflow learnings from `learnings/` and `evaluation/learnings/`.
- **Components**: `WorkflowVersionsPopup.tsx` provides the UI for listing, publishing, and reverting.
- **Endpoints**:
  - `GET /api/workflow/versions`: List all published versions.
  - `POST /api/workflow/versions/publish`: Create a new snapshot with a required label.
  - `POST /api/workflow/versions/revert`: Restore config files from a previous version.
  - `DELETE /api/workflow/versions`: Remove a version.

### 3. Execution Control (The 3-Dropdown System)

The toolbar features a powerful three-tier selection system for execution:

1.  **Iteration Selector**: Select a "Run Folder" (e.g., `run_20260308_1430`).
2.  **Phase Selector**: Select between `Planning`, `Workflow`, or `Evaluation`.
3.  **Step Selector**: Focus execution on a specific step or start from a point.

### 4. Progress Monitoring

- **Auto-Highlighting**: The canvas polls for execution progress and highlights the "currently active" node with a pulse effect.
- **Execution Logs**: Clicking a node opens the `ExecutionLogsPopup`, showing real-time tool calls and LLM outputs for that specific step.
- **Batch Progress**: The `BatchProgressHeader` provides a high-level summary of the entire run's status.

## UI Components & Layout

- **`WorkflowFlow.tsx`**: The main React Flow container.
- **`WorkflowLayout.tsx`**: Handles automatic node placement using Elk.js for topology-aware layouts.
- **`NodeConfigFooter.tsx`**: Standardized footer for all nodes containing action buttons (Delete, Edit, Expand).
- **`MultiStepSidebar.tsx`**: A unified sidebar for configuring complex step properties and agent behaviors.

## Advanced Features

- **Topology-Aware Layout**: Nodes automatically rearrange themselves when new steps are added to maintain readability.
- **Grouped Movement**: Sub-processes or loops can be moved as single units.
- **Validation Colors**: Nodes turn Green (Success), Red (Failure), or Yellow (Running) based on the latest execution logs.
