# Refactoring Plan: Todo Creation Human Controller

## Overview
The file `agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller.go` is over 4300 lines long. It handles everything from orchestrator lifecycle to step execution, progress tracking, file management, and agent creation.

## Goal
Split `controller.go` into multiple smaller, focused files within the `todo_creation_human` package to improve maintainability and readability.

## Proposed File Structure

All files will remain in `agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/` and belong to `package todo_creation_human`.

### 1. `controller.go` (Core)
Contains the main struct definition, constructor, and high-level entry points.
- **Structs**: `HumanControlledTodoPlannerOrchestrator`
- **Functions**:
    - `NewHumanControlledTodoPlannerOrchestrator`
    - `Execute`
    - `CreateTodoList`
    - `GetType`
    - `getSessionID`, `getWorkflowID`
    - `SetFastExecuteMode`, `IsFastExecuteStep`
    - `SetSkipHumanInput`, `IsSkipHumanInput`
    - `GetLearningDetailLevel`, `SetLearningDetailLevel`

### 2. `controller_types.go` (Types)
Contains data structures used across the controller.
- **Structs**:
    - `BranchStepProgress`
    - `StepProgress`
    - `TodoStep`
    - `TodoStepsExtractedEvent`

### 3. `controller_run_manager.go` (File/Folder Management)
Contains logic for managing run folders and workspace files.
- **Functions**:
    - `resolveRunFolder`
    - `listRunFolders`
    - `createRunFolderStructure`
    - `determineRunFolderForCleanup`
    - `shouldAskDeleteOldProgress`
    - `cleanupExecutionArtifactsForFreshStart`
    - `workspaceFileExists`

### 4. `controller_progress.go` (State Management)
Contains logic for tracking and persisting execution progress.
- **Functions**:
    - `getStepsProgressPath`
    - `loadStepProgress`
    - `saveStepProgress`
    - `deleteStepProgress`
    - `initializeFreshProgress`

### 5. `controller_execution.go` (Step Execution)
Contains the core logic for executing steps and handling control flow.
- **Functions**:
    - `runExecutionPhase`
    - `executeSingleStep`
    - `executeConditionalStep`
    - `handlePlanChange`
    - `requestHumanFeedback`
    - `formatValidationResponseForTemplate`
    - `sanitizeTitleForAgentName`
    - `max` (helper)

### 6. `controller_learning.go` (Learning Logic)
Contains logic for the learning phases (success/failure analysis).
- **Functions**:
    - `runSuccessLearningPhase`
    - `runFailureLearningPhase`
    - `extractRefinedTaskDescription`
    - `formatLearningHistoryForExecution`

### 7. `controller_agent_factory.go` (Agent Creation)
Contains methods for creating specific agents used by the controller.
- **Functions**:
    - `createExecutionAgent`
    - `createExecutionOnlyAgent`
    - `createLearningReadingAgent`
    - `createValidationAgent`
    - `createSuccessLearningAgent`
    - `createFailureLearningAgent`

## Implementation Steps
1.  Create the new files in `agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/`.
2.  Move the code chunk by chunk from `controller.go` to the respective new files.
3.  Ensure package declaration `package todo_creation_human` is at the top of every file.
4.  Copy necessary imports to each file.
5.  Run `go build ./...` to verify no compilation errors.
6.  Run tests to ensure no regression.
