# Evaluation Phase Implementation Plan

## Overview

The Evaluation system consists of **three separate workflow phases**:

1. **Create Evaluation Plan**: User-driven phase to create evaluation guides/plans (similar to Planning phase)
2. **Execute Evaluation**: Execute evaluation plan steps against workflow execution results
3. **Scoring Phase** (Planned): Calculate scores based on execution outputs and success criteria

**Key Characteristics:**
- **Phase 1 (Create)**: User-driven conversation to create evaluation guide, analyzing the **execution plan** (plan.json).
- **Phase 2 (Execute)**: Execute evaluation steps to check execution results.
- **Phase 3 (Score)**: Analyze execution outputs against success criteria and generate evaluation report.
- **Evaluation Plan**: Creates `evaluation/evaluation_plan.json` (similar structure to `plan.json`).
- **Goal-Oriented**: Focuses on verifying if the overall objective was met with high quality.
- **Single Step Preference**: Defaults to a single, comprehensive evaluation step unless complexity demands more.

---

## Goals

### Phase 1: Create Evaluation Plan
1. **Analyze Execution Plan**: Infer goals and success metrics directly from `planning/plan.json` (Objective is ignored).
2. **Confirm Strategy**: Mandatory `human_feedback` loop to confirm evaluation strategy before creating steps.
3. **Generate Evaluation Plan**: Create structured evaluation plan, defaulting to a single comprehensive step.

### Phase 2: Execute Evaluation
4. **Execute Evaluation Steps**: Run evaluation steps against workflow execution results.
5. **Store Results**: Save evaluation results in `evaluation/runs/{targetRunFolder}/execution/`.
6. **Store Learnings**: Save learnings in `evaluation/learnings/{stepID}/` (separate from workflow learnings).

### Phase 3: Scoring Phase (Planned)
7. **Calculate Scores**: Analyze execution outputs against success criteria (Score 0-10).
8. **Generate Report**: Create `evaluation/runs/{targetRunFolder}/evaluation_report.json`.

---

## Architecture Overview

### Phase 1: Create Evaluation Plan
Follows the **Independent Manager** pattern:
- **`EvaluationManager`**: Fully independent manager (like `PlanImprovementManager`) handling agent creation and execution.
- **`HumanControlledEvaluationAgent`**: Conversational agent with self-contained prompt logic.
- **Phase registration**: `"evaluation-designer"` in `workflow_orchestrator.go`.

### Phase 2: Execute Evaluation
**Reuses existing `Execute()` method** via wrapper:
- Uses same execution infrastructure (`StepBasedWorkflowOrchestrator.Execute()`).
- **Input**: `evaluation/evaluation_plan.json` instead of `planning/plan.json`.
- **Output**: `evaluation/runs/{targetRunFolder}/execution/` folder.
- **Learnings**: `evaluation/learnings/{stepID}/` folder (separate from workflow learnings).
- **Token Usage**: `evaluation/runs/{targetRunFolder}/token_usage.json`.
- **Phase registration**: `"evaluation-execution"` in `workflow_orchestrator.go`.

### Phase 3: Scoring Phase
- **`WorkflowEvaluationScoringAgent`**: Agent that analyzes outputs and calculates scores.
- **Factory Method**: `createEvaluationScoringAgent()` in `controller_agent_factory.go`.
- **LLM Selection**: Uses `presetPhaseLLM` (like other phase agents).
- **Event Bridging**: Uses `CreateAndSetupStandardAgentWithConfig` for proper event emission.
- **Input**: Execution outputs from `evaluation/runs/{targetRunFolder}/execution/step-{N}/`.
- **Output**: `evaluation/runs/{targetRunFolder}/evaluation_report.json`.

---

## File Structure

### Created Files

**Phase 1: Create Evaluation Plan**
1. **`evaluation_types.go`**: Data structures
   - `EvaluationPlan` struct
   - `EvaluationStep` struct (implements `PlanStepInterface`)

