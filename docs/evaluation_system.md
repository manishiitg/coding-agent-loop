# Evaluation System

This document describes the comprehensive evaluation system for the Workflow Orchestrator, including the UI mode toggle, evaluation phases (Create, Execute, Score), and architectural implementation.

---

## 1. Overview

The Evaluation system allows users to create, execute, and analyze evaluation plans to verify the quality and correctness of workflow execution results. It provides a dedicated "Eval Mode" in the UI and distinct workflow phases for designing and running evaluations.

**Key Components:**
- **UI Mode Toggle**: Switch between "Plan+Exec Mode" (main workflow) and "Eval Mode" (evaluation workflow).
- **Evaluation Phases**:
    1.  **Evaluation Designer**: Creates the evaluation plan based on the execution plan.
    2.  **Evaluation Execution**: Runs the evaluation steps against execution results.
    3.  **Evaluation Scoring**: Calculates scores and generates reports.
- **Separation of Concerns**: Evaluation data (`evaluation/`) is kept distinct from execution data (`planning/`, `runs/`).

---

## 2. UI: Workflow Mode Toggle

A segmented control in the `WorkflowToolbar` switches between two distinct canvas modes:

1.  **Plan+Exec Mode** (default): Shows main workflow steps from `planning/plan.json`. Focuses on Planning and Execution phases.
2.  **Eval Mode**: Shows evaluation steps from `evaluation/evaluation_plan.json`. Focuses on Eval Designer, Eval Execution, and Evaluation Debugger phases.

When toggling modes, the canvas content completely replaces (not side-by-side). Each mode has its own:
-   Plan data
-   Step configurations
-   Layout persistence
-   Available phases

### Implementation Details (Frontend)

-   **State**: `workflowMode` ('plan' | 'eval') in `useWorkflowStore`.
-   **Canvas**: `WorkflowCanvas` conditionally renders nodes based on mode.
-   **Toolbar**: Filters available phases based on mode.
-   **Hooks**: `useEvaluationPlanData` handles loading/saving evaluation-specific files.
-   **Persistence**: Layouts saved to `planning/workflow_layout.json` (Plan) and `evaluation/eval_layout.json` (Eval).

---

## 3. Evaluation Phases & Architecture

### Phase 1: Create Evaluation Plan (Evaluation Designer)

**Goal**: Create a structured evaluation plan (`evaluation/evaluation_plan.json`) that defines how to verify the workflow's success.

-   **Input**: `planning/plan.json` (Main Execution Plan).
-   **Process**:
    1.  **Analyze**: The agent infers goals and success metrics directly from the execution plan (ignoring the original objective).
    2.  **Confirm**: Mandatory `human_feedback` loop to confirm evaluation strategy.
    3.  **Generate**: Creates evaluation steps. Defaults to a single, comprehensive step unless complexity demands more.
-   **Output**: `evaluation/evaluation_plan.json`.
-   **Manager**: `EvaluationManager` (Independent Manager pattern).
-   **Agent**: `HumanControlledEvaluationAgent`.

### Phase 2: Execute Evaluation (Evaluation Execution)

**Goal**: Execute the evaluation steps against the results of a specific workflow run.

-   **Input**: `evaluation/evaluation_plan.json` and target run folder (e.g., `runs/iteration-15`).
-   **Process**:
    -   Uses the standard `StepBasedWorkflowOrchestrator.Execute()` method but wrapped by `ExecuteEvaluationOnly()`.
    -   Sets `isEvaluationMode = true`.
    -   Redirects paths to evaluation-specific folders.
-   **Cross-Folder Access**:
    -   Injects `TARGET_RUN_PATH` variable pointing to the target run folder.
    -   Dynamically updates **Folder Guard** to allow reading from the target run folder.
    -   Agent prompt instructs to use `{{TARGET_RUN_PATH}}` to access execution artifacts.
-   **Output**: Verification reports in `evaluation/runs/{targetRunFolder}/execution/`.

### Phase 3: Scoring Phase (Evaluation Scoring)

**Goal**: Calculate a final score (0-10) and generate a comprehensive report.

-   **Trigger**: Runs automatically after all evaluation steps complete.
-   **Agent**: `WorkflowEvaluationScoringAgent`.
-   **Process**:
    -   Analyzes outputs from Phase 2.
    -   Compares against success criteria.
    -   Calculates score.
-   **Output**: `evaluation/runs/{targetRunFolder}/evaluation_report.json`.

