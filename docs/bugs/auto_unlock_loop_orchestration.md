# Bug Report: Orchestration/Todo Task Auto-Unlock Loop

## Status: Fixed ✅

## Symptoms
- Workflow steps (particularly `orchestration` and `todo_task` sub-agents) keep resetting their learnings.
- Steps appear "Unlocked" in the UI even after being manually or automatically locked.
- Logs show frequent: `🔄 Step definition changed for ... (Hash mismatch) - Triggering learning reset`.
- UI occasionally shows steps as "Locked (Auto)" even when they have been reset backend-side.

## Root Cause
### 1. Hash Mismatch (The Loop)
The **Step Hash Guard** calculates a SHA256 hash of a step's Title, Description, and Success Criteria to detect plan modifications. If the hash changes, it resets learnings to ensure safety.

For **Orchestration** and **Todo Task** steps, the controller was modifying the sub-agent step object **in-place** (via pointers) to inject dynamic runtime instructions and success criteria. Because these instructions vary slightly between runs (LLM-generated), the hash always changed. The Hash Guard perceived this as a plan modification, triggering a reset and unlock on every execution.

### 2. In-Memory Plan Corruption
Because the modification was in-place on the `PlanStepInterface` pointers, the "original" plan in memory was being corrupted. Even if the Hash Guard tried to look at the original plan, it would see the mutated version.

### 3. Stale UI Indicators
The `ResetLearningMetadata` function was updating the `AutoUnlockedAt` timestamp but **not** clearing the `AutoLockedAt` fields. The UI uses the presence of `AutoLockedAt` to display the "Locked" badge, leading to a state where the backend had unlocked the step but the frontend still showed it as locked.

### 4. Broken Todo Task Instructions
In `TodoTaskPlanStep`, instructions were being passed to `executeSingleStep` via the `previousExecutionResults` parameter. However, that parameter is designed to be indexed by step number. Sub-agents do not align with global step indices, causing the instructions to be ignored or causing index-out-of-bounds logic in the prompt builder.

## The Fix
### 1. "Clone & Modify" Pattern
Modified both `controller_orchestration.go` and `controller_todo_task.go` to stop in-place modifications.
- The controller now creates a **shallow copy** of the `RegularPlanStep` struct.
- Dynamic instructions are appended to the **copy**.
- The **copy** is passed for execution, keeping the original plan pointer pristine.

### 2. Hash Guard Originality
Updated `CheckAndResetStepHash` in `controller_learning_helpers.go` to:
- Recursively search the `approvedPlan` (the static source of truth) for the original step definition.
- Use the **original static definition** for hash calculation, ignoring runtime overrides.

### 3. Metadata Cleanup
Updated `ResetLearningMetadata` to explicitly clear `AutoLockedAt`, `AutoLockReason`, and `AutoLockIteration` whenever a step is unlocked.

### 4. Robust Instruction Passing
In `TodoTaskPlanStep`, instructions are now embedded directly into the cloned step's `Description` (matching the Orchestration pattern) instead of relying on the fragile `previousExecutionResults` array.

## Verification
- Verified with `go build ./...` in `agent_go`.
- Manual verification of hash stability across iterations for orchestrated steps.
