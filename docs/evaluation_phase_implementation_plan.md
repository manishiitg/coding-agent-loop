# Evaluation Phase Implementation Plan

## 📋 Overview

The Evaluation system consists of **two separate workflow phases**:

1. **Create Evaluation Plan**: User-driven phase to create evaluation guides/plans (similar to Planning phase)
2. **Execute Evaluation**: Execute evaluation plan steps against workflow execution results

**Key Characteristics:**
- **Phase 1 (Create)**: User-driven conversation to create evaluation guide, analyzing the **execution plan** (plan.json).
- **Phase 2 (Execute)**: Execute evaluation steps to check execution results and generate scores.
- **Evaluation Plan**: Creates `evaluation/evaluation_plan.json` (similar structure to `plan.json`).
- **Goal-Oriented**: Focuses on verifying if the overall objective was met with high quality.
- **Single Step Preference**: Defaults to a single, comprehensive evaluation step unless complexity demands more.

---

## 🎯 Goals

### Phase 1: Create Evaluation Plan
1. **Analyze Execution Plan**: Infer goals and success metrics directly from `planning/plan.json` (Objective is ignored).
2. **Confirm Strategy**: Mandatory `human_feedback` loop to confirm evaluation strategy before creating steps.
3. **Generate Evaluation Plan**: Create structured evaluation plan, defaulting to a single comprehensive step.

### Phase 2: Execute Evaluation
3. **Execute Evaluation Steps**: Run evaluation steps against workflow execution results.
4. **Score Evaluation**: Generate scores (0-10) based on success criteria.
5. **Store Results**: Save evaluation results in `evaluation/execution/`.

---

## 🏗️ Architecture Overview

### Phase 1: Create Evaluation Plan
Follows the **Independent Manager** pattern:
- **`EvaluationManager`**: Fully independent manager (like `PlanImprovementManager`) handling agent creation and execution.
- **`HumanControlledEvaluationAgent`**: Conversational agent with self-contained prompt logic.
- **Phase registration**: `"evaluation-planning"` in `workflow_orchestrator.go`.

### Phase 2: Execute Evaluation
**Reuses existing `Execute()` method** via wrapper:
- Uses same execution infrastructure (`StepBasedWorkflowOrchestrator.Execute()`).
- **Input**: `evaluation/evaluation_plan.json` instead of `planning/plan.json`.
- **Output**: `evaluation/execution/` folder instead of `runs/`.
- **Phase registration**: `"evaluation-execution"` in `workflow_orchestrator.go`.

---

## 📁 File Structure

### Created Files

**Phase 1: Create Evaluation Plan**
1. **`evaluation_types.go`**: Data structures
   - `EvaluationPlan` struct
   - `EvaluationStep` struct

2. **`evaluation_manager.go`**: Independent Manager
   - `CreateEvaluationGuideOnly()` method
   - Handles file operations, agent creation, and execution loop.
   - Refactored to be fully independent from Orchestrator methods where possible.

3. **`evaluation_agent.go`**: Conversational Agent
   - `HumanControlledEvaluationAgent`
   - Contains `evaluationSystemPromptProcessor` for self-contained prompt generation.
   - Tools: `add_evaluation_step`, `update_evaluation_step`, `delete_evaluation_step`, `human_feedback`.

**Phase 2: Execute Evaluation**
4. **`evaluation_execution.go`**: Execution Wrapper
   - `ExecuteEvaluationOnly()` method on `StepBasedWorkflowOrchestrator`.
   - Configures output folder to `evaluation/execution/`.

### Workspace Structure

```
workspace/step_based_workflow/
├── evaluation/
│   ├── evaluation_plan.json      # Evaluation plan
│   └── runs/                     # Evaluation execution results
│       ├── iteration-X/          # Evaluation results for Workflow Iteration X (No groups)
│       │   ├── step-1/           # Step 1 evaluation execution folder
│       │   └── steps_done.json   # Progress tracking for evaluation
│       └── iteration-X/group-Y/  # Evaluation results for Workflow Iteration X, Group Y
│           ├── step-1/           
│           └── steps_done.json
├── planning/
│   └── plan.json                 # Execution plan (Input for evaluation design)
└── runs/iteration-X/execution/   # Workflow execution results (Subject of evaluation)
```

---

## 🔄 Implementation Pattern

1. **Manager** (`EvaluationManager`): Independent manager using `CreateAndSetupStandardAgentWithConfig`.
2. **Agent** (`HumanControlledEvaluationAgent`): Conversational agent with tools.
3. **Tools**: Standard CRUD for evaluation steps + `human_feedback`.
4. **System Prompt**: 
   - **Source of Truth**: `planning/plan.json`.
   - **Pre-Validation**: Removed (optional/future feature).
   - **Strategy**: Prefer single, comprehensive step.
   - **Confirmation**: Mandatory `human_feedback` before action.
5. **File Operations**: Save/load `evaluation/evaluation_plan.json`.

---

## 🛠️ Cross-Folder Access (TARGET_RUN_PATH)

To allow evaluation steps (running in `evaluation/runs/...`) to access the original workflow artifacts (in `runs/...`), a special mechanism is used:

1.  **Variable Injection**: `ExecuteEvaluationOnly` calculates the absolute path to the target execution folder and injects it as `TARGET_RUN_PATH` into the agent's variable context.
2.  **Prompt Instruction**: The Evaluation Designer is explicitly instructed to use `{{TARGET_RUN_PATH}}` when referencing files to check (e.g., `Read file {{TARGET_RUN_PATH}}/output.json`).
3.  **Folder Guard Update**: The `setupExecutionFolderGuard` function dynamically adds `TARGET_RUN_PATH` to the allowed read paths if the variable is present, bypassing standard isolation rules for this specific path.

---

## 📝 Key Design Decisions

1.  **Infer Goals from Plan**: The global objective is removed from the context. The agent infers the goal solely from the execution plan.
2. **Single Step Default**: The prompt explicitly instructs to favor a single, holistic evaluation step over many granular checks.
3. **PreValidation Removed**: The requirement for a `PreValidation` schema was removed from the prompt to simplify the initial implementation and focus on high-level quality checks.
4. **Independent Manager**: `EvaluationManager` was refactored to be an independent struct, decoupling it from the main orchestrator's state where possible.
5. **Execution Folder**: Results are stored in `evaluation/execution/` to keep them distinct from regular workflow runs (`runs/`).

---

## ✅ Implementation Status

- [x] **Core Infrastructure**: Types, Manager, Agent created.
- [x] **Tool Implementation**: Add/Update/Delete steps + Human Feedback.
- [x] **Prompt Engineering**: Refined for goal inference, single-step preference, and mandatory confirmation.
- [x] **Execution Wrapper**: `ExecuteEvaluationOnly` implemented and integrated.
- [x] **Integration**: Phases registered in `workflow_orchestrator.go`.
- [x] **Documentation**: Updated `workflow_orchestrator.md` and this plan.
- [x] **Verification**: Build verified successfully.