---

## 4. File Structure & Storage

All evaluation data is isolated in the `evaluation/` directory within the workspace.

```
Workflow/{workflow_name}/
├── evaluation/                           # ALL evaluation data goes here
│   ├── evaluation_plan.json              # Evaluation plan (created by Designer)
│   ├── step_config.json                  # Agent configs for evaluation steps
│   ├── eval_layout.json                  # Canvas layout for Eval Mode
│   ├── learnings/                        # Evaluation-specific learnings
│   │   └── {stepID}/
│   │       ├── {Title}_learning.md
│   │       └── .learning_metadata.json
│   └── runs/                             # Evaluation execution results
│       └── {targetRunFolder}/            # Matches target run (e.g., "iteration-1")
│           ├── execution/                # Step outputs (verification reports)
│           │   └── step-{N}/
│           ├── logs/                     # Detailed execution logs
│           ├── steps_done.json           # Progress tracking
│           ├── token_usage.json          # Token usage for evaluation
│           └── evaluation_report.json    # Final score and report
├── planning/
│   ├── plan.json                         # Main execution plan
│   └── step_config.json                  # Main step configs
├── runs/                                 # Main execution results (Subject of evaluation)
│   └── {iteration}/
└── ...
```

**Key Storage Rules:**
-   **Evaluation Plan**: `evaluation/evaluation_plan.json`
-   **Evaluation Config**: `evaluation/step_config.json`
-   **Evaluation Learnings**: `evaluation/learnings/` (distinct from `learnings/`)
-   **Evaluation Results**: `evaluation/runs/{targetRunFolder}/`

---

## 5. Evaluation Mode Implementation Details

### `isEvaluationMode` Flag

A dedicated flag `isEvaluationMode` propagates through the orchestrator and execution context to control behavior:

1.  **Routing**: Determines whether to use `evaluation/learnings` or `learnings/`.
2.  **Config**: Selects `evaluation/step_config.json` vs `planning/step_config.json`.
3.  **Automation**: Auto-sets `SkipHumanInput=true` and `DisableValidation=true` for evaluation steps (evaluators verify others, they don't usually need verification themselves).

### Folder Guard Updates

The Folder Guard system checks `isEvaluationMode` to:
-   Allow write access to `evaluation/` folders.
-   Allow read access to `runs/` (target run data) when in evaluation mode.

### Tool Improvements for Evaluation

-   **`delete_evaluation_step`**: Handles Python-style list parsing (`['id1', 'id2']`) and cleans up associated learnings.
-   **`add_evaluation_step`**: Returns informative state showing all steps after addition.

---

## 6. Debugging & Improvement Phases

The system provides separate debugging phases for each mode:

*   **Plan Debugger (Plan+Exec Mode)**:
    *   Phase ID: `plan-improvement`
    *   Target: `planning/plan.json`
    *   Goal: Fix logic errors in the main workflow.
*   **Evaluation Debugger (Eval Mode)**:
    *   Phase ID: `evaluation-debugger`
    *   Target: `evaluation/evaluation_plan.json`
    *   Goal: Fix inaccurate evaluation criteria or evidence checking.

---

## 7. Configuration

### Backend Integration

-   **Phases Registered**: `evaluation-designer`, `evaluation-execution`, `evaluation-debugger`.
-   **Managers**:
    -   `EvaluationManager`: Handles designer phase.
    -   `EvaluationDebuggerManager`: Handles debugger phase.
-   **Execution**: `ExecuteEvaluationOnly()` wrapper in `StepBasedWorkflowOrchestrator`.

### Frontend Integration

-   **Hooks**: `useEvaluationPlanData` (loads/saves eval data), `useEvaluationPlanToFlow` (visualization).
-   **Components**: `WorkflowCanvas` renders based on mode. `StepSidebar` edits eval-specific configs.

---

## 8. Verification Checklist

- [x] **Mode Toggle**: Can switch between Plan and Eval modes in UI.
- [x] **Plan Loading**: Eval mode loads `evaluation/evaluation_plan.json`.
- [x] **Execution**: "Eval Execution" runs evaluation steps against target run.
- [x] **Scoring**: "Evaluation Report" is generated with score.
- [x] **Isolation**: Evaluation files are strictly kept in `evaluation/`.
- [x] **Cross-Access**: Evaluation agents can read files from `runs/` via `TARGET_RUN_PATH`.