2. **`evaluation_manager.go`**: Independent Manager
   - `CreateEvaluationGuideOnly()` method
   - Handles file operations, agent creation, and execution loop.
   - Tools: `add_evaluation_step`, `update_evaluation_step`, `delete_evaluation_step`, `human_feedback`.
   - Delete tool supports Python-style list parsing (e.g., `['id1', 'id2']`).

3. **`evaluation_agent.go`**: Conversational Agent
   - `HumanControlledEvaluationAgent`
   - Contains `evaluationSystemPromptProcessor` for self-contained prompt generation.
   - Clear guidance that `add_evaluation_step` **APPENDS** to existing plan.

**Phase 2: Execute Evaluation**
4. **`evaluation_execution.go`**: Execution Wrapper
   - `ExecuteEvaluationOnly()` method on `StepBasedWorkflowOrchestrator`.
   - Sets `isEvaluationMode = true` for separate learnings path.
   - Configures `selectedRunFolder` to `../evaluation/runs/{targetRunFolder}`.
   - Calls `SetIterationFolder()` for token usage persistence.

**Phase 3: Scoring Phase (Planned)**
5. **`evaluation_scoring_agent.go`**: Scoring Agent
   - `WorkflowEvaluationScoringAgent`
   - Analyzes execution outputs against success criteria.
   - Calls `submit_score` tool with evaluation results.

### Workspace Structure

```
Workflow/{workflow_name}/
├── evaluation/                           # ALL evaluation data goes here
│   ├── evaluation_plan.json              # Evaluation plan (created by EvaluationManager)
│   ├── learnings/                        # Evaluation-specific learnings (separate from workflow)
│   │   └── {stepID}/                     # Step-specific learnings folder
│   │       ├── {Title}_learning.md       # Learning content
│   │       └── .learning_metadata.json   # Learning metadata (success counts, etc.)
│   └── runs/                             # Evaluation execution results
│       └── {targetRunFolder}/            # e.g., "iteration-1" or "iteration-1/group-1"
│           ├── execution/                # Step execution outputs (step-level)
│           │   └── step-{N}/             # Output for each evaluation step
│           │       ├── verification_report.json
│           │       └── output.json
│           ├── logs/                     # Detailed execution logs
│           │   └── step-{N}/             # Per-step logs
│           │       ├── execution/        # Execution agent outputs
│           │       │   └── execution-attempt-{N}-iteration-{N}.json
│           │       └── validation/       # Validation agent outputs (if enabled)
│           │           └── validation.json
│           ├── steps_done.json           # Progress tracking (completed step indices)
│           ├── token_usage.json          # Token usage for this evaluation run
│           └── evaluation_report.json    # Final scoring results (after all steps)
├── planning/
│   └── plan.json                         # Execution plan (Input for evaluation design)
├── learnings/                            # Workflow learnings (NOT evaluation)
│   └── {stepID}/
│       ├── {Title}_learning.md
│       └── .learning_metadata.json
├── runs/                                 # Workflow execution results (Subject of evaluation)
│   └── {iteration}/
│       ├── execution/
│       │   └── step-{N}/
│       ├── steps_done.json
│       └── token_usage.json
└── knowledgebase/                        # Persistent files across runs
```

### File Storage Summary

| Component | Location | Description |
|-----------|----------|-------------|
| Evaluation Plan | `evaluation/evaluation_plan.json` | Step definitions for evaluation |
| Evaluation Learnings | `evaluation/learnings/{stepID}/` | Learnings from evaluation runs |
| Evaluation Execution | `evaluation/runs/{target}/execution/step-{N}/` | Step outputs (verification files) |
| Evaluation Logs | `evaluation/runs/{target}/logs/step-{N}/execution/` | Execution result files |
| Evaluation Progress | `evaluation/runs/{target}/steps_done.json` | Completed step indices |
| Evaluation Tokens | `evaluation/runs/{target}/token_usage.json` | LLM token usage |
| Evaluation Report | `evaluation/runs/{target}/evaluation_report.json` | Final scores |
| Workflow Learnings | `learnings/{stepID}/` | Learnings from workflow execution |
| Workflow Execution | `runs/{iteration}/execution/step-{N}/` | Workflow step outputs |
| Workflow Logs | `runs/{iteration}/logs/step-{N}/execution/` | Execution result files |

