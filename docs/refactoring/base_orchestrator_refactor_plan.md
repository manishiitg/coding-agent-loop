# Refactoring Plan: Base Orchestrator

## Overview
The file `agent_go/pkg/orchestrator/base_orchestrator.go` has grown to over 2500 lines, making it difficult to maintain and navigate. It acts as a "God Object" handling configuration, event emission, path validation, folder guarding, agent creation, workspace operations, human feedback, and token tracking.

## Goal
Split `base_orchestrator.go` into multiple smaller, focused files within the same `orchestrator` package. This will improve readability and maintainability without changing the external API or package structure.

## Proposed File Structure

All files will remain in `agent_go/pkg/orchestrator/` and belong to `package orchestrator`.

### 1. `base_orchestrator.go` (Core)
Contains the main struct definition, interface, and constructor.
- **Structs/Interfaces**: `Orchestrator`, `BaseOrchestrator`
- **Functions**:
    - `NewBaseOrchestrator`
    - `getMapKeys`

### 2. `base_orchestrator_types.go` (Types)
Contains configuration structs and common types.
- **Structs**: `LLMConfig`, `APIKeys`, `BedrockKey`, `StepTokenUsage`
- **Constants**: `OrchestratorType` constants

### 3. `base_orchestrator_getters.go` (Accessors)
Contains all getter and setter methods for `BaseOrchestrator`.
- **Functions**:
    - `GetLogger`
    - `GetStartTime`
    - `GetOrchestratorType`
    - `GetObjective`, `SetObjective`
    - `GetWorkspacePath`, `SetWorkspacePath`
    - `SetWorkspacePathForFolderGuard`, `GetFolderGuardPaths`
    - `GetContextAwareBridge`
    - `GetProvider`, `GetModel`, `GetMCPConfigPath`, `GetTemperature`
    - `GetAgentMode`
    - `GetSelectedServers`, `GetSelectedTools`
    - `GetUseCodeExecutionMode`
    - `GetLLMConfig`
    - `GetTracer`
    - `GetMaxTurns`
    - `GetType`

### 4. `base_orchestrator_events.go` (Event Handling)
Contains methods related to event emission.
- **Functions**:
    - `emitEvent`
    - `EmitOrchestratorStart`
    - `EmitOrchestratorEnd`
    - `EmitUnifiedCompletionEvent`

### 5. `base_orchestrator_paths.go` (Path Validation)
Contains logic for path validation and normalization.
- **Functions**:
    - `validatePathInWorkspace`
    - `validatePathInAllowedPaths`
    - `normalizePathForAllowedPaths`
    - `normalizePathForWorkspace`

### 6. `base_orchestrator_folder_guard.go` (Security)
Contains logic for the folder guard system (read/write access control).
- **Functions**:
    - `ShouldFilterWriteTool`
    - `EnhanceToolDescriptionWithFolderGuard`
    - `WrapWorkspaceToolsWithFolderGuard`
    - `PrepareWorkspaceToolsWithFolderGuard`

### 7. `base_orchestrator_agent_factory.go` (Agent Creation)
Contains methods for creating and configuring sub-agents.
- **Private Helpers**:
    - `createBaseAgentConfig`: Core logic for creating agent config.
    - `setupStandardAgent`: Core logic for initializing agent, connecting event bridge, and registering tools.
- **Public Wrappers** (Delegates to helpers):
    - `CreateStandardAgentConfig`
    - `CreateStandardAgentConfigWithCustomServers`
    - `CreateStandardAgentConfigWithLLM`
    - `CreateAndSetupStandardAgent`
    - `CreateAndSetupStandardAgentWithCustomServers`
    - `CreateAndSetupStandardAgentWithConfig`
    - `CreateAndSetupStandardAgentWithSystemPrompt`

### 8. `base_orchestrator_workspace.go` (File Operations)
Contains methods for interacting with the workspace file system.
- **Functions**:
    - `ReadWorkspaceFile`
    - `CheckWorkspaceFileExists`
    - `WriteWorkspaceFile`
    - `DeleteWorkspaceFile`
    - `CleanupDirectory`
    - `ListWorkspaceDirectories`
    - `ListWorkspaceFiles`

### 9. `base_orchestrator_feedback.go` (Human Interaction)
Contains methods for requesting human feedback.
- **Functions**:
    - `RequestHumanFeedback`
    - `RequestYesNoFeedback`
    - `RequestMultipleChoiceFeedback`

### 10. `base_orchestrator_tools.go` (Tool Management)
Contains methods for tool filtering and management.
- **Functions**:
    - `getToolNamesByCategory`
    - `ConvertOldFormatToNewFormat`
    - `FilterCustomToolsByCategory`

### 11. `base_orchestrator_tokens.go` (Observability)
Contains methods for tracking token usage.
- **Functions**:
    - `AccumulateStepTokens`
    - `GetStepTokenUsage`
    - `EmitStepTokenUsage`

## Implementation Steps
1.  Create the new files in `agent_go/pkg/orchestrator/`.
2.  Move the code chunk by chunk from `base_orchestrator.go` to the respective new files.
    -   **Optimization**: When moving code to `base_orchestrator_agent_factory.go`, refactor the duplicated setup logic from `CreateAndSetup...` functions into a single private `setupStandardAgent` method.
3.  Ensure package declaration `package orchestrator` is at the top of every file.
4.  Copy necessary imports to each file.
5.  Run `go build ./...` to verify no compilation errors.
6.  Run tests to ensure no regression.