---

## Evaluation Mode Implementation

### isEvaluationMode Flag

A dedicated flag propagates through the system to route evaluation data to separate folders:

1. **Orchestrator Level**: `hcpo.isEvaluationMode` field on `StepBasedWorkflowOrchestrator`.
2. **Execution Context**: `IsEvaluationMode` field in `ExecutionContext` struct.
3. **Set in**: `ExecuteEvaluationOnly()` sets `hcpo.isEvaluationMode = true`.

### Folder Guard Updates

The following functions check `isEvaluationMode` to determine learnings path:

- **`setupExecutionFolderGuard`**: Routes to `evaluation/learnings/{stepID}` or `learnings/{stepID}`.
- **`setupConditionalFolderGuard`**: Same routing logic.
- **`setupLearningFolderGuard`**: Same routing logic for learning agents.
- **`getLearningFolderPathByStepID`**: Helper function with `isEvaluationMode` parameter.

### Token Usage Persistence

- `ExecuteEvaluationOnly()` calls `SetIterationFolder(selectedRunFolder)`.
- This ensures `token_usage.json` is written to `evaluation/runs/{targetRunFolder}/token_usage.json`.

### Path Handling Rules

**IMPORTANT**: `ReadWorkspaceFile` and `WriteWorkspaceFile` auto-prepend `workspacePath` for relative paths.

| Pattern | Example | Result |
|---------|---------|--------|
| ✅ Relative path | `runs/iter-1/steps_done.json` | `{workspacePath}/runs/iter-1/steps_done.json` |
| ❌ Path with workspace | `{workspace}/runs/iter-1/steps_done.json` | `{workspacePath}/{workspacePath}/runs/...` (DUPLICATE!) |

- **DO**: Pass relative paths to `ReadWorkspaceFile`/`WriteWorkspaceFile`
- **DON'T**: Include `GetWorkspacePath()` in paths passed to these functions
- Absolute paths (starting with `/`) are passed through unchanged

---

## Cross-Folder Access (TARGET_RUN_PATH)

To allow evaluation steps (running in `evaluation/runs/...`) to access the original workflow artifacts (in `runs/...`), a special mechanism is used:

1.  **Variable Injection**: `ExecuteEvaluationOnly` calculates the absolute path to the target execution folder and injects it as `TARGET_RUN_PATH` into the agent's variable context.
2.  **Prompt Instruction**: The Evaluation Designer is explicitly instructed to use `{{TARGET_RUN_PATH}}` when referencing files to check (e.g., `Read file {{TARGET_RUN_PATH}}/output.json`).
3.  **Folder Guard Update**: The `setupExecutionFolderGuard` function dynamically adds `TARGET_RUN_PATH` to the allowed read paths if the variable is present, bypassing standard isolation rules for this specific path.

---

## Tool Improvements

### Delete Evaluation Step Tool

The `delete_evaluation_step` tool has been enhanced:

1. **Informative Responses**: Returns detailed message showing:
   - Which steps were actually deleted
   - Which IDs were not found (warning)
   - Current state of the plan after deletion

2. **Python-Style List Parsing**: Handles LLM outputs like `['id1', 'id2']` by:
   - First trying JSON array parsing
   - Falling back to converting single quotes to double quotes
   - Final fallback: treating as single ID string

3. **Learnings Cleanup**: Automatically deletes `evaluation/learnings/{stepID}` when a step is deleted.

### Add Evaluation Step Tool

- Returns informative message showing all step IDs in the plan after adding.
- Prompt clarifies that this tool **APPENDS** (does not replace existing steps).

---

## Key Design Decisions

1.  **Infer Goals from Plan**: The global objective is removed from the context. The agent infers the goal solely from the execution plan.
2. **Single Step Default**: The prompt explicitly instructs to favor a single, holistic evaluation step over many granular checks.
3. **PreValidation Removed**: The requirement for a `PreValidation` schema was removed from the prompt to simplify the initial implementation and focus on high-level quality checks.
4. **Independent Manager**: `EvaluationManager` was refactored to be an independent struct, decoupling it from the main orchestrator's state where possible.
5. **Separate Learnings**: Evaluation learnings are stored in `evaluation/learnings/` to keep them distinct from workflow learnings in `learnings/`.
6. **Explicit Evaluation Mode**: Using `isEvaluationMode` flag rather than inferring from folder paths ensures consistent behavior across all components.

---

## Implementation Status

### Core Infrastructure
- [x] **Types, Manager, Agent created**: `EvaluationPlan`, `EvaluationStep`, `EvaluationManager`, `HumanControlledEvaluationAgent`.
- [x] **Tool Implementation**: `add_evaluation_step`, `update_evaluation_step`, `delete_evaluation_step`, `human_feedback`.
- [x] **Prompt Engineering**: Goal inference, single-step preference, mandatory confirmation.
- [x] **Execution Wrapper**: `ExecuteEvaluationOnly()` implemented and integrated.
- [x] **Integration**: Phases registered in `workflow_orchestrator.go`.

### Evaluation Mode Routing
- [x] **Evaluation Mode Flag**: `isEvaluationMode` propagated through orchestrator and `ExecutionContext`.
- [x] **Separate Learnings Path**: `evaluation/learnings/{stepID}/` for evaluation learnings.
- [x] **Learning Metadata Paths**: All paths use `getLearningsBasePath()` helper.
- [x] **Token Usage Path**: Routed to `evaluation/runs/{targetRunFolder}/token_usage.json`.
- [x] **Folder Guard Updates**: All folder guard functions check `isEvaluationMode`.
- [x] **Relative Path Fix**: All paths passed to `ReadWorkspaceFile`/`WriteWorkspaceFile` are relative (no workspace prefix).
- [x] **Progress Path Fix**: `getStepsProgressPath()` returns relative path to avoid double-prepending workspace.

### Scoring Phase
- [x] **Scoring Agent**: `WorkflowEvaluationScoringAgent` with `submit_score` tool.
- [x] **Factory Method**: `createEvaluationScoringAgent()` in `controller_agent_factory.go`.
- [x] **LLM Selection**: Uses `selectEvaluationScoringLLM()` with `presetPhaseLLM` priority.
- [x] **Event Bridging**: Uses `CreateAndSetupStandardAgentWithConfig` for proper event emission.
- [x] **Scoring Execution**: Runs after all evaluation steps via `runEvaluationScoringPhase()`.
- [x] **Evaluation Report**: Generated at `evaluation/runs/{targetRunFolder}/evaluation_report.json`.
- [x] **Date/Time in Prompts**: Scoring agent includes current date/time in user prompt.
- [x] **Output Reading Fix**: `readStepExecutionOutput()` now reads from correct paths:
  - `logs/{stepFolder}/execution/` for execution result files
  - `logs/{stepFolder}/validation/` for validation files
  - `execution/{stepFolder}/` for step output files

### Automation
- [x] **Skip Human Input**: Evaluation mode auto-sets `SkipHumanInput=true` for automated runs.
- [x] **Validation Disabled**: Evaluation steps have `DisableValidation=true` (auto-approve).

### Tool Improvements
- [x] **Delete Tool**: Informative responses, Python list parsing, learnings cleanup.
- [x] **Add Tool**: Returns all step IDs after adding.

### Documentation & Verification
- [x] **Documentation**: Updated this plan with file storage locations.
- [x] **Build Verified**: All changes compile successfully.
